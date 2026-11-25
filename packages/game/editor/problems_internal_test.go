package editor

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
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
