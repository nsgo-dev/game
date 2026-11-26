package editorcmd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestBuiltInAgentModelsAndUsageMacOSScreenshots renders Settings › Models and connections,
// Settings › Usage and privacy, Connect dialog API key step, Agent panel 529 Model error card,
// and Agent panel Limit reached + Queue on real macOS (`-target macos`) in Dark and Light themes
// for visual inspection beside docs/design/editor/Settings.dc.html, AgentStates.dc.html, and Connect.dc.html.
func TestBuiltInAgentModelsAndUsageMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	sceneDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sceneDir, "level_01.scene.yaml"), []byte("version: 1\nname: Level 01\nlayers:\n  - name: world\n    space: world\n    order: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pf := &project.File{
		Version:    project.CurrentVersion,
		Name:       "Dungeon Run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.4.2",
		Window:     project.Window{Width: 640, Height: 360},
		Autoload: &project.Autoload{
			Scenes:    []project.Entry{{Name: "level_01", Path: "scenes/level_01.scene.yaml"}},
			MainScene: "level_01",
		},
	}
	data, err := project.Encode(pf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(529)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]string{
				"type":    "overloaded_error",
				"message": "Overloaded",
			},
		})
	}))
	defer srv.Close()

	var schedMu sync.Mutex
	var scheduled []func()
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 14, 12, 0, 0, time.UTC)
	ed.Now = func() time.Time { return now }
	_ = ed.SetLanguage(editor.LanguageEnglish)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.BuiltInAgentEndpoint = srv.URL
	ed.BuiltInHTTPClient = srv.Client()
	ed.AgentScheduleAfter = func(_ time.Duration, fn func()) func() {
		schedMu.Lock()
		scheduled = append(scheduled, fn)
		schedMu.Unlock()
		return func() {}
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Secrets(map[string]string{
		editor.AgentAPIKeySecretAccount: "sk-ant-screenshot-key",
	}))

	app.Post(func() {
		ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
			ID:        "claude-1",
			Name:      "claude-code",
			Transport: "stdio",
		})
		ed.RecordAgentReviewOutcome(true, 6)
		ed.RecordAgentReviewOutcome(false, 1)
		ed.RecordPlaytestRun(380)
	})
	app.Settle()

	// 1. Connect dialog › Built in, with your API key
	app.Post(func() { ed.OpenConnectDialogForMode(editor.AgentModeAPIKey) })
	app.Settle()
	app.Screenshot("connect-apikey-dark")
	app.Post(func() { ed.CloseConnectDialog() })
	app.Settle()

	// 2. Agent panel › 529 Model error card (try 2 of 4 · Retrying in 8 s) in Dark & Light
	app.Post(func() {
		ed.SetRightRailTab(editor.RightTabAgent)
		_ = ed.SubmitAgentTaskPrompt(editor.AgentTaskPlaytest, "Analyze floor 3 deaths")
	})
	app.WaitUntil(5*time.Second, func() bool {
		turns := ed.AgentTurns()
		return len(turns) == 1 && turns[0].ModelError != nil && turns[0].ModelError.RetryAttempt == 1
	})
	app.Post(func() {
		schedMu.Lock()
		var fn func()
		if len(scheduled) > 0 {
			fn = scheduled[len(scheduled)-1]
		}
		schedMu.Unlock()
		if fn != nil {
			fn()
		}
	})
	app.WaitUntil(5*time.Second, func() bool {
		turns := ed.AgentTurns()
		return len(turns) == 1 && turns[0].ModelError != nil && turns[0].ModelError.RetryAttempt == 2
	})
	app.Settle()
	app.Screenshot("agent-529-error-dark")
	app.Post(func() { _ = ed.SetAppearance(editor.AppearanceLight) })
	app.Settle()
	app.Screenshot("agent-529-error-light")
	app.Post(func() { _ = ed.SetAppearance(editor.AppearanceDark) })
	app.Settle()

	// 3. Agent panel › Limit reached + Queued requests in Dark & Light
	app.Post(func() {
		ed.StopAgentRun()
		_ = ed.SetWeeklyRequestLimit(1)
		_ = ed.SubmitAgentPrompt("Make the coins spin slowly")
		_ = ed.SubmitAgentPrompt("Add a lever beside the door")
	})
	app.Settle()
	app.Screenshot("agent-limit-reached-dark")
	app.Post(func() { _ = ed.SetAppearance(editor.AppearanceLight) })
	app.Settle()
	app.Screenshot("agent-limit-reached-light")
}
