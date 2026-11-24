package datatable_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
	"github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
)

type Stats struct {
	Speed float64 `table:"speed"`
}

type Unit struct {
	Stats
	ID   string `json:"id" schema:"required"`
	Rank *int
}

func TestSchema(t *testing.T) {
	s, err := datatable.Schema[Enemy](jsonschema.Options{ID: "https://example.com/enemies.json", Docs: map[string]string{"Enemy.HP": "Hit points."}})
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if s.ID != "https://example.com/enemies.json" || len(s.Types) != 1 || s.Types[0] != jsonschema.TypeObject || len(s.Required) != 1 || s.Required[0] != "version" {
		t.Fatalf("root = %+v", s)
	}
	if v := s.Property("version"); v == nil || v.Const != datatable.FormatVersion {
		t.Fatalf("version = %+v", v)
	}
	rowsSchema := s.Property("rows")
	if rowsSchema == nil || len(rowsSchema.Types) != 2 || rowsSchema.Types[0] != jsonschema.TypeArray || rowsSchema.Types[1] != jsonschema.TypeNull || rowsSchema.Items.Ref != "#/definitions/Enemy" {
		t.Fatalf("rows = %+v", rowsSchema)
	}
	row := s.Definitions["Enemy"]
	var names []string
	for _, p := range row.Properties {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "enemy_id,display_name,hp,Armor,move_speed,is_boss,tags,drop_ids,cooldown" {
		t.Fatalf("columns = %s", got)
	}
	if row.Property("hp").Description != "Hit points." {
		t.Fatalf("hp description = %q", row.Property("hp").Description)
	}
	if got := row.Property("cooldown").Types; len(got) != 2 {
		t.Fatalf("cooldown types = %v, want string or integer", got)
	}

	// The table MarshalYAML writes is in the canonical form, so the schema accepts it.
	table, err := datatable.MarshalYAML(wantEnemies())
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	if err := s.ValidateYAML(table); err != nil {
		t.Fatalf("ValidateYAML of a canonical table:\n%s\n%v", table, err)
	}
	hand := "version: 1\nrows:\n  - enemy_id: goblin\n    cooldown: 500ms\n  - enemy_id: dragon\n    cooldown: 2000000000\n"
	if _, err := datatable.LoadYAML[Enemy]("enemies.yaml", []byte(hand)); err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if err := s.ValidateYAML([]byte(hand)); err != nil {
		t.Fatalf("ValidateYAML: %v", err)
	}
	// A misspelt column or an out-of-range cell is flagged where it is written.
	err = s.ValidateYAML([]byte("version: 1\nrows:\n  - enemy_id: goblin\n    hp_max: 45\n    Armor: -1\n"))
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) || len(verr.Issues) != 2 || verr.Issues[0].Path != "/rows/0/Armor" || verr.Issues[1].Path != "/rows/0/hp_max" {
		t.Fatalf("ValidateYAML of a misspelt table = %v", err)
	}
	// LoadYAML reads a missing or null rows key as a table with no rows, so the schema accepts both.
	for _, empty := range []string{"version: 1\n", "version: 1\nrows:\n", "version: 1\nrows: []\n"} {
		if rows, err := datatable.LoadYAML[Enemy]("enemies.yaml", []byte(empty)); err != nil || len(rows) != 0 {
			t.Fatalf("LoadYAML(%q) = %v, %v", empty, rows, err)
		}
		if err := s.ValidateYAML([]byte(empty)); err != nil {
			t.Fatalf("ValidateYAML(%q): %v", empty, err)
		}
	}
	// What LoadYAML refuses at the top level, the schema refuses too.
	for _, bad := range []string{"", "- enemy_id: goblin\n", "rows: []\n", "version: 2\nrows: []\n", "version: 1\nrows: []\nextra: 1\n"} {
		if _, err := datatable.LoadYAML[Enemy]("enemies.yaml", []byte(bad)); err == nil {
			t.Fatalf("LoadYAML(%q) succeeded", bad)
		}
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("ValidateYAML(%q) succeeded", bad)
		}
	}
}

type Base struct {
	ID string `table:"id"`
}

type Shadowed struct {
	Base
	ID   string `table:"id"`
	Note string `table:"note"`
}

func TestSchemaRefusesSharedColumns(t *testing.T) {
	// LoadYAML refuses a row type in which two fields share a column, and so does Schema.
	if _, err := datatable.LoadYAML[Shadowed]("t.yaml", []byte("version: 1\nrows: []\n")); err == nil {
		t.Fatal("LoadYAML of a type with a shared column succeeded")
	}
	if _, err := datatable.Schema[Shadowed](jsonschema.Options{}); err == nil {
		t.Fatal("Schema of a type with a shared column succeeded")
	}
}

func TestSchemaEmbeddedAndErrors(t *testing.T) {
	s, err := datatable.Schema[Unit](jsonschema.Options{})
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if err := s.Validate(map[string]any{"version": 1, "rows": []any{map[string]any{"id": "a", "speed": 1.5, "Rank": 2}}}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := s.Validate(map[string]any{"version": 1, "rows": []any{map[string]any{"speed": 1}}}); err == nil {
		t.Fatal("a row without its required id validated")
	}
	if _, err := datatable.Schema[int](jsonschema.Options{}); err == nil {
		t.Fatal("Schema of a non-struct row succeeded")
	}
}
