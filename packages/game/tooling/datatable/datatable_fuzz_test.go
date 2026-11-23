package datatable_test

import (
	"bytes"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
)

func FuzzLoadCSV(f *testing.F) {
	f.Add([]byte("enemy_id,display_name,hp,armor,move_speed,is_boss,tags,drop_ids,cooldown\ngoblin,Goblin,45,2,3.5,false,a;b,1;2,500ms\n"))
	f.Add([]byte(""))
	f.Add([]byte("enemy_id,hp\ngoblin,not_a_number\n"))
	f.Add([]byte("\"unterminated"))
	f.Add([]byte("a,b,c\n1\n1,2,3,4\n"))
	f.Add([]byte("\xef\xbb\xbfhp\n9999999999999999999999999999\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = datatable.LoadCSV[Enemy]("fuzz.csv", data)
	})
}

func FuzzLoadJSON(f *testing.F) {
	f.Add([]byte(`[{"enemy_id":"goblin","hp":45,"tags":["a","b"],"drop_ids":[1,2]}]`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[1, "two", null, [], {}]`))
	f.Add([]byte(`[{"hp": 1e309, "move_speed": "NaN", "is_boss": "yes"}]`))
	f.Add([]byte(`[{"tags": [1, 2, {"nested": true}]}]`))
	f.Add([]byte(`[{"hp": 1}] trailing`))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = datatable.LoadJSON[Enemy]("fuzz.json", data)
	})
}

// FuzzLoadYAML checks that LoadYAML never panics and that every table it accepts saves to a canonical form: MarshalYAML succeeds, its output loads back, and saving that again gives the same bytes.
func FuzzLoadYAML(f *testing.F) {
	f.Add([]byte("version: 1\nrows:\n  - enemy_id: goblin\n    hp: 45\n    tags: [a, b]\n"))
	f.Add([]byte(""))
	f.Add([]byte("null"))
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 2\nrows: []\n"))
	f.Add([]byte("- enemy_id: goblin\n"))
	f.Add([]byte("version: 1\nrows: &a [*a]"))
	f.Add([]byte("version: 1\nrows:\n- 123\n- [1, 2]\n- {hp: true, armor: -5}\n"))
	f.Add([]byte("version: 1\nrows:\n- {display_name: \"true\", tags: [\"a, b\", \"\", \"multi\\nline\"], cooldown: -1.5s}\n"))
	f.Add([]byte("version: 1\nrows:\n- {hp: 1, HP: 2}\n"))
	f.Add([]byte("version: 1\nextra: x\nrows: []\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		rows, err := datatable.LoadYAML[Enemy]("fuzz.yaml", data)
		if err != nil {
			return
		}
		first, err := datatable.MarshalYAML(rows)
		if err != nil {
			t.Fatalf("MarshalYAML of loaded rows failed: %v", err)
		}
		again, err := datatable.LoadYAML[Enemy]("fuzz.yaml", first)
		if err != nil {
			t.Fatalf("LoadYAML of saved table failed: %v\n%s", err, first)
		}
		second, err := datatable.MarshalYAML(again)
		if err != nil {
			t.Fatalf("MarshalYAML of reloaded rows failed: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("save is not canonical:\nfirst:\n%s\nsecond:\n%s", first, second)
		}
	})
}
