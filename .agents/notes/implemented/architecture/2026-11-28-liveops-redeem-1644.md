# Agent Note: redeem code core

Status: implemented

## Problem

Promo and compensation codes lacked a shared concurrency-safe redemption primitive with typed expiry and usage errors.

## Decision

Add `liveops/redeem.Store` with normalized codes, expiry, total-use and per-account limits. A mutex makes one-time redemption atomic and returns the reward identifier for an economy idempotency call.

## Consequences

An economy service can layer persistent campaign storage, rate limiting and reward grants over a deterministic core. News, inbox delivery and durable database adapters remain follow-up work.

## Alternatives considered

A check-then-increment API was rejected because concurrent retries could redeem a one-time code twice. Case-sensitive codes were rejected to avoid avoidable support failures.
