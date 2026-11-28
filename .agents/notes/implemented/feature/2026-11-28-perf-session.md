# Agent Note: Release-safe performance session summary

Status: implemented

## Problem

The debug overlay and GC monitor are stripped from release builds, leaving no compact frame percentiles, jank count, memory peak or GC summary that a shipped game can record.

## Decision

`pkg/perf/session` provides a fixed-size recorder independent of build tags. `Observe` stores frame durations in a 4096-sample ring, counts frames at least twice the target interval, tracks longest frame and reads runtime metrics for heap, memory, GC cycles and pause histograms. `Summary` computes deterministic p50/p95/p99 values from the retained samples.

## Alternatives considered

- Reuse `pkg/tooling/gamedebug` in release builds. That links the overlay-oriented package and keeps its no-op release contract, so it cannot provide field metrics.
- Allocate a slice for every frame. A fixed ring avoids steady-state allocations in a hot path.

## Consequences

Games can ship a small runtime recorder and send summaries through their own consent-gated analytics. The package reports runtime metrics available on the target, and game teams still need to establish device-specific baselines.
