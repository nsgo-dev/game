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

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/releaseplan"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
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
	gameBin := buildGameTool(t, "../cmd/game", "game")
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
