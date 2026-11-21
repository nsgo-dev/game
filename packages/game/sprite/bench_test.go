package sprite_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func BenchmarkBatchDraw1000(b *testing.B) {
	frames := []sprite.Frame{
		{Name: "f0", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		{Name: "f1", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		{Name: "f2", X: 32, Y: 0, W: 12, H: 14, OffsetX: 2, OffsetY: 1, Width: 16, Height: 16},
		{Name: "f3", X: 44, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
	}
	atlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:bench-sheet.png"},
		Frames: map[string]sprite.Frame{
			"f0": frames[0],
			"f1": frames[1],
			"f2": frames[2],
			"f3": frames[3],
		},
	}

	sprites := make([]*sprite.Sprite, 1000)
	for i := range sprites {
		s := sprite.NewSprite(frames[i%len(frames)])
		s.X = float32((i % 40) * 16)
		s.Y = float32((i / 40) * 16)
		s.Z = (i * 7) % 16
		if i%5 == 0 {
			s.OriginX, s.OriginY = 8, 8
			s.Rotation = float32(i%8) * (math.Pi / 4)
		}
		if i%7 == 0 {
			s.FlipX = true
		}
		if i%11 == 0 {
			s.Alpha = 0.75
		}
		sprites[i] = &s
	}

	batch := &sprite.Batch{Atlas: atlas, Sprites: sprites}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 4096)}
	// Warm internal sort buffer once.
	batch.Draw(&d)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		d.Ops = d.Ops[:0]
		batch.Draw(&d)
	}
	if len(d.Ops) == 0 {
		b.Fatal("expected draw ops")
	}
}

func BenchmarkLoadAtlas(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var last *sprite.Atlas
	for range b.N {
		a, err := sprite.LoadAtlas(heroImage, asepriteHash)
		if err != nil {
			b.Fatal(err)
		}
		last = a
	}
	if len(last.Frames) == 0 {
		b.Fatal("expected loaded frames")
	}
}
