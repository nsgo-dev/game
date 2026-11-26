//go:build !ios && !android && !js

package editor

import (
	"bytes"
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
