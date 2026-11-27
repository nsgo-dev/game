package framedata_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/framedata"
)

func body(length int) framedata.Box {
	return framedata.Box{Kind: framedata.KindHurt, Start: 1, End: length, X: -12, Y: -80, Width: 24, Height: 80}
}

func poke(start, end, group, hitstun int) framedata.Box {
	return framedata.Box{Kind: framedata.KindHit, Start: start, End: end, X: 12, Y: -66, Width: 28, Height: 12, Group: group, Damage: 10, Hitstun: hitstun, Blockstun: hitstun / 2, KnockbackX: 3}
}

// builtDoc is a Doc written in Go, with cancel windows on hit and on block, a looping attack and a heavy poke, validated like a parsed file.
func builtDoc(t testing.TB) *framedata.Doc {
	t.Helper()
	doc := &framedata.Doc{Version: framedata.CurrentVersion, Moves: []framedata.Move{
		{Name: "idle", Length: 8, Loop: true, Boxes: []framedata.Box{body(8)}},
		{Name: "onhit", Length: 14, Boxes: []framedata.Box{body(14), poke(4, 6, 0, 14)}, Cancels: []framedata.Cancel{{Start: 4, End: 10, When: framedata.OnHit, Into: []string{"finisher"}}}},
		{Name: "onblock", Length: 14, Boxes: []framedata.Box{body(14), poke(4, 6, 0, 14)}, Cancels: []framedata.Cancel{{Start: 4, End: 10, When: framedata.OnBlock, Into: []string{"finisher"}}}},
		{Name: "heavy", Length: 20, Boxes: []framedata.Box{body(20), poke(4, 6, 0, 20)}},
		{Name: "spin", Length: 6, Loop: true, Boxes: []framedata.Box{body(6), poke(2, 3, 0, 1)}},
		{Name: "finisher", Length: 10, Boxes: []framedata.Box{body(10)}},
	}}
	if err := framedata.Validate(doc); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return doc
}

func TestCancelWindowsOnHitAndOnBlock(t *testing.T) {
	for _, move := range []string{"onhit", "onblock"} {
		for _, block := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/block=%v", move, block), func(t *testing.T) {
				doc := builtDoc(t)
				a := &framedata.Fighter{Moves: doc, Neutral: "idle"}
				b := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 40, FacingLeft: true, Blocking: block}
				w := &framedata.World{Fighters: []*framedata.Fighter{a, b}}
				w.Step()
				mustStart(t, a, move)
				open := (move == "onhit") != block
				for step := 1; step <= 13; step++ {
					w.Step()
					want := open && step >= 4 && step <= 10
					if got := a.CanStart("finisher"); got != want {
						t.Fatalf("frame %d: CanStart(finisher) = %v, want %v", a.Frame(), got, want)
					}
				}
			})
		}
	}
}

func TestSeveralHitsInOneStepTakeTheLongestStun(t *testing.T) {
	for _, heavyFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("heavyFirst=%v", heavyFirst), func(t *testing.T) {
			doc := builtDoc(t)
			a := &framedata.Fighter{Moves: doc, Neutral: "idle"}
			b := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 40}
			c := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 80, FacingLeft: true}
			fs := []*framedata.Fighter{a, b, c}
			if heavyFirst {
				fs = []*framedata.Fighter{c, b, a}
			}
			w := &framedata.World{Fighters: fs}
			if err := a.Force("onhit"); err != nil {
				t.Fatal(err)
			}
			if err := c.Force("heavy"); err != nil {
				t.Fatal(err)
			}
			for step := 1; step <= 4; step++ {
				hits := w.Step()
				if step < 4 {
					continue
				}
				if len(hits) != 2 || hits[0].Victim != b || hits[1].Victim != b {
					t.Fatalf("hits = %+v, want a and c both hitting b", hits)
				}
			}
			if b.Stun() != 20 {
				t.Fatalf("victim stun = %d, want the longer 20", b.Stun())
			}
		})
	}
}

func TestLoopingMoveHitsOncePerCycle(t *testing.T) {
	doc := builtDoc(t)
	a := &framedata.Fighter{Moves: doc, Neutral: "idle"}
	b := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 40, FacingLeft: true}
	w := &framedata.World{Fighters: []*framedata.Fighter{a, b}}
	if err := a.Force("spin"); err != nil {
		t.Fatal(err)
	}
	var steps []int
	for step := 1; step <= 18; step++ {
		for range w.Step() {
			steps = append(steps, step)
		}
	}
	if fmt.Sprint(steps) != "[2 8 14]" {
		t.Fatalf("spin hit on steps %v, want [2 8 14]", steps)
	}
}

func TestKnockbackPointsAwayFromTheAttacker(t *testing.T) {
	doc := builtDoc(t)
	// A cross-up: the attacker faces right, but its hitbox behind it reaches a victim on its left.
	doc.Moves = append(doc.Moves, framedata.Move{Name: "crossup", Length: 6, Boxes: []framedata.Box{
		body(6),
		{Kind: framedata.KindHit, Start: 2, End: 2, X: -40, Y: -66, Width: 30, Height: 12, Hitstun: 5, KnockbackX: 3},
	}})
	for _, tc := range []struct {
		name       string
		victimX    float32
		facingLeft bool
		want       float32
	}{
		{"victim left of a right-facing attacker", -20, false, -3},
		{"victim right of a left-facing attacker", 20, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &framedata.Fighter{Moves: doc, Neutral: "idle", FacingLeft: tc.facingLeft}
			b := &framedata.Fighter{Moves: doc, Neutral: "idle", X: tc.victimX}
			w := &framedata.World{Fighters: []*framedata.Fighter{a, b}}
			if err := a.Force("crossup"); err != nil {
				t.Fatal(err)
			}
			w.Step()
			hits := w.Step()
			if len(hits) != 1 || hits[0].Attacker != a {
				t.Fatalf("hits = %+v, want the cross-up", hits)
			}
			if hits[0].KnockbackX != tc.want {
				t.Fatalf("KnockbackX = %v, want %v", hits[0].KnockbackX, tc.want)
			}
		})
	}
}

func TestUnvalidatedGroupsNeverHit(t *testing.T) {
	for _, group := range []int{-1, framedata.MaxGroup + 1} {
		t.Run(fmt.Sprint(group), func(t *testing.T) {
			doc := builtDoc(t)
			doc.Moves = append(doc.Moves, framedata.Move{Name: "bad", Length: 8, Boxes: []framedata.Box{body(8), poke(2, 6, group, 5)}})
			a := &framedata.Fighter{Moves: doc, Neutral: "idle"}
			b := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 40, FacingLeft: true}
			w := &framedata.World{Fighters: []*framedata.Fighter{a, b}}
			if err := a.Force("bad"); err != nil {
				t.Fatal(err)
			}
			for range 8 {
				if hits := w.Step(); len(hits) != 0 {
					t.Fatalf("a hitbox in group %d hit: %+v", group, hits)
				}
			}
		})
	}
}

// replay steps w for steps steps from its current state, driving the fighters from the step number alone, and returns the hits as text.
func replay(w *framedata.World, from, steps int) []string {
	var log []string
	script := []string{"jab", "double", "jab", "fireball"}
	for step := from; step < from+steps; step++ {
		for i, f := range w.Fighters {
			f.Blocking = (step/7+i)%3 == 0
			f.Start(script[(step*(i+3)/5)%len(script)])
			f.X += float32((step+i)%3 - 1)
		}
		for _, h := range w.Step() {
			log = append(log, fmt.Sprintf("%d:%v>%v %s f%d g%d b%v s%d k%v", step, idx(w.Fighters, h.Attacker), idx(w.Fighters, h.Victim), h.Move.Name, h.Frame, h.Group, h.Blocked, h.Stun, h.KnockbackX))
		}
	}
	return log
}

func TestSaveAndRestoreReplayTheSameHits(t *testing.T) {
	doc := loadDoc(t)
	w := &framedata.World{Fighters: []*framedata.Fighter{
		{Moves: doc, Neutral: "idle", X: 0},
		{Moves: doc, Neutral: "idle", X: 36, FacingLeft: true},
		{Moves: doc, Neutral: "idle", X: 70, FacingLeft: true},
	}}
	replay(w, 1, 100)
	var saved framedata.WorldState
	w.Save(&saved)
	first := replay(w, 101, 200)
	if len(first) < 5 {
		t.Fatalf("the replayed steps produced only %d hits", len(first))
	}
	var end framedata.WorldState
	w.Save(&end)
	for run := range 3 {
		if err := w.Restore(&saved); err != nil {
			t.Fatal(err)
		}
		var again framedata.WorldState
		w.Save(&again)
		if !reflect.DeepEqual(again, saved) {
			t.Fatalf("run %d: restored state %+v\nwant %+v", run, again, saved)
		}
		if got := replay(w, 101, 200); fmt.Sprint(got) != fmt.Sprint(first) {
			t.Fatalf("run %d diverged after Restore:\n%v\n%v", run, first, got)
		}
		var after framedata.WorldState
		w.Save(&after)
		if !reflect.DeepEqual(after, end) {
			t.Fatalf("run %d ended in a different state", run)
		}
	}
}

func TestSaveSharesNoMemory(t *testing.T) {
	w, a, _ := duel(t, 40)
	w.Step()
	mustStart(t, a, "double")
	for range 5 {
		w.Step()
	}
	var s framedata.WorldState
	w.Save(&s)
	if len(s.Fighters[0].Landed) != 1 || s.Fighters[0].Landed[0] != (framedata.Landed{Victim: 1, Groups: 1}) {
		t.Fatalf("Landed = %+v, want group 0 on fighter 1", s.Fighters[0].Landed)
	}
	if err := w.Restore(&s); err != nil {
		t.Fatal(err)
	}
	// The second hit lands on the live fighter; the saved state must not see it.
	for range 4 {
		w.Step()
	}
	if s.Fighters[0].Landed[0].Groups != 1 {
		t.Fatalf("saved Landed changed to %+v after stepping", s.Fighters[0].Landed)
	}
	allocs := testing.AllocsPerRun(100, func() { w.Save(&s) })
	if allocs != 0 {
		t.Fatalf("Save allocates %v times once warm", allocs)
	}
}

func TestRestoreRefusesForeignStates(t *testing.T) {
	w, _, _ := duel(t, 40)
	w.Step()
	var good framedata.WorldState
	w.Save(&good)
	for _, tc := range []struct {
		name string
		edit func(s *framedata.WorldState)
	}{
		{"fighter count", func(s *framedata.WorldState) { s.Fighters = s.Fighters[:1] }},
		{"unknown move", func(s *framedata.WorldState) { s.Fighters[0].Move = "nope" }},
		{"frame past the move", func(s *framedata.WorldState) { s.Fighters[0].Frame = 9 }},
		{"frame without a move", func(s *framedata.WorldState) { s.Fighters[0].Move = "" }},
		{"negative stun", func(s *framedata.WorldState) { s.Fighters[1].Stun = -1 }},
		{"landed on itself", func(s *framedata.WorldState) {
			s.Fighters[0].Landed = []framedata.Landed{{Victim: 0, Groups: 1}}
		}},
		{"landed out of range", func(s *framedata.WorldState) {
			s.Fighters[1].Landed = []framedata.Landed{{Victim: 2, Groups: 1}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := framedata.WorldState{Fighters: append([]framedata.FighterState(nil), good.Fighters...)}
			s.Fighters[1].X = 99
			tc.edit(&s)
			if err := w.Restore(&s); err == nil {
				t.Fatal("Restore accepted the state")
			}
			if w.Fighters[1].X != 40 {
				t.Fatal("a refused Restore changed the world")
			}
		})
	}
}
