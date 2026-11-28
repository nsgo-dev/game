# backend

`github.com/nsgo-dev/game/pkg/online/backend` composes existing online HTTP handlers and compiled game RPC routes into one embeddable `http.Handler`.

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

`Mount` keeps each service's existing authentication, CORS, validation and error responses intact under its prefix. `RPC` accepts only `POST /v1/rpc/{name}`, requires the caller-supplied authentication function, limits JSON requests to 1 MiB and passes a typed `Caller` plus raw JSON to compiled game code. Successful RPC results are JSON. Authentication failures return 401, malformed JSON returns 400 and handler errors return 400.

The mux is safe to embed in an application server and has no goroutines or service stores of its own. Token verification, rate limiting, before/after service hooks and combined YAML configuration remain application concerns until the individual services expose typed hook points.
