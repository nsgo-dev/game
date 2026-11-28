package battle

import (
	"github.com/nsgo-dev/game/pkg/random"
	"testing"
)

func TestDeterministicOrderDamageAndSnapshot(t *testing.T) {
	r := New([]Fighter{{ID: "a", Attack: 8, Defense: 1, Agility: 4}}, []Fighter{{ID: "e", HP: 10, MaxHP: 10, Defense: 2, Alive: true}}, Rules{}, random.New(7))
	events := r.Resolve([]Command{{Actor: 0, Action: Action{ID: "hit", Scope: OneEnemy, Power: 5}, Target: 0}})
	if len(events) != 2 || events[0].Amount != 11 || !r.Won {
		t.Fatalf("events=%+v runner=%+v", events, r)
	}
	s := r.Snapshot()
	if err := r.Restore(s); err != nil {
		t.Fatal(err)
	}
}

func TestAllEnemiesScope(t *testing.T) {
	r := New([]Fighter{{Attack: 10}}, []Fighter{{HP: 5, MaxHP: 5}, {HP: 6, MaxHP: 6}}, Rules{}, random.New(1))
	e := r.Resolve([]Command{{Actor: 0, Action: Action{Scope: AllEnemies, Power: 1}}})
	if len(e) != 3 || !r.Won {
		t.Fatalf("events=%+v", e)
	}
}
