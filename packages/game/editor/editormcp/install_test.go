package editormcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

func TestInstallIdempotentAndPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir)

	// First install writes both Claude Code (.mcp.json) and Codex (.codex/config.toml).
	first, err := editormcp.Install(dir)
	if err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if len(first.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(first.Files))
	}
	for _, f := range first.Files {
		if !f.Changed {
			t.Errorf("first Install %s Changed = false, want true", f.RelPath)
		}
	}
	if !editormcp.Installed(first.Root) {
		t.Fatal("Installed() = false after Install")
	}

	claudePath := filepath.Join(dir, ".mcp.json")
	codexPath := filepath.Join(dir, ".codex", "config.toml")
	claudeFirst, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	codexFirst, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatalf("read .codex/config.toml: %v", err)
	}

	// Verify .mcp.json structure.
	var parsed struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(claudeFirst, &parsed); err != nil {
		t.Fatalf("unmarshal .mcp.json: %v\n%s", err, claudeFirst)
	}
	srv, ok := parsed.MCPServers[editormcp.ServerName]
	if !ok || srv.Command != "go" || strings.Join(srv.Args, " ") != "tool game mcp" {
		t.Fatalf("claude server config = %+v (ok=%v)", srv, ok)
	}

	// Verify .codex/config.toml content.
	codexStr := string(codexFirst)
	if !strings.Contains(codexStr, "[mcp_servers."+editormcp.ServerName+"]") ||
		!strings.Contains(codexStr, `command = "go"`) ||
		!strings.Contains(codexStr, `args = ["tool", "game", "mcp"]`) {
		t.Fatalf("unexpected .codex/config.toml:\n%s", codexStr)
	}

	// Running Install a second time (even from a subdirectory) produces identical files byte for byte.
	second, err := editormcp.Install(filepath.Join(dir, "scenes"))
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	for _, f := range second.Files {
		if f.Changed {
			t.Errorf("second Install %s Changed = true, want false", f.RelPath)
		}
	}
	claudeSecond, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	codexSecond, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(claudeFirst, claudeSecond) {
		t.Fatalf(".mcp.json changed on second run:\n--- first ---\n%s\n--- second ---\n%s", claudeFirst, claudeSecond)
	}
	if !bytes.Equal(codexFirst, codexSecond) {
		t.Fatalf(".codex/config.toml changed on second run:\n--- first ---\n%s\n--- second ---\n%s", codexFirst, codexSecond)
	}

	// Existing unrelated MCP servers and Codex settings are preserved across Install.
	preClaude := `{
  "mcpServers": {
    "other-server": {
      "command": "npx",
      "args": ["other-mcp"]
    }
  }
}
`
	preCodex := `# Existing Codex config
model = "o4-mini"

[mcp_servers.other]
command = "npx"
args = ["other"]
`
	if err := os.WriteFile(claudePath, []byte(preClaude), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(preCodex), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := editormcp.Install(dir); err != nil {
		t.Fatalf("Install over existing configs: %v", err)
	}
	afterClaude, _ := os.ReadFile(claudePath)
	if !strings.Contains(string(afterClaude), `"other-server"`) || !strings.Contains(string(afterClaude), `"`+editormcp.ServerName+`"`) {
		t.Fatalf(".mcp.json lost existing server:\n%s", afterClaude)
	}
	afterCodex, _ := os.ReadFile(codexPath)
	if !strings.Contains(string(afterCodex), `model = "o4-mini"`) || !strings.Contains(string(afterCodex), "[mcp_servers.other]") || !strings.Contains(string(afterCodex), "[mcp_servers."+editormcp.ServerName+"]") {
		t.Fatalf(".codex/config.toml lost existing config:\n%s", afterCodex)
	}
	// Running again after merge is also byte-identical.
	if _, err := editormcp.Install(dir); err != nil {
		t.Fatal(err)
	}
	afterCodex2, _ := os.ReadFile(codexPath)
	if !bytes.Equal(afterCodex, afterCodex2) {
		t.Fatalf("merged .codex/config.toml not idempotent:\n%s\nvs\n%s", afterCodex, afterCodex2)
	}

	// Invalid JSON in .mcp.json is refused without clobbering.
	bad := []byte("{not valid json")
	if err := os.WriteFile(claudePath, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := editormcp.Install(dir); err == nil {
		t.Fatal("Install succeeded on invalid .mcp.json, want error")
	}
	if got, _ := os.ReadFile(claudePath); !bytes.Equal(got, bad) {
		t.Fatalf("invalid .mcp.json was overwritten: %q", got)
	}

	// Outside any project returns an error.
	if _, err := editormcp.Install(t.TempDir()); err == nil {
		t.Fatal("Install outside project succeeded, want error")
	}
}

func TestServerTracksProxyClientInfoAndDisconnect(t *testing.T) {
	endpointDir(t)
	dir := t.TempDir()
	writeProject(t, dir)
	root := mustRoot(t, dir)

	ed := newEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	// Prevent real timer races in unit test.
	ed.AgentScheduleAfter = func(_ time.Duration, _ func()) func() { return func() {} }

	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}

	st, ct := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- editormcp.NewProxy(root).Run(ctx, st) }()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Make a call so the proxy is guaranteed to have connected to the editor's socket.
	mustOK(t, call(t, cs, editormcp.ToolSelectionGet, nil))

	readStatus := func() (editor.AgentStatus, string, []editor.AgentTurn) {
		var status editor.AgentStatus
		var tip string
		var turns []editor.AgentTurn
		if err := s.Editor(context.Background(), func(ed *editor.Editor) error {
			status = ed.AgentStatus()
			tip = ed.AgentStatusTooltip()
			turns = ed.AgentTurns()
			return nil
		}); err != nil {
			t.Fatalf("s.Editor: %v", err)
		}
		return status, tip, turns
	}

	status, tip, turns := readStatus()
	if status != editor.AgentStatusConnected {
		t.Fatalf("AgentStatus = %q, want connected", status)
	}
	if tip != "Claude Code over stdio" {
		t.Fatalf("AgentStatusTooltip = %q, want %q", tip, "Claude Code over stdio")
	}
	if len(turns) == 0 || !turns[0].External {
		t.Fatalf("expected external terminal turn recorded, got %+v", turns)
	}

	// Dropping the client transitions the editor into Reconnecting.
	_ = cs.Close()
	cancel()
	<-done

	// Wait briefly for server's serveConn goroutine to observe EOF and notify the editor.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, _, _ = readStatus()
		if status != editor.AgentStatusConnected {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if status != editor.AgentStatusReconnecting && status != editor.AgentStatusDisconnected {
		t.Fatalf("after drop AgentStatus = %q, want reconnecting or disconnected", status)
	}
}

func TestInstallPreservesModeRefusesSymlinksAndHandlesMultilineTOML(t *testing.T) {
	t.Run("default 0o600 and preserves existing 0o600 permissions", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir)

		if _, err := editormcp.Install(dir); err != nil {
			t.Fatalf("Install: %v", err)
		}
		claudePath := filepath.Join(dir, ".mcp.json")
		codexPath := filepath.Join(dir, ".codex", "config.toml")
		for _, path := range []string{claudePath, codexPath} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s mode = %04o, want owner-only (no group/other bits)", path, info.Mode().Perm())
			}
		}

		// Tightly permissioned pre-existing file with secrets must stay 0o600 after update.
		preClaude := `{"mcpServers":{"secret":{"command":"env","env":{"API_KEY":"top-secret"}}}}` + "\n"
		if err := os.WriteFile(claudePath, []byte(preClaude), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(claudePath, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := editormcp.Install(dir); err != nil {
			t.Fatalf("Install over 0o600 .mcp.json: %v", err)
		}
		info, err := os.Stat(claudePath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf(".mcp.json mode widened to %04o, want 0600", info.Mode().Perm())
		}
	})

	t.Run("refuses symlink escaping project root", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir)
		outside := filepath.Join(t.TempDir(), "victim.json")
		orig := []byte(`{"untouched":true}`)
		if err := os.WriteFile(outside, orig, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, ".mcp.json")); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		if _, err := editormcp.Install(dir); err == nil {
			t.Fatal("Install succeeded through symlink outside project root, want error")
		}
		got, _ := os.ReadFile(outside)
		if !bytes.Equal(got, orig) {
			t.Fatalf("outside symlink target was modified: %q", got)
		}
	})

	t.Run("refuses .codex symlink escaping project root", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir)
		outsideDir := t.TempDir()
		if err := os.Symlink(outsideDir, filepath.Join(dir, ".codex")); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		if _, err := editormcp.Install(dir); err == nil {
			t.Fatal("Install succeeded through .codex symlink outside project root, want error")
		}
		if _, err := os.Stat(filepath.Join(outsideDir, "config.toml")); !os.IsNotExist(err) {
			t.Fatalf("config.toml was written outside project root via .codex symlink")
		}
	})

	t.Run("handles multi-line TOML arrays and refuses malformed headers", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir)
		codexDir := filepath.Join(dir, ".codex")
		if err := os.MkdirAll(codexDir, 0o700); err != nil {
			t.Fatal(err)
		}
		codexPath := filepath.Join(codexDir, "config.toml")
		pre := `model = "o4-mini"

[mcp_servers.` + editormcp.ServerName + `]
command = "old"
args = [
  "old-arg",
  "[not-a-section]",
]

[mcp_servers.other]
command = "npx"
args = [
  "other-arg"
]
`
		if err := os.WriteFile(codexPath, []byte(pre), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := editormcp.Install(dir); err != nil {
			t.Fatalf("Install with multi-line TOML arrays: %v", err)
		}
		after, err := os.ReadFile(codexPath)
		if err != nil {
			t.Fatal(err)
		}
		s := string(after)
		if strings.Contains(s, "old-arg") || strings.Contains(s, "[not-a-section]") {
			t.Fatalf("stale multi-line array elements survived section replacement:\n%s", s)
		}
		if !strings.Contains(s, "[mcp_servers.other]") || !strings.Contains(s, `"other-arg"`) {
			t.Fatalf("unrelated [mcp_servers.other] section was lost:\n%s", s)
		}

		// Malformed TOML header must be refused without overwriting.
		bad := []byte("[unclosed_header\ncommand = \"go\"\n")
		if err := os.WriteFile(codexPath, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := editormcp.Install(dir); err == nil {
			t.Fatal("Install succeeded on malformed .codex/config.toml, want error")
		}
		got, _ := os.ReadFile(codexPath)
		if !bytes.Equal(got, bad) {
			t.Fatalf("malformed .codex/config.toml was overwritten: %q", got)
		}
	})
}
