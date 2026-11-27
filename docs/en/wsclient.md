# wsclient

`game/online/wsclient` is a plain RFC 6455 WebSocket client for servers that are not a [netplay](netplay.md) `WebSocketServer`: third-party game backends such as Nakama or Colyseus, a Node or Elixir game server, Twitch or YouTube chat integration for a streamer game, a hosted realtime database, or a custom JSON protocol shared with a web dashboard. The same API works on native targets and in the browser (`js/wasm`).

```go
import "github.com/nsgo-dev/game/pkg/online/wsclient"
```

To talk to a server written with this repository's netplay package, keep using `netplay.DialWebSocket`: it speaks netplay's own handshake and channel framing on top of WebSocket, which a plain client does not.

```go
ws, err := wsclient.Dial(ctx, "wss://chat.example.com/socket", wsclient.Options{
	Protocols:  []string{"v1.json"},
	Header:     http.Header{"Authorization": {"Bearer " + tok}}, // native only
	MaxMessage: 1 << 20,
})
if err != nil {
	return err
}
// Each tick, on the engine goroutine:
for msg, ok := ws.Recv(); ok; msg, ok = ws.Recv() {
	if msg.Text {
		handle(string(msg.Data))
	}
}
_ = ws.SendText(`{"op":"join"}`)
if ws.State() == wsclient.Closed {
	code, reason := ws.CloseStatus()
	_ = code
	_ = reason
}
```

`Dial` blocks until the connection is open or fails, on every target, and returns a ready client. The client is `Open` until either side closes the connection or the network drops it; a plain client does not reconnect, so a `Closed` client is done and a new `Dial` takes its place.

## API

| Method | Meaning |
| --- | --- |
| `Send(data)` | Send one binary message. |
| `SendText(text)` | Send one UTF-8 text message. |
| `Recv()` | Pop the oldest received message without blocking; drain it every tick. |
| `State()` | `Open` or `Closed`. |
| `Close(code, reason)` | Send a close frame with an RFC 6455 status code (the `Close*` constants) and end the connection. A zero code sends no code. |
| `CloseStatus()` | The code and reason of the close that ended the connection: the peer's when it closed first, otherwise the ones `Close` was called with. `CloseNoStatus` (1005) means the close frame carried no code, `CloseAbnormal` (1006) that the connection dropped with no close frame at all. |
| `Err()` | Why the connection closed: nil for a clean close, `ErrMessageTooLarge` or `ErrProtocol` for one the client ended, or the read error for a dropped connection. |
| `Protocol()` | The subprotocol the server selected, or `""` for none. |
| `Buffered()` | How many received messages wait for `Recv`. |
| `Dropped()` | How many received messages were dropped because the inbox was full (see `MaxInbox`). |

## Options

| Option | Meaning |
| --- | --- |
| `Protocols` | Subprotocols offered in preference order; the server picks one or none. Works on every target. |
| `Header` | Extra handshake headers such as an `Authorization` token. Native only: the browser WebSocket API cannot set headers, so a browser `Dial` with a non-empty `Header` fails with `ErrHeadersUnsupported` instead of silently dropping them. On the web, put credentials in a cookie the browser attaches, or in a subprotocol or query parameter the server accepts. |
| `MaxMessage` | The largest message, in bytes, the client receives or sends (4 MiB when zero). A larger incoming message is refused: the connection closes with 1009 and `Err` reports `ErrMessageTooLarge`. |
| `MaxInbox` | The most received messages queued for `Recv` (256 when zero, negative for unbounded). Past the bound the oldest queued message is dropped to make room and `Dropped` counts the loss, so a game that stops draining loses the stalest state first rather than growing memory without bound. |

## Behavior notes

- The client masks its frames and answers the server's pings with pongs on its own, as RFC 6455 asks of clients; browsers do both by themselves. There is no client-initiated ping or idle timeout, because a third-party server may stay silent for as long as it likes.
- Fragmented messages are reassembled before `Recv` returns them, up to `MaxMessage` across all fragments. A peer that starts a new message before finishing a fragmented one is a protocol error: the connection closes with 1002 and `Err` reports `ErrProtocol`.
- `Close` waits at most a second for a server that does not read, then closes the socket anyway, so the game loop never hangs on a close. A `Send` blocked on a server that does not read is broken by `Close` and returns `ErrClosed`.
- `Send` and `SendText` are safe from any goroutine; writes are serialized.
- The handshake is only as private as the scheme: use `wss://` in production so the handshake headers and the traffic are encrypted.
- On the web, `Dial` suspends its goroutine until the browser finishes the asynchronous connect; the page's event loop keeps running, but dial during loading or from a worker, not inside a frame callback that must return.
- `permessage-deflate` and other extensions are not negotiated; the framing is plain RFC 6455.

## Testing

`pkg/online/wsclient/ws_test.go` runs the native client against an in-process server: text and binary echoes, server pings answered, fragmented messages reassembled, close codes and reasons in both directions, subprotocol negotiation and its mismatch, handshake headers, refused upgrades, oversized messages refused, the bounded inbox dropping oldest, and context-canceled dials. The browser client shares the same API and compiles under `go tool nsgo check -target web`; the repository has no headless-browser test harness, so its WebSocket API wiring is covered by the shared code paths (inbox, options, close-code validation) rather than by an end-to-end web test.
