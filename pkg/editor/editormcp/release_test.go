package editormcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/pkg/editor/keychain"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

// Verifies an Autonomous MCP agent can edit scenes and draft release notes,
// but calling release_review_uploads NEVER starts an upload without person confirmation in Review uploads.
func TestReleaseMCPToolsDraftNotesAndNeverUploadWithoutConfirmation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: Dungeon Run
id: dev.dungeonrun.game
appVersion: 0.4.0
window:
  width: 640
  height: 360
export:
  presets:
    - platform: web
    - platform: android
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: Dungeon Run\nid: dev.dungeonrun.game\nversion: 0.4.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.Keychain = keychain.NewMemory()
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	ed.GitRunner = func(_ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --short HEAD":
			return "5b0e8a4\n", nil
		case "rev-parse --abbrev-ref HEAD":
			return "main\n", nil
		case "describe --tags --abbrev=0":
			return "v0.3.2\n", nil
		case "rev-list --count v0.3.2..HEAD":
			return "3\n", nil
		case "log --pretty=format:%s -n 32 v0.3.2..HEAD":
			return "feat: coins worth 25 points\nfix: knight draw order\nfeat: slimes turn at ledges\n", nil
		default:
			return "", nil
		}
	}
	uploadCalls := 0
	ed.ReleaseUploader = editor.ReleaseUploaderFunc(func(context.Context, editor.ReleaseUploadRequest, func(editor.ReleaseUploadProgress)) (editor.ReleaseUploadResult, error) {
		uploadCalls++
		return editor.ReleaseUploadResult{}, nil
	})

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAgentPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestItchIO, "studio", "itch-token"); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestGooglePlay, "studio", "play-token"); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(ed, editormcp.Options{})
	defer s.Close()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, st); err != nil {
		t.Fatal(err)
	}
	agent, err := mcp.NewClient(&mcp.Implementation{Name: "autonomous-agent"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	// 1. Agent drafts release notes via MCP -> saved directly to release-notes.md.
	res, err := agent.CallTool(ctx, &mcp.CallToolParams{
		Name: editormcp.ToolReleaseNotesDraft,
		Arguments: map[string]any{
			"kept_reviews": []string{"Floor 1 has a slime guarding the plank"},
		},
	})
	if err != nil {
		t.Fatalf("release_notes_draft failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("release_notes_draft error: %v", res.Content)
	}
	notesOnDisk, err := os.ReadFile(filepath.Join(dir, editor.DefaultReleaseNotesFile))
	if err != nil {
		t.Fatalf("expected release-notes.md on disk: %v", err)
	}
	if !strings.Contains(string(notesOnDisk), "Coins worth 25 points") || !strings.Contains(string(notesOnDisk), "Fixed: knight draw order") {
		t.Fatalf("unexpected drafted release notes:\n%s", string(notesOnDisk))
	}

	// 2. Autonomous agent calls release_review_uploads -> opens Review uploads dialog, ZERO uploads sent!
	res, err = agent.CallTool(ctx, &mcp.CallToolParams{
		Name:      editormcp.ToolReleaseReviewUploads,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("release_review_uploads failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("release_review_uploads error: %v", res.Content)
	}
	snap := ed.BuildReleaseSnapshot()
	if !snap.UploadReview.Open || len(snap.UploadReview.Items) != 2 {
		t.Fatalf("expected Review uploads open with 2 items, got %+v", snap.UploadReview)
	}
	if uploadCalls != 0 {
		t.Fatalf("MCP release_review_uploads started %d uploads, want 0", uploadCalls)
	}

	// 3. Even if an MCP tool handler attempted to call ConfirmReleaseUploads through Server.Editor,
	// asAgent marks the actor as an agent so ConfirmReleaseUploads MUST fail!
	err = s.Editor(ctx, func(ed *editor.Editor) error {
		return ed.ConfirmReleaseUploads(snap.UploadReview.SessionID)
	})
	if err != editor.ErrUploadRequiresPersonConfirmation {
		t.Fatalf("ConfirmReleaseUploads from MCP agent = %v, want ErrUploadRequiresPersonConfirmation", err)
	}
	if uploadCalls != 0 {
		t.Fatalf("uploads started from MCP agent = %d, want 0", uploadCalls)
	}
}
