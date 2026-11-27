//go:build !release

package debug_test

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestOverlayAIInspectorBehaviorTreeAndMachine(t *testing.T) {
	inRange := false
	tree := ai.Named("root", ai.Selector(
		ai.Named("attack", ai.Sequence(
			ai.Named("in_range", ai.Condition(func() bool { return inRange })),
			ai.Named("swing", ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning })),
		)),
		ai.Named("patrol", ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning })),
	))
	ins := ai.NewBTInspector(tree)
	tree.Tick(16 * time.Millisecond)

	fsm := flow.NewMachine("patrol")
	fsm.AddNamedTransition("spot", "patrol", "chase", func() bool { return true })
	fsm.Update(16 * time.Millisecond)

	dbg := debug.New(debug.Options{Visible: true})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(400)},
			Children: []core.Node{dbg.View(cx)},
		}
	})

	if app.Exists("debug-ai-inspector") {
		t.Fatal("debug-ai-inspector mounted before SetAI")
	}

	// Push behavior tree snapshot.
	app.Post(func() {
		dbg.SetAI(debug.BehaviorTreeSnapshot("guard-1", ins))
	})
	app.Settle()

	if !app.Exists("debug-ai-inspector") || !app.Exists("debug-ai-tree") {
		t.Fatal("debug-ai-inspector or debug-ai-tree not mounted after SetAI")
	}
	texts := app.Texts()
	for _, want := range []string{
		"AI · guard-1",
		`* selector "root"`,
		`  sequence "attack"`,
		`* action "patrol"`,
		"Running",
		"Failure",
		"Idle",
	} {
		if !slices.Contains(texts, want) {
			t.Fatalf("BT overlay texts %v missing %q", texts, want)
		}
	}

	// Advance tree onto attack branch and verify active path updates.
	app.Post(func() {
		inRange = true
		tree.Tick(16 * time.Millisecond)
		dbg.SetAI(debug.BehaviorTreeSnapshot("guard-1", ins))
	})
	app.Settle()

	texts = app.Texts()
	for _, want := range []string{`* sequence "attack"`, `* action "swing"`, `  action "patrol"`} {
		if !slices.Contains(texts, want) {
			t.Fatalf("updated BT overlay texts %v missing %q", texts, want)
		}
	}

	// Switch to state machine snapshot.
	app.Post(func() {
		dbg.SetAI(debug.StateMachineSnapshot("boss", fsm))
	})
	app.Settle()

	if !app.Exists("debug-ai-machine") || app.Exists("debug-ai-tree") {
		t.Fatal("expected debug-ai-machine without debug-ai-tree after switching to machine snapshot")
	}
	texts = app.Texts()
	for _, want := range []string{"AI · boss", "* state chase", "prev: patrol", "History (1)", "step 1: patrol -> chase (spot)"} {
		if !slices.Contains(texts, want) {
			t.Fatalf("FSM overlay texts %v missing %q", texts, want)
		}
	}

	// ClearAI hides the section.
	app.Post(dbg.ClearAI)
	app.Settle()
	if app.Exists("debug-ai-inspector") {
		t.Fatal("debug-ai-inspector still mounted after ClearAI")
	}
}

func TestOverlayAIInspectorPullProviderUpdatesOnFrame(t *testing.T) {
	fsm := flow.NewMachine("idle")
	goAlert := false
	fsm.AddNamedTransition("noise", "idle", "alert", func() bool { return goAlert })
	fsm.Update(16 * time.Millisecond)

	dbg := debug.New(debug.Options{
		Visible: true,
		AI: func() debug.AISnapshot {
			return debug.StateMachineSnapshot("sentry", fsm)
		},
	})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(400)},
			Children: []core.Node{dbg.View(cx)},
		}
	})

	if !slices.Contains(app.Texts(), "* state idle") {
		t.Fatalf("initial pull texts %v missing '* state idle'", app.Texts())
	}

	app.Post(func() {
		goAlert = true
		fsm.Update(16 * time.Millisecond)
		dbg.RecordFrame(16*time.Millisecond, 2*time.Millisecond, 2*time.Millisecond)
	})
	app.Settle()

	texts := app.Texts()
	if !slices.Contains(texts, "* state alert") || !slices.Contains(texts, "step 2: idle -> alert (noise)") {
		t.Fatalf("updated pull texts %v missing alert transition", texts)
	}

	snap := dbg.AISnapshot()
	if snap.Agent != "sentry" || snap.Machine == nil || snap.Machine.Current != "alert" {
		t.Fatalf("AISnapshot() = %+v, want sentry in alert", snap)
	}
}

func TestDrawSteeringAndPrimitivesRespectZoomAndVisibility(t *testing.T) {
	agent := ai.Agent{
		Pos:      core.Point{X: 100, Y: 80},
		Vel:      core.Point{X: 40, Y: 0},
		MaxSpeed: 60,
		MaxForce: 120,
		Radius:   12,
	}
	info := debug.SteeringDebug{
		Agent:           agent,
		DesiredVelocity: core.Point{X: 50, Y: -20},
		Target:          core.Point{X: 200, Y: 40},
		HasTarget:       true,
		Path: []core.Point{
			{X: 100, Y: 80},
			{X: 150, Y: 60},
			{X: 200, Y: 40},
		},
		WaypointIndex: 1,
		VelocityScale: 0.5,
	}

	for _, zoom := range []float32{0.5, 1.0, 2.5} {
		dbg := debug.New(debug.Options{Visible: true, LineWidth: 2})
		d := &core.Drawing{}
		d.Scale(zoom, zoom)
		dbg.DrawSteering(d, info)

		wantLW := float32(2) / zoom
		var saves, restores, arcs, lines, lineWidths int
		for _, op := range d.Ops {
			switch op.Kind {
			case core.DrawSave:
				saves++
			case core.DrawRestore:
				restores++
			case core.DrawArc:
				arcs++
			case core.DrawLineTo:
				lines++
			case core.DrawLineWidth:
				lineWidths++
				if math.Abs(float64(op.W-wantLW)) > 1e-4 {
					t.Fatalf("zoom %v: line width = %v, want %v", zoom, op.W, wantLW)
				}
			}
		}
		if saves == 0 || saves != restores {
			t.Fatalf("zoom %v: unbalanced save/restore (%d vs %d)", zoom, saves, restores)
		}
		// Path dots (3) + active waypoint ring (1) + target reticle (1) + agent radius (1) = 6 arcs.
		if arcs != 6 {
			t.Fatalf("zoom %v: drew %d arcs, want 6", zoom, arcs)
		}
		// Path polyline (2) + active waypoint line (1) + target sightline (1) + target crosshair (2)
		// + desired arrow (3) + velocity arrow (3) = 12 LineTo ops.
		if lines != 12 {
			t.Fatalf("zoom %v: drew %d LineTo ops, want 12", zoom, lines)
		}
		if lineWidths == 0 {
			t.Fatalf("zoom %v: no line widths set", zoom)
		}

		// Hidden overlay emits zero draw ops.
		dbg.SetVisible(false)
		hidden := &core.Drawing{}
		dbg.DrawSteering(hidden, info)
		dbg.DrawVector(hidden, agent.Pos, agent.Vel, core.RGB(0x22C55E))
		dbg.DrawPath(hidden, info.Path, core.RGB(0xA78BFA))
		dbg.DrawTarget(hidden, info.Target, core.RGB(0xF59E0B))
		if len(hidden.Ops) != 0 {
			t.Fatalf("hidden overlay emitted %d ops, want 0", len(hidden.Ops))
		}
	}

	// Ensure FormatBTTrace / FormatMachineInspection lines stay readable in debug overlay texts.
	if !strings.Contains(ai.FormatBTTrace([]ai.BTNodeTrace{{Kind: "action", Name: "go", LastStatus: ai.StatusRunning, Ticked: true, Active: true}}), `* action "go" = Running`) {
		t.Fatal("unexpected FormatBTTrace output")
	}
}
