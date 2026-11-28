package selfhost

import (
	"testing"
	"time"
)

func TestTournamentJoinAttemptsRoundsAndEnd(t *testing.T) {
	base := time.Unix(100, 0)
	called := 0
	t1 := NewTournament(TournamentConfig{Name: "cup", Start: base, Duration: time.Hour, MaxEntrants: 1, MaxAttempts: 2, JoinRequired: true})
	t1.OnRoundEnd = func(r TournamentRound, e []Entry) {
		called++
		if r.Number != 1 || len(e) != 1 || e[0].Account != "a" {
			t.Fatalf("end=%+v %v", r, e)
		}
	}
	if e := t1.Join("a", base); e != nil {
		t.Fatal(e)
	}
	if e := t1.Join("b", base); e != ErrTournamentFull {
		t.Fatal(e)
	}
	if e := t1.Submit("a", 4, base); e != nil {
		t.Fatal(e)
	}
	if e := t1.Submit("a", 5, base); e != nil {
		t.Fatal(e)
	}
	if e := t1.Submit("a", 6, base); e != ErrAttemptsExceeded {
		t.Fatal(e)
	}
	t1.EndRound(base.Add(time.Hour))
	if called != 1 {
		t.Fatal("callback count")
	}
}
