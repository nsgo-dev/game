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

func TestReloadSkipsFilesAPackGroupReadsFromItsOwnFS(t *testing.T) {
	const sharedURI = "asset:data/shared.txt"
	base := fstest.MapFS{
		"sprites/hero.json": {Data: []byte(heroAtlasJSON)},
		"fonts/pixel.fnt":   {Data: []byte("BMFont-v1")},
		"data/shared.txt":   {Data: []byte("shared v1")},
	}
	pack := fstest.MapFS{
		"sprites/hero.json": {Data: []byte(heroAtlasJSON)},
		"fonts/pixel.fnt":   {Data: []byte("pack font")},
	}
	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithFS(base))
		mgr.Group("base").
			AddAtlas("hero", heroJSONURI, core.Src(heroPNGURI)).
			AddBytes("font", fontURI)
		mgr.Group("dlc").SetFS(pack).
			AddAtlas("packHero", heroJSONURI, core.Src(heroPNGURI)).
			AddBytes("packFont", fontURI).
			AddBytes("shared", sharedURI)
		for _, name := range []string{"base", "dlc"} {
			mgr.LoadGroup(name, func(err error) {
				if err != nil {
					t.Errorf("LoadGroup(%s): %v", name, err)
				}
			})
		}
		return core.Canvas{Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)}}
	}, envWithMedia()...)
	app.WaitUntil(time.Second, func() bool { return mgr.Group("base").Done() && mgr.Group("dlc").Done() })

	var (
		atlasNames, fontNames, sharedNames []string
		err                                error
		packHero                           *sprite.Atlas
		packFont, shared                   []byte
	)
	app.Post(func() {
		atlasNames, err = mgr.Reload(heroJSONURI, []byte(heroAtlasJSONWithJump))
		if err != nil {
			return
		}
		fontNames, err = mgr.Reload(fontURI, []byte("BMFont-v2"))
		if err != nil {
			return
		}
		sharedNames, err = mgr.Reload(sharedURI, []byte("shared v2"))
		packHero, _ = mgr.Atlas("packHero")
		packFont, _ = mgr.Bytes("packFont")
		shared, _ = mgr.Bytes("shared")
	})
	app.Settle()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(atlasNames, []string{"hero"}) || !slices.Equal(fontNames, []string{"font"}) {
		t.Fatalf("reloaded %v and %v, want only the base group's [hero] and [font]", atlasNames, fontNames)
	}
	if len(packHero.Frames) != 2 || string(packFont) != "pack font" {
		t.Fatalf("the pack's own files changed: %d frames, font %q", len(packHero.Frames), packFont)
	}
	// The pack has no data/shared.txt, so its group read the base file and shows the edit.
	if !slices.Equal(sharedNames, []string{"shared"}) || string(shared) != "shared v2" {
		t.Fatalf("shared = %v %q, want [shared] %q", sharedNames, shared, "shared v2")
	}
}

func TestReloadFollowsTheDensityTheScreenShows(t *testing.T) {
	const (
		heroImage   = "asset:images/hero.png"
		heroImage2x = "asset:images/hero@2x.png"
		sheet2x     = "asset:sprites/hero@2x.png"
	)
	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithFS(fstest.MapFS{"sprites/hero.json": {Data: []byte(heroAtlasJSON)}}))
		// Inline bytes keep the test off image prefetching; Reload matches by URI either way.
		mgr.Group("ui").
			AddImage("hero", core.ImageSource{URI: heroImage, Scales: core.ScalesOf(1, 2), Data: []byte("embedded")}).
			AddAtlas("sheet", heroJSONURI, core.ImageSource{URI: heroPNGURI, Scales: core.ScalesOf(1, 2), Data: []byte("embedded sheet")})
		mgr.LoadGroup("ui", func(err error) {
			if err != nil {
				t.Errorf("LoadGroup: %v", err)
			}
		})
		return core.Canvas{Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)}}
	}, nsgotest.Environment(core.Environment{PixelRatio: 2}))
	app.WaitUntil(time.Second, func() bool { return mgr.Group("ui").Done() })

	reload := func(uri, data string) ([]string, core.ImageSource, *sprite.Atlas) {
		t.Helper()
		var (
			names []string
			err   error
			img   core.ImageSource
			sheet *sprite.Atlas
		)
		app.Post(func() {
			names, err = mgr.Reload(uri, []byte(data))
			img, _ = mgr.Image("hero")
			sheet, _ = mgr.Atlas("sheet")
		})
		app.Settle()
		if err != nil {
			t.Fatal(err)
		}
		return names, img, sheet
	}

	// A 2x screen does not show the 1x file: saving it changes nothing.
	names, img, sheet := reload(heroImage, "1x")
	if len(names) != 0 || string(img.Data) != "embedded" || img.Scales != core.ScalesOf(1, 2) {
		t.Fatalf("saving the 1x file replaced %v: %+v", names, img)
	}
	for i, data := range []string{"2x v1", "2x v2"} {
		names, img, _ = reload(heroImage2x, data)
		if !slices.Equal(names, []string{"hero"}) || string(img.Data) != data || img.URI != heroImage2x || img.Density() != 2 {
			t.Fatalf("save %d of the 2x file: %v, %+v (density %v)", i+1, names, img, img.Density())
		}
	}
	names, _, sheet = reload(sheet2x, "sheet 2x")
	if !slices.Equal(names, []string{"sheet"}) || string(sheet.Image.Data) != "sheet 2x" || sheet.Image.Density() != 2 || len(sheet.Frames) != 2 {
		t.Fatalf("saving the atlas's 2x image: %v, %+v", names, sheet.Image)
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
