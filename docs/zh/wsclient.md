# wsclient

`game/online/wsclient` 是一个朴素的 RFC 6455 WebSocket 客户端，用来连接不是 [netplay](netplay.md) `WebSocketServer` 的服务器：Nakama、Colyseus 等第三方游戏后端、Node 或 Elixir 写的游戏服务器、为主播游戏接入 Twitch 或 YouTube 聊天、托管实时数据库，或与网页仪表盘共用的自定义 JSON 协议。同一套 API 在原生平台和浏览器（`js/wasm`）上都能用。

```go
import "github.com/nsgo-dev/game/pkg/online/wsclient"
```

要连接用本仓库 netplay 包写的服务器，继续用 `netplay.DialWebSocket`：它在 WebSocket 之上说 netplay 自己的握手和通道分帧，朴素客户端不懂这套协议。

```go
ws, err := wsclient.Dial(ctx, "wss://chat.example.com/socket", wsclient.Options{
	Protocols:  []string{"v1.json"},
	Header:     http.Header{"Authorization": {"Bearer " + tok}}, // 仅原生平台
	MaxMessage: 1 << 20,
})
if err != nil {
	return err
}
// 每一帧，在引擎 goroutine 上：
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

`Dial` 在每个平台上都会阻塞到连接建立或失败，返回一个就绪的客户端。客户端一直是 `Open`，直到某一方关闭连接或网络断开；朴素客户端不会自动重连，`Closed` 的客户端就此结束，重新 `Dial` 一个新的一。

## API

| 方法 | 含义 |
| --- | --- |
| `Send(data)` | 发送一条二进制消息。 |
| `SendText(text)` | 发送一条 UTF-8 文本消息。 |
| `Recv()` | 非阻塞地取出最早收到的消息；每一帧都要掏空它。 |
| `State()` | `Open` 或 `Closed`。 |
| `Close(code, reason)` | 发送携带 RFC 6455 状态码（`Close*` 常量）的关闭帧并结束连接。传 0 表示不带状态码。 |
| `CloseStatus()` | 结束这次连接的关闭码与原因：对端先关闭就是对端的，否则是调用 `Close` 时传的。`CloseNoStatus`（1005）表示关闭帧没带状态码，`CloseAbnormal`（1006）表示连接在没有任何关闭帧的情况下断掉。 |
| `Err()` | 连接关闭的原因：正常关闭为 nil，客户端主动结束为 `ErrMessageTooLarge` 或 `ErrProtocol`，连接掉线则为读取错误。 |
| `Protocol()` | 服务器选定的子协议，没有则为 `""`。 |
| `Buffered()` | 等待 `Recv` 取走的消息条数。 |
| `Dropped()` | 因为收件箱已满而被丢弃的消息条数（见 `MaxInbox`）。 |

## 选项

| 选项 | 含义 |
| --- | --- |
| `Protocols` | 按偏好顺序提供的子协议；服务器选定一个或都不选。所有平台都支持。 |
| `Header` | 额外的握手请求头，比如 `Authorization` 令牌。仅原生平台：浏览器 WebSocket API 无法设置请求头，所以在浏览器里给非空的 `Header` 会让 `Dial` 返回 `ErrHeadersUnsupported`，而不是悄悄丢弃。在 Web 上把凭据放进浏览器自动携带的 Cookie，或放进服务器接受的子协议或查询参数里。 |
| `MaxMessage` | 客户端接收或发送的最大消息字节数（为 0 时 4 MiB）。更大的入站消息会被拒绝：连接以 1009 关闭，`Err` 报告 `ErrMessageTooLarge`。 |
| `MaxInbox` | 排队等待 `Recv` 的消息上限（为 0 时 256，负数表示不设限）。超出上限时丢弃最旧的消息腾出位置，并用 `Dropped` 计数，所以停止消费的游戏先丢最旧的状态，而不是让内存无限增长。 |

## 行为说明

- 客户端按 RFC 6455 的要求给自己的帧加掩码，并自动用 pong 回应服务器的 ping；浏览器两者都自己完成。客户端不主动发 ping，也没有空闲超时，因为第三方服务器想安静多久都可以。
- 分片消息在所有分片合计不超过 `MaxMessage` 的前提下重组后再交给 `Recv`。对端没发完分片消息就开始新消息属于协议错误：连接以 1002 关闭，`Err` 报告 `ErrProtocol`。
- `Close` 对不读取数据的服务器最多等一秒，然后照样关掉套接字，游戏循环绝不会卡在关闭上。被不读取数据的服务器堵住的 `Send` 会被 `Close` 打断并返回 `ErrClosed`。
- `Send` 和 `SendText` 可以在任何 goroutine 上调用；写入是串行化的。
- 握手的私密性取决于协议 scheme：生产环境用 `wss://`，握手请求头和后续流量才会加密。
- 在 Web 上，`Dial` 会挂起自己的 goroutine 直到浏览器完成异步连接；页面的事件循环继续运转，但请在加载阶段或 worker 里拨号，不要在必须返回的帧回调里。
- 不协商 `permessage-deflate` 等扩展；分帧是纯粹的 RFC 6455。

## 测试

`pkg/online/wsclient/ws_test.go` 让原生客户端连接一个进程内的服务器进行测试：文本与二进制回显、回应服务器 ping、重组分片消息、两个方向的关闭码与原因、子协议协商及其不匹配、握手请求头、被拒绝的升级、超大消息被拒、有界收件箱丢弃最旧消息，以及被取消的 context 拨号。浏览器客户端共享同一套 API，并在 `go tool nsgo check -target web` 下编译通过；仓库没有无头浏览器测试设施，所以它的 WebSocket API 接线由共享代码路径（收件箱、选项、关闭码校验）覆盖，而不是端到端的 Web 测试。
