# Agent Note: Liveops ads configuration

Status: implemented

## Problem

The liveops ads wrapper enforced reward and frequency policy, but placements and platform unit IDs had no versioned file contract.

## Decision

`AdsConfig` defines strict version-1 YAML with per-platform rewarded, interstitial, and banner unit maps plus interstitial interval and session cap. Parsing rejects unknown fields, empty IDs, negative limits, and trailing documents. The existing `Ads` wrapper remains responsible for completion-only rewards, pause/reset hooks, no-fill handling, overlap refusal, and caps.

## Alternatives considered

Keeping unit IDs in Go would require a rebuild for store configuration changes. An untyped map would permit misspelled policy fields. Both were rejected in favor of strict YAML.

## Consequences

Mini-game integrations can load reviewable placement configuration independently from the host adapter. Native ad SDKs and the NSGo mini-game host remain outside this repository.
