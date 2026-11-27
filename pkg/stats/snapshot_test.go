package stats_test

import (
	"errors"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/stats"
)

func TestSheetSnapshotRestoreAndSavePart(t *testing.T) {
	s1, err := stats.NewSheet(
		stats.Def{Name: "hp", Base: 100, Min: stats.Float(0)},
		stats.Def{Name: "atk", Base: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.SetBase("hp", 120); err != nil {
		t.Fatal(err)
	}
	id1, err := s1.Add("equip", stats.Modifier{Stat: "atk", Op: stats.OpAdd, Value: 5})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s1.AddTimed("buff", stats.Modifier{Stat: "atk", Op: stats.OpMul, Value: 2}, 5)
	if err != nil {
		t.Fatal(err)
	}
	s1.Step()
	s1.Step()

	snap := s1.Snapshot()

	s2, err := stats.NewSheet(
		stats.Def{Name: "hp", Base: 100, Min: stats.Float(0)},
		stats.Def{Name: "atk", Base: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Restore(snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if s2.Base("hp") != 120 || s2.Value("atk") != 30 || s2.Steps() != 2 {
		t.Fatalf("restored hp base=%v atk=%v steps=%d; want 120, 30, 2", s2.Base("hp"), s2.Value("atk"), s2.Steps())
	}
	if rem, ok := s2.Remaining(id2); !ok || rem != 3 {
		t.Fatalf("Remaining(%d) = (%d, %v), want (3, true)", id2, rem, ok)
	}
	id3, err := s2.Add("new", stats.Modifier{Stat: "hp", Op: stats.OpAdd, Value: 10})
	if err != nil {
		t.Fatal(err)
	}
	if id3 <= id1 || id3 <= id2 {
		t.Fatalf("new modifier ID %d reused old IDs (%d, %d)", id3, id1, id2)
	}

	// Refused snapshot leaves sheet unchanged.
	bad := snap
	bad.Bases = []stats.BaseState{{Stat: "unknown", Base: 1}}
	if err := s2.Restore(bad); !errors.Is(err, stats.ErrInvalidSnapshot) {
		t.Fatalf("unknown stat err = %v, want ErrInvalidSnapshot", err)
	}
	badNaN := snap
	badNaN.Bases = []stats.BaseState{{Stat: "hp", Base: math.NaN()}}
	if err := s2.Restore(badNaN); !errors.Is(err, stats.ErrInvalidSnapshot) {
		t.Fatalf("NaN base err = %v, want ErrInvalidSnapshot", err)
	}
	if s2.Base("hp") != 120 || s2.Value("hp") != 130 {
		t.Fatal("failed Restore mutated sheet")
	}

	// SavePart round-trip and reset on older slot.
	store := save.NewFileStore(t.TempDir())
	g1 := save.NewGame()
	g1.Register("stats", 1, s1.SavePart())
	if err := g1.Save(store, "slot1"); err != nil {
		t.Fatal(err)
	}
	g2 := save.NewGame()
	g2.Register("stats", 1, s2.SavePart())
	if err := g2.Load(store, "slot1"); err != nil {
		t.Fatal(err)
	}
	if s2.Value("hp") != 120 || s2.Value("atk") != 30 {
		t.Fatalf("SavePart load gave hp=%v atk=%v", s2.Value("hp"), s2.Value("atk"))
	}

	// Loading an older slot without "stats" resets s2 back to initial bases and no modifiers.
	oldStore := save.NewFileStore(t.TempDir())
	var x = 1
	oldGame := save.NewGame()
	oldGame.Register("x", 1, save.ValuePart(&x))
	if err := oldGame.Save(oldStore, "old"); err != nil {
		t.Fatal(err)
	}
	var x2 int
	g3 := save.NewGame()
	g3.Register("x", 1, save.ValuePart(&x2))
	g3.Register("stats", 1, s2.SavePart())
	if err := g3.Load(oldStore, "old"); err != nil {
		t.Fatal(err)
	}
	if s2.Base("hp") != 100 || s2.Value("atk") != 10 || len(s2.Modifiers()) != 0 || s2.Steps() != 0 {
		t.Fatalf("ResetState failed: base hp=%v atk=%v mods=%d steps=%d", s2.Base("hp"), s2.Value("atk"), len(s2.Modifiers()), s2.Steps())
	}
}
