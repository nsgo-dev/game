package rpgdata

import "testing"

func TestDatabaseRoundTripAndReferences(t *testing.T) {
	d := &Database{Version: 1, Classes: []Class{{ID: "hero", MaxLevel: 10, LevelXP: 100, Stats: map[string][]int{"hp": {20, 30}}}}, Actors: []Actor{{ID: "a", Name: "Hero", Class: "hero"}}, Skills: []Skill{{ID: "slash", Name: "Slash", Scope: "one_enemy"}}, Enemies: []Enemy{{ID: "slime", Name: "Slime", HP: 10}}, Troops: []Troop{{ID: "intro", Members: []TroopMember{{Enemy: "slime"}}}}}
	raw, err := Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classes[0].StatAt("hp", 2) != 30 {
		t.Fatalf("stat = %d", got.Classes[0].StatAt("hp", 2))
	}
	bad := *d
	bad.Troops = []Troop{{ID: "bad", Members: []TroopMember{{Enemy: "missing"}}}}
	if _, err := Encode(&bad); err == nil {
		t.Fatal("unknown enemy accepted")
	}
}

func TestProgressionLevelUps(t *testing.T) {
	p := NewProgression(1, 4, 100)
	if got := p.AddExperience(250); len(got) != 1 || p.Level != 2 || p.Experience != 150 {
		t.Fatalf("progression = %+v, levels=%v", p, got)
	}
}
