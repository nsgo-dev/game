// Package liveops holds provider-agnostic live operations primitives for NSGo games: crash reports, consent-gated analytics, a cloud-save sync interface, in-app purchases credited into the save, local re-engagement reminders, store review prompt policies, and share cards and payload-carrying invites. It links no third-party analytics, crash-reporting, ad, push or payment SDKs.
//
// # Scope (#194)
//
// In scope for the core game module:
//   - Crash reports (#195): panic recovery around game.Loop and arbitrary callbacks, with a fixed-size step ring buffer, stack trace, metadata and an optional state snapshot handed to the app's reporter.
//   - Analytics (#198): a provider-agnostic, strictly opt-in event API whose consent state persists in core.Storage. Zero events are sent or buffered before explicit ConsentGranted.
//   - Cloud saves (#196): the provider-agnostic CloudSync interface, PlatformCloud over NSGo's cloud package (iCloud on macOS/iOS, Google Play Games Saved Games on Android, and the in-memory store in nsgotest), DirCloud for Steam Auto-Cloud and other directory-synced stores, CloudClient (#557) for the self-hosted cloudsave service, and CloudSyncer (#539), which detects slots changed on two devices and resolves them by policy without silently losing either copy.
//   - In-app purchases (#369): Purchases over NSGo's purchase services, which credits consumables into the save exactly once through a Ledger saved with them, keeps unlocks and subscriptions from the store's entitlements, restores them, follows purchases waiting for approval and hands transactions to an optional server before delivering them.
//   - Local reminders (#1310): Reminders over NSGo's core.Scope.ShowNotification, declared in versioned YAML (RemindersConfig), asking permission only on first Schedule or explicit RequestPermission, honoring a persisted player setting and local-time QuietHours, and cancelling or rescheduling active reminders when the app comes to the front.
//   - Store review prompts (#1767): ReviewPrompt over core.Storage and core.Scope.RequestReview, gating in-app rating prompts on minimum sessions, days since first launch, positive moments, cooldown and once-per-version rules, suppressing prompts after negative moments, and failing closed on corrupt state or unsupported platforms.
//   - Share cards and invites (#1738): Invites over a small ShareHost interface (PlatformShareHost for mobile, desktop and web via core.Scope.Share, core.Scope.OnOpenLinks and core.Scope.AppURL, plus MiniGameShareHost ready for NSGo's mini-game share cards in nsgo-dev/nsgo#2672), with bounded canonical key=value payloads, optional HMAC-SHA256 signing and verification, at-most-once cold-launch and warm-resume delivery, and a 5:4 frame snapshot helper.
//
// Pending upstream:
//   - Remote push (#1310): a thin Push wrapper for device-token registration and remote payload routing waits on NSGo shipping remote push (nsgo-dev/nsgo#59) and settling its design decisions (nsgo-dev/nsgo#1323). Until then, remote push is unavailable everywhere.
//
// Out of scope for the core game module:
//   - Ads (#199): ad networks (AdMob, AppLovin and similar) require proprietary native SDKs with store-policy and privacy obligations that do not belong in the core game layer. Games that show ads embed native SDK views directly through NSGo.
package liveops
