package worldmap

import (
	"bytes"
	"image"
	"image/color"
	_ "image/png"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/fog"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// DefaultTileColorProperty is the tile custom property name checked first when choosing a tile's minimap color.
const DefaultTileColorProperty = "minimap_color"

// TileMinimapOptions configures how NewTileMinimap resolves tile colors and selects layers.
type TileMinimapOptions struct {
	// ColorProperty names the tile custom property providing an explicit minimap color (parsed via tilemap.Properties.Color). Empty uses DefaultTileColorProperty.
	ColorProperty string
	// Images maps tileset image URIs (with or without an "asset:" prefix) or tileset names to decoded image.Image pixel data for average-color sampling. Tilesets whose Image.Pixels or Image.Data is already populated do not need an entry.
	Images map[string]image.Image
	// Layers limits rendering to these tile layer indices in order; nil or empty composites every visible tile layer in map order.
	Layers []int
	// TileColor optionally overrides color lookup for a stripped tile GID; returning ok=false falls back to ColorProperty and then the tile's average tileset image color.
	TileColor func(tileID uint32) (core.Color, bool)
}

// TileMinimapStyle sets the colors, stroke widths, marker sizes, and padding used by TileMinimap.Draw.
//
// A zero (transparent) color leaves that element out, except Fog: when both Fog.Unseen and Fog.Explored are transparent, fog.DefaultStyle() is used. Non-positive widths or sizes use DefaultTileMinimapStyle's.
type TileMinimapStyle struct {
	// Background fills the destination rectangle before drawing the terrain image.
	Background core.Color
	// Border outlines the destination rectangle with BorderWidth.
	Border      core.Color
	BorderWidth float32
	// Fog sets the colors laid over Unseen and Explored cells when a fog.Fog is attached. When both are transparent, fog.DefaultStyle() is used.
	Fog fog.Style
	// Camera outlines the camera's visible rectangle with CameraWidth.
	Camera      core.Color
	CameraWidth float32
	// Marker is the default color for markers that do not set Color or match MarkerKinds.
	Marker      core.Color
	MarkerKinds map[string]core.Color
	MarkerSize  float32
	// Padding insets the fitted map inside the destination rectangle, in points.
	Padding float32
}

// DefaultTileMinimapStyle returns a dark style with a white camera frame, gold default markers, and fog.DefaultStyle().
func DefaultTileMinimapStyle() TileMinimapStyle {
	return TileMinimapStyle{
		Background:  core.RGBA(0x0b0f1a, 0.85),
		Border:      core.RGB(0xc8d6ec),
		BorderWidth: 1,
		Fog:         fog.DefaultStyle(),
		Camera:      core.RGB(0xffffff),
		CameraWidth: 1.5,
		Marker:      core.RGB(0xf2c14e),
		MarkerSize:  4,
		Padding:     4,
	}
}

func (s *TileMinimapStyle) resolved() TileMinimapStyle {
	def := DefaultTileMinimapStyle()
	if s == nil {
		return def
	}
	out := *s
	if out.BorderWidth <= 0 {
		out.BorderWidth = def.BorderWidth
	}
	if out.CameraWidth <= 0 {
		out.CameraWidth = def.CameraWidth
	}
	if out.MarkerSize <= 0 {
		out.MarkerSize = def.MarkerSize
	}
	if out.Padding < 0 {
		out.Padding = 0
	}
	if !visible(out.Fog.Unseen) && !visible(out.Fog.Explored) {
		out.Fog = def.Fog
	}
	return out
}

// TileMarker is a unit blip or point of interest drawn on a TileMinimap in world coordinates.
type TileMarker struct {
	// Position is the marker's location in world coordinates.
	Position math2d.Vec2
	// Kind selects a color from TileMinimapStyle.MarkerKinds when Color is transparent.
	Kind string
	// Color, when non-transparent, overrides Kind and TileMinimapStyle.Marker for this marker.
	Color core.Color
	// Size is the marker square's width and height in minimap points; <= 0 uses TileMinimapStyle.MarkerSize.
	Size float32
	// RequireVisible, when true and a fog.Fog is drawn, hides this marker unless its map cell is currently fog.Visible to the drawn team.
	RequireVisible bool
}

// TileMinimapOverlay holds the per-frame overlays drawn on top of a TileMinimap's terrain.
type TileMinimapOverlay struct {
	// Fog, when non-nil, overlays Team's fog of war on the minimap image, skipping recomposition when Fog.Revision(Team) has not changed.
	Fog *fog.Fog
	// Team selects which team's fog of war to display.
	Team fog.Team
	// Camera, when non-nil, draws the camera's visible rectangle over the minimap.
	Camera *tilemap.Camera
	// Markers are unit blips and points of interest drawn in world coordinates.
	Markers []TileMarker
}

// TileMinimap renders a tilemap.Tilemap's tile layers to a compact cached RGBA image with one color per tile,
// incrementally updating only cells changed via Tilemap.SetTile (or small FillRect calls), optionally overlaying a team's
// fog.Fog cached by fog.Revision, and drawing unit/POI markers and the camera's visible rectangle.
//
// Orthogonal maps render 1 pixel per tile; isometric maps render as a solid diamond aligned with Tilemap.WorldBounds.
// A TileMinimap is not safe for concurrent use.
type TileMinimap struct {
	m             *tilemap.Tilemap
	colorProperty string
	images        map[string]image.Image
	layers        []int
	tileColorFn   func(uint32) (core.Color, bool)

	width       int
	height      int
	orientation tilemap.Orientation

	terrain      *image.RGBA
	terrainVer   uint64
	tileRev      uint64
	terrainDirty bool

	composited *image.RGBA
	compVer    uint64
	fogPtr     *fog.Fog
	fogTeam    fog.Team
	fogRev     uint64
	fogStyle   fog.Style
	fogReady   bool
	lastHasFog bool

	colorCache   map[uint32]color.RGBA
	decodedCache map[*tilemap.Tileset]image.Image

	// Telemetry counters for verification and tests.
	terrainRebuilds uint64
	cellUpdates     uint64
	fogComposites   uint64
}

// NewTileMinimap builds a TileMinimap for m and renders its initial terrain image.
func NewTileMinimap(m *tilemap.Tilemap, opts *TileMinimapOptions) *TileMinimap {
	tm := &TileMinimap{
		m:             m,
		colorProperty: DefaultTileColorProperty,
		colorCache:    make(map[uint32]color.RGBA),
		decodedCache:  make(map[*tilemap.Tileset]image.Image),
		terrainDirty:  true,
	}
	if opts != nil {
		if opts.ColorProperty != "" {
			tm.colorProperty = opts.ColorProperty
		}
		if len(opts.Images) > 0 {
			tm.images = make(map[string]image.Image, len(opts.Images))
			for k, v := range opts.Images {
				tm.images[k] = v
			}
		}
		if len(opts.Layers) > 0 {
			tm.layers = slices.Clone(opts.Layers)
		}
		tm.tileColorFn = opts.TileColor
	}
	if m != nil {
		tm.syncTerrain(nil, 0, fog.Style{})
	}
	return tm
}

// Invalidate clears cached tile colors and forces the next Sync or Draw to rebuild the full terrain image.
// Call this after mutating layer.Data directly, toggling layer.Visible/Opacity, or editing tile properties/tilesets.
func (tm *TileMinimap) Invalidate() {
	if tm == nil {
		return
	}
	clear(tm.colorCache)
	clear(tm.decodedCache)
	tm.terrainDirty = true
	tm.fogReady = false
}

// TerrainImage returns the cached fog-free terrain image, or nil for an empty map.
func (tm *TileMinimap) TerrainImage() *image.RGBA {
	if tm == nil {
		return nil
	}
	return tm.terrain
}

// Image returns the latest rendered minimap image: the fog-composited image when the last Sync or Draw used a fog.Fog, otherwise the terrain image.
func (tm *TileMinimap) Image() *image.RGBA {
	if tm == nil {
		return nil
	}
	if tm.lastHasFog && tm.composited != nil {
		return tm.composited
	}
	return tm.terrain
}

// Sync brings the cached terrain image (incrementally for SetTile edits) and optional fog overlay (skipping unchanged fog.Revision frames) up to date, returning the core.ImageSource ready to draw.
func (tm *TileMinimap) Sync(f *fog.Fog, team fog.Team, style *TileMinimapStyle) core.ImageSource {
	if tm == nil || tm.m == nil {
		return core.ImageSource{}
	}
	st := style.resolved()
	tm.syncTerrain(f, team, st.Fog)
	if tm.terrain == nil {
		tm.lastHasFog = false
		return core.ImageSource{}
	}
	if f == nil {
		tm.lastHasFog = false
		return core.ImageSource{
			Pixels:  tm.terrain,
			Version: tm.terrainVer,
			Size:    core.Size{Width: core.Points(tm.terrain.Bounds().Dx()), Height: core.Points(tm.terrain.Bounds().Dy())},
		}
	}
	tm.syncFog(f, team, st.Fog)
	tm.lastHasFog = true
	return core.ImageSource{
		Pixels:  tm.composited,
		Version: tm.compVer,
		Size:    core.Size{Width: core.Points(tm.composited.Bounds().Dx()), Height: core.Points(tm.composited.Bounds().Dy())},
	}
}

func (tm *TileMinimap) imageDimensions(w, h int, orient tilemap.Orientation) (imgW, imgH int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	if orient == tilemap.OrientationIsometric {
		return w + h, w + h - 1
	}
	return w, h
}

func (tm *TileMinimap) syncTerrain(f *fog.Fog, team fog.Team, fogStyle fog.Style) {
	m := tm.m
	if m == nil || m.Width <= 0 || m.Height <= 0 {
		tm.terrain = nil
		tm.composited = nil
		tm.width, tm.height = 0, 0
		tm.fogReady = false
		return
	}

	needFull := tm.terrainDirty || tm.terrain == nil || tm.width != m.Width || tm.height != m.Height || tm.orientation != m.Orientation
	if needFull {
		imgW, imgH := tm.imageDimensions(m.Width, m.Height, m.Orientation)
		if tm.terrain == nil || tm.terrain.Bounds().Dx() != imgW || tm.terrain.Bounds().Dy() != imgH {
			tm.terrain = image.NewRGBA(image.Rect(0, 0, imgW, imgH))
			tm.composited = nil
		} else {
			clear(tm.terrain.Pix)
		}
		tm.width = m.Width
		tm.height = m.Height
		tm.orientation = m.Orientation
		for row := range m.Height {
			for col := range m.Width {
				tm.writeTerrainCell(col, row, tm.composeCell(col, row))
			}
		}
		tm.tileRev = m.TileRevision()
		tm.terrainDirty = false
		tm.terrainVer++
		tm.terrainRebuilds++
		tm.fogReady = false
		return
	}

	curRev := m.TileRevision()
	if curRev == tm.tileRev {
		return
	}

	// If fog is already composited for the same (fog, team, revision, style), update only the changed cells in composited too so a single SetTile stays O(1) even with fog enabled.
	patchFog := f != nil && tm.fogReady && tm.composited != nil &&
		tm.fogPtr == f && tm.fogTeam == team && tm.fogRev == f.Revision(team) && tm.fogStyle == fogStyle
	var unseenFog, exploredFog premulColor
	if patchFog {
		unseenFog = premulFromCore(fogStyle.Unseen)
		exploredFog = premulFromCore(fogStyle.Explored)
	}

	updated := false
	newRev, ok := m.TileChangesSince(tm.tileRev, func(col, row int) {
		if col < 0 || row < 0 || col >= tm.width || row >= tm.height {
			return
		}
		c := tm.composeCell(col, row)
		tm.writeTerrainCell(col, row, c)
		tm.cellUpdates++
		updated = true
		if patchFog {
			tm.writeFoggedCell(col, row, c, f.State(team, col, row), unseenFog, exploredFog)
		}
	})
	if !ok {
		tm.terrainDirty = true
		tm.syncTerrain(f, team, fogStyle)
		return
	}
	tm.tileRev = newRev
	if updated {
		tm.terrainVer++
		if patchFog {
			tm.compVer++
		} else {
			tm.fogReady = false
		}
	}
}

func (tm *TileMinimap) syncFog(f *fog.Fog, team fog.Team, fogStyle fog.Style) {
	rev := f.Revision(team)
	if tm.fogReady && tm.composited != nil &&
		tm.fogPtr == f && tm.fogTeam == team && tm.fogRev == rev && tm.fogStyle == fogStyle {
		return
	}

	b := tm.terrain.Bounds()
	if tm.composited == nil || tm.composited.Bounds() != b {
		tm.composited = image.NewRGBA(b)
	} else if tm.orientation == tilemap.OrientationIsometric {
		// Pixels outside the diamond stay transparent.
		clear(tm.composited.Pix)
	}

	unseen := premulFromCore(fogStyle.Unseen)
	explored := premulFromCore(fogStyle.Explored)
	w, h := tm.width, tm.height

	if tm.orientation == tilemap.OrientationIsometric {
		tPix, tStride := tm.terrain.Pix, tm.terrain.Stride
		cPix, cStride := tm.composited.Pix, tm.composited.Stride
		for row := range h {
			for col := range w {
				x0 := col - row + h - 1
				y := col + row
				tOff := y*tStride + x0*4
				base := color.RGBA{R: tPix[tOff], G: tPix[tOff+1], B: tPix[tOff+2], A: tPix[tOff+3]}
				out := applyFogColor(base, f.State(team, col, row), unseen, explored)
				cOff := y*cStride + x0*4
				cPix[cOff+0] = out.R
				cPix[cOff+1] = out.G
				cPix[cOff+2] = out.B
				cPix[cOff+3] = out.A
				cPix[cOff+4] = out.R
				cPix[cOff+5] = out.G
				cPix[cOff+6] = out.B
				cPix[cOff+7] = out.A
			}
		}
	} else {
		tPix, tStride := tm.terrain.Pix, tm.terrain.Stride
		cPix, cStride := tm.composited.Pix, tm.composited.Stride
		for row := range h {
			tRow := tPix[row*tStride:]
			cRow := cPix[row*cStride:]
			for col := range w {
				off := col * 4
				base := color.RGBA{R: tRow[off], G: tRow[off+1], B: tRow[off+2], A: tRow[off+3]}
				out := applyFogColor(base, f.State(team, col, row), unseen, explored)
				cRow[off+0] = out.R
				cRow[off+1] = out.G
				cRow[off+2] = out.B
				cRow[off+3] = out.A
			}
		}
	}

	tm.fogPtr = f
	tm.fogTeam = team
	tm.fogRev = rev
	tm.fogStyle = fogStyle
	tm.fogReady = true
	tm.compVer++
	tm.fogComposites++
}

func (tm *TileMinimap) writeTerrainCell(col, row int, c color.RGBA) {
	if tm.orientation == tilemap.OrientationIsometric {
		x0 := col - row + tm.height - 1
		y := col + row
		off := y*tm.terrain.Stride + x0*4
		tm.terrain.Pix[off+0] = c.R
		tm.terrain.Pix[off+1] = c.G
		tm.terrain.Pix[off+2] = c.B
		tm.terrain.Pix[off+3] = c.A
		tm.terrain.Pix[off+4] = c.R
		tm.terrain.Pix[off+5] = c.G
		tm.terrain.Pix[off+6] = c.B
		tm.terrain.Pix[off+7] = c.A
		return
	}
	off := row*tm.terrain.Stride + col*4
	tm.terrain.Pix[off+0] = c.R
	tm.terrain.Pix[off+1] = c.G
	tm.terrain.Pix[off+2] = c.B
	tm.terrain.Pix[off+3] = c.A
}

func (tm *TileMinimap) writeFoggedCell(col, row int, base color.RGBA, state fog.State, unseen, explored premulColor) {
	out := applyFogColor(base, state, unseen, explored)
	if tm.orientation == tilemap.OrientationIsometric {
		x0 := col - row + tm.height - 1
		y := col + row
		off := y*tm.composited.Stride + x0*4
		tm.composited.Pix[off+0] = out.R
		tm.composited.Pix[off+1] = out.G
		tm.composited.Pix[off+2] = out.B
		tm.composited.Pix[off+3] = out.A
		tm.composited.Pix[off+4] = out.R
		tm.composited.Pix[off+5] = out.G
		tm.composited.Pix[off+6] = out.B
		tm.composited.Pix[off+7] = out.A
		return
	}
	off := row*tm.composited.Stride + col*4
	tm.composited.Pix[off+0] = out.R
	tm.composited.Pix[off+1] = out.G
	tm.composited.Pix[off+2] = out.B
	tm.composited.Pix[off+3] = out.A
}

func (tm *TileMinimap) composeCell(col, row int) color.RGBA {
	m := tm.m
	var out color.RGBA
	applyLayer := func(li int) {
		if li < 0 || li >= len(m.Layers) {
			return
		}
		l := &m.Layers[li]
		if !l.Visible || l.Opacity <= 0 {
			return
		}
		gid := m.TileAt(li, col, row)
		if gid == 0 {
			return
		}
		c := tm.tileColor(gid)
		if c.A == 0 {
			return
		}
		if l.Opacity < 1 {
			c = scalePremul(c, l.Opacity)
			if c.A == 0 {
				return
			}
		}
		out = overPremul(c, out)
	}
	if len(tm.layers) > 0 {
		for _, li := range tm.layers {
			applyLayer(li)
		}
		return out
	}
	for li := range m.Layers {
		applyLayer(li)
	}
	return out
}

func (tm *TileMinimap) tileColor(gid uint32) color.RGBA {
	id := gid & tilemap.TileIDMask
	if id == 0 {
		return color.RGBA{}
	}
	if c, ok := tm.colorCache[id]; ok {
		return c
	}
	c := tm.resolveTileColor(id)
	tm.colorCache[id] = c
	return c
}

func (tm *TileMinimap) resolveTileColor(tileID uint32) color.RGBA {
	if tm.tileColorFn != nil {
		if c, ok := tm.tileColorFn(tileID); ok {
			return premulRGBA(c)
		}
	}
	if props := tm.m.TileProperties(tileID); props != nil {
		if c, ok := props.Color(tm.colorProperty); ok {
			return premulRGBA(c)
		}
	}
	ts, localID := tm.m.TilesetForGID(tileID)
	if ts == nil {
		return color.RGBA{}
	}
	return tm.averageTilesetColor(ts, localID)
}

func (tm *TileMinimap) averageTilesetColor(ts *tilemap.Tileset, localID uint32) color.RGBA {
	if ts.Animations != nil {
		if anim, ok := ts.Animations[localID]; ok && anim != nil && len(anim.Frames) > 0 {
			localID = anim.Frames[0].TileID
		}
	}

	var imgSrc core.ImageSource
	var sx, sy, sw, sh float32
	if len(ts.Frames) > 0 {
		if int(localID) >= len(ts.Frames) {
			return color.RGBA{}
		}
		f := ts.Frames[localID]
		if f.W <= 0 || f.H <= 0 {
			return color.RGBA{}
		}
		imgSrc = ts.Image
		if ts.Atlas != nil && imgSrc.URI == "" && len(imgSrc.Data) == 0 && imgSrc.Pixels == nil {
			imgSrc = ts.Atlas.Image
		}
		sx, sy, sw, sh = f.X, f.Y, f.W, f.H
	} else {
		if ts.Columns <= 0 || ts.TileWidth <= 0 || ts.TileHeight <= 0 {
			return color.RGBA{}
		}
		if ts.TileCount > 0 && int(localID) >= ts.TileCount {
			return color.RGBA{}
		}
		col := int(localID) % ts.Columns
		row := int(localID) / ts.Columns
		imgSrc = ts.Image
		sx = ts.Margin + float32(col)*(ts.TileWidth+ts.Spacing)
		sy = ts.Margin + float32(row)*(ts.TileHeight+ts.Spacing)
		sw, sh = ts.TileWidth, ts.TileHeight
	}

	src := tm.resolveTilesetImage(ts, imgSrc)
	if src == nil {
		return color.RGBA{}
	}
	b := src.Bounds()
	x0 := max(b.Min.X, b.Min.X+int(math.Round(float64(sx))))
	y0 := max(b.Min.Y, b.Min.Y+int(math.Round(float64(sy))))
	x1 := min(b.Max.X, b.Min.X+int(math.Round(float64(sx+sw))))
	y1 := min(b.Max.Y, b.Min.Y+int(math.Round(float64(sy+sh))))
	if x0 >= x1 || y0 >= y1 {
		return color.RGBA{}
	}

	var sumR, sumG, sumB, sumA, count uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, bl, a := src.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			sumR += uint64(r)
			sumG += uint64(g)
			sumB += uint64(bl)
			sumA += uint64(a)
			count++
		}
	}
	if sumA == 0 || count == 0 {
		return color.RGBA{}
	}
	// Alpha-weighted mean straight RGB of visible pixels, and mean alpha of visible pixels.
	// Ignoring fully transparent pixels preserves opaque color for isometric diamond tiles whose bounding-box corners are transparent.
	sr := uint8(min((sumR*255+sumA/2)/sumA, 255))
	sg := uint8(min((sumG*255+sumA/2)/sumA, 255))
	sb := uint8(min((sumB*255+sumA/2)/sumA, 255))
	denom := uint64(0x101) * count
	sa := uint8(min((sumA+denom/2)/denom, 255))
	return color.RGBA{
		R: uint8((uint32(sr)*uint32(sa) + 127) / 255),
		G: uint8((uint32(sg)*uint32(sa) + 127) / 255),
		B: uint8((uint32(sb)*uint32(sa) + 127) / 255),
		A: sa,
	}
}

func (tm *TileMinimap) resolveTilesetImage(ts *tilemap.Tileset, imgSrc core.ImageSource) image.Image {
	if imgSrc.Pixels != nil {
		return imgSrc.Pixels
	}
	if img, ok := tm.decodedCache[ts]; ok {
		return img
	}
	if len(imgSrc.Data) > 0 {
		if img, _, err := image.Decode(bytes.NewReader(imgSrc.Data)); err == nil {
			tm.decodedCache[ts] = img
			return img
		}
	}
	if len(tm.images) > 0 {
		if imgSrc.URI != "" {
			if img, ok := tm.images[imgSrc.URI]; ok && img != nil {
				tm.decodedCache[ts] = img
				return img
			}
			if trimmed, ok := strings.CutPrefix(imgSrc.URI, "asset:"); ok {
				if img, ok := tm.images[trimmed]; ok && img != nil {
					tm.decodedCache[ts] = img
					return img
				}
			}
		}
		if ts.Name != "" {
			if img, ok := tm.images[ts.Name]; ok && img != nil {
				tm.decodedCache[ts] = img
				return img
			}
		}
	}
	return nil
}

// Projection fits the tilemap's WorldBounds into dst inset by padding, preserving aspect ratio and centering it.
// It returns false when the tilemap is empty or the inset dst is empty.
func (tm *TileMinimap) Projection(dst core.Bounds, padding float32) (Projection, bool) {
	if tm == nil || tm.m == nil || tm.m.Width <= 0 || tm.m.Height <= 0 || tm.m.TileWidth <= 0 || tm.m.TileHeight <= 0 {
		return Projection{}, false
	}
	if padding < 0 {
		padding = 0
	}
	box := math2d.FromBounds(dst).Expand(-padding)
	if box.Empty() {
		return Projection{}, false
	}
	wb := math2d.FromBounds(tm.m.WorldBounds())
	if wb.Empty() {
		return Projection{}, false
	}
	scale := min(box.W/wb.W, box.H/wb.H)
	if !(scale > 0) || !finite(scale) {
		return Projection{}, false
	}
	bc, wc := box.Center(), wb.Center()
	return Projection{
		Scale:  scale,
		Offset: math2d.Vec2{X: bc.X - wc.X*scale, Y: bc.Y - wc.Y*scale},
	}, true
}

// WorldToMinimap converts a world-space point into minimap coordinates inside dst using style's Padding.
func (tm *TileMinimap) WorldToMinimap(dst core.Bounds, world math2d.Vec2, style *TileMinimapStyle) (math2d.Vec2, bool) {
	if !finite(world.X) || !finite(world.Y) {
		return math2d.Vec2{}, false
	}
	st := style.resolved()
	proj, ok := tm.Projection(dst, st.Padding)
	if !ok {
		return math2d.Vec2{}, false
	}
	return proj.ToMap(world), true
}

// MinimapToWorld converts a point in dst's coordinate space (such as a pointer click) into world coordinates using style's Padding.
func (tm *TileMinimap) MinimapToWorld(dst core.Bounds, pos math2d.Vec2, style *TileMinimapStyle) (math2d.Vec2, bool) {
	if !finite(pos.X) || !finite(pos.Y) {
		return math2d.Vec2{}, false
	}
	st := style.resolved()
	proj, ok := tm.Projection(dst, st.Padding)
	if !ok {
		return math2d.Vec2{}, false
	}
	return proj.ToWorld(pos), true
}

// PanCamera centers cam on the world point under click when click lies inside dst, then calls cam.Clamp().
// It returns false without modifying cam when cam is nil, click is outside dst or non-finite, or the minimap projection is empty.
func (tm *TileMinimap) PanCamera(cam *tilemap.Camera, dst core.Bounds, click math2d.Vec2, style *TileMinimapStyle) bool {
	if cam == nil || !finite(click.X) || !finite(click.Y) {
		return false
	}
	if !math2d.FromBounds(dst).Contains(click) {
		return false
	}
	world, ok := tm.MinimapToWorld(dst, click, style)
	if !ok || !finite(world.X) || !finite(world.Y) {
		return false
	}
	cam.X = world.X
	cam.Y = world.Y
	cam.Clamp()
	return true
}

func (tm *TileMinimap) imageWorldRect() math2d.Rect {
	wb := math2d.FromBounds(tm.m.WorldBounds())
	if tm.orientation == tilemap.OrientationIsometric {
		// In the (W+H) x (W+H-1) diamond image, each row spans half a tile vertically and centers
		// on TileToWorld(col, row).Y = (col+row+1)*(TileHeight/2), inset by TileHeight/4 at top and bottom.
		inset := tm.m.TileHeight / 4
		return math2d.Rect{
			X: wb.X,
			Y: wb.Y + inset,
			W: wb.W,
			H: wb.H - 2*inset,
		}
	}
	return wb
}

// Draw updates the minimap image incrementally and draws the terrain, optional fog, unit/POI markers, and camera rectangle into d, clipped to dst.
func (tm *TileMinimap) Draw(d *core.Drawing, dst core.Bounds, overlay *TileMinimapOverlay, style *TileMinimapStyle) {
	if tm == nil || d == nil {
		return
	}
	box := math2d.FromBounds(dst)
	if box.Empty() {
		return
	}
	st := style.resolved()

	d.Save()
	d.BeginPath().Rect(box.X, box.Y, box.W, box.H).Clip().BeginPath()
	if visible(st.Background) {
		d.SetFillColor(st.Background).FillRect(box.X, box.Y, box.W, box.H)
	}

	if proj, ok := tm.Projection(dst, st.Padding); ok {
		var f *fog.Fog
		var team fog.Team
		if overlay != nil {
			f = overlay.Fog
			team = overlay.Team
		}
		imgSrc := tm.Sync(f, team, &st)
		if imgSrc.Pixels != nil {
			ir := proj.RectToMap(tm.imageWorldRect())
			d.SetImageSmoothing(false).DrawImageIn(imgSrc, ir.X, ir.Y, ir.W, ir.H)
		}

		if overlay != nil && len(overlay.Markers) > 0 {
			for i := range overlay.Markers {
				mk := &overlay.Markers[i]
				if !finite(mk.Position.X) || !finite(mk.Position.Y) {
					continue
				}
				if mk.RequireVisible && f != nil {
					col, row := tm.m.WorldToTile(core.Point{X: core.Points(mk.Position.X), Y: core.Points(mk.Position.Y)})
					if !f.Visible(team, col, row) {
						continue
					}
				}
				c := mk.Color
				if !visible(c) {
					if kc, ok := st.MarkerKinds[mk.Kind]; ok {
						c = kc
					} else {
						c = st.Marker
					}
				}
				if !visible(c) {
					continue
				}
				sz := mk.Size
				if sz <= 0 {
					sz = st.MarkerSize
				}
				p := proj.ToMap(mk.Position)
				h := sz / 2
				r := math2d.Rect{X: p.X - h, Y: p.Y - h, W: sz, H: sz}
				if !r.Overlaps(box) {
					continue
				}
				d.SetFillColor(c).FillRect(r.X, r.Y, r.W, r.H)
			}
		}

		if overlay != nil && overlay.Camera != nil && visible(st.Camera) {
			cam := overlay.Camera
			if cam.Width > 0 && cam.Height > 0 {
				if cam.Rotation == 0 {
					cr := proj.RectToMap(math2d.FromBounds(cam.VisibleBounds()))
					d.BeginPath().Rect(cr.X, cr.Y, cr.W, cr.H).SetStrokeColor(st.Camera).SetLineWidth(st.CameraWidth).Stroke().BeginPath()
				} else {
					corners := [4]core.Point{
						cam.ViewToWorld(core.Point{X: 0, Y: 0}),
						cam.ViewToWorld(core.Point{X: core.Points(cam.Width), Y: 0}),
						cam.ViewToWorld(core.Point{X: core.Points(cam.Width), Y: core.Points(cam.Height)}),
						cam.ViewToWorld(core.Point{X: 0, Y: core.Points(cam.Height)}),
					}
					p0 := proj.ToMap(math2d.Vec2{X: float32(corners[0].X), Y: float32(corners[0].Y)})
					p1 := proj.ToMap(math2d.Vec2{X: float32(corners[1].X), Y: float32(corners[1].Y)})
					p2 := proj.ToMap(math2d.Vec2{X: float32(corners[2].X), Y: float32(corners[2].Y)})
					p3 := proj.ToMap(math2d.Vec2{X: float32(corners[3].X), Y: float32(corners[3].Y)})
					d.BeginPath().
						MoveTo(p0.X, p0.Y).
						LineTo(p1.X, p1.Y).
						LineTo(p2.X, p2.Y).
						LineTo(p3.X, p3.Y).
						ClosePath().
						SetStrokeColor(st.Camera).
						SetLineWidth(st.CameraWidth).
						Stroke().
						BeginPath()
				}
			}
		}
	}

	if visible(st.Border) {
		d.BeginPath().Rect(box.X, box.Y, box.W, box.H).SetStrokeColor(st.Border).SetLineWidth(st.BorderWidth).Stroke().BeginPath()
	}
	d.Restore()
}

type premulColor struct {
	r, g, b, a uint32
	invA       uint32
}

func premulFromCore(c core.Color) premulColor {
	a := uint32(c & 0xff)
	if a == 0 {
		return premulColor{invA: 255}
	}
	r := uint32((c >> 24) & 0xff)
	g := uint32((c >> 16) & 0xff)
	b := uint32((c >> 8) & 0xff)
	if a == 255 {
		return premulColor{r: r, g: g, b: b, a: 255, invA: 0}
	}
	return premulColor{
		r:    (r*a + 127) / 255,
		g:    (g*a + 127) / 255,
		b:    (b*a + 127) / 255,
		a:    a,
		invA: 255 - a,
	}
}

func premulRGBA(c core.Color) color.RGBA {
	p := premulFromCore(c)
	return color.RGBA{R: uint8(p.r), G: uint8(p.g), B: uint8(p.b), A: uint8(p.a)}
}

func scalePremul(c color.RGBA, opacity float32) color.RGBA {
	if opacity <= 0 {
		return color.RGBA{}
	}
	if opacity >= 1 {
		return c
	}
	s := uint32(math.Round(float64(opacity) * 255))
	return color.RGBA{
		R: uint8((uint32(c.R)*s + 127) / 255),
		G: uint8((uint32(c.G)*s + 127) / 255),
		B: uint8((uint32(c.B)*s + 127) / 255),
		A: uint8((uint32(c.A)*s + 127) / 255),
	}
}

func overPremul(src, dst color.RGBA) color.RGBA {
	if src.A == 0 {
		return dst
	}
	if src.A == 255 || dst.A == 0 {
		return src
	}
	invA := uint32(255 - src.A)
	return color.RGBA{
		R: uint8(min(uint32(src.R)+(uint32(dst.R)*invA+127)/255, 255)),
		G: uint8(min(uint32(src.G)+(uint32(dst.G)*invA+127)/255, 255)),
		B: uint8(min(uint32(src.B)+(uint32(dst.B)*invA+127)/255, 255)),
		A: uint8(min(uint32(src.A)+(uint32(dst.A)*invA+127)/255, 255)),
	}
}

func applyFogColor(base color.RGBA, state fog.State, unseen, explored premulColor) color.RGBA {
	var ov premulColor
	switch state {
	case fog.Visible:
		return base
	case fog.Explored:
		ov = explored
	default:
		ov = unseen
	}
	if ov.a == 0 {
		return base
	}
	if ov.a == 255 {
		return color.RGBA{R: uint8(ov.r), G: uint8(ov.g), B: uint8(ov.b), A: 255}
	}
	return color.RGBA{
		R: uint8(min(ov.r+(uint32(base.R)*ov.invA+127)/255, 255)),
		G: uint8(min(ov.g+(uint32(base.G)*ov.invA+127)/255, 255)),
		B: uint8(min(ov.b+(uint32(base.B)*ov.invA+127)/255, 255)),
		A: uint8(min(ov.a+(uint32(base.A)*ov.invA+127)/255, 255)),
	}
}
