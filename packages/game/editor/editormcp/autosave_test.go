package editormcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

func TestSaveFailureWarnsAgentUntilRetrySucceeds(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores read-only directory permissions")
	}
	f := openFixture(t)
	now := time.Date(2026, 11, 26, 14, 40, 0, 0, time.UTC)
	f.ed.Now = func() time.Time { return now }

	scenesDir := filepath.Join(f.dir, "scenes")
	t.Cleanup(func() { _ = os.Chmod(scenesDir, 0o755) })

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Baseline: healthy save status carries no warning.
	baseRes := call(t, cs, editormcp.ToolSelectionGet, nil)
	baseText := mustOK(t, baseRes)
	if strings.Contains(baseText, "edits are not on disk") {
		t.Fatalf("unexpected save warning before failure:\n%s", baseText)
	}
	if baseRes.Meta != nil && baseRes.Meta["nsgo.dev/editsNotOnDisk"] == true {
		t.Fatalf("unexpected editsNotOnDisk meta before failure: %v", baseRes.Meta)
	}

	// Make scenes/ read-only, make a kept edit, and let autosave fire after 2 s.
	if err := os.Chmod(scenesDir, 0o555); err != nil {
		t.Fatalf("chmod read-only: %v", err)
	}
	if err := f.ed.MoveObject("hero", 88, 99); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	f.ed.StepAutoSave()
	f.ed.WaitAutoSave()

	if st := f.ed.SaveStatus(); st.Kind != editor.SaveStatusFailed {
		t.Fatalf("SaveStatus = %+v, want failed", st)
	}

	// Every MCP tool result warns the agent that edits are not on disk until saving succeeds.
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{editormcp.ToolSelectionGet, nil},
		{editormcp.ToolSceneRead, map[string]any{}},
		{editormcp.ToolGameCheck, nil},
	} {
		res := call(t, cs, tc.tool, tc.args)
		out := mustOK(t, res)
		if !strings.Contains(out, "edits are not on disk") || !strings.Contains(out, "folder is read-only") {
			t.Fatalf("%s result missing save failure warning:\n%s", tc.tool, out)
		}
		if res.Meta == nil || res.Meta["nsgo.dev/editsNotOnDisk"] != true {
			t.Fatalf("%s result missing nsgo.dev/editsNotOnDisk meta: %v", tc.tool, res.Meta)
		}
	}

	// Restore permissions and Retry: file writes cleanly and clears the MCP warning.
	if err := os.Chmod(scenesDir, 0o755); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	if err := f.ed.RetrySave(); err != nil {
		t.Fatalf("RetrySave: %v", err)
	}
	if f.ed.SaveFailed() || f.ed.SceneDirty() {
		t.Fatalf("after RetrySave: failed=%v dirty=%v", f.ed.SaveFailed(), f.ed.SceneDirty())
	}

	recoveredRes := call(t, cs, editormcp.ToolSelectionGet, nil)
	recoveredText := mustOK(t, recoveredRes)
	if strings.Contains(recoveredText, "edits are not on disk") {
		t.Fatalf("stale save warning persisted after RetrySave:\n%s", recoveredText)
	}
	if recoveredRes.Meta != nil && recoveredRes.Meta["nsgo.dev/editsNotOnDisk"] == true {
		t.Fatalf("stale editsNotOnDisk meta persisted after RetrySave: %v", recoveredRes.Meta)
	}
}
