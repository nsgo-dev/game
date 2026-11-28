package rpgdata

import (
	"bytes"
	"os"
	"slices"
	"testing"
)

func TestParseEncodeRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/meadow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Troops) != 1 || len(d.Troops[0].Members) != 2 || d.Actors[0].Class != "hero" {
		t.Fatalf("parsed = %+v", d)
	}
	out, err := Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, raw) {
		t.Fatalf("Encode changed the file:\n%s", out)
	}
}

func TestIDsAreUniquePerCategory(t *testing.T) {
	d := &Database{
		Version: CurrentVersion,
		Classes: []Class{{ID: "hero"}},
		Actors:  []Actor{{ID: "hero", Name: "Hero", Class: "hero"}},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("an actor and a class sharing an ID: %v", err)
	}
}

func TestEncodeRefusesBrokenReference(t *testing.T) {
	d := &Database{
		Version: CurrentVersion,
		Enemies: []Enemy{{ID: "slime", Name: "Slime", HP: 10}},
		Troops:  []Troop{{ID: "bad", Members: []TroopMember{{Enemy: "missing"}}}},
	}
	if _, err := Encode(d); err == nil {
		t.Fatal("Encode accepted a troop with an unknown enemy")
	}
}

func TestStatAt(t *testing.T) {
	c := Class{Stats: map[string][]int{"hp": {20, 26, 32}}}
	for _, tc := range []struct{ level, want int }{{0, 20}, {1, 20}, {2, 26}, {3, 32}, {9, 32}} {
		if got := c.StatAt("hp", tc.level); got != tc.want {
			t.Errorf("StatAt(hp, %d) = %d, want %d", tc.level, got, tc.want)
		}
	}
	if got := c.StatAt("mp", 2); got != 0 {
		t.Errorf("StatAt(mp, 2) = %d, want 0", got)
	}
}

func TestSkillsLearnedAt(t *testing.T) {
	c := Class{Skills: []LearnedSkill{{ID: "slash", Level: 1}, {ID: "cleave", Level: 3}, {ID: "guard", Level: 3}}}
	if got := c.SkillsLearnedAt(3); !slices.Equal(got, []string{"cleave", "guard"}) {
		t.Fatalf("SkillsLearnedAt(3) = %v", got)
	}
	if got := c.SkillsLearnedAt(2); got != nil {
		t.Fatalf("SkillsLearnedAt(2) = %v, want none", got)
	}
}

func TestProgressionLevelUps(t *testing.T) {
	p := NewProgression(1, 4, 100)
	// Level 1 to 2 takes 100, 2 to 3 takes 200: 250 reaches level 2 with 150 left over.
	if got := p.AddExperience(250); !slices.Equal(got, []int{2}) || p.Level != 2 || p.Experience != 150 {
		t.Fatalf("after 250: %+v, levels %v", p, got)
	}
	// 150 + 550 = 700 pays for 2 to 3 (200) and 3 to 4 (300), then the cap stops it.
	if got := p.AddExperience(550); !slices.Equal(got, []int{3, 4}) || p.Level != 4 || p.Experience != 200 {
		t.Fatalf("after 550 more: %+v, levels %v", p, got)
	}
	if got := p.AddExperience(10_000); got != nil || p.Level != 4 || p.NeedForNext() != 0 {
		t.Fatalf("past the cap: %+v, levels %v", p, got)
	}
}

func TestNewProgressionClampsLevel(t *testing.T) {
	if p := NewProgression(0, 5, 10); p.Level != 1 {
		t.Errorf("level 0 became %d, want 1", p.Level)
	}
	if p := NewProgression(9, 5, 10); p.Level != 5 {
		t.Errorf("level 9 with cap 5 became %d, want 5", p.Level)
	}
	if p := NewProgression(9, 0, 10); p.Level != 9 {
		t.Errorf("level 9 with no cap became %d, want 9", p.Level)
	}
}
