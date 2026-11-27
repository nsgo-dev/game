package pixelfx_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/explosions"
	"github.com/nsgo-dev/game/pkg/pixelfx/fire"
	"github.com/nsgo-dev/game/pkg/pixelfx/moves"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func loadSheet(t *testing.T, fr *pixelfx.Frames, name string) (*sprite.Atlas, map[string]any) {
	t.Helper()
	img, js, err := fr.SheetPNG(name, name+".png")
	if err != nil {
		t.Fatal(err)
	}
	atlas, err := sprite.LoadAtlas(core.ImageSource{Data: img}, js)
	if err != nil {
		t.Fatalf("sprite.LoadAtlas: %v\n%s", err, js)
	}
	var doc map[string]any
	if err := json.Unmarshal(js, &doc); err != nil {
		t.Fatal(err)
	}
	return atlas, doc["meta"].(map[string]any)["pixelfx"].(map[string]any)
}

func TestSheetLoadsAndLoopsALoop(t *testing.T) {
	fr := pixelfx.Render(fire.Torch, pixelfx.Options{}, 0)
	if len(fr.Pixels) != 60 {
		t.Fatalf("a 2 s loop at 30 fps has %d frames, want 60", len(fr.Pixels))
	}
	atlas, meta := loadSheet(t, fr, "torch")
	anim, ok := atlas.Animations["torch"]
	if !ok || !anim.Loop || len(anim.Frames) != 60 || anim.Frames[0].Duration != 33*time.Millisecond {
		t.Fatalf("animation loop %v, %d frames, first %v", anim.Loop, len(anim.Frames), anim.Frames)
	}
	f0 := anim.Frames[0].Frame
	if f0.W != 72 || f0.H != 120 || f0.PivotX != 36 || f0.PivotY != 94 {
		t.Fatalf("frame %+v, want 72x120 with its pivot at the origin (36, 94)", f0)
	}
	if meta["id"] != "fire/torch" || meta["loop"] != true || meta["fps"].(float64) != 30 {
		t.Fatalf("pixelfx meta %v", meta)
	}
	// play it: a loop starts over after its last frame
	anim.Update(61 * 33 * time.Millisecond)
	if anim.Done() {
		t.Fatal("the loop finished")
	}
}

func TestSheetPlaysAOneShotOnce(t *testing.T) {
	fr := pixelfx.Render(explosions.SmallExplosion, pixelfx.Options{}, 10)
	if len(fr.Pixels) != 55 {
		t.Fatalf("a 5.4 s one-shot at 10 fps has %d frames, want 55 (its end included)", len(fr.Pixels))
	}
	atlas, _ := loadSheet(t, fr, "boom")
	anim := atlas.Animations["boom"]
	if anim.Loop {
		t.Fatal("a one-shot loops")
	}
	anim.Update(10 * time.Second)
	if !anim.Done() {
		t.Fatal("a one-shot does not finish")
	}
}

func TestActSheetCarriesItsTiming(t *testing.T) {
	fr := pixelfx.RenderAct(moves.GroundPound, pixelfx.Options{}, 12)
	atlas, meta := loadSheet(t, fr, "pound")
	if len(atlas.Animations["pound"].Frames) != len(fr.Pixels) || atlas.Animations["pound"].Loop {
		t.Fatal("the acted sheet's animation does not play its frames once")
	}
	act := meta["act"].(map[string]any)
	if act["windup"].(float64) != 0.5 || len(act["hits"].([]any)) != 3 || len(act["poses"].([]any)) != 12 {
		t.Fatalf("act timing %v", act)
	}
	if meta["acted"] != true {
		t.Fatal("the meta does not say the frames are acted out")
	}
}

func TestSheetRefusesMalformedFrames(t *testing.T) {
	ok := func() pixelfx.Frames {
		return pixelfx.Frames{Effect: fire.Torch, W: 2, H: 2, FPS: 10, Pixels: [][]byte{make([]byte, 16)}}
	}
	for _, c := range []struct {
		name string
		edit func(*pixelfx.Frames)
	}{
		{"no frames", func(f *pixelfx.Frames) { f.Pixels = nil }},
		{"no effect", func(f *pixelfx.Frames) { f.Effect = nil }},
		{"zero fps", func(f *pixelfx.Frames) { f.FPS = 0 }},
		{"negative fps", func(f *pixelfx.Frames) { f.FPS = -1 }},
		{"short frame", func(f *pixelfx.Frames) { f.Pixels = append(f.Pixels, make([]byte, 15)) }},
		{"long frame", func(f *pixelfx.Frames) { f.Pixels[0] = make([]byte, 17) }},
	} {
		fr := ok()
		c.edit(&fr)
		if _, _, err := fr.Sheet("x", "x.png"); err == nil {
			t.Errorf("%s: Sheet succeeded", c.name)
		}
	}
	fr := ok()
	if _, _, err := fr.Sheet("x", "x.png"); err != nil {
		t.Errorf("well-formed frames: %v", err)
	}
}
