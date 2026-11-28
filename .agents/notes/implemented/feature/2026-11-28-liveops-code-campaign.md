# Agent Note: Idempotent gift-code campaign policy

Status: implemented

## Problem

Gift and compensation codes need one-use and per-account limits with expiry, while retries must not grant the same reward twice.

## Decision

`CodeCampaign.Redeem` normalizes codes, checks expiry, locks claim state, and records `(account, code)` before returning the reward identifier. Unknown, used, expired and limit failures have distinct sentinel errors.

## Alternatives considered

- **Let each game implement checks around a map lookup**: rejected because retry races can grant twice and error semantics drift.
- **Persist rewards inside this package**: rejected because the economy service owns durable grants and must commit the claim with its reward transaction.

## Consequences

A server can reuse the claim policy and map errors consistently. The current primitive is in-memory and requires a durable caller to persist claims atomically and add request rate limiting.

## Verification

`TestCodeCampaignRedemptionIsIdempotentAndBounded` covers normalization, duplicate claims, per-account limits and expiry. Package checks and changelog/note verification cover the change.
