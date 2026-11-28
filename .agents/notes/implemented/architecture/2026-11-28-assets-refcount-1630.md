# Agent Note: asset reference-count cache

Status: implemented

## Problem

Asset groups had no reusable primitive for sharing one decoded value while multiple owners held it.

## Decision

Add `assets.RefCache`, keyed by URI, returning idempotent release handles. A value is loaded once and its close callback runs after the last release.

## Consequences

Manager integrations can share resource lifetime across groups without duplicating decoded values. Group YAML parsing, manager integration and stats attribution remain follow-up work.

## Alternatives considered

Copying values per group was rejected because it increases memory and creates release-order gaps. Uncoordinated close callbacks were rejected because one group could invalidate another group's asset.
