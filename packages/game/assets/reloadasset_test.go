package assets_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestReloadAssetSkipsDensitiesTheScreenDoesNotShowAndImagesWithoutFiles(t *testing.T) {
	const (
		heroImage   = "asset:images/hero.png"
		heroImage2x = "asset:images/hero@2x.png"
	)
	fsys := fstest.MapFS{
		"images/hero.png":    {Data: []byte("1x on disk")},
		"images/hero@2x.png": {Data: []byte("2x on disk")},
	}
	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithFS(fsys))
		mgr.Group("ui").
			AddImage("hero", core.ImageSource{URI: heroImage, Scales: core.ScalesOf(1, 2), Data: []byte("embedded")}).
			AddImage("generated", core.ImageSource{Data: []byte("generated")})
		mgr.LoadGroup("ui", func(err error) {
			if err != nil {
				t.Errorf("LoadGroup: %v", err)
			}
		})
		return core.Canvas{Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)}}
	}, nsgotest.Environment(core.Environment{PixelRatio: 2}))
	app.WaitUntil(time.Second, func() bool { return mgr.Group("ui").Done() })

	reload := func(key string) ([]assets.ReloadedAsset, error, core.ImageSource, int) {
		t.Helper()
		var (
			got []assets.ReloadedAsset
			err error
			img core.ImageSource
			rev int
		)
		app.Post(func() {
			got, err = mgr.ReloadAsset(context.Background(), "", key)
			img, _ = mgr.Image("hero")
			rev = mgr.Revision()
		})
		app.Settle()
		return got, err, img, rev
	}

	// A 2x screen does not show the 1x file: reloading it leaves the multi-scale source alone.
	got, err, img, rev := reload("images/hero.png")
	if err != nil || len(got) != 1 || got[0].Skipped != assets.SkippedDensity || got[0].Err != nil {
		t.Fatalf("reload 1x = (%+v, %v), want skipped for density", got, err)
	}
	if string(img.Data) != "embedded" || img.Scales != core.ScalesOf(1, 2) || rev != 0 {
		t.Fatalf("reloading the 1x file changed the image: %+v (revision %d)", img, rev)
	}

	got, err, img, rev = reload("images/hero@2x.png")
	if err != nil || len(got) != 1 || got[0].Skipped != "" || got[0].Err != nil {
		t.Fatalf("reload 2x = (%+v, %v)", got, err)
	}
	if string(img.Data) != "2x on disk" || img.URI != heroImage2x || img.Density() != 2 || rev != 1 {
		t.Fatalf("reloading the 2x file = %+v (density %v, revision %d)", img, img.Density(), rev)
	}

	// A group reload skips the generated image instead of failing the group.
	var groupRes []assets.ReloadedAsset
	app.Post(func() { groupRes, err = mgr.ReloadGroup(context.Background(), "ui") })
	app.Settle()
	if err != nil || len(groupRes) != 2 {
		t.Fatalf("ReloadGroup(ui) = (%+v, %v)", groupRes, err)
	}
	if groupRes[1].Name != "generated" || groupRes[1].Skipped != assets.SkippedNoFile || groupRes[1].Err != nil {
		t.Fatalf("generated image = %+v, want skipped with no file", groupRes[1])
	}
}

func TestReloadAssetRunsCustomLoadersOffTheEngineGoroutine(t *testing.T) {
	fsys := fstest.MapFS{"data/table.txt": {Data: []byte("v1")}}
	var (
		mu       sync.Mutex
		calls    int
		released []string
	)
	gate := make(chan struct{})
	load := func(_ context.Context, data []byte) (any, error) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if !first {
			// A loader that ignores ctx: ReloadAsset must give up on ctx without waiting for it on the engine goroutine.
			<-gate
		}
		return string(data), nil
	}
	release := func(v any) {
		mu.Lock()
		released = append(released, v.(string))
		mu.Unlock()
	}

	var mgr *assets.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithFS(fsys))
		mgr.Group("data").AddCustomLoader("table", "asset:data/table.txt", load, release)
		mgr.LoadGroup("data", func(err error) {
			if err != nil {
				t.Errorf("LoadGroup: %v", err)
			}
		})
		return core.View{}
	})
	app.WaitUntil(time.Second, func() bool { return mgr.Group("data").Done() })

	fsys["data/table.txt"] = &fstest.MapFile{Data: []byte("v2")}
	var (
		err error
		cur any
	)
	app.Post(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err = mgr.ReloadAsset(ctx, "", "table")
		cur, _ = mgr.Get("table")
	})
	app.Settle()
	if !errors.Is(err, context.DeadlineExceeded) || cur != "v1" {
		t.Fatalf("ReloadAsset with a stuck loader = %v, table %v; want deadline exceeded and v1 kept", err, cur)
	}

	// Once the loader finishes, the value nobody applied is released.
	close(gate)
	app.WaitUntil(time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Equal(released, []string{"v2"})
	})
	app.Post(func() { cur, _ = mgr.Get("table") })
	app.Settle()
	if cur != "v1" {
		t.Fatalf("table after abandoned reload = %v, want v1", cur)
	}
}
