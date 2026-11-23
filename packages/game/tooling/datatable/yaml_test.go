package datatable_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const enemiesYAML = `version: 1
rows:
  - enemy_id: goblin
    display_name: Cave Goblin
    hp: 45
    Armor: 2
    move_speed: 3.5
    is_boss: false
    tags: [melee, scout]
    drop_ids: [101, 102]
    cooldown: 500ms
  - enemy_id: dragon
    display_name: Ancient Wyrm
    hp: 1200
    Armor: 40
    move_speed: 6.25
    is_boss: true
    tags: [flying, fire, boss]
    drop_ids: [900]
    cooldown: 2s
`

func TestMarshalYAMLIsCanonicalAndRoundTrips(t *testing.T) {
	got, err := datatable.MarshalYAML(wantEnemies())
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	if string(got) != enemiesYAML {
		t.Fatalf("MarshalYAML =\n%s\nwant\n%s", got, enemiesYAML)
	}

	rows, err := datatable.LoadYAML[Enemy]("enemies.yaml", got)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if !reflect.DeepEqual(rows, wantEnemies()) {
		t.Fatalf("LoadYAML(MarshalYAML(rows)) =\n%+v\nwant\n%+v", rows, wantEnemies())
	}

	again, err := datatable.MarshalYAML(rows)
	if err != nil {
		t.Fatalf("MarshalYAML again: %v", err)
	}
	if string(again) != enemiesYAML {
		t.Fatalf("second save changed the file:\n%s", again)
	}
}

type Item struct {
	ID      string   `table:"id"`
	Label   string   `table:"label"`
	Weight  float32  `table:"weight"`
	Stack   *int     `table:"stack"`
	Aliases []string `table:"aliases"`
	Price   uint64   `table:"price"`
	Delta   int8     `table:"delta"`
}

func TestMarshalYAMLQuotesAmbiguousStringsAndKeepsValues(t *testing.T) {
	stack := 0
	rows := []Item{
		{ID: "true", Label: "", Weight: 3, Stack: &stack, Aliases: []string{"a, b", "null", " padded ", "x: y"}, Price: 18446744073709551615, Delta: -128},
		{ID: "123", Label: "line one\nline two", Weight: -0.125, Aliases: []string{"multi\nline", "#hash", "[bracket]"}},
		{ID: "plain", Label: "Iron Sword", Weight: 1e-7},
	}
	data, err := datatable.MarshalYAML(rows)
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	got, err := datatable.LoadYAML[Item]("items.yaml", data)
	if err != nil {
		t.Fatalf("LoadYAML of saved table: %v\n%s", err, data)
	}
	// A nil list is saved as [] and loads back as an empty list.
	want := append([]Item(nil), rows...)
	want[2].Aliases = []string{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip =\n%#v\nwant\n%#v\nfile:\n%s", got, want, data)
	}
	if got[0].Stack == nil || *got[0].Stack != 0 || got[1].Stack != nil {
		t.Fatalf("pointer fields did not round-trip: %v, %v", got[0].Stack, got[1].Stack)
	}
	if !strings.Contains(string(data), "    stack: null\n") {
		t.Fatalf("nil pointer not written as null:\n%s", data)
	}
	again, err := datatable.MarshalYAML(got)
	if err != nil {
		t.Fatalf("MarshalYAML again: %v", err)
	}
	if string(again) != string(data) {
		t.Fatalf("second save changed the file:\nfirst:\n%s\nsecond:\n%s", data, again)
	}
}

func TestMarshalYAMLEmptyTable(t *testing.T) {
	data, err := datatable.MarshalYAML[Enemy](nil)
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	if want := "version: 1\nrows: []\n"; string(data) != want {
		t.Fatalf("MarshalYAML(nil) = %q, want %q", data, want)
	}
	rows, err := datatable.LoadYAML[Enemy]("empty.yaml", data)
	if err != nil || len(rows) != 0 {
		t.Fatalf("LoadYAML(empty table) = %v, %v", rows, err)
	}
	rows, err = datatable.LoadYAML[Enemy]("empty.yaml", []byte("version: 1\n"))
	if err != nil || len(rows) != 0 {
		t.Fatalf("LoadYAML(no rows key) = %v, %v", rows, err)
	}
}

func TestMarshalYAMLRefusesUnsavableValues(t *testing.T) {
	type bad struct {
		Name  string         `table:"name"`
		Extra map[string]int `table:"extra"`
	}
	_, err := datatable.MarshalYAML([]bad{{Name: "a"}})
	var ce *datatable.CellError
	if !errors.As(err, &ce) || ce.Row != 1 || ce.Field != "extra" {
		t.Fatalf("MarshalYAML(map field) = %v, want CellError at row 1 field extra", err)
	}

	type num struct {
		V float64 `table:"v"`
	}
	_, err = datatable.MarshalYAML([]num{{V: 1}, {V: posInf()}})
	if !errors.As(err, &ce) || ce.Row != 2 || ce.Field != "v" {
		t.Fatalf("MarshalYAML(+Inf) = %v, want CellError at row 2 field v", err)
	}
}

func posInf() float64 {
	zero := 0.0
	return 1 / zero
}

func TestLoadYAMLIsStrict(t *testing.T) {
	cases := []struct {
		name  string
		data  string
		want  string
		row   int
		field string
		is    error
	}{
		{name: "empty file", data: "", want: "empty YAML data table"},
		{name: "null document", data: "null\n", want: "empty YAML data table"},
		{name: "bare list", data: "- enemy_id: goblin\n", want: "not a bare list of rows"},
		{name: "scalar root", data: "goblin\n", want: "must be a mapping"},
		{name: "missing version", data: "rows: []\n", want: "missing version"},
		{name: "unsupported version", data: "version: 2\nrows: []\n", want: "unsupported version 2"},
		{name: "string version", data: "version: \"1\"\nrows: []\n", want: "version must be an integer"},
		{name: "unknown top-level key", data: "version: 1\nname: enemies\nrows: []\n", want: `unknown top-level key "name"`},
		{name: "duplicate rows key", data: "version: 1\nrows: []\nrows: []\n", want: "duplicate"},
		{name: "rows not a list", data: "version: 1\nrows: {enemy_id: goblin}\n", want: "rows must be a list"},
		{name: "row not a mapping", data: "version: 1\nrows:\n  - goblin\n", row: 1, field: "<row>"},
		{name: "unknown field", data: "version: 1\nrows:\n  - enemy_id: goblin\n  - enemy_id: orc\n    hit_points: 9\n", row: 2, field: "hit_points", is: datatable.ErrUnknownField},
		{name: "ignored field is unknown", data: "version: 1\nrows:\n  - Ignored: x\n", row: 1, field: "Ignored", is: datatable.ErrUnknownField},
		{name: "duplicate field by case", data: "version: 1\nrows:\n  - hp: 1\n    HP: 2\n", row: 1, field: "HP", is: datatable.ErrDuplicateField},
		{name: "duplicate field by tag and name", data: "version: 1\nrows:\n  - armor: 1\n    Armor: 2\n", row: 1, field: "Armor", is: datatable.ErrDuplicateField},
		{name: "second document", data: "version: 1\nrows:\n  - enemy_id: goblin\n---\nversion: 9\nbogus: 1\n", want: "unexpected second YAML document"},
		{name: "empty second document", data: "version: 1\nrows: []\n---\n", want: "unexpected second YAML document"},
		{name: "merge key in a row", data: "version: 1\nrows:\n  - &b {enemy_id: a, display_name: x}\n  - <<: *b\n    enemy_id: b\n", want: "merge keys (<<) are not supported", row: 2, field: "<<"},
		{name: "merge key replacing version", data: "<<: {version: 1}\nrows: []\n", want: "merge keys (<<) are not supported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := datatable.LoadYAML[Enemy]("enemies.yaml", []byte(tc.data))
			if err == nil {
				t.Fatalf("LoadYAML accepted %q: %+v", tc.data, rows)
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
			if tc.row == 0 {
				return
			}
			var ce *datatable.CellError
			if !errors.As(err, &ce) || ce.File != "enemies.yaml" || ce.Row != tc.row || ce.Field != tc.field {
				t.Fatalf("error = %#v, want CellError at row %d field %q", err, tc.row, tc.field)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("error = %v, want errors.Is %v", err, tc.is)
			}
		})
	}
}

func TestSaveYAMLThenLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enemies.yaml")
	if err := datatable.SaveYAML(path, wantEnemies()); err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != enemiesYAML {
		t.Fatalf("saved file =\n%s\nwant\n%s", data, enemiesYAML)
	}
	rows, err := datatable.LoadFile[Enemy](path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !reflect.DeepEqual(rows, wantEnemies()) {
		t.Fatalf("LoadFile = %+v, want %+v", rows, wantEnemies())
	}
}

func TestYAMLTableHotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "enemies.yaml")
	v1 := "version: 1\nrows:\n  - enemy_id: goblin\n    display_name: Goblin\n    hp: 40\n    move_speed: 3\n"
	typo := "version: 1\nrows:\n  - enemy_id: goblin\n    display_name: Goblin\n    hp: 40\n    move_sped: 3\n"
	v2 := "version: 1\nrows:\n  - enemy_id: goblin\n    display_name: Goblin\n    hp: 65\n    move_speed: 4.2\n  - enemy_id: orc\n    display_name: Orc\n    hp: 110\n    move_speed: 2.5\n"

	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := datatable.NewTable[Enemy](path)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	if tbl.Version() != 1 || len(tbl.Rows()) != 1 || tbl.Rows()[0].HP != 40 || tbl.Rows()[0].Speed != 3 {
		t.Fatalf("initial table: ver=%d rows=%+v", tbl.Version(), tbl.Rows())
	}

	var (
		reloads atomic.Int32
		failed  atomic.Bool
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(20*time.Millisecond),
			hotreload.WithDebounce(20*time.Millisecond),
		)
		tbl.WatchWithError(w, func(rows []Enemy, err error) {
			if err != nil {
				failed.Store(true)
				return
			}
			if len(rows) == 2 && rows[0].HP == 65 {
				reloads.Add(1)
			}
		})
		return core.View{}
	})

	// A misspelt field is refused: the table keeps v1 and reports the row and key.
	if err := os.WriteFile(path, []byte(typo), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, failed.Load)
	if tbl.Version() != 1 || tbl.Rows()[0].HP != 40 {
		t.Fatalf("refused reload changed the table: ver=%d rows=%+v", tbl.Version(), tbl.Rows())
	}
	var ce *datatable.CellError
	if !errors.As(tbl.LastError(), &ce) || ce.Row != 1 || ce.Field != "move_sped" || !errors.Is(ce, datatable.ErrUnknownField) {
		t.Fatalf("LastError = %v, want unknown field move_sped at row 1", tbl.LastError())
	}

	// The fixed file replaces the rows and bumps Version.
	if err := os.WriteFile(path, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, func() bool { return reloads.Load() >= 1 })
	if tbl.Version() != 2 || tbl.LastError() != nil {
		t.Fatalf("after valid reload: ver=%d err=%v", tbl.Version(), tbl.LastError())
	}
	got := tbl.Rows()
	if len(got) != 2 || got[0].HP != 65 || got[0].Speed != 4.2 || got[1].ID != "orc" || got[1].HP != 110 {
		t.Fatalf("reloaded rows = %+v", got)
	}
}

type shadowBase struct {
	ID   string `table:"id"`
	Name string `table:"name"`
}

// shadowedByEmbedding has two fields for "name": the embedded one, declared first, and its own.
type shadowedByEmbedding struct {
	shadowBase
	Name string `table:"name"`
}

// sharedTag has two fields tagged "hp".
type sharedTag struct {
	ID string `table:"id"`
	A  int    `table:"hp"`
	B  int    `table:"hp"`
}

func TestYAMLRefusesTypesWithSharedColumns(t *testing.T) {
	const data = "version: 1\nrows:\n  - id: a\n"
	if _, err := datatable.MarshalYAML([]shadowedByEmbedding{{shadowBase: shadowBase{ID: "a", Name: "base"}, Name: "outer"}}); err == nil || !strings.Contains(err.Error(), `column "name"`) {
		t.Fatalf("MarshalYAML(shadowedByEmbedding) error = %v, want a shared column error", err)
	}
	if _, err := datatable.LoadYAML[shadowedByEmbedding]("t.yaml", []byte(data)); err == nil || !strings.Contains(err.Error(), `column "name"`) {
		t.Fatalf("LoadYAML[shadowedByEmbedding] error = %v, want a shared column error", err)
	}
	if _, err := datatable.MarshalYAML([]sharedTag{{ID: "a", A: 1, B: 2}}); err == nil || !strings.Contains(err.Error(), `column "hp"`) {
		t.Fatalf("MarshalYAML(sharedTag) error = %v, want a shared column error", err)
	}
	if _, err := datatable.LoadYAML[sharedTag]("t.yaml", []byte(data)); err == nil || !strings.Contains(err.Error(), `column "hp"`) {
		t.Fatalf("LoadYAML[sharedTag] error = %v, want a shared column error", err)
	}

	// CSV keeps reading such a type as before: the first field declared takes the column.
	rows, err := datatable.LoadCSV[sharedTag]("t.csv", []byte("id,hp\na,7\n"))
	if err != nil {
		t.Fatalf("LoadCSV[sharedTag]: %v", err)
	}
	if want := []sharedTag{{ID: "a", A: 7}}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("LoadCSV[sharedTag] = %+v, want %+v", rows, want)
	}
}
