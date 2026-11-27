package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	mapWidth  = 320
	mapHeight = 180

	travelerStartX = 160
	travelerStartY = 146
	travelerSpeed  = 92.0 // virtual points per second

	portalCenterX = 160
	portalCenterY = 88
	portalRadius  = 28.0

	courtyardMinX = 38
	courtyardMaxX = 282
	courtyardMinY = 24
	courtyardMaxY = 158
)

var (
	mapAtlasOnce   sync.Once
	mapAtlasCached *sprite.Atlas
)

// Map2D is the top-down 2D Courtyard Overworld scene paired with the 3D Lantern Shrine.
// It uses game/scene.Scene, tilemap.Camera, and a sprite.Batch drawn into the host's 2D Canvas.
type Map2D struct {
	Scene    *scene.Scene
	Camera   *tilemap.Camera
	Atlas    *sprite.Atlas
	Traveler sprite.Sprite

	PrevX     float32
	PrevY     float32
	Time      float64
	WispPhase float64
	Steps     uint64
	PropBatch *sprite.Batch
	lanterns  [4]sprite.Sprite
	crateSp   sprite.Sprite
	crystalSp sprite.Sprite
}

// NewMap2D constructs the 2D courtyard map scene with its sprite atlas, world layers, and portal indicator.
func NewMap2D() *Map2D {
	atlas := courtyardAtlas()
	cam := &tilemap.Camera{
		X:      mapWidth / 2,
		Y:      mapHeight / 2,
		Zoom:   1,
		Width:  mapWidth,
		Height: mapHeight,
	}
	sc := scene.NewScene(cam)

	m := &Map2D{
		Scene:  sc,
		Camera: cam,
		Atlas:  atlas,
	}

	m.Traveler = sprite.NewSprite(atlas.Frames["traveler"])
	m.Traveler.Z = 20

	cornerOffsets := [4][2]float32{
		{72, 42},
		{248, 42},
		{72, 134},
		{248, 134},
	}
	for i, pt := range cornerOffsets {
		sp := sprite.NewSprite(atlas.Frames["lantern"])
		sp.X, sp.Y = pt[0], pt[1]
		sp.Z = 10
		m.lanterns[i] = sp
	}

	m.crateSp = sprite.NewSprite(atlas.Frames["crate"])
	m.crateSp.X, m.crateSp.Y = 104, 108
	m.crateSp.Z = 12

	m.crystalSp = sprite.NewSprite(atlas.Frames["crystal"])
	m.crystalSp.X, m.crystalSp.Y = 216, 70
	m.crystalSp.Z = 12

	m.Reset()

	// Layer 0: Twilight courtyard flagstones, central dais, glowing shrine portal, and orbiting wisp.
	floorLayer := sc.AddLayer(scene.NewLayer("courtyard", scene.SpaceWorld, 0))
	floorLayer.AddDrawable(scene.NewDrawable(m.drawCourtyardFloor))

	// Layer 10: Corner lanterns, cedar crate, azure crystal cluster, and the movable traveler sprite.
	propsLayer := sc.AddLayer(scene.NewLayer("props", scene.SpaceWorld, 10))
	m.PropBatch = &sprite.Batch{
		Atlas: atlas,
		Sprites: []*sprite.Sprite{
			&m.lanterns[0],
			&m.lanterns[1],
			&m.lanterns[2],
			&m.lanterns[3],
			&m.crateSp,
			&m.crystalSp,
			&m.Traveler,
		},
	}
	propsLayer.AddBatch(m.PropBatch)

	// Layer 100: Screen-space shrine portal proximity cue.
	cueLayer := sc.AddLayer(scene.NewLayer("portal-cue", scene.SpaceScreen, 100))
	cueLayer.AddDrawable(scene.NewDrawable(m.drawPortalCue))

	return m
}

// Reset returns the traveler to the southern courtyard entrance.
func (m *Map2D) Reset() {
	m.Traveler.X = travelerStartX
	m.Traveler.Y = travelerStartY
	m.PrevX = travelerStartX
	m.PrevY = travelerStartY
}

// Step advances the 2D traveler from shared input axes (moveX, moveY) and updates the courtyard glow & wisp.
func (m *Map2D) Step(dt time.Duration, moveX, moveY, lightPhase float64) {
	if dt <= 0 {
		return
	}
	sec := dt.Seconds()
	m.Time += sec
	m.WispPhase = lightPhase
	m.Steps++

	m.PrevX = m.Traveler.X
	m.PrevY = m.Traveler.Y

	if moveX != 0 || moveY != 0 {
		if len2 := moveX*moveX + moveY*moveY; len2 > 1 {
			inv := 1.0 / math.Sqrt(len2)
			moveX *= inv
			moveY *= inv
		}
		m.Traveler.X += float32(moveX * travelerSpeed * sec)
		m.Traveler.Y += float32(moveY * travelerSpeed * sec)
		m.Traveler.X = clampFloat32(m.Traveler.X, courtyardMinX, courtyardMaxX)
		m.Traveler.Y = clampFloat32(m.Traveler.Y, courtyardMinY, courtyardMaxY)
		if moveX < -0.05 {
			m.Traveler.FlipX = true
		} else if moveX > 0.05 {
			m.Traveler.FlipX = false
		}
	}
	m.Scene.Update(dt)
}

// NearPortal reports whether the traveler is standing inside the glowing shrine portal ring.
func (m *Map2D) NearPortal() bool {
	dx := float64(m.Traveler.X - portalCenterX)
	dy := float64(m.Traveler.Y - portalCenterY)
	return math.Hypot(dx, dy) <= portalRadius
}

// TravelerPos returns the traveler's current 2D courtyard coordinates.
func (m *Map2D) TravelerPos() math2d.Vec2 {
	return math2d.Vec2{X: m.Traveler.X, Y: m.Traveler.Y}
}

// Draw interpolates the traveler by alpha and renders the 2D courtyard scene.
func (m *Map2D) Draw(d *core.Drawing, alpha float64) {
	if d == nil {
		return
	}
	a := float32(max(0.0, min(1.0, alpha)))
	savedX, savedY := m.Traveler.X, m.Traveler.Y
	m.Traveler.X = m.PrevX + (savedX-m.PrevX)*a
	m.Traveler.Y = m.PrevY + (savedY-m.PrevY)*a
	m.Scene.Draw(d)
	m.Traveler.X, m.Traveler.Y = savedX, savedY
}

func fillCircle(d *core.Drawing, cx, cy, r float32) {
	d.BeginPath().Arc(cx, cy, r, 0, 2*math.Pi, false).Fill()
}

func strokeCircle(d *core.Drawing, cx, cy, r float32) {
	d.BeginPath().Arc(cx, cy, r, 0, 2*math.Pi, false).Stroke()
}

func (m *Map2D) drawCourtyardFloor(d *core.Drawing) {
	// Night sky backdrop.
	d.SetFillColor(core.RGB(clearSkyHex))
	d.FillRect(0, 0, mapWidth, mapHeight)

	// Outer stone courtyard enclosure.
	d.SetFillColor(core.RGB(0x162033))
	d.BeginPath().RoundRect(24, 14, mapWidth-48, mapHeight-28, 10).Fill()
	d.SetStrokeColor(core.RGB(0x2E3F5E))
	d.SetLineWidth(1.5)
	d.BeginPath().RoundRect(24, 14, mapWidth-48, mapHeight-28, 10).Stroke()

	// Flagstone tile grid.
	const tile = 16
	for y := 22; y < mapHeight-24; y += tile {
		for x := 32; x < mapWidth-34; x += tile {
			if ((x/tile)+(y/tile))%2 == 0 {
				d.SetFillColor(core.RGB(0x1C283E))
			} else {
				d.SetFillColor(core.RGB(0x192438))
			}
			d.FillRect(float32(x)+1, float32(y)+1, tile-2, tile-2)
		}
	}

	// Southern entrance path leading north to the shrine dais.
	d.SetFillColor(core.RGB(0x25344E))
	d.FillRect(portalCenterX-14, portalCenterY+22, 28, 56)

	// Warm glow pools beneath the four corner lanterns.
	pulse := float32(0.88 + 0.12*math.Sin(m.Time*2.8))
	for _, sp := range m.lanterns {
		d.SetFillColor(core.RGBA(0xF59E0B, 0.14*pulse))
		fillCircle(d, sp.X, sp.Y, 18)
	}

	// Central stepped dais & pulsing 3D Shrine Portal.
	d.SetFillColor(core.RGB(0x2B3A55))
	fillCircle(d, portalCenterX, portalCenterY, 32)
	d.SetFillColor(core.RGB(0x36496A))
	fillCircle(d, portalCenterX, portalCenterY, 24)

	portalAlpha := float32(0.32 + 0.16*math.Sin(m.Time*3.6))
	if m.NearPortal() {
		portalAlpha = 0.65
	}
	d.SetFillColor(core.RGBA(0xFBBF24, portalAlpha))
	fillCircle(d, portalCenterX, portalCenterY, 17)
	d.SetFillColor(core.RGB(0xFDE68A))
	fillCircle(d, portalCenterX, portalCenterY, 8)
	d.SetStrokeColor(core.RGBA(0xF8D27A, 0.85))
	d.SetLineWidth(1.5)
	strokeCircle(d, portalCenterX, portalCenterY, float32(portalRadius))

	// Orbiting warm light wisp (synced with the 3D scene's LightPhase).
	wispX := float32(portalCenterX + 40*math.Cos(m.WispPhase))
	wispY := float32(portalCenterY + 26*math.Sin(m.WispPhase))
	d.SetFillColor(core.RGBA(0xFB923C, 0.35))
	fillCircle(d, wispX, wispY, 8)
	d.SetFillColor(core.RGB(0xFDE68A))
	fillCircle(d, wispX, wispY, 3.2)
}

func (m *Map2D) drawPortalCue(d *core.Drawing) {
	if !m.NearPortal() {
		return
	}
	ring := float32(portalRadius + 3 + 2*math.Sin(m.Time*6.0))
	d.SetStrokeColor(core.RGBA(0xFDE68A, 0.95))
	d.SetLineWidth(2)
	strokeCircle(d, portalCenterX, portalCenterY, ring)
}

// drawPauseOverlay renders a translucent twilight veil over the paused 3D or 2D scene.
func drawPauseOverlay(d *core.Drawing, _ float64) {
	if d == nil {
		return
	}
	d.SetFillColor(core.RGBA(0x070B14, 0.56))
	d.FillRect(0, 0, mapWidth, mapHeight)
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// courtyardAtlas builds the deterministic procedural 64x16 sprite sheet for the 2D courtyard map.
func courtyardAtlas() *sprite.Atlas {
	mapAtlasOnce.Do(func() {
		img := image.NewNRGBA(image.Rect(0, 0, 64, 16))
		drawTravelerTile(img, 0)
		drawLanternTile(img, 16)
		drawCrateTile(img, 32)
		drawCrystalTile(img, 48)

		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			panic(err)
		}

		frame := func(name string, x float32) sprite.Frame {
			return sprite.Frame{
				Name:   name,
				X:      x,
				Y:      0,
				W:      16,
				H:      16,
				Width:  16,
				Height: 16,
				PivotX: 8,
				PivotY: 8,
			}
		}
		mapAtlasCached = &sprite.Atlas{
			Image: core.ImageSource{Data: buf.Bytes()},
			Frames: map[string]sprite.Frame{
				"traveler": frame("traveler", 0),
				"lantern":  frame("lantern", 16),
				"crate":    frame("crate", 32),
				"crystal":  frame("crystal", 48),
			},
		}
	})
	return mapAtlasCached
}

func fillTileRect(img *image.NRGBA, ox, x0, y0, w, h int, c color.NRGBA) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			img.SetNRGBA(ox+x, y, c)
		}
	}
}

func drawTravelerTile(img *image.NRGBA, ox int) {
	// Drop shadow
	fillTileRect(img, ox, 4, 13, 8, 2, color.NRGBA{0, 0, 0, 110})
	// Indigo cloak body
	fillTileRect(img, ox, 4, 6, 8, 7, color.NRGBA{56, 118, 210, 255})
	fillTileRect(img, ox, 5, 7, 6, 5, color.NRGBA{96, 165, 250, 255})
	// Hood & head
	fillTileRect(img, ox, 5, 2, 6, 5, color.NRGBA{30, 64, 138, 255})
	fillTileRect(img, ox, 6, 3, 4, 3, color.NRGBA{253, 230, 138, 255})
	// Handheld warm lantern on the right
	fillTileRect(img, ox, 12, 6, 2, 5, color.NRGBA{180, 115, 55, 255})
	fillTileRect(img, ox, 12, 4, 3, 3, color.NRGBA{251, 191, 36, 255})
}

func drawLanternTile(img *image.NRGBA, ox int) {
	// Stone base & cap
	fillTileRect(img, ox, 4, 11, 8, 4, color.NRGBA{100, 116, 139, 255})
	fillTileRect(img, ox, 5, 4, 6, 7, color.NRGBA{71, 85, 105, 255})
	fillTileRect(img, ox, 3, 3, 10, 2, color.NRGBA{148, 163, 184, 255})
	// Warm inner flame
	fillTileRect(img, ox, 6, 6, 4, 4, color.NRGBA{251, 191, 36, 255})
	fillTileRect(img, ox, 7, 7, 2, 2, color.NRGBA{254, 243, 199, 255})
}

func drawCrateTile(img *image.NRGBA, ox int) {
	// Cedar wood crate with dark framing bands
	fillTileRect(img, ox, 3, 3, 10, 10, color.NRGBA{120, 74, 36, 255})
	fillTileRect(img, ox, 4, 4, 8, 8, color.NRGBA{170, 112, 60, 255})
	fillTileRect(img, ox, 4, 7, 8, 2, color.NRGBA{120, 74, 36, 255})
	fillTileRect(img, ox, 7, 4, 2, 8, color.NRGBA{120, 74, 36, 255})
}

func drawCrystalTile(img *image.NRGBA, ox int) {
	// Azure crystal spires
	fillTileRect(img, ox, 6, 2, 4, 11, color.NRGBA{56, 189, 248, 255})
	fillTileRect(img, ox, 7, 3, 2, 8, color.NRGBA{186, 230, 253, 255})
	fillTileRect(img, ox, 3, 6, 3, 7, color.NRGBA{14, 165, 233, 255})
	fillTileRect(img, ox, 10, 7, 3, 6, color.NRGBA{2, 132, 199, 255})
}
