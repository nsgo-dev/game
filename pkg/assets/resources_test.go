package assets_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestManagerLoadsGroupFromAppResourcesWithoutWithFS verifies that a Manager
// created without WithFS or WithReadAsset reads "asset:" URIs from the app's
// resources through core.Scope.ReadAsset — the same resources/ directory images
// and sounds resolve — both headless in nsgotest and in a real browser via
// `go tool nsgo test -target web ./pkg/assets` with nothing embedded.
func TestManagerLoadsGroupFromAppResourcesWithoutWithFS(t *testing.T) {
	var (
		mgr     *assets.Manager
		group   *assets.Group
		doneErr error
		done    atomic.Bool
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx)
		group = mgr.Group("stage").
			AddAtlas("hero", "asset:sprites/hero.json", core.Src("asset:sprites/hero.png")).
			AddBytes("config", "asset:data/config.txt").
			AddCustom("level", "asset:maps/level1.txt", func(data []byte) (any, error) {
				return &fakeMap{Name: strings.TrimSpace(string(data))}, nil
			}, nil)

		mgr.LoadGroup("stage", func(err error) {
			doneErr = err
			done.Store(true)
		})
		return core.View{}
	}, appResourcesTestOptions(t)...)

	app.WaitUntil(5*time.Second, func() bool { return done.Load() })
	if doneErr != nil {
		t.Fatalf("LoadGroup(stage) failed: %v", doneErr)
	}
	if !group.Done() || group.Progress() != 1 || group.Loaded() != 3 || group.Total() != 3 {
		t.Fatalf("group state: done=%v progress=%v loaded=%d total=%d err=%v",
			group.Done(), group.Progress(), group.Loaded(), group.Total(), group.Err())
	}

	atlas, ok := mgr.Atlas("hero")
	if !ok || atlas == nil {
		t.Fatal("mgr.Atlas(hero) missing")
	}
	if len(atlas.Frames) != 2 || atlas.Frames["run.png"].X != 16 {
		t.Fatalf("unexpected atlas frames: %+v", atlas.Frames)
	}
	if atlas.Image.URI != "asset:sprites/hero.png" || len(atlas.Image.Data) != 0 {
		t.Fatalf("atlas image = %+v, want URI-backed resource without embedded Data", atlas.Image)
	}

	cfg, ok := mgr.Bytes("config")
	if !ok || strings.TrimSpace(string(cfg)) != "hello-app-resources" {
		t.Fatalf("mgr.Bytes(config) = %q, ok=%v", string(cfg), ok)
	}

	rawLevel, ok := mgr.Get("level")
	if !ok {
		t.Fatal("mgr.Get(level) missing")
	}
	if fm, ok := rawLevel.(*fakeMap); !ok || fm.Name != "cavern-stage-1" {
		t.Fatalf("mgr.Get(level) = %#v, want fakeMap{Name: cavern-stage-1}", rawLevel)
	}
}

func TestManagerStandaloneLoadersFromAppResources(t *testing.T) {
	var (
		mgr       *assets.Manager
		doneCount atomic.Int32
		gotBytes  string
		gotAtlas  *sprite.Atlas
		gotCustom string
		gotErr    error
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx)
		mgr.LoadBytes("asset:data/config.txt", func(b []byte, err error) {
			if err != nil {
				t.Errorf("LoadBytes: %v", err)
			}
			gotBytes = strings.TrimSpace(string(b))
			doneCount.Add(1)
		})
		mgr.LoadAtlas("asset:sprites/hero.json", core.Src("asset:sprites/hero.png"), func(a *sprite.Atlas, err error) {
			if err != nil {
				t.Errorf("LoadAtlas: %v", err)
			}
			gotAtlas = a
			doneCount.Add(1)
		})
		mgr.LoadCustom("asset:maps/level1.txt", func(_ context.Context, data []byte) (any, error) {
			return strings.TrimSpace(string(data)), nil
		}, nil, func(v any, err error) {
			if err != nil {
				t.Errorf("LoadCustom: %v", err)
			}
			if s, ok := v.(string); ok {
				gotCustom = s
			}
			doneCount.Add(1)
		})
		mgr.LoadBytes("asset:does-not-exist.txt", func(_ []byte, err error) {
			gotErr = err
			doneCount.Add(1)
		})
		return core.View{}
	}, appResourcesTestOptions(t)...)

	app.WaitUntil(5*time.Second, func() bool { return doneCount.Load() == 4 })
	if gotBytes != "hello-app-resources" {
		t.Fatalf("LoadBytes = %q, want hello-app-resources", gotBytes)
	}
	if gotAtlas == nil || len(gotAtlas.Frames) != 2 {
		t.Fatalf("LoadAtlas = %+v, want 2 frames", gotAtlas)
	}
	if gotCustom != "cavern-stage-1" {
		t.Fatalf("LoadCustom = %q, want cavern-stage-1", gotCustom)
	}
	if !errors.Is(gotErr, core.ErrAssetNotFound) {
		t.Fatalf("missing resource error = %v, want core.ErrAssetNotFound", gotErr)
	}
}

func TestManagerAppResourcesRejectsTraversalAndAllowsOverrides(t *testing.T) {
	var (
		defaultErr   error
		defaultDone  atomic.Bool
		overrideData string
		overrideDone atomic.Bool
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		defaultMgr := assets.NewManager(cx)
		defaultMgr.LoadBytes("asset:../escape.txt", func(_ []byte, err error) {
			defaultErr = err
			defaultDone.Store(true)
		})

		fsMgr := assets.NewManager(cx, assets.WithFS(fstest.MapFS{
			"data/config.txt": &fstest.MapFile{Data: []byte("from-fs-override")},
		}))
		fsMgr.LoadBytes("asset:data/config.txt", func(b []byte, err error) {
			if err != nil {
				t.Errorf("WithFS LoadBytes: %v", err)
			}
			overrideData = string(b)
			overrideDone.Store(true)
		})
		return core.View{}
	}, appResourcesTestOptions(t)...)

	app.WaitUntil(5*time.Second, func() bool { return defaultDone.Load() && overrideDone.Load() })
	if defaultErr == nil {
		t.Fatal("expected error for path traversal asset:../escape.txt")
	}
	if overrideData != "from-fs-override" {
		t.Fatalf("WithFS override = %q, want from-fs-override", overrideData)
	}
}

func TestReloadAssetWithoutFSReturnsErrorInsteadOfDeadlocking(t *testing.T) {
	var (
		mgr     *assets.Manager
		group   *assets.Group
		loadErr error
		loaded  atomic.Bool
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx)
		group = mgr.Group("stage").AddBytes("config", "asset:data/config.txt")
		mgr.LoadGroup("stage", func(err error) {
			loadErr = err
			loaded.Store(true)
		})
		return core.View{}
	}, appResourcesTestOptions(t)...)

	app.WaitUntil(5*time.Second, func() bool { return loaded.Load() })
	if loadErr != nil || !group.Done() {
		t.Fatalf("LoadGroup failed: %v", loadErr)
	}

	var reloadErr error
	app.Post(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, reloadErr = mgr.ReloadAsset(ctx, "stage", "config")
	})
	app.Settle()
	if reloadErr == nil || !strings.Contains(reloadErr.Error(), "WithFS") {
		t.Fatalf("ReloadAsset without WithFS = %v, want error explaining WithFS/WithReadAsset", reloadErr)
	}
}
