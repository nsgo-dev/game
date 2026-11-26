package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mapGetenv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveAgentConfig(t *testing.T) {
	t.Run("auto selects anthropic from ANTHROPIC_API_KEY", func(t *testing.T) {
		cfg, err := ResolveAgentConfig(AgentConfig{}, mapGetenv(map[string]string{
			"ANTHROPIC_API_KEY": "sk-ant-test",
		}))
		if err != nil {
			t.Fatalf("ResolveAgentConfig: %v", err)
		}
		if cfg.Provider != ProviderAnthropic || cfg.Model != DefaultAnthropicModel || cfg.BaseURL != DefaultAnthropicBaseURL {
			t.Fatalf("unexpected resolved config: %+v", cfg)
		}
	})

	t.Run("auto selects openai from OPENAI_API_KEY", func(t *testing.T) {
		cfg, err := ResolveAgentConfig(AgentConfig{}, mapGetenv(map[string]string{
			"OPENAI_API_KEY":  "sk-openai-test",
			"OPENAI_BASE_URL": "https://proxy.example/v1/",
		}))
		if err != nil {
			t.Fatalf("ResolveAgentConfig: %v", err)
		}
		if cfg.Provider != ProviderOpenAI || cfg.Model != DefaultOpenAIModel || cfg.BaseURL != "https://proxy.example/v1" {
			t.Fatalf("unexpected resolved config: %+v", cfg)
		}
	})

	t.Run("auto infers anthropic from claude model with AGENT_EVAL_API_KEY", func(t *testing.T) {
		cfg, err := ResolveAgentConfig(AgentConfig{}, mapGetenv(map[string]string{
			"AGENT_EVAL_API_KEY":   "sk-generic",
			"AGENT_EVAL_MODEL":     "claude-3-7-sonnet",
			"AGENT_EVAL_MAX_TURNS": "12",
		}))
		if err != nil {
			t.Fatalf("ResolveAgentConfig: %v", err)
		}
		if cfg.Provider != ProviderAnthropic || cfg.Model != "claude-3-7-sonnet" || cfg.MaxTurns != 12 {
			t.Fatalf("unexpected resolved config: %+v", cfg)
		}
	})

	t.Run("auto defaults to openai-compatible for generic key and gateway URL", func(t *testing.T) {
		cfg, err := ResolveAgentConfig(AgentConfig{}, mapGetenv(map[string]string{
			"AGENT_EVAL_API_KEY":  "sk-router",
			"AGENT_EVAL_BASE_URL": "https://openrouter.ai/api/v1",
			"AGENT_EVAL_MODEL":    "deepseek/deepseek-chat",
		}))
		if err != nil {
			t.Fatalf("ResolveAgentConfig: %v", err)
		}
		if cfg.Provider != ProviderOpenAI || cfg.BaseURL != "https://openrouter.ai/api/v1" || cfg.Model != "deepseek/deepseek-chat" {
			t.Fatalf("unexpected resolved config: %+v", cfg)
		}
	})

	t.Run("missing keys return ErrAgentUnconfigured", func(t *testing.T) {
		for _, provider := range []string{"", ProviderAuto, ProviderAnthropic, ProviderOpenAI} {
			_, err := ResolveAgentConfig(AgentConfig{Provider: provider}, mapGetenv(nil))
			if !errors.Is(err, ErrAgentUnconfigured) {
				t.Errorf("provider=%q: got err=%v, want ErrAgentUnconfigured", provider, err)
			}
		}
	})

	t.Run("invalid provider or max turns return config error, not unconfigured", func(t *testing.T) {
		if _, err := ResolveAgentConfig(AgentConfig{Provider: "bogus"}, mapGetenv(nil)); err == nil || errors.Is(err, ErrAgentUnconfigured) {
			t.Errorf("bogus provider err=%v, want non-unconfigured error", err)
		}
		if _, err := ResolveAgentConfig(AgentConfig{}, mapGetenv(map[string]string{
			"OPENAI_API_KEY":       "sk-test",
			"AGENT_EVAL_MAX_TURNS": "zero",
		})); err == nil || errors.Is(err, ErrAgentUnconfigured) {
			t.Errorf("invalid max turns err=%v, want non-unconfigured error", err)
		}
	})
}

func TestSkipIfUnconfigured(t *testing.T) {
	root := testRepoRoot(t)
	tasksDir := filepath.Join(root, "tools", "agenteval", "tasks")

	for _, mode := range []string{ModeAgent, ModeCommand} {
		t.Run(mode+"_skipped", func(t *testing.T) {
			report, err := RunSuite(context.Background(), RunOptions{
				RepoRoot:           root,
				TasksDir:           tasksDir,
				Mode:               mode,
				SkipIfUnconfigured: true,
				Getenv:             mapGetenv(nil),
			})
			if err != nil {
				t.Fatalf("RunSuite(%s, skip): %v", mode, err)
			}
			if !report.Skipped || !report.Passed || report.SkipReason == "" {
				t.Fatalf("unexpected skipped report: %+v", report)
			}
			md := report.RenderMarkdown()
			if !strings.Contains(md, "SKIPPED") || !strings.Contains(md, mode) {
				t.Fatalf("markdown missing SKIPPED notice:\n%s", md)
			}
		})

		t.Run(mode+"_error_without_skip", func(t *testing.T) {
			_, err := RunSuite(context.Background(), RunOptions{
				RepoRoot: root,
				TasksDir: tasksDir,
				Mode:     mode,
				Getenv:   mapGetenv(nil),
			})
			if err == nil {
				t.Fatalf("RunSuite(%s) without credentials unexpectedly succeeded", mode)
			}
		})
	}
}

func TestCommandModeUsesEnvFallback(t *testing.T) {
	root := testRepoRoot(t)
	ref := filepath.Join(root, "tools", "agenteval", "testdata", "fixtures", "add-menu", "reference")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := RunSuite(ctx, RunOptions{
		RepoRoot: root,
		TasksDir: filepath.Join(root, "tools", "agenteval", "tasks"),
		Mode:     ModeCommand,
		Getenv: mapGetenv(map[string]string{
			"AGENT_EVAL_COMMAND": `cp -R '` + ref + `/.' .`,
		}),
		TaskFilter:       []string{"add-menu"},
		MinScoreOverride: 100,
		Timeout:          2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RunSuite(command env fallback): %v", err)
	}
	if !report.Passed || report.Tasks[0].Score != 100 {
		t.Fatalf("command env fallback failed:\n%s", report.RenderMarkdown())
	}
}

func readFixtureFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("readFixtureFiles(%s): %v", dir, err)
	}
	return files
}

func TestAgentModeAnthropicEndToEnd(t *testing.T) {
	root := testRepoRoot(t)
	refFiles := readFixtureFiles(t, filepath.Join(root, "tools", "agenteval", "testdata", "fixtures", "add-menu", "reference"))

	var turn atomic.Int32
	var sawSkillRead atomic.Bool
	var sawSecretScrubbed atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.Header.Get("x-api-key") != "sk-ant-mock" || r.Header.Get("anthropic-version") != anthropicAPIVersion {
			http.Error(w, "missing anthropic headers", http.StatusUnauthorized)
			return
		}
		var req struct {
			Model    string `json:"model"`
			System   string `json:"system"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !strings.Contains(req.System, "skills/nsgo-game/SKILL.md") {
			http.Error(w, "system prompt missing skill reference", http.StatusBadRequest)
			return
		}

		n := turn.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch n {
		case 1:
			// Verify initial user prompt includes docs, skill, and MCP verification guidance.
			userText := string(req.Messages[0].Content)
			if !strings.Contains(userText, "skills/nsgo-game/SKILL.md") || !strings.Contains(userText, "docs/en/mcp.md") || !strings.Contains(userText, "mcp.json") {
				http.Error(w, "user prompt missing docs/skill/mcp context", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stop_reason": "tool_use",
				"usage":       map[string]int{"input_tokens": 120, "output_tokens": 45},
				"content": []map[string]any{
					{"type": "text", "text": "Reading the staged skill and docs first."},
					{
						"type": "tool_use",
						"id":   "tu_1",
						"name": "list_files",
						"input": map[string]any{
							"scope": "context",
							"path":  "skills/nsgo-game",
						},
					},
					{
						"type": "tool_use",
						"id":   "tu_2",
						"name": "read_file",
						"input": map[string]any{
							"scope":      "context",
							"path":       "skills/nsgo-game/SKILL.md",
							"start_line": 1,
							"end_line":   15,
						},
					},
				},
			})
		case 2:
			// Confirm the tool results from turn 1 contained SKILL.md content.
			last := string(req.Messages[len(req.Messages)-1].Content)
			if strings.Contains(last, "name: nsgo-game") && strings.Contains(last, "references/mcp-tools.md") {
				sawSkillRead.Store(true)
			}
			// Write the reference menu files and run nsgo check + verify secret scrubbing.
			blocks := []map[string]any{
				{"type": "text", "text": "Writing the menu scene and Go code, then running nsgo check."},
			}
			idx := 10
			for rel, content := range refFiles {
				if rel == "go.mod" {
					continue
				}
				blocks = append(blocks, map[string]any{
					"type": "tool_use",
					"id":   "tu_w_" + rel,
					"name": "write_file",
					"input": map[string]any{
						"path":    rel,
						"content": content,
					},
				})
				idx++
			}
			blocks = append(blocks, map[string]any{
				"type": "tool_use",
				"id":   "tu_cmd",
				"name": "run_command",
				"input": map[string]any{
					"command": `test -z "$ANTHROPIC_API_KEY" && test -z "$AGENT_EVAL_API_KEY" && test -s "$AGENTEVAL_SKILL_FILE" && test -s "$AGENTEVAL_MCP_CONFIG" && go tool nsgo check && echo SCRUB_OK`,
				},
			})
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stop_reason": "tool_use",
				"usage":       map[string]int{"input_tokens": 300, "output_tokens": 220},
				"content":     blocks,
			})
		default:
			last := string(req.Messages[len(req.Messages)-1].Content)
			if strings.Contains(last, "SCRUB_OK") {
				sawSecretScrubbed.Store(true)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stop_reason": "end_turn",
				"usage":       map[string]int{"input_tokens": 180, "output_tokens": 30},
				"content": []map[string]any{
					{"type": "text", "text": "Implemented the title/pause menu and verified with go tool nsgo check."},
				},
			})
		}
	}))
	defer srv.Close()

	// Set sensitive env vars in process to verify run_command scrubs them.
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret-must-not-leak")
	t.Setenv("AGENT_EVAL_API_KEY", "sk-secret-must-not-leak-2")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	report, err := RunSuite(ctx, RunOptions{
		RepoRoot: root,
		TasksDir: filepath.Join(root, "tools", "agenteval", "tasks"),
		Mode:     ModeAgent,
		Agent: AgentConfig{
			Provider:     ProviderAnthropic,
			Model:        "claude-test",
			BaseURL:      srv.URL,
			APIKey:       "sk-ant-mock",
			MaxTurns:     6,
			HTTPClient:   srv.Client(),
			RetryBackoff: -1,
		},
		TaskFilter:       []string{"add-menu"},
		MinScoreOverride: 100,
		Timeout:          2 * time.Minute,
		AgentTimeout:     2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RunSuite(agent/anthropic): %v", err)
	}
	if !sawSkillRead.Load() {
		t.Fatal("mock agent did not observe staged skills/nsgo-game/SKILL.md content")
	}
	if !sawSecretScrubbed.Load() {
		t.Fatal("run_command tool did not scrub secrets or pass nsgo check")
	}
	if !report.Passed || report.TotalTasks != 1 || report.Tasks[0].Score != 100 {
		t.Fatalf("anthropic agent run failed:\n%s", report.RenderMarkdown())
	}
	if report.Provider != ProviderAnthropic || report.Model != "claude-test" {
		t.Fatalf("report metadata mismatch: provider=%q model=%q", report.Provider, report.Model)
	}
	tr := report.Tasks[0]
	if tr.AgentTurns != 3 || tr.AgentToolCalls < 4 || tr.AgentPromptTokens != 600 || tr.AgentCompletionTokens != 295 {
		t.Fatalf("unexpected agent telemetry: %+v", tr)
	}

	md := report.RenderMarkdown()
	for _, want := range []string{
		"Agent Evaluation Suite (agent)",
		"`anthropic`",
		"`claude-test`",
		"600 prompt / 295 completion",
		"3 turns",
		"`add-menu`",
		"Criterion Breakdown",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}

	mdPath := filepath.Join(t.TempDir(), "nested", "summary.md")
	if err := report.WriteMarkdown(mdPath); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	rawMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawMD) != md {
		t.Fatalf("WriteMarkdown output mismatch")
	}
}

func TestAgentModeOpenAIEndToEndWithRetry(t *testing.T) {
	root := testRepoRoot(t)
	refFiles := readFixtureFiles(t, filepath.Join(root, "tools", "agenteval", "testdata", "fixtures", "add-menu", "reference"))

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-openai-mock" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		n := attempts.Add(1)
		// Simulate a transient 429 on the very first attempt to exercise retry logic.
		if n == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if n == 2 {
			var calls []map[string]any
			for rel, content := range refFiles {
				if rel == "go.mod" {
					continue
				}
				argsBytes, _ := json.Marshal(map[string]any{
					"path":    rel,
					"content": content,
				})
				calls = append(calls, map[string]any{
					"id":   "call_" + rel,
					"type": "function",
					"function": map[string]any{
						"name":      "write_file",
						"arguments": string(argsBytes),
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"finish_reason": "tool_calls",
						"message": map[string]any{
							"role":       "assistant",
							"content":    "Writing files for add-menu.",
							"tool_calls": calls,
						},
					},
				},
				"usage": map[string]int{"prompt_tokens": 200, "completion_tokens": 150},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": "Done.",
					},
				},
			},
			"usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 10},
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	report, err := RunSuite(ctx, RunOptions{
		RepoRoot: root,
		TasksDir: filepath.Join(root, "tools", "agenteval", "tasks"),
		Mode:     ModeAgent,
		Agent: AgentConfig{
			Provider:     ProviderOpenAI,
			Model:        "gpt-test",
			BaseURL:      srv.URL + "/v1",
			APIKey:       "sk-openai-mock",
			MaxTurns:     5,
			HTTPClient:   srv.Client(),
			RetryBackoff: time.Millisecond,
		},
		TaskFilter:       []string{"add-menu"},
		MinScoreOverride: 100,
		Timeout:          2 * time.Minute,
		AgentTimeout:     2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RunSuite(agent/openai): %v", err)
	}
	if !report.Passed || report.Tasks[0].Score != 100 {
		t.Fatalf("openai agent run failed:\n%s", report.RenderMarkdown())
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 HTTP attempts (1 retry + 2 turns), got %d", attempts.Load())
	}
	if report.Provider != ProviderOpenAI || report.Model != "gpt-test" {
		t.Fatalf("unexpected provider/model: %q/%q", report.Provider, report.Model)
	}
}

func TestAgentModeFailureAndMaxTurnsStillGrade(t *testing.T) {
	root := testRepoRoot(t)
	tasksDir := filepath.Join(root, "tools", "agenteval", "tasks")

	t.Run("provider 400 error records agent_error and grades starter", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "invalid request payload", http.StatusBadRequest)
		}))
		defer srv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		report, err := RunSuite(ctx, RunOptions{
			RepoRoot: root,
			TasksDir: tasksDir,
			Mode:     ModeAgent,
			Agent: AgentConfig{
				Provider:     ProviderOpenAI,
				Model:        "gpt-bad",
				BaseURL:      srv.URL,
				APIKey:       "sk-test",
				HTTPClient:   srv.Client(),
				RetryBackoff: -1,
			},
			TaskFilter:       []string{"add-menu"},
			MinScoreOverride: 100,
			Timeout:          2 * time.Minute,
		})
		if err != nil {
			t.Fatalf("RunSuite: %v", err)
		}
		tr := report.Tasks[0]
		if tr.Passed {
			t.Fatalf("expected starter to fail after agent error")
		}
		if !strings.Contains(tr.AgentError, "HTTP 400") {
			t.Fatalf("AgentError = %q, want HTTP 400", tr.AgentError)
		}
		if tr.Score == 0 {
			t.Fatalf("expected grader to still award starter partial credit, got 0")
		}
		md := report.RenderMarkdown()
		if !strings.Contains(md, "(agent error)") || !strings.Contains(md, "Agent error:") {
			t.Fatalf("markdown missing agent error visibility:\n%s", md)
		}
	})

	t.Run("max turns limit stops loop and records error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"finish_reason": "tool_calls",
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call_loop",
									"type": "function",
									"function": map[string]any{
										"name":      "list_files",
										"arguments": `{"scope":"workspace"}`,
									},
								},
							},
						},
					},
				},
			})
		}))
		defer srv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		report, err := RunSuite(ctx, RunOptions{
			RepoRoot: root,
			TasksDir: tasksDir,
			Mode:     ModeAgent,
			Agent: AgentConfig{
				Provider:     ProviderOpenAI,
				Model:        "gpt-loop",
				BaseURL:      srv.URL,
				APIKey:       "sk-test",
				MaxTurns:     1,
				HTTPClient:   srv.Client(),
				RetryBackoff: -1,
			},
			TaskFilter:       []string{"add-menu"},
			MinScoreOverride: 100,
			Timeout:          2 * time.Minute,
		})
		if err != nil {
			t.Fatalf("RunSuite: %v", err)
		}
		tr := report.Tasks[0]
		if tr.AgentTurns != 1 || tr.AgentToolCalls != 1 {
			t.Fatalf("expected 1 turn and 1 tool call, got %d turns / %d calls", tr.AgentTurns, tr.AgentToolCalls)
		}
		if !strings.Contains(tr.AgentError, "max turns (1)") {
			t.Fatalf("AgentError = %q, want max turns message", tr.AgentError)
		}
	})
}

func TestAgentToolsIsolationAndErrors(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	ctxDir := filepath.Join(tmp, "context")
	outside := filepath.Join(tmp, "secret.txt")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("alpha\nbeta\nalpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	env := agentEnv{workspaceDir: ws, contextDir: ctxDir, task: &Task{ID: "t", Title: "T"}}

	// Path traversal & symlink reads must be refused.
	for _, args := range []string{
		`{"scope":"workspace","path":"../secret.txt"}`,
		`{"scope":"workspace","path":"/etc/passwd"}`,
		`{"scope":"workspace","path":"escape.txt"}`,
		`{"scope":"bogus","path":"hello.txt"}`,
	} {
		out, isErr := executeAgentTool(context.Background(), env, agentToolCall{
			Name:      "read_file",
			Arguments: json.RawMessage(args),
		})
		if !isErr {
			t.Errorf("read_file(%s) succeeded unexpectedly: %s", args, out)
		}
	}

	// Symlink write must be refused.
	if out, isErr := executeAgentTool(context.Background(), env, agentToolCall{
		Name:      "write_file",
		Arguments: json.RawMessage(`{"path":"escape.txt","content":"pwn"}`),
	}); !isErr {
		t.Errorf("write_file symlink succeeded unexpectedly: %s", out)
	}

	// Ambiguous edit_file must fail; replace_all succeeds.
	if _, isErr := executeAgentTool(context.Background(), env, agentToolCall{
		Name:      "edit_file",
		Arguments: json.RawMessage(`{"path":"hello.txt","old_string":"alpha","new_string":"gamma"}`),
	}); !isErr {
		t.Error("ambiguous edit_file succeeded unexpectedly")
	}
	if out, isErr := executeAgentTool(context.Background(), env, agentToolCall{
		Name:      "edit_file",
		Arguments: json.RawMessage(`{"path":"hello.txt","old_string":"alpha","new_string":"gamma","replace_all":true}`),
	}); isErr || !strings.Contains(out, "2 occurrence(s)") {
		t.Errorf("replace_all edit_file failed: isErr=%v out=%q", isErr, out)
	}

	// Unknown tool and malformed JSON return graceful tool errors.
	if _, isErr := executeAgentTool(context.Background(), env, agentToolCall{Name: "nope", Arguments: json.RawMessage(`{}`)}); !isErr {
		t.Error("unknown tool did not return error")
	}
	if _, isErr := executeAgentTool(context.Background(), env, agentToolCall{Name: "read_file", Arguments: json.RawMessage(`{bad`)}); !isErr {
		t.Error("malformed args did not return error")
	}
}

func TestRunCLISkipAndMarkdownOut(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AGENT_EVAL_API_KEY", "")
	t.Setenv("AGENT_EVAL_PROVIDER", "")
	t.Setenv("AGENT_EVAL_COMMAND", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "report.json")
	mdPath := filepath.Join(dir, "report.md")
	summaryPath := filepath.Join(dir, "step-summary.md")

	err := runCLI([]string{
		"-mode=agent",
		"-skip-if-unconfigured",
		"-out", jsonPath,
		"-markdown-out", mdPath,
		"-summary", summaryPath,
	})
	if err != nil {
		t.Fatalf("runCLI(-mode=agent -skip-if-unconfigured): %v", err)
	}

	rawJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SuiteReport
	if err := json.Unmarshal(rawJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Skipped || !decoded.Passed || decoded.Mode != ModeAgent {
		t.Fatalf("unexpected decoded CLI report: %+v", decoded)
	}

	rawMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	rawSummary, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawMD), "SKIPPED") || !strings.Contains(string(rawSummary), "SKIPPED") {
		t.Fatalf("markdown/summary missing SKIPPED:\nmd=%s\nsummary=%s", rawMD, rawSummary)
	}
}
