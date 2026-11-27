// Package liveops holds provider-agnostic live operations primitives for NSGo games: crash reports, consent-gated analytics, a cloud-save sync interface and in-app purchases credited into the save. It links no third-party analytics, crash-reporting, ad or payment SDKs.
//
// # Scope (#194)
//
// In scope for the core game module:
//   - Crash reports (#195): panic recovery around game.Loop and arbitrary callbacks, with a fixed-size step ring buffer, stack trace, metadata and an optional state snapshot handed to the app's reporter.
//   - Analytics (#198): a provider-agnostic, strictly opt-in event API whose consent state persists in core.Storage. Zero events are sent or buffered before explicit ConsentGranted.
//   - Cloud saves (#196): the provider-agnostic CloudSync interface, PlatformCloud over NSGo's cloud package (iCloud on macOS/iOS, Google Play Games Saved Games on Android, and the in-memory store in nsgotest), DirCloud for Steam Auto-Cloud and other directory-synced stores, CloudClient (#557) for the self-hosted cloudsave service, and CloudSyncer (#539), which detects slots changed on two devices and resolves them by policy without silently losing either copy.
//   - In-app purchases (#369): Purchases over NSGo's purchase services, which credits consumables into the save exactly once through a Ledger saved with them, keeps unlocks and subscriptions from the store's entitlements, restores them, follows purchases waiting for approval and hands transactions to an optional server before delivering them.
//
// Out of scope for the core game module:
//   - Ads (#199): ad networks (AdMob, AppLovin and similar) require proprietary native SDKs with store-policy and privacy obligations that do not belong in the core game layer. Games that show ads embed native SDK views directly through NSGo.
package liveops
