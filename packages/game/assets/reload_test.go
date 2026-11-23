package assets_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const heroAtlasJSONWithJump = `{
	"frames": {
		"idle.png": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}},
		"run.png":  {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}},
		"jump.png": {"frame": {"x": 32, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}}
	},
	"meta": {"scale": "1"}
}`

// loadReloadGroup starts an app whose manager has loaded a group with an atlas, an image and raw bytes from memory, and a canvas drawing from the manager's Revision.
func loadReloadGroup(t *testing.T) (*nsgotest.App, *assets.Manager, *int) {
	t.Helper()
	fsys := fstest.MapFS{
		"sprites/hero.json": {Data: []byte(heroAtlasJSON)},
		"fonts/pixel.fnt":   {Data: []byte("BMFont-v1")},
	}
	var (
		mgr   *assets.Manager
		draws int
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithFS(fsys))
		mgr.Group("level1").
			AddAtlas("hero", heroJSONURI, core.Src(heroPNGURI)).
			AddImage("bg", core.Src(bgPNGURI)).
			AddBytes("font", fontURI)
		mgr.LoadGroup("level1", func(err error) {
			if err != nil {
				t.Errorf("LoadGroup: %v", err)
			}
		})
		return core.Canvas{
			Style: core.Style{Width: core.Pt(32), Height: core.Pt(32)},
			Draw: func(*core.Drawing, core.Size) {
				_ = mgr.Revision()
				draws++
			},
		}
	}, envWithMedia()...)
	app.WaitUntil(time.Second, func() bool { return mgr.Group("level1").Done() })
	return app, mgr, &draws
}

func TestReloadReplacesAtlasFramesInPlaceAndRedraws(t *testing.T) {
	app, mgr, draws := loadReloadGroup(t)

	var (
		held   = mustAtlas(t, app, mgr)
		names  []string
		err    error
		before = *draws
	)
	app.Post(func() { names, err = mgr.Reload("asset:/sprites/./hero.json", []byte(heroAtlasJSONWithJump)) })
	app.Settle()
	if err != nil || !slices.Equal(names, []string{"hero"}) {
		t.Fatalf("Reload = %v, %v, want [hero], nil", names, err)
	}
	if _, ok := held.Frames["jump.png"]; !ok || len(held.Frames) != 3 {
		t.Fatalf("held atlas frames = %v, want the reloaded three", held.Frames)
	}
	if held.Image.URI != heroPNGURI {
		t.Fatalf("atlas image = %+v, want %s kept", held.Image, heroPNGURI)
	}
	if *draws <= before {
		t.Fatal("canvas reading Revision did not redraw")
	}
	var rev int
	app.Post(func() { rev = mgr.Revision() })
	app.Settle()
	if rev != 1 {
		t.Fatalf("Revision = %d, want 1", rev)
	}
}

func TestReloadKeepsAtlasWhenNewJSONIsBroken(t *testing.T) {
	app, mgr, _ := loadReloadGroup(t)
	held := mustAtlas(t, app, mgr)

	var (
		names []string
		err   error
		rev   int
	)
	app.Post(func() {
		names, err = mgr.Reload(heroJSONURI, []byte(`{"frames": [`))
		rev = mgr.Revision()
	})
	app.Settle()
	if err == nil || !strings.Contains(err.Error(), `atlas "hero"`) {
		t.Fatalf("Reload error = %v, want the atlas's parse error", err)
	}
	if len(names) != 0 || rev != 0 {
		t.Fatalf("Reload replaced %v (revision %d), want nothing", names, rev)
	}
	if len(held.Frames) != 2 {
		t.Fatalf("broken JSON changed the atlas: %v", held.Frames)
	}
}

func TestReloadAtlasImageImageAndBytes(t *testing.T) {
	app, mgr, _ := loadReloadGroup(t)
	held := mustAtlas(t, app, mgr)

	png := []byte("new hero pixels")
	var (
		atlasNames, bgNames, fontNames, none []string
		err                                  error
		bg                                   core.ImageSource
		font                                 []byte
		rev                                  int
	)
	app.Post(func() {
		atlasNames, err = mgr.Reload(heroPNGURI, png)
		if err != nil {
			return
		}
		bgNames, err = mgr.Reload(bgPNGURI, []byte("new bg"))
		if err != nil {
			return
		}
		fontNames, err = mgr.Reload(fontURI, []byte("BMFont-v2"))
		if err != nil {
			return
		}
		none, err = mgr.Reload("asset:sounds/click.wav", []byte("RIFF"))
		bg, _ = mgr.Image("bg")
		font, _ = mgr.Bytes("font")
		rev = mgr.Revision()
	})
	app.Settle()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(atlasNames, []string{"hero"}) || !slices.Equal(bgNames, []string{"bg"}) || !slices.Equal(fontNames, []string{"font"}) || len(none) != 0 {
		t.Fatalf("reloaded %v %v %v %v, want [hero] [bg] [font] []", atlasNames, bgNames, fontNames, none)
	}
	if string(held.Image.Data) != string(png) || held.Image.URI != heroPNGURI || len(held.Frames) != 2 {
		t.Fatalf("atlas after image reload = %+v", held)
	}
	if string(bg.Data) != "new bg" || bg.URI != bgPNGURI {
		t.Fatalf("bg = %+v, want the new bytes under %s", bg, bgPNGURI)
	}
	if string(font) != "BMFont-v2" {
		t.Fatalf("font = %q", font)
	}
	if rev != 3 {
		t.Fatalf("Revision = %d, want 3", rev)
	}

	// The atlas's JSON reloaded after its image keeps drawing the new image.
	app.Post(func() { _, err = mgr.Reload(heroJSONURI, []byte(heroAtlasJSONWithJump)) })
	app.Settle()
	if err != nil {
		t.Fatal(err)
	}
	if string(held.Image.Data) != string(png) || len(held.Frames) != 3 {
		t.Fatalf("atlas after JSON reload = %+v", held)
	}
}

func TestReloadSkipsReleasedGroups(t *testing.T) {
	app, mgr, _ := loadReloadGroup(t)
	var (
		names []string
		err   error
	)
	app.Post(func() {
		mgr.ReleaseGroup("level1")
		names, err = mgr.Reload(heroJSONURI, []byte(heroAtlasJSONWithJump))
	})
	app.Settle()
	if err != nil || len(names) != 0 {
		t.Fatalf("Reload after release = %v, %v, want nothing", names, err)
	}
}

// mustAtlas returns the hero atlas the manager holds, read on the engine goroutine.
func mustAtlas(t *testing.T, app *nsgotest.App, mgr *assets.Manager) *sprite.Atlas {
	t.Helper()
	var (
		atlas *sprite.Atlas
		ok    bool
	)
	app.Post(func() { atlas, ok = mgr.Atlas("hero") })
	app.Settle()
	if !ok || atlas == nil {
		t.Fatal("hero atlas not loaded")
	}
	return atlas
}
