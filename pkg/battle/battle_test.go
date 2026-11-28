package battle

import "testing"

func TestOrderTiesAndDeterministicDamage(t *testing.T) {
	b, err := New([]Actor{{ID: "hero", HP: 10, Attack: 5, Agility: 10}}, []Actor{{ID: "a", HP: 4, Defense: 1, Agility: 10}, {ID: "b", HP: 4, Defense: 1, Agility: 2}}, Rules{Damage: func(Actor, Actor, Skill) int { return 3 }}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Order(); len(got) != 3 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("order=%v", got)
	}
	events, err := b.Resolve(Command{Actor: 0, Skill: Skill{ID: "hit", Scope: ScopeOneEnemy, Power: 1}, Target: 0})
	if err != nil {
		t.Fatal(err)
	}
	if b.Troop[0].HP != 1 || events[1].Kind != EventDamage {
		t.Fatalf("events=%v troop=%+v", events, b.Troop[0])
	}
}

func TestScopesStateAndSnapshot(t *testing.T) {
	b, _ := New([]Actor{{ID: "hero", HP: 5, MaxHP: 10, Attack: 5}}, []Actor{{ID: "a", HP: 4}, {ID: "b", HP: 4}}, Rules{Damage: func(Actor, Actor, Skill) int { return 1 }}, 2)
	s := b.Snapshot()
	if _, err := b.Resolve(Command{Actor: 0, Skill: Skill{Scope: ScopeAllEnemies, Power: 5}}); err != nil {
		t.Fatal(err)
	}
	if !b.Troop[0].Alive || !b.Troop[1].Alive {
		t.Fatal("unexpected defeat")
	}
	b.Restore(s)
	if b.Troop[0].HP != 4 || b.Troop[1].HP != 4 {
		t.Fatalf("restore=%+v", b.Troop)
	}
	if _, err := b.Resolve(Command{Actor: 0, Skill: Skill{Scope: ScopeOneAlly, Power: 2}, Target: 3}); err == nil {
		t.Fatal("invalid ally target accepted")
	}
}
