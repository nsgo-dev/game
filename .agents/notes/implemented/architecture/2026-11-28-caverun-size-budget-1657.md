# Agent Note: Cave Run size budget

Status: implemented

## Problem

The quality gate measured only a small size probe. Cave Run's much larger WebAssembly bundle could regress without a download-size signal.

## Decision

Add a dedicated Cave Run WebAssembly budget and a Depot Linux quality job that builds the example with release flags, then invokes `game size` against the budget. Add an Android budget file for the release job to adopt when the shared Android build path is available.

## Consequences

A real game bundle now has an explicit reviewed allowance. The budget notes record the measured baseline and headroom. The existing probe gate remains separate for package-level regressions.

## Alternatives considered

Using the probe budget for Cave Run would hide the game's dependencies and fail immediately. A separate budget keeps the two artifacts' scopes and review decisions independent.
