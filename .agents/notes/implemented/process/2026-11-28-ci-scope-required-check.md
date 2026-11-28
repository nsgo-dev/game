# Agent Note: CI scope and aggregate required check

Status: implemented

## Problem

Every pull request started every heavy test, quality, and example lane, while path-filtered workflows would leave required checks missing when a lane did not run.

## Decision

`internal/ciscope` classifies changed paths as full, mobile, or documentation-only and promotes docs changes when copy-paste command contracts change. Workflows keep their job names and guard expensive steps with the scope output. `Required CI` runs with `always()` and aggregates lane results so branch protection can require one stable check.

## Alternatives considered

Workflow-level path filters would save more runner startup time, but skipped required workflows remain pending and block merges. Per-package test selection would reduce work further, but it needs dependency-aware graphing and is outside this issue.

## Consequences

Documentation edits that change commands still receive the full validation suite. Pure prose changes avoid heavy steps while retaining repository checks. The classifier uses the pull request base when available and falls back to a full scope when it cannot establish a diff.
