package entities_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/save"
)

type hp struct {
	Value int `json:"value"`
}

func TestLoadWorldErrorsNameSlotLineAndFix(t *testing.T) {
	cases := []struct {
		name, snapshot, field string
		sentinel              error
	}{
		{"zero generation", `{"entities": [{"index": 0, "generation": 1}, {"index": 1, "generation": 0}]}`, "data.entities[1].generation", entities.ErrInvalidSnapshot},
		{"duplicate index", `{"entities": [{"index": 3, "generation": 1}, {"index": 3, "generation": 2}]}`, "data.entities[1].index", entities.ErrInvalidSnapshot},
		{"unknown parent", `{"entities": [{"index": 0, "generation": 1, "parent": 7}]}`, "data.entities[0].parent", entities.ErrInvalidSnapshot},
		{"cycle", `{"entities": [{"index": 0, "generation": 1, "parent": 1}, {"index": 1, "generation": 1, "parent": 0}]}`, "data.entities[0].parent", entities.ErrInvalidSnapshot},
		{"unknown codec", `{"entities": [{"index": 0, "generation": 1, "components": [{"type": "hp", "data": {"value": 3}}, {"type": "mana", "data": {}}]}]}`, "data.entities[0].components[1].type", entities.ErrUnknownCodec},
		{"bad component", `{"entities": [{"index": 0, "generation": 1, "components": [{"type": "hp", "data": {"value": "full"}}]}]}`, "data.entities[0].components[0].data", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			data := loaderrtest.Indent(`{"version": 1, "data": ` + tc.snapshot + `}`)
			if err := os.WriteFile(filepath.Join(dir, "world.json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			w := entities.NewWorld()
			if err := entities.RegisterCodec[hp](w, "hp"); err != nil {
				t.Fatal(err)
			}
			err := entities.LoadWorld(save.NewFileStore(dir), "world", w)
			if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
				t.Fatalf("err = %v, want one matching %v", err, tc.sentinel)
			}
			loaderrtest.ExpectField(t, err, filepath.Join(dir, "world.json"), data, tc.field)
		})
	}
}

func TestLoadWorldFallsBackToTheBackupWhenThePrimaryIsRefused(t *testing.T) {
	cases := []struct{ name, primary string }{
		{"unknown parent", `{"version": 1, "data": {"entities": [{"index": 0, "generation": 1, "parent": 7}]}}`},
		{"unknown codec", `{"version": 1, "data": {"entities": [{"index": 0, "generation": 1, "components": [{"type": "mana", "data": {}}]}]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// The backup leaves out parent and components, so a value the refused primary decoded must not survive into it.
			backup := `{"version": 1, "data": {"entities": [{"index": 0, "generation": 1}]}}`
			if err := os.WriteFile(filepath.Join(dir, "world.json"), []byte(tc.primary), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "world.json.bak"), []byte(backup), 0o644); err != nil {
				t.Fatal(err)
			}
			w := entities.NewWorld()
			if err := entities.RegisterCodec[hp](w, "hp"); err != nil {
				t.Fatal(err)
			}
			if err := entities.LoadWorld(save.NewFileStore(dir), "world", w); err != nil {
				t.Fatalf("LoadWorld = %v, want the backup restored", err)
			}
			snap, err := w.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if len(snap.Entities) != 1 || snap.Entities[0].Parent != nil || len(snap.Entities[0].Components) != 0 {
				t.Fatalf("restored %+v, want the backup's one entity without parent or components", snap.Entities)
			}
		})
	}
}
