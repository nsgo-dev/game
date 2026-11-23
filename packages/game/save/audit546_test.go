package save_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/save"
)

func TestLoadRewritesCorruptPrimaryFromBackup(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)
	first := playerState{Name: "First", Level: 1}
	if err := store.Save("slot1", first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("slot1", playerState{Name: "Second", Level: 2}); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(dir, "slot1.json")
	if err := os.WriteFile(primary, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got playerState
	if err := store.Load("slot1", &got); err != nil || !equalPlayer(got, first) {
		t.Fatalf("Load = (%+v, %v), want %+v", got, err, first)
	}
	data, err := os.ReadFile(primary)
	if err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(primary + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(bak) {
		t.Fatalf("primary was not rewritten from the backup:\n%s\nvs\n%s", data, bak)
	}
}

func TestEnvelopeRefusesUnknownAndDuplicateFields(t *testing.T) {
	for name, payload := range map[string]string{
		"unknown":   `{"version":1,"data":{"name":"A"},"extra":1}`,
		"duplicate": `{"version":1,"data":{"name":"A"},"data":{"name":"B"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "slot1.json"), []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			var got playerState
			if err := save.NewFileStore(dir).Load("slot1", &got); !errors.Is(err, save.ErrCorrupt) {
				t.Fatalf("Load err = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestDeleteRemovesBackupBeforePrimary(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)
	if err := store.Save("slot1", playerState{Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("slot1", playerState{Name: "B"}); err != nil {
		t.Fatal(err)
	}
	// Make the primary impossible to remove: a Delete that stops there must leave the slot whole, not a backup alone that Load would resurrect.
	primary := filepath.Join(dir, "slot1.json")
	if err := os.Remove(primary); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(primary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(primary, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("slot1"); err == nil {
		t.Fatal("Delete of an unremovable primary succeeded")
	}
	if _, err := os.Stat(primary + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup still present after Delete stopped at the primary: %v", err)
	}
}

func TestWriteSweepsStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".save-123.tmp")
	fresh := filepath.Join(dir, ".save-456.tmp")
	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := save.NewFileStore(dir).Save("slot1", playerState{Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale temp file survived: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a temp file that may belong to a write in progress was removed: %v", err)
	}
}
