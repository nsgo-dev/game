package assets_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestGroupStatsAndReloadAsset(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("sprites/hero.json", heroAtlasJSON)
	mustWrite("fonts/pixel.fnt", "BMFont-v1")

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithFS(os.DirFS(dir)))
		mgr.Group("level1").
			AddAtlas("hero", heroJSONURI, core.Src(heroPNGURI)).
			AddBytes("font", fontURI)
		mgr.Group("level2").
			AddBytes("font2", fontURI)
		mgr.LoadGroup("level1", func(err error) {
			if err != nil {
				t.Errorf("LoadGroup: %v", err)
			}
		})
		return core.View{}
	}, envWithMedia()...)
	app.WaitUntil(time.Second, func() bool { return mgr.Group("level1").Done() })

	var (
		names []string
		stats []assets.GroupStats
		found bool
	)
	app.Post(func() {
		names = mgr.GroupNames()
		stats = mgr.GroupStats()
		_, found = mgr.LookupGroup("does_not_exist")
	})
	app.Settle()

	if found {
		t.Fatal("LookupGroup should not find or create missing group")
	}
	if !slices.Equal(names, []string{"level1", "level2"}) {
		t.Fatalf("GroupNames = %v, want [level1 level2]", names)
	}
	if len(stats) != 2 {
		t.Fatalf("len(GroupStats) = %d, want 2", len(stats))
	}
	if stats[0].Name != "level1" || stats[0].State != assets.StateLoaded || stats[0].Loaded != 2 || stats[0].Bytes <= 0 {
		t.Fatalf("stats[0] = %+v", stats[0])
	}
	if stats[1].Name != "level2" || stats[1].State != assets.StateUnloaded || stats[1].Bytes != 0 {
		t.Fatalf("stats[1] = %+v", stats[1])
	}

	// Update hero.json on disk and reload by name.
	held := mustAtlas(t, app, mgr)
	mustWrite("sprites/hero.json", heroAtlasJSONWithJump)
	var (
		reloaded []assets.ReloadedAsset
		err      error
	)
	app.Post(func() {
		reloaded, err = mgr.ReloadAsset(context.Background(), "", "hero")
	})
	app.Settle()
	if err != nil || len(reloaded) != 1 || reloaded[0].Name != "hero" || reloaded[0].Frames != 3 {
		t.Fatalf("ReloadAsset(hero) = (%+v, %v)", reloaded, err)
	}
	if _, ok := held.Frames["jump.png"]; !ok {
		t.Fatalf("held atlas missing jump.png after ReloadAsset: %+v", held.Frames)
	}

	// Update font and reload entire group.
	mustWrite("fonts/pixel.fnt", "BMFont-v2-longer")
	app.Post(func() {
		reloaded, err = mgr.ReloadGroup(context.Background(), "level1")
	})
	app.Settle()
	if err != nil || len(reloaded) != 2 {
		t.Fatalf("ReloadGroup(level1) = (%+v, %v)", reloaded, err)
	}
	var gotFont []byte
	app.Post(func() { gotFont, _ = mgr.Bytes("font") })
	app.Settle()
	if string(gotFont) != "BMFont-v2-longer" {
		t.Fatalf("font after ReloadGroup = %q", gotFont)
	}

	// ReloadGroup on unloaded group returns error.
	app.Post(func() { _, err = mgr.ReloadGroup(context.Background(), "level2") })
	app.Settle()
	if err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("ReloadGroup(level2) = %v, want not loaded", err)
	}
}
