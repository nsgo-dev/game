package flow_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/statedump"
)

func TestMachineTransitionHistoryOrderStepsAndLabels(t *testing.T) {
	m := flow.NewMachine("idle")
	if m.HistoryLimit() != flow.DefaultHistoryLimit {
		t.Fatalf("default HistoryLimit() = %d, want %d", m.HistoryLimit(), flow.DefaultHistoryLimit)
	}
	if m.Step() != 0 {
		t.Fatalf("initial Step() = %d, want 0", m.Step())
	}

	var spotted, inRange, stunned, recovered bool
	m.AddNamedTransition("spot_player", "idle", "alert", func() bool { return spotted })
	m.AddTransition("alert", "chase", func() bool { return inRange })
	m.AddNamedAnyTransition("hit_stun", "stunned", func() bool { return stunned })
	m.AddAnyTransition("idle", func() bool { return recovered })

	// SetState before Start records step 0 with label "SetState".
	m.SetState("patrol")
	// SetState back to idle after Start still at step 0.
	m.SetState("idle")

	// Step 1: no transition.
	m.Update(16 * time.Millisecond)
	if m.Step() != 1 {
		t.Fatalf("Step() = %d, want 1", m.Step())
	}

	// Step 2: named transition idle -> alert.
	spotted = true
	m.Update(16 * time.Millisecond)

	// Step 3: unnamed transition alert -> chase (default "alert->chase").
	inRange = true
	m.Update(16 * time.Millisecond)

	// Step 4: named wildcard transition chase -> stunned.
	stunned = true
	m.Update(16 * time.Millisecond)

	// Step 5: unnamed wildcard transition stunned -> idle (default "*->idle").
	stunned = false
	recovered = true
	m.Update(16 * time.Millisecond)

	want := []flow.TransitionRecord[string]{
		{From: "idle", To: "patrol", Step: 0, Transition: "SetState"},
		{From: "patrol", To: "idle", Step: 0, Transition: "SetState"},
		{From: "idle", To: "alert", Step: 2, Transition: "spot_player"},
		{From: "alert", To: "chase", Step: 3, Transition: "alert->chase"},
		{From: "chase", To: "stunned", Step: 4, Transition: "hit_stun"},
		{From: "stunned", To: "idle", Step: 5, Transition: "*->idle"},
	}

	got := m.History()
	if !slices.Equal(got, want) {
		t.Fatalf("History() =\n%+v\nwant:\n%+v", got, want)
	}
}

func TestMachineHistoryRingBufferAndLimitChanges(t *testing.T) {
	m := flow.NewMachine(0)
	m.SetHistoryLimit(3)
	if m.HistoryLimit() != 3 {
		t.Fatalf("HistoryLimit() = %d, want 3", m.HistoryLimit())
	}

	for i := 0; i < 6; i++ {
		next := i + 1
		m.AddNamedTransition("advance", i, next, func() bool { return true })
	}

	for range 6 {
		m.Update(16 * time.Millisecond)
	}

	// Only the last 3 transitions (steps 4, 5, 6) are retained in chronological order.
	want := []flow.TransitionRecord[int]{
		{From: 3, To: 4, Step: 4, Transition: "advance"},
		{From: 4, To: 5, Step: 5, Transition: "advance"},
		{From: 5, To: 6, Step: 6, Transition: "advance"},
	}
	if got := m.History(); !slices.Equal(got, want) {
		t.Fatalf("wrapped History() = %+v, want %+v", got, want)
	}

	// Shrinking to 2 retains the newest 2 transitions in order.
	m.SetHistoryLimit(2)
	if got := m.History(); !slices.Equal(got, want[1:]) {
		t.Fatalf("shrunk History() = %+v, want %+v", got, want[1:])
	}

	// Growing to 5 preserves existing 2 entries and allows more.
	m.SetHistoryLimit(5)
	if got := m.History(); !slices.Equal(got, want[1:]) {
		t.Fatalf("grown History() = %+v, want %+v", got, want[1:])
	}
	m.SetState(0)
	if got := m.History(); len(got) != 3 || got[2].To != 0 || got[2].Transition != "SetState" {
		t.Fatalf("after SetState History() = %+v", got)
	}

	// ClearHistory discards entries but keeps HistoryLimit and Step.
	m.ClearHistory()
	if len(m.History()) != 0 || m.HistoryLimit() != 5 || m.Step() != 6 {
		t.Fatalf("after ClearHistory: len=%d limit=%d step=%d", len(m.History()), m.HistoryLimit(), m.Step())
	}

	// Setting limit to 0 disables history recording.
	m.SetHistoryLimit(0)
	m.SetState(1)
	if len(m.History()) != 0 || m.HistoryLimit() != 0 {
		t.Fatalf("disabled history recorded entries: len=%d limit=%d", len(m.History()), m.HistoryLimit())
	}
}

func TestMachineHistoryZeroAllocs(t *testing.T) {
	m := flow.NewMachine(0)
	m.SetHistoryLimit(8)
	m.AddNamedTransition("toggle", 0, 1, func() bool { return true })
	m.AddNamedTransition("toggle", 1, 0, func() bool { return true })

	dst := make([]flow.TransitionRecord[int], 0, 8)
	// Warm up ring buffer.
	for range 10 {
		m.Update(16 * time.Millisecond)
		dst = m.HistoryInto(dst)
	}

	allocs := testing.AllocsPerRun(200, func() {
		m.Update(16 * time.Millisecond)
		dst = m.HistoryInto(dst)
	})
	if allocs != 0 {
		t.Fatalf("Update + HistoryInto allocated %.2f times/run, want 0", allocs)
	}
}

func TestMachineInspectorSnapshotFormatAndStatedump(t *testing.T) {
	run := func(triggerChase bool) []byte {
		m := flow.NewMachine("patrol")
		m.AddNamedTransition("spot", "patrol", "alert", func() bool { return true })
		m.AddNamedTransition("engage", "alert", "chase", func() bool { return triggerChase })

		m.Update(16 * time.Millisecond)
		m.Update(16 * time.Millisecond)

		var insp flow.MachineInspector = m
		snap := insp.Inspect()
		if snap.Step != 2 {
			t.Fatalf("Inspect().Step = %d, want 2", snap.Step)
		}
		if insp.FormatState() != flow.FormatMachineInspection(snap) {
			t.Fatalf("FormatState() mismatch:\n%s\nvs\n%s", insp.FormatState(), flow.FormatMachineInspection(snap))
		}

		dump := &statedump.Dump{Version: statedump.FormatVersion, Seed: 7, Rate: 60, Steps: 2}
		if err := dump.SetValue("enemy_fsm", insp.FormatState()); err != nil {
			t.Fatalf("SetValue: %v", err)
		}
		data, err := statedump.Encode(dump)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if _, err := statedump.Parse(data); err != nil {
			t.Fatalf("Parse round-trip: %v", err)
		}
		return data
	}

	a1 := run(false)
	a2 := run(false)
	if diff := statedump.Diff(a1, a2); diff != "" {
		t.Fatalf("identical FSM runs differed:\n%s", diff)
	}

	b := run(true)
	diff := statedump.Diff(a1, b)
	if diff == "" {
		t.Fatal("expected non-empty statedump diff when FSM diverged")
	}
	if !strings.Contains(diff, "state=chase") || !strings.Contains(diff, "alert -> chase (engage)") {
		t.Fatalf("diff missing expected FSM transition lines:\n%s", diff)
	}
}
