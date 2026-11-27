package scene_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	edgeSize     = 32
	edgeOldColor = core.Color(0xff2266cc)
	edgeNewColor = core.Color(0xffcc6622)
)

func fill2D(c core.Color) *scene.Controller {
	return &scene.Controller{
		Draw: func(d *core.Drawing, _ float64) {
			d.SetFillColor(c).FillRect(0, 0, edgeSize, edgeSize)
		},
	}
}

// visibleSceneColors returns the first fill color of each scene canvas under the stage whose wrapper is not hidden.
func visibleSceneColors(app *nsgotest.App) []core.Color {
	var out []core.Color
	for _, c := range findCanvasChildren(app.Find("id:stage"), "transition-overlay") {
		if c.Parent != nil && c.Parent.Style.Hidden {
			continue
		}
		if _, col, ok := overlayFillAlpha(c.Ops); ok {
			out = append(out, col)
		}
	}
	return out
}

func startEdgeHost(t *testing.T, setup func(cx *core.Scope, mgr *scene.Manager)) (*nsgotest.App, *scene.Manager) {
	t.Helper()
	var mgr *scene.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		if setup != nil {
			setup(cx, mgr)
		}
		return mgr.View(scene.HostOptions{
			StageTestID:      "stage",
			TransitionTestID: "transition-overlay",
			Style:            core.Style{Width: core.Pt(edgeSize), Height: core.Pt(edgeSize)},
		})
	}, nsgotest.Size(edgeSize, edgeSize), nsgotest.ManualFrames())
	return app, mgr
}

func TestManagerViewNilDrawTransitionShowsIncomingScene(t *testing.T) {
	oldScene, newScene := fill2D(edgeOldColor), fill2D(edgeNewColor)
	app, mgr := startEdgeHost(t, func(_ *core.Scope, mgr *scene.Manager) { mgr.Switch(oldScene, nil) })

	app.Post(func() { mgr.Switch(newScene, &scene.Transition{Duration: 400 * time.Millisecond}) })
	app.Settle()
	app.Advance(100 * time.Millisecond)
	if !mgr.Transitioning() {
		t.Fatal("nil-Draw transition ended early")
	}
	if got := visibleSceneColors(app); !reflect.DeepEqual(got, []core.Color{edgeNewColor}) {
		t.Fatalf("mid-transition visible scene colors = %#x, want only the incoming scene %#x", got, edgeNewColor)
	}
	if ops := len(app.Find("id:transition-overlay").Ops); ops != 0 {
		t.Fatalf("nil-Draw transition overlay ops = %d, want 0", ops)
	}

	app.Advance(320 * time.Millisecond)
	if mgr.Transitioning() {
		t.Fatal("nil-Draw transition did not finish")
	}
	if got := visibleSceneColors(app); !reflect.DeepEqual(got, []core.Color{edgeNewColor}) {
		t.Fatalf("after transition visible scene colors = %#x, want %#x", got, edgeNewColor)
	}
}

func TestManagerViewZeroDurationTransitionSwitchesAtOnce(t *testing.T) {
	oldScene, newScene := fill2D(edgeOldColor), fill2D(edgeNewColor)
	app, mgr := startEdgeHost(t, func(_ *core.Scope, mgr *scene.Manager) { mgr.Switch(oldScene, nil) })

	app.Post(func() { mgr.Switch(newScene, scene.Fade(0, core.RGB(0x000000))) })
	app.Settle()
	if mgr.Transitioning() {
		t.Fatal("zero-duration transition is still running")
	}
	if got := visibleSceneColors(app); !reflect.DeepEqual(got, []core.Color{edgeNewColor}) {
		t.Fatalf("visible scene colors = %#x, want %#x", got, edgeNewColor)
	}
}

func TestManagerViewHonoursReplacedConstructorDraw(t *testing.T) {
	oldScene, newScene := fill2D(edgeOldColor), fill2D(edgeNewColor)
	app, mgr := startEdgeHost(t, func(_ *core.Scope, mgr *scene.Manager) { mgr.Switch(oldScene, nil) })

	calls := 0
	tr := scene.Fade(400*time.Millisecond, core.RGB(0x000000))
	tr.Draw = func(d *core.Drawing, _ core.Size, _ float64, _, drawNew func(*core.Drawing)) {
		calls++
		drawNew(d)
	}
	app.Post(func() { mgr.Switch(newScene, tr) })
	app.Settle()
	app.Advance(100 * time.Millisecond)
	if calls == 0 {
		t.Fatal("replaced Transition.Draw never ran under Manager.View")
	}
	if _, col, ok := overlayFillAlpha(app.Find("id:transition-overlay").Ops); !ok || col != edgeNewColor {
		t.Fatalf("transition overlay first fill = %#x (ok=%v), want the replaced Draw's incoming scene %#x", col, ok, edgeNewColor)
	}
}

func TestManagerViewDisposesViewScopeInSameStepAsExit(t *testing.T) {
	var (
		events  []string
		exited  bool
		afterOn int
	)
	view3D := &scene.Controller{
		Exit: func() {
			exited = true
			events = append(events, "exit")
		},
		View: func(scx *core.Scope) core.Node {
			scx.Cleanup(func() { events = append(events, "view-cleanup") })
			return gpu.View(scx, gpu.ViewProps{
				TestID: "edge-3d",
				Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				OnFrame: func(*gpu.Frame) {
					if exited {
						afterOn++
					}
				},
			})
		},
	}
	app, mgr := startEdgeHost(t, func(_ *core.Scope, mgr *scene.Manager) { mgr.Switch(view3D, nil) })
	app.Advance(50 * time.Millisecond)
	if len(app.Query("id:edge-3d")) != 1 {
		t.Fatal("3D view not mounted")
	}

	var atReturn []string
	app.Post(func() {
		mgr.Switch(fill2D(edgeNewColor), nil)
		atReturn = append([]string(nil), events...)
	})
	app.Settle()
	app.Advance(50 * time.Millisecond)

	if want := []string{"exit", "view-cleanup"}; !reflect.DeepEqual(atReturn, want) {
		t.Fatalf("events when Switch returned = %v, want %v", atReturn, want)
	}
	if afterOn != 0 {
		t.Fatalf("OnFrame ran %d times after Exit", afterOn)
	}
	if got := len(app.QueryMounted("id:edge-3d")); got != 0 {
		t.Fatalf("3D view still mounted after exit: %d", got)
	}
}

func TestManagerViewInterruptedViewTransition(t *testing.T) {
	var (
		builds, cleanups int
		lifecycle        []string
	)
	scene2D := fill2D(edgeOldColor)
	scene2D.Exit = func() { lifecycle = append(lifecycle, "exit-2d") }
	view3D := &scene.Controller{
		Enter: func(*core.Scope) { lifecycle = append(lifecycle, "enter-3d") },
		Exit:  func() { lifecycle = append(lifecycle, "exit-3d") },
		View: func(scx *core.Scope) core.Node {
			builds++
			scx.Cleanup(func() { cleanups++ })
			return core.View{TestID: "edge-view", Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)}}
		},
	}
	pause := fill2D(edgeNewColor)
	pause.Transparent = true

	app, mgr := startEdgeHost(t, func(_ *core.Scope, mgr *scene.Manager) { mgr.Switch(scene2D, nil) })

	// Push interrupts a running 2D -> 3D fade: the outgoing 2D scene exits and unmounts at once, the 3D view stays mounted once.
	app.Post(func() { mgr.Switch(view3D, scene.Fade(400*time.Millisecond, core.RGB(0x000000))) })
	app.Settle()
	app.Advance(100 * time.Millisecond)
	app.Post(func() { mgr.Push(pause) })
	app.Settle()
	app.Advance(20 * time.Millisecond)
	if mgr.Transitioning() {
		t.Fatal("Push did not finish the running transition")
	}
	if ops := len(app.Find("id:transition-overlay").Ops); ops != 0 {
		t.Fatalf("transition overlay ops after interruption = %d, want 0", ops)
	}
	if got := visibleSceneColors(app); !reflect.DeepEqual(got, []core.Color{edgeNewColor}) {
		t.Fatalf("visible 2D scenes after Push = %#x, want only the pause overlay %#x", got, edgeNewColor)
	}
	views := app.Query("id:edge-view")
	if len(views) != 1 || builds != 1 || cleanups != 0 {
		t.Fatalf("3D view after Push: visible=%d builds=%d cleanups=%d, want 1, 1, 0", len(views), builds, cleanups)
	}

	// A second Switch interrupts a running fade back to 2D: the 3D view and the pause overlay leave, and only the new 2D scene remains.
	app.Post(func() {
		mgr.Pop()
		mgr.Switch(scene2D, scene.Fade(400*time.Millisecond, core.RGB(0x000000)))
	})
	app.Settle()
	app.Advance(100 * time.Millisecond)
	app.Post(func() { mgr.Switch(fill2D(edgeNewColor), nil) })
	app.Settle()
	app.Advance(20 * time.Millisecond)
	if mgr.Transitioning() {
		t.Fatal("second Switch left a transition running")
	}
	if got := len(app.QueryMounted("id:edge-view")); got != 0 || cleanups != 1 {
		t.Fatalf("3D view after interrupted fade: mounted=%d cleanups=%d, want 0 and 1", got, cleanups)
	}
	if got := visibleSceneColors(app); !reflect.DeepEqual(got, []core.Color{edgeNewColor}) {
		t.Fatalf("visible 2D scenes after second Switch = %#x, want %#x", got, edgeNewColor)
	}
	want := []string{"enter-3d", "exit-2d", "exit-3d", "exit-2d"}
	if !reflect.DeepEqual(lifecycle, want) {
		t.Fatalf("lifecycle = %v, want %v", lifecycle, want)
	}
}

func TestManagerViewMountedInTwoHosts(t *testing.T) {
	var (
		mgr    *scene.Manager
		builds int
	)
	view := &scene.Controller{
		View: func(*core.Scope) core.Node {
			builds++
			return core.View{TestID: "twice-view", Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)}}
		},
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		mgr.Switch(view, nil)
		return core.View{
			Style: core.Style{Width: core.Pt(edgeSize), Height: core.Pt(edgeSize * 2)},
			Children: []core.Node{
				mgr.View(scene.HostOptions{StageTestID: "stage-a", Style: core.Style{Height: core.Pt(edgeSize)}}),
				mgr.View(scene.HostOptions{StageTestID: "stage-b", Style: core.Style{Height: core.Pt(edgeSize)}}),
			},
		}
	}, nsgotest.Size(edgeSize, edgeSize*2), nsgotest.ManualFrames())

	if got := len(app.Query("id:twice-view")); got != 2 || builds != 2 {
		t.Fatalf("scene mounted in two hosts: visible=%d builds=%d, want 2 and 2", got, builds)
	}
	app.Post(func() { mgr.Switch(nil, nil) })
	app.Settle()
	if got := len(app.QueryMounted("id:twice-view")); got != 0 {
		t.Fatalf("views still mounted after the scene left: %d", got)
	}
}
