package assets_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const (
	heroJSONURI = "asset:sprites/hero.json"
	heroPNGURI  = "asset:sprites/hero.png"
	bgPNGURI    = "asset:images/bg.png"
	clickURI    = "asset:sounds/click.wav"
	jumpURI     = "asset:sounds/jump.wav"
	mapURI      = "asset:maps/level1.json"
	fontURI     = "asset:fonts/pixel.fnt"
)

const heroAtlasJSON = `{
	"frames": {
		"idle.png": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}},
		"run.png":  {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}}
	},
	"meta": {"scale": "1"}
}`

type fakeMap struct {
	Name string
}

var playsAudio = core.Environment{Audio: true}

func envWithMedia(opts ...nsgotest.Option) []nsgotest.Option {
	base := []nsgotest.Option{
		nsgotest.Environment(playsAudio),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{
				clickURI: 100 * time.Millisecond,
				jumpURI:  150 * time.Millisecond,
			}
			a.ImageSizes = map[string]core.Size{
				heroPNGURI: {Width: 32, Height: 16},
				bgPNGURI:   {Width: 320, Height: 180},
			}
		},
	}
	return append(base, opts...)
}

func TestAsyncGroupLoadsMixedAssetsWithoutBlockingFrames(t *testing.T) {
	atlasGate := make(chan struct{})
	mapGate := make(chan struct{})
	fontGate := make(chan struct{})

	var (
		mgr        *assets.Manager
		group      *assets.Group
		loopTicks  int
		loopDraws  int
		progresses []float64
		doneCh     = make(chan error, 1)
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l := &game.Loop{
			Update: func(time.Duration) { loopTicks++ },
			Draw:   func(float64) { loopDraws++ },
		}
		l.Run(cx)

		mgr = assets.NewManager(cx, assets.WithReadAsset(func(ctx context.Context, uri string) ([]byte, error) {
			switch uri {
			case heroJSONURI:
				select {
				case <-atlasGate:
					return []byte(heroAtlasJSON), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			case mapURI:
				select {
				case <-mapGate:
					return []byte("level-1-map"), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			case fontURI:
				select {
				case <-fontGate:
					return []byte("BMFont-data"), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			default:
				return nil, fmt.Errorf("unexpected uri %q", uri)
			}
		}))

		group = mgr.Group("level1").
			AddAtlas("hero", heroJSONURI, core.Src(heroPNGURI)).
			AddSound("click", audio.Source{URI: clickURI}).
			AddImage("bg", core.Src(bgPNGURI)).
			AddBytes("font", fontURI).
			AddCustom("map", mapURI, func(b []byte) (any, error) {
				return &fakeMap{Name: string(b)}, nil
			}, nil)

		cx.Effect(func() {
			progresses = append(progresses, group.Progress())
		})

		mgr.LoadGroup("level1", func(err error) {
			doneCh <- err
		})
		return core.View{}
	}, envWithMedia()...)

	// While the three file-backed assets are blocked on their gates, the game
	// loop keeps ticking every frame without blocking.
	app.Advance(200 * time.Millisecond)
	if loopTicks < 10 || loopDraws < 10 {
		t.Fatalf("loop ran %d updates and %d draws while loading; expected frames to keep ticking", loopTicks, loopDraws)
	}
	if group.Done() {
		t.Fatal("group reported done while assets are still gated")
	}
	if group.Total() != 5 {
		t.Fatalf("total %d, want 5", group.Total())
	}

	// Sound and image finish via NSGo's platform callbacks (2 of 5 = 0.4).
	app.WaitUntil(time.Second, func() bool { return group.Loaded() == 2 })

	// Release the gated assets one at a time and check progress advances.
	close(atlasGate)
	app.WaitUntil(time.Second, func() bool { return group.Loaded() == 3 })
	app.Advance(50 * time.Millisecond)

	close(mapGate)
	app.WaitUntil(time.Second, func() bool { return group.Loaded() == 4 })

	close(fontGate)
	app.WaitUntil(time.Second, func() bool { return group.Done() })

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("LoadGroup returned error: %v", err)
		}
	default:
		t.Fatal("onDone callback did not fire")
	}

	if group.Progress() != 1 || group.Loaded() != 5 || group.Total() != 5 || group.Err() != nil {
		t.Fatalf("final state: progress=%v loaded=%d total=%d err=%v", group.Progress(), group.Loaded(), group.Total(), group.Err())
	}

	// Progress must move monotonically from 0 to 1.
	if len(progresses) < 2 || progresses[0] != 0 || progresses[len(progresses)-1] != 1 {
		t.Fatalf("unexpected progress history: %v", progresses)
	}
	for i := 1; i < len(progresses); i++ {
		if progresses[i] < progresses[i-1] {
			t.Fatalf("progress went backwards: %v", progresses)
		}
	}

	// Verify typed getters on both Group and Manager.
	atlas, ok := group.Atlas("hero")
	if !ok || atlas == nil {
		t.Fatal("group.Atlas(hero) missing")
	}
	if len(atlas.Frames) != 2 || atlas.Frames["run.png"].X != 16 {
		t.Fatalf("unexpected atlas frames: %+v", atlas.Frames)
	}
	if a2, ok := mgr.Atlas("hero"); !ok || a2 != atlas {
		t.Fatal("mgr.Atlas(hero) did not return the same atlas")
	}

	snd, ok := group.Sound("click")
	if !ok || snd == nil {
		t.Fatal("group.Sound(click) missing")
	}
	if s2, ok := mgr.Sound("click"); !ok || s2 != snd {
		t.Fatal("mgr.Sound(click) did not return the same sound")
	}

	img, ok := group.Image("bg")
	if !ok || img.URI != bgPNGURI {
		t.Fatalf("group.Image(bg) = %+v, %v", img, ok)
	}
	if img2, ok := mgr.Image("bg"); !ok || img2.URI != bgPNGURI {
		t.Fatalf("mgr.Image(bg) = %+v, %v", img2, ok)
	}

	rawFont, ok := group.Bytes("font")
	if !ok || string(rawFont) != "BMFont-data" {
		t.Fatalf("group.Bytes(font) = %q, %v", string(rawFont), ok)
	}
	if rawFont2, ok := mgr.Bytes("font"); !ok || string(rawFont2) != "BMFont-data" {
		t.Fatalf("mgr.Bytes(font) = %q, %v", string(rawFont2), ok)
	}

	rawMap, ok := group.Get("map")
	if !ok {
		t.Fatal("group.Get(map) missing")
	}
	if fm, ok := rawMap.(*fakeMap); !ok || fm.Name != "level-1-map" {
		t.Fatalf("group.Get(map) = %#v", rawMap)
	}
	if rawMap2, ok := mgr.Get("map"); !ok || rawMap2 != rawMap {
		t.Fatal("mgr.Get(map) mismatch")
	}
}

func TestEmptyGroupAndIdempotentLoad(t *testing.T) {
	var (
		mgr         *assets.Manager
		emptyGroup  *assets.Group
		sharedGroup *assets.Group
		emptyCalled bool
		reads       atomic.Int32
		gate        = make(chan struct{})
		waitersDone atomic.Int32
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithReadAsset(func(ctx context.Context, uri string) ([]byte, error) {
			reads.Add(1)
			<-gate
			return []byte("ok"), nil
		}))

		emptyGroup = mgr.Group("empty")
		mgr.LoadGroup("empty", func(err error) {
			if err != nil {
				t.Errorf("empty group error: %v", err)
			}
			emptyCalled = true
		})

		sharedGroup = mgr.Group("shared").AddBytes("cfg", "asset:cfg.json")
		mgr.LoadGroup("shared", func(err error) {
			if err == nil {
				waitersDone.Add(1)
			}
		})
		// Second LoadGroup while in flight attaches without re-reading.
		mgr.LoadGroup("shared", func(err error) {
			if err == nil {
				waitersDone.Add(1)
			}
		})
		return core.View{}
	}, envWithMedia()...)

	if !emptyCalled || !emptyGroup.Done() || emptyGroup.Progress() != 1 {
		t.Fatalf("empty group: called=%v done=%v progress=%v", emptyCalled, emptyGroup.Done(), emptyGroup.Progress())
	}

	close(gate)
	app.WaitUntil(time.Second, func() bool { return sharedGroup.Done() })
	if reads.Load() != 1 {
		t.Fatalf("reads=%d, want 1", reads.Load())
	}
	if waitersDone.Load() != 2 {
		t.Fatalf("waitersDone=%d, want 2", waitersDone.Load())
	}

	// Third LoadGroup after completion returns immediately without re-reading.
	var alreadyDone bool
	app.Post(func() {
		mgr.LoadGroup("shared", func(err error) {
			if err != nil {
				t.Errorf("already-loaded group error: %v", err)
			}
			alreadyDone = true
		})
	})
	app.Settle()
	if !alreadyDone || reads.Load() != 1 {
		t.Fatalf("alreadyDone=%v reads=%d, want true and 1", alreadyDone, reads.Load())
	}
}

func TestReleaseGroupClosesSoundsRunsHooksAndReloads(t *testing.T) {
	var (
		mgr      *assets.Manager
		group    *assets.Group
		released []string
		mu       sync.Mutex
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithReadAsset(func(_ context.Context, uri string) ([]byte, error) {
			switch uri {
			case heroJSONURI:
				return []byte(heroAtlasJSON), nil
			case mapURI:
				return []byte("map-v1"), nil
			default:
				return nil, fmt.Errorf("missing %q", uri)
			}
		}))

		group = mgr.Group("stage1").
			AddAtlas("hero", heroJSONURI, core.Src(heroPNGURI)).
			AddSound("click", audio.Source{URI: clickURI}).
			AddSound("jump", audio.Source{URI: jumpURI}).
			AddImage("bg", core.Src(bgPNGURI)).
			AddCustom("map", mapURI, func(b []byte) (any, error) {
				return &fakeMap{Name: string(b)}, nil
			}, func(v any) {
				mu.Lock()
				released = append(released, v.(*fakeMap).Name)
				mu.Unlock()
			})

		mgr.LoadGroup("stage1", nil)
		return core.View{}
	}, envWithMedia()...)

	app.WaitUntil(time.Second, func() bool { return group.Done() })
	if len(app.Sounds) != 2 {
		t.Fatalf("decoded sounds = %d, want 2", len(app.Sounds))
	}
	for i, s := range app.Sounds {
		if s.Unloaded {
			t.Fatalf("sound %d unloaded before release", i)
		}
	}

	// Pre-closing one sound must not cause ReleaseGroup to panic or fail.
	app.Post(func() {
		s, ok := group.Sound("click")
		if !ok {
			t.Error("click sound missing")
			return
		}
		s.Close()
		mgr.ReleaseGroup("stage1")
	})
	app.Settle()

	for i, s := range app.Sounds {
		if !s.Unloaded {
			t.Fatalf("sound %d (%s) was not unloaded by ReleaseGroup", i, s.Source.URI)
		}
	}
	mu.Lock()
	gotReleased := slices.Clone(released)
	mu.Unlock()
	if !slices.Equal(gotReleased, []string{"map-v1"}) {
		t.Fatalf("released hooks = %v, want [map-v1]", gotReleased)
	}

	if group.Progress() != 0 || group.Loaded() != 0 || group.Done() || group.Total() != 5 {
		t.Fatalf("after release: progress=%v loaded=%d done=%v total=%d", group.Progress(), group.Loaded(), group.Done(), group.Total())
	}
	if _, ok := group.Atlas("hero"); ok {
		t.Fatal("atlas still present after ReleaseGroup")
	}
	if _, ok := group.Sound("jump"); ok {
		t.Fatal("sound still present after ReleaseGroup")
	}
	if _, ok := group.Image("bg"); ok {
		t.Fatal("image still present after ReleaseGroup")
	}
	if _, ok := group.Get("map"); ok {
		t.Fatal("custom asset still present after ReleaseGroup")
	}

	// Reload the same group cleanly.
	var reloadErr error
	var reloaded bool
	app.Post(func() {
		mgr.LoadGroup("stage1", func(err error) {
			reloadErr = err
			reloaded = true
		})
	})
	app.WaitUntil(time.Second, func() bool { return reloaded })
	if reloadErr != nil {
		t.Fatalf("reload error: %v", reloadErr)
	}
	if !group.Done() || group.Progress() != 1 {
		t.Fatalf("after reload: done=%v progress=%v", group.Done(), group.Progress())
	}
	if _, ok := group.Atlas("hero"); !ok {
		t.Fatal("atlas missing after reload")
	}
	if _, ok := group.Sound("click"); !ok {
		t.Fatal("sound missing after reload")
	}
	if len(app.Sounds) != 4 || app.Sounds[2].Unloaded || app.Sounds[3].Unloaded {
		t.Fatalf("expected two freshly decoded sounds after reload, got %d", len(app.Sounds))
	}
}

func TestLoadingErrorReportsCleanlyWithoutHanging(t *testing.T) {
	slowCancelled := make(chan struct{})
	var (
		mgr            *assets.Manager
		group          *assets.Group
		failNow        atomic.Bool
		customReleased atomic.Int32
		doneErr        error
		doneFired      atomic.Bool
	)
	failNow.Store(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithReadAsset(func(ctx context.Context, uri string) ([]byte, error) {
			switch uri {
			case "asset:bad.json":
				if failNow.Load() {
					return nil, errors.New("disk read failed")
				}
				return []byte("ok"), nil
			case "asset:slow.dat":
				if failNow.Load() {
					<-ctx.Done()
					close(slowCancelled)
					return nil, ctx.Err()
				}
				return []byte("slow-ok"), nil
			case mapURI:
				return []byte("fast-custom"), nil
			default:
				return nil, fmt.Errorf("unexpected %q", uri)
			}
		}))

		group = mgr.Group("broken").
			AddSound("click", audio.Source{URI: clickURI}).
			AddCustom("fast", mapURI, func(b []byte) (any, error) {
				return &fakeMap{Name: string(b)}, nil
			}, func(any) {
				customReleased.Add(1)
			}).
			AddBytes("bad", "asset:bad.json").
			AddBytes("slow", "asset:slow.dat")

		mgr.LoadGroup("broken", func(err error) {
			doneErr = err
			doneFired.Store(true)
		})
		return core.View{}
	}, envWithMedia()...)

	app.WaitUntil(time.Second, func() bool { return doneFired.Load() })
	if doneErr == nil || !strings.Contains(doneErr.Error(), "disk read failed") {
		t.Fatalf("doneErr = %v, want error containing 'disk read failed'", doneErr)
	}
	if group.Err() == nil || group.Done() || group.Loading() {
		t.Fatalf("after error: err=%v done=%v loading=%v", group.Err(), group.Done(), group.Loading())
	}

	// Slow asset's context must have been cancelled promptly.
	select {
	case <-slowCancelled:
	case <-time.After(time.Second):
		t.Fatal("slow in-flight read was not cancelled on group failure")
	}

	// Any sound decoded during the failed attempt must be closed cleanly.
	app.Settle()
	for i, s := range app.Sounds {
		if !s.Unloaded {
			t.Fatalf("sound %d leaked after failed load", i)
		}
	}

	// Retrying once the underlying error is resolved succeeds.
	failNow.Store(false)
	doneFired.Store(false)
	doneErr = nil
	app.Post(func() {
		mgr.LoadGroup("broken", func(err error) {
			doneErr = err
			doneFired.Store(true)
		})
	})
	app.WaitUntil(time.Second, func() bool { return doneFired.Load() })
	if doneErr != nil || !group.Done() || group.Progress() != 1 || group.Err() != nil {
		t.Fatalf("retry failed: doneErr=%v done=%v progress=%v err=%v", doneErr, group.Done(), group.Progress(), group.Err())
	}
}

func TestDisposingScopeMidLoadCancelsAndReleasesWithoutLeaking(t *testing.T) {
	ctxCancelled := make(chan struct{})
	lateGate := make(chan struct{})
	lateDone := make(chan struct{})

	var (
		shown          *core.Signal[bool]
		callbackFired  atomic.Bool
		groupReleased  atomic.Int32
		directReleased atomic.Int32
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, true)
		return core.Show{
			When: shown.Get,
			Then: func(child *core.Scope) core.Node {
				mgr := assets.NewManager(child, assets.WithReadAsset(func(ctx context.Context, uri string) ([]byte, error) {
					switch uri {
					case "asset:cancellable":
						<-ctx.Done()
						close(ctxCancelled)
						return nil, ctx.Err()
					case "asset:ignores-ctx":
						// Simulates a reader that ignores ctx and finishes after disposal.
						<-lateGate
						return []byte("late-resource"), nil
					default:
						return nil, fmt.Errorf("unexpected %q", uri)
					}
				}))

				mgr.Group("ephemeral").
					AddSound("click", audio.Source{URI: clickURI}).
					AddBytes("cancel", "asset:cancellable").
					AddCustom("late", "asset:ignores-ctx", func(b []byte) (any, error) {
						return &fakeMap{Name: string(b)}, nil
					}, func(any) {
						groupReleased.Add(1)
						close(lateDone)
					})

				mgr.LoadGroup("ephemeral", func(error) {
					callbackFired.Store(true)
				})

				mgr.LoadCustom("asset:ignores-ctx", func(_ context.Context, b []byte) (any, error) {
					return &fakeMap{Name: string(b)}, nil
				}, func(any) {
					directReleased.Add(1)
				}, func(any, error) {
					callbackFired.Store(true)
				})

				return core.View{}
			},
		}
	}, envWithMedia()...)

	// Wait for the sound to decode while the other assets remain in flight.
	app.WaitUntil(time.Second, func() bool { return len(app.Sounds) == 1 })

	// Dispose the child scope mid-load.
	app.Post(func() { shown.Set(false) })
	app.Settle()

	select {
	case <-ctxCancelled:
	case <-time.After(time.Second):
		t.Fatal("manager context was not cancelled when scope was disposed")
	}

	// Sound loaded before disposal must be unloaded.
	if !app.Sounds[0].Unloaded {
		t.Fatal("sound was not unloaded when scope was disposed")
	}

	// Now unblock the late worker goroutines that finish AFTER scope disposal.
	close(lateGate)
	select {
	case <-lateDone:
	case <-time.After(time.Second):
		t.Fatal("late group custom resource was not released after scope disposal")
	}
	app.WaitUntil(time.Second, func() bool { return directReleased.Load() == 1 })
	app.Advance(50 * time.Millisecond)

	if groupReleased.Load() != 1 || directReleased.Load() != 1 {
		t.Fatalf("released counts: group=%d direct=%d, want 1 and 1", groupReleased.Load(), directReleased.Load())
	}
	if callbackFired.Load() {
		t.Fatal("user callback fired after scope was disposed")
	}
}

func TestLoadingScreenShowsProgressAndCallsOnReady(t *testing.T) {
	gate1 := make(chan struct{})
	gate2 := make(chan struct{})

	var (
		mgr   *assets.Manager
		ready *core.Signal[bool]
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ready = core.NewSignal(cx, false)
		mgr = assets.NewManager(cx, assets.WithReadAsset(func(ctx context.Context, uri string) ([]byte, error) {
			switch uri {
			case "asset:a":
				<-gate1
				return []byte("a"), nil
			case "asset:b":
				<-gate2
				return []byte("b"), nil
			default:
				return nil, fmt.Errorf("unexpected %q", uri)
			}
		}))
		mgr.Group("intro").
			AddSound("click", audio.Source{URI: clickURI}).
			AddBytes("a", "asset:a").
			AddBytes("b", "asset:b")

		return core.Show{
			When: ready.Get,
			Then: func(*core.Scope) core.Node {
				return core.Text{Content: "Game Ready"}
			},
			Else: func(*core.Scope) core.Node {
				return assets.LoadingScreen{
					Manager: mgr,
					Group:   "intro",
					Title:   "Loading Intro",
					TestID:  "loading-screen",
					OnReady: func() { ready.Set(true) },
				}
			},
		}
	}, envWithMedia()...)

	// Initial state: LoadingScreen is visible with a progressbar.
	app.Find("id:loading-screen")
	app.Find("text:Loading Intro")
	app.Find("role:progressbar")

	// Sound finishes first (1/3 = 33%).
	app.WaitFor("text:33%", time.Second)

	// Complete first file asset (2/3 = 67%).
	close(gate1)
	app.WaitFor("text:67%", time.Second)

	// Complete second asset -> OnReady switches to the game view.
	close(gate2)
	app.WaitFor("text:Game Ready", time.Second)
	if app.Exists("id:loading-screen") {
		t.Fatal("loading screen still mounted after OnReady switched views")
	}
	// Sounds loaded by LoadingScreen belong to the Manager's scope, so
	// unmounting LoadingScreen must NOT unload them.
	if len(app.Sounds) != 1 || app.Sounds[0].Unloaded {
		t.Fatalf("sound was unloaded when LoadingScreen unmounted: %+v", app.Sounds)
	}
	if _, ok := mgr.Sound("click"); !ok {
		t.Fatal("mgr.Sound(click) missing after LoadingScreen unmounted")
	}
}

func TestLoadingViewCustomRenderAndError(t *testing.T) {
	var (
		mgr        *assets.Manager
		gotErr     error
		errFired   atomic.Bool
		emptyReady atomic.Bool
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithReadAsset(func(_ context.Context, uri string) ([]byte, error) {
			return nil, errors.New("asset missing")
		}))
		mgr.Group("failing").AddBytes("missing", "asset:nope")

		return core.View{
			Children: []core.Node{
				assets.LoadingView(cx, assets.LoadingScreenProps{
					Manager: mgr,
					Group:   "failing",
					OnError: func(err error) {
						gotErr = err
						errFired.Store(true)
					},
					Render: func(progress float64, err error) core.Node {
						if err != nil {
							return core.Text{Content: "Failed: " + err.Error()}
						}
						return core.Text{Content: fmt.Sprintf("Custom %.0f%%", progress*100)}
					},
				}),
				assets.LoadingScreen{
					Manager: mgr,
					Group:   "empty-screen",
					OnReady: func() { emptyReady.Store(true) },
				},
			},
		}
	}, envWithMedia()...)

	app.WaitUntil(time.Second, func() bool { return errFired.Load() && emptyReady.Load() })
	if gotErr == nil || !strings.Contains(gotErr.Error(), "asset missing") {
		t.Fatalf("OnError got %v, want error containing 'asset missing'", gotErr)
	}
	app.WaitFor("text:asset missing", time.Second)
}

func TestDefaultFSReadsAssetURIs(t *testing.T) {
	fsys := fstest.MapFS{
		"sprites/hero.json": &fstest.MapFile{Data: []byte(heroAtlasJSON)},
		"data/config.txt":   &fstest.MapFile{Data: []byte("hello-fs")},
	}

	var (
		mgr   *assets.Manager
		group *assets.Group
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithFS(fsys))
		group = mgr.Group("fs").
			AddAtlas("hero", heroJSONURI, core.ImageSource{Data: []byte("fake-png")}).
			AddBytes("cfg", "asset:data/config.txt")
		mgr.LoadGroup("fs", nil)
		return core.View{}
	}, envWithMedia()...)

	app.WaitUntil(time.Second, func() bool { return group.Done() })
	atlas, ok := group.Atlas("hero")
	if !ok || len(atlas.Frames) != 2 {
		t.Fatalf("atlas = %+v, ok=%v", atlas, ok)
	}
	cfg, ok := group.Bytes("cfg")
	if !ok || string(cfg) != "hello-fs" {
		t.Fatalf("cfg = %q, ok=%v", string(cfg), ok)
	}
}

func TestStandaloneAsyncLoaders(t *testing.T) {
	var (
		mgr       *assets.Manager
		doneCount atomic.Int32
		gotBytes  []byte
		gotAtlas  bool
		gotSound  *audio.Sound
		gotCustom string
		gotErr    error
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithReadAsset(func(_ context.Context, uri string) ([]byte, error) {
			switch uri {
			case heroJSONURI:
				return []byte(heroAtlasJSON), nil
			case mapURI:
				return []byte("world-1"), nil
			case fontURI:
				return []byte("font-bytes"), nil
			default:
				return nil, errors.New("not found")
			}
		}))

		mgr.LoadBytes(fontURI, func(b []byte, err error) {
			if err != nil {
				t.Errorf("LoadBytes: %v", err)
			}
			gotBytes = b
			doneCount.Add(1)
		})
		mgr.LoadAtlas(heroJSONURI, core.Src(heroPNGURI), func(a *sprite.Atlas, err error) {
			if err != nil {
				t.Errorf("LoadAtlas: %v", err)
			}
			gotAtlas = a != nil && len(a.Frames) == 2
			doneCount.Add(1)
		})
		mgr.LoadSound(audio.Source{URI: clickURI}, func(s *audio.Sound, err error) {
			if err != nil {
				t.Errorf("LoadSound: %v", err)
			}
			gotSound = s
			doneCount.Add(1)
		})
		mgr.LoadCustom(mapURI, func(_ context.Context, data []byte) (any, error) {
			return string(data), nil
		}, nil, func(v any, err error) {
			if err != nil {
				t.Errorf("LoadCustom: %v", err)
			}
			gotCustom, _ = v.(string)
			doneCount.Add(1)
		})
		mgr.LoadBytes("asset:missing", func(_ []byte, err error) {
			gotErr = err
			doneCount.Add(1)
		})
		return core.View{}
	}, envWithMedia()...)

	app.WaitUntil(time.Second, func() bool { return doneCount.Load() == 5 })
	if string(gotBytes) != "font-bytes" || !gotAtlas || gotSound == nil || gotCustom != "world-1" || gotErr == nil {
		t.Fatalf("bytes=%q atlas=%v sound=%v custom=%q err=%v", string(gotBytes), gotAtlas, gotSound != nil, gotCustom, gotErr)
	}
}

func TestReleaseGroupMidLoadCancelsAndDoesNotLeak(t *testing.T) {
	lateGate := make(chan struct{})
	var (
		mgr           *assets.Manager
		group         *assets.Group
		callbackFired atomic.Bool
		lateReleased  atomic.Int32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = assets.NewManager(cx, assets.WithReadAsset(func(_ context.Context, uri string) ([]byte, error) {
			<-lateGate
			return []byte("late"), nil
		}))
		group = mgr.Group("mid").
			AddSound("click", audio.Source{URI: clickURI}).
			AddCustom("late", "asset:late", func(b []byte) (any, error) {
				return &fakeMap{Name: string(b)}, nil
			}, func(any) {
				lateReleased.Add(1)
			})
		mgr.LoadGroup("mid", func(error) {
			callbackFired.Store(true)
		})
		return core.View{}
	}, envWithMedia()...)

	app.WaitUntil(time.Second, func() bool { return group.Loaded() == 1 })
	app.Post(func() { mgr.ReleaseGroup("mid") })
	app.Settle()

	if !app.Sounds[0].Unloaded {
		t.Fatal("already-loaded sound was not unloaded on mid-load ReleaseGroup")
	}
	if group.Progress() != 0 || group.Loading() || group.Done() {
		t.Fatalf("after mid-load release: progress=%v loading=%v done=%v", group.Progress(), group.Loading(), group.Done())
	}

	close(lateGate)
	app.WaitUntil(time.Second, func() bool { return lateReleased.Load() == 1 })
	app.Advance(50 * time.Millisecond)

	if callbackFired.Load() {
		t.Fatal("onDone fired after mid-load ReleaseGroup")
	}
}
