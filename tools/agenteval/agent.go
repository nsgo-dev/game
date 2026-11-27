package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// ProviderAuto selects Anthropic or OpenAI automatically from the configured API key, model, or base URL.
	ProviderAuto = "auto"
	// ProviderAnthropic uses the Anthropic Messages API (/v1/messages).
	ProviderAnthropic = "anthropic"
	// ProviderOpenAI uses the OpenAI-compatible Chat Completions API (/chat/completions).
	ProviderOpenAI = "openai"

	// DefaultAnthropicBaseURL is the default Anthropic API base URL.
	DefaultAnthropicBaseURL = "https://api.anthropic.com"
	// DefaultAnthropicModel is the default Anthropic model when AGENT_EVAL_MODEL is unset.
	DefaultAnthropicModel = "claude-sonnet-4-20250514"
	// DefaultOpenAIBaseURL is the default OpenAI API base URL.
	DefaultOpenAIBaseURL = "https://api.openai.com/v1"
	// DefaultOpenAIModel is the default OpenAI model when AGENT_EVAL_MODEL is unset.
	DefaultOpenAIModel = "gpt-4.1"

	// DefaultAgentMaxTurns bounds the number of model turns per task in agent mode.
	DefaultAgentMaxTurns = 25
	// DefaultAgentCommandTimeout bounds a single run_command tool call inside agent mode.
	DefaultAgentCommandTimeout = 2 * time.Minute

	anthropicAPIVersion     = "2023-06-01"
	agentMaxResponseBytes   = 4 << 20
	agentMaxReadFileBytes   = 64 << 10
	agentMaxCmdOutputBytes  = 32 << 10
	agentMaxTranscriptBytes = 64 << 10
	agentMaxHTTPRetries     = 2
)

// ErrAgentUnconfigured is returned when mode=agent or mode=command has no API key or command configured.
var ErrAgentUnconfigured = errors.New("coding agent is not configured")

// AgentConfig configures the built-in coding agent driver used by ModeAgent.
type AgentConfig struct {
	Provider       string        // auto, anthropic, or openai
	Model          string        // optional model override
	BaseURL        string        // optional API base URL override
	APIKey         string        // optional API key override (otherwise read from env)
	MaxTurns       int           // maximum model turns per task (default DefaultAgentMaxTurns)
	CommandTimeout time.Duration // per-command timeout for the run_command tool (default DefaultAgentCommandTimeout)
	HTTPClient     *http.Client  // optional HTTP client (defaults to http.DefaultClient)
	RetryBackoff   time.Duration // base backoff for HTTP 429/5xx retries (default 500ms; negative disables delay)
}

// AgentRunResult summarizes one built-in coding agent run on a single task.
type AgentRunResult struct {
	Provider         string
	Model            string
	Turns            int
	ToolCalls        int
	PromptTokens     int
	CompletionTokens int
	Output           string
	Err              error
}

// ResolveAgentConfig fills unset fields in cfg from environment variables via getenv and validates the result.
func ResolveAgentConfig(cfg AgentConfig, getenv func(string) string) (AgentConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	resolved := cfg
	resolved.Provider = strings.ToLower(strings.TrimSpace(firstNonEmpty(cfg.Provider, getenv("AGENT_EVAL_PROVIDER"), ProviderAuto)))
	resolved.Model = strings.TrimSpace(firstNonEmpty(cfg.Model, getenv("AGENT_EVAL_MODEL")))
	resolved.BaseURL = strings.TrimSpace(firstNonEmpty(cfg.BaseURL, getenv("AGENT_EVAL_BASE_URL")))
	resolved.APIKey = strings.TrimSpace(cfg.APIKey)

	if resolved.MaxTurns <= 0 {
		if raw := strings.TrimSpace(getenv("AGENT_EVAL_MAX_TURNS")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				return AgentConfig{}, fmt.Errorf("invalid AGENT_EVAL_MAX_TURNS %q: must be a positive integer", raw)
			}
			resolved.MaxTurns = n
		} else {
			resolved.MaxTurns = DefaultAgentMaxTurns
		}
	}
	if resolved.CommandTimeout <= 0 {
		resolved.CommandTimeout = DefaultAgentCommandTimeout
	}
	if resolved.HTTPClient == nil {
		resolved.HTTPClient = http.DefaultClient
	}
	if resolved.RetryBackoff == 0 {
		resolved.RetryBackoff = 500 * time.Millisecond
	}

	genericKey := strings.TrimSpace(getenv("AGENT_EVAL_API_KEY"))
	anthropicKey := strings.TrimSpace(getenv("ANTHROPIC_API_KEY"))
	openaiKey := strings.TrimSpace(getenv("OPENAI_API_KEY"))

	switch resolved.Provider {
	case ProviderAuto:
		switch {
		case resolved.APIKey != "" || genericKey != "":
			if resolved.APIKey == "" {
				resolved.APIKey = genericKey
			}
			if looksAnthropic(resolved.BaseURL, resolved.Model) {
				resolved.Provider = ProviderAnthropic
			} else {
				resolved.Provider = ProviderOpenAI
			}
		case anthropicKey != "":
			resolved.Provider = ProviderAnthropic
			resolved.APIKey = anthropicKey
		case openaiKey != "":
			resolved.Provider = ProviderOpenAI
			resolved.APIKey = openaiKey
		default:
			return AgentConfig{}, fmt.Errorf("%w: set ANTHROPIC_API_KEY, OPENAI_API_KEY, AGENT_EVAL_API_KEY, or AGENT_EVAL_COMMAND", ErrAgentUnconfigured)
		}
	case ProviderAnthropic:
		resolved.APIKey = firstNonEmpty(resolved.APIKey, genericKey, anthropicKey)
		if resolved.APIKey == "" {
			return AgentConfig{}, fmt.Errorf("%w: provider anthropic requires ANTHROPIC_API_KEY or AGENT_EVAL_API_KEY", ErrAgentUnconfigured)
		}
	case ProviderOpenAI:
		resolved.APIKey = firstNonEmpty(resolved.APIKey, genericKey, openaiKey)
		if resolved.APIKey == "" {
			return AgentConfig{}, fmt.Errorf("%w: provider openai requires OPENAI_API_KEY or AGENT_EVAL_API_KEY", ErrAgentUnconfigured)
		}
	default:
		return AgentConfig{}, fmt.Errorf("unknown agent provider %q (want %q, %q, or %q)", resolved.Provider, ProviderAuto, ProviderAnthropic, ProviderOpenAI)
	}

	switch resolved.Provider {
	case ProviderAnthropic:
		if resolved.Model == "" {
			resolved.Model = DefaultAnthropicModel
		}
		if resolved.BaseURL == "" {
			resolved.BaseURL = strings.TrimSpace(firstNonEmpty(getenv("ANTHROPIC_BASE_URL"), DefaultAnthropicBaseURL))
		}
	case ProviderOpenAI:
		if resolved.Model == "" {
			resolved.Model = DefaultOpenAIModel
		}
		if resolved.BaseURL == "" {
			resolved.BaseURL = strings.TrimSpace(firstNonEmpty(getenv("OPENAI_BASE_URL"), DefaultOpenAIBaseURL))
		}
	}
	resolved.BaseURL = strings.TrimRight(resolved.BaseURL, "/")
	return resolved, nil
}

func looksAnthropic(baseURL, model string) bool {
	return strings.Contains(strings.ToLower(baseURL), "anthropic") ||
		strings.HasPrefix(strings.ToLower(model), "claude")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type agentToolSpec struct {
	Name        string
	Description string
	Schema      map[string]any
}

var builtinAgentTools = []agentToolSpec{
	{
		Name:        "list_files",
		Description: "List relative file paths in the game workspace or staged documentation/skill context.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scope": map[string]any{
					"type":        "string",
					"enum":        []string{"workspace", "context"},
					"description": "Directory to list: 'workspace' for the game project being edited, or 'context' for staged docs and the nsgo-game skill.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional relative subdirectory inside the chosen scope.",
				},
			},
			"required": []string{"scope"},
		},
	},
	{
		Name:        "read_file",
		Description: "Read a text file from the game workspace or staged docs/skill context with line numbers.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scope": map[string]any{
					"type":        "string",
					"enum":        []string{"workspace", "context"},
					"description": "Where to read from: 'workspace' or 'context'.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Relative forward-slash file path within the chosen scope.",
				},
				"start_line": map[string]any{
					"type":        "integer",
					"description": "Optional 1-based starting line number.",
				},
				"end_line": map[string]any{
					"type":        "integer",
					"description": "Optional 1-based inclusive ending line number.",
				},
			},
			"required": []string{"scope", "path"},
		},
	},
	{
		Name:        "write_file",
		Description: "Create or overwrite a file in the game workspace (creating parent directories as needed).",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative forward-slash file path inside the workspace.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Full file contents to write.",
				},
			},
			"required": []string{"path", "content"},
		},
	},
	{
		Name:        "edit_file",
		Description: "Replace an exact text snippet inside an existing workspace file.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative forward-slash file path inside the workspace.",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "Exact existing text to replace.",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "Replacement text.",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "If true, replace all occurrences; otherwise old_string must appear exactly once.",
				},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
	},
	{
		Name:        "run_command",
		Description: "Run a shell command in the game workspace with GOWORK configured (for example `go tool nsgo check` or `go tool nsgo test ./...`).",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command executed via `sh -c` in the workspace directory.",
				},
			},
			"required": []string{"command"},
		},
	},
}

type agentToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type agentTurnReply struct {
	Text             string
	ToolCalls        []agentToolCall
	PromptTokens     int
	CompletionTokens int
	StopReason       string
}

type agentEnv struct {
	workspaceDir  string
	contextDir    string
	skillDir      string
	mcpConfigPath string
	workFile      string
	promptPath    string
	task          *Task
	cmdTimeout    time.Duration
}

const agentSystemPrompt = `You are a coding agent solving a Go game-development task on NSGo (` + "`github.com/nsgo-dev/game`" + `) inside an isolated workspace.

Workflow:
1. Read the staged Agent Skill (` + "`skills/nsgo-game/SKILL.md`" + ` and relevant files in ` + "`skills/nsgo-game/references/`" + `) and the required docs in scope="context".
2. Inspect the starter files in scope="workspace" using list_files and read_file.
3. Implement all requirements cleanly in Go and plain-text versioned YAML (version: 1, no unknown fields, deterministic formatting).
4. Verify your solution with run_command using ` + "`go tool nsgo check`" + ` and ` + "`go tool nsgo test ./...`" + ` (write or run headless nsgotest + game/mcp checks where helpful).
5. When everything passes, stop calling tools and give a brief summary of your changes and verification results.`

// runBuiltinAgent drives a multi-turn tool-use loop against the configured LLM provider to solve task in workspaceDir.
func runBuiltinAgent(ctx context.Context, cfg AgentConfig, env agentEnv, prompt string) AgentRunResult {
	res := AgentRunResult{
		Provider: cfg.Provider,
		Model:    cfg.Model,
	}

	var transcript bytes.Buffer
	appendTranscript := func(s string) {
		if transcript.Len() >= agentMaxTranscriptBytes {
			return
		}
		remain := agentMaxTranscriptBytes - transcript.Len()
		if len(s) > remain {
			transcript.WriteString(s[:remain])
			transcript.WriteString("\n...[transcript truncated]")
			return
		}
		transcript.WriteString(s)
	}

	wsFiles, _ := listScopedFiles(env.workspaceDir, "")
	userMsg := prompt
	if len(wsFiles) > 0 {
		var b strings.Builder
		b.WriteString(prompt)
		b.WriteString("\n## Starter Workspace Files\n\n")
		for _, f := range wsFiles {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		userMsg = b.String()
	}

	switch cfg.Provider {
	case ProviderAnthropic:
		runAnthropicLoop(ctx, cfg, env, userMsg, &res, appendTranscript)
	case ProviderOpenAI:
		runOpenAILoop(ctx, cfg, env, userMsg, &res, appendTranscript)
	default:
		res.Err = fmt.Errorf("unsupported agent provider %q", cfg.Provider)
	}

	res.Output = strings.TrimSpace(transcript.String())
	return res
}

// --- Anthropic Messages API ---

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Tools     []anthropicTool    `json:"tools"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func runAnthropicLoop(ctx context.Context, cfg AgentConfig, env agentEnv, userMsg string, res *AgentRunResult, logFn func(string)) {
	tools := make([]anthropicTool, len(builtinAgentTools))
	for i, t := range builtinAgentTools {
		tools[i] = anthropicTool{Name: t.Name, Description: t.Description, InputSchema: t.Schema}
	}
	messages := []anthropicMessage{{Role: "user", Content: userMsg}}

	for turn := 1; turn <= cfg.MaxTurns; turn++ {
		if ctx.Err() != nil {
			res.Err = ctx.Err()
			return
		}
		reqBody := anthropicRequest{
			Model:     cfg.Model,
			MaxTokens: 8192,
			System:    agentSystemPrompt,
			Tools:     tools,
			Messages:  messages,
		}
		reply, rawBlocks, err := callAnthropic(ctx, cfg, reqBody)
		if err != nil {
			res.Err = err
			return
		}
		res.Turns++
		res.PromptTokens += reply.PromptTokens
		res.CompletionTokens += reply.CompletionTokens
		if strings.TrimSpace(reply.Text) != "" {
			logFn(fmt.Sprintf("[turn %d] %s\n", turn, strings.TrimSpace(reply.Text)))
		}
		messages = append(messages, anthropicMessage{Role: "assistant", Content: rawBlocks})

		if len(reply.ToolCalls) == 0 {
			return
		}

		toolResults := make([]anthropicContentBlock, 0, len(reply.ToolCalls))
		for _, tc := range reply.ToolCalls {
			res.ToolCalls++
			out, isErr := executeAgentTool(ctx, env, tc)
			logFn(fmt.Sprintf("[turn %d tool %s] ok=%v (%d bytes)\n", turn, tc.Name, !isErr, len(out)))
			toolResults = append(toolResults, anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: tc.ID,
				Content:   out,
				IsError:   isErr,
			})
		}
		messages = append(messages, anthropicMessage{Role: "user", Content: toolResults})
	}
	res.Err = fmt.Errorf("agent reached max turns (%d)", cfg.MaxTurns)
}

func callAnthropic(ctx context.Context, cfg AgentConfig, payload anthropicRequest) (agentTurnReply, []anthropicContentBlock, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return agentTurnReply{}, nil, fmt.Errorf("marshal anthropic request: %w", err)
	}
	endpoint := cfg.BaseURL + "/v1/messages"
	if strings.HasSuffix(cfg.BaseURL, "/v1") {
		endpoint = cfg.BaseURL + "/messages"
	}
	headers := map[string]string{
		"Content-Type":      "application/json",
		"x-api-key":         cfg.APIKey,
		"anthropic-version": anthropicAPIVersion,
	}
	respBytes, err := doJSONPostWithRetry(ctx, cfg, endpoint, headers, body)
	if err != nil {
		return agentTurnReply{}, nil, err
	}
	var parsed anthropicResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return agentTurnReply{}, nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	reply := agentTurnReply{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		StopReason:       parsed.StopReason,
	}
	var texts []string
	for _, blk := range parsed.Content {
		switch blk.Type {
		case "text":
			if blk.Text != "" {
				texts = append(texts, blk.Text)
			}
		case "tool_use":
			args := blk.Input
			if len(bytes.TrimSpace(args)) == 0 {
				args = json.RawMessage(`{}`)
			}
			reply.ToolCalls = append(reply.ToolCalls, agentToolCall{
				ID:        blk.ID,
				Name:      blk.Name,
				Arguments: args,
			})
		}
	}
	reply.Text = strings.Join(texts, "\n")
	return reply, parsed.Content, nil
}

// --- OpenAI-compatible Chat Completions API ---

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Tools    []openAITool    `json:"tools"`
}

type openAIResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string           `json:"role"`
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func runOpenAILoop(ctx context.Context, cfg AgentConfig, env agentEnv, userMsg string, res *AgentRunResult, logFn func(string)) {
	tools := make([]openAITool, len(builtinAgentTools))
	for i, t := range builtinAgentTools {
		tools[i] = openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		}
	}
	messages := []openAIMessage{
		{Role: "system", Content: agentSystemPrompt},
		{Role: "user", Content: userMsg},
	}

	for turn := 1; turn <= cfg.MaxTurns; turn++ {
		if ctx.Err() != nil {
			res.Err = ctx.Err()
			return
		}
		reqBody := openAIRequest{
			Model:    cfg.Model,
			Messages: messages,
			Tools:    tools,
		}
		reply, rawCalls, err := callOpenAI(ctx, cfg, reqBody)
		if err != nil {
			res.Err = err
			return
		}
		res.Turns++
		res.PromptTokens += reply.PromptTokens
		res.CompletionTokens += reply.CompletionTokens
		if strings.TrimSpace(reply.Text) != "" {
			logFn(fmt.Sprintf("[turn %d] %s\n", turn, strings.TrimSpace(reply.Text)))
		}

		assistantMsg := openAIMessage{
			Role:      "assistant",
			ToolCalls: rawCalls,
		}
		if reply.Text != "" {
			assistantMsg.Content = reply.Text
		}
		messages = append(messages, assistantMsg)

		if len(reply.ToolCalls) == 0 {
			return
		}

		for _, tc := range reply.ToolCalls {
			res.ToolCalls++
			out, isErr := executeAgentTool(ctx, env, tc)
			logFn(fmt.Sprintf("[turn %d tool %s] ok=%v (%d bytes)\n", turn, tc.Name, !isErr, len(out)))
			messages = append(messages, openAIMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    out,
			})
		}
	}
	res.Err = fmt.Errorf("agent reached max turns (%d)", cfg.MaxTurns)
}

func callOpenAI(ctx context.Context, cfg AgentConfig, payload openAIRequest) (agentTurnReply, []openAIToolCall, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return agentTurnReply{}, nil, fmt.Errorf("marshal openai request: %w", err)
	}
	endpoint := cfg.BaseURL + "/chat/completions"
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + cfg.APIKey,
	}
	respBytes, err := doJSONPostWithRetry(ctx, cfg, endpoint, headers, body)
	if err != nil {
		return agentTurnReply{}, nil, err
	}
	var parsed openAIResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return agentTurnReply{}, nil, fmt.Errorf("decode openai response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return agentTurnReply{}, nil, errors.New("openai response contained no choices")
	}
	choice := parsed.Choices[0]
	reply := agentTurnReply{
		Text:             choice.Message.Content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		StopReason:       choice.FinishReason,
	}
	for _, tc := range choice.Message.ToolCalls {
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		reply.ToolCalls = append(reply.ToolCalls, agentToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(args),
		})
	}
	return reply, choice.Message.ToolCalls, nil
}

func doJSONPostWithRetry(ctx context.Context, cfg AgentConfig, endpoint string, headers map[string]string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= agentMaxHTTPRetries; attempt++ {
		if attempt > 0 && cfg.RetryBackoff > 0 {
			delay := cfg.RetryBackoff * time.Duration(1<<(attempt-1))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build HTTP request: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := cfg.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request to %s: %w", endpoint, err)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, agentMaxResponseBytes+1))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read HTTP response from %s: %w", endpoint, readErr)
			continue
		}
		if len(respBody) > agentMaxResponseBytes {
			return nil, fmt.Errorf("HTTP response from %s exceeded %d bytes", endpoint, agentMaxResponseBytes)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, nil
		}
		lastErr = fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, compactOutput(respBody, 300))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			continue
		}
		return nil, lastErr
	}
	return nil, lastErr
}

// --- Workspace-scoped tool execution ---

func executeAgentTool(ctx context.Context, env agentEnv, tc agentToolCall) (string, bool) {
	switch tc.Name {
	case "list_files":
		var args struct {
			Scope string `json:"scope"`
			Path  string `json:"path"`
		}
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments for list_files: %v", err), true
		}
		base, err := resolveScopeBase(env, args.Scope)
		if err != nil {
			return "error: " + err.Error(), true
		}
		files, err := listScopedFiles(base, args.Path)
		if err != nil {
			return "error: " + err.Error(), true
		}
		if len(files) == 0 {
			return "(no files)", false
		}
		return strings.Join(files, "\n"), false

	case "read_file":
		var args struct {
			Scope     string `json:"scope"`
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
		}
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments for read_file: %v", err), true
		}
		base, err := resolveScopeBase(env, args.Scope)
		if err != nil {
			return "error: " + err.Error(), true
		}
		out, err := readScopedFile(base, args.Path, args.StartLine, args.EndLine)
		if err != nil {
			return "error: " + err.Error(), true
		}
		return out, false

	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments for write_file: %v", err), true
		}
		if err := writeWorkspaceFile(env.workspaceDir, args.Path, args.Content); err != nil {
			return "error: " + err.Error(), true
		}
		return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), false

	case "edit_file":
		var args struct {
			Path       string `json:"path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments for edit_file: %v", err), true
		}
		n, err := editWorkspaceFile(env.workspaceDir, args.Path, args.OldString, args.NewString, args.ReplaceAll)
		if err != nil {
			return "error: " + err.Error(), true
		}
		return fmt.Sprintf("replaced %d occurrence(s) in %s", n, args.Path), false

	case "run_command":
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			return fmt.Sprintf("error: invalid arguments for run_command: %v", err), true
		}
		if strings.TrimSpace(args.Command) == "" {
			return "error: command is required", true
		}
		out, exitCode, err := runAgentToolCommand(ctx, env, args.Command)
		if err != nil {
			return fmt.Sprintf("exit_code=%d\nerror: %v\n%s", exitCode, err, out), true
		}
		if exitCode != 0 {
			return fmt.Sprintf("exit_code=%d\n%s", exitCode, out), true
		}
		return fmt.Sprintf("exit_code=0\n%s", out), false

	default:
		return fmt.Sprintf("error: unknown tool %q", tc.Name), true
	}
}

func resolveScopeBase(env agentEnv, scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "workspace":
		return env.workspaceDir, nil
	case "context":
		return env.contextDir, nil
	default:
		return "", fmt.Errorf("invalid scope %q (want 'workspace' or 'context')", scope)
	}
}

func checkNoSymlinksUnder(base, target string) error {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absBase, target)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	cur := absBase
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink: %s", filepath.ToSlash(rel))
		}
	}
	return nil
}

func listScopedFiles(base, sub string) ([]string, error) {
	root := base
	if strings.TrimSpace(sub) != "" && strings.TrimSpace(sub) != "." {
		var err error
		root, err = safeJoin(base, sub)
		if err != nil {
			return nil, err
		}
		if err := checkNoSymlinksUnder(base, root); err != nil {
			return nil, err
		}
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	return files, nil
}

func readScopedFile(base, rel string, startLine, endLine int) (string, error) {
	target, err := safeJoin(base, rel)
	if err != nil {
		return "", err
	}
	if err := checkNoSymlinksUnder(base, target); err != nil {
		return "", err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > len(lines) {
		return fmt.Sprintf("(file has %d lines; start_line %d is past EOF)", len(lines), startLine), nil
	}
	if endLine < startLine {
		return "", fmt.Errorf("end_line (%d) must be >= start_line (%d)", endLine, startLine)
	}
	var b strings.Builder
	for i := startLine; i <= endLine; i++ {
		line := fmt.Sprintf("%d: %s\n", i, lines[i-1])
		if b.Len()+len(line) > agentMaxReadFileBytes {
			fmt.Fprintf(&b, "... (truncated at line %d of %d; use start_line/end_line to read more)\n", i-1, len(lines))
			break
		}
		b.WriteString(line)
	}
	return b.String(), nil
}

func writeWorkspaceFile(workspaceDir, rel, content string) error {
	target, err := safeJoin(workspaceDir, rel)
	if err != nil {
		return err
	}
	if err := checkNoSymlinksUnder(workspaceDir, target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(content), 0o644)
}

func editWorkspaceFile(workspaceDir, rel, oldStr, newStr string, replaceAll bool) (int, error) {
	if oldStr == "" {
		return 0, errors.New("old_string must not be empty")
	}
	target, err := safeJoin(workspaceDir, rel)
	if err != nil {
		return 0, err
	}
	if err := checkNoSymlinksUnder(workspaceDir, target); err != nil {
		return 0, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return 0, err
	}
	src := string(data)
	count := strings.Count(src, oldStr)
	if count == 0 {
		return 0, fmt.Errorf("old_string not found in %s", rel)
	}
	if !replaceAll && count > 1 {
		return 0, fmt.Errorf("old_string matched %d times in %s (provide more context or set replace_all=true)", count, rel)
	}
	var updated string
	if replaceAll {
		updated = strings.ReplaceAll(src, oldStr, newStr)
	} else {
		updated = strings.Replace(src, oldStr, newStr, 1)
		count = 1
	}
	if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
		return 0, err
	}
	return count, nil
}

func runAgentToolCommand(ctx context.Context, env agentEnv, command string) (string, int, error) {
	timeout := env.cmdTimeout
	if timeout <= 0 {
		timeout = DefaultAgentCommandTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	killProcessGroupOnCancel(cmd)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = env.workspaceDir
	cmd.Env = cleanAgentToolEnv(env)

	var out cappedOutputBuffer
	out.limit = agentMaxCmdOutputBytes
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err == nil {
		return out.String(), 0, nil
	}
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		return out.String(), -1, fmt.Errorf("command timed out after %s", timeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out.String(), exitErr.ExitCode(), nil
	}
	return out.String(), -1, err
}

var scrubbedAgentEnvKeys = map[string]bool{
	"ANTHROPIC_API_KEY":              true,
	"OPENAI_API_KEY":                 true,
	"AGENT_EVAL_API_KEY":             true,
	"AGENT_EVAL_COMMAND":             true,
	"GITHUB_TOKEN":                   true,
	"GH_TOKEN":                       true,
	"ACTIONS_RUNTIME_TOKEN":          true,
	"ACTIONS_ID_TOKEN_REQUEST_TOKEN": true,
}

func cleanAgentToolEnv(env agentEnv) []string {
	base := cleanGoEnv(env.workFile)
	filtered := make([]string, 0, len(base)+9)
	for _, kv := range base {
		k, _, ok := strings.Cut(kv, "=")
		if ok && scrubbedAgentEnvKeys[k] {
			continue
		}
		filtered = append(filtered, kv)
	}
	return append(filtered,
		"AGENTEVAL_TASK_ID="+env.task.ID,
		"AGENTEVAL_TASK_TITLE="+env.task.Title,
		"AGENTEVAL_PROMPT_FILE="+env.promptPath,
		"AGENTEVAL_WORKSPACE="+env.workspaceDir,
		"AGENTEVAL_CONTEXT_DIR="+env.contextDir,
		"AGENTEVAL_SKILL_DIR="+env.skillDir,
		"AGENTEVAL_SKILL_FILE="+filepath.Join(env.skillDir, "SKILL.md"),
		"AGENTEVAL_MCP_CONFIG="+env.mcpConfigPath,
		"AGENTEVAL_DOCS="+strings.Join(env.task.ContextDocs, ","),
	)
}

type cappedOutputBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedOutputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 {
		return len(p), nil
	}
	remain := b.limit - b.buf.Len()
	if remain > 0 {
		if len(p) <= remain {
			_, _ = b.buf.Write(p)
		} else {
			_, _ = b.buf.Write(p[:remain])
			b.truncated = true
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *cappedOutputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.buf.String()
	if b.truncated {
		return s + "\n... (output truncated)"
	}
	return s
}
