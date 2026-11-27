# Agent Note: liveops.Invites share cards and payload wire format

Status: implemented

## Problem

Games targeting mini games, mobile, and the web need to share invite cards carrying structured payloads (such as room/party IDs and reward invite IDs) and read those payloads back when another player cold-starts or resumes the game from the card. NSGo v0.3.4 provides `core.Scope.Share`, `core.Scope.OnOpenLinks`, `core.Scope.AppURL`, `core.Scope.OnAppURLChange`, and `core.Scope.Capture` for native and web targets, while mini-game share cards and launch options wait on upstream `nsgo-dev/nsgo#2672`. Without a shared game-layer contract, each game hand-rolls payload encoding, size enforcement, signature checks, and launch/resume once-only delivery. Issue #1738, part of #1479.

## Decision

`pkg/online/liveops` provides `Invites` over a 5-method `ShareHost` interface (`invites.go`, `invites_payload.go`, `invites_host.go`, `invites_snapshot.go`):

- **Canonical wire payload format**: payloads encode as lexicographically sorted `key=value&…` query strings (`EncodeInvitePayload` / `DecodeInvitePayload`) using `url.QueryEscape` / `url.QueryUnescape`. Keys are bounded to 1–64 bytes of ASCII `[A-Za-z0-9._-]`, values must be valid UTF-8 with no ASCII control characters (`< 0x20` or `0x7f`), key count is capped at `MaxInvitePayloadKeys` (32), and the total encoded length (including the optional signature) is capped by the host limit (`DefaultMaxInvitePayloadBytes` = 1024). Duplicate keys, empty segments, missing `=`, bad percent-escapes, and oversized payloads are rejected safely before sharing or delivery; unknown valid keys are preserved.
- **Standard keys and HMAC-SHA256**: `InviteIDKey` (`"id"`) carries the invite's unique ID (surfaced as `Invite.ID` for server reward-grant idempotency, #1643). `InviteSigKey` (`"sig"`) carries an optional base64url raw HMAC-SHA256 tag (43 ASCII bytes) computed over the canonical sorted unsigned `key=value&…` string. Decoding re-sorts non-`sig` keys and re-encodes values before constant-time `hmac.Equal` verification so signatures remain valid even if a messaging app or browser reorders parameters or rewrites `+` as `%20`. `Card.Signature` and `Invite.RawQuery` allow production games to keep the HMAC secret exclusively on the game server (`SignInvitePayload` / `VerifyInvitePayload`), while `InvitesOptions.HMACKey` supports local verification when desired.
- **At-most-once launch and resume delivery**: `NewInvites` reads any cold-start invite from `ShareHost.LaunchInvite()` once and exposes it through `Opened() (Invite, bool)`, which consumes it on first call. Warm resume invites (`ShareHost.OnInvite`) are delivered once each to `OnInvite` listeners and queued for reactive once-only popping via `Consume() (Invite, bool)`. Separating internal `arrived` and `pending` `core.Signal[int]` ticks ensures a `core.Effect` calling `Consume()` re-runs only when a new invite arrives, while UI badges reading `PendingCount()` or `Incoming()` update immediately when items are consumed.
- **Platform adapters and mini-game boundary**: `PlatformShareHost` implements `ShareHost` on mobile, desktop, and web over NSGo's public `core` APIs, supporting both query (`InviteEncodeQuery`) and fragment (`InviteEncodeFragment`) URL encoding, auto-deriving `BaseURL` from `cx.AppURL()` on `core.OSWeb`, suppressing the single queued `OnOpenLinks` duplicate when `LaunchInvite` already read the cold-start URL from `cx.AppURL()`, and offering `FallbackToClipboard` when `cx.Share` returns `core.CodeUnavailable`. `MiniGameShareHost` wraps `MiniGameShareDriver` (`ShareAppMessage`, `ShowShareMenu` with `WithShareTicket`, `OnShareAppMessage`, `LaunchOptions`, `OnShow`), preserving `Scene` and `ShareTicket` and degrading cleanly to `ErrShareUnavailable` when given a `nil` driver until `nsgo-dev/nsgo#2672` lands.
- **5:4 frame snapshot helper**: `CaptureCardSnapshot` (and pure image helpers `CropCardSnapshot` / `EncodeCardSnapshotPNG`) center-crops any captured frame from `core.Scope.Capture` to a 5:4 rectangle and downscales it to at most `DefaultCardSnapshotWidth` (500×400) without upscaling smaller images, returning a PNG `core.ImageSource` ready for `Card.Image`.

## Wire contract invariants

1. `EncodeInvitePayload` -> `DecodeInvitePayload` -> `EncodeInvitePayload` must be byte-for-byte deterministic.
2. Oversized or invalid cards must be refused in `PrepareCard` before `ShareHost.Share` is ever invoked.
3. Any payload containing duplicate keys (including duplicate `sig`) must be rejected so parameter smuggling cannot bypass HMAC verification.
4. When `HMACKey` is non-empty, a present but invalid `sig` must always be rejected with `ErrInvalidInviteSignature`, even when `RequireSignature` is false (only unsigned payloads are accepted with `Verified: false`).
5. `Opened()` must return each cold-start invite at most once, and `Consume()` must return each warm-resume invite at most once.

## Alternatives considered

- **Base64-encoded JSON blob in a single `p=` query parameter**: compact for nested structures, but breaks human readability, prevents mini-game analytics or external links from inspecting individual parameters like `room` or `id`, and bloats short payloads by ~33% plus JSON syntax overhead against WeChat's query length budget. Canonical `key=value&…` matches WeChat's native `query` map and standard URL query strings directly.
- **Computing HMAC over the raw wire query bytes as received**: simpler on decode, but fragile whenever chat apps, URL shorteners, browsers, or mini-game `query` object deserializers reorder keys or normalize `%20` vs `+`. Canonicalizing sorted non-`sig` pairs before HMAC verification eliminates false rejections while remaining strict against duplicate keys and tampering.
- **Embedding a client-side `PlatformShareHost` check for mini-game globals via build tags**: premature before `nsgo-dev/nsgo#2672` settles NSGo's mini-game driver API. Isolating mini-game integration behind `MiniGameShareDriver` keeps `liveops` 100% portable across `macos_arm64`, `web`, `ios_simulator`, and `android_arm64` today and gives NSGo #2672 a direct drop-in hook.

## Consequences

- **Benefits**: games get a uniform, SDK-free share-card and invite API across mobile, desktop, web, and mini games, with strict bounds, deterministic HMAC verification, reactive once-only consumption, 5:4 frame snapshots, and full headless unit + `nsgotest` coverage (`pkg/online/liveops/invites_test.go`).
- **Costs**: live mini-game forwarding on WeChat/Douyin still requires wiring a `MiniGameShareDriver` (or upgrading once `nsgo-dev/nsgo#2672` lands), and payload values are restricted to flat `map[string]string` pairs within the host byte limit.
