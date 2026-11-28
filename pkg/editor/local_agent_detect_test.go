package editor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeFakeExec(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolveLocalAgentBinaryFindsClaudeCodexOutsideMinimalGUIPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix executable mode test")
	}
	home := t.TempDir()

	// 1. Claude Code installed via official native installer symlink in ~/.local/bin/claude
	claudeTarget := filepath.Join(home, ".local", "share", "claude", "versions", "2.1.332")
	writeFakeExec(t, claudeTarget)
	claudeLink := filepath.Join(home, ".local", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(claudeLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(claudeTarget, claudeLink); err != nil {
		t.Fatal(err)
	}

	// 2. Codex installed under custom npm prefix declared in ~/.npmrc
	npmPrefix := filepath.Join(home, ".custom-npm-prefix")
	codexBin := filepath.Join(npmPrefix, "bin", "codex")
	writeFakeExec(t, codexBin)
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte("prefix=~/.custom-npm-prefix\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate macOS Finder/Spotlight launchd minimal PATH.
	env := localAgentSearchEnv{
		goos:          "darwin",
		pathEnv:       "/usr/bin:/bin:/usr/sbin:/sbin",
		homeDir:       home,
		loginShellEnv: func() string { return "" },
	}

	gotClaude, ok := resolveLocalAgentBinaryIn("claude", env)
	if !ok || gotClaude != claudeLink {
		t.Fatalf("resolveLocalAgentBinaryIn(claude) = (%q, %v), want (%q, true)", gotClaude, ok, claudeLink)
	}

	gotCodex, ok := resolveLocalAgentBinaryIn("codex", env)
	if !ok || gotCodex != codexBin {
		t.Fatalf("resolveLocalAgentBinaryIn(codex) = (%q, %v), want (%q, true)", gotCodex, ok, codexBin)
	}
}

func TestResolveLocalAgentBinaryNVMSemverOrderAndShellFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix executable mode test")
	}
	home := t.TempDir()

	oldBin := filepath.Join(home, ".nvm", "versions", "node", "v18.19.0", "bin", "codex")
	newBin := filepath.Join(home, ".nvm", "versions", "node", "v22.14.0", "bin", "codex")
	writeFakeExec(t, oldBin)
	writeFakeExec(t, newBin)

	customDir := filepath.Join(home, "my-nix-profile", "bin")
	customClaude := filepath.Join(customDir, "claude")
	writeFakeExec(t, customClaude)

	env := localAgentSearchEnv{
		goos:          "darwin",
		pathEnv:       "/usr/bin:/bin",
		homeDir:       home,
		loginShellEnv: func() string { return customDir + ":/usr/bin" },
	}

	gotCodex, ok := resolveLocalAgentBinaryIn("codex", env)
	if !ok || gotCodex != newBin {
		t.Fatalf("nvm semver order: got (%q, %v), want newest (%q, true)", gotCodex, ok, newBin)
	}

	gotClaude, ok := resolveLocalAgentBinaryIn("claude", env)
	if !ok || gotClaude != customClaude {
		t.Fatalf("login shell fallback: got (%q, %v), want (%q, true)", gotClaude, ok, customClaude)
	}
}

func TestEnrichedAgentEnvPrependsBinaryAndCommonDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix path test")
	}
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	codexDir := filepath.Join(home, ".npm-global", "bin")
	codexBin := filepath.Join(codexDir, "codex")
	writeFakeExec(t, codexBin)

	env := localAgentSearchEnv{
		goos:    "darwin",
		homeDir: home,
	}
	gotEnv := enrichedAgentEnvWith([]string{"FOO=bar", "PATH=/usr/bin:/bin"}, codexBin, env)
	var gotPath string
	for _, kv := range gotEnv {
		if after, ok := strings.CutPrefix(kv, "PATH="); ok {
			gotPath = after
		}
	}
	parts := filepath.SplitList(gotPath)
	if len(parts) < 3 || parts[0] != codexDir {
		t.Fatalf("enriched PATH = %q, want %q first", gotPath, codexDir)
	}
	foundLocal := false
	for _, p := range parts {
		if p == localBin {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatalf("enriched PATH = %q, missing %q", gotPath, localBin)
	}
}

func TestGhostButtonHasNoBorderAndConnectDialogShowsDetectedAgents(t *testing.T) {
	// 1. specBtnGhost must not set a 1px border (which caused bright white boxes in Dark mode).
	p := theme.Dark()
	btnNode := specPanelButton(p, specPanelButtonSpec{
		TestID:  "ghost-check",
		Text:    "Later",
		Variant: specBtnGhost,
	})
	btn, ok := btnNode.(base.Button)
	if !ok {
		t.Fatalf("specPanelButton returned %T, want base.Button", btnNode)
	}
	if btn.Style.BorderWidth != 0 {
		t.Fatalf("specBtnGhost BorderWidth = %v, want 0", btn.Style.BorderWidth)
	}

	// 2. ConnectAgentDialogView surfaces detected local agents and supports re-detection.
	ed, err := NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	scanCount := 0
	ed.FirstRunDetectAgents = func() []FirstRunAgentOption {
		scanCount++
		return []FirstRunAgentOption{
			{Kind: "claude", Name: "Claude Code", Description: "Found on this computer", Detected: true, ExecutablePath: "/Users/test/.local/bin/claude"},
			{Kind: "codex", Name: "Codex", Description: "Found on this computer", Detected: true, ExecutablePath: "/Users/test/.npm-global/bin/codex"},
			{Kind: "apikey", Name: "An API key", Description: "The editor calls the model itself", Detected: true},
		}
	}
	ed.OpenConnectDialog()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:    core.OSMacOS,
		Idiom: core.IdiomDesktop,
		Dark:  true,
	}))
	app.Settle()

	if !app.Exists("id:connect-detected-claude") {
		t.Fatal("Connect dialog missing connect-detected-claude chip")
	}
	if !app.Exists("id:connect-detected-codex") {
		t.Fatal("Connect dialog missing connect-detected-codex chip")
	}
	before := scanCount
	app.Press("id:connect-local-refresh")
	app.Settle()
	if scanCount <= before {
		t.Fatalf("pressing connect-local-refresh did not re-scan local agents (scanCount=%d)", scanCount)
	}
}
