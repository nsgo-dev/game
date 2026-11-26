package main

import (
	"testing"
	"time"
)

// testRun starts the first song's chart named id.
func testRun(t testing.TB, id string) *Run {
	t.Helper()
	song := &Songs.Songs[0]
	for i := range song.Charts {
		if song.Charts[i].ID == id {
			return NewRun(Songs.SongFile, song, Songs.Tracks[0], &song.Charts[i])
		}
	}
	t.Fatalf("no chart %q", id)
	return nil
}

// hitAll hits every note of r offset from its time, in chart order, advancing the run to each hit first.
func hitAll(t testing.TB, r *Run, offset time.Duration) {
	t.Helper()
	for i, n := range r.Chart().Notes {
		pos := r.NoteTime(i) + offset
		r.Advance(pos)
		if _, ok := r.Hit(n.Lane, pos); !ok {
			t.Fatalf("note %d was not hit", i)
		}
	}
	r.Advance(r.End())
}

func TestARunPlayedPerfectlyClearsWithTheTopScore(t *testing.T) {
	r := testRun(t, "hard")
	hitAll(t, r, 0)
	n := len(r.Chart().Notes)
	if r.Status != Cleared || r.Score() != MaxScore || r.MaxCombo != n || r.Counts[Perfect] != n {
		t.Fatalf("status %v, score %d, max combo %d, counts %v", r.Status, r.Score(), r.MaxCombo, r.Counts)
	}
	if r.Energy != MaxEnergy {
		t.Fatalf("energy %d, want the most, %d", r.Energy, MaxEnergy)
	}
	if Rank(r.Score()) != "S" {
		t.Fatalf("rank %s", Rank(r.Score()))
	}
}

func TestAHitIsGradedByHowCloseItLands(t *testing.T) {
	cases := []struct {
		offset time.Duration
		grade  Grade
	}{
		{0, Perfect},
		{45 * time.Millisecond, Perfect},
		{-45 * time.Millisecond, Perfect},
		{46 * time.Millisecond, Great},
		{-90 * time.Millisecond, Great},
		{91 * time.Millisecond, Good},
		{-135 * time.Millisecond, Good},
	}
	for _, c := range cases {
		r := testRun(t, "easy")
		n := r.Chart().Notes[0]
		j, ok := r.Hit(n.Lane, r.NoteTime(0)+c.offset)
		if !ok || j.Grade != c.grade || j.Offset != c.offset || j.Note != 0 {
			t.Errorf("a hit %v off: %+v, %v; want %v", c.offset, j, ok, c.grade)
		}
	}
}

func TestAHitNoNoteIsNearCostsNothing(t *testing.T) {
	r := testRun(t, "easy")
	first := r.Chart().Notes[0]
	if _, ok := r.Hit(first.Lane, r.NoteTime(0)-136*time.Millisecond); ok {
		t.Fatal("a hit before the good window was judged")
	}
	if _, ok := r.Hit((first.Lane+1)%Lanes, r.NoteTime(0)); ok {
		t.Fatal("a hit in a lane with no note near was judged")
	}
	if r.Combo != 0 || r.Energy != Songs.Energy.Start || r.Judged(0) {
		t.Fatalf("an empty hit changed the run: combo %d, energy %d", r.Combo, r.Energy)
	}
}

func TestAHitTooLateMissesTheNoteAndTakesTheNext(t *testing.T) {
	r := testRun(t, "easy")
	notes := r.Chart().Notes
	// Notes 0 and 7 share lane 0; a hit just before note 7 misses note 0 and lands on 7.
	if notes[0].Lane != notes[7].Lane {
		t.Fatalf("notes 0 and 7 are in lanes %d and %d", notes[0].Lane, notes[7].Lane)
	}
	for i := 1; i < 7; i++ {
		r.Hit(notes[i].Lane, r.NoteTime(i))
	}
	j, ok := r.Hit(notes[7].Lane, r.NoteTime(7)-10*time.Millisecond)
	if !ok || j.Note != 7 || j.Grade != Perfect {
		t.Fatalf("judged %+v, %v", j, ok)
	}
	if !r.Judged(0) || r.Counts[Miss] != 1 || r.Combo != 1 || r.MaxCombo != 6 {
		t.Fatalf("note 0 judged %v, counts %v, combo %d, max %d", r.Judged(0), r.Counts, r.Combo, r.MaxCombo)
	}
}

func TestMissesComeInTimeOrderAndRunTheEnergyOut(t *testing.T) {
	r := testRun(t, "hard")
	e := Songs.Energy
	needed := (e.Start + -e.Miss - 1) / -e.Miss
	missed := r.Advance(r.End())
	if r.Status != Failed || len(missed) != needed || r.Energy != 0 {
		t.Fatalf("status %v after %d misses, energy %d; want a fail after %d", r.Status, len(missed), r.Energy, needed)
	}
	for i := 1; i < len(missed); i++ {
		if r.NoteTime(missed[i].Note) < r.NoteTime(missed[i-1].Note) {
			t.Fatalf("miss %d comes before the one before it", i)
		}
	}
	if _, ok := r.Hit(0, r.End()); ok || r.Advance(r.End()+time.Hour) != nil {
		t.Fatal("a failed run went on")
	}
}

func TestARunClearsOnlyOnceTheSongEnds(t *testing.T) {
	r := testRun(t, "easy")
	notes := r.Chart().Notes
	for i, n := range notes {
		r.Hit(n.Lane, r.NoteTime(i)+60*time.Millisecond)
	}
	r.Advance(r.End() - time.Millisecond)
	if r.Status != Playing {
		t.Fatalf("status %v before the end", r.Status)
	}
	r.Advance(r.End())
	n := len(notes)
	if r.Status != Cleared || r.Counts[Great] != n || r.Score() != MaxScore*7/10 || Rank(r.Score()) != "D" {
		t.Fatalf("status %v, counts %v, score %d", r.Status, r.Counts, r.Score())
	}
}

func TestRank(t *testing.T) {
	for score, want := range map[int]string{MaxScore: "S", 950_000: "S", 949_999: "A", 900_000: "A", 850_000: "B", 800_000: "C", 799_999: "D", 0: "D"} {
		if got := Rank(score); got != want {
			t.Errorf("Rank(%d) = %s, want %s", score, got, want)
		}
	}
}

func TestUnwrapLoop(t *testing.T) {
	const length = 6400 * time.Millisecond
	for _, c := range []struct{ pos, expected, want time.Duration }{
		{time.Second, time.Second, time.Second},
		{100 * time.Millisecond, length + 80*time.Millisecond, length + 100*time.Millisecond},
		{length - 20*time.Millisecond, 2*length + 10*time.Millisecond, 2*length - 20*time.Millisecond},
		{0, -time.Second, 0},
	} {
		if got := unwrapLoop(c.pos, length, c.expected); got != c.want {
			t.Errorf("unwrapLoop(%v, %v) = %v, want %v", c.pos, c.expected, got, c.want)
		}
	}
	if got := unwrapLoop(time.Second, 0, time.Hour); got != time.Second {
		t.Errorf("a track of no known length moved to %v", got)
	}
}

// BenchmarkRun plays the hard chart through, hitting every note, as a step of the game judges them.
func BenchmarkRun(b *testing.B) {
	for b.Loop() {
		hitAll(b, testRun(b, "hard"), 20*time.Millisecond)
	}
}
