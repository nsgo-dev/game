package flow_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/flow"
	"go.yaml.in/yaml/v3"
)

func TestMachineSnapshotRestoreYAMLAndJSONParity(t *testing.T) {
	type aiState string
	const (
		patrol aiState = "patrol"
		chase  aiState = "chase"
		search aiState = "search"
		dead   aiState = "dead"
	)

	type sim struct {
		m    *flow.Machine[aiState]
		log  []string
		dist float64
		hp   int
	}

	build := func() *sim {
		s := &sim{dist: 100, hp: 10}
		m := flow.NewMachine(patrol)
		m.AddState(patrol, flow.State{
			Enter:  func() { s.log = append(s.log, "enter:patrol") },
			Update: func(dt time.Duration) { s.log = append(s.log, "update:patrol") },
			Exit:   func() { s.log = append(s.log, "exit:patrol") },
		})
		m.AddState(chase, flow.State{
			Enter:  func() { s.log = append(s.log, "enter:chase") },
			Update: func(dt time.Duration) { s.log = append(s.log, "update:chase") },
			Exit:   func() { s.log = append(s.log, "exit:chase") },
		})
		m.AddState(search, flow.State{
			Enter:  func() { s.log = append(s.log, "enter:search") },
			Update: func(dt time.Duration) { s.log = append(s.log, "update:search") },
			Exit:   func() { s.log = append(s.log, "exit:search") },
		})
		m.AddState(dead, flow.State{
			Enter:  func() { s.log = append(s.log, "enter:dead") },
			Update: func(dt time.Duration) { s.log = append(s.log, "update:dead") },
		})
		m.AddAnyTransition(dead, func() bool { return s.hp <= 0 })
		m.AddTransition(patrol, chase, func() bool { return s.dist < 50 })
		m.AddTransition(chase, search, func() bool { return s.dist > 80 })
		m.AddTransition(search, patrol, func() bool { return m.TimeInState() >= 60*time.Millisecond })
		s.m = m
		return s
	}

	dt := 20 * time.Millisecond
	orig := build()

	// Advance into search with 40ms elapsed (one step away from returning to patrol).
	orig.m.Update(dt) // step 1: enter patrol, update patrol (20ms)
	orig.dist = 30
	orig.m.Update(dt) // step 2: patrol -> chase (20ms)
	orig.dist = 90
	orig.m.Update(dt) // step 3: chase -> search (20ms)
	orig.m.Update(dt) // step 4: stay in search (40ms)

	if orig.m.Current() != search || orig.m.Previous() != chase || orig.m.TimeInState() != 40*time.Millisecond || !orig.m.Started() {
		t.Fatalf("unexpected pre-snapshot state: cur=%v prev=%v time=%v started=%v",
			orig.m.Current(), orig.m.Previous(), orig.m.TimeInState(), orig.m.Started())
	}

	snap := orig.m.Snapshot()

	// YAML byte-for-byte round-trip.
	y1, err := yaml.Marshal(snap)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}
	var fromYAML flow.MachineSnapshot[aiState]
	if err := yaml.Unmarshal(y1, &fromYAML); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}
	y2, err := yaml.Marshal(fromYAML)
	if err != nil {
		t.Fatalf("second yaml.Marshal failed: %v", err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("YAML round-trip changed bytes:\n--- first ---\n%s\n--- second ---\n%s", y1, y2)
	}

	// JSON round-trip.
	jBytes, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var fromJSON flow.MachineSnapshot[aiState]
	if err := json.Unmarshal(jBytes, &fromJSON); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Restore into a fresh unstarted machine (from YAML) and an already-started machine (from JSON).
	restoredYAML := build()
	restoredYAML.dist = orig.dist
	restoredYAML.hp = orig.hp
	if err := restoredYAML.m.Restore(fromYAML); err != nil {
		t.Fatalf("Restore(fromYAML) failed: %v", err)
	}
	if len(restoredYAML.log) != 0 {
		t.Fatalf("Restore(fromYAML) invoked callbacks: %v", restoredYAML.log)
	}

	restoredJSON := build()
	restoredJSON.m.Start()
	restoredJSON.m.Update(dt)
	restoredJSON.log = nil
	restoredJSON.dist = orig.dist
	restoredJSON.hp = orig.hp
	if err := restoredJSON.m.Restore(fromJSON); err != nil {
		t.Fatalf("Restore(fromJSON) failed: %v", err)
	}
	if len(restoredJSON.log) != 0 {
		t.Fatalf("Restore(fromJSON) invoked Enter/Exit callbacks: %v", restoredJSON.log)
	}

	// Clear orig log from pre-snapshot steps and step all three machines identically.
	orig.log = nil

	for step := 5; step <= 12; step++ {
		for _, s := range []*sim{orig, restoredYAML, restoredJSON} {
			switch step {
			case 8:
				s.dist = 20 // patrol -> chase
			case 10:
				s.hp = 0 // any -> dead
			}
			s.m.Update(dt)
		}
		for name, got := range map[string]*sim{"yaml": restoredYAML, "json": restoredJSON} {
			if got.m.Current() != orig.m.Current() ||
				got.m.Previous() != orig.m.Previous() ||
				got.m.TimeInState() != orig.m.TimeInState() ||
				got.m.Started() != orig.m.Started() {
				t.Fatalf("step %d (%s): got cur=%v prev=%v time=%v started=%v, want cur=%v prev=%v time=%v started=%v",
					step, name,
					got.m.Current(), got.m.Previous(), got.m.TimeInState(), got.m.Started(),
					orig.m.Current(), orig.m.Previous(), orig.m.TimeInState(), orig.m.Started())
			}
		}
	}

	if !slices.Equal(restoredYAML.log, orig.log) {
		t.Fatalf("YAML restored callback log mismatch:\ngot:  %v\nwant: %v", restoredYAML.log, orig.log)
	}
	if !slices.Equal(restoredJSON.log, orig.log) {
		t.Fatalf("JSON restored callback log mismatch:\ngot:  %v\nwant: %v", restoredJSON.log, orig.log)
	}
}

func TestMachineSnapshotUnstartedAndZeroEnumState(t *testing.T) {
	type heroState int
	const (
		stateIdle heroState = iota
		stateRun
	)

	// Unstarted machine snapshot restores with Started=false so first Update still calls Enter.
	origEnters := 0
	orig := flow.NewMachine(stateIdle)
	orig.AddState(stateIdle, flow.State{Enter: func() { origEnters++ }})
	orig.AddTransition(stateIdle, stateRun, func() bool { return false })

	snap := orig.Snapshot()
	data, err := yaml.Marshal(snap)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}
	var decoded flow.MachineSnapshot[heroState]
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	cloneEnters := 0
	clone := flow.NewMachine(stateIdle)
	clone.AddState(stateIdle, flow.State{Enter: func() { cloneEnters++ }})
	clone.AddTransition(stateIdle, stateRun, func() bool { return false })
	clone.Start()
	cloneEnters = 0

	if err := clone.Restore(decoded); err != nil {
		t.Fatalf("Restore unstarted failed: %v", err)
	}
	if clone.Started() || cloneEnters != 0 {
		t.Fatalf("after Restore(unstarted): started=%v enters=%d", clone.Started(), cloneEnters)
	}

	orig.Update(16 * time.Millisecond)
	clone.Update(16 * time.Millisecond)
	if origEnters != 1 || cloneEnters != 1 || clone.TimeInState() != orig.TimeInState() {
		t.Fatalf("after first Update: origEnters=%d cloneEnters=%d origTime=%v cloneTime=%v",
			origEnters, cloneEnters, orig.TimeInState(), clone.TimeInState())
	}

	// Transition from zero-value enum (stateIdle=0) to transition-only state (stateRun=1).
	run := true
	m1 := flow.NewMachine(stateIdle)
	m1.AddTransition(stateIdle, stateRun, func() bool { return run })
	m1.Update(16 * time.Millisecond)
	if m1.Current() != stateRun || m1.Previous() != stateIdle {
		t.Fatalf("m1 state = (%v, prev %v), want (run, idle)", m1.Current(), m1.Previous())
	}

	m2 := flow.NewMachine(stateIdle)
	m2.AddTransition(stateIdle, stateRun, func() bool { return run })
	if err := m2.Restore(m1.Snapshot()); err != nil {
		t.Fatalf("Restore with transition-only target and zero enum previous failed: %v", err)
	}
	if m2.Current() != stateRun || m2.Previous() != stateIdle || m2.TimeInState() != 16*time.Millisecond {
		t.Fatalf("m2 restored = (%v, prev %v, time %v)", m2.Current(), m2.Previous(), m2.TimeInState())
	}
}

func TestMachineRestoreValidationAndAtomicity(t *testing.T) {
	exitCalls := 0
	enterCalls := 0
	m := flow.NewMachine("idle")
	m.AddState("idle", flow.State{
		Enter: func() { enterCalls++ },
		Exit:  func() { exitCalls++ },
	})
	m.AddState("run", flow.State{
		Enter: func() { enterCalls++ },
		Exit:  func() { exitCalls++ },
	})
	m.AddTransition("idle", "run", func() bool { return true })
	m.Update(25 * time.Millisecond)
	enterCalls, exitCalls = 0, 0

	// Snapshot from a different machine shape (unknown state "fly").
	other := flow.NewMachine("fly")
	other.AddState("fly", flow.State{})
	other.Update(10 * time.Millisecond)
	badCurrent := other.Snapshot()

	cases := []struct {
		name    string
		snap    flow.MachineSnapshot[string]
		wantSub string
	}{
		{
			name:    "unknown current state",
			snap:    badCurrent,
			wantSub: "fly",
		},
		{
			name: "unknown previous state",
			snap: flow.MachineSnapshot[string]{
				Version:  flow.MachineSnapshotVersion,
				Current:  "idle",
				Previous: "jump",
				Started:  true,
			},
			wantSub: "jump",
		},
		{
			name: "unsupported version",
			snap: flow.MachineSnapshot[string]{
				Version: 99,
				Current: "idle",
				Started: true,
			},
			wantSub: "version",
		},
		{
			name: "negative timeInState",
			snap: flow.MachineSnapshot[string]{
				Version:     flow.MachineSnapshotVersion,
				Current:     "idle",
				TimeInState: -time.Millisecond,
				Started:     true,
			},
			wantSub: "negative",
		},
		{
			name: "unstarted with timeInState",
			snap: flow.MachineSnapshot[string]{
				Version:     flow.MachineSnapshotVersion,
				Current:     "idle",
				TimeInState: 10 * time.Millisecond,
				Started:     false,
			},
			wantSub: "unstarted",
		},
		{
			name: "unstarted with previous state",
			snap: flow.MachineSnapshot[string]{
				Version:  flow.MachineSnapshotVersion,
				Current:  "idle",
				Previous: "run",
				Started:  false,
			},
			wantSub: "unstarted",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.Restore(tc.snap)
			if err == nil {
				t.Fatalf("Restore(%s) succeeded, want error", tc.name)
			}
			if !errors.Is(err, flow.ErrInvalidSnapshot) {
				t.Fatalf("err = %v, want wrapping ErrInvalidSnapshot", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %q, want substring %q", err.Error(), tc.wantSub)
			}
			// Machine state must remain completely untouched.
			if m.Current() != "run" || m.Previous() != "idle" || m.TimeInState() != 25*time.Millisecond || !m.Started() {
				t.Fatalf("machine mutated after failed Restore: cur=%q prev=%q time=%v started=%v",
					m.Current(), m.Previous(), m.TimeInState(), m.Started())
			}
			if enterCalls != 0 || exitCalls != 0 {
				t.Fatalf("failed Restore called hooks: enter=%d exit=%d", enterCalls, exitCalls)
			}
		})
	}

	// Nil receiver refuses cleanly without panicking.
	var nilMachine *flow.Machine[string]
	if err := nilMachine.Restore(flow.MachineSnapshot[string]{Version: flow.MachineSnapshotVersion}); !errors.Is(err, flow.ErrInvalidSnapshot) {
		t.Fatalf("nilMachine.Restore = %v, want ErrInvalidSnapshot", err)
	}
}
