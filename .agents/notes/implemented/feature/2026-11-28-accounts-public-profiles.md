# Agent Note: Public account profile directory

Status: implemented

## Problem

Account records exposed only private email and identity data. Games had no shared username, display name or avatar directory and repeatedly implemented unsafe name claims in unrelated services.

## Decision

`accounts.PublicDirectory` stores only public profile fields and atomically indexes normalized usernames. NFKC normalization and case folding happen before uniqueness checks. `Users` accepts batches of IDs or usernames and returns copies without private account fields. Username cooldown and a server-provided name filter are explicit policy hooks. The directory accepts a clock for deterministic tests and can be replaced by a persistent adapter.

This change keeps the existing `Store` interface and service schema stable while providing the public-profile contract. Persistence migration and HTTP endpoints can adopt the same types in a follow-up without changing lookup semantics.

## Alternatives considered

- Put public fields directly into `Account` and every account store. That would require a coordinated SQLite/Postgres migration and alter private record semantics before the public contract was settled.
- Let each leaderboard or chat service keep names. That duplicates uniqueness rules and allows inconsistent names across social surfaces.

## Consequences

Games can claim and batch-read public profiles without exposing email or identities. The current core is in-memory, so deployments that need restart persistence must provide an adapter and migrate storage before enabling it in production.
