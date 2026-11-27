package editormcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	gamemcp "github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/statedump"
)

func TestPhoneSessionsListAndCallToolPermissionsAndPrivacy(t *testing.T) {
	f := openFixture(t)
	t.Cleanup(func() { _ = f.ed.Close() })
	f.ed.LANAddressResolver = func() (string, error) { return "127.0.0.1", nil }

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. Before serving, phone_sessions_list reports serving=false and 0 sessions,
	// and phone_call_tool returns an actionable error.
	listRes := call(t, cs, editormcp.ToolPhoneSessionsList, nil)
	mustOK(t, listRes)
	var listOut editormcp.PhoneSessionsListResult
	structured(t, listRes, &listOut)
	if listOut.Serving || listOut.SessionCount != 0 {
		t.Fatalf("pre-serve phone_sessions_list = %+v, want serving=false count=0", listOut)
	}
	notServingRes := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{"tool": gamemcp.ToolStateDump})
	if !notServingRes.IsError || !strings.Contains(text(notServingRes), "not running") {
		t.Fatalf("call while not serving = err:%v text:%q", notServingRes.IsError, text(notServingRes))
	}

	// 2. Start serving a fixture web build directory.
	webDir := filepath.Join(t.TempDir(), "web")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<!doctype html><html><body>ok</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := f.ed.StartWebBuildServer(webDir)
	if err != nil {
		t.Fatalf("StartWebBuildServer: %v", err)
	}

	// Register a simulated phone session over HTTP.
	client := &http.Client{Timeout: 5 * time.Second}
	handshakeBody, _ := json.Marshal(map[string]any{
		"token":        snap.BridgeToken,
		"clientId":     "pixel-9-chrome",
		"device":       "Pixel 9",
		"playMode":     true,
		"capabilities": []string{"inspect", "state_dump", "screenshot"},
	})
	resp, err := client.Post(snap.URL+"__nsgo_mcp/session", "application/json", bytes.NewReader(handshakeBody))
	if err != nil {
		t.Fatalf("POST /__nsgo_mcp/session: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d, want 200", resp.StatusCode)
	}

	// phone_sessions_list reports the connected phone.
	listRes = call(t, cs, editormcp.ToolPhoneSessionsList, nil)
	mustOK(t, listRes)
	structured(t, listRes, &listOut)
	if !listOut.Serving || listOut.SessionCount != 1 || len(listOut.Sessions) != 1 || listOut.Sessions[0].SessionID != "pixel-9-chrome" || listOut.Sessions[0].Device != "Pixel 9" {
		t.Fatalf("phone_sessions_list = %+v", listOut)
	}

	// Build a tiny valid 2x2 PNG to test game_screenshot image decoding too.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 0x33, G: 0xaa, B: 0xff, A: 0xff})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	pngB64 := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	// Start a poll loop simulating the phone answering game_state_dump and game_screenshot.
	pollCtx, cancelPoll := context.WithCancel(context.Background())
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		for pollCtx.Err() == nil {
			req, _ := http.NewRequestWithContext(pollCtx, http.MethodGet, snap.URL+"__nsgo_mcp/poll?id=pixel-9-chrome&token="+snap.BridgeToken, nil)
			pr, perr := client.Do(req)
			if perr != nil {
				return
			}
			if pr.StatusCode == http.StatusNoContent {
				_ = pr.Body.Close()
				continue
			}
			if pr.StatusCode != http.StatusOK {
				_ = pr.Body.Close()
				return
			}
			var incoming struct {
				RequestID string          `json:"requestId"`
				Tool      string          `json:"tool"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.NewDecoder(pr.Body).Decode(&incoming)
			_ = pr.Body.Close()

			var result any
			switch incoming.Tool {
			case gamemcp.ToolStateDump:
				result = map[string]any{
					"version": 1,
					"rate":    60,
					"steps":   42,
					"yaml":    "version: 1\nrate: 60\nsteps: 42\n",
				}
			case gamemcp.ToolScreenshot:
				result = map[string]any{
					"width":      2,
					"height":     2,
					"format":     "png",
					"bytes":      pngBuf.Len(),
					"png_base64": pngB64,
				}
			default:
				result = map[string]any{"echo": incoming.Tool}
			}
			replyBody, _ := json.Marshal(map[string]any{
				"token":     snap.BridgeToken,
				"sessionId": "pixel-9-chrome",
				"requestId": incoming.RequestID,
				"result":    result,
			})
			rr, rerr := client.Post(snap.URL+"__nsgo_mcp/reply", "application/json", bytes.NewReader(replyBody))
			if rerr == nil {
				_ = rr.Body.Close()
			}
		}
	}()
	t.Cleanup(func() {
		cancelPoll()
		_ = f.ed.StopWebBuildServer()
		<-pollDone
	})

	// 3. Under default Balanced profile, phone_call_tool (matching run · playtests) is Allowed
	// and auto-selects the sole connected phone session when session_id is omitted.
	dumpRes := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"tool": gamemcp.ToolStateDump,
	})
	mustOK(t, dumpRes)
	var callOut editormcp.PhoneCallToolResult
	structured(t, dumpRes, &callOut)
	if callOut.SessionID != "pixel-9-chrome" || callOut.Device != "Pixel 9" || callOut.Tool != gamemcp.ToolStateDump {
		t.Fatalf("phone_call_tool structured = %+v", callOut)
	}
	if !strings.Contains(string(callOut.Result), `"steps":42`) {
		t.Fatalf("phone_call_tool result = %s, want steps:42", callOut.Result)
	}

	// 4. game_screenshot decodes png_base64 into MCP ImageContent.
	shotRes := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"session_id": "pixel-9-chrome",
		"tool":       gamemcp.ToolScreenshot,
	})
	mustOK(t, shotRes)
	foundImage := false
	for _, c := range shotRes.Content {
		if ic, ok := c.(*mcp.ImageContent); ok && ic.MIMEType == "image/png" && bytes.Equal(ic.Data, pngBuf.Bytes()) {
			foundImage = true
		}
	}
	if !foundImage {
		t.Fatalf("expected PNG ImageContent from phone_call_tool(game_screenshot), got %+v", shotRes.Content)
	}

	// 5. Privacy gate: turning off ViewportImages blocks phone game_screenshot.
	if err := f.ed.SetAgentPrivacy(editor.PrivacyViewportImages, false); err != nil {
		t.Fatal(err)
	}
	blockedShot := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"tool": gamemcp.ToolScreenshot,
	})
	if !blockedShot.IsError || !strings.Contains(strings.ToLower(text(blockedShot)), "viewport") {
		t.Fatalf("expected ViewportImages privacy refusal, got err:%v text:%q", blockedShot.IsError, text(blockedShot))
	}
	if err := f.ed.SetAgentPrivacy(editor.PrivacyViewportImages, true); err != nil {
		t.Fatal(err)
	}

	// 6. Permission rules: under Cautious profile, phone_call_tool requires Ask permission (rule 2 of 7)
	// while phone_sessions_list (ActionRead) still succeeds.
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileCautious); err != nil {
		t.Fatal(err)
	}
	mustOK(t, call(t, cs, editormcp.ToolPhoneSessionsList, nil))
	cautiousCall := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"tool": gamemcp.ToolStateDump,
	})
	if !cautiousCall.IsError || !strings.Contains(text(cautiousCall), "permission required") || !strings.Contains(text(cautiousCall), "Cautious") {
		t.Fatalf("cautious phone_call_tool = err:%v text:%q", cautiousCall.IsError, text(cautiousCall))
	}

	// 7. Custom Deny rule specifically for "playtests: phone/game_console_exec" blocks only that tool
	// while still allowing game_state_dump under Balanced.
	if err := f.ed.SelectPermissionProfile(editor.PermissionProfileBalanced); err != nil {
		t.Fatal(err)
	}
	perms := f.ed.Permissions()
	perms.Rules = append([]editor.PermissionRule{{
		Action:   editor.PermissionActionRun,
		Patterns: []string{"playtests: phone/game_console_exec"},
		Decision: editor.PermissionDeny,
	}}, perms.Rules...)
	if err := f.ed.SetPermissions(perms); err != nil {
		t.Fatal(err)
	}
	deniedConsole := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"tool":      "game_console_exec",
		"arguments": map[string]any{"command": "help"},
	})
	if !deniedConsole.IsError || !strings.Contains(text(deniedConsole), "permission denied") {
		t.Fatalf("custom denied phone tool = err:%v text:%q", deniedConsole.IsError, text(deniedConsole))
	}
	mustOK(t, call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"tool": gamemcp.ToolStateDump,
	}))

	// 8. When two phone sessions are connected and session_id is omitted, phone_call_tool asks to disambiguate.
	secondBody, _ := json.Marshal(map[string]any{
		"token":    snap.BridgeToken,
		"clientId": "iphone-16-safari",
		"device":   "iPhone 16",
		"playMode": true,
	})
	resp2, err := client.Post(snap.URL+"__nsgo_mcp/session", "application/json", bytes.NewReader(secondBody))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	ambigRes := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"tool": gamemcp.ToolStateDump,
	})
	if !ambigRes.IsError || !strings.Contains(text(ambigRes), "session_id") {
		t.Fatalf("ambiguous phone_call_tool = err:%v text:%q", ambigRes.IsError, text(ambigRes))
	}
}

func findHeadlessBrowser() string {
	if b := strings.TrimSpace(os.Getenv("NSGO_BROWSER")); b != "" {
		if p, err := exec.LookPath(b); err == nil {
			return p
		}
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "linux":
		candidates = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"}
	case "windows":
		candidates = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// #1506 Acceptance:
// Builds a real NSGo Play-mode web build, serves it through the editor's LAN QR web build server,
// loads it in a real headless browser, verifies the browser auto-discovers /__nsgo_mcp/info and
// registers a session in WebBuildServerState, and retrieves a live game_state_dump (plus scene/loop
// inspection) from the running WASM game through the editor MCP tools (phone_sessions_list & phone_call_tool).
func TestPhoneWebBuildPlayModeBridgeHeadlessBrowserIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a real web WASM app and loads it in a headless browser")
	}
	browserPath := findHeadlessBrowser()
	if browserPath == "" {
		t.Skip("no Chromium-based headless browser installed (set NSGO_BROWSER to enable)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Build the real NSGo web fixture app (Play-mode web build) into a temp output directory.
	fixtureDir, err := filepath.Abs(filepath.Join("testdata", "phonebridge"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(fixtureDir, ".nsgo"))
	})
	buildOut := filepath.Join(t.TempDir(), "out")
	buildCmd := exec.CommandContext(ctx, "go", "tool", "nsgo", "build", "-target", "web", "-output", buildOut)
	buildCmd.Dir = fixtureDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go tool nsgo build -target web failed: %v\n%s", err, out)
	}
	webTarget := ""
	entries, rerr := os.ReadDir(buildOut)
	if rerr != nil {
		t.Fatalf("read build output dir %s: %v", buildOut, rerr)
	}
	for _, e := range entries {
		if e.IsDir() {
			cand := filepath.Join(buildOut, e.Name())
			if _, err := os.Stat(filepath.Join(cand, "index.html")); err == nil {
				webTarget = cand
				break
			}
		}
	}
	if webTarget == "" {
		if _, err := os.Stat(filepath.Join(buildOut, "index.html")); err == nil {
			webTarget = buildOut
		}
	}
	if webTarget == "" {
		t.Fatalf("expected built web directory with index.html in %s", buildOut)
	}

	// 2. Serve the built web output through the editor's LAN server and start editormcp.Server.
	f := openFixture(t)
	t.Cleanup(func() { _ = f.ed.Close() })
	f.ed.LANAddressResolver = func() (string, error) { return "127.0.0.1", nil }

	snap, err := f.ed.StartWebBuildServer(webTarget)
	if err != nil {
		t.Fatalf("StartWebBuildServer(%s): %v", webTarget, err)
	}
	t.Cleanup(func() { _ = f.ed.StopWebBuildServer() })

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 3. Launch headless Chromium pointing at the served LAN URL (exactly as a phone scanning the QR code does).
	profileDir := t.TempDir()
	chromeArgs := []string{
		"--headless=new",
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--mute-audio",
		"--remote-debugging-port=0",
	}
	if runtime.GOOS == "darwin" {
		chromeArgs = append(chromeArgs, "--disable-features=MacAppCodeSignClone")
	}
	if runtime.GOOS == "linux" {
		chromeArgs = append(chromeArgs, "--no-sandbox")
	}
	chromeArgs = append(chromeArgs, snap.URL)

	var browserLog bytes.Buffer
	browserCtx, stopBrowser := context.WithCancel(ctx)
	browserCmd := exec.CommandContext(browserCtx, browserPath, chromeArgs...)
	browserCmd.Stderr = &browserLog
	browserCmd.Stdout = &browserLog
	browserCmd.Cancel = func() error {
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			return browserCmd.Process.Signal(syscall.SIGTERM)
		}
		return browserCmd.Process.Kill()
	}
	browserCmd.WaitDelay = 5 * time.Second
	if err := browserCmd.Start(); err != nil {
		stopBrowser()
		t.Fatalf("start headless browser %s: %v", browserPath, err)
	}
	browserDone := make(chan error, 1)
	go func() {
		browserDone <- browserCmd.Wait()
	}()
	t.Cleanup(func() {
		stopBrowser()
		select {
		case <-browserDone:
		case <-time.After(6 * time.Second):
			if browserCmd.Process != nil {
				_ = browserCmd.Process.Kill()
			}
			<-browserDone
		}
	})

	// 4. Wait for the WASM game in the headless browser to auto-discover /__nsgo_mcp/info,
	// load /__nsgo_mcp/bridge.js, and register a session in WebBuildServerState.
	var connected editor.WebBridgeSession
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		state := f.ed.WebBuildServerState()
		if len(state.ConnectedClients) > 0 {
			connected = state.ConnectedClients[0]
			break
		}
		select {
		case err := <-browserDone:
			t.Fatalf("headless browser exited before connecting to MCP bridge: %v\nlog:\n%s", err, browserLog.String())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if connected.SessionID == "" {
		t.Fatalf("timed out waiting for headless browser session in WebBuildServerState (url=%s)\nbrowser log:\n%s", snap.URL, browserLog.String())
	}

	// 5. Verify phone_sessions_list reports the live browser session.
	listRes := call(t, cs, editormcp.ToolPhoneSessionsList, nil)
	mustOK(t, listRes)
	var listOut editormcp.PhoneSessionsListResult
	structured(t, listRes, &listOut)
	if !listOut.Serving || listOut.SessionCount != 1 || listOut.Sessions[0].SessionID != connected.SessionID {
		t.Fatalf("phone_sessions_list = %+v, want session %q", listOut, connected.SessionID)
	}

	// 6. Call game_state_dump through phone_call_tool and verify real ECS/scene/loop/value state from WASM!
	dumpCallRes := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"session_id": connected.SessionID,
		"tool":       gamemcp.ToolStateDump,
	})
	mustOK(t, dumpCallRes)
	var dumpWrapper editormcp.PhoneCallToolResult
	structured(t, dumpCallRes, &dumpWrapper)
	var dumpOut gamemcp.StateDumpResult
	if err := json.Unmarshal(dumpWrapper.Result, &dumpOut); err != nil {
		t.Fatalf("unmarshal game_state_dump result %s: %v", dumpWrapper.Result, err)
	}
	if dumpOut.Version != statedump.FormatVersion || dumpOut.Rate != 60 || dumpOut.Steps != 5 {
		t.Fatalf("unexpected state dump header: version=%d rate=%d steps=%d", dumpOut.Version, dumpOut.Rate, dumpOut.Steps)
	}
	if dumpOut.Scene == nil || dumpOut.Scene.Current != "dungeon_floor_1" || dumpOut.Scene.Depth != 1 {
		t.Fatalf("unexpected state dump scene: %+v", dumpOut.Scene)
	}
	if len(dumpOut.Entities) != 1 || len(dumpOut.Entities[0].Components) != 2 {
		t.Fatalf("unexpected state dump entities: %+v", dumpOut.Entities)
	}
	if string(dumpOut.Values["score"]) != "420" {
		t.Fatalf("unexpected state dump values[score] = %s, want 420", dumpOut.Values["score"])
	}
	parsedYAML, err := statedump.Parse([]byte(dumpOut.YAML))
	if err != nil {
		t.Fatalf("statedump.Parse returned YAML failed: %v\n%s", err, dumpOut.YAML)
	}
	if parsedYAML.Steps != 5 || len(parsedYAML.Entities) != 1 {
		t.Fatalf("parsed state dump YAML mismatch: %+v", parsedYAML)
	}

	// 7. Also verify loop stepping & live entity inspection round-trip against the same WASM game.
	stepRes := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"tool":      gamemcp.ToolLoopStep,
		"arguments": map[string]any{"count": 3},
	})
	mustOK(t, stepRes)
	var stepWrapper editormcp.PhoneCallToolResult
	structured(t, stepRes, &stepWrapper)
	var stepOut gamemcp.LoopStepResult
	if err := json.Unmarshal(stepWrapper.Result, &stepOut); err != nil {
		t.Fatalf("unmarshal game_loop_step result %s: %v", stepWrapper.Result, err)
	}
	if stepOut.StepsAdvanced != 3 || stepOut.TotalSteps != 8 {
		t.Fatalf("game_loop_step = %+v, want advanced=3 total=8", stepOut)
	}

	entitiesRes := call(t, cs, editormcp.ToolPhoneCallTool, map[string]any{
		"tool": gamemcp.ToolEntitiesList,
	})
	mustOK(t, entitiesRes)
	var entWrapper editormcp.PhoneCallToolResult
	structured(t, entitiesRes, &entWrapper)
	var entOut gamemcp.EntitiesListResult
	if err := json.Unmarshal(entWrapper.Result, &entOut); err != nil {
		t.Fatalf("unmarshal game_entities_list result %s: %v", entWrapper.Result, err)
	}
	if entOut.Total != 1 || len(entOut.Entities) != 1 || entOut.Entities[0].Name != "knight" {
		t.Fatalf("game_entities_list = %+v, want 1 entity named knight", entOut)
	}
}
