package gameui

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
)

// DefaultScreenFontSize is the default virtual font size in points used for screen files when a node omits fontSize.
const DefaultScreenFontSize float32 = 12

// DeviceProfile describes a reference display size, safe-area insets, and device class used to preview and validate screens.
type DeviceProfile struct {
	// ID is the machine identifier (for example "phone", "tablet", "desktop", "ultrawide").
	ID string
	// Name is the human-readable profile name (for example "Phone 19.5:9 (notch)").
	Name string
	// Class is the device class matched against ScreenNodeDoc.DeviceClasses.
	Class DeviceClass
	// Width and Height are the virtual canvas dimensions in points at this aspect ratio.
	Width  float32
	Height float32
	// SafeArea specifies the notch, rounded-corner, and home-indicator insets in virtual points.
	SafeArea core.Edges
	// Touch reports whether this device uses touch input (and enforces minimum touch target size).
	Touch bool
}

// ReferenceDevices returns the four standard reference profiles for a 640×360 design canvas:
// Phone 19.5:9 with a notch (780×360), Tablet 4:3 (640×480), Desktop 16:9 (640×360), and Ultrawide 21:9 (840×360).
func ReferenceDevices() []DeviceProfile {
	return ReferenceDevicesFor(640, 360)
}

// ReferenceDevicesFor returns the four standard reference profiles scaled to match a screen's design size:
// Phone 19.5:9 with a notch, Tablet 4:3, Desktop 16:9, and Ultrawide 21:9.
func ReferenceDevicesFor(designW, designH float32) []DeviceProfile {
	if designW <= 0 {
		designW = 640
	}
	if designH <= 0 {
		designH = 360
	}
	scale := designH / 360
	phoneW := designH * 19.5 / 9.0
	tabletH := max(designH, designW*3.0/4.0)
	ultraW := designH * 21.0 / 9.0
	return []DeviceProfile{
		{
			ID:     "phone",
			Name:   "Phone 19.5:9 (notch)",
			Class:  DevicePhone,
			Width:  phoneW,
			Height: designH,
			SafeArea: core.Edges{
				Left:   core.Points(44 * scale),
				Right:  core.Points(44 * scale),
				Top:    0,
				Bottom: core.Points(20 * scale),
			},
			Touch: true,
		},
		{
			ID:     "tablet",
			Name:   "Tablet 4:3",
			Class:  DeviceTablet,
			Width:  designW,
			Height: tabletH,
			SafeArea: core.Edges{
				Top:    core.Points(20 * scale),
				Bottom: core.Points(20 * scale),
			},
			Touch: true,
		},
		{
			ID:     "desktop",
			Name:   "Desktop 16:9",
			Class:  DeviceDesktop,
			Width:  designW,
			Height: designH,
			Touch:  false,
		},
		{
			ID:     "ultrawide",
			Name:   "Ultrawide 21:9",
			Class:  DeviceUltrawide,
			Width:  ultraW,
			Height: designH,
			Touch:  false,
		},
	}
}

// MeasureScreenText estimates single-line text width and height in virtual points for screen layout and overflow checks.
// Full-width CJK and East Asian glyphs measure at 1.0× fontSize, spaces at 0.25× fontSize, and other glyphs at 0.5× fontSize.
// When fontSize <= 0, DefaultScreenFontSize (12) is used.
func MeasureScreenText(text string, fontSize float32) (width, height float32) {
	if fontSize <= 0 {
		fontSize = DefaultScreenFontSize
	}
	var w float32
	for _, r := range text {
		switch {
		case isWideScreenRune(r):
			w += fontSize
		case r == ' ':
			w += fontSize * 0.25
		default:
			w += fontSize * 0.5
		}
	}
	return w, fontSize * 1.25
}

func isWideScreenRune(r rune) bool {
	return (r >= 0x3000 && r <= 0x9FFF) ||
		(r >= 0xAC00 && r <= 0xD7AF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFF00 && r <= 0xFFEF) ||
		(r >= 0x20000 && r <= 0x2FA1F)
}

func screenTextMeasurer(text string, font core.Font) (float32, float32) {
	return MeasureScreenText(text, float32(font.Size))
}

// ScreenBuildOptions configures BuildScreen.
type ScreenBuildOptions struct {
	// SafeArea specifies the display safe-area insets in virtual points (for notches, rounded corners, and home indicators).
	SafeArea core.Edges
	// DeviceClass selects which conditional nodes (ScreenNodeDoc.DeviceClasses) are visible.
	// When empty, no device-class filtering is applied and all nodes remain visible.
	DeviceClass DeviceClass
	// ViewportWidth and ViewportHeight optionally override the Viewport's virtual dimensions (for example 780×360 for a 19.5:9 phone).
	// When zero, the document's DesignWidth and DesignHeight are used.
	ViewportWidth  float32
	ViewportHeight float32
	// Text resolves localization textKey values. When nil and Catalog is set, Catalog.T(Locale, key) is used; when both are nil, the key itself is displayed.
	Text func(key string) string
	// Catalog optionally resolves textKey values when Text is nil.
	Catalog *l10n.Catalog
	// Locale optionally selects the locale in Catalog (empty uses Catalog's active locale).
	Locale string
	// Theme optionally overrides the built Root's theme; nil uses DefaultTheme tuned for DefaultScreenFontSize and screen text measurement.
	Theme *Theme
	// OnClick optionally binds click callbacks by node ID for button and touch_button nodes.
	OnClick map[string]func()
	// Sprites optionally resolves a node's Sprite path (and optional SpriteFrame) into a core.ImageSource and source rectangle.
	Sprites func(path, frame string) (core.ImageSource, core.Bounds, bool)
}

type builtNodeRecord struct {
	doc            ScreenNodeDoc
	widget         Widget
	effectiveClass []DeviceClass
}

// BuiltScreen is the runtime UI tree constructed from a *.screen.yaml document.
type BuiltScreen struct {
	// Root is the interactive gameui.Root configured with the screen's Viewport and theme.
	Root *Root
	// Navigator is a SpatialNavigator attached to Root with explicit focus-order overrides wired.
	Navigator *SpatialNavigator
	// Doc is a clone of the ScreenDoc this screen was built from.
	Doc *ScreenDoc
	// Canvas is the root ScreenCanvas child of Root.
	Canvas *ScreenCanvas
	// Nodes maps every node ID in the screen to its built Widget.
	Nodes map[string]Widget
	// FocusOrder lists focusable widgets that declared an explicit focusOrder, sorted ascending.
	FocusOrder []Widget

	records []builtNodeRecord
}

// Widget returns the built Widget with id, or nil if no node has that ID.
func (s *BuiltScreen) Widget(id string) Widget {
	if s == nil {
		return nil
	}
	return s.Nodes[id]
}

// SetSafeArea updates the safe-area insets used by subsequent Layout and Draw calls.
func (s *BuiltScreen) SetSafeArea(insets core.Edges) {
	if s == nil || s.Canvas == nil {
		return
	}
	s.Canvas.SafeArea = insets
}

// SetDeviceClass updates conditional visibility for all nodes carrying deviceClasses restrictions.
// Passing an empty DeviceClass makes all nodes visible.
func (s *BuiltScreen) SetDeviceClass(class DeviceClass) {
	if s == nil {
		return
	}
	for i := range s.records {
		rec := &s.records[i]
		visible := deviceClassMatches(rec.effectiveClass, class)
		setWidgetHidden(rec.widget, !visible)
	}
}

// SetTextLookup refreshes all localized text nodes using fn (or the raw textKey when fn is nil).
func (s *BuiltScreen) SetTextLookup(fn func(key string) string) {
	if s == nil {
		return
	}
	resolve := func(key string) string {
		if key == "" {
			return ""
		}
		if fn != nil {
			return fn(key)
		}
		return key
	}
	for i := range s.records {
		rec := &s.records[i]
		if rec.doc.TextKey == "" {
			continue
		}
		txt := resolve(rec.doc.TextKey)
		switch w := rec.widget.(type) {
		case *Label:
			w.Text = txt
		case *Button:
			w.Text = txt
		case *TouchButton:
			w.Text = txt
		}
	}
}

// ApplyCatalog refreshes all localized text nodes from cat for locale (or cat's fallback locale when locale is empty).
func (s *BuiltScreen) ApplyCatalog(cat *l10n.Catalog, locale string) {
	if cat == nil {
		s.SetTextLookup(nil)
		return
	}
	loc := locale
	if loc == "" {
		loc = cat.FallbackLocale()
	}
	s.SetTextLookup(func(key string) string {
		return cat.T(loc, key)
	})
}

// Layout delegates to s.Root.Layout(bounds).
func (s *BuiltScreen) Layout(bounds core.Bounds) {
	if s != nil && s.Root != nil {
		s.Root.Layout(bounds)
	}
}

// Draw delegates to s.Root.Draw(d, bounds).
func (s *BuiltScreen) Draw(d *core.Drawing, bounds core.Bounds) {
	if s != nil && s.Root != nil {
		s.Root.Draw(d, bounds)
	}
}

// BuildScreen constructs a runtime BuiltScreen (including a configured gameui.Root, Viewport, safe-area anchoring,
// localized text, device-class visibility, and focus-order navigation) from doc.
func BuildScreen(doc *ScreenDoc, opts ScreenBuildOptions) (*BuiltScreen, error) {
	if err := ValidateScreen(doc); err != nil {
		return nil, err
	}

	var th Theme
	if opts.Theme != nil {
		th = resolveTheme(opts.Theme)
	} else {
		th = DefaultTheme()
		th.Font.Size = core.Points(DefaultScreenFontSize)
	}
	if th.TextMeasurer == nil {
		th.TextMeasurer = screenTextMeasurer
	}

	resolveText := opts.Text
	if resolveText == nil && opts.Catalog != nil {
		cat := opts.Catalog
		loc := opts.Locale
		if loc == "" {
			loc = cat.FallbackLocale()
		}
		resolveText = func(key string) string {
			return cat.T(loc, key)
		}
	}
	textFor := func(key string) string {
		if key == "" {
			return ""
		}
		if resolveText != nil {
			return resolveText(key)
		}
		return key
	}

	vw := doc.DesignWidth
	vh := doc.DesignHeight
	if opts.ViewportWidth > 0 {
		vw = opts.ViewportWidth
	}
	if opts.ViewportHeight > 0 {
		vh = opts.ViewportHeight
	}
	vp := NewViewport(vw, vh, doc.EffectiveScaleMode())
	vp.LetterboxColor = core.Transparent

	canvas := &ScreenCanvas{
		SafeArea: opts.SafeArea,
		Items:    make([]AnchoredItem, 0, len(doc.Nodes)),
	}
	built := &BuiltScreen{
		Doc:    doc.Clone(),
		Canvas: canvas,
		Nodes:  make(map[string]Widget),
	}

	type focusEntry struct {
		order  int
		widget Widget
	}
	var focusEntries []focusEntry

	var buildNode func(n ScreenNodeDoc, inherited []DeviceClass, inAnchoredParent bool) (Widget, error)
	buildNode = func(n ScreenNodeDoc, inherited []DeviceClass, inAnchoredParent bool) (Widget, error) {
		effClasses := intersectDeviceClasses(inherited, n.DeviceClasses)
		hidden := !deviceClassMatches(effClasses, opts.DeviceClass)

		var bgColor core.Color
		if n.Background != "" {
			c, err := core.ParseColor(n.Background)
			if err != nil {
				return nil, fmt.Errorf("gameui: node %q background: %w", n.ID, err)
			}
			bgColor = c
		}
		var textColor core.Color
		if n.TextColor != "" {
			c, err := core.ParseColor(n.TextColor)
			if err != nil {
				return nil, fmt.Errorf("gameui: node %q textColor: %w", n.ID, err)
			}
			textColor = c
		}
		var font core.Font
		if n.FontSize > 0 {
			font.Size = core.Points(n.FontSize)
		}
		pad := n.Padding.Edges()

		width := n.Width
		if n.SizeMode == SizeFitText {
			width = 0
		}

		var w Widget
		switch n.Type {
		case NodeVBox:
			box := &VBox{
				Spacing:  n.Spacing,
				Padding:  pad,
				Width:    width,
				Height:   n.Height,
				Hidden:   hidden,
				Children: make([]Widget, 0, len(n.Children)),
			}
			if bgColor != 0 {
				cr := n.CornerRadius
				box.DrawBackground = func(d *core.Drawing, b core.Bounds) {
					d.SetFillColor(bgColor).FillPath(rectPath(float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height), max(0, cr)), core.NonZero)
				}
			}
			for _, ch := range n.Children {
				cw, err := buildNode(ch, effClasses, false)
				if err != nil {
					return nil, err
				}
				box.Children = append(box.Children, cw)
			}
			w = box

		case NodeHBox:
			box := &HBox{
				Spacing:  n.Spacing,
				Padding:  pad,
				Width:    width,
				Height:   n.Height,
				Hidden:   hidden,
				Children: make([]Widget, 0, len(n.Children)),
			}
			if bgColor != 0 {
				cr := n.CornerRadius
				box.DrawBackground = func(d *core.Drawing, b core.Bounds) {
					d.SetFillColor(bgColor).FillPath(rectPath(float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height), max(0, cr)), core.NonZero)
				}
			}
			for _, ch := range n.Children {
				cw, err := buildNode(ch, effClasses, false)
				if err != nil {
					return nil, err
				}
				box.Children = append(box.Children, cw)
			}
			w = box

		case NodeContainer:
			sub := &ScreenCanvas{
				Background:   bgColor,
				CornerRadius: n.CornerRadius,
				Width:        width,
				Height:       n.Height,
				Hidden:       hidden,
				Items:        make([]AnchoredItem, 0, len(n.Children)),
			}
			for _, ch := range n.Children {
				cw, err := buildNode(ch, effClasses, true)
				if err != nil {
					return nil, err
				}
				sub.Items = append(sub.Items, makeAnchoredItem(ch, cw))
			}
			w = sub

		case NodeLabel:
			align := core.TextAlignStart
			if n.Anchor == AnchorTopCenter || n.Anchor == AnchorCenter || n.Anchor == AnchorBottomCenter {
				align = core.TextAlignCenter
			}
			w = &Label{
				Text:         textFor(n.TextKey),
				Color:        textColor,
				Background:   bgColor,
				CornerRadius: n.CornerRadius,
				Padding:      pad,
				Font:         font,
				TextAlign:    align,
				Width:        width,
				Height:       n.Height,
				Hidden:       hidden,
			}

		case NodeButton:
			btn := &Button{
				Text:         textFor(n.TextKey),
				OnClick:      opts.OnClick[n.ID],
				Font:         font,
				TextColor:    textColor,
				Background:   bgColor,
				CornerRadius: n.CornerRadius,
				Padding:      pad,
				FocusOrder:   n.FocusOrder,
				TouchHitSize: n.TouchHitSize,
				Width:        width,
				Height:       n.Height,
				Hidden:       hidden,
			}
			w = btn

		case NodeSpriteRow:
			sr := &SpriteRow{
				SpritePath:  n.Sprite,
				SpriteFrame: n.SpriteFrame,
				Count:       n.SpriteCount,
				Spacing:     n.Spacing,
				Color:       bgColor,
				Width:       width,
				Height:      n.Height,
				Hidden:      hidden,
			}
			if n.Sprite != "" && opts.Sprites != nil {
				if img, src, ok := opts.Sprites(n.Sprite, n.SpriteFrame); ok {
					sr.Image = img
					sr.Src = src
					sr.HasImage = true
				}
			}
			w = sr

		case NodeColorRect:
			w = &ColorRect{
				Color:        bgColor,
				CornerRadius: n.CornerRadius,
				Width:        width,
				Height:       n.Height,
				Hidden:       hidden,
			}

		case NodeImageProgress:
			ip := &ImageProgress{
				Value:      n.Value,
				Min:        n.Min,
				Max:        n.Max,
				StartAngle: n.StartAngle,
				Span:       n.Span,
				Width:      width,
				Height:     n.Height,
				Hidden:     hidden,
			}
			if fill, ok := ParseFillMode(n.Fill); ok {
				ip.Fill = fill
			}
			if opts.Sprites != nil {
				layers := []struct {
					path string
					out  **NineSlice
				}{
					{n.Under, &ip.Under},
					{n.Progress, &ip.Progress},
					{n.Over, &ip.Over},
				}
				for _, layer := range layers {
					if layer.path == "" {
						continue
					}
					if img, src, ok := opts.Sprites(layer.path, ""); ok {
						*layer.out = &NineSlice{Image: img, Src: src}
					}
				}
			}
			w = ip

		case NodeTouchStick:
			w = &TouchStick{
				HitSize:   n.TouchHitSize,
				BaseColor: bgColor,
				KnobColor: textColor,
				Width:     width,
				Height:    n.Height,
				Hidden:    hidden,
			}

		case NodeTouchButton:
			w = &TouchButton{
				Text:       textFor(n.TextKey),
				HitSize:    n.TouchHitSize,
				OnClick:    opts.OnClick[n.ID],
				Font:       font,
				TextColor:  textColor,
				Background: bgColor,
				FocusOrder: n.FocusOrder,
				Width:      width,
				Height:     n.Height,
				Hidden:     hidden,
			}
		}

		built.Nodes[n.ID] = w
		built.records = append(built.records, builtNodeRecord{
			doc:            n,
			widget:         w,
			effectiveClass: effClasses,
		})
		if n.FocusOrder > 0 && isFocusableKind(n.Type) {
			focusEntries = append(focusEntries, focusEntry{order: n.FocusOrder, widget: w})
		}
		_ = inAnchoredParent
		return w, nil
	}

	for _, node := range doc.Nodes {
		w, err := buildNode(node, nil, true)
		if err != nil {
			return nil, err
		}
		canvas.Items = append(canvas.Items, makeAnchoredItem(node, w))
	}

	root := NewRoot(canvas)
	root.Theme = &th
	root.Viewport = &vp

	nav := NewSpatialNavigator(root)
	slices.SortFunc(focusEntries, func(a, b focusEntry) int {
		return a.order - b.order
	})
	if len(focusEntries) > 0 {
		built.FocusOrder = make([]Widget, len(focusEntries))
		for i, fe := range focusEntries {
			built.FocusOrder[i] = fe.widget
		}
		if len(focusEntries) > 1 {
			for i, fe := range focusEntries {
				prev := focusEntries[(i-1+len(focusEntries))%len(focusEntries)].widget
				next := focusEntries[(i+1)%len(focusEntries)].widget
				nav.SetOverrides(fe.widget, NavOverrides{
					Up:   prev,
					Down: next,
				})
			}
		}
	}

	built.Root = root
	built.Navigator = nav
	return built, nil
}

func makeAnchoredItem(n ScreenNodeDoc, w Widget) AnchoredItem {
	var off ScreenOffsetDoc
	if n.Offset != nil {
		off = *n.Offset
	}
	passThrough := n.Type == NodeContainer && n.Width == 0 && n.Height == 0 && (n.Anchor == "" || n.Anchor == AnchorFill) && off == (ScreenOffsetDoc{})
	return AnchoredItem{
		ID:          n.ID,
		Widget:      w,
		Anchor:      n.EffectiveAnchor(),
		Offset:      off,
		SafeArea:    n.KeepInSafeArea(),
		SizeMode:    n.SizeMode,
		FixedWidth:  n.Width,
		FixedHeight: n.Height,
		PassThrough: passThrough,
	}
}

func isFocusableKind(t ScreenNodeType) bool {
	return t == NodeButton || t == NodeTouchButton
}

func intersectDeviceClasses(parent, child []DeviceClass) []DeviceClass {
	if len(parent) == 0 {
		return slices.Clone(child)
	}
	if len(child) == 0 {
		return slices.Clone(parent)
	}
	var out []DeviceClass
	for _, c := range child {
		if slices.Contains(parent, c) {
			out = append(out, c)
		}
	}
	return out
}

func deviceClassMatches(allowed []DeviceClass, current DeviceClass) bool {
	if current == "" || len(allowed) == 0 {
		return true
	}
	return slices.Contains(allowed, current)
}

func setWidgetHidden(w Widget, hidden bool) {
	switch v := w.(type) {
	case *VBox:
		v.Hidden = hidden
	case *HBox:
		v.Hidden = hidden
	case *Panel:
		v.Hidden = hidden
	case *Label:
		v.Hidden = hidden
	case *Button:
		v.Hidden = hidden
	case *Toggle:
		v.Hidden = hidden
	case *Slider:
		v.Hidden = hidden
	case *ProgressBar:
		v.Hidden = hidden
	case *Spacer:
		v.Hidden = hidden
	case *List:
		v.Hidden = hidden
	case *ColorRect:
		v.Hidden = hidden
	case *SpriteRow:
		v.Hidden = hidden
	case *ImageProgress:
		v.Hidden = hidden
	case *TouchStick:
		v.Hidden = hidden
	case *TouchButton:
		v.Hidden = hidden
	case *ScreenCanvas:
		v.Hidden = hidden
	}
}
