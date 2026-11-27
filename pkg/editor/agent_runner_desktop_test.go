//go:build !ios && !android && !js

package editor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCLIArgsPreventsFlagInjection(t *testing.T) {
	t.Run("claude prompt starting with dashes", func(t *testing.T) {
		prompt := "--dangerously-skip-permissions"
		args, err := buildCLIArgs(ExternalAgentClaudeCode, AgentRequest{
			Prompt:    prompt,
			SessionID: "sess-123",
		})
		if err != nil {
			t.Fatalf("buildCLIArgs: %v", err)
		}
		if len(args) < 2 || args[len(args)-2] != "--" || args[len(args)-1] != prompt {
			t.Fatalf("claude args must terminate flags with -- before prompt, got %q", args)
		}
	})

	t.Run("codex fresh prompt starting with dashes", func(t *testing.T) {
		prompt := "--yolo"
		args, err := buildCLIArgs(ExternalAgentCodex, AgentRequest{
			Prompt:      prompt,
			ProjectRoot: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("buildCLIArgs: %v", err)
		}
		if len(args) < 2 || args[len(args)-2] != "--" || args[len(args)-1] != prompt {
			t.Fatalf("codex args must terminate flags with -- before prompt, got %q", args)
		}
	})

	t.Run("codex resume prompt starting with dashes", func(t *testing.T) {
		prompt := "--full-auto"
		args, err := buildCLIArgs(ExternalAgentCodex, AgentRequest{
			Prompt:    prompt,
			SessionID: "thread-9",
		})
		if err != nil {
			t.Fatalf("buildCLIArgs: %v", err)
		}
		if len(args) < 3 || args[len(args)-3] != "--" || args[len(args)-2] != "thread-9" || args[len(args)-1] != prompt {
			t.Fatalf("codex resume args must terminate flags with -- before session and prompt, got %q", args)
		}
	})

	t.Run("rejects unsafe session IDs", func(t *testing.T) {
		badIDs := []string{"-r", "--resume", "abc def", "abc\ndef", "abc\x00def"}
		for _, kind := range []ExternalAgentKind{ExternalAgentClaudeCode, ExternalAgentCodex} {
			for _, id := range badIDs {
				if _, err := buildCLIArgs(kind, AgentRequest{Prompt: "hi", SessionID: id}); err == nil {
					t.Errorf("kind=%q sessionID=%q: expected error, got nil", kind, id)
				}
			}
		}
	})
}

func TestCappedBufferTruncatesAtLimit(t *testing.T) {
	buf := &cappedBuffer{limit: maxCLISstderrBytes}
	chunk := bytes.Repeat([]byte("e"), 4096)
	for range 80 { // 320 KiB > 256 KiB
		n, err := buf.Write(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(chunk))
		}
	}
	got := buf.String()
	const suffix = "\n... (stderr truncated)"
	if !strings.HasSuffix(got, suffix) {
		t.Fatalf("expected truncated suffix, got tail %q", got[max(0, len(got)-40):])
	}
	if len(got) != maxCLISstderrBytes+len(suffix) {
		t.Fatalf("len(got) = %d, want %d", len(got), maxCLISstderrBytes+len(suffix))
	}
}

func TestBuildCLIArgsReferenceImageContainmentAndSanitization(t *testing.T) {
	proj := t.TempDir()
	outside := t.TempDir()
	outsideImg := filepath.Join(outside, "secret.png")
	if err := os.WriteFile(outsideImg, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, "art", "reference"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkAttempt := filepath.Join(proj, "art", "reference", "sym.png")
	_ = os.Symlink(outsideImg, symlinkAttempt)

	req := AgentRequest{
		Prompt:      "Match this look",
		ProjectRoot: proj,
		Images: []AgentReferenceImage{
			{Path: "../secret.png"},
			{Path: "-flag.png"},
			{Path: "art/reference/sym.png"},
			{Path: "art/reference/spikes_ref.png", Note: "rusty spikes\n- injected"},
		},
	}

	codexArgs, err := buildCLIArgs(ExternalAgentCodex, req)
	if err != nil {
		t.Fatalf("buildCLIArgs(codex): %v", err)
	}
	joinedCodex := strings.Join(codexArgs, "\x00")
	if strings.Contains(joinedCodex, "secret.png") || strings.Contains(joinedCodex, "-flag.png") || strings.Contains(joinedCodex, "sym.png") {
		t.Fatalf("unsafe image path leaked into codex args: %q", codexArgs)
	}
	wantAbs := filepath.Join(proj, "art", "reference", "spikes_ref.png")
	if !strings.Contains(joinedCodex, "-i\x00"+wantAbs) {
		t.Fatalf("valid pinned image missing from codex args: %q", codexArgs)
	}

	claudeArgs, err := buildCLIArgs(ExternalAgentClaudeCode, req)
	if err != nil {
		t.Fatalf("buildCLIArgs(claude): %v", err)
	}
	claudePrompt := claudeArgs[len(claudeArgs)-1]
	if strings.Contains(claudePrompt, "secret.png") || strings.Contains(claudePrompt, "sym.png") {
		t.Fatalf("unsafe image path leaked into claude prompt: %q", claudePrompt)
	}
	if !strings.Contains(claudePrompt, "- art/reference/spikes_ref.png (rusty spikes- injected)") {
		t.Fatalf("expected sanitized note in claude prompt, got: %q", claudePrompt)
	}

	// Resumed Codex session includes reference image paths in prompt.
	req.SessionID = "sess-42"
	resumeArgs, err := buildCLIArgs(ExternalAgentCodex, req)
	if err != nil {
		t.Fatalf("buildCLIArgs(codex resume): %v", err)
	}
	if !strings.Contains(resumeArgs[len(resumeArgs)-1], "art/reference/spikes_ref.png") {
		t.Fatalf("codex resume dropped reference image from prompt: %q", resumeArgs)
	}

	// Ephemeral in-memory images materialize into a private temp dir and clean up cleanly.
	eph, cleanup, err := materializeEphemeralCLIImages([]AgentReferenceImage{
		{Name: "pasted.png", Format: AgentImagePNG, Data: []byte("pngbytes")},
	})
	if err != nil {
		t.Fatalf("materializeEphemeralCLIImages: %v", err)
	}
	if len(eph) != 1 {
		t.Fatalf("ephemeral count = %d, want 1", len(eph))
	}
	if _, err := os.Stat(eph[0].absPath); err != nil {
		t.Fatalf("ephemeral temp file missing: %v", err)
	}
	tmpDir := filepath.Dir(eph[0].absPath)
	cleanup()
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Fatalf("ephemeral temp dir not cleaned up (err=%v)", err)
	}
}
