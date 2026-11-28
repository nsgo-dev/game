# Agent Note: Store capture plans

Status: implemented

## Problem

Store screenshots and cover images were captured manually, so dimensions and reproducibility were not reviewable or repeatable.

## Decision

`pkg/tooling/storecapture` owns a versioned YAML plan with named shots, scenes, recordings, fixed-step capture points, and preset or explicit sizes. The CLI `game capture` loads the same plan and writes deterministic PNG outputs, with `-shot` selecting one shot.

## Alternatives considered

Keeping captures as untracked files avoids a new format but cannot rebuild media after an art change. A video-only workflow does not provide deterministic per-store stills.

## Consequences

Plans are reviewable and repeated renders are byte-identical. The renderer currently emits deterministic image output from shot metadata, leaving integration with the upstream movie-mode runtime as a follow-up.
