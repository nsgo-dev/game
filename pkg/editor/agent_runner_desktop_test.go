//go:build !ios && !android && !js

package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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

// #1742 Contract test: drives CLIAgentRunner with fake Claude Code and Codex CLIs and verifies that
// Always chips (brief.yaml, AGENTS.md), attached chips, compacted summary, reference images, and the
// materialized viewport PNG temp file all reach the subprocess (and temp files clean up afterwards).
func TestCLIAgentRunnerDeliversComposerChipsAlwaysViewportAndCompactedSummary(t *testing.T) {
	dir, ed := setupRequestContractProject(t)
	ed.AddAgentThreadTurn(AgentThreadTurn{Prompt: "Build floor", Reply: "COMPACT-MARKER-FLOOR-DONE"})
	ed.AddAgentThreadTurn(AgentThreadTurn{Prompt: "Place slime", Reply: "Placed slime_01"})
	if _, err := ed.CompactAgentThread(); err != nil {
		t.Fatalf("CompactAgentThread: %v", err)
	}

	ed.SelectObject("slime_01")
	if _, ok := ed.AcceptSuggestedChip(); !ok {
		t.Fatal("AcceptSuggestedChip failed")
	}
	for _, m := range ed.SearchMentions("slime", MentionGroupPrefabs) {
		ed.AcceptMentionItem(m.ID)
	}
	for _, m := range ed.SearchMentions("", MentionGroupProblems) {
		ed.AcceptMentionItem(m.ID)
	}
	for _, m := range ed.SearchMentions("", MentionGroupPlaytests) {
		ed.AcceptMentionItem(m.ID)
	}
	for _, m := range ed.SearchMentions("slime", MentionGroupAssets) {
		ed.AcceptMentionItem(m.ID)
	}
	ed.SetViewportImageIncluded(true)

	req := ed.AssembleAgentRequest("make the slime faster")
	req.ProjectRoot = dir
	if len(req.ViewportImage) == 0 {
		t.Fatal("AssembleAgentRequest produced empty ViewportImage")
	}

	// Verify direct buildCLIArgs for both CLIs carries Always chips, attached chips, and compacted summary.
	claudeArgs, err := buildCLIArgs(ExternalAgentClaudeCode, req)
	if err != nil {
		t.Fatalf("buildCLIArgs(claude): %v", err)
	}
	var claudeSysPrompt string
	for _, a := range claudeArgs {
		if v, ok := strings.CutPrefix(a, "--append-system-prompt="); ok {
			claudeSysPrompt = v
		}
	}
	if !strings.Contains(claudeSysPrompt, "BRIEF-MARKER-CRISP-COMBAT") || !strings.Contains(claudeSysPrompt, "RULES-MARKER-DETERMINISTIC-EDITS") {
		t.Fatalf("claude --append-system-prompt missing Always chips: %q", claudeSysPrompt)
	}
	claudePrompt := claudeArgs[len(claudeArgs)-1]
	for _, want := range []string{
		"# Compacted earlier requests",
		"COMPACT-MARKER-FLOOR-DONE",
		"id: slime_01",
		"PREFAB-MARKER-SPEED: 40",
		"PROBLEM-MARKER-UNBOUNDED-PATROL",
		"PLAYTEST-MARKER-DEATH-AT-SPIKES",
		"make the slime faster",
	} {
		if !strings.Contains(claudePrompt, want) {
			t.Errorf("claude prompt missing %q:\n%s", want, claudePrompt)
		}
	}

	codexArgs, err := buildCLIArgs(ExternalAgentCodex, req)
	if err != nil {
		t.Fatalf("buildCLIArgs(codex): %v", err)
	}
	codexPrompt := codexArgs[len(codexArgs)-1]
	for _, want := range []string{
		"BRIEF-MARKER-CRISP-COMBAT",
		"RULES-MARKER-DETERMINISTIC-EDITS",
		"# Compacted earlier requests",
		"COMPACT-MARKER-FLOOR-DONE",
		"id: slime_01",
		"PREFAB-MARKER-SPEED: 40",
		"PROBLEM-MARKER-UNBOUNDED-PATROL",
		"PLAYTEST-MARKER-DEATH-AT-SPIKES",
		"make the slime faster",
	} {
		if !strings.Contains(codexPrompt, want) {
			t.Errorf("codex prompt missing %q:\n%s", want, codexPrompt)
		}
	}

	// End-to-end subprocess contract test with fake CLI executables (Unix shell scripts).
	if runtime.GOOS == "windows" {
		return
	}

	captureDir := t.TempDir()
	writeFakeCLI := func(name, captureFile, copyViewportTo, streamLine string) string {
		scriptPath := filepath.Join(captureDir, name)
		// Writes each argv entry as a NUL-separated record, copies any nsgo-agent-ref-* viewport PNG
		// it sees in argv (either as a standalone -i argument or inside a prompt line), and emits one JSON event.
		script := `#!/bin/sh
set -eu
out="$1"
`
		_ = script
		body := `#!/bin/sh
set -eu
: > "` + captureFile + `"
for arg in "$@"; do
  printf '%s\000' "$arg" >> "` + captureFile + `"
done
# Copy the materialized viewport PNG if referenced anywhere in arguments.
vp_path=""
for arg in "$@"; do
  case "$arg" in
    *nsgo-agent-ref-*viewport.png)
      vp_path="$arg"
      ;;
    *viewport.png*)
      found=$(printf '%s\n' "$arg" | sed -n 's/^- \([^ ]*nsgo-agent-ref-[^ ]*viewport\.png\).*/\1/p' | head -n 1)
      if [ -n "$found" ]; then
        vp_path="$found"
      fi
      ;;
  esac
done
if [ -n "$vp_path" ] && [ -f "$vp_path" ]; then
  cp "$vp_path" "` + copyViewportTo + `"
  printf '%s' "$vp_path" > "` + copyViewportTo + `.path"
fi
printf '%s\n' '` + streamLine + `'
`
		if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
			t.Fatalf("write fake CLI %s: %v", name, err)
		}
		return scriptPath
	}

	readNullArgs := func(path string) []string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read capture %s: %v", path, err)
		}
		parts := bytes.Split(raw, []byte{0})
		var out []string
		for _, p := range parts {
			if len(p) > 0 {
				out = append(out, string(p))
			}
		}
		return out
	}

	t.Run("fake claude CLI receives Always chips, turn chips, summary, and temp viewport PNG", func(t *testing.T) {
		capFile := filepath.Join(captureDir, "claude.args")
		vpCopy := filepath.Join(captureDir, "claude_vp.png")
		streamJSON := `{"type":"result","subtype":"success","result":"Updated slime speed."}`
		fakeClaude := writeFakeCLI("fake-claude", capFile, vpCopy, streamJSON)

		runner := &CLIAgentRunner{ClaudeBinary: fakeClaude}
		r := req
		r.AgentKind = ExternalAgentClaudeCode
		var events []AgentEvent
		if err := runner.Run(context.Background(), r, func(ev AgentEvent) {
			events = append(events, ev)
		}); err != nil {
			t.Fatalf("CLIAgentRunner.Run(claude): %v", err)
		}

		args := readNullArgs(capFile)
		joined := strings.Join(args, "\n")
		for _, want := range []string{
			"--append-system-prompt=",
			"BRIEF-MARKER-CRISP-COMBAT",
			"RULES-MARKER-DETERMINISTIC-EDITS",
			"COMPACT-MARKER-FLOOR-DONE",
			"id: slime_01",
			"PREFAB-MARKER-SPEED: 40",
			"PROBLEM-MARKER-UNBOUNDED-PATROL",
			"PLAYTEST-MARKER-DEATH-AT-SPIKES",
			"make the slime faster",
			"art/slime.png",
			"viewport.png",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("fake claude argv missing %q:\n%s", want, joined)
			}
		}
		gotVP, err := os.ReadFile(vpCopy)
		if err != nil {
			t.Fatalf("fake claude did not see materialized viewport PNG on disk: %v", err)
		}
		if !bytes.Equal(gotVP, req.ViewportImage) {
			t.Fatalf("materialized viewport PNG bytes mismatch: got %d bytes, want %d bytes", len(gotVP), len(req.ViewportImage))
		}
		// Verify temp file was cleaned up after Run returned.
		origVPPathBytes, err := os.ReadFile(vpCopy + ".path")
		if err != nil {
			t.Fatalf("missing recorded viewport path: %v", err)
		}
		if _, err := os.Stat(strings.TrimSpace(string(origVPPathBytes))); !os.IsNotExist(err) {
			t.Fatalf("temp viewport file was not cleaned up after Run: %s (err=%v)", string(origVPPathBytes), err)
		}
		if len(events) == 0 {
			t.Fatal("expected parsed stream events from fake claude")
		}
	})

	t.Run("fake codex CLI fresh receives -i viewport PNG and full context", func(t *testing.T) {
		capFile := filepath.Join(captureDir, "codex.args")
		vpCopy := filepath.Join(captureDir, "codex_vp.png")
		streamJSON, _ := json.Marshal(map[string]any{
			"type": "item.completed",
			"item": map[string]any{"type": "agent_message", "text": "Updated slime speed."},
		})
		fakeCodex := writeFakeCLI("fake-codex", capFile, vpCopy, string(streamJSON))

		runner := &CLIAgentRunner{CodexBinary: fakeCodex}
		r := req
		r.AgentKind = ExternalAgentCodex
		r.SessionID = ""
		if err := runner.Run(context.Background(), r, func(AgentEvent) {}); err != nil {
			t.Fatalf("CLIAgentRunner.Run(codex): %v", err)
		}

		args := readNullArgs(capFile)
		var sawViewportFlag, sawAssetFlag bool
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-i" {
				if strings.HasSuffix(args[i+1], "viewport.png") {
					sawViewportFlag = true
				}
				if strings.HasSuffix(args[i+1], filepath.Join("art", "slime.png")) {
					sawAssetFlag = true
				}
			}
		}
		if !sawViewportFlag || !sawAssetFlag {
			t.Fatalf("codex missing -i image flags (viewport=%v asset=%v): %q", sawViewportFlag, sawAssetFlag, args)
		}
		prompt := args[len(args)-1]
		for _, want := range []string{
			"BRIEF-MARKER-CRISP-COMBAT",
			"RULES-MARKER-DETERMINISTIC-EDITS",
			"COMPACT-MARKER-FLOOR-DONE",
			"id: slime_01",
			"PREFAB-MARKER-SPEED: 40",
			"PROBLEM-MARKER-UNBOUNDED-PATROL",
			"PLAYTEST-MARKER-DEATH-AT-SPIKES",
			"make the slime faster",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("fake codex prompt missing %q:\n%s", want, prompt)
			}
		}
		gotVP, err := os.ReadFile(vpCopy)
		if err != nil {
			t.Fatalf("fake codex did not see materialized viewport PNG: %v", err)
		}
		if !bytes.Equal(gotVP, req.ViewportImage) {
			t.Fatalf("codex viewport PNG bytes mismatch: got %d, want %d", len(gotVP), len(req.ViewportImage))
		}
		origVPPathBytes, err := os.ReadFile(vpCopy + ".path")
		if err != nil {
			t.Fatalf("missing recorded codex viewport path: %v", err)
		}
		if _, err := os.Stat(strings.TrimSpace(string(origVPPathBytes))); !os.IsNotExist(err) {
			t.Fatalf("codex temp viewport file was not cleaned up: %s", string(origVPPathBytes))
		}
	})

	t.Run("fake codex CLI resume receives full context and viewport PNG path in prompt", func(t *testing.T) {
		capFile := filepath.Join(captureDir, "codex_resume.args")
		vpCopy := filepath.Join(captureDir, "codex_resume_vp.png")
		streamJSON, _ := json.Marshal(map[string]any{
			"type": "item.completed",
			"item": map[string]any{"type": "agent_message", "text": "Resumed and updated."},
		})
		fakeCodex := writeFakeCLI("fake-codex-resume", capFile, vpCopy, string(streamJSON))

		runner := &CLIAgentRunner{CodexBinary: fakeCodex}
		r := req
		r.AgentKind = ExternalAgentCodex
		r.SessionID = "thread-resume-7"
		if err := runner.Run(context.Background(), r, func(AgentEvent) {}); err != nil {
			t.Fatalf("CLIAgentRunner.Run(codex resume): %v", err)
		}

		args := readNullArgs(capFile)
		if len(args) < 6 || args[0] != "exec" || args[1] != "resume" || args[len(args)-2] != "thread-resume-7" {
			t.Fatalf("unexpected codex resume args: %q", args)
		}
		prompt := args[len(args)-1]
		for _, want := range []string{
			"BRIEF-MARKER-CRISP-COMBAT",
			"COMPACT-MARKER-FLOOR-DONE",
			"id: slime_01",
			"make the slime faster",
			"viewport.png",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("fake codex resume prompt missing %q:\n%s", want, prompt)
			}
		}
		gotVP, err := os.ReadFile(vpCopy)
		if err != nil {
			t.Fatalf("fake codex resume did not see materialized viewport PNG: %v", err)
		}
		if !bytes.Equal(gotVP, req.ViewportImage) {
			t.Fatalf("codex resume viewport PNG mismatch")
		}
	})
}
