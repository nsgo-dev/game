# Agent Note: Social friends and blocks

Status: implemented

## Problem

Accounts had no shared friend edges or block state for parties, chat and friend leaderboard queries.

## Decision

`social.Graph` stores symmetric edge states, converts mutual invites to friends, prevents blocked interactions, supports limits and returns deterministic friend listings.

## Alternatives considered

Letting clients submit friend lists cannot enforce blocks or mutual acceptance. Per-feature lists duplicate edge state and drift between chat, parties and leaderboards.

## Consequences

The in-memory graph provides the lifecycle contract for SQL and HTTP adapters. Account identity verification and notification delivery remain service integrations.
