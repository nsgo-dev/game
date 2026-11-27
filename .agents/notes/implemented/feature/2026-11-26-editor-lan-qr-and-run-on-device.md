# Agent Note: Editor LAN web build server, pure-Go QR code, Play-mode MCP bridge, and RUN ON DEVICE targets

Status: implemented

## Problem

After completing a Web or Android build on the [Build and Release screen](2026-11-26-editor-build-release-screen.md), testing the build on a phone still required copying files manually or running separate CLI commands (#1049). The editor needed:

1. A local-network-only HTTP server for finished web builds that shows its LAN URL and a scannable QR code, and immediately frees its TCP port when stopped or when the editor closes.
2. A Play-mode MCP debug bridge back to the editor so an agent can inspect and debug a web build running on a phone.
3. Live RUN ON DEVICE controls (`Build.dc.html`) for this Mac's/computer's browser, a phone over Wi-Fi/QR, and a USB-connected Android device through `go tool nsgo` device commands.

## Decision

1. **Self-contained pure-Go ISO/IEC 18004 QR encoder (`packages/game/internal/qrcode`)**:
   - Implement a zero-dependency deterministic QR Code Model 2 encoder (versions 1–10, byte mode, error correction levels `L`/`M`/`Q`/`H`, full GF(256) Reed-Solomon ECC, all 8 masks with ISO/IEC 18004 N1–N4 penalty selection, BCH format/version info) in `packages/game/internal/qrcode/qrcode.go`.
   - Render the QR code via `core.Canvas` (`drawQRCode` in `packages/game/editor/build_release_ui.go`) with integer-snapped module cells on a 4-module light quiet zone using Spec tokens (`specTextStrong` `#FFFFFF` background and `specBg` `#101216` modules, 18.75:1 AAA contrast).
2. **LAN-only web build HTTP server and immediate port release (`packages/game/editor/build_device.go`)**:
   - `StartWebBuildServer` resolves a private RFC 1918 IPv4 LAN address (falling back to `127.0.0.1` when offline; injectable via `Editor.LANAddressResolver`, configurable port via `Editor.WebBuildServePort`), refuses any non-LAN/public IP (`ErrNonLANAddress`), binds explicitly to that IPv4 interface (`tcp4`), and enforces a LAN-only client IP middleware (`isRequestFromLAN`) returning `403 Forbidden` to non-LAN peers.
   - Serves directories through `safeDirFS` (refusing symlink escapes outside the build directory) or `.zip` build archives directly with explicit MIME types (`application/wasm`, `text/html; charset=utf-8`, `text/javascript; charset=utf-8`, `audio/opus`, etc.) and `Cache-Control: no-store`.
   - `StopWebBuildServer` cancels pending bridge polls, closes both the `http.Server` and `net.Listener`, and waits for the server goroutine to exit before returning, guaranteeing the TCP port is immediately re-bindable. Wired into the UI **Stop serving** button, `Editor.Close()`, `EditorView`'s `cx.Cleanup`, and `resetBuildReleaseForProject()`.
   - Non-dry-run web builds automatically invoke `autoServeWebBuildIfReady` on completion without calling `e.notify()` from the build goroutine (UI notification goes through `Scope.Post`).
3. **Non-mutating Play-mode MCP bridge over LAN (`/__nsgo_mcp/*`), web runtime hook (`internal/webmcpbridge`), and editor MCP phone tools (`pkg/editor/editormcp/phone.go`)** (#1049, #1506):
   - Preserve exact byte-for-byte contents of `index.html` and all build assets while advertising bridge discovery via `X-NSGo-MCP-Bridge: /__nsgo_mcp/info` and `X-NSGo-Play-Mode: debug` response headers.
   - Serve `/__nsgo_mcp/info`, `/__nsgo_mcp/bridge.js` (idempotent guard, `pagehide` session disconnect via `DELETE` with `keepalive`), token-authenticated `/__nsgo_mcp/session` (POST connect / DELETE disconnect), `/__nsgo_mcp/rpc` (JSON-RPC 2.0 with optional `Editor.WebBridgeHandler`), and long-poll bidirectional tool dispatch (`/__nsgo_mcp/poll` + `/__nsgo_mcp/reply`, driven from Go by `Editor.CallWebBridgeTool` and observed via `Editor.WebBridgeSessions`).
   - On `js && wasm`, `internal/webmcpbridge.Register` (called from `pkg/mcp.Bridge.Attach` at `PriorityBridge` and as a fallback from `game.Loop.Run` at `PriorityLoop`) exposes `window.__nsgoMCP.callTool` / `listTools` posting onto the engine goroutine (`core.Scope.Post`) and runs a one-time CSP-safe `HEAD` probe on `window.location.href`: when `X-NSGo-Play-Mode: debug` is present, it validates and fetches `X-NSGo-MCP-Bridge` (`/__nsgo_mcp/info`) and injects `<script src="/__nsgo_mcp/bridge.js">`; production hosts without the header exit quietly without requesting `/__nsgo_mcp/*`.
   - `pkg/mcp` adds `game_state_dump` (`statedump.Capture` + `statedump.Encode` + loop counters) and `Bridge.CallTool` so both `nsgo mcp` and `window.__nsgoMCP.callTool` serve the exact same tool set.
   - `pkg/editor/editormcp` adds `phone_sessions_list` (`ActionRead`) and `phone_call_tool` (`ActionRun`, target `playtests: phone/<tool>`, obeying `permissions.yaml` and `AgentPrivacyPolicy`, decoding `game_screenshot` `png_base64` into MCP `ImageContent` and calling `Editor.CallWebBridgeTool` off the editor goroutine).
4. **Live RUN ON DEVICE orchestration (`build_device.go`, `build_device_desktop.go`, `build_device_other.go`)**:
   - Split desktop process/browser/adb defaults (`build_device_desktop.go`) from mobile stubs (`build_device_other.go`) so `go tool nsgo check -target ios ./editor` stays clean.
   - Expose injectable hooks (`OpenBrowserURL`, `DeviceDiscoverer`, `DeviceCommandRunner`) and live `BuildDeviceState` transitions (`ready`, `serving`, `connected`, `stopped`, `installing`, `running`, `disconnected`, `unavailable`, `failed`) for browser, QR phone, and USB Android (`go -C <dir> tool nsgo build -target <target> -run -device <serial>`).

## Alternatives considered

- **Adding an external QR library to `packages/game/go.mod` (e.g. promoting indirect `rsc.io/qr` or importing `go-qrcode`)**: Slightly less code to maintain. Rejected because #1049 flags a new QR dependency as needing explicit approval, `packages/game/internal/qrcode` stays ~400 lines of pure Go with zero dependencies and an independent ISO/IEC 18004 decoder + Reed-Solomon syndrome verifier in tests, and `go.mod` / `go.sum` remain untouched.
- **Injecting a `<script>` tag directly into served `index.html` bytes for the MCP bridge**: Automatic zero-config hook inside arbitrary HTML files. Rejected because mutating `index.html` alters the served build's exact bytes, breaks content-length/integrity verification, and complicates CSP; exposing `X-NSGo-MCP-Bridge` headers plus `/__nsgo_mcp/info` and `/__nsgo_mcp/bridge.js` keeps served build files byte-for-byte intact while giving Play-mode runtimes clean discovery.
- **Binding the LAN server to `0.0.0.0:<port>`**: Works across every local interface automatically. Rejected because `0.0.0.0` would expose the test build on non-LAN or VPN/public interfaces; binding explicitly to a validated private LAN IPv4 (or loopback) plus per-request client IP checking guarantees local-network-only access.

## Consequences

- **Benefits**:
  - Authors can immediately scan a crisp Spec-styled QR code on their phone over Wi-Fi after a web build finishes, copy the URL, or stop/restart serving with immediate port release.
  - Phone Play-mode sessions connect back to the LAN MCP bridge and support bidirectional tool invocation without mutating build files.
  - RUN ON DEVICE reports accurate live states across Mac/computer browser, Wi-Fi phone, and USB Android targets, and compiles cleanly on both desktop and `-target ios`.
- **Costs and known limits**:
  - `packages/game/internal/qrcode` supports QR versions 1–10 (up to 213 bytes at EC level M / 271 bytes at EC level L), which covers LAN URLs with ample headroom; arbitrary kilobyte payloads would require extending the RS block table beyond version 10.
  - Default USB Android discovery relies on `adb` on `PATH` when `Editor.DeviceDiscoverer` is not injected, gracefully reporting no connected USB device when `adb` is unavailable.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo check ./internal/qrcode && env -u GOROOT go tool nsgo check ./editor && env -u GOROOT go tool nsgo check -target ios ./editor && env -u GOROOT go tool nsgo check ./editor/editorcmd)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./internal/qrcode && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor -- -run 'TestBuildRelease|TestOpenFromTheFileBrowserOpensTabs|TestDiscardFollowsTheProjectBack|TestLightingHandlesAlignOnParallaxLayer|TestLocaleCatalogsComplete|TestDesktopMenuBarMatchesRegistryAndExecutesActions')`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test -target macos -run TestBuildReleaseWebQRAndDevicesMacOSScreenshots ./editor/editorcmd)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
