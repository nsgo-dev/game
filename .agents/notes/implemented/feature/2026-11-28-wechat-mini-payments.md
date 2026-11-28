# Agent Note: WeChat mini game payment boundary

Status: implemented

## Problem

WeChat mini game virtual payments (#1740) do not fit `liveops.Purchases`: the client call `wx.requestMidasPaymentGameItem` needs a `signData` JSON signed with the game's AppKey (`paySig`) and with the player's session key (`signature`), and the platform delivers goods by pushing `minigame_game_pay_goods_deliver_notify` to the game's server, retrying 13 times over 12 hours until the server answers `ErrCode: 0`. The AppKey must never reach a client, the client's success callback can be lost, and a delivery handler that is not idempotent grants goods twice on retries.

## Decision

`pkg/online/storeverify/wechatpay` is a core-free server package:

- `SignOrder` marshals a fixed-order `signData` (`mode: goods`, `currencyType: CNY`, camelCase field names as the platform reads them) and signs it as the platform documents: `paySig = hmac_sha256(appKey, "requestMidasPaymentGameItem&" + signData)`, `signature = hmac_sha256(sessionKey, signData)`. It validates `OutTradeNo` against the platform's rule (at most 32 characters of letters, digits and `_-|*@`, no leading underscore) so a bad order number fails on the server, not at the payment sheet.
- `Handler.Handle(ctx, event, payload, payEventSig)` verifies `PayEventSig = hmac_sha256(appKey, event + "&" + payload)` over the raw `Payload` string with a constant-time compare, refuses any event but goods delivery, decodes the payload with the platform's field names (`OpenId`, `Env`, `OutTradeNo`, `GoodsInfo{ProductId, Quantity, ...}`) tolerating fields it does not declare, and refuses a payload whose `Env` differs from `Handler.Env`.
- Delivery is once per `OutTradeNo`: a per-key lock inside the `Handler` serializes concurrent retries, `DeliveryStore.Get` short-circuits an order already delivered, and `Put` runs only after `Deliver` succeeds, so a failed grant is retried by the platform's next push.

The tests pin `signData`, `paySig`, `signature` and `PayEventSig` to values computed independently with `openssl dgst -sha256 -hmac`, and cover a tampered payload, a wrong signature, a foreign event, both environment mismatches, ten sequential retries, sixteen concurrent retries and a failed delivery.

The English and Chinese liveops guides also carry the same integration checklist. It keeps the AppKey and session key on the server, treats the raw signed delivery push as the purchase authority, requires environment isolation, and makes durable `OutTradeNo` idempotency explicit for deployments that span restarts or processes. The checklist requires success only after both the grant and delivery record have succeeded, preserving platform retries after a failed grant.

## Alternatives considered

### Signing a re-marshaled `Delivery` struct

Strongest argument: the handler works on typed values only. Rejected: the platform signs the exact `Payload` string it sends, and Go's re-encoding changes field names, order and unknown fields, so no real push would verify.

### Treating `Handler.Env == 0` as "accept any environment"

Strongest argument: a zero-value handler works in both environments during development. Rejected: `0` is production, so that default would let sandbox purchases, which cost nothing, grant goods in production.

### One mutex around the whole `Handle`

Strongest argument: simpler. Rejected: `Deliver` writes to the game's database while the platform times a push out after 3 seconds, and unrelated orders should not queue behind each other.

## Consequences

Games plug the boundary into their own HTTP endpoint and store. The lock only covers one process: several processes behind one durable store must make `Deliver` idempotent on `OutTradeNo` (grant in the transaction that records the order), which the docs state. Still open in #1740: the push URL's token check and safe-mode AES decryption, SQLite and Postgres stores with a `storetest` suite, an order query for reconciliation, a client-side `Purchases` mode, and mapping `-15009` to the spending-limit error.
