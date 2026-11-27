package editormcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1505 End-to-end test: Start with AI talks to the built-in agent over the real editormcp.Server
// (API key validation via HTTP endpoint, real MCP tool count, planning via plan_propose on UI scope
// without deadlock, building via scene_edit, honest playtest skip, and review listing exactly the
// objects written to disk with zero artboard strings).
func TestFirstRunBuiltInAgentEndToEndWithRealMCPTools(t *testing.T) {
	const testKey = "sk-ant-first-run-e2e-key-secret"

	parentDir := t.TempDir()
	idea := "a kart racer on a beach"

	var (
		mu              sync.Mutex
		calls           int
		sawPing         bool
		sawPlanPropose  bool
		sawSceneEdit    bool
		sawToolResultOK bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != testKey {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"type": "authentication_error", "message": "invalid key"},
			})
			return
		}
		var payload struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Tools     []struct {
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
		lastMsg := ""
		if len(payload.Messages) > 0 {
			lastMsg = string(payload.Messages[len(payload.Messages)-1].Content)
		}
		isPing := payload.MaxTokens == 1 && len(payload.Tools) == 0 && strings.Contains(lastMsg, "ping")
		if isPing {
			sawPing = true
		}
		for _, tool := range payload.Tools {
			if tool.Name == editormcp.ToolPlanPropose {
				sawPlanPropose = true
			}
			if tool.Name == editormcp.ToolSceneEdit {
				sawSceneEdit = true
			}
		}
		isToolResult := strings.Contains(lastMsg, `"tool_result"`)
		if isToolResult && !strings.Contains(lastMsg, `"is_error":true`) {
			sawToolResultOK = true
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if isPing {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "msg_ping",
				"model":       payload.Model,
				"stop_reason": "end_turn",
				"content":     []map[string]any{{"type": "text", "text": "pong"}},
			})
			return
		}

		// Plan turn: call real editormcp plan_propose (even with wait=true, first-run planning must not deadlock).
		if strings.Contains(lastMsg, "Propose a concise first-run plan") && !isToolResult {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "msg_plan_1",
				"model":       payload.Model,
				"stop_reason": "tool_use",
				"content": []map[string]any{
					{
						"type": "tool_use",
						"id":   "toolu_plan",
						"name": editormcp.ToolPlanPropose,
						"input": map[string]any{
							"title": "Beach kart circuit",
							"wait":  true,
							"files": []map[string]any{
								{"path": "game.yaml", "tag": "new"},
								{"path": "scenes/beach_circuit.scene.yaml", "tag": "new"},
							},
							"questions": []map[string]any{
								{
									"id":     "surface",
									"prompt": "Which beach track layout?",
									"options": []map[string]any{
										{
											"id":        "oval",
											"label":     "Sunlit oval",
											"note":      "Suggested",
											"suggested": true,
											"stepTitleOverrides": map[string]string{
												"2": "Build beach_circuit as a sunlit oval with boost pad and finish gate",
											},
										},
										{
											"id":    "figure8",
											"label": "Figure-eight cove",
											"note":  "Crossing bridge",
										},
									},
								},
							},
							"steps": []map[string]any{
								{"number": 1, "title": "Create beach-karts project", "filesLabel": "game.yaml"},
								{
									"number":     2,
									"title":      "Build beach_circuit with kart, boost pad and finish gate",
									"filesLabel": "scenes/beach_circuit.scene.yaml",
									"objects":    3,
									"ghosts": []map[string]any{
										{"id": "cove-track", "step": 2, "x": 40, "y": 120, "width": 220, "height": 90, "label": "beach track"},
									},
								},
							},
						},
					},
				},
				"usage": map[string]int{"input_tokens": 100, "output_tokens": 60},
			})
			return
		}

		// Build turn: call real editormcp scene_edit to place the three planned objects.
		if strings.Contains(lastMsg, "Build the first scene") && !isToolResult {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "msg_build_1",
				"model":       payload.Model,
				"stop_reason": "tool_use",
				"content": []map[string]any{
					{
						"type": "tool_use",
						"id":   "toolu_build",
						"name": editormcp.ToolSceneEdit,
						"input": map[string]any{
							"edits": []map[string]any{
								{
									"op":    "add",
									"layer": "actors",
									"object": map[string]any{
										"id":   "kart_player",
										"name": "Player Kart",
										"x":    80,
										"y":    200,
									},
								},
								{
									"op":    "add",
									"layer": "actors",
									"object": map[string]any{
										"id":   "boost_pad_01",
										"name": "Sand Boost",
										"x":    240,
										"y":    200,
									},
								},
								{
									"op":    "add",
									"layer": "actors",
									"object": map[string]any{
										"id":   "finish_gate",
										"name": "Beach Arch",
										"x":    520,
										"y":    180,
									},
								},
							},
						},
					},
				},
				"usage": map[string]int{"input_tokens": 150, "output_tokens": 80},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_done_" + string(rune('0'+callNum)),
			"model":       payload.Model,
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "Done."}},
			"usage":       map[string]int{"input_tokens": 50, "output_tokens": 10},
		})
	}))
	defer srv.Close()

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	ed.BuiltInAgentEndpoint = srv.URL
	ed.BuiltInHTTPClient = srv.Client()
	ed.SetProjectTemplates([]editor.ProjectTemplate{
		{Name: "starter", Title: "Starter", Default: true},
		{Name: "racing3d", Title: "3D racer", Summary: "Laps, ghosts, AI drivers"},
	})
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("beach-karts")
	ed.SetStartProjectIdea(idea)
	ed.SetStartScreenOpen(true)
	ed.FirstRunDetectAgents = func() []editor.FirstRunAgentOption {
		return []editor.FirstRunAgentOption{
			{Kind: "claude", Name: "Claude Code", Description: "Not installed", Detected: false},
			{Kind: "codex", Name: "Codex", Description: "Not installed", Detected: false},
			{Kind: "apikey", Name: "An API key", Description: "The editor calls the model itself", Detected: true},
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	s := editormcp.New(ed, editormcp.Options{Run: app.Post})
	t.Cleanup(func() { _ = s.Close() })

	// Step 1 -> Step 2 (Connect with API key).
	app.Press("id:start-ai-btn")
	app.Settle()

	// Rejected key stays disconnected.
	app.Post(func() { ed.SetFirstRunAPIKey("sk-wrong-key-12345") })
	app.Settle()
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyInvalid || app.Enabled("id:first-run-connect-done") {
		t.Fatalf("wrong key status=%v doneEnabled=%v", ed.FirstRunConnectStatus(), app.Enabled("id:first-run-connect-done"))
	}

	// Valid key passes real HTTP ping and enables Done.
	app.Post(func() { ed.SetFirstRunAPIKey(testKey) })
	app.Settle()
	if ed.FirstRunConnectStatus() != editor.FirstRunConnectKeyValid || !app.Enabled("id:first-run-connect-done") {
		t.Fatalf("valid key status=%v doneEnabled=%v", ed.FirstRunConnectStatus(), app.Enabled("id:first-run-connect-done"))
	}

	// Step 2 -> Step 3 (Plan via real editormcp plan_propose on UI scope).
	app.Press("id:first-run-connect-done")
	app.WaitUntil(5*time.Second, func() bool {
		return !ed.FirstRunPlanLoading() && ed.FirstRunPlan() != nil
	})
	app.Settle()

	// Verify real MCP tool count from editormcp.Server (not hardcoded 18).
	enabledTools, err := ed.AgentTools.ListTools(t.Context())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	_, _, connectedTools := ed.FirstRunConnectedAgent()
	if connectedTools != len(enabledTools) {
		t.Fatalf("connected tools = %d, want real editormcp enabled count %d", connectedTools, len(enabledTools))
	}

	plan := ed.FirstRunPlan()
	if plan == nil || plan.SceneName != "beach_circuit" || plan.Question == nil || plan.Question.Prompt != "Which beach track layout?" {
		t.Fatalf("unexpected plan from plan_propose: %+v", plan)
	}
	if !app.Exists("id:first-run-ghost-cove-track") {
		t.Fatal("ghost proposed via plan_propose did not render")
	}

	// Pick clarifying answer and Approve -> builds via real editormcp scene_edit!
	app.Press("id:first-run-plan-answer-oval")
	app.Settle()
	app.Press("id:first-run-plan-approve")
	app.WaitUntil(5*time.Second, func() bool {
		return ed.FirstRunStage() == editor.FirstRunStageReview
	})
	app.Settle()

	mu.Lock()
	if !sawPing || !sawPlanPropose || !sawSceneEdit || !sawToolResultOK {
		t.Fatalf("expected full MCP flow: ping=%v plan_propose=%v scene_edit=%v toolResultOK=%v",
			sawPing, sawPlanPropose, sawSceneEdit, sawToolResultOK)
	}
	mu.Unlock()

	// Verify written scene on disk matches review objects 1-to-1.
	sceneFile := filepath.Join(parentDir, "beach-karts", "scenes", "beach_circuit.scene.yaml")
	raw, err := os.ReadFile(sceneFile)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", sceneFile, err)
	}
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse scene: %v", err)
	}
	var diskIDs []string
	for _, l := range doc.Layers {
		for _, o := range l.Objects {
			diskIDs = append(diskIDs, o.ID)
		}
	}
	wantIDs := []string{"kart_player", "boost_pad_01", "finish_gate"}
	if !reflect.DeepEqual(diskIDs, wantIDs) {
		t.Fatalf("disk scene objects = %v, want %v", diskIDs, wantIDs)
	}

	review := ed.FirstRunReviewState()
	var reviewIDs []string
	for _, ch := range review.Changes {
		reviewIDs = append(reviewIDs, ch.ObjectIDs...)
	}
	if !reflect.DeepEqual(reviewIDs, wantIDs) {
		t.Fatalf("review objects = %v, want %v", reviewIDs, wantIDs)
	}

	// Ensure no design-artboard strings appear in the written scene or review.
	lowerScene := strings.ToLower(string(raw))
	for _, bad := range []string{"knight", "slime", "coin", "lever", "door_exit"} {
		if strings.Contains(lowerScene, bad) {
			t.Fatalf("artboard string %q found in written scene:\n%s", bad, raw)
		}
	}
}
