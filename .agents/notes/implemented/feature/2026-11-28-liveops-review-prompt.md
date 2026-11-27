# Agent Note: liveops.ReviewPrompt store rating policy

Status: implemented

## Problem

NSGo v0.3.4 exposes `core.Scope.RequestReview` and `core.Env.CanRequestReview`, leaving it to each game to decide when to ask for a store rating. Apple StoreKit caps prompts at three per 365 days per app and discourages triggering from buttons or mid-action interruptions, while Google Play applies an opaque quota and discourages prompting before sufficient usage or behind leading pre-prompts. Without a shared persisted policy, each game had to hand-roll session counts, cooldown tracking, negative-moment suppression, and corrupt-state handling. Issue #1767, part of #1479.

## Decision

`pkg/online/liveops/review.go` provides `ReviewPrompt` over `core.Storage` and `core.Scope.RequestReview`:

- **Policy thresholds and defaults**: `NewReviewPrompt(store, opts)` starts a session and gates `MaybeAsk(cx)` on `MinSessions` (`DefaultReviewMinSessions` = 3, avoiding first-session prompts), `MinDays` since first launch (`DefaultReviewMinDays` = 3), `MinPositiveMoments` in the current `AppVersion` (`DefaultReviewMinPositiveMoments` = 3), `Cooldown` across all app versions (`DefaultReviewCooldown` = 122 days, mathematically guaranteeing at most three prompts in any rolling 365-day window), and once per `AppVersion`.
- **Moments and suppression**: `Moment(MomentPositive)` increments persisted per-version positive moments; `Moment(MomentNegative)` sets an in-memory session-scoped suppression flag so no prompt fires for the remainder of that session (even after later positive moments), cleared on the next launch or `RecordSession()`. `ReviewPromptOptions.Suppress` lets games block prompts while a purchase sheet, ad flow, or modal is open.
- **Version reset vs lifetime fields**: when `AppVersion` changes, `positiveMoments` resets to 0 so the new release earns fresh engagement before prompting, while `firstLaunch`, `sessions`, and `lastPromptedAt` persist so cooldown still enforces store-wide quotas across hotfix releases.
- **Strict versioned JSON and fail-closed writes**: state persists under `StorageKey` (`DefaultReviewPromptKey` = `"liveops.review_prompt"`) with `version: 1` (`ReviewPromptStateVersion`), fixed struct key order, and RFC 3339 UTC timestamps. Decoding uses `DisallowUnknownFields`, rejects trailing tokens, unknown/future versions, negative counters, missing/invalid timestamps, and `lastPromptedVersion` without `lastPromptedAt`. Any corrupt or unrecognised payload—or zero `core.Storage`—fails closed without overwriting stored data. `MaybeAsk` persists `lastPromptedAt` and `lastPromptedVersion` before calling `cx.RequestReview()`, failing closed if `store.SetItem` errors so a storage failure never causes repeated prompts.
- **Platform no-op**: when `cx.Env().CanRequestReview()` is false, `MaybeAsk(cx)` returns `false` without mutating state or calling `RequestReview`, allowing unconditional calls on all targets.
- **Template adoption deferred to `v0.1.1`**: template modules (`template/puzzle2d/template/go.mod.tmpl`) pin the published `github.com/nsgo-dev/game v0.1.0` and CI tests them with `GOWORK=off`, so wiring `ReviewPrompt` into `puzzle2d` waits until a `v0.1.1` release publishes `liveops.ReviewPrompt`.

## Alternatives considered

- **Resetting `sessions` and `firstLaunch` on every `AppVersion` change**: would force long-time players to wait `MinDays` and `MinSessions` again after every release. Preserving `firstLaunch`, `sessions`, and `lastPromptedAt` while resetting `positiveMoments` requires fresh positive gameplay in the new version without discarding lifetime tenure or bypassing Apple's 365-day cap on hotfixes.
- **Clearing negative-moment suppression on the next `MomentPositive`**: a player who fails repeatedly and then wins once in the same session may still be frustrated. Keeping `MomentNegative` active for the rest of the session and clearing it on the next launch (or `RecordSession`) is safer for ratings.
- **Resetting corrupt storage to a fresh state**: wiping corrupt or future-version JSON would allow a downgraded build to erase a newer schema's `lastPromptedAt` and re-prompt inside Apple's quota. Failing closed without overwriting preserves forward-compatibility and never double-prompts.

## Consequences

- **Benefits**: games get a zero-SDK, cross-platform review prompt policy with deterministic JSON persistence, store-compliant defaults, and full headless `nsgotest` coverage (`pkg/online/liveops/review_test.go`).
- **Costs**: a storage entry externally corrupted (or written by a future schema version) keeps `ReviewPrompt` permanently suppressed until storage is cleared or upgraded. Long-running mobile games that stay resident across days must call `RecordSession()` on foreground resume if they want background/foreground transitions to count as distinct sessions.
