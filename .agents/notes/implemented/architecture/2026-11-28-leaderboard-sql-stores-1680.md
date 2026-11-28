# Agent Note: SQL persistence for self-hosted leaderboards

Status: implemented

## Problem

The self-hosted leaderboard's file store replays all scores into process memory and blindly appends replacements. Multiple service processes can therefore overwrite a better score with an older or lower submission.

## Decision

Add a driver-agnostic `selfhost/sqlstore` backed by `database/sql`, with SQLite and Postgres adapters. The versioned `leaderboard_records` table is keyed by board, period and account. Save runs in a transaction and compares the existing score according to the board order before updating. Postgres migrations and writes use advisory locks, while SQLite uses one immediate writer connection.

## Consequences

The same store contract can be used by the current service and the daemon can select file, SQLite or Postgres persistence. SQL writes preserve the best score under concurrent retries. Ranking queries still use the service's in-process index and remain a follow-up for the full Ranker split described by issue #1680.

## Alternatives considered

- Replacing the existing Store interface with a full Ranker in this change would require moving service query and rotation behavior at once. The compatibility hook keeps existing file and memory stores working while SQL persistence lands.
- Using a process-local mutex would not coordinate replicas. Database transactions and Postgres advisory locks provide the required cross-process serialization.
