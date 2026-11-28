# backend

`github.com/nsgo-dev/game/pkg/online/backend` 将现有在线 HTTP 处理器和编译进游戏的 RPC 路由组合为一个可嵌入的 `http.Handler`。

```go
b := backend.New(func(r *http.Request) (backend.Caller, error) {
    claims, err := verifyBearer(r)
    if err != nil { return backend.Caller{}, err }
    return backend.Caller{AccountID: claims.Subject}, nil
})
_ = b.Mount("/accounts/", accountsService.Handler())
_ = b.Mount("/saves/", cloudsaveService.Handler())
_ = b.RPC("claim-daily-reward", func(ctx context.Context, caller backend.Caller, raw json.RawMessage) (any, error) {
    return rewards.Claim(ctx, caller.AccountID)
})
http.ListenAndServe(":8080", b)
```

`Mount` 会在前缀下保留服务原有的认证、CORS、校验和错误响应。`RPC` 只接受 `POST /v1/rpc/{name}`，要求调用方提供认证函数，限制 JSON 请求为 1 MiB，并将类型化的 `Caller` 和原始 JSON 传给编译进服务端的游戏代码。成功的 RPC 返回 JSON。认证失败返回 401，JSON 格式错误返回 400，处理函数错误返回 400。

该 mux 可嵌入应用服务器，本身不创建协程，也不持有服务存储。令牌校验、限流、服务 before/after hook 和组合 YAML 配置在各服务暴露类型化 hook 点之前仍由应用负责。
