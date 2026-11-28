//go:build !ios && !android && !js

package editor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const maxCLISstderrBytes = 256 * 1024

// cappedBuffer captures up to limit bytes so a noisy subprocess stderr cannot cause unbounded RAM growth.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
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

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.buf.String()
	if b.truncated {
		return s + "\n... (stderr truncated)"
	}
	return s
}

// CLIAgentRunner runs an external agent CLI (Claude Code or Codex) non-interactively per composer request with streamed JSON output, connected to the editor's MCP server and the person's servers from editor.yaml, never to agent configuration the project itself carries.
type CLIAgentRunner struct {
	// ClaudeBinary overrides the "claude" executable path (defaults to PATH lookup).
	ClaudeBinary string
	// CodexBinary overrides the "codex" executable path (defaults to PATH lookup).
	CodexBinary string
}

func defaultCLIAgentRunner() AgentRunner {
	return &CLIAgentRunner{}
}

// Run executes req against Claude Code (`claude --print --output-format=stream-json --verbose`) or Codex (`codex exec --json`), streaming parsed AgentEvents to emit.
func (r *CLIAgentRunner) Run(ctx context.Context, req AgentRequest, emit func(AgentEvent)) error {
	kind, bin, err := r.resolveCLI(req.AgentKind)
	if err != nil {
		return err
	}
	input := RenderAgentModelInput(req)
	ephemeralFiles, cleanup, err := materializeEphemeralCLIImages(input.Images)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}
	args, err := buildCLIArgsForInput(kind, req, input, ephemeralFiles)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = nil
	env := enrichedAgentEnv(os.Environ(), bin)
	filteredEnv := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && k == "NSGO_EDITOR_TASK_ID" {
			continue
		}
		filteredEnv = append(filteredEnv, kv)
	}
	if req.TaskID != "" {
		filteredEnv = append(filteredEnv, "NSGO_EDITOR_TASK_ID="+req.TaskID)
	}
	cmd.Env = filteredEnv
	if req.ProjectRoot != "" {
		cmd.Dir = req.ProjectRoot
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("agent runner: stdout pipe: %w", err)
	}
	stderrBuf := &cappedBuffer{limit: maxCLISstderrBytes}
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("agent runner: start %s: %w", bin, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var events []AgentEvent
		var ok bool
		if kind == ExternalAgentCodex {
			events, ok = ParseCodexStreamLine(line)
		} else {
			events, ok = ParseClaudeStreamLine(line)
		}
		if ok {
			for _, ev := range events {
				emit(ev)
			}
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanErr != nil && !errors.Is(scanErr, io.EOF) {
		return fmt.Errorf("agent runner: read %s output: %w", bin, scanErr)
	}
	if waitErr != nil {
		detail := strings.TrimSpace(stderrBuf.String())
		if detail != "" {
			return fmt.Errorf("%s failed: %s", bin, detail)
		}
		return fmt.Errorf("%s failed: %w", bin, waitErr)
	}
	return nil
}

func (r *CLIAgentRunner) resolveCLI(kind ExternalAgentKind) (ExternalAgentKind, string, error) {
	claudeBin := r.ClaudeBinary
	if claudeBin == "" {
		claudeBin = "claude"
	}
	codexBin := r.CodexBinary
	if codexBin == "" {
		codexBin = "codex"
	}
	switch kind {
	case ExternalAgentCodex:
		path, ok := resolveLocalAgentBinary(codexBin)
		if !ok {
			return "", "", fmt.Errorf("codex CLI not found in PATH or standard install locations: install Codex or connect an agent")
		}
		return ExternalAgentCodex, path, nil
	case ExternalAgentClaudeCode:
		path, ok := resolveLocalAgentBinary(claudeBin)
		if !ok {
			return "", "", fmt.Errorf("claude CLI not found in PATH or standard install locations: install Claude Code or connect an agent")
		}
		return ExternalAgentClaudeCode, path, nil
	default:
		if path, ok := resolveLocalAgentBinary(claudeBin); ok {
			return ExternalAgentClaudeCode, path, nil
		}
		if path, ok := resolveLocalAgentBinary(codexBin); ok {
			return ExternalAgentCodex, path, nil
		}
		return "", "", fmt.Errorf("no supported external agent CLI (claude or codex) found in PATH or standard install locations")
	}
}

func validateCLISessionID(id string) error {
	if id == "" {
		return nil
	}
	if strings.HasPrefix(id, "-") {
		return fmt.Errorf("agent runner: invalid session id %q: must not start with '-'", id)
	}
	for _, r := range id {
		if r <= 0x20 || r == 0x7f {
			return fmt.Errorf("agent runner: invalid session id %q: contains whitespace or control character", id)
		}
	}
	return nil
}

type cliRefImage struct {
	absPath string
	display string
	note    string
}

func materializeEphemeralCLIImages(images []AgentReferenceImage) ([]cliRefImage, func(), error) {
	var ephemeral []AgentReferenceImage
	for _, img := range images {
		if img.Path == "" && len(img.Data) > 0 {
			ephemeral = append(ephemeral, img)
		}
	}
	if len(ephemeral) == 0 {
		return nil, nil, nil
	}
	tmpDir, err := os.MkdirTemp("", "nsgo-agent-ref-*")
	if err != nil {
		return nil, nil, fmt.Errorf("agent runner: create temp dir for reference images: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	out := make([]cliRefImage, 0, len(ephemeral))
	for i, img := range ephemeral {
		format := img.Format
		if format == "" {
			format = AgentImagePNG
		}
		cleanName := sanitizeReferenceFileName(img.Name, format)
		fileName := fmt.Sprintf("%02d_%s", i+1, cleanName)
		absPath := filepath.Join(tmpDir, fileName)
		if err := os.WriteFile(absPath, img.Data, 0o600); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("agent runner: write temp reference image: %w", err)
		}
		out = append(out, cliRefImage{
			absPath: absPath,
			display: absPath,
			note:    sanitizeSingleLineMeta(img.Note),
		})
	}
	return out, cleanup, nil
}

func buildCLIArgs(kind ExternalAgentKind, req AgentRequest) ([]string, error) {
	return buildCLIArgsWithEphemeral(kind, req, nil)
}

func buildCLIArgsWithEphemeral(kind ExternalAgentKind, req AgentRequest, ephemeral []cliRefImage) ([]string, error) {
	return buildCLIArgsForInput(kind, req, RenderAgentModelInput(req), ephemeral)
}

func buildCLIArgsForInput(kind ExternalAgentKind, req AgentRequest, input AgentModelInput, ephemeral []cliRefImage) ([]string, error) {
	if err := validateCLISessionID(req.SessionID); err != nil {
		return nil, err
	}
	var refs []cliRefImage
	if len(input.Images) > 0 && req.ProjectRoot != "" {
		for _, img := range input.Images {
			if img.Path == "" {
				continue
			}
			cleanRel := cleanBriefRefPath(img.Path)
			if cleanRel == "" || strings.HasPrefix(cleanRel, "-") {
				continue
			}
			abs := filepath.Join(req.ProjectRoot, filepath.FromSlash(cleanRel))
			if strings.HasPrefix(abs, "-") {
				continue
			}
			if err := verifyAssetGenWritePath(req.ProjectRoot, abs, cleanRel); err != nil {
				continue
			}
			refs = append(refs, cliRefImage{
				absPath: abs,
				display: cleanRel,
				note:    sanitizeSingleLineMeta(img.Note),
			})
		}
	}
	for _, eph := range ephemeral {
		if eph.absPath != "" && !strings.HasPrefix(eph.absPath, "-") {
			refs = append(refs, eph)
		}
	}

	appendRefPrompt := func(base string) string {
		if len(refs) == 0 {
			return base
		}
		var b strings.Builder
		b.WriteString(base)
		b.WriteString("\n\nReference images:")
		for _, ref := range refs {
			b.WriteString("\n- ")
			b.WriteString(ref.display)
			if ref.note != "" {
				b.WriteString(" (")
				b.WriteString(ref.note)
				b.WriteByte(')')
			}
		}
		return b.String()
	}

	if kind == ExternalAgentCodex {
		policy, err := codexLaunchPolicyArgs(req.ProjectRoot, req.MCPServers)
		if err != nil {
			return nil, err
		}
		prompt := agentUntrustedContentNotice + "\n\n" + input.FullPrompt
		if req.SessionID != "" {
			args := append([]string{"exec", "resume", "--json"}, policy...)
			return append(args, "--", req.SessionID, appendRefPrompt(prompt)), nil
		}
		args := append([]string{"exec", "--json"}, policy...)
		if req.ProjectRoot != "" {
			args = append(args, "-C", req.ProjectRoot)
		}
		for _, ref := range refs {
			args = append(args, "-i", ref.absPath)
		}
		return append(args, "--", prompt), nil
	}
	args := []string{"--print", "--output-format=stream-json", "--verbose"}
	policy, err := claudeLaunchPolicyArgs(req.MCPServers)
	if err != nil {
		return nil, err
	}
	args = append(args, policy...)
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	sysPrompt := agentUntrustedContentNotice
	if input.AlwaysContext != "" {
		sysPrompt += "\n\n" + input.AlwaysContext
	}
	args = append(args, "--append-system-prompt="+sysPrompt)
	prompt := appendRefPrompt(input.TurnPrompt)
	return append(args, "--", prompt), nil
}

// claudeDeniedBuiltinTools are Claude Code's built-in tools that change files or run commands directly. The composer denies them, even when the person's own Claude Code settings allow them, so every change goes through the editor's MCP tools, turns and permissions.yaml.
var claudeDeniedBuiltinTools = []string{"Bash", "PowerShell", "Edit", "Write", "NotebookEdit"}

// codexLaunchPolicyArgs returns the Codex flags that keep a project's own agent configuration from running, even in a folder the person once trusted in Codex: every folder from the project root up is marked untrusted for this run only (so no project .codex/config.toml, MCP servers or hooks load), execpolicy .rules files are ignored, the editor's server and the person's enabled servers from editor.yaml are passed inline (so Codex has the editor's tools without folder trust), and the sandbox is pinned read-only with no approval prompts so edits go through the editor's MCP tools.
func codexLaunchPolicyArgs(projectRoot string, servers []AgentMCPServer) ([]string, error) {
	args := []string{"--ignore-rules", "-c", `sandbox_mode="read-only"`, "-c", `approval_policy="never"`}
	untrusted, err := codexUntrustedProjectsOverride(projectRoot)
	if err != nil {
		return nil, err
	}
	args = append(args, "-c", untrusted)
	editorCmd, editorArgs := EditorMCPServerCommand()
	seen := map[string]bool{editorMCPServerName: true}
	add := func(name, command string, cmdArgs []string) error {
		cmdTOML, err := json.Marshal(command)
		if err != nil {
			return fmt.Errorf("agent runner: encode MCP server %s: %w", name, err)
		}
		if cmdArgs == nil {
			cmdArgs = []string{}
		}
		argsTOML, err := json.Marshal(cmdArgs)
		if err != nil {
			return fmt.Errorf("agent runner: encode MCP server %s: %w", name, err)
		}
		args = append(args, "-c", "mcp_servers."+name+".command="+string(cmdTOML), "-c", "mcp_servers."+name+".args="+string(argsTOML))
		return nil
	}
	if err := add(editorMCPServerName, editorCmd, editorArgs); err != nil {
		return nil, err
	}
	for _, srv := range servers {
		if seen[srv.Name] || !srv.IsEnabled() || !validLocalAgentServerName(srv.Name) || strings.TrimSpace(srv.Command) == "" {
			continue
		}
		seen[srv.Name] = true
		if err := add(srv.Name, srv.Command, srv.Args); err != nil {
			return nil, err
		}
	}
	return args, nil
}

// codexUntrustedProjectsOverride returns a `projects={...}` Codex config override that marks the project root and each of its parent folders, both as given and with symlinks resolved, untrusted for one run. Codex loads a .codex/config.toml layer for every trusted folder between the working directory and the repository root, so each one is covered. JSON strings are valid TOML basic strings, so the paths are quoted with encoding/json.
func codexUntrustedProjectsOverride(projectRoot string) (string, error) {
	dir := projectRoot
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("agent runner: working directory: %w", err)
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("agent runner: project root: %w", err)
	}
	starts := []string{abs}
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
		starts = append(starts, real)
	}
	var b strings.Builder
	b.WriteString("projects={")
	seen := map[string]bool{}
	for _, start := range starts {
		for d := start; ; d = filepath.Dir(d) {
			if !seen[d] {
				seen[d] = true
				key, err := json.Marshal(d)
				if err != nil {
					return "", fmt.Errorf("agent runner: encode project path: %w", err)
				}
				if len(seen) > 1 {
					b.WriteByte(',')
				}
				b.Write(key)
				b.WriteString(`={trust_level="untrusted"}`)
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	b.WriteByte('}')
	return b.String(), nil
}

// claudeLaunchPolicyArgs returns the Claude Code flags that keep a project's own agent configuration from running: only the person's user settings load (no project or local .claude/settings*.json, so no project hooks or permission rules), only the editor's server and the person's enabled servers from editor.yaml connect (never the project's .mcp.json), and tools outside those servers are denied rather than asked about.
func claudeLaunchPolicyArgs(servers []AgentMCPServer) ([]string, error) {
	editorCmd, editorArgs := EditorMCPServerCommand()
	cfg := map[string]map[string]any{
		editorMCPServerName: {"command": editorCmd, "args": editorArgs},
	}
	allowed := []string{"mcp__" + editorMCPServerName + "__*"}
	for _, srv := range servers {
		if srv.Name == editorMCPServerName || !srv.IsEnabled() || !validLocalAgentServerName(srv.Name) || strings.TrimSpace(srv.Command) == "" {
			continue
		}
		if _, dup := cfg[srv.Name]; dup {
			continue
		}
		entry := map[string]any{"command": srv.Command}
		if len(srv.Args) > 0 {
			entry["args"] = srv.Args
		}
		cfg[srv.Name] = entry
		allowed = append(allowed, "mcp__"+srv.Name+"__*")
	}
	cfgJSON, err := json.Marshal(map[string]any{"mcpServers": cfg})
	if err != nil {
		return nil, fmt.Errorf("agent runner: encode MCP config: %w", err)
	}
	return []string{
		"--setting-sources=user",
		"--strict-mcp-config",
		"--mcp-config=" + string(cfgJSON),
		"--permission-mode=dontAsk",
		"--allowedTools=" + strings.Join(allowed, ","),
		"--disallowedTools=" + strings.Join(claudeDeniedBuiltinTools, ","),
	}, nil
}

// validLocalAgentServerName reports whether name is a safe MCP server identifier (letters, digits, '_', '-') that cannot alter a Claude Code tool rule list.
func validLocalAgentServerName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
