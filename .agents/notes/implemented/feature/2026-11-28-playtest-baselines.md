# Agent Note: Playtest baseline declarations

Status: implemented

## Problem

Playtest configurations had metrics but no shared contract for skilled reference wins, degraded routes, or no-input outcomes.

## Decision

Version 1 `playtest.yaml` accepts an optional `baselines` section with idle outcome and duration, reference minimum win rate, and degraded reaction/drop parameters with win-rate limits. Reports accept a deterministic `baselines` block. Duration and numeric ranges are validated, and unknown fields remain refused by the strict YAML decoder.

## Alternatives considered

Introducing a new schema version would force every existing template to migrate for an optional section, so the additive fields remain in version 1. Keeping policy only in hand-written template tests would avoid format changes but would preserve inconsistent reporting and gates.

## Consequences

Existing playtests remain valid. The declaration and report data model is ready for the idle/degraded runner profiles, while gameplay-specific runner execution remains a follow-up integration with the dependent bot-profile work.
