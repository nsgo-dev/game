package main

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// starLayers are the starfield's layers, far to near: how many stars each has, how fast it scrolls against the stage's scroll speed, and how big and bright its stars are.
var starLayers = []struct {
	count int
	speed float32
	size  float32
	alpha float32
}{
	{40, 0.25, 1, 0.35},
	{24, 0.6, 1.5, 0.6},
	{12, 1.2, 2, 0.9},
}

// draw records the playfield into d, which is in playfield points, with the ship, enemies and boss placed alpha of the way from the last step to this one.
func (w *world) draw(d *core.Drawing, alpha float32) {
	d.SetFillColor(core.RGB(0x070b1a)).FillRect(0, 0, w.width, w.height)
	w.drawStars(d)

	w.sprites = w.sprites[:0]
	for _, it := range w.items {
		it.sprite.X, it.sprite.Y = it.x, it.y
		// Items pulse so they stand out from the bullets.
		it.sprite.Alpha = 0.75 + 0.25*float32(math.Sin(w.elapsed.Seconds()*10))
		w.sprites = append(w.sprites, &it.sprite)
	}
	for _, e := range w.enemies {
		place(&e.sprite, e.prevX, e.prevY, e.x, e.y, alpha)
		e.sprite.Effect = hitEffect(e.flash > 0)
		w.sprites = append(w.sprites, &e.sprite)
	}
	if b := w.boss; b != nil && !b.dead {
		place(&b.sprite, b.prevX, b.prevY, b.x, b.y, alpha)
		b.sprite.Effect = hitEffect(b.flash > 0)
		w.sprites = append(w.sprites, &b.sprite)
	}
	if s := &w.ship; s.alive {
		place(&s.sprite, s.prevX, s.prevY, s.x, s.y, alpha)
		s.sprite.Alpha = 1
		// The ship blinks while it cannot be hit.
		if s.invulnerable > 0 && (s.invulnerable/(80*time.Millisecond))%2 == 0 {
			s.sprite.Alpha = 0.35
		}
		w.sprites = append(w.sprites, &s.sprite)
	}
	w.batch.Sprites = w.sprites
	w.batch.Draw(d)

	w.shipShots.Draw(d)
	w.explosions.Draw(d)
	w.enemyShots.Draw(d)
	if s := &w.ship; s.alive {
		// The hit box, which is all that counts, drawn over the ship.
		d.SetFillColor(core.RGB(0xffffff)).BeginPath().Arc(s.sprite.X, s.sprite.Y, w.stage.Ship.Radius, 0, 2*math.Pi, false).Fill()
	}
	if w.bombFlash > 0 {
		d.SetFillColor(core.RGBA(0xffffff, float32(w.bombFlash.Seconds())*1.5)).FillRect(0, 0, w.width, w.height)
	}
}

// drawStars draws the starfield scrolling down past the ship, each star placed by a hash of its index so the field needs no state.
func (w *world) drawStars(d *core.Drawing) {
	for li, layer := range starLayers {
		d.SetFillColor(core.RGBA(0xcbd5e1, layer.alpha))
		for i := range layer.count {
			h := hash(uint32(li*1000 + i))
			x := float32(h%1000) / 1000 * w.width
			y := float32((h/1000)%1000)/1000*w.height + w.scroll*layer.speed
			y = float32(math.Mod(float64(y), float64(w.height)))
			d.FillRect(x, y, layer.size, layer.size)
		}
	}
}

// place puts s alpha of the way from (px, py) to (x, y).
func place(s *sprite.Sprite, px, py, x, y, alpha float32) {
	s.X, s.Y = lerpf(px, x, alpha), lerpf(py, y, alpha)
}

// hitFlash turns a sprite that was just hit halfway to white for a moment.
var hitFlash = sprite.Effect{Flash: sprite.Flash{Color: core.RGB(0xffffff), Strength: 0.5}}

// hitEffect is the effect of a sprite that was just hit, hitFlash, or of one that was not, none, which keeps it on the batch's fast path.
func hitEffect(hit bool) *sprite.Effect {
	if hit {
		return &hitFlash
	}
	return nil
}

// hash is a small integer hash, which places the stars.
func hash(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}
