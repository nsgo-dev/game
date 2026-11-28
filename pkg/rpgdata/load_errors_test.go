package rpgdata_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/rpgdata"
)

func TestParseLoadErrors(t *testing.T) {
	cases := []struct {
		name  string
		data  string
		field string
	}{
		{"unknown class", "version: 1\nclasses:\n  - id: hero\nactors:\n  - id: ren\n    name: Ren\n    class: mage\n", "actors[0].class"},
		{"unknown learned skill", "version: 1\nclasses:\n  - id: hero\n    skills:\n      - id: fire\n        level: 1\n", "classes[0].skills[0].id"},
		{"learned past the cap", "version: 1\nclasses:\n  - id: hero\n    maxLevel: 5\n    skills:\n      - id: slash\n        level: 6\nskills:\n  - id: slash\n    name: Slash\n    scope: one_enemy\n", "classes[0].skills[0].level"},
		{"duplicate enemy", "version: 1\nenemies:\n  - id: slime\n    name: Slime\n    hp: 1\n  - id: slime\n    name: Slime\n    hp: 2\n", "enemies[1].id"},
		{"unknown troop member", "version: 1\nenemies:\n  - id: slime\n    name: Slime\n    hp: 1\ntroops:\n  - id: t\n    members:\n      - enemy: bat\n        x: 0\n        y: 0\n", "troops[0].members[0].enemy"},
		{"empty troop", "version: 1\ntroops:\n  - id: t\n    members: []\n", "troops[0].members"},
		{"empty stat curve", "version: 1\nclasses:\n  - id: hero\n    stats:\n      hp: []\n", "classes[0].stats.hp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := []byte(c.data)
			_, err := rpgdata.Parse(data)
			loaderrtest.ExpectField(t, err, "", data, c.field)
		})
	}

	t.Run("unknown field", func(t *testing.T) {
		_, err := rpgdata.Parse([]byte("version: 1\nenemies:\n  - id: slime\n    name: Slime\n    hp: 1\n    mp: 3\n"))
		loaderrtest.Expect(t, err, "", 6)
	})

	t.Run("trailing document", func(t *testing.T) {
		_, err := rpgdata.Parse([]byte("version: 1\n---\nversion: 1\n"))
		loaderrtest.Expect(t, err, "", 1)
	})
}
