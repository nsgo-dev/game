package editor_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func writeWebBuildFixture(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "web-build")
	files := map[string][]byte{
		"index.html":       []byte("<!doctype html><html><head><title>Dungeon Run</title></head><body><script src=\"app.js\"></script></body></html>"),
		"app.js":           []byte("console.log('dungeon-run web build');\n"),
		"game.wasm":        {0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00},
		"assets/hero.png":  {0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x01, 0x02},
		"audio/sfx.opus":   []byte("OggS-fake-opus-payload"),
		"styles/main.css":  []byte("body { background: #101216; }\n"),
		"data/config.json": []byte(`{"version":1,"scene":"floor1"}`),
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, files
}

// #1049 Acceptance 1 & 2:
// Serves a web build fixture on a local/LAN address, verifies exact contents & MIME types of index.html
// and build assets, rejects non-LAN addresses and symlink escapes, and immediately frees the TCP port on stop.
func TestBuildReleaseWebLANServerFetchesAssetsAndFreesPort(t *testing.T) {
	webDir, wantFiles := writeWebBuildFixture(t)

	// Add an outside secret and a symlink inside webDir pointing outside -> must be refused (403).
	outsideSecret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsideSecret, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	hasSymlink := os.Symlink(outsideSecret, filepath.Join(webDir, "escape.txt")) == nil

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	// Reject public (non-LAN) bind address.
	ed.LANAddressResolver = func() (string, error) { return "8.8.8.8", nil }
	if _, err := ed.StartWebBuildServer(webDir); !errors.Is(err, editor.ErrNonLANAddress) {
		t.Fatalf("public IP bind err = %v, want ErrNonLANAddress", err)
	}

	// Bind to loopback/local address on an ephemeral free port.
	ed.LANAddressResolver = func() (string, error) { return "127.0.0.1", nil }
	snap, err := ed.StartWebBuildServer(webDir)
	if err != nil {
		t.Fatalf("StartWebBuildServer: %v", err)
	}
	if !snap.Serving || snap.URL == "" || snap.Port <= 0 || snap.ListenAddr == "" {
		t.Fatalf("unexpected WebBuildServerSnapshot: %+v", snap)
	}

	// Verify valid deterministic QR code encodes the served LAN URL.
	qr1, err := editor.EncodeBuildQR(snap.URL)
	if err != nil {
		t.Fatalf("EncodeBuildQR(%q): %v", snap.URL, err)
	}
	qr2, err := editor.EncodeBuildQR(snap.URL)
	if err != nil {
		t.Fatalf("EncodeBuildQR second call: %v", err)
	}
	if qr1.Size < 21 || qr1.QuietZone() != 4 || qr1.Version != qr2.Version || qr1.Mask != qr2.Mask {
		t.Fatalf("unexpected QR metadata: %+v vs %+v", qr1, qr2)
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// Fetch root `/` -> serves index.html with exact bytes, MIME type, no-store, and MCP bridge headers.
	resp, err := client.Get(snap.URL)
	if err != nil {
		t.Fatalf("GET %s: %v", snap.URL, err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	if !bytes.Equal(body, wantFiles["index.html"]) {
		t.Fatalf("index.html body mismatch:\ngot:  %q\nwant: %q", body, wantFiles["index.html"])
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("index.html Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	if hdr := resp.Header.Get("X-NSGo-MCP-Bridge"); hdr != "/__nsgo_mcp/info" {
		t.Fatalf("X-NSGo-MCP-Bridge = %q, want /__nsgo_mcp/info", hdr)
	}
	if hdr := resp.Header.Get("X-NSGo-Play-Mode"); hdr != "debug" {
		t.Fatalf("X-NSGo-Play-Mode = %q, want debug", hdr)
	}

	// Fetch every build asset and verify exact contents and MIME types.
	wantMIME := map[string]string{
		"index.html":       "text/html; charset=utf-8",
		"app.js":           "text/javascript; charset=utf-8",
		"game.wasm":        "application/wasm",
		"assets/hero.png":  "image/png",
		"audio/sfx.opus":   "audio/opus",
		"styles/main.css":  "text/css; charset=utf-8",
		"data/config.json": "application/json",
	}
	for rel, wantBytes := range wantFiles {
		r, gerr := client.Get(snap.URL + rel)
		if gerr != nil {
			t.Fatalf("GET %s: %v", rel, gerr)
		}
		gotBytes, rerr := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if rerr != nil {
			t.Fatal(rerr)
		}
		if r.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", rel, r.StatusCode)
		}
		if !bytes.Equal(gotBytes, wantBytes) {
			t.Fatalf("GET %s body mismatch: got %x, want %x", rel, gotBytes, wantBytes)
		}
		if gotCT := r.Header.Get("Content-Type"); gotCT != wantMIME[rel] {
			t.Fatalf("GET %s Content-Type = %q, want %q", rel, gotCT, wantMIME[rel])
		}
	}

	// Symlink escaping serve directory must be forbidden.
	if hasSymlink {
		r, gerr := client.Get(snap.URL + "escape.txt")
		if gerr != nil {
			t.Fatalf("GET escape.txt: %v", gerr)
		}
		_ = r.Body.Close()
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("GET symlink escape status = %d, want 403", r.StatusCode)
		}
	}

	// Stopping the server immediately closes the listener and frees the TCP port.
	listenAddr := snap.ListenAddr
	if err := ed.StopWebBuildServer(); err != nil {
		t.Fatalf("StopWebBuildServer: %v", err)
	}
	if ed.WebBuildServerState().Serving {
		t.Fatal("WebBuildServerState().Serving = true after stop")
	}

	// Immediately re-bind the exact same address and port — must succeed without delay.
	ln, err := net.Listen("tcp4", listenAddr)
	if err != nil {
		t.Fatalf("port %s was not immediately freed after StopWebBuildServer: %v", listenAddr, err)
	}
	_ = ln.Close()

	// Also verify serving directly from a web build .zip archive.
	zipPath := filepath.Join(t.TempDir(), "dungeon-run-0.4.0-web.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for rel, content := range wantFiles {
		w, werr := zw.Create(rel)
		if werr != nil {
			t.Fatal(werr)
		}
		if _, werr := w.Write(content); werr != nil {
			t.Fatal(werr)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}

	zipSnap, err := ed.StartWebBuildServer(zipPath)
	if err != nil {
		t.Fatalf("StartWebBuildServer(zip): %v", err)
	}
	zr, err := client.Get(zipSnap.URL + "game.wasm")
	if err != nil {
		t.Fatalf("GET zip game.wasm: %v", err)
	}
	zBody, _ := io.ReadAll(zr.Body)
	_ = zr.Body.Close()
	if zr.StatusCode != http.StatusOK || !bytes.Equal(zBody, wantFiles["game.wasm"]) {
		t.Fatalf("zip game.wasm = (%d, %x), want (200, %x)", zr.StatusCode, zBody, wantFiles["game.wasm"])
	}

	// Editor.Close() also stops the server and immediately frees its port.
	zipAddr := zipSnap.ListenAddr
	if err := ed.Close(); err != nil {
		t.Fatalf("Editor.Close(): %v", err)
	}
	ln2, err := net.Listen("tcp4", zipAddr)
	if err != nil {
		t.Fatalf("port %s not freed after Editor.Close(): %v", zipAddr, err)
	}
	_ = ln2.Close()
}

// Verifies the MCP bridge discovery endpoint, bootstrap JS, Play-mode phone handshake,
// JSON-RPC endpoint, bidirectional CallWebBridgeTool round-trip, and disconnect lifecycle.
func TestBuildReleaseWebMCPBridgeHandshakeAndToolRoundTrip(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  presets:
    - platform: web
`)
	webDir, _ := writeWebBuildFixture(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.LANAddressResolver = func() (string, error) { return "127.0.0.1", nil }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	snap, err := ed.StartWebBuildServer(webDir)
	if err != nil {
		t.Fatalf("StartWebBuildServer: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Discover bridge info via /__nsgo_mcp/info.
	infoResp, err := client.Get(snap.BridgeInfoURL)
	if err != nil {
		t.Fatalf("GET %s: %v", snap.BridgeInfoURL, err)
	}
	var info editor.WebBridgeInfo
	if err := json.NewDecoder(infoResp.Body).Decode(&info); err != nil {
		t.Fatalf("decode bridge info: %v", err)
	}
	_ = infoResp.Body.Close()
	if info.Protocol != "nsgo-web-mcp-v1" || !info.PlayMode || !info.EditorBridgeReady || info.Token == "" {
		t.Fatalf("unexpected WebBridgeInfo: %+v", info)
	}

	// 2. Fetch browser bootstrap script /__nsgo_mcp/bridge.js.
	jsResp, err := client.Get(snap.URL + "__nsgo_mcp/bridge.js")
	if err != nil {
		t.Fatalf("GET bridge.js: %v", err)
	}
	jsBody, _ := io.ReadAll(jsResp.Body)
	_ = jsResp.Body.Close()
	if jsResp.StatusCode != http.StatusOK || !strings.Contains(jsResp.Header.Get("Content-Type"), "text/javascript") || !strings.Contains(string(jsBody), "__nsgoMCP") {
		t.Fatalf("unexpected bridge.js response: status=%d ct=%q len=%d", jsResp.StatusCode, jsResp.Header.Get("Content-Type"), len(jsBody))
	}

	// 3. Unauthorized session handshake is rejected with 401.
	badRes, err := client.Post(snap.URL+"__nsgo_mcp/session", "application/json", strings.NewReader(`{"token":"wrong","device":"Evil"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = badRes.Body.Close()
	if badRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token handshake status = %d, want 401", badRes.StatusCode)
	}

	// 4. Valid phone Play-mode handshake registers session and updates RUN ON DEVICE phone target.
	handshakeReq, _ := json.Marshal(map[string]any{
		"token":        info.Token,
		"clientId":     "iphone-15-safari",
		"device":       "iPhone 15",
		"playMode":     true,
		"capabilities": []string{"inspect", "screenshot", "console"},
	})
	sessRes, err := client.Post(snap.URL+"__nsgo_mcp/session", "application/json", bytes.NewReader(handshakeReq))
	if err != nil {
		t.Fatalf("POST /__nsgo_mcp/session: %v", err)
	}
	var sessAck struct {
		OK        bool   `json:"ok"`
		SessionID string `json:"sessionId"`
		Bridge    string `json:"bridge"`
		PlayMode  bool   `json:"playMode"`
	}
	if err := json.NewDecoder(sessRes.Body).Decode(&sessAck); err != nil {
		t.Fatalf("decode session ack: %v", err)
	}
	_ = sessRes.Body.Close()
	if !sessAck.OK || sessAck.SessionID != "iphone-15-safari" || sessAck.Bridge != "nsgo-game-editor" || !sessAck.PlayMode {
		t.Fatalf("unexpected session ack: %+v", sessAck)
	}

	sessions := ed.WebBridgeSessions()
	if len(sessions) != 1 || sessions[0].Device != "iPhone 15" || !sessions[0].PlayMode {
		t.Fatalf("WebBridgeSessions() = %+v", sessions)
	}
	brSnap := ed.BuildReleaseSnapshot()
	var phoneDev *editor.BuildDeviceEntry
	for i := range brSnap.Devices {
		if brSnap.Devices[i].ID == "phone-wifi" {
			phoneDev = &brSnap.Devices[i]
		}
	}
	if phoneDev == nil || phoneDev.State != editor.BuildDeviceConnected || !strings.Contains(phoneDev.Name, "iPhone 15") {
		t.Fatalf("phone device after MCP connect = %+v", phoneDev)
	}

	// 5. JSON-RPC endpoint ping & tools/list.
	rpcReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
		"token":   info.Token,
	})
	rpcRes, err := client.Post(snap.URL+"__nsgo_mcp/rpc", "application/json", bytes.NewReader(rpcReq))
	if err != nil {
		t.Fatalf("POST /__nsgo_mcp/rpc: %v", err)
	}
	var rpcAck struct {
		Result map[string]any `json:"result"`
	}
	if err := json.NewDecoder(rpcRes.Body).Decode(&rpcAck); err != nil {
		t.Fatal(err)
	}
	_ = rpcRes.Body.Close()
	if rpcAck.Result["pong"] != true {
		t.Fatalf("rpc ping result = %+v", rpcAck.Result)
	}

	// 6. Bidirectional MCP tool round-trip: editor/agent -> phone poll -> phone reply -> editor result.
	phoneErrCh := make(chan error, 1)
	go func() {
		pollURL := snap.URL + "__nsgo_mcp/poll?id=" + sessAck.SessionID + "&token=" + info.Token
		pr, perr := client.Get(pollURL)
		if perr != nil {
			phoneErrCh <- perr
			return
		}
		var call struct {
			RequestID string          `json:"requestId"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if derr := json.NewDecoder(pr.Body).Decode(&call); derr != nil {
			_ = pr.Body.Close()
			phoneErrCh <- derr
			return
		}
		_ = pr.Body.Close()
		if call.Tool != "game_entities_list" {
			phoneErrCh <- errors.New("unexpected tool: " + call.Tool)
			return
		}
		replyPayload, _ := json.Marshal(map[string]any{
			"token":     info.Token,
			"sessionId": sessAck.SessionID,
			"requestId": call.RequestID,
			"result":    map[string]any{"entities": []string{"knight", "slime_01", "door"}},
		})
		rr, rerr := client.Post(snap.URL+"__nsgo_mcp/reply", "application/json", bytes.NewReader(replyPayload))
		if rerr != nil {
			phoneErrCh <- rerr
			return
		}
		_ = rr.Body.Close()
		phoneErrCh <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rawResult, err := ed.CallWebBridgeTool(ctx, sessAck.SessionID, "game_entities_list", map[string]any{"layer": "world"})
	if err != nil {
		t.Fatalf("CallWebBridgeTool: %v", err)
	}
	if err := <-phoneErrCh; err != nil {
		t.Fatalf("simulated phone poll/reply error: %v", err)
	}
	if !strings.Contains(string(rawResult), `"slime_01"`) {
		t.Fatalf("CallWebBridgeTool result = %s, want slime_01", rawResult)
	}

	// 7. Disconnect phone session -> phone device returns to Serving state.
	delReq, _ := http.NewRequest(http.MethodDelete, snap.URL+"__nsgo_mcp/session?id="+sessAck.SessionID+"&token="+info.Token, nil)
	delRes, err := client.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = delRes.Body.Close()
	if delRes.StatusCode != http.StatusOK {
		t.Fatalf("DELETE session status = %d, want 200", delRes.StatusCode)
	}
	if len(ed.WebBridgeSessions()) != 0 {
		t.Fatalf("expected 0 sessions after disconnect, got %+v", ed.WebBridgeSessions())
	}
}

// Verifies auto-serving after a web build finishes, QR card UI buttons (Copy URL, Stop/Start serving),
// and RUN ON DEVICE state transitions for This Mac's browser, Phone via QR, and Android USB device.
func TestBuildReleaseRunOnDeviceMacBrowserPhoneQRAndAndroidUSB(t *testing.T) {
	dir := writeBuildReleaseProject(t, `export:
  output: dist
  presets:
    - platform: web
    - platform: android
      targets: [android_arm64]
`)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })

	ed.LANAddressResolver = func() (string, error) { return "127.0.0.1", nil }
	ed.ReleaseCheckRunner = func(_ context.Context, _ string) ([]assetcheck.Problem, time.Duration, error) {
		return nil, time.Second, nil
	}
	// Non-dry-run web compile writes real index.html + game.wasm into dist/web so auto-serve picks it up!
	ed.ExportCommandRunner = func(_ context.Context, args []string, stdout, _ io.Writer) error {
		if slices.Contains(args, "web") {
			webOut := filepath.Join(dir, "dist", "web")
			if err := os.MkdirAll(webOut, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(webOut, "index.html"), []byte("<h1>Web Build OK</h1>"), 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(webOut, "game.wasm"), []byte{0, 'a', 's', 'm', 1, 0, 0, 0}, 0o644); err != nil {
				return err
			}
		}
		_, _ = io.WriteString(stdout, "built ok\n")
		return nil
	}

	var openedBrowserURLs []string
	ed.OpenBrowserURL = func(rawURL string) error {
		openedBrowserURLs = append(openedBrowserURLs, rawURL)
		return nil
	}
	var copiedClipboard string
	ed.WriteClipboard = func(text string) {
		copiedClipboard = text
	}

	connectedAndroid := true
	ed.DeviceDiscoverer = func(_ context.Context, _ string) ([]editor.ConnectedDevice, error) {
		if !connectedAndroid {
			return nil, nil
		}
		return []editor.ConnectedDevice{{
			ID:        "pixel7-usb-01",
			Name:      "Pixel 7",
			Platform:  "android",
			Transport: "usb",
			Online:    true,
		}}, nil
	}

	androidInstallStarted := make(chan struct{}, 1)
	androidInstallRelease := make(chan struct{})
	var deviceCmdReq editor.DeviceCommandRequest
	ed.DeviceCommandRunner = func(ctx context.Context, req editor.DeviceCommandRequest, stdout, _ io.Writer) error {
		deviceCmdReq = req
		androidInstallStarted <- struct{}{}
		select {
		case <-androidInstallRelease:
			_, _ = io.WriteString(stdout, "installed and launched on Pixel 7\n")
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenBuildReleaseScreen(); err != nil {
		t.Fatal(err)
	}

	// Before Android builds, android-usb is unavailable ("after Android builds").
	findDev := func(id string) editor.BuildDeviceEntry {
		for _, d := range ed.BuildReleaseSnapshot().Devices {
			if d.ID == id {
				return d
			}
		}
		t.Fatalf("device %q not found in snapshot", id)
		return editor.BuildDeviceEntry{}
	}
	if d := findDev("android-usb"); d.Available || d.State != editor.BuildDeviceUnavailable || !strings.Contains(d.Name, "Pixel 7") {
		t.Fatalf("pre-build android-usb = %+v, want Pixel 7 unavailable", d)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Run real build (web + android). Web auto-starts LAN server!
	app.Press("id:build-release-primary-action")
	app.WaitUntil(5*time.Second, func() bool { return !ed.ReleaseBuildRunning() })
	app.Settle()

	app.Press("id:build-preset-pick-web")
	app.Settle()

	ws := ed.WebBuildServerState()
	if !ws.Serving || ws.URL == "" {
		t.Fatalf("expected web build to auto-serve on LAN, got %+v", ws)
	}
	if !app.Exists("id:build-result-qr") || !app.Exists("id:build-qr-canvas") || !app.Exists("id:build-qr-url") {
		t.Fatal("QR card, canvas, or URL node missing")
	}

	// Copy URL button copies the active LAN URL.
	app.Press("id:build-qr-copy-url")
	app.Settle()
	if copiedClipboard != ws.URL {
		t.Fatalf("copied clipboard = %q, want %q", copiedClipboard, ws.URL)
	}

	// 1. This Mac's browser -> opens served LAN URL via OpenBrowserURL and transitions to Running.
	app.Press("id:build-device-browser")
	app.Settle()
	if !slices.Equal(openedBrowserURLs, []string{ws.URL}) {
		t.Fatalf("openedBrowserURLs = %v, want [%s]", openedBrowserURLs, ws.URL)
	}
	if d := findDev("browser"); d.State != editor.BuildDeviceRunning {
		t.Fatalf("browser device after launch = %+v, want running", d)
	}

	// 2. Android USB device -> Ready after Android finishes -> Installing -> Running -> Disconnected.
	if d := findDev("android-usb"); !d.Available || d.State != editor.BuildDeviceReady {
		t.Fatalf("post-build android-usb = %+v, want ready", d)
	}
	app.Press("id:build-device-android-usb")
	select {
	case <-androidInstallStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Android USB install command")
	}
	app.Settle()
	if d := findDev("android-usb"); d.State != editor.BuildDeviceInstalling {
		t.Fatalf("android-usb during install = %+v, want installing", d)
	}
	close(androidInstallRelease)
	if err := ed.WaitAndroidDeviceRun(); err != nil {
		t.Fatalf("WaitAndroidDeviceRun: %v", err)
	}
	app.Settle()
	if d := findDev("android-usb"); d.State != editor.BuildDeviceRunning {
		t.Fatalf("android-usb after install = %+v, want running", d)
	}
	wantArgs := []string{"-C", dir, "tool", "nsgo", "build", "-target", "android_arm64", "-run", "-device", "pixel7-usb-01", "-output", "dist"}
	if !slices.Equal(deviceCmdReq.Args, wantArgs) {
		t.Fatalf("Android DeviceCommandRequest.Args = %v, want %v", deviceCmdReq.Args, wantArgs)
	}

	// Unplug USB device and refresh -> transitions cleanly to Disconnected.
	connectedAndroid = false
	if _, err := ed.RefreshConnectedDevices(); err != nil {
		t.Fatalf("RefreshConnectedDevices: %v", err)
	}
	app.Settle()
	if d := findDev("android-usb"); d.Available || d.State != editor.BuildDeviceDisconnected {
		t.Fatalf("android-usb after unplug = %+v, want disconnected", d)
	}

	// 3. Stop serving button stops server, frees the port, shows Stopped state, and Start serving restarts it.
	listenAddr := ws.ListenAddr
	app.Press("id:build-qr-toggle-serve")
	app.Settle()
	if ed.WebBuildServerState().Serving {
		t.Fatal("web server still serving after pressing Stop serving")
	}
	if !app.Exists("id:build-qr-stopped-placeholder") {
		t.Fatal("stopped placeholder not shown after pressing Stop serving")
	}
	if d := findDev("phone-wifi"); d.State != editor.BuildDeviceStopped {
		t.Fatalf("phone-wifi after stop = %+v, want stopped", d)
	}
	ln, err := net.Listen("tcp4", listenAddr)
	if err != nil {
		t.Fatalf("port %s not freed after UI Stop serving: %v", listenAddr, err)
	}
	_ = ln.Close()

	// Press Start serving -> restarts serving and restores QR canvas!
	app.Press("id:build-qr-toggle-serve")
	app.Settle()
	if !ed.WebBuildServerState().Serving || !app.Exists("id:build-qr-canvas") {
		t.Fatalf("expected Start serving to restore LAN server and QR canvas: %+v", ed.WebBuildServerState())
	}
}

// Security, concurrency (-race), and Spec Light theme regression tests for the LAN web build server,
// MCP bridge, Android USB runner, and QR card.
func TestWebBuildServerSecurityConcurrencyAndSpecLight(t *testing.T) {
	t.Run("blocks_dotfiles_manifests_dns_rebinding_cross_origin_and_session_flood", func(t *testing.T) {
		webDir, _ := writeWebBuildFixture(t)
		// Plant sensitive dotfiles and manifests inside the served directory.
		for rel, body := range map[string]string{
			".git/config":       "[core]\nrepositoryformatversion = 0\n",
			".env":              "SECRET_KEY=supersecret\n",
			".game/editor.yaml": "version: 1\n",
			"game.yaml":         "name: secret\n",
			"nsgo.yaml":         "app: secret\n",
			"go.mod":            "module secret\n",
		} {
			p := filepath.Join(webDir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		ed, err := editor.New(nil, testResolver())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ed.Close() }()
		ed.LANAddressResolver = func() (string, error) { return "127.0.0.1", nil }

		snap, err := ed.StartWebBuildServer(webDir)
		if err != nil {
			t.Fatalf("StartWebBuildServer: %v", err)
		}
		client := &http.Client{Timeout: 5 * time.Second}

		// 1. Dotfiles and project manifests must be refused with 403 Forbidden.
		for _, badPath := range []string{".git/config", ".env", ".game/editor.yaml", "game.yaml", "nsgo.yaml", "go.mod"} {
			res, err := client.Get(snap.URL + badPath)
			if err != nil {
				t.Fatalf("GET %s: %v", badPath, err)
			}
			_ = res.Body.Close()
			if res.StatusCode != http.StatusForbidden {
				t.Fatalf("GET %s status = %d, want 403 Forbidden", badPath, res.StatusCode)
			}
		}

		// 2. DNS rebinding: mismatched Host header must be refused with 403 Forbidden.
		rebindReq, _ := http.NewRequest(http.MethodGet, snap.URL+"__nsgo_mcp/info", nil)
		rebindReq.Host = "attacker.example:12345"
		rebindRes, err := client.Do(rebindReq)
		if err != nil {
			t.Fatalf("rebind GET: %v", err)
		}
		_ = rebindRes.Body.Close()
		if rebindRes.StatusCode != http.StatusForbidden {
			t.Fatalf("DNS rebinding Host status = %d, want 403 Forbidden", rebindRes.StatusCode)
		}

		// 3. Cross-origin Origin header on bridge POST must be refused with 403 Forbidden.
		csrfReq, _ := http.NewRequest(http.MethodPost, snap.URL+"__nsgo_mcp/session", strings.NewReader(`{"token":"`+snap.BridgeToken+`"}`))
		csrfReq.Header.Set("Content-Type", "application/json")
		csrfReq.Header.Set("Origin", "https://evil.example")
		csrfRes, err := client.Do(csrfReq)
		if err != nil {
			t.Fatalf("cross-origin POST: %v", err)
		}
		_ = csrfRes.Body.Close()
		if csrfRes.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin bridge POST status = %d, want 403 Forbidden", csrfRes.StatusCode)
		}

		// 4. Session cap: up to 16 distinct clients succeed; 17th returns 429 Too Many Requests.
		for i := range 17 {
			body := fmt.Sprintf(`{"token":%q,"clientId":"client-%d","device":"Phone %d","playMode":true}`, snap.BridgeToken, i, i)
			req, _ := http.NewRequest(http.MethodPost, snap.URL+"__nsgo_mcp/session", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("session %d POST: %v", i, err)
			}
			_ = res.Body.Close()
			if i < 16 && res.StatusCode != http.StatusOK {
				t.Fatalf("session %d status = %d, want 200", i, res.StatusCode)
			}
			if i == 16 && res.StatusCode != http.StatusTooManyRequests {
				t.Fatalf("17th session status = %d, want 429 Too Many Requests", res.StatusCode)
			}
		}

		// 5. Range request streams cleanly via http.ServeContent.
		rangeReq, _ := http.NewRequest(http.MethodGet, snap.URL+"app.js", nil)
		rangeReq.Header.Set("Range", "bytes=0-6")
		rangeRes, err := client.Do(rangeReq)
		if err != nil {
			t.Fatalf("Range GET app.js: %v", err)
		}
		gotRange, _ := io.ReadAll(rangeRes.Body)
		_ = rangeRes.Body.Close()
		if rangeRes.StatusCode != http.StatusPartialContent || string(gotRange) != "console" {
			t.Fatalf("Range GET app.js = (%d, %q), want (206, %q)", rangeRes.StatusCode, string(gotRange), "console")
		}
	})

	t.Run("android_runner_concurrent_stdout_stderr_race_free_and_rejects_flag_injection", func(t *testing.T) {
		dir := writeBuildReleaseProject(t, `export:
  output: dist
  presets:
    - platform: android
      targets: [android_arm64]
`)
		ed, err := editor.New(nil, testResolver())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ed.Close() }()
		if err := ed.OpenProject(dir); err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenBuildReleaseScreen(); err != nil {
			t.Fatal(err)
		}

		// Complete an Android build first.
		_ = ed.SetBuildPresetIncluded("android", true)
		ed.ReleaseCheckRunner = func(_ context.Context, _ string) ([]assetcheck.Problem, time.Duration, error) {
			return nil, time.Millisecond, nil
		}
		ed.ReleaseStepRunner = func(ctx context.Context, req editor.ReleaseStepRequest, stdout, stderr io.Writer) error {
			return nil
		}
		ed.ReleasePlaytestRunner = func(ctx context.Context, platform, projDir string) (editor.BuildSmokePlaytestResult, error) {
			return editor.BuildSmokePlaytestResult{Summary: "ok"}, nil
		}
		ed.ReleaseMeasureRunner = func(ctx context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
			return editor.BuildArtifactResult{ArtifactPath: filepath.Join(dir, "dist", "app.aab"), SizeBytes: 1024}, nil
		}
		if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{}); err != nil {
			t.Fatalf("StartReleaseBuild: %v", err)
		}
		if err := ed.WaitReleaseBuild(); err != nil {
			t.Fatalf("WaitReleaseBuild: %v", err)
		}

		// 1. Malicious serial starting with '-' must be rejected before spawning subprocess.
		ed.DeviceDiscoverer = func(ctx context.Context, dir string) ([]editor.ConnectedDevice, error) {
			return []editor.ConnectedDevice{{ID: "-evil-flag", Name: "Evil", Platform: "android", Transport: "usb", Online: true}}, nil
		}
		if _, err := ed.RefreshConnectedDevices(); err != nil {
			t.Fatalf("RefreshConnectedDevices: %v", err)
		}
		if err := ed.RunBuildOnAndroidDevice(); err == nil || !strings.Contains(err.Error(), "invalid Android device serial") {
			t.Fatalf("RunBuildOnAndroidDevice with -evil-flag = %v, want invalid serial error", err)
		}

		// 2. Concurrent writes to stdout and stderr from the runner must be race-free under -race.
		ed.DeviceDiscoverer = func(ctx context.Context, dir string) ([]editor.ConnectedDevice, error) {
			return []editor.ConnectedDevice{{ID: "pixel8-usb:5555", Name: "Pixel 8", Platform: "android", Transport: "usb", Online: true}}, nil
		}
		if _, err := ed.RefreshConnectedDevices(); err != nil {
			t.Fatalf("RefreshConnectedDevices valid: %v", err)
		}
		ed.DeviceCommandRunner = func(ctx context.Context, req editor.DeviceCommandRequest, stdout, stderr io.Writer) error {
			if _, hasDeadline := ctx.Deadline(); !hasDeadline {
				return errors.New("expected timeout deadline on Android device run context")
			}
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				for range 200 {
					_, _ = io.WriteString(stdout, "stdout log line\n")
				}
			}()
			go func() {
				defer wg.Done()
				for range 200 {
					_, _ = io.WriteString(stderr, "stderr log line\n")
				}
			}()
			wg.Wait()
			return nil
		}
		if err := ed.RunBuildOnAndroidDevice(); err != nil {
			t.Fatalf("RunBuildOnAndroidDevice valid: %v", err)
		}
		if err := ed.WaitAndroidDeviceRun(); err != nil {
			t.Fatalf("WaitAndroidDeviceRun valid: %v", err)
		}
	})

	t.Run("spec_light_qr_card_uses_light_spec_palette", func(t *testing.T) {
		webDir, _ := writeWebBuildFixture(t)
		dir := writeBuildReleaseProject(t, `export:
  output: dist
  presets:
    - platform: web
`)
		ed, err := editor.New(nil, testResolver())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ed.Close() }()
		if err := ed.SetAppearance(editor.AppearanceLight); err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenProject(dir); err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenBuildReleaseScreen(); err != nil {
			t.Fatal(err)
		}
		ed.LANAddressResolver = func() (string, error) { return "127.0.0.1", nil }
		_ = ed.SetBuildPresetIncluded("web", true)
		ed.ReleaseCheckRunner = func(_ context.Context, _ string) ([]assetcheck.Problem, time.Duration, error) {
			return nil, time.Millisecond, nil
		}
		ed.ExportCommandRunner = func(_ context.Context, _ []string, stdout, _ io.Writer) error {
			_, _ = io.WriteString(stdout, "ok\n")
			return nil
		}
		ed.ReleasePlaytestRunner = func(ctx context.Context, platform, projDir string) (editor.BuildSmokePlaytestResult, error) {
			return editor.BuildSmokePlaytestResult{Summary: "ok"}, nil
		}
		ed.ReleaseMeasureRunner = func(ctx context.Context, platform string, plan *releaseplan.Plan) (editor.BuildArtifactResult, error) {
			return editor.BuildArtifactResult{ArtifactPath: filepath.Join(webDir, "index.html"), WebBuildDir: webDir, SizeBytes: 1024}, nil
		}
		if err := ed.StartReleaseBuild(editor.BuildReleaseOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := ed.WaitReleaseBuild(); err != nil {
			t.Fatal(err)
		}
		ed.SelectBuildPreset("web")

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   false,
			Locale: "en-US",
		}))

		light := editor.LightSpecPalette()
		qrCard := app.Find("id:build-result-qr").Style
		if qrCard.Background != light.Raised || qrCard.BorderColor != light.LineRaised {
			t.Fatalf("Spec Light build-result-qr bg=0x%08x border=0x%08x, want LightSpecPalette().Raised/LineRaised 0x%08x/0x%08x",
				uint32(qrCard.Background), uint32(qrCard.BorderColor), uint32(light.Raised), uint32(light.LineRaised))
		}
		qrStatus := app.Find("id:build-qr-status").Style
		if qrStatus.Background != light.Added.Fill || qrStatus.BorderColor != light.Added.Edge {
			t.Fatalf("Spec Light build-qr-status bg=0x%08x border=0x%08x, want LightSpecPalette().Added 0x%08x/0x%08x",
				uint32(qrStatus.Background), uint32(qrStatus.BorderColor), uint32(light.Added.Fill), uint32(light.Added.Edge))
		}
	})
}
