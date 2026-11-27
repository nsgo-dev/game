package framedata_test

import (
	"fmt"
	"testing"

	"github.com/nsgo-dev/game/pkg/framedata"
	"github.com/nsgo-dev/game/pkg/math2d"
)

// duel returns a World with fighter a at x=0 facing right and fighter b at bx facing left, both idle.
func duel(t testing.TB, bx float32) (*framedata.World, *framedata.Fighter, *framedata.Fighter) {
	t.Helper()
	doc := loadDoc(t)
	a := &framedata.Fighter{Moves: doc, Neutral: "idle"}
	b := &framedata.Fighter{Moves: doc, Neutral: "idle", X: bx, FacingLeft: true}
	return &framedata.World{Fighters: []*framedata.Fighter{a, b}}, a, b
}

func mustStart(t *testing.T, f *framedata.Fighter, name string) {
	t.Helper()
	if !f.Start(name) {
		t.Fatalf("Start(%q) refused on frame %d of %v (stun %d)", name, f.Frame(), moveName(f), f.Stun())
	}
}

func moveName(f *framedata.Fighter) string {
	if m := f.Move(); m != nil {
		return m.Name
	}
	return "<none>"
}

func TestNeutralStartsOnFirstStepAndLoops(t *testing.T) {
	w, a, _ := duel(t, 40)
	if a.Move() != nil || a.Frame() != 0 {
		t.Fatalf("fighter has move %v frame %d before its first step", moveName(a), a.Frame())
	}
	for step := 1; step <= 17; step++ {
		w.Step()
		want := (step-1)%8 + 1
		if moveName(a) != "idle" || a.Frame() != want {
			t.Fatalf("step %d: %s frame %d, want idle frame %d", step, moveName(a), a.Frame(), want)
		}
	}
}

func TestJabHitsOnItsFirstActiveFrameOnce(t *testing.T) {
	w, a, b := duel(t, 40)
	w.Step()
	mustStart(t, a, "jab")
	var log []framedata.Hit
	for step := 1; step <= 14; step++ {
		hits := w.Step()
		if a.Frame() != step || moveName(a) != "jab" {
			t.Fatalf("step %d: attacker on %s frame %d", step, moveName(a), a.Frame())
		}
		log = append(log, hits...)
		if step == 4 && len(hits) != 1 {
			t.Fatalf("step 4 found %d hits, want 1", len(hits))
		}
	}
	if len(log) != 1 {
		t.Fatalf("jab hit %d times over its active frames, want once", len(log))
	}
	h := log[0]
	want := framedata.Hit{Attacker: a, Victim: b, Move: a.Move(), Frame: 4, Box: 2, Damage: 30, Stun: 14, KnockbackX: 2.5, X: 34, Y: -60}
	if h != want {
		t.Fatalf("hit = %+v\nwant  %+v", h, want)
	}
	if hit, blocked := a.Contact(); !hit || blocked {
		t.Fatalf("attacker Contact() = %v, %v; want hit", hit, blocked)
	}
	// The jab ends after frame 14 and the attacker goes back to idle.
	w.Step()
	if moveName(a) != "idle" || a.Frame() != 1 {
		t.Fatalf("after the jab the attacker is on %s frame %d, want idle frame 1", moveName(a), a.Frame())
	}
}

func TestWhiffOutOfRange(t *testing.T) {
	w, a, _ := duel(t, 60)
	w.Step()
	mustStart(t, a, "jab")
	for range 14 {
		if hits := w.Step(); len(hits) != 0 {
			t.Fatalf("jab at range 60 hit: %+v", hits)
		}
	}
}

func TestHitstunMatchesFrameAdvantage(t *testing.T) {
	for _, block := range []bool{false, true} {
		t.Run(fmt.Sprintf("block=%v", block), func(t *testing.T) {
			w, a, b := duel(t, 40)
			b.Blocking = block
			w.Step()
			mustStart(t, a, "jab")
			hitStep := 0
			aFree, bFree := 0, 0
			for step := 1; step <= 40 && (aFree == 0 || bFree == 0); step++ {
				if hits := w.Step(); len(hits) > 0 {
					hitStep = step
					if hits[0].Blocked != block {
						t.Fatalf("hit Blocked = %v, want %v", hits[0].Blocked, block)
					}
				}
				if hitStep == 0 {
					continue
				}
				if aFree == 0 && a.Actionable() {
					aFree = step
				}
				if bFree == 0 && b.Actionable() {
					bFree = step
				}
			}
			jab, _ := a.Moves.Move("jab")
			fd, _ := jab.FrameData()
			want := fd.OnHit
			if block {
				want = fd.OnBlock
			}
			if got := bFree - aFree; got != want {
				t.Fatalf("attacker acts at step %d, victim at %d: advantage %d, FrameData says %d", aFree, bFree, got, want)
			}
		})
	}
}

func TestVictimCannotActDuringStun(t *testing.T) {
	w, a, b := duel(t, 40)
	w.Step()
	mustStart(t, a, "jab")
	for range 4 {
		w.Step()
	}
	if b.Stun() != 14 {
		t.Fatalf("victim stun = %d, want 14", b.Stun())
	}
	for remaining := 14; remaining > 0; remaining-- {
		if b.CanStart("jab") || b.Start("jab") {
			t.Fatalf("victim started a move with %d stun left", b.Stun())
		}
		w.Step()
	}
	mustStart(t, b, "jab")
}

func TestForceIgnoresStunAndRejectsUnknownMoves(t *testing.T) {
	w, a, b := duel(t, 40)
	w.Step()
	mustStart(t, a, "jab")
	for range 4 {
		w.Step()
	}
	if err := b.Force("fireball"); err != nil {
		t.Fatal(err)
	}
	if moveName(b) != "fireball" || b.Stun() != 14 {
		t.Fatalf("Force left %s with stun %d", moveName(b), b.Stun())
	}
	if err := b.Force("nope"); err == nil {
		t.Fatal("Force accepted an unknown move")
	}
	if b.Start("nope") || b.CanStart("nope") {
		t.Fatal("Start accepted an unknown move")
	}
}

func TestCancelWindowsWaitForContact(t *testing.T) {
	t.Run("whiff", func(t *testing.T) {
		w, a, _ := duel(t, 200)
		w.Step()
		mustStart(t, a, "jab")
		for step := 1; step <= 13; step++ {
			w.Step()
			if a.CanStart("fireball") {
				t.Fatalf("whiffed jab can cancel on frame %d", a.Frame())
			}
		}
	})
	for _, block := range []bool{false, true} {
		t.Run(fmt.Sprintf("block=%v", block), func(t *testing.T) {
			w, a, b := duel(t, 40)
			b.Blocking = block
			w.Step()
			mustStart(t, a, "jab")
			for step := 1; step <= 13; step++ {
				w.Step()
				want := step >= 4 && step <= 10
				if got := a.CanStart("fireball"); got != want {
					t.Fatalf("frame %d: CanStart(fireball) = %v, want %v", a.Frame(), got, want)
				}
				if a.CanStart("double") {
					t.Fatalf("frame %d: jab cancels into a move its window does not list", a.Frame())
				}
			}
			if hit, blocked := a.Contact(); hit == block || blocked != block {
				t.Fatalf("Contact() = %v, %v with block %v", hit, blocked, block)
			}
		})
	}
	t.Run("cancel", func(t *testing.T) {
		w, a, _ := duel(t, 40)
		w.Step()
		mustStart(t, a, "jab")
		for range 5 {
			w.Step()
		}
		mustStart(t, a, "fireball")
		if a.Frame() != 1 {
			t.Fatalf("cancelled into fireball on frame %d, want 1", a.Frame())
		}
		if hit, blocked := a.Contact(); hit || blocked {
			t.Fatal("a new move instance kept the old one's contact")
		}
		w.Step()
		if moveName(a) != "fireball" || a.Frame() != 1 {
			t.Fatalf("the step after the cancel shows %s frame %d, want fireball frame 1", moveName(a), a.Frame())
		}
	})
}

func TestMultiHitGroupsEachHitOnce(t *testing.T) {
	doc := loadDoc(t)
	// The attacker faces left this time, so its knockback points to -X.
	a := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 40, FacingLeft: true}
	b := &framedata.Fighter{Moves: doc, Neutral: "idle"}
	w := &framedata.World{Fighters: []*framedata.Fighter{a, b}}
	w.Step()
	mustStart(t, a, "double")
	var steps []int
	var hits []framedata.Hit
	for step := 1; step <= 20; step++ {
		for _, h := range w.Step() {
			steps = append(steps, step)
			hits = append(hits, h)
		}
	}
	if len(hits) != 2 || steps[0] != 5 || steps[1] != 9 {
		t.Fatalf("double hit on steps %v, want [5 9]", steps)
	}
	if hits[0].Group != 0 || hits[1].Group != 1 || hits[1].Damage != 25 || hits[1].Stun != 16 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[1].KnockbackX != -4 || hits[1].KnockbackY != -3 {
		t.Fatalf("knockback = (%v, %v), want (-4, -3) from an attacker facing left", hits[1].KnockbackX, hits[1].KnockbackY)
	}
	// The second hit replaced the first's remaining stun: 16 set on step 9, 11 steps later 5 are left.
	if b.Stun() != 5 {
		t.Fatalf("victim stun = %d, want 5", b.Stun())
	}
}

func TestSimultaneousHitsTrade(t *testing.T) {
	w, a, b := duel(t, 40)
	w.Step()
	mustStart(t, a, "jab")
	mustStart(t, b, "jab")
	for range 3 {
		if hits := w.Step(); len(hits) != 0 {
			t.Fatalf("hit before the active frames: %+v", hits)
		}
	}
	hits := w.Step()
	if len(hits) != 2 || hits[0].Attacker != a || hits[0].Victim != b || hits[1].Attacker != b || hits[1].Victim != a {
		t.Fatalf("trade = %+v, want a hits b then b hits a", hits)
	}
	if hits[1].KnockbackX != -2.5 {
		t.Fatalf("b's knockback = %v, want -2.5", hits[1].KnockbackX)
	}
	for _, f := range []*framedata.Fighter{a, b} {
		if moveName(f) != "idle" || f.Stun() != 14 {
			t.Fatalf("after the trade a fighter is on %s with stun %d", moveName(f), f.Stun())
		}
	}
}

func TestTeamsDoNotHitEachOther(t *testing.T) {
	w, a, b := duel(t, 40)
	a.Team, b.Team = 1, 1
	w.Step()
	mustStart(t, a, "jab")
	for range 14 {
		if hits := w.Step(); len(hits) != 0 {
			t.Fatalf("teammates hit: %+v", hits)
		}
	}
}

func TestFighterWithoutNeutralHasNoBoxes(t *testing.T) {
	doc := loadDoc(t)
	a := &framedata.Fighter{Moves: doc}
	w := &framedata.World{Fighters: []*framedata.Fighter{a, nil}}
	w.Step()
	if a.Move() != nil || len(a.Boxes(nil)) != 0 {
		t.Fatalf("a fighter without Neutral is on %s", moveName(a))
	}
	mustStart(t, a, "jab")
	for range 15 {
		w.Step()
	}
	if a.Move() != nil || a.Frame() != 0 {
		t.Fatalf("after the jab the fighter is on %s frame %d, want none", moveName(a), a.Frame())
	}
}

func TestBoxesMirrorWhenFacingLeft(t *testing.T) {
	w, a, b := duel(t, 100)
	if err := a.Force("jab"); err != nil {
		t.Fatal(err)
	}
	if err := b.Force("jab"); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if hits := w.Step(); len(hits) != 0 {
			t.Fatalf("jabs at range 100 hit: %+v", hits)
		}
	}
	got := a.Boxes(nil)
	want := []framedata.WorldBox{
		{Kind: framedata.KindHurt, Rect: math2d.Rect{X: -12, Y: -80, W: 24, H: 80}, Index: 0},
		{Kind: framedata.KindHurt, Rect: math2d.Rect{X: 12, Y: -66, W: 18, H: 12}, Index: 1},
		{Kind: framedata.KindHit, Rect: math2d.Rect{X: 12, Y: -66, W: 28, H: 12}, Index: 2},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("facing right boxes = %+v\nwant %+v", got, want)
	}
	got = b.Boxes(got[:0])
	want = []framedata.WorldBox{
		{Kind: framedata.KindHurt, Rect: math2d.Rect{X: 88, Y: -80, W: 24, H: 80}, Index: 0},
		{Kind: framedata.KindHurt, Rect: math2d.Rect{X: 70, Y: -66, W: 18, H: 12}, Index: 1},
		{Kind: framedata.KindHit, Rect: math2d.Rect{X: 60, Y: -66, W: 28, H: 12}, Index: 2},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("facing left boxes = %+v\nwant %+v", got, want)
	}
}

// match plays a scripted round between three fighters and returns every hit as text, for comparing runs.
func match(t testing.TB) []string {
	doc := loadDoc(t)
	fs := []*framedata.Fighter{
		{Moves: doc, Neutral: "idle", X: 0},
		{Moves: doc, Neutral: "idle", X: 36, FacingLeft: true},
		{Moves: doc, Neutral: "idle", X: 70, FacingLeft: true},
	}
	w := &framedata.World{Fighters: fs}
	script := []string{"jab", "double", "jab", "fireball"}
	var log []string
	for step := 1; step <= 600; step++ {
		for i, f := range fs {
			f.Blocking = (step/7+i)%3 == 0
			f.Start(script[(step*(i+3)/5)%len(script)])
			f.X += float32((step+i)%3 - 1)
		}
		for _, h := range w.Step() {
			log = append(log, fmt.Sprintf("%d:%v>%v %s f%d g%d b%v d%d s%d k%v@%v,%v",
				step, idx(fs, h.Attacker), idx(fs, h.Victim), h.Move.Name, h.Frame, h.Group, h.Blocked, h.Damage, h.Stun, h.KnockbackX, h.X, h.Y))
		}
	}
	return log
}

func idx(fs []*framedata.Fighter, f *framedata.Fighter) int {
	for i, g := range fs {
		if g == f {
			return i
		}
	}
	return -1
}

func TestFrameDataDrivesHitsDeterministically(t *testing.T) {
	first := match(t)
	if len(first) < 10 {
		t.Fatalf("the scripted match produced only %d hits: %v", len(first), first)
	}
	for run := range 3 {
		again := match(t)
		if fmt.Sprint(again) != fmt.Sprint(first) {
			t.Fatalf("run %d diverged:\n%v\n%v", run, first, again)
		}
	}
}

func TestStepDoesNotAllocate(t *testing.T) {
	w, a, b := duel(t, 40)
	for range 30 {
		w.Step()
	}
	step := 0
	allocs := testing.AllocsPerRun(200, func() {
		step++
		a.Start("jab")
		b.Blocking = step%2 == 0
		w.Step()
	})
	if allocs != 0 {
		t.Fatalf("Step allocates %v times per call", allocs)
	}
}

func BenchmarkStep(b *testing.B) {
	for _, n := range []int{2, 8} {
		b.Run(fmt.Sprintf("fighters=%d", n), func(b *testing.B) {
			doc := loadDoc(b)
			w := &framedata.World{}
			for i := range n {
				w.Fighters = append(w.Fighters, &framedata.Fighter{Moves: doc, Neutral: "idle", X: float32(i * 30), FacingLeft: i%2 == 1})
			}
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				for _, f := range w.Fighters {
					f.Start("jab")
				}
				w.Step()
			}
		})
	}
}
