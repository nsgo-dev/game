package gamecli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func TestPixelfxWritesASheetSpriteLoads(t *testing.T) {
	dir := t.TempDir()
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"pixelfx", "-out", dir, "-set", "seed=9", "-set", "palette=#220000,#ff0000,#ffff00,#ffffff", "-flip", "fire/torch"}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "60 frames of 72x120 at 30 fps") {
		t.Fatalf("stdout %q", stdout)
	}
	img, err := os.ReadFile(filepath.Join(dir, "torch.png"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile(filepath.Join(dir, "torch.json"))
	if err != nil {
		t.Fatal(err)
	}
	atlas, err := sprite.LoadAtlas(core.ImageSource{Data: img}, js)
	if err != nil {
		t.Fatal(err)
	}
	if anim := atlas.Animations["torch"]; !anim.Loop || len(anim.Frames) != 60 {
		t.Fatalf("torch animation: loop %v, %d frames", anim.Loop, len(anim.Frames))
	}
}

func TestPixelfxActsOutAMove(t *testing.T) {
	dir := t.TempDir()
	a, _, stderr := newTestApp(t)
	if code := run([]string{"pixelfx", "-out", dir, "-act", "-fps", "10", "-name", "pound", "moves/ground-pound"}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	js, err := os.ReadFile(filepath.Join(dir, "pound.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Meta struct {
			Pixelfx struct {
				ID    string          `json:"id"`
				Acted bool            `json:"acted"`
				Act   json.RawMessage `json:"act"`
			} `json:"pixelfx"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(js, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Meta.Pixelfx.ID != "moves/ground-pound" || !doc.Meta.Pixelfx.Acted || len(doc.Meta.Pixelfx.Act) == 0 {
		t.Fatalf("meta %+v", doc.Meta.Pixelfx)
	}
}

func TestPixelfxListsEffectsAndOptions(t *testing.T) {
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"pixelfx", "-list"}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"fire/torch", "-set wood=#0e080a,", "moves/ground-pound", "acted", "explosions/small-explosion", "-set stone=#4a3f69"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("-list does not show %q:\n%s", want, stdout)
		}
	}
}

func TestPixelfxRefusesWhatItCannotRender(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"pixelfx"}, "expected one effect"},
		{[]string{"pixelfx", "fire/nope"}, `no effect "fire/nope"`},
		{[]string{"pixelfx", "-set", "speed=3", "fire/torch"}, `has no option "speed"; it has iron, palette, seed, wood`},
		{[]string{"pixelfx", "-set", "seed=fast", "fire/torch"}, "is not a number"},
		{[]string{"pixelfx", "-set", "seed", "fire/torch"}, "is not name=value"},
		{[]string{"pixelfx", "-fps", "-2", "fire/torch"}, "-fps must not be negative"},
		{[]string{"pixelfx", "-set", "seed=4abc", "fire/torch"}, `"4abc" is not a number`},
		{[]string{"pixelfx", "-set", "seed=1 2", "fire/torch"}, `"1 2" is not a number`},
		{[]string{"pixelfx", "-set", "palette=fff", "fire/torch"}, `color "fff" is not`},
		{[]string{"pixelfx", "-name", "../x", "fire/torch"}, "must be a file name"},
		{[]string{"pixelfx", "-name", "a/b", "fire/torch"}, "must be a file name"},
		{[]string{"pixelfx", "-name", "..", "fire/torch"}, "must be a file name"},
	} {
		a, _, stderr := newTestApp(t)
		if code := run(c.args, a); code != 2 || !strings.Contains(stderr.String(), c.want) {
			t.Errorf("%v: exit %d, stderr %q, want 2 and %q", c.args, code, stderr, c.want)
		}
	}
}
