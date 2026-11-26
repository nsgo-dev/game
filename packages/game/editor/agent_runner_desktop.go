//go:build !ios && !android && !js

package editor

import (
	"bufio"
	"bytes"
	"context"
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

// CLIAgentRunner runs an external agent CLI (Claude Code or Codex) non-interactively per composer request with streamed JSON output and the project's MCP config.
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
	args, err := buildCLIArgs(kind, req)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = nil
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
		path, err := exec.LookPath(codexBin)
		if err != nil {
			return "", "", fmt.Errorf("codex CLI not found in PATH: install Codex or connect an agent")
		}
		return ExternalAgentCodex, path, nil
	case ExternalAgentClaudeCode:
		path, err := exec.LookPath(claudeBin)
		if err != nil {
			return "", "", fmt.Errorf("claude CLI not found in PATH: install Claude Code or connect an agent")
		}
		return ExternalAgentClaudeCode, path, nil
	default:
		if path, err := exec.LookPath(claudeBin); err == nil {
			return ExternalAgentClaudeCode, path, nil
		}
		if path, err := exec.LookPath(codexBin); err == nil {
			return ExternalAgentCodex, path, nil
		}
		return "", "", fmt.Errorf("no supported external agent CLI (claude or codex) found in PATH")
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

func buildCLIArgs(kind ExternalAgentKind, req AgentRequest) ([]string, error) {
	if err := validateCLISessionID(req.SessionID); err != nil {
		return nil, err
	}
	if kind == ExternalAgentCodex {
		if req.SessionID != "" {
			return []string{"exec", "resume", "--json", "--", req.SessionID, req.Prompt}, nil
		}
		args := []string{"exec", "--json"}
		if req.ProjectRoot != "" {
			args = append(args, "-C", req.ProjectRoot)
		}
		return append(args, "--", req.Prompt), nil
	}
	args := []string{"--print", "--output-format=stream-json", "--verbose"}
	if req.ProjectRoot != "" {
		cfg := filepath.Join(req.ProjectRoot, ".mcp.json")
		if _, err := os.Stat(cfg); err == nil {
			args = append(args, "--mcp-config", cfg)
		}
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	return append(args, "--", req.Prompt), nil
}
