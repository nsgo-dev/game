//go:build release

package debug_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/framedata"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestReleaseBuildIsDisabledZeroCostStub(t *testing.T) {
	if debug.Enabled() {
		t.Fatal("Enabled() = true in release build, want false")
	}

	dbg := debug.New(debug.Options{
		ToggleKey: "F3",
		Visible:   true,
		TargetFPS: 60,
		LineWidth: 2,
		Scenes: func() []debug.SceneSnapshot {
			t.Error("Options.Scenes called in release build")
			return nil
		},
		AI: func() debug.AISnapshot {
			t.Error("Options.AI called in release build")
			return debug.AISnapshot{}
		},
	})

	if dbg.Visible() {
		t.Fatal("Visible() = true in release build, want false")
	}
	dbg.SetVisible(true)
	dbg.Toggle()
	dbg.NoteFrameCause("shader compile")
	dbg.RecordFrame(16*time.Millisecond, 4*time.Millisecond, 2*time.Millisecond)
	if dbg.Visible() {
		t.Fatal("Visible() became true after SetVisible/Toggle in release build")
	}

	var updates, draws int
	loop := &game.Loop{
		Rate:   60,
		Update: func(time.Duration) { updates++ },
		Draw:   func(float64) { draws++ },
	}
	dbg.WrapLoop(loop)

	sp := sprite.NewSprite(sprite.Frame{Name: "hero", W: 16, H: 16, Width: 16, Height: 16})
	batch := &sprite.Batch{Sprites: []*sprite.Sprite{&sp}}
	shapes := debug.ShapeOverlay{LineWidth: 2}
	box := core.Bounds{Origin: core.Point{X: 1, Y: 2}, Size: core.Size{Width: 10, Height: 12}}
	fighter := &framedata.Fighter{Moves: &framedata.Doc{Version: 1, Moves: []framedata.Move{{
		Name: "idle", Length: 1, Loop: true,
		Boxes: []framedata.Box{{Kind: framedata.KindHurt, Start: 1, End: 1, Width: 10, Height: 10}},
	}}}, Neutral: "idle"}
	(&framedata.World{Fighters: []*framedata.Fighter{fighter}}).Step()

	btIns := ai.NewBTInspector(ai.Named("root", ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning })))
	fsm := flow.NewMachine("idle")
	steer := debug.SteeringDebug{
		Agent:           ai.Agent{Pos: core.Point{X: 10, Y: 20}, Vel: core.Point{X: 5, Y: 0}, Radius: 8},
		DesiredVelocity: core.Point{X: 10, Y: 0},
		Target:          core.Point{X: 50, Y: 20},
		HasTarget:       true,
		Path:            []core.Point{{X: 10, Y: 20}, {X: 50, Y: 20}},
	}

	var viewNode core.Node
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		dbg.Attach(cx)
		loop.Run(cx)
		viewNode = dbg.View(cx)
		return core.View{
			Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)},
			Children: []core.Node{
				core.Canvas{
					TestID: "canvas",
					Style:  core.Style{Width: core.Pt(200), Height: core.Pt(200)},
					Draw: func(d *core.Drawing, size core.Size) {
						dbg.Stats().DrawGraph(d, size, 60)
						dbg.DrawShapes(d, func(s *debug.ShapeOverlay) {
							t.Error("DrawShapes callback invoked in release build")
							s.DrawBox(d, box, core.RGB(0x22C55E))
						})
						dbg.DrawBox(d, box, core.RGB(0x22C55E))
						dbg.DrawCircle(d, 10, 10, 5, core.RGB(0x38BDF8))
						dbg.DrawSpriteBounds(d, &sp, core.RGB(0xF59E0B))
						dbg.DrawBatchBounds(d, batch, core.RGB(0xF59E0B))
						shapes.DrawBox(d, box, core.RGB(0x22C55E))
						shapes.DrawCircle(d, 10, 10, 5, core.RGB(0x38BDF8))
						shapes.DrawSpriteBounds(d, &sp, core.RGB(0xF59E0B))
						shapes.DrawBatchBounds(d, batch, core.RGB(0xF59E0B))
						dbg.DrawFighterBoxes(d, fighter, core.RGB(0x38BDF8), core.RGB(0xEF4444))
						shapes.DrawFighterBoxes(d, fighter, core.RGB(0x38BDF8), core.RGB(0xEF4444))
						dbg.DrawSteering(d, steer)
						dbg.DrawVector(d, steer.Agent.Pos, steer.Agent.Vel, core.RGB(0x22C55E))
						dbg.DrawPath(d, steer.Path, core.RGB(0xA78BFA))
						dbg.DrawTarget(d, steer.Target, core.RGB(0xF59E0B))
						shapes.DrawSteering(d, steer)
						shapes.DrawVector(d, steer.Agent.Pos, steer.Agent.Vel, core.RGB(0x22C55E))
						shapes.DrawPath(d, steer.Path, core.RGB(0xA78BFA))
						shapes.DrawTarget(d, steer.Target, core.RGB(0xF59E0B))
					},
				},
				viewNode,
			},
		}
	})

	if viewNode != nil {
		t.Fatalf("View(cx) = %#v in release build, want nil", viewNode)
	}
	if app.Exists("debug-overlay") {
		t.Fatal("debug-overlay exists in release build")
	}

	app.Key("F3")
	if dbg.Visible() || app.Exists("debug-overlay") {
		t.Fatal("pressing F3 showed overlay in release build")
	}

	app.Advance(100 * time.Millisecond)
	if updates == 0 || draws == 0 {
		t.Fatalf("loop did not run normally after WrapLoop: updates=%d draws=%d", updates, draws)
	}

	dbg.RecordFrame(16*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	dbg.SetDrawStats(debug.DrawStats{DrawCalls: 5, Sprites: 10})
	if dbg.HasDrawStats() || dbg.DrawStats() != (debug.DrawStats{}) {
		t.Fatal("Overlay recorded DrawStats in release build")
	}
	dbg.ResetStats()
	dbg.SetScenes([]debug.SceneSnapshot{{Name: "world"}})
	dbg.RefreshScenes()
	dbg.SetAI(debug.BehaviorTreeSnapshot("guard", btIns))
	dbg.SetAI(debug.StateMachineSnapshot("guard", fsm))
	dbg.RefreshAI()
	if got := dbg.AISnapshot(); got.Agent != "" || len(got.Tree) != 0 || got.Machine != nil {
		t.Fatalf("AISnapshot() in release = %+v, want zero", got)
	}
	dbg.ClearAI()

	stats := dbg.Stats()
	stats.Record(16*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	if stats.Len() != 0 || stats.FPS() != 0 || stats.MaxTotal() != 0 || stats.Latest() != (debug.FrameSample{}) || stats.At(0) != (debug.FrameSample{}) || stats.Average() != (debug.FrameSample{}) {
		t.Fatal("FrameStats recorded data in release build")
	}
	if out := stats.Samples(nil); len(out) != 0 {
		t.Fatalf("Samples() returned %d items in release build, want 0", len(out))
	}
	stats.Reset()

	if n := len(app.Find("canvas").Ops); n != 0 {
		t.Fatalf("canvas recorded %d ops in release build, want 0", n)
	}

	// Console release stubs.
	c := debug.NewConsole()
	vFloat := 1.5
	vInt := 3
	vBool := true
	vStr := "hello"
	c.RegisterCommand("cmd", "help", func([]string) (string, error) {
		t.Error("console command invoked in release build")
		return "bad", nil
	})
	c.RegisterFloat("f", 0, 10, &vFloat)
	c.RegisterInt("i", 0, 10, &vInt)
	c.RegisterBool("b", &vBool)
	c.RegisterString("s", &vStr)
	if out, err := c.Exec("set f 9"); out != "" || err != nil || vFloat != 1.5 {
		t.Fatalf("Console.Exec in release = (%q, %v), vFloat=%v", out, err, vFloat)
	}
	if len(c.Tunables()) != 0 || len(c.Commands()) != 0 {
		t.Fatal("Console Tunables/Commands returned data in release build")
	}
	if _, ok := c.Tunable("f"); ok {
		t.Fatal("Console.Tunable returned true in release build")
	}
	if ch, err := c.SetTunable("f", 2.0, false); ch != (debug.TunableChange{}) || err == nil || vFloat != 1.5 {
		t.Fatalf("Console.SetTunable in release = (%+v, %v), vFloat=%v; want an error and no change", ch, err, vFloat)
	}
	if len(c.History()) != 0 || c.HistoryPrev() != "" || c.HistoryNext() != "" || len(c.Entries()) != 0 || len(c.Complete("s")) != 0 {
		t.Fatal("Console history/entries/complete returned data in release build")
	}
	if err := c.SaveTunables(app.Storage(), "k"); err != nil {
		t.Fatalf("SaveTunables in release error = %v", err)
	}
	if err := c.LoadTunables(app.Storage(), "k"); err != nil {
		t.Fatalf("LoadTunables in release error = %v", err)
	}
	_ = debug.ConsoleView(nil, c)

	// Profiler release stubs.
	p := debug.NewProfiler(300)
	p.SetNow(time.Now)
	p.SetTraceContext(context.Background())
	p.StartRecording(10)
	p.BeginFrame()
	tok := p.Begin("physics")
	p.End(tok)
	p.Scope("render").End()
	p.EndFrame()
	p.StopRecording()
	if p.Recording() || p.RecordedFrames() != 0 || len(p.TopScopes(5)) != 0 {
		t.Fatal("Profiler recorded data in release build")
	}
	p.Reset()
	var traceBuf bytes.Buffer
	if err := p.WriteChromeTrace(&traceBuf); err != nil || traceBuf.Len() != 0 {
		t.Fatalf("WriteChromeTrace in release = (len=%d, err=%v), want (0, nil)", traceBuf.Len(), err)
	}

	// MemoryTracker release stubs + pure estimates.
	mem := debug.NewMemoryTracker()
	mem.SetBudget("lvl", 10)
	mem.SetTotalBudget(10)
	mem.Track("lvl", "a.png", debug.AssetImage, 1000)
	if mem.GroupBytes("lvl") != 0 || mem.TotalBytes() != 0 || mem.TotalBudget() != 0 || len(mem.Groups()) != 0 {
		t.Fatal("MemoryTracker recorded bytes in release build")
	}
	if err := mem.CheckBudget("lvl"); err != nil {
		t.Fatalf("CheckBudget in release error = %v", err)
	}
	if err := mem.CheckAllBudgets(); err != nil {
		t.Fatalf("CheckAllBudgets in release error = %v", err)
	}
	mem.Untrack("lvl", "a.png")
	mem.ReleaseGroup("lvl")
	if debug.EstimateImageBytes(4, 4) != 64 || debug.EstimatePCMBytes(10, 2, 2) != 40 {
		t.Fatal("Estimate helpers returned wrong values in release build")
	}

	// GCMonitor and alloc helper release stubs.
	gc := debug.NewGCMonitor(60)
	if sample := gc.SampleFrame(16 * time.Millisecond); sample != (debug.GCFrameSample{}) {
		t.Fatalf("GCMonitor.SampleFrame in release = %+v, want zero", sample)
	}
	if gc.TotalCycles() != 0 || gc.MaxPause() != 0 || gc.LastPause() != 0 || gc.RecentHitches() != 0 || gc.HeapObjectsBytes() != 0 || gc.AllocsPerFrame() != 0 || gc.AllocBytesPerFrame() != 0 || gc.Len() != 0 || gc.At(0) != (debug.GCFrameSample{}) || gc.Latest() != (debug.GCFrameSample{}) {
		t.Fatal("GCMonitor returned non-zero metrics in release build")
	}
	gc.Reset()
	if got := debug.MeasureAllocs(10, func() { _ = make([]byte, 64) }); got != 0 {
		t.Fatalf("MeasureAllocs in release = %v, want 0", got)
	}
	debug.AssertZeroAllocs(t, 10, func() { _ = make([]byte, 64) })

	// Overlay accessors in release build.
	if dbg.Console() == nil || dbg.Profiler() == nil || dbg.Memory() == nil || dbg.GC() == nil {
		t.Fatal("Overlay release accessors returned nil")
	}
}

func TestReleaseBuildLinksNoOverlayCode(t *testing.T) {
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" || runtime.GOOS == "android" || runtime.GOOS == "ios" {
		t.Skip("symbol inspection requires a native desktop executable")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	out, err := exec.Command("go", "tool", "nm", exe).CombinedOutput()
	if err != nil {
		t.Skipf("go tool nm unavailable: %v", err)
	}
	syms := string(out)
	for _, forbidden := range []string{
		"packages/game/debug.(*Overlay).aiInspectorView",
		"packages/game/debug.buildAITreeView",
		"packages/game/debug.buildAIMachineView",
		"packages/game/debug.resolveSteeringColors",
		"packages/game/debug.overlayPanelStyle",
		"packages/game/debug.(*Overlay).sceneInspectorView",
	} {
		if strings.Contains(syms, forbidden) {
			t.Fatalf("release binary linked debug overlay symbol %q", forbidden)
		}
	}
}
