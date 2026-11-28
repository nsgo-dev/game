# Agent Note: App Store account token derivation

Status: implemented

## Problem

Purchases required a UUID account token while accounts use opaque hex IDs, leaving requests difficult to associate with a player.

## Decision

`accounts.AppAccountToken` derives a stable UUID v5-shaped value from a domain-separated SHA-256 digest of the account ID. It contains no secret and is deterministic across devices and services.

## Consequences

Purchase requests can carry a stable account correlation token without exposing the account ID format. Full App Store JWS verification, certificate roots, notifications and promotional offer signing remain follow-up work.

## Alternatives considered

Random UUIDs were rejected because the server could not reproduce them after restart. Sending the raw account ID was rejected because StoreKit expects a UUID token and the ID format is an internal contract.
