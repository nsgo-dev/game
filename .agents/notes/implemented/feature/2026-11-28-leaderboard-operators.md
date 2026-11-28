# Agent Note: Leaderboard operator and monthly period boundary

Status: implemented

## Problem

Self-hosted boards only kept a best score and supported all-time, daily and weekly periods. Games could not express set/increment/decrement semantics, attach a subscore or metadata, or run monthly seasons.

## Decision

Board configuration gains an operator enum, metadata byte bound and monthly reset. Records and public entries carry subscore and JSON metadata. Monthly periods use calendar month IDs and boundaries rather than fixed durations. Existing absent fields keep the best operator and old period behavior.

This change lands the config, record and period boundary in the existing selfhost core. Store serialization and full submission operator application remain adapter work so old file and SQL data continue to load while #1680 integration proceeds.

## Alternatives considered

- Encode operators as separate boards. That duplicates ranking configuration and prevents one logical board from changing its update semantics.
- Approximate months as 30-day periods. Calendar-month IDs and `AddDate` preserve short months and match user-visible seasons.

## Consequences

Config loaders can validate monthly seasons and downstream adapters can preserve metadata and subscores. Until store and submit integration adopts the fields, deployments should treat this PR as a compatible format boundary rather than enabling non-best operators against old stores.
