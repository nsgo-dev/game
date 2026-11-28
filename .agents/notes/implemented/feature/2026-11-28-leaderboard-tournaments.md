# Agent Note: Deterministic leaderboard tournament core

Status: implemented

## Problem

Self-hosted leaderboards had no join window, entrant or attempt limits, recurring round model or exactly-once round settlement callback.

## Decision

`selfhost.Tournament` models scheduled and recurring rounds in memory. It atomically admits entrants, counts attempts, keeps each account's best score and settles each round once through `OnRoundEnd`. Ranking ties use account ID for deterministic order. Persistence and HTTP adapters can record `ended` rounds using the same round number before delivering rewards.

## Alternatives considered

- Encode tournaments as ordinary boards and track attempts externally. That races submissions and makes recurring settlement difficult to make exactly once.
- Add SQL schema and HTTP routes in this first boundary. The existing leaderboard store migrations are separate work, so the lifecycle semantics land independently for adapters to adopt.

## Consequences

Games can test tournament windows and limits without cron jobs. Deployments need a persistence adapter for restart-safe settlement and should connect the callback to their reward system once storage integration lands.
