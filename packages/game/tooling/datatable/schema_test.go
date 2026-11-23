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
	if s.ID != "https://example.com/enemies.json" || len(s.Types) != 1 || s.Types[0] != jsonschema.TypeArray || s.Items.Ref != "#/definitions/Enemy" {
		t.Fatalf("root = %+v", s)
	}
	row := s.Definitions["Enemy"]
	var names []string
	for _, p := range row.Properties {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "enemy_id,display_name,hp,armor,move_speed,is_boss,tags,drop_ids,cooldown" {
		t.Fatalf("columns = %s", got)
	}
	if row.Property("hp").Description != "Hit points." {
		t.Fatalf("hp description = %q", row.Property("hp").Description)
	}
	if got := row.Property("cooldown").Types; len(got) != 2 {
		t.Fatalf("cooldown types = %v, want string or integer", got)
	}

	// The YAML table the loader tests read is in the canonical form, so the schema accepts it.
	table := "- enemy_id: goblin\n  display_name: Cave Goblin\n  hp: 45\n  armor: 2\n  move_speed: 3.5\n  is_boss: false\n  tags: [melee]\n  drop_ids: [101]\n  cooldown: 500ms\n- enemy_id: dragon\n  cooldown: 2000000000\n"
	if _, err := datatable.LoadYAML[Enemy]("enemies.yaml", []byte(table)); err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if err := s.ValidateYAML([]byte(table)); err != nil {
		t.Fatalf("ValidateYAML of a canonical table: %v", err)
	}
	// A misspelt column loads silently as zero; the schema points at it.
	err = s.ValidateYAML([]byte("- enemy_id: goblin\n  hp_max: 45\n  armor: -1\n"))
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) || len(verr.Issues) != 2 || verr.Issues[0].Path != "/0/armor" || verr.Issues[1].Path != "/0/hp_max" {
		t.Fatalf("ValidateYAML of a misspelt table = %v", err)
	}
}

func TestSchemaEmbeddedAndErrors(t *testing.T) {
	s, err := datatable.Schema[Unit](jsonschema.Options{})
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if err := s.Validate([]any{map[string]any{"id": "a", "speed": 1.5, "rank": 2}}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := s.Validate([]any{map[string]any{"speed": 1}}); err == nil {
		t.Fatal("a row without its required id validated")
	}
	if _, err := datatable.Schema[int](jsonschema.Options{}); err == nil {
		t.Fatal("Schema of a non-struct row succeeded")
	}
}
