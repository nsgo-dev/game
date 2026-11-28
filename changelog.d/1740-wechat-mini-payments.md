### Added

- `game/pkg/online/storeverify/wechatpay`: the NSGo-free server side of WeChat mini game virtual payments: `SignOrder` builds the `signData`, `paySig` and user-state `signature` for `wx.requestMidasPaymentGameItem`, and `Handler` verifies goods delivery pushes (event, `PayEventSig`, environment) and delivers each `OutTradeNo` once through a `DeliveryStore` (#1740).

### Changed

- `docs/en/liveops.md` and `docs/zh/liveops.md`: add a bilingual integration checklist for server-only signing keys, raw push verification, environment isolation and durable idempotent delivery (#1740).
