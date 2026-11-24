//go:build !release

package devstate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/tooling/devstate"
)

func TestLoadSnapshotErrorNamesFileLineAndFix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte("{\n  \"scene_id\": \"title\",\n  \"loop_tick\": \"soon\"\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok, err := devstate.NewStore(path).LoadSnapshot()
	if !ok {
		t.Fatal("LoadSnapshot ok = false for a stored snapshot")
	}
	loaderrtest.Expect(t, err, path, 3)
}
