package editor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
)

func TestOpenFileErrorNamesFileLineAndFix(t *testing.T) {
	ed := newTestEditor(t)
	path := filepath.Join(t.TempDir(), "cave.scene.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nlayers:\n  - name: bg\n    space: isometric\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaderrtest.Expect(t, ed.OpenFile(path), path, 4)

	missing := filepath.Join(t.TempDir(), "none.yaml")
	loaderrtest.Expect(t, ed.OpenFile(missing), missing, 0)
}
