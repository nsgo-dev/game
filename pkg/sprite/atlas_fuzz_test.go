package sprite_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func FuzzLoadAtlas(f *testing.F) {
	for _, name := range []string{
		"aseprite-hash.json",
		"aseprite-array.json",
		"texturepacker-hash.json",
	} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatalf("read seed %s: %v", name, err)
		}
		f.Add(data)
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"frames":{}}`))
	f.Add([]byte(`{"frames":[]}`))

	img := core.ImageSource{URI: "asset:fuzz.png"}
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := sprite.LoadAtlas(img, data)
		if err != nil || a == nil {
			return
		}

		var sprites []*sprite.Sprite
		for _, frame := range a.Frames {
			s := sprite.NewSprite(frame)
			s.X, s.Y = 10, 20
			sprites = append(sprites, &s)
			if len(sprites) >= 64 {
				break
			}
		}
		if len(sprites) > 0 {
			sprites[0].FlipX = true
			sprites[0].Alpha = 0.5
		}

		batch := &sprite.Batch{Atlas: a, Sprites: sprites}
		var d core.Drawing
		batch.Draw(&d)

		checkedAnims := 0
		for _, anim := range a.Animations {
			anim.Update(16 * time.Millisecond)
			_ = anim.Frame()
			anim.Update(500 * time.Millisecond)
			anim.Reset()
			checkedAnims++
			if checkedAnims >= 16 {
				break
			}
		}
	})
}
