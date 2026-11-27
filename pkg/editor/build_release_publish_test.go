package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func runGitIsolated(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func snapshotDirFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// #1050 Acceptance 1:
// Drafted release notes read real git commits since the last tag (or initial commit) + kept reviews + changelog fragments,
// write to the exact Markdown file `game release -release-notes` reads, and editing them modifies ONLY that file.
func TestBuildReleaseNotesDraftedFromGitCommitsAndSavedToExactFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	// Ensure isolated git environment even if host has no global git config.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	dir := writeBuildReleaseProject(t, `export:
  output: dist/release
  presets:
    - platform: web
`)
	// Initialize real git repo with local user.name & user.email.
	runGitIsolated(t, dir, "init", "-b", "main")
	runGitIsolated(t, dir, "config", "user.name", "Test Author")
	runGitIsolated(t, dir, "config", "user.email", "test@example.com")
	runGitIsolated(t, dir, "add", ".")
	runGitIsolated(t, dir, "commit", "-m", "chore: initial project")
	runGitIsolated(t, dir, "tag", "v0.3.2")

	// Add commits since v0.3.2 (mix of player-facing feat/fix and internal chore).
	commits := []struct {
		file string
		body string
		msg  string
	}{
		{"README.md", "v1\n", "feat(coins): coins are worth 25 points instead of 10"},
		{"README.md", "v2\n", "feat(ai): slimes turn at ledges instead of walking off"},
		{"README.md", "v3\n", "fix(render): the Knight was drawn behind slimes"},
		{"README.md", "v4\n", "chore(ci): bump build cache key"},
	}
	for _, c := range commits {
		if err := os.WriteFile(filepath.Join(dir, c.file), []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitIsolated(t, dir, "add", c.file)
		runGitIsolated(t, dir, "commit", "-m", c.msg)
	}

	// Add a changelog fragment under changelog.d/.
	if err := os.MkdirAll(filepath.Join(dir, "changelog.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "changelog.d", "door-creak.md"), []byte("### Added\n\n- The door creaks and stays locked until you pull the lever\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Second, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	// Configure the exact -release-notes file path that `game release -release-notes` will read.
	if err := ed.SetReleaseNotesFile("notes/0.4.0.md"); err != nil {
		t.Fatalf("SetReleaseNotesFile: %v", err)
	}
	ed.SetReleaseKeptReviews([]string{
		"Floor 1 has a slime guarding the plank and a coin trail to the door",
	})

	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	snap := ed.BuildReleaseSnapshot()
	if snap.CommitsSinceRelease != 4 {
		t.Fatalf("CommitsSinceRelease = %d, want 4", snap.CommitsSinceRelease)
	}
	if snap.LastReleaseVersion != "0.3.2" {
		t.Fatalf("LastReleaseVersion = %q, want 0.3.2", snap.LastReleaseVersion)
	}
	if !strings.Contains(snap.ReleaseNotesProvenance, "Drafted from 4 commits") || !strings.Contains(snap.ReleaseNotesProvenance, "since 0.3.2") {
		t.Fatalf("ReleaseNotesProvenance = %q, want 'Drafted from 4 commits ... since 0.3.2'", snap.ReleaseNotesProvenance)
	}
	if snap.ReleaseNotesFile != "notes/0.4.0.md" {
		t.Fatalf("ReleaseNotesFile = %q, want notes/0.4.0.md", snap.ReleaseNotesFile)
	}

	// Verify the drafted Markdown was written to the exact file on disk and is player-facing.
	notesAbs := filepath.Join(dir, "notes", "0.4.0.md")
	diskBytes, err := os.ReadFile(notesAbs)
	if err != nil {
		t.Fatalf("expected drafted notes at %s: %v", notesAbs, err)
	}
	diskNotes := string(diskBytes)
	for _, want := range []string{
		"Floor 1 has a slime guarding the plank and a coin trail to the door.",
		"- The door creaks and stays locked until you pull the lever",
		"- Coins are worth 25 points instead of 10",
		"- Slimes turn at ledges instead of walking off",
		"- Fixed: the Knight was drawn behind slimes",
	} {
		if !strings.Contains(diskNotes, want) {
			t.Fatalf("drafted notes missing %q:\n%s", want, diskNotes)
		}
	}
	// Internal CI chore should be omitted when player-facing commits exist.
	if strings.Contains(diskNotes, "bump build cache key") {
		t.Fatalf("internal chore commit leaked into player-facing notes:\n%s", diskNotes)
	}

	// Verify `game release -dry-run -release-notes <notesAbs>` reads that exact file and matches the Build & Release dry-run plan.
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{
		DryRun:       true,
		ReleaseNotes: ed.ReleaseNotesFile(),
	}); err != nil {
		t.Fatalf("StartReleaseBuild: %v", err)
	}
	if err := ed.WaitReleaseBuild(); err != nil {
		t.Fatalf("WaitReleaseBuild: %v", err)
	}
	gameBin := buildGameTool(t, "../../cmd/game", "game")
	cliOut, cliErr := runBuiltTool(t, gameBin, "release", "-dry-run", "-release-notes", notesAbs, dir)
	if cliErr != "" && !strings.Contains(cliErr, "no assets section") {
		t.Fatalf("game release stderr = %q", cliErr)
	}
	if got := ed.ReleaseDryRunOutput(); got != cliOut {
		t.Fatalf("BuildRelease dry-run output mismatch with game release -release-notes:\ngot:\n%s\nwant:\n%s", got, cliOut)
	}
	if !strings.Contains(cliOut, "-release-notes") || !strings.Contains(cliOut, "0.4.0.md") {
		t.Fatalf("expected -release-notes path in command: %s", cliOut)
	}

	// Snapshot EVERY file in the project, then edit release notes in place and assert ONLY notes/0.4.0.md changed!
	before := snapshotDirFiles(t, dir)
	editedText := "Floor 1 update!\n\n- Coins now grant 25 points\n- Fixed Knight draw order"
	ed.SetReleaseNotesText(editedText)
	after := snapshotDirFiles(t, dir)

	if len(before) != len(after) {
		t.Fatalf("editing release notes changed file count: before=%d after=%d", len(before), len(after))
	}
	var changedFiles []string
	for rel, afterData := range after {
		beforeData, ok := before[rel]
		if !ok || !bytes.Equal(beforeData, afterData) {
			changedFiles = append(changedFiles, rel)
		}
	}
	if !slices.Equal(changedFiles, []string{"notes/0.4.0.md"}) {
		t.Fatalf("editing release notes changed %v, want ONLY [notes/0.4.0.md]", changedFiles)
	}
	if got := strings.TrimSpace(string(after["notes/0.4.0.md"])); got != editedText {
		t.Fatalf("notes/0.4.0.md content = %q, want %q", got, editedText)
	}

	// Verify pluggable ReleaseNotesDrafter (agent drafter) + RedraftReleaseNotes writes ONLY notes/0.4.0.md.
	drafterCalled := false
	ed.ReleaseNotesDrafter = func(ctx context.Context, req editor.ReleaseNotesDraftRequest) (editor.ReleaseNotesDraft, error) {
		drafterCalled = true
		if req.CommitCount != 4 || req.SinceRef != "0.3.2" {
			t.Errorf("drafter req = (%d, %q), want (4, 0.3.2)", req.CommitCount, req.SinceRef)
		}
		return editor.ReleaseNotesDraft{
			Markdown:   "Agent-crafted player notes for " + req.Version,
			Provenance: "Drafted by agent from 4 commits since 0.3.2",
		}, nil
	}
	beforeRedraft := snapshotDirFiles(t, dir)
	draft, err := ed.RedraftReleaseNotes()
	if err != nil {
		t.Fatalf("RedraftReleaseNotes: %v", err)
	}
	if !drafterCalled {
		t.Fatal("expected custom ReleaseNotesDrafter to be called")
	}
	if draft.Markdown != "Agent-crafted player notes for "+ed.Project.AppVersion {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	afterRedraft := snapshotDirFiles(t, dir)
	for rel, afterData := range afterRedraft {
		if rel == "notes/0.4.0.md" {
			if !strings.Contains(string(afterData), "Agent-crafted player notes") {
				t.Fatalf("notes/0.4.0.md not updated by Redraft: %q", string(afterData))
			}
			continue
		}
		if !bytes.Equal(beforeRedraft[rel], afterData) {
			t.Fatalf("Redraft unexpectedly modified %s", rel)
		}
	}
}

// Verifies untagged git repos draft from commits since initial commit and refuse symlink escape on release-notes.md.
func TestBuildReleaseNotesUntaggedRepoAndSymlinkContainment(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: web
`)
	runGitIsolated(t, dir, "init", "-b", "main")
	runGitIsolated(t, dir, "config", "user.name", "Test Author")
	runGitIsolated(t, dir, "config", "user.email", "test@example.com")
	runGitIsolated(t, dir, "add", ".")
	runGitIsolated(t, dir, "commit", "-m", "feat: playable dungeon room")

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	snap := ed.BuildReleaseSnapshot()
	if snap.CommitsSinceRelease != 1 || snap.ReleaseNotesSinceRef != "initial commit" {
		t.Fatalf("untagged repo metadata = (%d, %q), want (1, initial commit)", snap.CommitsSinceRelease, snap.ReleaseNotesSinceRef)
	}
	if snap.ReleaseNotesProvenance != "Drafted from 1 commit since initial commit" {
		t.Fatalf("ReleaseNotesProvenance = %q", snap.ReleaseNotesProvenance)
	}
	if data, err := os.ReadFile(filepath.Join(dir, editor.DefaultReleaseNotesFile)); err != nil || !strings.Contains(string(data), "Playable dungeon room") {
		t.Fatalf("default release-notes.md = (%q, %v)", string(data), err)
	}

	// Refuse escaping path or symlink target for SetReleaseNotesFile.
	if err := ed.SetReleaseNotesFile("../escape.md"); err == nil {
		t.Fatal("expected escaping release notes path to be refused")
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "evil-notes.md")); err == nil {
		if err := ed.SetReleaseNotesFile("evil-notes.md"); err == nil {
			t.Fatal("expected symlinked release notes file to be refused")
		}
		if got, _ := os.ReadFile(outside); string(got) != "untouched" {
			t.Fatalf("outside file modified: %q", string(got))
		}
	}
}

// #1050 Acceptance 2:
// No upload starts without the person confirming it in Review uploads, even with an Autonomous/allow-all agent.
// Review uploads lists every artifact, its size, destination (itch.io, Google Play internal testing, TestFlight), and version before anything is sent.
func TestBuildReleaseUploadsAlwaysAskFirstEvenWithAutonomousAgent(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  output: dist
  presets:
    - platform: web
    - platform: android
      targets: [android_arm64]
    - platform: ios
`)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	kc := editor.NewMemoryKeychain()
	ed.Keychain = kc
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	ed.ReleaseReadinessInspector = func(platform, _ string, _ *project.File) []editor.BuildReadinessItem {
		return nil // all 3 platforms ready
	}
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return nil
	}
	ed.ReleaseMeasureRunner = func(ctx context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		switch platform {
		case "web":
			return editor.BuildArtifactResult{
				ArtifactPath: filepath.Join(plan.OutputDir, "game-0.4.0-web.zip"),
				ArtifactName: "game-0.4.0-web.zip",
				SizeBytes:    mbBytes(18.9),
			}, nil
		case "android":
			return editor.BuildArtifactResult{
				ArtifactPath: filepath.Join(plan.OutputDir, "game-0.4.0.aab"),
				ArtifactName: "game-0.4.0.aab",
				SizeBytes:    mbBytes(34.2),
			}, nil
		default:
			return editor.BuildArtifactResult{
				ArtifactPath: filepath.Join(plan.OutputDir, "game-0.4.0.ipa"),
				ArtifactName: "game-0.4.0.ipa",
				SizeBytes:    mbBytes(38.6),
			}, nil
		}
	}

	var (
		mu      sync.Mutex
		uploads []editor.ReleaseUploadRequest
		failIOS = true
	)
	ed.ReleaseUploader = editor.ReleaseUploaderFunc(func(ctx context.Context, req editor.ReleaseUploadRequest, progress func(editor.ReleaseUploadProgress)) (editor.ReleaseUploadResult, error) {
		progress(editor.ReleaseUploadProgress{Fraction: 0.5, Message: "Uploading package…"})
		mu.Lock()
		uploads = append(uploads, req)
		shouldFail := failIOS && req.Destination == editor.PublishDestTestFlight
		mu.Unlock()
		if shouldFail {
			return editor.ReleaseUploadResult{}, errors.New("App Store Connect rejected duplicate build number")
		}
		progress(editor.ReleaseUploadProgress{Fraction: 1, Message: "Uploaded"})
		return editor.ReleaseUploadResult{
			Destination: req.Destination,
			Platform:    req.Platform,
			Version:     req.Version,
			URL:         "https://store.example/" + string(req.Destination) + "/" + req.Version,
			Message:     "Uploaded to " + req.DestinationLabel,
		}, nil
	})

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetReleaseVersion("0.4.0"); err != nil {
		t.Fatal(err)
	}
	// Grant the agent the most permissive profile ("Autonomous" — allowed everything else).
	if err := ed.SetAgentPermissionProfile(editor.PermissionProfileAutonomous); err != nil {
		t.Fatal(err)
	}
	if !editor.StoreUploadRequiresConfirmation(ed.AgentPermissionProfile()) {
		t.Fatal("StoreUploadRequiresConfirmation must ALWAYS be true even for Autonomous profile")
	}

	// Store credentials in the system keychain (never in project files).
	if err := ed.StorePublishCredential(editor.PublishDestItchIO, "studio-itch", "itch-secret-token-111"); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestGooglePlay, "studio-play", "gplay-secret-json-222"); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestTestFlight, "studio-apple", "apple-secret-key-333"); err != nil {
		t.Fatal(err)
	}

	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	// Build all 3 presets first so artifacts exist in BuildReleaseSnapshot.
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}

	// 1. Direct upload call MUST fail without Review uploads confirmation.
	if err := ed.StartReleaseUpload(); !errors.Is(err, editor.ErrUploadRequiresConfirmation) {
		t.Fatalf("StartReleaseUpload = %v, want ErrUploadRequiresConfirmation", err)
	}
	// 2. ConfirmReleaseUploads with bogus session ID when dialog is closed MUST fail.
	if err := ed.ConfirmReleaseUploads(1); !errors.Is(err, editor.ErrUploadRequiresConfirmation) {
		t.Fatalf("ConfirmReleaseUploads while closed = %v, want ErrUploadRequiresConfirmation", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// 3. Open Review uploads dialog: lists EVERY artifact, destination, size, and version — ZERO uploads sent!
	app.Press("id:build-release-review-uploads")
	app.Settle()
	if !app.Exists("id:build-review-uploads-dialog") {
		t.Fatal("Review uploads confirmation dialog did not open")
	}
	snap := ed.BuildReleaseSnapshot()
	rev := snap.UploadReview
	if !rev.Open || rev.SessionID == 0 {
		t.Fatalf("UploadReview not open with valid SessionID: %+v", rev)
	}
	if len(rev.Items) != 3 {
		t.Fatalf("UploadReview items = %d, want 3 (web, android, ios)", len(rev.Items))
	}
	wantDests := map[string]struct {
		dest  editor.PublishDestination
		label string
		name  string
	}{
		"web":     {editor.PublishDestItchIO, "itch.io", "game-0.4.0-web.zip"},
		"android": {editor.PublishDestGooglePlay, "Google Play · internal testing", "game-0.4.0.aab"},
		"ios":     {editor.PublishDestTestFlight, "TestFlight", "game-0.4.0.ipa"},
	}
	for _, it := range rev.Items {
		w, ok := wantDests[it.Platform]
		if !ok {
			t.Fatalf("unexpected upload item platform %q", it.Platform)
		}
		if it.Destination != w.dest || it.DestinationLabel != w.label || it.ArtifactName != w.name || it.Version != "0.4.0" || it.SizeBytes <= 0 || !it.HasCredential {
			t.Fatalf("unexpected upload item for %s: %+v", it.Platform, it)
		}
		if !app.Exists("id:build-upload-item-" + it.Platform) {
			t.Fatalf("missing UI row for %s", it.Platform)
		}
	}

	// 4. Even with Autonomous profile AND open dialog AND real sessionID, an AGENT actor CANNOT confirm uploads!
	agentErr := ed.WithAgentActor(func() error {
		return ed.ConfirmReleaseUploads(rev.SessionID)
	})
	if !errors.Is(agentErr, editor.ErrUploadRequiresPersonConfirmation) {
		t.Fatalf("agent ConfirmReleaseUploads = %v, want ErrUploadRequiresPersonConfirmation", agentErr)
	}
	mu.Lock()
	if len(uploads) != 0 {
		t.Fatalf("uploads started before person confirmed: %d", len(uploads))
	}
	mu.Unlock()

	// 5. Pressing Cancel closes Review uploads without starting any upload.
	app.Press("id:build-review-uploads-cancel")
	app.Settle()
	if ed.BuildReleaseSnapshot().UploadReview.Open {
		t.Fatal("Review uploads should be closed after Cancel")
	}
	if err := ed.ConfirmReleaseUploads(rev.SessionID); !errors.Is(err, editor.ErrUploadRequiresConfirmation) {
		t.Fatalf("ConfirmReleaseUploads after Cancel = %v, want ErrUploadRequiresConfirmation", err)
	}
	mu.Lock()
	if len(uploads) != 0 {
		t.Fatalf("uploads started after Cancel: %d", len(uploads))
	}
	mu.Unlock()

	// 6. Re-open Review uploads and have the PERSON press Confirm and upload 3.
	app.Press("id:build-release-review-uploads")
	app.Settle()
	confirmedSession := ed.BuildReleaseSnapshot().UploadReview.SessionID
	app.Press("id:build-review-uploads-confirm")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseUploadRunning() })
	app.Settle()

	mu.Lock()
	gotUploads := slices.Clone(uploads)
	mu.Unlock()
	if len(gotUploads) != 3 {
		t.Fatalf("confirmed uploads count = %d, want 3", len(gotUploads))
	}
	// Verify exact artifact, destination, version, release notes path, and keychain credentials were passed.
	for _, u := range gotUploads {
		if u.Version != "0.4.0" {
			t.Fatalf("upload %s version = %q, want 0.4.0", u.Platform, u.Version)
		}
		if !strings.HasSuffix(u.ReleaseNotesPath, editor.DefaultReleaseNotesFile) {
			t.Fatalf("upload %s release notes path = %q", u.Platform, u.ReleaseNotesPath)
		}
		switch u.Destination {
		case editor.PublishDestItchIO:
			if u.Credential.Secret != "itch-secret-token-111" {
				t.Fatalf("itch credential mismatch")
			}
		case editor.PublishDestGooglePlay:
			if u.Credential.Secret != "gplay-secret-json-222" {
				t.Fatalf("google play credential mismatch")
			}
		case editor.PublishDestTestFlight:
			if u.Credential.Secret != "apple-secret-key-333" {
				t.Fatalf("testflight credential mismatch")
			}
		}
	}

	// Verify per-item completion and error reporting (web & android Done, ios Failed with clear error).
	finalRev := ed.BuildReleaseSnapshot().UploadReview
	if !finalRev.Completed {
		t.Fatal("expected UploadReview.Completed = true")
	}
	for _, it := range finalRev.Items {
		switch it.Platform {
		case "web", "android":
			if it.Status != editor.UploadStatusDone || it.Progress != 1 || it.ResultURL == "" {
				t.Fatalf("%s expected done with URL: %+v", it.Platform, it)
			}
		case "ios":
			if it.Status != editor.UploadStatusFailed || !strings.Contains(it.Err, "duplicate build number") {
				t.Fatalf("ios expected failed with error message: %+v", it)
			}
		}
	}

	// 7. Replaying the consumed confirmation sessionID MUST fail without triggering any more uploads.
	if err := ed.ConfirmReleaseUploads(confirmedSession); !errors.Is(err, editor.ErrUploadRequiresConfirmation) {
		t.Fatalf("replayed ConfirmReleaseUploads = %v, want ErrUploadRequiresConfirmation", err)
	}
}

// #1050 Acceptance 3:
// Credentials stay in the system keychain, are never serialized into snapshots or logs,
// and are NEVER written into any project file on disk.
func TestBuildReleaseCredentialsStayInKeychainNeverInProjectFiles(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: web
    - platform: android
    - platform: ios
`)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	kc := editor.NewMemoryKeychain()
	ed.Keychain = kc
	ed.ReleaseCheckRunner = func(ctx context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	ed.ReleaseReadinessInspector = func(platform, _ string, _ *project.File) []editor.BuildReadinessItem {
		return nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	secrets := map[editor.PublishDestination]string{
		editor.PublishDestItchIO:     "SUPER-SECRET-ITCH-TOKEN-98765",
		editor.PublishDestGooglePlay: "SUPER-SECRET-GPLAY-KEY-54321",
		editor.PublishDestTestFlight: "SUPER-SECRET-TESTFLIGHT-KEY-13579",
	}
	for dest, sec := range secrets {
		if err := ed.StorePublishCredential(dest, "publisher@example.com", sec); err != nil {
			t.Fatalf("StorePublishCredential(%s): %v", dest, err)
		}
		if !ed.HasPublishCredential(dest) {
			t.Fatalf("HasPublishCredential(%s) = false after store", dest)
		}
	}
	if kc.Len() != 3 {
		t.Fatalf("keychain entry count = %d, want 3", kc.Len())
	}

	// Verify BuildReleaseSnapshot JSON never contains any secret.
	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	snapJSON, err := json.Marshal(ed.BuildReleaseSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range secrets {
		if strings.Contains(string(snapJSON), sec) {
			t.Fatalf("secret leaked into BuildReleaseSnapshot JSON")
		}
	}

	// Verify KeychainCredential redacts secrets in String/GoString and JSON.
	cred := editor.KeychainCredential{
		Destination: editor.PublishDestItchIO,
		Account:     "publisher@example.com",
		Secret:      secrets[editor.PublishDestItchIO],
	}
	if strings.Contains(cred.String(), cred.Secret) || strings.Contains(cred.GoString(), cred.Secret) {
		t.Fatalf("KeychainCredential did not redact Secret: %q", cred.String())
	}
	credJSON, _ := json.Marshal(cred)
	if strings.Contains(string(credJSON), cred.Secret) {
		t.Fatalf("KeychainCredential serialized Secret into JSON: %s", credJSON)
	}

	// Walk the ENTIRE project directory and assert ZERO files contain any secret!
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		for dest, sec := range secrets {
			if bytes.Contains(data, []byte(sec)) {
				t.Fatalf("secret for %s was written into project file %s!", dest, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify DeletePublishCredential removes the secret from the system keychain.
	if err := ed.DeletePublishCredential(editor.PublishDestItchIO); err != nil {
		t.Fatal(err)
	}
	if ed.HasPublishCredential(editor.PublishDestItchIO) {
		t.Fatal("HasPublishCredential(itch.io) still true after delete")
	}
}

func TestBuildReleasePublishSecurityLifecycleFocusAndSpecLight(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: web
    - platform: android
`)

	// 1. Keychain rejects flag/control characters; release notes reject NUL and >256 KiB; unsafe -tag ignored.
	kc := editor.NewMemoryKeychain()
	if err := kc.Set("-bad-service", "acc", "secret"); err == nil {
		t.Fatal("expected keychain to reject leading '-' service")
	}
	if err := kc.Set(editor.KeychainServiceName, "acc\nnewline", "secret"); err == nil {
		t.Fatal("expected keychain to reject newline in account")
	}
	if err := editor.ValidateReleaseNotes("hello\x00world"); err == nil {
		t.Fatal("expected ValidateReleaseNotes to reject NUL byte")
	}
	if err := editor.ValidateReleaseNotes(strings.Repeat("a", (256<<10)+1)); err == nil {
		t.Fatal("expected ValidateReleaseNotes to reject oversized markdown")
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.Keychain = kc
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	ed.ReleaseReadinessInspector = func(platform, _ string, _ *project.File) []editor.BuildReadinessItem {
		return nil
	}
	ed.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return nil
	}
	ed.ReleaseMeasureRunner = func(ctx context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		name := "game-0.4.0-" + platform + ".zip"
		return editor.BuildArtifactResult{
			ArtifactPath: filepath.Join(plan.OutputDir, name),
			ArtifactName: name,
			SizeBytes:    mbBytes(12.5),
		}, nil
	}
	ed.GitRunner = func(_ string, args ...string) (string, error) {
		if strings.Join(args, " ") == "describe --tags --abbrev=0" {
			return "--malicious-flag\n", nil
		}
		if strings.Join(args, " ") == "rev-list --count HEAD" {
			return "1\n", nil
		}
		if strings.Join(args, " ") == "log --pretty=format:%s -n 32 HEAD" {
			return "feat: safe commit\n", nil
		}
		return "", nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestItchIO, "studio", "itch-secret-1"); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestGooglePlay, "studio", "play-secret-1"); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}
	// Unsafe -tag must fall back to initial commit rather than passing --malicious-flag..HEAD.
	if snap := ed.BuildReleaseSnapshot(); snap.ReleaseNotesSinceRef == "--malicious-flag" {
		t.Fatalf("unsafe git tag was accepted as ReleaseNotesSinceRef: %q", snap.ReleaseNotesSinceRef)
	}

	// 2. NaN / +Inf / -Inf upload progress is clamped safely to [0, 1].
	ed.ReleaseUploader = editor.ReleaseUploaderFunc(func(_ context.Context, req editor.ReleaseUploadRequest, progress func(editor.ReleaseUploadProgress)) (editor.ReleaseUploadResult, error) {
		progress(editor.ReleaseUploadProgress{Fraction: math.NaN(), Message: "nan"})
		progress(editor.ReleaseUploadProgress{Fraction: math.Inf(-1), Message: "neg"})
		progress(editor.ReleaseUploadProgress{Fraction: math.Inf(1), Message: "pos"})
		return editor.ReleaseUploadResult{Destination: req.Destination, Platform: req.Platform, Version: req.Version, Message: "Uploaded"}, nil
	})
	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConfirmReleaseUploads(ed.BuildReleaseSnapshot().UploadReview.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseUpload(); err != nil {
		t.Fatalf("WaitReleaseUpload with NaN/Inf progress: %v", err)
	}
	for _, it := range ed.BuildReleaseSnapshot().UploadReview.Items {
		if math.IsNaN(it.Progress) || math.IsInf(it.Progress, 0) || it.Progress < 0 || it.Progress > 1 {
			t.Fatalf("item %s invalid progress after NaN/Inf clamp: %v", it.Platform, it.Progress)
		}
	}
	ed.CloseReviewUploads()

	// 3. CancelReleaseUpload fires automatically on Editor.Close() and OpenDemo() without racing on e.Project.
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	ed.ReleaseUploader = editor.ReleaseUploaderFunc(func(ctx context.Context, _ editor.ReleaseUploadRequest, _ func(editor.ReleaseUploadProgress)) (editor.ReleaseUploadResult, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		select {
		case cancelled <- struct{}{}:
		default:
		}
		return editor.ReleaseUploadResult{}, ctx.Err()
	})
	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConfirmReleaseUploads(ed.BuildReleaseSnapshot().UploadReview.SessionID); err != nil {
		t.Fatal(err)
	}
	<-started
	// Switching to demo cancels the in-flight upload cleanly with no race on e.Project.
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo during active upload: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("OpenDemo did not cancel in-flight release upload")
	}

	// Re-open project on a fresh editor, start upload, and verify Editor.Close() cancels it cleanly too.
	edClose, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	edClose.Keychain = kc
	edClose.ReleaseCheckRunner = ed.ReleaseCheckRunner
	edClose.ReleaseReadinessInspector = ed.ReleaseReadinessInspector
	edClose.ExportCommandRunner = ed.ExportCommandRunner
	edClose.ReleaseMeasureRunner = ed.ReleaseMeasureRunner
	edClose.GitRunner = ed.GitRunner
	edClose.ReleaseUploader = ed.ReleaseUploader
	if err := edClose.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := edClose.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := edClose.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := edClose.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}
	if err := edClose.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	if err := edClose.ConfirmReleaseUploads(edClose.BuildReleaseSnapshot().UploadReview.SessionID); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := edClose.Close(); err != nil {
		t.Fatalf("Editor.Close during active upload: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Editor.Close did not cancel in-flight release upload")
	}
	_ = ed.Close()

	// 4. UI test: Spec Light palette on Review uploads, keychain input survives brRev ticks without focus loss,
	// and Esc closes Review uploads, scrubs kcSecret, and restores focus to build-release-review-uploads.
	edUI, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = edUI.Close() })
	edUI.Keychain = editor.NewMemoryKeychain()
	edUI.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	edUI.ReleaseReadinessInspector = func(platform, _ string, _ *project.File) []editor.BuildReadinessItem {
		return nil
	}
	edUI.ExportCommandRunner = func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return nil
	}
	edUI.ReleaseMeasureRunner = func(ctx context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		name := "game-0.4.0-" + platform + ".zip"
		return editor.BuildArtifactResult{
			ArtifactPath: filepath.Join(plan.OutputDir, name),
			ArtifactName: name,
			SizeBytes:    mbBytes(12.5),
		}, nil
	}
	edUI.GitRunner = func(_ string, _ ...string) (string, error) { return "", nil }
	_ = edUI.SetAppearance(editor.AppearanceLight)
	if err := edUI.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := edUI.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := edUI.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := edUI.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, edUI)
	}, nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: false}), nsgotest.Size(1440, 900))
	app.Settle()

	// Open Review uploads via build-release-review-uploads.
	app.Focus("id:build-release-review-uploads")
	app.Settle()
	app.Press("id:build-release-review-uploads")
	app.Settle()
	if !app.Exists("id:build-review-uploads-dialog") {
		t.Fatal("Review uploads dialog did not open")
	}

	// Verify Spec Light palette surface on Review uploads dialog.
	dlg := app.Find("id:build-review-uploads-dialog")
	lightRaised := editor.LightSpecPalette().Raised
	if dlg.Style.Background != lightRaised {
		t.Fatalf("Spec Light Review uploads background = %v, want %v", dlg.Style.Background, lightRaised)
	}

	// Open keychain drawer, focus secret input, type, trigger brRev bump, and verify input node/focus survived!
	app.Post(func() {
		edUI.OpenKeychainCredentialEditor(editor.PublishDestItchIO)
	})
	app.Settle()
	app.Focus("id:build-keychain-secret")
	app.Type("id:build-keychain-secret", "ephemeral-secret")
	app.Settle()
	secBefore := app.Find("id:build-keychain-secret")
	if !secBefore.Focused {
		t.Fatal("build-keychain-secret did not hold focus before brRev bump")
	}
	app.Post(func() {
		_ = edUI.SetReleaseUploadItemIncluded("web", false)
		_ = edUI.SetReleaseUploadItemIncluded("web", true)
	})
	app.Settle()
	secAfter := app.Find("id:build-keychain-secret")
	if secAfter.ID != secBefore.ID || !secAfter.Focused {
		t.Fatalf("brRev bump remounted or stole focus from build-keychain-secret (ID %d -> %d, focused=%v)", secBefore.ID, secAfter.ID, secAfter.Focused)
	}

	// Press Esc -> closes Review uploads, restores focus to build-release-review-uploads, and scrubs typed secret.
	app.Key("escape")
	app.Advance(250 * time.Millisecond)
	app.Settle()
	if edUI.BuildReleaseSnapshot().UploadReview.Open {
		t.Fatal("Esc did not close Review uploads dialog")
	}
	if btn := app.Find("id:build-release-review-uploads"); !btn.Focused {
		t.Fatal("Esc closing Review uploads did not restore focus to build-release-review-uploads")
	}

	// Re-open Review uploads + keychain drawer: typed secret must have been scrubbed to "".
	app.Press("id:build-release-review-uploads")
	app.Settle()
	app.Post(func() {
		edUI.OpenKeychainCredentialEditor(editor.PublishDestItchIO)
	})
	app.Settle()
	if got := app.Find("id:build-keychain-secret").Text; got != "" {
		t.Fatalf("keychain secret retained %q after dialog close, want empty", got)
	}
}

type fakePublishInvocation struct {
	Args    []string          `json:"args"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	Program string            `json:"program"`
}

// buildFakeCLI compiles a tiny standalone Go executable into binDir/name(.exe) that records each
// invocation (program name, args, working directory, and publish credential env vars) as JSON lines
// to FAKE_PUBLISH_LOG, emits simulated progress/status output, and optionally fails with FAKE_PUBLISH_FAIL.
func buildFakeCLI(t *testing.T, binDir, name string) string {
	t.Helper()
	srcDir := t.TempDir()
	src := `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type record struct {
	Args    []string          ` + "`json:\"args\"`" + `
	Cwd     string            ` + "`json:\"cwd\"`" + `
	Env     map[string]string ` + "`json:\"env\"`" + `
	Program string            ` + "`json:\"program\"`" + `
}

func main() {
	cwd, _ := os.Getwd()
	env := map[string]string{}
	for _, k := range []string{
		"BUTLER_API_KEY",
		"NSGO_PLAY_SERVICE_ACCOUNT",
		"NSGO_APP_STORE_KEY",
		"NSGO_APP_STORE_KEY_ID",
		"NSGO_APP_STORE_ISSUER",
	} {
		if v := os.Getenv(k); v != "" {
			env[k] = v
		}
	}
	rec := record{
		Program: filepath.Base(os.Args[0]),
		Args:    os.Args[1:],
		Cwd:     cwd,
		Env:     env,
	}
	if logPath := os.Getenv("FAKE_PUBLISH_LOG"); logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			b, _ := json.Marshal(rec)
			_, _ = f.Write(append(b, '\n'))
			_ = f.Close()
		}
	}
	if failMsg := os.Getenv("FAKE_PUBLISH_FAIL"); failMsg != "" {
		// Echo credential if FAKE_PUBLISH_ECHO_SECRET is set to verify redaction.
		if os.Getenv("FAKE_PUBLISH_ECHO_SECRET") == "1" {
			fmt.Fprintf(os.Stderr, "%s (key=%s)\n", failMsg, os.Getenv("BUTLER_API_KEY"))
		} else {
			fmt.Fprintln(os.Stderr, failMsg)
		}
		os.Exit(1)
	}

	args := os.Args[1:]
	// Strip leading "tool nsgo" if invoked as fake "go".
	if len(args) >= 2 && args[0] == "tool" && args[1] == "nsgo" {
		args = args[2:]
	}
	joined := strings.Join(args, " ")
	switch {
	case strings.HasPrefix(joined, "publish itch "):
		fmt.Fprintln(os.Stderr, "uploading studio/dungeon-run:channel to itch.io 50%")
		fmt.Fprintln(os.Stdout, "uploaded studio/dungeon-run:channel")
	case strings.HasPrefix(joined, "publish -target android"):
		fmt.Fprintln(os.Stdout, "uploading App Bundle 45%")
		fmt.Fprintln(os.Stdout, "committed dev.dungeonrun.game version code 4 to Google Play track internal")
	case strings.HasPrefix(joined, "publish ios "):
		fmt.Fprintln(os.Stderr, "uploading dev.dungeonrun.game 0.4.0 (4) to App Store Connect and waiting for processing")
		fmt.Fprintln(os.Stderr, "Uploading 80%")
		fmt.Fprintln(os.Stdout, "App Store Connect processed dev.dungeonrun.game 0.4.0 (4)")
	default:
		fmt.Fprintf(os.Stderr, "unexpected command: %v\n", os.Args)
		os.Exit(2)
	}
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	outName := name
	if filepath.Ext(os.Args[0]) == ".exe" {
		outName += ".exe"
	}
	outPath := filepath.Join(binDir, outName)
	cmd := exec.Command("go", "build", "-o", outPath, filepath.Join(srcDir, "main.go"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake %s: %v\n%s", name, err, out)
	}
	return outPath
}

func readFakePublishInvocations(t *testing.T, logPath string) []fakePublishInvocation {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake publish log: %v", err)
	}
	var out []fakePublishInvocation
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec fakePublishInvocation
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal invocation %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// #1762 Acceptance 1:
// Default uploader invokes nsgo publish for itch.io, Google Play internal, and TestFlight with
// credentials passed ONLY in environment variables (never on the command line or in project files),
// streams progress, and returns the printed store URL or processing state.
func TestBuildReleaseDefaultUploaderRunsNSGoPublishWithFakeBinary(t *testing.T) {
	binDir := t.TempDir()
	buildFakeCLI(t, binDir, "nsgo")
	logPath := filepath.Join(t.TempDir(), "invocations.jsonl")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_PUBLISH_LOG", logPath)
	t.Setenv("FAKE_PUBLISH_FAIL", "")
	// Ensure unrelated host env credentials are never leaked to another destination's upload.
	t.Setenv("BUTLER_API_KEY", "host-stale-butler-key")
	t.Setenv("NSGO_PLAY_SERVICE_ACCOUNT", "host-stale-play-key")
	t.Setenv("NSGO_APP_STORE_KEY", "host-stale-apple-key")
	t.Setenv("NSGO_APP_STORE_KEY_ID", "HOSTKEYID")
	t.Setenv("NSGO_APP_STORE_ISSUER", "host-issuer")

	dir := writeBuildReleaseProject(t, `export:
  output: dist
  presets:
    - platform: web
    - platform: macos
      targets: [macos_arm64]
    - platform: android
      targets: [android_arm64]
    - platform: ios
`)
	nsgoYAML := `name: dungeon-run
id: dev.dungeonrun.game
version: 0.4.0
itch:
  project: studio/dungeon-run
  channels:
    macos_arm64: osx-arm64-beta
`
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte(nsgoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	aabPath := filepath.Join(dir, "dist", "dungeon-run-0.4.0.aab")
	ipaPath := filepath.Join(dir, "dist", "dungeon-run-0.4.0.ipa")
	if err := os.WriteFile(aabPath, []byte("fake-aab"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ipaPath, []byte("fake-ipa"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.Keychain = editor.NewMemoryKeychain()
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	ed.ReleaseReadinessInspector = func(string, string, *project.File) []editor.BuildReadinessItem {
		return nil
	}
	ed.ExportCommandRunner = func(context.Context, []string, io.Writer, io.Writer) error {
		return nil
	}
	ed.ReleaseMeasureRunner = func(_ context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		switch platform {
		case "web":
			return editor.BuildArtifactResult{
				ArtifactPath: filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-web.zip"),
				ArtifactName: "dungeon-run-0.4.0-web.zip",
				SizeBytes:    mbBytes(18.9),
			}, nil
		case "macos":
			return editor.BuildArtifactResult{
				ArtifactPath: filepath.Join(plan.OutputDir, "dungeon-run-0.4.0-macos.zip"),
				ArtifactName: "dungeon-run-0.4.0-macos.zip",
				SizeBytes:    mbBytes(32.1),
			}, nil
		case "android":
			return editor.BuildArtifactResult{
				ArtifactPath: aabPath,
				ArtifactName: "dungeon-run-0.4.0.aab",
				SizeBytes:    mbBytes(34.2),
			}, nil
		default:
			return editor.BuildArtifactResult{
				ArtifactPath: ipaPath,
				ArtifactName: "dungeon-run-0.4.0.ipa",
				SizeBytes:    mbBytes(38.6),
			}, nil
		}
	}
	// Leave ed.ReleaseUploader nil so the real defaultReleaseUploader runs!

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetReleaseVersion("0.4.0"); err != nil {
		t.Fatal(err)
	}
	itchSecret := "itch-butler-secret-token-777"
	playSecret := `{"client_email":"bot@play.example","private_key":"FAKE-PLAY-KEY-888"}`
	appleKey := "-----BEGIN PRIVATE KEY-----\nFAKE-APPLE-KEY-999\n-----END PRIVATE KEY-----"
	appleSecret := `{"key":"-----BEGIN PRIVATE KEY-----\nFAKE-APPLE-KEY-999\n-----END PRIVATE KEY-----","key_id":"KEYID12345","issuer":"11111111-2222-3333-4444-555555555555"}`

	if err := ed.StorePublishCredential(editor.PublishDestItchIO, "studio", itchSecret); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestGooglePlay, "studio-play", playSecret); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestTestFlight, "studio-apple", appleSecret); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}

	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	rev := ed.BuildReleaseSnapshot().UploadReview
	if len(rev.Items) != 4 {
		t.Fatalf("Review uploads items = %d, want 4", len(rev.Items))
	}
	wantTracks := map[string]string{
		"web":     "html5",
		"macos":   "osx-arm64-beta",
		"android": "internal track",
		"ios":     "TestFlight beta",
	}
	for _, it := range rev.Items {
		if it.Track != wantTracks[it.Platform] {
			t.Fatalf("item %s Track = %q, want %q", it.Platform, it.Track, wantTracks[it.Platform])
		}
	}

	if err := ed.ConfirmReleaseUploads(rev.SessionID); err != nil {
		t.Fatalf("ConfirmReleaseUploads: %v", err)
	}
	if err := ed.WaitReleaseUpload(); err != nil {
		t.Fatalf("WaitReleaseUpload: %v", err)
	}

	invocations := readFakePublishInvocations(t, logPath)
	if len(invocations) != 4 {
		t.Fatalf("fake nsgo invocations = %d, want 4: %+v", len(invocations), invocations)
	}

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		realDir = dir
	}

	// Verify exact commands, working directory, and env-only credentials.
	wantArgs := [][]string{
		{"publish", "itch", "-target", "web", "-output", "dist", "-version", "0.4.0"},
		{"publish", "itch", "-target", "macos_arm64", "-output", "dist", "-version", "0.4.0"},
		{"publish", "-target", "android", "-track", "internal", filepath.FromSlash("dist/dungeon-run-0.4.0.aab")},
		{"publish", "ios", filepath.FromSlash("dist/dungeon-run-0.4.0.ipa")},
	}
	allSecrets := []string{itchSecret, playSecret, appleSecret, appleKey, "FAKE-PLAY-KEY-888", "FAKE-APPLE-KEY-999"}
	for i, inv := range invocations {
		invCwd, _ := filepath.EvalSymlinks(inv.Cwd)
		if invCwd != realDir {
			t.Fatalf("invocation[%d] cwd = %q, want %q", i, invCwd, realDir)
		}
		if !slices.Equal(inv.Args, wantArgs[i]) {
			t.Fatalf("invocation[%d] args = %q, want %q", i, inv.Args, wantArgs[i])
		}
		joinedArgs := strings.Join(inv.Args, " ")
		for _, sec := range allSecrets {
			if strings.Contains(joinedArgs, sec) {
				t.Fatalf("invocation[%d] leaked credential onto command line: %q", i, joinedArgs)
			}
		}
		switch i {
		case 0, 1:
			if inv.Env["BUTLER_API_KEY"] != itchSecret {
				t.Fatalf("itch invocation[%d] BUTLER_API_KEY = %q, want %q", i, inv.Env["BUTLER_API_KEY"], itchSecret)
			}
			if inv.Env["NSGO_PLAY_SERVICE_ACCOUNT"] != "" || inv.Env["NSGO_APP_STORE_KEY"] != "" {
				t.Fatalf("itch invocation[%d] received unrelated credentials: %+v", i, inv.Env)
			}
		case 2:
			if inv.Env["NSGO_PLAY_SERVICE_ACCOUNT"] != playSecret {
				t.Fatalf("android invocation NSGO_PLAY_SERVICE_ACCOUNT = %q, want %q", inv.Env["NSGO_PLAY_SERVICE_ACCOUNT"], playSecret)
			}
			if inv.Env["BUTLER_API_KEY"] != "" || inv.Env["NSGO_APP_STORE_KEY"] != "" {
				t.Fatalf("android invocation received unrelated credentials: %+v", inv.Env)
			}
		case 3:
			if inv.Env["NSGO_APP_STORE_KEY"] != appleKey || inv.Env["NSGO_APP_STORE_KEY_ID"] != "KEYID12345" || inv.Env["NSGO_APP_STORE_ISSUER"] != "11111111-2222-3333-4444-555555555555" {
				t.Fatalf("ios invocation env = %+v", inv.Env)
			}
			if inv.Env["BUTLER_API_KEY"] != "" || inv.Env["NSGO_PLAY_SERVICE_ACCOUNT"] != "" {
				t.Fatalf("ios invocation received unrelated credentials: %+v", inv.Env)
			}
		}
	}

	// Verify returned URLs and processing states on the Review uploads snapshot.
	finalRev := ed.BuildReleaseSnapshot().UploadReview
	if !finalRev.Completed || finalRev.LastErr != "" {
		t.Fatalf("final UploadReview = %+v", finalRev)
	}
	for _, it := range finalRev.Items {
		if it.Status != editor.UploadStatusDone || it.Progress != 1 {
			t.Fatalf("item %s not Done: %+v", it.Platform, it)
		}
		switch it.Platform {
		case "web", "macos":
			if it.ResultURL != "https://studio.itch.io/dungeon-run" {
				t.Fatalf("%s ResultURL = %q, want https://studio.itch.io/dungeon-run", it.Platform, it.ResultURL)
			}
			if !strings.Contains(it.StatusMessage, "uploaded studio/dungeon-run:channel") {
				t.Fatalf("%s StatusMessage = %q", it.Platform, it.StatusMessage)
			}
		case "android":
			if !strings.Contains(it.StatusMessage, "committed dev.dungeonrun.game version code 4 to Google Play track internal") {
				t.Fatalf("android StatusMessage = %q", it.StatusMessage)
			}
		case "ios":
			if !strings.Contains(it.StatusMessage, "App Store Connect processed dev.dungeonrun.game 0.4.0 (4)") {
				t.Fatalf("ios StatusMessage = %q", it.StatusMessage)
			}
		}
	}

	// Ensure zero credentials were written into any file in the project directory.
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		for _, sec := range allSecrets {
			if bytes.Contains(data, []byte(sec)) {
				t.Fatalf("credential leaked into project file %s", p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// #1762 Acceptance 1 (failure & redaction):
// A failing nsgo publish command surfaces its message on the failed upload item and redacts any secret.
func TestBuildReleaseDefaultUploaderSurfacesFailureAndRedactsSecrets(t *testing.T) {
	binDir := t.TempDir()
	buildFakeCLI(t, binDir, "nsgo")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_PUBLISH_FAIL", "publish itch: set itch.project in nsgo.yaml to owner/game")
	t.Setenv("FAKE_PUBLISH_ECHO_SECRET", "1")

	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: web
`)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.Keychain = editor.NewMemoryKeychain()
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	secret := "SUPER-SECRET-ITCH-KEY-DO-NOT-LEAK"
	if err := ed.StorePublishCredential(editor.PublishDestItchIO, "studio", secret); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	rev := ed.BuildReleaseSnapshot().UploadReview
	if err := ed.ConfirmReleaseUploads(rev.SessionID); err != nil {
		t.Fatal(err)
	}
	waitErr := ed.WaitReleaseUpload()
	if waitErr == nil {
		t.Fatal("expected WaitReleaseUpload to return error on failing nsgo publish")
	}
	if !strings.Contains(waitErr.Error(), "set itch.project in nsgo.yaml") {
		t.Fatalf("WaitReleaseUpload error = %q, want nsgo publish stderr message", waitErr.Error())
	}
	if strings.Contains(waitErr.Error(), secret) {
		t.Fatalf("WaitReleaseUpload error leaked secret: %q", waitErr.Error())
	}
	finalRev := ed.BuildReleaseSnapshot().UploadReview
	if len(finalRev.Items) != 1 || finalRev.Items[0].Status != editor.UploadStatusFailed {
		t.Fatalf("expected failed web upload item: %+v", finalRev.Items)
	}
	if !strings.Contains(finalRev.Items[0].Err, "set itch.project in nsgo.yaml") || !strings.Contains(finalRev.Items[0].Err, "[redacted]") {
		t.Fatalf("failed item Err = %q, want failure message with [redacted]", finalRev.Items[0].Err)
	}
	if strings.Contains(finalRev.Items[0].Err, secret) {
		t.Fatalf("failed item Err leaked secret: %q", finalRev.Items[0].Err)
	}
}

// #1762 Proposal:
// In a Go module whose go.mod pins github.com/nsgo-dev/nsgo/cmd/nsgo as a tool,
// defaultReleaseUploader invokes `go tool nsgo publish ...` in the project directory.
func TestBuildReleaseDefaultUploaderUsesGoToolNSGoInModule(t *testing.T) {
	binDir := t.TempDir()
	buildFakeCLI(t, binDir, "go")
	logPath := filepath.Join(t.TempDir(), "go-invocations.jsonl")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_PUBLISH_LOG", logPath)
	t.Setenv("FAKE_PUBLISH_FAIL", "")
	// Ensure unrelated host env credentials are never leaked to another destination's upload.
	t.Setenv("BUTLER_API_KEY", "host-stale-butler-key")
	t.Setenv("NSGO_PLAY_SERVICE_ACCOUNT", "host-stale-play-key")
	t.Setenv("NSGO_APP_STORE_KEY", "host-stale-apple-key")
	t.Setenv("NSGO_APP_STORE_KEY_ID", "HOSTKEYID")
	t.Setenv("NSGO_APP_STORE_ISSUER", "host-issuer")

	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: web
`)
	goMod := `module example.com/testgame

go 1.27.3

toolchain go1.27.3

tool (
	github.com/nsgo-dev/nsgo/cmd/nsgo
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.Keychain = editor.NewMemoryKeychain()
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetReleaseVersion("0.5.0"); err != nil {
		t.Fatal(err)
	}
	if err := ed.StorePublishCredential(editor.PublishDestItchIO, "studio", "mod-itch-secret"); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConfirmReleaseUploads(ed.BuildReleaseSnapshot().UploadReview.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseUpload(); err != nil {
		t.Fatalf("WaitReleaseUpload: %v", err)
	}

	invocations := readFakePublishInvocations(t, logPath)
	if len(invocations) != 1 {
		t.Fatalf("fake go invocations = %d, want 1", len(invocations))
	}
	want := []string{"tool", "nsgo", "publish", "itch", "-target", "web", "-output", "dist", "-version", "0.5.0"}
	if !slices.Equal(invocations[0].Args, want) {
		t.Fatalf("go args = %q, want %q", invocations[0].Args, want)
	}
	if invocations[0].Env["BUTLER_API_KEY"] != "mod-itch-secret" {
		t.Fatalf("BUTLER_API_KEY = %q, want mod-itch-secret", invocations[0].Env["BUTLER_API_KEY"])
	}
}

// #1762 Acceptance 2 & SteamPipe exclusion:
// Review uploads shows exact itch.io channels (from nsgo.yaml itch.channels or NSGo defaults per built target),
// and excludes Steam-channel desktop artifacts from itch.io, showing them as "upload with SteamPipe".
func TestBuildReleaseReviewUploadsItchChannelsAndSteamPipe(t *testing.T) {
	// 1. DefaultPublishDestinationForPlatform matches NSGo default itch.io channels.
	for _, tc := range []struct {
		platform string
		dest     editor.PublishDestination
		track    string
	}{
		{"web", editor.PublishDestItchIO, "html5"},
		{"macos", editor.PublishDestItchIO, "osx-arm64, osx-x64"},
		{"windows", editor.PublishDestItchIO, "windows-arm64, windows-x64"},
		{"linux", editor.PublishDestItchIO, "linux-arm64, linux-x64"},
		{"macos_arm64", editor.PublishDestItchIO, "osx-arm64"},
		{"android", editor.PublishDestGooglePlay, "internal track"},
		{"ios", editor.PublishDestTestFlight, "TestFlight beta"},
	} {
		dest, track := editor.DefaultPublishDestinationForPlatform(tc.platform)
		if dest != tc.dest || track != tc.track {
			t.Fatalf("DefaultPublishDestinationForPlatform(%q) = (%q, %q), want (%q, %q)", tc.platform, dest, track, tc.dest, tc.track)
		}
	}

	// 2. Review uploads uses nsgo.yaml itch.channels overrides per built target and NSGo defaults otherwise.
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: macos
    - platform: windows
      targets: [windows_x64]
    - platform: linux
    - platform: web
`)
	nsgoYAML := `name: dungeon-run
id: dev.dungeonrun.game
version: 0.4.0
itch:
  project: studio/dungeon-run
  channels:
    macos_arm64: osx-arm64-beta
    linux_x64: linux64-custom
`
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte(nsgoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.Keychain = editor.NewMemoryKeychain()
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	rev := ed.BuildReleaseSnapshot().UploadReview
	wantTracks := map[string]string{
		"macos":   "osx-arm64-beta, osx-x64",
		"windows": "windows-x64",
		"linux":   "linux-arm64, linux64-custom",
		"web":     "html5",
	}
	for _, it := range rev.Items {
		if it.Destination != editor.PublishDestItchIO || it.DestinationLabel != "itch.io" {
			t.Fatalf("%s unexpected dest: %+v", it.Platform, it)
		}
		if it.Track != wantTracks[it.Platform] {
			t.Fatalf("%s Track = %q, want %q", it.Platform, it.Track, wantTracks[it.Platform])
		}
	}
	ed.CloseReviewUploads()

	// 3. Switching to steam channel removes desktop builds from itch.io and shows "upload with SteamPipe",
	// while keeping web on itch.io.
	if err := ed.SetExportChannel("steam"); err != nil {
		t.Fatal(err)
	}
	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	steamRev := ed.BuildReleaseSnapshot().UploadReview
	for _, it := range steamRev.Items {
		switch it.Platform {
		case "macos", "windows", "linux":
			if !it.ManualOnly || it.Destination != "" || it.DestinationLabel != "upload with SteamPipe" || it.Included {
				t.Fatalf("steam desktop %s should be ManualOnly upload with SteamPipe and excluded: %+v", it.Platform, it)
			}
			if err := ed.SetReleaseUploadItemIncluded(it.Platform, true); err == nil {
				t.Fatalf("SetReleaseUploadItemIncluded(%s, true) should refuse SteamPipe item", it.Platform)
			}
		case "web":
			if it.ManualOnly || it.Destination != editor.PublishDestItchIO || it.Track != "html5" || !it.Included {
				t.Fatalf("web should stay on itch.io even when steam channel is active: %+v", it)
			}
		}
	}

	// Render the Review uploads UI and verify SteamPipe badges appear for desktop rows (and no itch key button for them).
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()
	for _, plat := range []string{"macos", "windows", "linux"} {
		if !app.Exists("id:build-upload-manual-" + plat) {
			t.Fatalf("expected SteamPipe manual badge for %s", plat)
		}
		if app.Exists("id:build-upload-key-" + plat) {
			t.Fatalf("SteamPipe item %s must not show itch.io Add key button", plat)
		}
	}
	if !app.Exists("id:build-upload-key-web") {
		t.Fatal("expected itch.io Add key button for web")
	}

	// 4. Project whose nsgo.yaml sets updates.channel: steam and has ONLY desktop presets:
	// none are included and ConfirmReleaseUploads returns ErrNoUploadsSelected.
	steamDir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: macos
    - platform: windows
`)
	if err := os.WriteFile(filepath.Join(steamDir, project.NSGoFileName), []byte("name: steam-game\nid: dev.steam.game\nversion: 1.0.0\nupdates:\n  channel: steam\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edSteam, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = edSteam.Close() })
	edSteam.Keychain = editor.NewMemoryKeychain()
	edSteam.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	if err := edSteam.OpenProject(steamDir); err != nil {
		t.Fatal(err)
	}
	if err := edSteam.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := edSteam.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	onlySteam := edSteam.BuildReleaseSnapshot().UploadReview
	if len(onlySteam.Items) != 2 {
		t.Fatalf("steam-only items = %d, want 2", len(onlySteam.Items))
	}
	for _, it := range onlySteam.Items {
		if !it.ManualOnly || it.Included || it.DestinationLabel != "upload with SteamPipe" {
			t.Fatalf("expected SteamPipe manual item: %+v", it)
		}
	}
	if err := edSteam.ConfirmReleaseUploads(onlySteam.SessionID); !errors.Is(err, editor.ErrNoUploadsSelected) {
		t.Fatalf("ConfirmReleaseUploads with only SteamPipe items = %v, want ErrNoUploadsSelected", err)
	}

	// 5. Building with explicit BuildReleaseOptions{Channel: "steam"} records the channel on desktop
	// presets and shows them as "upload with SteamPipe" even when nsgo.yaml has no steam channel.
	builtSteamDir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: macos
    - platform: web
`)
	edBuiltSteam, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = edBuiltSteam.Close() })
	edBuiltSteam.Keychain = editor.NewMemoryKeychain()
	edBuiltSteam.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	edBuiltSteam.ExportCommandRunner = func(context.Context, []string, io.Writer, io.Writer) error {
		return nil
	}
	if err := edBuiltSteam.OpenProject(builtSteamDir); err != nil {
		t.Fatal(err)
	}
	if err := edBuiltSteam.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := edBuiltSteam.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true, Channel: "steam"}); err != nil {
		t.Fatal(err)
	}
	if err := edBuiltSteam.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}
	if err := edBuiltSteam.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	builtRev := edBuiltSteam.BuildReleaseSnapshot().UploadReview
	for _, it := range builtRev.Items {
		if it.Platform == "macos" && (!it.ManualOnly || it.DestinationLabel != "upload with SteamPipe" || it.Included) {
			t.Fatalf("built steam macos preset should be SteamPipe manual only: %+v", it)
		}
		if it.Platform == "web" && (it.ManualOnly || it.Destination != editor.PublishDestItchIO || it.Track != "html5") {
			t.Fatalf("built web preset should remain on itch.io html5: %+v", it)
		}
	}
}

// #1762: TestFlight keychain credentials also accept key_id:issuer in the account field paired with a raw .p8 key.
func TestBuildReleaseDefaultUploaderTestFlightAccountMetadata(t *testing.T) {
	binDir := t.TempDir()
	buildFakeCLI(t, binDir, "nsgo")
	logPath := filepath.Join(t.TempDir(), "tf-invocations.jsonl")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_PUBLISH_LOG", logPath)
	t.Setenv("FAKE_PUBLISH_FAIL", "")

	dir := writeBuildReleaseProject(t, `export:
  output: dist
  presets:
    - platform: ios
`)
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	ipaPath := filepath.Join(dir, "dist", "dungeon-run-0.4.0.ipa")
	if err := os.WriteFile(ipaPath, []byte("fake-ipa"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.Keychain = editor.NewMemoryKeychain()
	ed.ReleaseCheckRunner = func(context.Context, string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Millisecond, nil
	}
	ed.ReleaseReadinessInspector = func(string, string, *project.File) []editor.BuildReadinessItem {
		return nil
	}
	ed.ExportCommandRunner = func(context.Context, []string, io.Writer, io.Writer) error {
		return nil
	}
	ed.ReleaseMeasureRunner = func(_ context.Context, _ string, _ *releaseplan.Plan) (editor.BuildArtifactResult, error) {
		return editor.BuildArtifactResult{
			ArtifactPath: ipaPath,
			ArtifactName: "dungeon-run-0.4.0.ipa",
			SizeBytes:    mbBytes(30),
		}, nil
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	rawP8 := "-----BEGIN PRIVATE KEY-----\nRAW-P8-CONTENT\n-----END PRIVATE KEY-----"
	if err := ed.StorePublishCredential(editor.PublishDestTestFlight, "ABC1234567:99999999-8888-7777-6666-555555555555", rawP8); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}
	if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseBuild(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ReviewReleaseUploads(); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConfirmReleaseUploads(ed.BuildReleaseSnapshot().UploadReview.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := ed.WaitReleaseUpload(); err != nil {
		t.Fatalf("WaitReleaseUpload: %v", err)
	}
	invocations := readFakePublishInvocations(t, logPath)
	if len(invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(invocations))
	}
	env := invocations[0].Env
	if env["NSGO_APP_STORE_KEY"] != rawP8 || env["NSGO_APP_STORE_KEY_ID"] != "ABC1234567" || env["NSGO_APP_STORE_ISSUER"] != "99999999-8888-7777-6666-555555555555" {
		t.Fatalf("unexpected TestFlight env: %+v", env)
	}
}
