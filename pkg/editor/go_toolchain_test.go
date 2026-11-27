package editor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nsgo-dev/game/internal/gotoolchain"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeRecordingFakeGo(t *testing.T, goBin, logFile string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(goBin), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ]; then\n" +
		"  echo 'go version go1.27.4 darwin/arm64'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf 'ARGS=%s|PATH=%s\\n' \"$*\" \"$PATH\" >> \"" + logFile + "\"\n" +
		"exit 0\n"
	if err := os.WriteFile(goBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestGoToolchainExecSitesReceiveResolvedEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell fake go test")
	}

	root := t.TempDir()
	home := t.TempDir()
	minimalBin := filepath.Join(root, "usr", "bin")
	if err := os.MkdirAll(minimalBin, 0o755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(t.TempDir(), "invocations.log")
	goDir := filepath.Join(root, "usr", "local", "go", "bin")
	goBin := filepath.Join(goDir, "go")
	writeRecordingFakeGo(t, goBin, logFile)

	restore := gotoolchain.SetSearchEnvForTest(gotoolchain.SearchEnv{
		GOOS:           runtime.GOOS,
		PathEnv:        minimalBin,
		HomeDir:        home,
		RootDir:        root,
		LoginShellPATH: func() string { return "" },
	})
	defer restore()

	// Ensure process PATH itself does not contain any go binary (even on hosts with /usr/bin/go).
	t.Setenv("PATH", minimalBin)

	projDir := t.TempDir()
	gameYAML := "version: 1\nname: Test Game\nid: dev.example.testgame\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(projDir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	readLog := func() []string {
		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("read log file: %v", err)
		}
		var lines []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line != "" {
				lines = append(lines, line)
			}
		}
		return lines
	}
	assertLastInvocation := func(name, wantArgsSubstr string) {
		t.Helper()
		lines := readLog()
		if len(lines) == 0 {
			t.Fatalf("%s: expected fake go to be invoked", name)
		}
		last := lines[len(lines)-1]
		if !strings.Contains(last, wantArgsSubstr) {
			t.Fatalf("%s: last invocation = %q, want args containing %q", name, last, wantArgsSubstr)
		}
		if !strings.Contains(last, goDir) {
			t.Fatalf("%s: last invocation = %q, want resolved PATH containing %q", name, last, goDir)
		}
	}

	ctx := context.Background()

	// 1. Start Screen defaultCreateProject
	if err := defaultCreateProject(ctx, CreateProjectRequest{
		Name:     "demo",
		Dir:      filepath.Join(t.TempDir(), "demo"),
		Template: "starter",
	}, io.Discard, io.Discard); err != nil {
		t.Fatalf("defaultCreateProject: %v", err)
	}
	assertLastInvocation("defaultCreateProject", "tool game new")

	// 2. RunGameWith + startGameProcess
	ed, err := NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ed.Close() }()
	if err := ed.OpenProject(projDir); err != nil {
		t.Fatal(err)
	}
	var capturedSpec RunGameSpec
	ed.RunGameCommand = func(spec RunGameSpec, stdout, stderr io.Writer) (GameProcess, error) {
		capturedSpec = spec
		return startGameProcess(spec, stdout, stderr)
	}
	if err := ed.RunGame(); err != nil {
		t.Fatalf("RunGame: %v", err)
	}
	if err := ed.WaitGame(); err != nil {
		t.Fatalf("WaitGame: %v", err)
	}
	if capturedSpec.Name != "go" {
		t.Fatalf("RunGameSpec.Name = %q, want go", capturedSpec.Name)
	}
	hasResolvedPath := false
	for _, kv := range capturedSpec.Env {
		if after, ok := strings.CutPrefix(kv, "PATH="); ok && slices.Contains(filepath.SplitList(after), goDir) {
			hasResolvedPath = true
		}
	}
	if !hasResolvedPath {
		t.Fatalf("RunGameSpec.Env missing resolved goDir %q", goDir)
	}
	assertLastInvocation("startGameProcess", "tool game dev")

	// 3. Export panel defaultExportCommandRunner
	if err := defaultExportCommandRunner(ctx, []string{"tool", "game", "release", "-dry-run"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("defaultExportCommandRunner: %v", err)
	}
	assertLastInvocation("defaultExportCommandRunner", "tool game release -dry-run")

	// 4. Device command runner defaultDeviceCommandRunner
	if err := defaultDeviceCommandRunner(ctx, DeviceCommandRequest{Args: []string{"tool", "nsgo", "build", "-target", "android_arm64"}}, io.Discard, io.Discard); err != nil {
		t.Fatalf("defaultDeviceCommandRunner: %v", err)
	}
	assertLastInvocation("defaultDeviceCommandRunner", "tool nsgo build -target android_arm64")

	// 5. Changes panel checks (nsgo check + nsgo test)
	beforeLines := len(readLog())
	checkStatus := runDefaultChangesChecksCtx(ctx, projDir, time.Now())
	if !checkStatus.CheckPassed || !checkStatus.TestPassed {
		t.Fatalf("runDefaultChangesChecksCtx = %+v, want both passed", checkStatus)
	}
	afterLines := readLog()
	if len(afterLines) < beforeLines+2 {
		t.Fatalf("runDefaultChangesChecksCtx logged %d commands, want at least 2", len(afterLines)-beforeLines)
	}
	if !strings.Contains(afterLines[beforeLines], "tool nsgo check ./...") || !strings.Contains(afterLines[beforeLines], goDir) {
		t.Fatalf("changes check invocation = %q", afterLines[beforeLines])
	}
	if !strings.Contains(afterLines[beforeLines+1], "tool nsgo test ./...") || !strings.Contains(afterLines[beforeLines+1], goDir) {
		t.Fatalf("changes test invocation = %q", afterLines[beforeLines+1])
	}

	// 6. Code review checks runner (nsgo check + nsgo test)
	beforeLines = len(readLog())
	crRes, crErr := runDefaultCodeReviewChecks(ctx, CodeReviewCheckRequest{
		ProjectDir: projDir,
		Packages:   []string{"."},
	}, nil)
	if crErr != nil || !crRes.CheckPassed || !crRes.TestPassed {
		t.Fatalf("runDefaultCodeReviewChecks = (%+v, %v), want both passed", crRes, crErr)
	}
	afterLines = readLog()
	if len(afterLines) < beforeLines+2 {
		t.Fatalf("runDefaultCodeReviewChecks logged %d commands, want at least 2", len(afterLines)-beforeLines)
	}
	if !strings.Contains(afterLines[beforeLines], "tool nsgo check .") || !strings.Contains(afterLines[beforeLines], goDir) {
		t.Fatalf("code review check invocation = %q", afterLines[beforeLines])
	}
	if !strings.Contains(afterLines[beforeLines+1], "tool nsgo test -json . -count=1") || !strings.Contains(afterLines[beforeLines+1], goDir) {
		t.Fatalf("code review test invocation = %q", afterLines[beforeLines+1])
	}

	// 7. Asset build runner defaultAssetsCommandRunner
	if err := defaultAssetsCommandRunner(ctx, AssetsBuildSpec{
		Dir:  projDir,
		Name: "go",
		Args: []string{"tool", "game", "assets"},
	}, io.Discard, io.Discard); err != nil {
		t.Fatalf("defaultAssetsCommandRunner: %v", err)
	}
	assertLastInvocation("defaultAssetsCommandRunner", "tool game assets")

	// 8. Remote command transport
	tr, err := newRemoteCommandTransport(projDir)
	if err != nil {
		t.Fatalf("newRemoteCommandTransport: %v", err)
	}
	cmdTr, ok := tr.(*sdkmcp.CommandTransport)
	if !ok || cmdTr.Command == nil {
		t.Fatalf("newRemoteCommandTransport returned %T, want *sdkmcp.CommandTransport", tr)
	}
	if cmdTr.Command.Path != goBin {
		t.Fatalf("remote transport cmd.Path = %q, want %q", cmdTr.Command.Path, goBin)
	}
	remoteHasDir := false
	for _, kv := range cmdTr.Command.Env {
		if after, ok := strings.CutPrefix(kv, "PATH="); ok && slices.Contains(filepath.SplitList(after), goDir) {
			remoteHasDir = true
		}
	}
	if !remoteHasDir {
		t.Fatalf("remote transport cmd.Env missing resolved goDir %q", goDir)
	}

	// 9. Profiler ConnectProfilerCommand (fake go exits 0 immediately so MCP handshake fails,
	// but the invocation log proves the resolved binary and enriched PATH were used).
	beforeLines = len(readLog())
	profCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, _ = ConnectProfilerCommand(profCtx, projDir, "tool", "nsgo", "mcp")
	if len(readLog()) <= beforeLines {
		t.Fatal("ConnectProfilerCommand did not invoke resolved fake go")
	}
	assertLastInvocation("ConnectProfilerCommand", "tool nsgo mcp")
}

func TestStartScreenMissingGoNoticeAndDisabledActions(t *testing.T) {
	parentDir := t.TempDir()
	projDir := t.TempDir()
	gameYAML := "version: 1\nname: Play Check\nid: dev.example.playcheck\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(projDir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ed.Close() }()

	if err := ed.SetLanguage(LanguageEnglish); err != nil {
		t.Fatal(err)
	}
	ed.SetMinGoVersion("1.27.3")
	ed.SetProjectTemplates([]ProjectTemplate{
		{Name: "starter", Title: "Starter", Summary: "An empty scene and a loop", Dimension: "2d", Default: true},
		{Name: "platformer2d", Title: "2D platformer", Summary: "Rooms, checkpoints, coyote time", Dimension: "2d"},
		{Name: "racing3d", Title: "3D racer", Summary: "Laps, ghosts, AI drivers", Dimension: "3d"},
	})
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("my-game")
	ed.SetStartScreenOpen(true)

	probeCalls := 0
	currentStatus := GoToolchainStatus{Found: false, MinVersion: "1.27.3"}
	ed.GoToolchainProbe = func(minVersion string) GoToolchainStatus {
		probeCalls++
		st := currentStatus
		st.MinVersion = minVersion
		return st
	}

	var openedURL string
	ed.OpenBrowserURL = func(rawURL string) error {
		openedURL = rawURL
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:    core.OSMacOS,
		Idiom: core.IdiomDesktop,
		Dark:  true,
	}))
	app.Settle()

	// 1. Missing Go shows the Start Screen notice and disables Create + Start with AI.
	if !app.Exists("id:start-go-notice") {
		t.Fatal("missing-Go notice did not appear on Start Screen")
	}
	titleNode := app.Find("id:start-go-notice-title")
	if titleNode == nil || !strings.Contains(collectNodeText(titleNode), "NSGo Game Editor needs Go 1.27.3 or later") {
		t.Fatalf("notice title = %q, want 'NSGo Game Editor needs Go 1.27.3 or later'", collectNodeText(titleNode))
	}
	if app.Enabled("id:start-create") {
		t.Fatal("Create button should be disabled when Go is missing")
	}
	if app.Enabled("id:start-ai-btn") {
		t.Fatal("Start with AI button should be disabled when Go is missing")
	}

	// Clicking the download button opens https://go.dev/dl/.
	app.Press("id:start-go-download")
	app.Settle()
	if openedURL != GoDownloadURL {
		dlView := app.Find("id:start-go-download")
		t.Fatalf("openedURL = %q, want %q (download view=%+v)", openedURL, GoDownloadURL, dlView)
	}

	// Programmatic create and AI start guards return ErrGoToolchainUnavailable.
	var reqErr, aiErr error
	app.Post(func() {
		reqErr = ed.RequestCreateProject(CreateProjectRequest{Name: "my-game", ParentDir: parentDir})
		aiErr = ed.StartWithAI()
	})
	app.Settle()
	if !errors.Is(reqErr, ErrGoToolchainUnavailable) {
		t.Fatalf("RequestCreateProject err = %v, want ErrGoToolchainUnavailable", reqErr)
	}
	if !errors.Is(aiErr, ErrGoToolchainUnavailable) {
		t.Fatalf("StartWithAI err = %v, want ErrGoToolchainUnavailable", aiErr)
	}

	// Switching into the editor workspace disables Play and RunGame while Go is missing.
	app.Post(func() {
		if err := ed.OpenProject(projDir); err != nil {
			t.Errorf("OpenProject: %v", err)
		}
	})
	app.Settle()
	if app.Enabled("id:toolbar-play") {
		t.Fatal("Play button should be disabled when Go is missing")
	}
	if err := ed.RunGame(); !errors.Is(err, ErrGoToolchainUnavailable) {
		t.Fatalf("RunGame err = %v, want ErrGoToolchainUnavailable", err)
	}
	if cmdEnabled := editorCommandEnabled(ed, CmdRunGame); cmdEnabled {
		t.Fatalf("CmdRunGame enabled = %v, want false when Go is missing", cmdEnabled)
	}

	// 2. Outdated Go (1.26.2 < 1.27.3) keeps the notice visible and actions disabled on Retry.
	app.Post(func() { ed.SetStartScreenOpen(true) })
	app.Settle()
	currentStatus = GoToolchainStatus{
		Found:   true,
		Binary:  "/usr/local/go/bin/go",
		Version: "1.26.2",
	}
	beforeProbe := probeCalls
	app.Press("id:start-go-retry")
	app.Settle()
	if probeCalls <= beforeProbe {
		t.Fatalf("pressing Retry did not re-probe Go toolchain (calls=%d)", probeCalls)
	}
	if !app.Exists("id:start-go-notice") {
		t.Fatal("notice should remain visible when installed Go is older than required")
	}
	detailText := collectNodeText(app.Find("id:start-go-notice-detail"))
	if !strings.Contains(detailText, "1.26.2") {
		t.Fatalf("too-old notice detail = %q, want found version 1.26.2", detailText)
	}
	if app.Enabled("id:start-create") {
		t.Fatal("Create button should stay disabled when installed Go is too old")
	}

	// 3. Valid Go (1.27.4 >= 1.27.3) dismisses notice on Retry and re-enables Create and Play.
	currentStatus = GoToolchainStatus{
		Found:   true,
		Binary:  "/usr/local/go/bin/go",
		Version: "1.27.4",
	}
	app.Press("id:start-go-retry")
	app.Settle()
	if app.Exists("id:start-go-notice") {
		t.Fatal("notice should disappear after Retry finds a compatible Go")
	}
	if !app.Enabled("id:start-create") {
		t.Fatal("Create button should be enabled after Retry finds Go 1.27.4")
	}
	if !app.Enabled("id:start-ai-btn") {
		t.Fatal("Start with AI button should be enabled after Retry finds Go 1.27.4")
	}

	// Return to editor and verify Play and CmdRunGame are re-enabled.
	app.Post(func() { ed.SetStartScreenOpen(false) })
	app.Settle()
	if !app.Enabled("id:toolbar-play") {
		t.Fatal("Play button should be enabled after Retry finds Go 1.27.4")
	}
	if !editorCommandEnabled(ed, CmdRunGame) {
		t.Fatal("CmdRunGame enabled = false, want true after Retry finds Go 1.27.4")
	}
}

func editorCommandEnabled(ed *Editor, id string) bool {
	for _, cmd := range ed.Commands() {
		if cmd.ID == id {
			return cmd.Enabled == nil || cmd.Enabled()
		}
	}
	return false
}

func collectNodeText(n *nsgotest.View) string {
	if n == nil {
		return ""
	}
	var buf bytes.Buffer
	var walk func(cur *nsgotest.View)
	walk = func(cur *nsgotest.View) {
		if cur == nil {
			return
		}
		if cur.Text != "" {
			if buf.Len() > 0 {
				buf.WriteByte(' ')
			}
			buf.WriteString(cur.Text)
		}
		for _, ch := range cur.Children {
			walk(ch)
		}
	}
	walk(n)
	return buf.String()
}
