package save_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/save"
)

type loadProgress struct {
	Level int      `json:"level"`
	Items []string `json:"items"`
}

func TestLoadErrorsNameSlotLineAndFix(t *testing.T) {
	cases := []struct {
		name, json, field string
		sentinel          error
	}{
		{"future version", `{"version": 5, "data": {"level": 1}}`, "version", save.ErrFutureVersion},
		{"missing migration", `{"version": 1, "data": {"level": 1}}`, "version", save.ErrMissingMigration},
		{"null data", `{"version": 2, "data": null}`, "data", save.ErrCorrupt},
		{"zero version", `{"version": 0, "data": {}}`, "version", save.ErrCorrupt},
		{"wrong type", `{"version": 2, "data": {"level": 1, "items": ["a", 3]}}`, "data.items[1]", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			data := loaderrtest.Indent(tc.json)
			if err := os.WriteFile(filepath.Join(dir, "slot1.json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			store := save.NewFileStore(dir)
			store.Version = 2
			var p loadProgress
			err := store.Load("slot1", &p)
			if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
				t.Fatalf("err = %v, want one matching %v", err, tc.sentinel)
			}
			loaderrtest.ExpectField(t, err, filepath.Join(dir, "slot1.json"), data, tc.field)
		})
	}
	t.Run("syntax", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "slot1.json"), []byte("{\n  \"version\": 1,\n  \"data\": {\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var p loadProgress
		err := save.NewFileStore(dir).Load("slot1", &p)
		if !errors.Is(err, save.ErrCorrupt) {
			t.Fatalf("err = %v, want ErrCorrupt", err)
		}
		loaderrtest.Expect(t, err, filepath.Join(dir, "slot1.json"), 3)
	})
	t.Run("backup", func(t *testing.T) {
		dir := t.TempDir()
		bad := []byte("{\n  \"version\": 1,\n  \"data\": 5\n}")
		for _, name := range []string{"slot1.json", "slot1.json.bak"} {
			if err := os.WriteFile(filepath.Join(dir, name), bad, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var p loadProgress
		// The primary's error is the one returned.
		loaderrtest.ExpectField(t, save.NewFileStore(dir).Load("slot1", &p), filepath.Join(dir, "slot1.json"), bad, "data")
		if err := os.Remove(filepath.Join(dir, "slot1.json")); err != nil {
			t.Fatal(err)
		}
		loaderrtest.ExpectField(t, save.NewFileStore(dir).Load("slot1", &p), filepath.Join(dir, "slot1.json.bak"), bad, "data")
	})
}

type tagged struct {
	Name string   `json:"name"`
	Tag  *string  `json:"tag,omitempty"`
	List []string `json:"list,omitempty"`
}

func TestLoadCheckedFallsBackWithoutTheRefusedPrimary(t *testing.T) {
	dir := t.TempDir()
	primary := `{"version": 1, "data": {"name": "a", "tag": "bad", "list": ["x"]}}`
	backup := `{"version": 1, "data": {"name": "b"}}`
	if err := os.WriteFile(filepath.Join(dir, "slot1.json"), []byte(primary), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "slot1.json.bak"), []byte(backup), 0o644); err != nil {
		t.Fatal(err)
	}
	store := save.NewFileStore(dir)
	var v tagged
	check := func() error {
		if v.Tag != nil {
			return errors.New("tag refused")
		}
		return nil
	}
	if err := store.LoadChecked("slot1", &v, check); err != nil {
		t.Fatalf("LoadChecked = %v, want the backup loaded", err)
	}
	if v.Name != "b" || v.Tag != nil || v.List != nil {
		t.Fatalf("loaded %+v, want only the backup's name", v)
	}

	// When every copy is refused, dst keeps the value it had before the call.
	if err := os.WriteFile(filepath.Join(dir, "slot1.json.bak"), []byte(primary), 0o644); err != nil {
		t.Fatal(err)
	}
	start := tagged{Name: "start", List: []string{"kept"}}
	v = start
	if err := store.LoadChecked("slot1", &v, check); err == nil {
		t.Fatal("LoadChecked = nil, want the check's error")
	}
	if v.Name != start.Name || v.Tag != nil || len(v.List) != 1 || v.List[0] != "kept" {
		t.Fatalf("dst = %+v after a failed load, want %+v", v, start)
	}
}
