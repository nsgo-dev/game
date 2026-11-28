package battle

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/random"
)

var (
	hit   = Action{ID: "hit", Scope: OneEnemy, Power: 5}
	sweep = Action{ID: "sweep", Scope: AllEnemies, Power: 1}
)

func fighters(fs ...Fighter) []Fighter { return fs }

func TestOneEnemyDamageAndWin(t *testing.T) {
	r := New(fighters(Fighter{ID: "a", HP: 10, Attack: 8, Agility: 4}), fighters(Fighter{ID: "e", HP: 10, MaxHP: 10, Defense: 2}), Rules{}, random.New(7))
	events := r.Resolve([]Command{{Actor: Ref{PartySide, 0}, Action: hit, Target: 0}})
	want := []Event{
		{Kind: Damaged, Actor: Ref{PartySide, 0}, Target: Ref{TroopSide, 0}, Amount: 10},
		{Kind: Defeated, Actor: Ref{PartySide, 0}, Target: Ref{TroopSide, 0}},
		{Kind: Won},
	}
	if !reflect.DeepEqual(events, want) || !r.Won || r.Troop[0].HP != 0 || r.Turn != 1 {
		t.Fatalf("events=%+v runner=%+v", events, r)
	}
	if got := r.Resolve([]Command{{Actor: Ref{PartySide, 0}, Action: hit}}); got != nil || r.Turn != 1 {
		t.Fatalf("Resolve after the battle ended = %+v, turn %d", got, r.Turn)
	}
}

func TestDamageIsAtLeastOne(t *testing.T) {
	r := New(fighters(Fighter{HP: 1}), fighters(Fighter{HP: 5, Defense: 99}), Rules{}, nil)
	events := r.Resolve([]Command{{Actor: Ref{PartySide, 0}, Action: Action{Scope: OneEnemy}}})
	if len(events) != 1 || events[0].Amount != 1 || r.Troop[0].HP != 4 {
		t.Fatalf("events=%+v troop=%+v", events, r.Troop)
	}
}

func TestOrderByAgilityThenSideThenIndex(t *testing.T) {
	r := New(
		fighters(Fighter{ID: "p0", HP: 1, Agility: 3}, Fighter{ID: "p1", HP: 1, Agility: 5}, Fighter{ID: "p2", HP: 1, Agility: 3}),
		fighters(Fighter{ID: "t0", HP: 1, Agility: 3}, Fighter{ID: "t1", HP: 1, Agility: 9}),
		Rules{}, nil)
	cmds := []Command{
		{Actor: Ref{TroopSide, 0}}, {Actor: Ref{PartySide, 2}}, {Actor: Ref{PartySide, 0}},
		{Actor: Ref{TroopSide, 1}}, {Actor: Ref{PartySide, 1}},
		{Actor: Ref{PartySide, 7}}, {Actor: Ref{TroopSide, -1}}, {Actor: Ref{Side(5), 0}},
	}
	var got []Ref
	for _, c := range r.Order(cmds) {
		got = append(got, c.Actor)
	}
	want := []Ref{{TroopSide, 1}, {PartySide, 1}, {PartySide, 0}, {PartySide, 2}, {TroopSide, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestInvalidAndDefeatedActorsAreSkipped(t *testing.T) {
	r := New(fighters(Fighter{HP: 10, Attack: 20, Agility: 9}), fighters(Fighter{HP: 5, Attack: 20}, Fighter{HP: 5}), Rules{}, nil)
	events := r.Resolve([]Command{
		{Actor: Ref{PartySide, 3}, Action: hit},
		{Actor: Ref{TroopSide, 0}, Action: hit, Target: 0},
		{Actor: Ref{PartySide, 0}, Action: hit, Target: 0},
	})
	// The party member is faster and defeats troop 0 before it acts.
	if len(events) != 2 || events[1].Kind != Defeated || r.Party[0].HP != 10 {
		t.Fatalf("events=%+v party=%+v", events, r.Party)
	}
}

func TestTroopCanDefeatTheParty(t *testing.T) {
	r := New(fighters(Fighter{HP: 3}), fighters(Fighter{HP: 9, Attack: 4}), Rules{}, nil)
	events := r.Resolve([]Command{{Actor: Ref{TroopSide, 0}, Action: Action{Scope: OneEnemy}, Target: 0}})
	if !r.Lost || r.Won || events[len(events)-1].Kind != Lost || r.Party[0].Alive {
		t.Fatalf("events=%+v runner=%+v", events, r)
	}
}

func TestAllEnemiesScope(t *testing.T) {
	r := New(fighters(Fighter{HP: 1, Attack: 10}), fighters(Fighter{HP: 5}, Fighter{HP: 0}, Fighter{HP: 20}), Rules{}, nil)
	events := r.Resolve([]Command{{Actor: Ref{PartySide, 0}, Action: sweep}})
	var hitTargets []Ref
	for _, e := range events {
		if e.Kind == Damaged {
			hitTargets = append(hitTargets, e.Target)
		}
	}
	if want := []Ref{{TroopSide, 0}, {TroopSide, 2}}; !reflect.DeepEqual(hitTargets, want) || r.Won || r.Troop[2].HP != 9 {
		t.Fatalf("events=%+v troop=%+v", events, r.Troop)
	}
}

func TestFriendlyScopesHealWithinMaxHP(t *testing.T) {
	newRunner := func() *Runner {
		return New(fighters(Fighter{HP: 2, MaxHP: 10}, Fighter{HP: 9, MaxHP: 10}, Fighter{HP: 0, MaxHP: 10}), fighters(Fighter{HP: 1}), Rules{}, nil)
	}
	cure := Action{Power: 4}
	cases := []struct {
		scope  Scope
		target int
		hp     []int
	}{
		{Self, 1, []int{6, 9, 0}},
		{OneAlly, 1, []int{2, 10, 0}},
		{OneAlly, 2, []int{2, 9, 0}},
		{AllAllies, 0, []int{6, 10, 0}},
	}
	for _, tc := range cases {
		r := newRunner()
		cure.Scope = tc.scope
		r.Resolve([]Command{{Actor: Ref{PartySide, 0}, Action: cure, Target: tc.target}})
		var hp []int
		for _, f := range r.Party {
			hp = append(hp, f.HP)
		}
		if !reflect.DeepEqual(hp, tc.hp) || r.Troop[0].HP != 1 {
			t.Errorf("%s target %d: party HP %v, want %v; troop %+v", tc.scope, tc.target, hp, tc.hp, r.Troop)
		}
	}
}

func TestCostSpendsMP(t *testing.T) {
	r := New(fighters(Fighter{HP: 1, MP: 5}), fighters(Fighter{HP: 99}), Rules{}, nil)
	fire := Action{Scope: OneEnemy, Power: 3, Cost: 3}
	cmd := []Command{{Actor: Ref{PartySide, 0}, Action: fire}}
	if e := r.Resolve(cmd); len(e) != 1 || e[0].Kind != Damaged || r.Party[0].MP != 2 {
		t.Fatalf("first turn: events=%+v party=%+v", e, r.Party)
	}
	if e := r.Resolve(cmd); len(e) != 1 || e[0].Kind != NoMP || r.Party[0].MP != 2 || r.Troop[0].HP != 96 {
		t.Fatalf("second turn: events=%+v runner=%+v", e, r)
	}
}

// fight runs turns of mutual attacks with misses and criticals and returns every event.
func fight(r *Runner, turns int) [][]Event {
	jab := Action{Scope: OneEnemy, Power: 2, Accuracy: 0.6}
	var out [][]Event
	for range turns {
		out = append(out, r.Resolve([]Command{
			{Actor: Ref{PartySide, 0}, Action: jab},
			{Actor: Ref{PartySide, 1}, Action: jab, Target: 1},
			{Actor: Ref{TroopSide, 0}, Action: jab, Target: r.Turn % 2},
			{Actor: Ref{TroopSide, 1}, Action: jab},
		}))
	}
	return out
}

func newFight(seed uint64) *Runner {
	return New(
		fighters(Fighter{ID: "p0", HP: 40, Attack: 3, Defense: 1, Agility: 5}, Fighter{ID: "p1", HP: 30, Attack: 4, Agility: 7}),
		fighters(Fighter{ID: "t0", HP: 35, Attack: 3, Defense: 1, Agility: 6}, Fighter{ID: "t1", HP: 35, Attack: 3, Agility: 2}),
		Rules{CriticalChance: 0.25}, random.New(seed))
}

func TestReplayFromSeed(t *testing.T) {
	a, b := fight(newFight(42), 6), fight(newFight(42), 6)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed gave different events:\n%+v\n%+v", a, b)
	}
	var missed, crit bool
	for _, turn := range a {
		for _, e := range turn {
			missed = missed || e.Kind == Missed
			crit = crit || e.Critical
		}
	}
	if !missed || !crit {
		t.Fatalf("seed 42 rolled no miss (%v) or no critical (%v); pick a seed that exercises both", missed, crit)
	}
	if reflect.DeepEqual(a, fight(newFight(43), 6)) {
		t.Fatal("different seeds gave identical events")
	}
}

func TestSnapshotRestoreReplaysTheRest(t *testing.T) {
	r := newFight(9)
	fight(r, 2)
	snap := r.Snapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	rest := fight(r, 3)
	end := r.Snapshot()

	var loaded Snapshot
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	other := New(nil, nil, Rules{CriticalChance: 0.25}, random.New(1))
	if err := other.Restore(loaded); err != nil {
		t.Fatal(err)
	}
	if got := fight(other, 3); !reflect.DeepEqual(got, rest) {
		t.Fatalf("restored battle diverged:\n%+v\n%+v", got, rest)
	}
	if !reflect.DeepEqual(other.Snapshot(), end) {
		t.Fatalf("restored state %+v, want %+v", other.Snapshot(), end)
	}

	// Restore copies: mutating the snapshot afterwards does not reach the runner.
	loaded.Party[0].HP = -1
	if other.Party[0].HP == -1 {
		t.Fatal("Restore aliased the snapshot's fighters")
	}
	var nilRunner *Runner
	if err := nilRunner.Restore(snap); err == nil {
		t.Fatal("Restore on a nil Runner returned no error")
	}
}
