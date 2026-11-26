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
	ephemeralFiles, cleanup, err := materializeEphemeralCLIImages(req.Images)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}
	args, err := buildCLIArgsWithEphemeral(kind, req, ephemeralFiles)
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
	if err := validateCLISessionID(req.SessionID); err != nil {
		return nil, err
	}
	prompt := req.Prompt
	var refs []cliRefImage
	if len(req.Images) > 0 && req.ProjectRoot != "" {
		for _, img := range req.Images {
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
		if req.SessionID != "" {
			return []string{"exec", "resume", "--json", "--", req.SessionID, appendRefPrompt(prompt)}, nil
		}
		args := []string{"exec", "--json"}
		if req.ProjectRoot != "" {
			args = append(args, "-C", req.ProjectRoot)
		}
		for _, ref := range refs {
			args = append(args, "-i", ref.absPath)
		}
		return append(args, "--", prompt), nil
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
	prompt = appendRefPrompt(prompt)
	return append(args, "--", prompt), nil
}
