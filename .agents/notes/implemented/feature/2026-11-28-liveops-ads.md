# Agent Note: liveops.Ads mini-game ads policy wrapper

Status: implemented

## Problem

Ads were declared out of scope in #199 because ad networks such as AdMob need proprietary native SDKs. Mini games (WeChat, Douyin) show ads through the host's own JavaScript API instead, with no SDK to embed, and rewarded video is their main revenue source. The bugs every game writes into its own code are the same: granting a reward when the player closed the video early, granting it twice when the close handler fires twice, showing interstitials too often, and leaving the game running or input held under the ad. Issue #1739, part of #1479.

## Decision

`pkg/online/liveops/ads.go` provides `Ads` over a one-method `AdHost` interface (`Show(kind, placement, done func(ended bool, err error)) error`):

- **Reward correctness**: `ShowRewarded` reports `AdRewarded` only when the host passes `ended` true, `AdSkipped` otherwise, and calls the game's callback exactly once per show however many times the host calls `done`.
- **Failure mapping**: a host error wrapping `ErrAdNoFill` reports `AdNoFill`; any other host error reports `AdFailed` and is also returned from the show call.
- **One ad at a time**: a show requested while another is active reports `AdFailed` and returns `ErrAdBusy` without reaching the host, so the first ad's close cannot unpause the game under a second one.
- **Interstitial policy**: `ShowInterstitial` reports `AdCapped` without reaching the host when `Disabled` returns true (for example a bought `noads` entitlement), the per-session `InterstitialCap` is reached, or `InterstitialInterval` has not passed since the last shown interstitial. An interstitial that closes normally reports `AdShown`. A show counts toward the cap and interval once it is handed to the host, including when the host then reports no fill. `Disabled` does not affect rewarded ads, which the player opts into.
- **Pause and input**: `Pause(true)` and `ResetInput` run before the host is asked to show, and `Pause(false)` runs before the result is reported, so a game wires `Pause` to `game.Loop.Paused` and the mixer.

## Alternatives considered

- **Calling WeChat or Douyin APIs from the package**: needs NSGo's mini-game ad API (nsgo-dev/nsgo#2672), which has not shipped, and would make the policy untestable without a host. The `AdHost` interface keeps the policy testable with a fake and lets the mini-game adapter land when NSGo exposes the API.
- **Not counting a no-fill interstitial toward the cap**: hosts count requests in their own frequency limits, and a game retrying after every no-fill would hammer the host; counting the attempt keeps the interval meaningful.

## Consequences

- **Delivered**: the policy wrapper and its tests with a fake host (`pkg/online/liveops/ads_test.go`).
- **Not yet delivered**: the versioned `ads.yaml` with unit IDs and placements, banner ads, a mini-game `AdHost`, reward grants through the server with the show's ID as idempotency key, and analytics or consent wiring. Those remain open under #1739.
- **Host contract**: adapters must pass `ended` accurately; `Ads` cannot detect a host that reports a skipped video as ended.
