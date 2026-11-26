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

	if ed.AgentStatus() != editor.AgentStatusConnected {
		t.Fatalf("AgentStatus = %q, want connected", ed.AgentStatus())
	}
	if tip := ed.AgentStatusTooltip(); tip != "Claude Code over stdio" {
		t.Fatalf("AgentStatusTooltip = %q, want %q", tip, "Claude Code over stdio")
	}
	if turns := ed.AgentTurns(); len(turns) == 0 || !turns[0].External {
		t.Fatalf("expected external terminal turn recorded, got %+v", turns)
	}

	// Dropping the client transitions the editor into Reconnecting.
	_ = cs.Close()
	cancel()
	<-done

	// Wait briefly for server's serveConn goroutine to observe EOF and notify the editor.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && ed.AgentStatus() == editor.AgentStatusConnected {
		time.Sleep(5 * time.Millisecond)
	}
	if ed.AgentStatus() != editor.AgentStatusReconnecting && ed.AgentStatus() != editor.AgentStatusDisconnected {
		t.Fatalf("after drop AgentStatus = %q, want reconnecting or disconnected", ed.AgentStatus())
	}
}
