//go:build !ios && !android && !js

package editor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
	args := buildCLIArgs(kind, req)
	cmd := exec.CommandContext(ctx, bin, args...)
	if req.ProjectRoot != "" {
		cmd.Dir = req.ProjectRoot
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("agent runner: stdout pipe: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

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

func buildCLIArgs(kind ExternalAgentKind, req AgentRequest) []string {
	if kind == ExternalAgentCodex {
		if req.SessionID != "" {
			return []string{"exec", "resume", req.SessionID, "--json", req.Prompt}
		}
		args := []string{"exec", "--json"}
		if req.ProjectRoot != "" {
			args = append(args, "-C", req.ProjectRoot)
		}
		return append(args, req.Prompt)
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
	return append(args, req.Prompt)
}
