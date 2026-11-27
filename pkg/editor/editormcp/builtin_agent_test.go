package editormcp_test

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1030 Acceptance 1: A fake model endpoint runs a request end to end through the built-in agent
// with the editor's real MCP tools; the API key is read from nsgotest secrets and appears in no file under the project.
func TestBuiltInAgentEndToEndWithNSGoTestSecretsAndMCPTools(t *testing.T) {
	const secretKey = "sk-ant-nsgotest-secret-do-not-write-to-disk"

	f := openFixture(t)
	settingsPath := filepath.Join(t.TempDir(), "user-config", "editor.yaml")
	f.ed.SettingsPath = settingsPath

	var mu sync.Mutex
	var gotAPIKeys []string
	var sawSceneEditTool bool
	var sawDeleteFileTool bool
	var sawToolResult bool
	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		calls++
		callNum := calls
		gotAPIKeys = append(gotAPIKeys, r.Header.Get("x-api-key"))
		for _, tool := range payload.Tools {
			if tool.Name == editormcp.ToolSceneEdit {
				sawSceneEditTool = true
			}
			if tool.Name == editor.ToolNameGameDeleteFile {
				sawDeleteFileTool = true
			}
		}
		if callNum == 2 && strings.Contains(string(payload.Messages[len(payload.Messages)-1].Content), "tool_result") {
			sawToolResult = true
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if callNum == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "msg_1",
				"model":       payload.Model,
				"stop_reason": "tool_use",
				"content": []map[string]any{
					{
						"type": "tool_use",
						"id":   "toolu_1",
						"name": editormcp.ToolSceneEdit,
						"input": map[string]any{
							"edits": []map[string]any{
								{
									"op":    "add",
									"layer": "actors",
									"object": map[string]any{
										"id":   "slime_builtin",
										"name": "Built-in Slime",
										"x":    180,
										"y":    120,
									},
								},
							},
						},
					},
				},
				"usage": map[string]int{"input_tokens": 120, "output_tokens": 45},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_2",
			"model":       payload.Model,
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "Placed slime_builtin at (180, 120)."},
			},
			"usage": map[string]int{"input_tokens": 180, "output_tokens": 25},
		})
	}))
	defer srv.Close()

	f.ed.BuiltInAgentEndpoint = srv.URL
	f.ed.BuiltInHTTPClient = srv.Client()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1440, 900), nsgotest.Secrets(map[string]string{
		editor.AgentAPIKeySecretAccount: secretKey,
	}))

	s := editormcp.New(f.ed, editormcp.Options{Run: app.Post})
	t.Cleanup(func() { _ = s.Close() })

	// Select "Built in, with your API key" in Connect dialog (key is already in nsgotest secrets).
	f.ed.OpenConnectDialog()
	app.Settle()
	app.Press("id:connect-choice-key")
	app.Settle()
	if !app.Exists("id:connect-key-saved-status") {
		t.Fatal("Connect dialog did not detect API key stored in nsgotest secrets")
	}
	app.Press("id:connect-done")
	app.Settle()

	if f.ed.AgentMode() != editor.AgentModeAPIKey {
		t.Fatalf("AgentMode = %q, want apikey", f.ed.AgentMode())
	}
	if f.ed.AgentStatus() != editor.AgentStatusConnected {
		t.Fatalf("AgentStatus = %q, want connected", f.ed.AgentStatus())
	}

	beforeHist := f.ed.History.Index()

	// Submit prompt through the composer -> runs built-in agent -> executes real editormcp scene_edit -> finishes.
	if err := f.ed.SubmitAgentPrompt("Add a slime at 180, 120"); err != nil {
		t.Fatalf("SubmitAgentPrompt: %v", err)
	}
	app.WaitUntil(5*time.Second, func() bool {
		return !f.ed.AgentWorking() && len(f.ed.AgentTurns()) == 1 && f.ed.AgentTurns()[0].Done
	})
	app.Settle()

	mu.Lock()
	if len(gotAPIKeys) != 2 || gotAPIKeys[0] != secretKey || gotAPIKeys[1] != secretKey {
		t.Fatalf("endpoint x-api-key headers = %d entries (matched=%v)", len(gotAPIKeys), len(gotAPIKeys) == 2 && gotAPIKeys[0] == secretKey)
	}
	if !sawSceneEditTool || !sawToolResult {
		t.Fatalf("sawSceneEditTool=%v sawToolResult=%v", sawSceneEditTool, sawToolResult)
	}
	if sawDeleteFileTool {
		t.Fatal("disabled default-off tool game_delete_file was advertised to built-in agent")
	}
	mu.Unlock()

	obj, _, _ := f.ed.FindObject("slime_builtin")
	if obj == nil || obj.Name != "Built-in Slime" || obj.X != 180 || obj.Y != 120 {
		t.Fatalf("slime_builtin not placed by built-in agent: %+v", obj)
	}
	if f.ed.History.Index() != beforeHist+1 {
		t.Fatalf("expected 1 undo step, history index=%d (was %d)", f.ed.History.Index(), beforeHist)
	}

	turns := f.ed.AgentTurns()
	if len(turns) != 1 || turns[0].Summary != "Placed slime_builtin at (180, 120)." || len(turns[0].Steps) != 1 || turns[0].Steps[0].Status != editor.AgentStepDone {
		t.Fatalf("unexpected turn state: %+v", turns)
	}
	if turns[0].InputTokens != 300 || turns[0].OutputTokens != 70 {
		t.Fatalf("usage tokens = %d/%d, want 300/70", turns[0].InputTokens, turns[0].OutputTokens)
	}

	// Save scene + project + editor settings, then verify secretKey appears in NO file under the project or user settings.
	if err := f.ed.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := f.ed.SaveSettings(); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	projectDir := filepath.Dir(f.ed.ProjectPath)
	if err := filepath.WalkDir(projectDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(b), secretKey) {
			t.Errorf("API key found in project file %s", p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(settingsPath); err == nil && strings.Contains(string(b), secretKey) {
		t.Errorf("API key found in editor settings file %s", settingsPath)
	}

	// Undo removes the built-in agent's edit in one step.
	if !f.ed.Undo() {
		t.Fatal("Undo failed")
	}
	if obj, _, _ := f.ed.FindObject("slime_builtin"); obj != nil {
		t.Fatalf("Undo did not remove slime_builtin: %+v", obj)
	}
}

// Verifies the built-in agent's MCP tool bridge enforces Settings › Tools toggles and Settings › Usage and privacy switches
// (default-off game_delete_file and GoSource=false).
func TestBuiltInAgentRespectsDisabledToolsAndPrivacy(t *testing.T) {
	f := openFixture(t)
	projectDir := filepath.Dir(f.ed.ProjectPath)
	if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\nconst secretLogic = 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var calls int
	var toolResults []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		calls++
		callNum := calls
		if callNum == 2 && len(payload.Messages) > 0 {
			toolResults = append(toolResults, string(payload.Messages[len(payload.Messages)-1].Content))
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if callNum == 1 {
			// Model attempts to read main.go (GoSource=false) AND invoke disabled game_delete_file.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "msg_priv_1",
				"model":       payload.Model,
				"stop_reason": "tool_use",
				"content": []map[string]any{
					{
						"type":  "tool_use",
						"id":    "toolu_go",
						"name":  editormcp.ToolSceneRead,
						"input": map[string]any{"file": "main.go"},
					},
					{
						"type":  "tool_use",
						"id":    "toolu_del",
						"name":  editor.ToolNameGameDeleteFile,
						"input": map[string]any{"file": "scenes/level.scene.yaml"},
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_priv_2",
			"model":       payload.Model,
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "Both calls were refused as expected."},
			},
		})
	}))
	defer srv.Close()

	f.ed.BuiltInAgentEndpoint = srv.URL
	f.ed.BuiltInHTTPClient = srv.Client()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1280, 800), nsgotest.Secrets(map[string]string{
		editor.AgentAPIKeySecretAccount: "sk-ant-privacy-check",
	}))

	s := editormcp.New(f.ed, editormcp.Options{Run: app.Post})
	t.Cleanup(func() { _ = s.Close() })

	if err := f.ed.SubmitAgentTaskPrompt(editor.AgentTaskPlaytest, "Inspect main.go and delete scene"); err != nil {
		t.Fatalf("SubmitAgentTaskPrompt: %v", err)
	}
	app.WaitUntil(5*time.Second, func() bool {
		return !f.ed.AgentWorking() && len(f.ed.AgentTurns()) == 1 && f.ed.AgentTurns()[0].Done
	})

	mu.Lock()
	defer mu.Unlock()
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_result turn, got %d", len(toolResults))
	}
	got := toolResults[0]
	if strings.Contains(got, "secretLogic") {
		t.Fatalf("Go source content leaked to remote model despite GoSource=false: %s", got)
	}
	if !strings.Contains(got, "Go source code") || !strings.Contains(got, "turned off") {
		t.Fatalf("expected privacy and disabled-tool refusals in tool results, got: %s", got)
	}
}
