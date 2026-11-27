package gameui

import (
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// WorldProjection maps 2D game world coordinates into screen/window coordinates and defines
// the visible screen bounds used to automatically hide off-screen world anchors.
type WorldProjection struct {
	// WorldToScreen converts a world position (worldX, worldY) to screen/window coordinates (screenX, screenY).
	WorldToScreen func(worldX, worldY float32) (screenX, screenY float32)
	// ScreenBounds is the visible screen rectangle in window coordinates.
	// When non-zero, projected positions outside ScreenBounds are treated as off-screen and hidden.
	ScreenBounds core.Bounds
}

// NewWorldProjection constructs a WorldProjection from a custom projection function and visible screen bounds.
func NewWorldProjection(worldToScreen func(worldX, worldY float32) (screenX, screenY float32), screenBounds core.Bounds) WorldProjection {
	return WorldProjection{
		WorldToScreen: worldToScreen,
		ScreenBounds:  screenBounds,
	}
}

// NewCameraViewportProjection creates a WorldProjection combining a 2D camera centered at (camX, camY)
// with zoom and view dimensions (viewW, viewH) with a virtual-resolution Viewport (including scaling and letterboxing).
// Points projecting into letterbox bars or outside the window fall outside ScreenBounds.
func NewCameraViewportProjection(camX, camY, zoom, viewW, viewH float32, vp Viewport) WorldProjection {
	if zoom <= 0 {
		zoom = 1
	}
	if viewW <= 0 {
		viewW = vp.VirtualWidth
	}
	if viewH <= 0 {
		viewH = vp.VirtualHeight
	}
	halfW := viewW / 2
	halfH := viewH / 2

	if vp.WindowWidth > 0 && vp.WindowHeight > 0 {
		if vp.VirtualWidth <= 0 {
			vp.VirtualWidth = viewW
		}
		if vp.VirtualHeight <= 0 {
			vp.VirtualHeight = viewH
		}
		return WorldProjection{
			WorldToScreen: func(worldX, worldY float32) (float32, float32) {
				vx := (worldX-camX)*zoom + halfW
				vy := (worldY-camY)*zoom + halfH
				pt := vp.VirtualToWindow(core.Point{X: core.Points(vx), Y: core.Points(vy)})
				return float32(pt.X), float32(pt.Y)
			},
			ScreenBounds: vp.VisibleBounds(),
		}
	}

	return WorldProjection{
		WorldToScreen: func(worldX, worldY float32) (float32, float32) {
			return (worldX-camX)*zoom + halfW, (worldY-camY)*zoom + halfH
		},
		ScreenBounds: core.Bounds{
			Size: core.Size{Width: core.Points(viewW), Height: core.Points(viewH)},
		},
	}
}

// Project maps (worldX, worldY) to screen coordinates and reports whether the projected point is on-screen.
func (p WorldProjection) Project(worldX, worldY float32) (pt core.Point, onScreen bool) {
	sx, sy := worldX, worldY
	if p.WorldToScreen != nil {
		sx, sy = p.WorldToScreen(worldX, worldY)
	}
	pt = core.Point{X: core.Points(sx), Y: core.Points(sy)}

	if p.ScreenBounds == (core.Bounds{}) {
		return pt, true
	}
	const eps = 1e-4
	x0 := float32(p.ScreenBounds.Origin.X)
	y0 := float32(p.ScreenBounds.Origin.Y)
	x1 := x0 + float32(p.ScreenBounds.Size.Width)
	y1 := y0 + float32(p.ScreenBounds.Size.Height)
	onScreen = sx >= x0-eps && sx <= x1+eps && sy >= y0-eps && sy <= y1+eps
	return pt, onScreen
}

// WorldAnchor binds a native NSGo View (such as a name tag, speech bubble, or text input)
// to a 2D world position with optional screen-space pixel offset and normalized anchor pivot.
type WorldAnchor struct {
	// ID uniquely identifies this anchor inside an AnchorOverlay.
	ID string
	// WorldX and WorldY are the anchor's coordinates in 2D game world space.
	WorldX, WorldY float32
	// OffsetX and OffsetY shift the anchored view in screen points after world projection.
	OffsetX, OffsetY float32
	// AnchorX and AnchorY position the view relative to the projected point as fractions of the view's own size:
	// (0, 0) = top-left at point, (0.5, 0.5) = centered on point, (0.5, 1.0) = centered directly above point.
	AnchorX, AnchorY float32
	// Visible toggles whether this anchor is shown when on-screen.
	Visible bool
	// View builds the native NSGo view mounted at this anchor's screen position.
	View func(*core.Scope) core.View
	// TestID optionally assigns a TestID to the positioned anchor wrapper view.
	TestID string
}

// NewWorldAnchor returns a visible WorldAnchor at (worldX, worldY) rendering view.
func NewWorldAnchor(id string, worldX, worldY float32, view func(*core.Scope) core.View) *WorldAnchor {
	return &WorldAnchor{
		ID:      id,
		WorldX:  worldX,
		WorldY:  worldY,
		Visible: true,
		View:    view,
	}
}

type anchorScreenPos struct {
	ScreenX float32
	ScreenY float32
	AnchorX float32
	AnchorY float32
}

type anchorEntry struct {
	anchor      *WorldAnchor
	visible     *core.Signal[bool]
	pos         *core.Signal[anchorScreenPos]
	lastScreen  core.Point
	lastVisible bool
}

// AnchorOverlay renders native NSGo views anchored to 2D world positions inside a non-blocking overlay
// (PointerEventsNone on the overlay container, PointerEventsOn on each anchored view).
// Call Sync(proj) each frame to update screen coordinates in the same commit and automatically hide off-screen anchors.
type AnchorOverlay struct {
	// TestID optionally sets the TestID on the overlay container view.
	TestID string

	cx          *core.Scope
	order       *core.Signal[[]string]
	ids         []string
	entries     map[string]*anchorEntry
	lastProj    WorldProjection
	hasLastProj bool
}

// NewAnchorOverlay creates a signal-driven AnchorOverlay bound to cx.
func NewAnchorOverlay(cx *core.Scope) *AnchorOverlay {
	return &AnchorOverlay{
		cx:      cx,
		order:   core.NewSignalFunc(cx, []string(nil), slices.Equal),
		entries: make(map[string]*anchorEntry),
	}
}

// Add registers or replaces anchor by its ID.
// If Sync has already been called, the new anchor is immediately projected with the last WorldProjection.
func (o *AnchorOverlay) Add(anchor *WorldAnchor) {
	if o == nil || anchor == nil || anchor.ID == "" {
		return
	}
	if existing, ok := o.entries[anchor.ID]; ok {
		existing.anchor = anchor
		if o.hasLastProj {
			o.syncEntry(existing, o.lastProj)
		}
		return
	}

	entry := &anchorEntry{
		anchor:  anchor,
		visible: core.NewSignal(o.cx, false),
		pos:     core.NewSignal(o.cx, anchorScreenPos{}),
	}
	o.entries[anchor.ID] = entry
	o.ids = append(o.ids, anchor.ID)
	if o.hasLastProj {
		o.syncEntry(entry, o.lastProj)
	}
	o.order.Set(slices.Clone(o.ids))
}

// Remove deletes the anchor with id from the overlay. Returns true if it existed.
func (o *AnchorOverlay) Remove(id string) bool {
	if o == nil || id == "" {
		return false
	}
	if _, ok := o.entries[id]; !ok {
		return false
	}
	delete(o.entries, id)
	for i, existingID := range o.ids {
		if existingID == id {
			o.ids = append(o.ids[:i], o.ids[i+1:]...)
			break
		}
	}
	o.order.Set(slices.Clone(o.ids))
	return true
}

// Get returns the registered WorldAnchor with id, or nil if not found.
func (o *AnchorOverlay) Get(id string) *WorldAnchor {
	if o == nil {
		return nil
	}
	if entry, ok := o.entries[id]; ok {
		return entry.anchor
	}
	return nil
}

// SetWorldPosition updates an anchor's world coordinates. Call Sync to project and commit updated screen positions.
func (o *AnchorOverlay) SetWorldPosition(id string, worldX, worldY float32) bool {
	a := o.Get(id)
	if a == nil {
		return false
	}
	a.WorldX = worldX
	a.WorldY = worldY
	return true
}

// SetVisible updates an anchor's logical visibility flag. Call Sync to apply.
func (o *AnchorOverlay) SetVisible(id string, visible bool) bool {
	a := o.Get(id)
	if a == nil {
		return false
	}
	a.Visible = visible
	return true
}

// ScreenPosition returns the last synced screen position (including OffsetX/OffsetY) and on-screen visibility for id.
func (o *AnchorOverlay) ScreenPosition(id string) (core.Point, bool) {
	if o == nil {
		return core.Point{}, false
	}
	entry, ok := o.entries[id]
	if !ok {
		return core.Point{}, false
	}
	return entry.lastScreen, entry.lastVisible
}

// Sync projects all registered anchors through proj and atomically updates their screen positions
// and visibility signals in a single batch so anchored views stay aligned within the same frame
// and off-screen anchors are hidden immediately.
func (o *AnchorOverlay) Sync(proj WorldProjection) {
	if o == nil {
		return
	}
	o.lastProj = proj
	o.hasLastProj = true

	update := func() {
		for _, id := range o.ids {
			if entry := o.entries[id]; entry != nil {
				o.syncEntry(entry, proj)
			}
		}
	}
	if o.cx != nil {
		o.cx.Batch(update)
	} else {
		update()
	}
}

func (o *AnchorOverlay) syncEntry(entry *anchorEntry, proj WorldProjection) {
	a := entry.anchor
	pt, onScreen := proj.Project(a.WorldX, a.WorldY)
	vis := a.Visible && onScreen
	screenPt := core.Point{
		X: core.Points(float32(pt.X) + a.OffsetX),
		Y: core.Points(float32(pt.Y) + a.OffsetY),
	}
	entry.lastScreen = screenPt
	entry.lastVisible = vis
	entry.visible.Set(vis)
	if vis {
		entry.pos.Set(anchorScreenPos{
			ScreenX: float32(screenPt.X),
			ScreenY: float32(screenPt.Y),
			AnchorX: a.AnchorX,
			AnchorY: a.AnchorY,
		})
	}
}

// View returns the non-blocking absolute overlay core.View rendering all currently visible world anchors.
// The container uses PointerEventsNone so clicks pass through to the game canvas underneath, while each
// anchored wrapper re-enables interaction with PointerEventsOn.
func (o *AnchorOverlay) View(_ *core.Scope) core.View {
	return core.View{
		TestID: o.TestID,
		Style: core.Style{
			Position:      core.Absolute,
			Top:           core.Pt(0),
			Left:          core.Pt(0),
			Right:         core.Pt(0),
			Bottom:        core.Pt(0),
			PointerEvents: core.PointerEventsNone,
		},
		Children: []core.Node{
			core.Each(
				func() []string { return o.order.Get() },
				func(id string) string { return id },
				func(_ *core.Scope, item func() string, _ func() int) core.Node {
					id := item()
					entry := o.entries[id]
					if entry == nil {
						return nil
					}
					return core.Show{
						When: func() bool {
							return entry.visible.Get()
						},
						Then: func(cx *core.Scope) core.Node {
							var children []core.Node
							if entry.anchor != nil && entry.anchor.View != nil {
								children = []core.Node{entry.anchor.View(cx)}
							}
							var testID string
							if entry.anchor != nil {
								testID = entry.anchor.TestID
							}
							return core.View{
								TestID: testID,
								StyleFunc: func() core.Style {
									p := entry.pos.Get()
									st := core.Style{
										Position:      core.Absolute,
										Left:          core.Pt(core.Points(p.ScreenX)),
										Top:           core.Pt(core.Points(p.ScreenY)),
										PointerEvents: core.PointerEventsOn,
									}
									if p.AnchorX != 0 || p.AnchorY != 0 {
										st.Transform = core.Transform{
											TranslateXPercent: -p.AnchorX * 100,
											TranslateYPercent: -p.AnchorY * 100,
										}
									}
									return st
								},
								Children: children,
							}
						},
					}
				},
			),
		},
	}
}
