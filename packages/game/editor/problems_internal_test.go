package editor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestCheckProblemsStaleRunDoesNotOverwriteNewerAndNoGoroutineLeak verifies
// generation sequencing: if an older background check finishes after a newer
// check has already published results, the stale result is discarded, and all
// background goroutines exit cleanly without leaking.
func TestCheckProblemsStaleRunDoesNotOverwriteNewerAndNoGoroutineLeak(t *testing.T) {
	dir := t.TempDir()
	gameYAML := "version: 1\nname: Race\nid: dev.example.race\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstExited := make(chan struct{})
	var calls atomic.Int32

	staleProb := []assetcheck.Problem{{File: "stale.scene.yaml", Line: 1, Message: "stale result"}}
	freshProb := []assetcheck.Problem{{File: "fresh.scene.yaml", Line: 7, Message: "fresh result"}}

	ed.problems.checkFn = func(_ string, _ *sceneformat.ComponentRegistry) ([]assetcheck.Problem, error) {
		n := calls.Add(1)
		if n == 1 {
			close(firstStarted)
			<-releaseFirst
			defer close(firstExited)
			return staleProb, nil
		}
		return freshProb, nil
	}

	// OpenProject triggers run #1, which blocks on releaseFirst.
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	<-firstStarted
	if !ed.CheckingProblems() {
		t.Fatal("expected CheckingProblems() to be true while run #1 is in flight")
	}

	// Trigger run #2 while run #1 is still blocked; run #2 finishes first.
	ed.CheckProblems()
	ed.WaitForProblems()
	if got := ed.Problems(); !slices.Equal(got, freshProb) {
		t.Fatalf("after run #2: Problems = %+v, want %+v", got, freshProb)
	}
	if ed.CheckingProblems() {
		t.Fatal("expected CheckingProblems() to be false after run #2 completed")
	}

	// Release run #1 and wait for its goroutine to exit; it must NOT overwrite run #2.
	close(releaseFirst)
	select {
	case <-firstExited:
	case <-time.After(5 * time.Second):
		t.Fatal("run #1 goroutine did not exit")
	}
	// Give the stale goroutine a moment to pass the generation check and return.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := ed.Problems(); !slices.Equal(got, freshProb) {
			t.Fatalf("stale run #1 overwrote run #2: got %+v, want %+v", got, freshProb)
		}
		time.Sleep(5 * time.Millisecond)
		break
	}
	if got := ed.Problems(); !slices.Equal(got, freshProb) {
		t.Fatalf("after stale run #1 finished: Problems = %+v, want %+v", got, freshProb)
	}

	// Burst rapid re-checks and verify zero goroutine leaks.
	ed.problems.checkFn = nil
	ed.WaitForProblems()
	runtime.GC()
	before := runtime.NumGoroutine()

	for range 25 {
		ed.CheckProblems()
	}
	ed.WaitForProblems()

	// Wait briefly for superseded goroutines from the burst to wind down.
	leakDeadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(leakDeadline) {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("possible goroutine leak: before=%d after=%d", before, after)
	}
}

// #1063 acceptance: Problems panel loading, error, empty, problem-row, and warning-row states.
func TestProblemsPanelLoadingErrorEmptyAndWarningStates(t *testing.T) {
	dir := t.TempDir()
	gameYAML := "version: 1\nname: States\nid: dev.example.states\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	gate := make(chan struct{})
	var gateClosed atomic.Bool
	closeGate := func() {
		if gateClosed.CompareAndSwap(false, true) {
			close(gate)
		}
	}
	t.Cleanup(closeGate)
	var mode atomic.Int32 // 0 = block then error, 1 = problem + warning, 2 = empty
	started := make(chan struct{})

	ed.problems.checkFn = func(_ string, _ *sceneformat.ComponentRegistry) ([]assetcheck.Problem, error) {
		switch mode.Load() {
		case 0:
			select {
			case <-started:
			default:
				close(started)
			}
			<-gate
			return nil, errors.New("permission denied scanning project")
		case 1:
			return []assetcheck.Problem{
				{File: "scenes/level.scene.yaml", Line: 12, Message: `frame "ghost" is not in atlas`},
				{File: "scenes/level.scene.yaml", Line: 20, Message: "warning: unused sprite override"},
			}, nil
		default:
			return nil, nil
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	<-started
	app.Settle()

	// 1. Loading state while check is blocked in flight.
	if !app.Exists("id:problems-loading") {
		t.Fatal("expected problems-loading while background check is running")
	}

	// 2. Error state once blocked check returns an error.
	closeGate()
	app.WaitUntil(5*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemErr() != nil
	})
	app.Settle()
	if !app.Exists("id:problems-error") || !app.Exists("id:problems-error-retry") {
		t.Fatal("expected problems-error banner and retry button when check fails")
	}

	// 3. Retry transitions to populated problem + warning rows.
	mode.Store(1)
	app.Focus("id:problems-error-retry")
	app.Key("Space")
	app.WaitUntil(5*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 2
	})
	app.Settle()

	if !app.Exists("id:problems-row-0") || !app.Exists("id:problems-row-1") {
		t.Fatal("expected both error and warning problem rows")
	}
	if !problemIsWarning(ed.Problems()[1]) || problemIsWarning(ed.Problems()[0]) {
		t.Fatalf("problemIsWarning classification wrong for %+v", ed.Problems())
	}
	if row1 := app.Find("id:problems-row-1"); !strings.Contains(row1.Props.Label, "Warning") {
		t.Fatalf("warning row aria-label = %q, want Warning prefix", row1.Props.Label)
	}

	// 4. Empty state once all problems are resolved.
	mode.Store(2)
	app.Press("id:problems-refresh")
	app.WaitUntil(5*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 0
	})
	app.Settle()
	if !app.Exists("id:problems-empty") {
		t.Fatal("expected problems-empty when zero problems remain")
	}
}
