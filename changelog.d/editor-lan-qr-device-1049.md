### Added

- Serve finished web builds strictly on the local network with a scannable pure-Go QR code (`packages/game/internal/qrcode`), Copy URL and Stop/Start serving controls that immediately release the TCP port on stop or editor close (#1049).
- Expose a LAN Play-mode MCP debug bridge (`/__nsgo_mcp/info`, `/__nsgo_mcp/bridge.js`, `/__nsgo_mcp/session`, `/__nsgo_mcp/poll`, `/__nsgo_mcp/reply`, `/__nsgo_mcp/rpc`) so the editor and agent can inspect and debug served web builds running on a phone (#1049).
- Wire live RUN ON DEVICE states and launch actions on the Build & Release screen for this Mac's/computer's browser, phone via QR/LAN link, and USB-connected Android devices through `go tool nsgo` device commands (#1049).
