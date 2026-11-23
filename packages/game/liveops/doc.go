// Package liveops holds provider-agnostic live operations primitives for NSGo games: crash reports, consent-gated analytics and a cloud-save sync interface. It links no third-party analytics, crash-reporting or ad SDKs.
//
// # Scope (#194)
//
// In scope for the core game module:
//   - Crash reports (#195): panic recovery around game.Loop and arbitrary callbacks, with a fixed-size step ring buffer, stack trace, metadata and an optional state snapshot handed to the app's reporter.
//   - Analytics (#198): a provider-agnostic, strictly opt-in event API whose consent state persists in core.Storage. Zero events are sent or buffered before explicit ConsentGranted.
//   - Cloud saves (#196): the provider-agnostic CloudSync interface, and CloudSyncer (#539), which detects slots changed on two devices and resolves them by policy without silently losing either copy; built-in platform adapters (iCloud, Play Games, Steam Cloud) wait on platform cloud-save bindings.
//
// Out of scope for the core game module:
//   - Ads (#199): ad networks (AdMob, AppLovin and similar) require proprietary native SDKs with store-policy and privacy obligations that do not belong in the core game layer. Games that show ads embed native SDK views directly through NSGo.
package liveops
