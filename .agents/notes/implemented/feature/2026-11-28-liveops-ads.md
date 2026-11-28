# Agent Note: Mini-game ads wrapper

Status: implemented

## Problem

Mini-game host ad APIs differ, and game code often grants rewarded prizes on skipped videos or shows interstitials too frequently.

## Decision

`liveops.Ads` wraps an `AdHost`, reports rewards only when playback ends, guarantees one callback per show, and enforces disabled, cap and interval policies for interstitials. Pause and input-reset hooks surround each show.

## Alternatives considered

Embedding WeChat or Douyin APIs in the package would prevent testing and require platform SDKs. A small host interface keeps the policy portable and lets adapters own host-specific callbacks.

## Consequences

Host adapters must invoke their completion callback once and pass the ended flag accurately. Reward grants and analytics remain caller concerns, so games can use their own idempotency and consent systems.

