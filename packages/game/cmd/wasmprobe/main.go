// Command wasmprobe is a minimal entry point referencing the game packages
// (game, sprite, sound, tilemap) so the web size budget check can build and
// measure their combined GOOS=js GOARCH=wasm footprint.
package main

import (
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func main() {
	var d core.Drawing

	atlas, _ := sprite.LoadAtlas(core.ImageSource{URI: "asset:sheet.png"}, nil)
	if atlas != nil {
		s := sprite.NewSprite(sprite.Frame{W: 16, H: 16, Width: 16, Height: 16})
		b := &sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&s}}
		b.Draw(&d)
	}

	m, _ := tilemap.LoadTiled(nil, tilemap.TilesetLoader{})
	cam := &tilemap.Camera{Width: 320, Height: 180, Zoom: 1}
	if m != nil {
		m.Update(16 * time.Millisecond)
		cam.Apply(&d)
		m.Draw(&d, cam)
		_, _ = m.Raycast(core.Point{}, core.Point{X: 32, Y: 32}, tilemap.TileSolid)
	}

	var mixer *sound.Mixer
	if mixer != nil {
		_ = mixer.SetMaster(1)
	}
	_ = sound.NewMixer

	loop := &game.Loop{
		Rate:   60,
		Update: func(time.Duration) {},
		Draw:   func(float64) {},
	}
	_ = loop
}
