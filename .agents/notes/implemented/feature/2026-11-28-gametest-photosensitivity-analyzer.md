# Agent Note: gametest photosensitivity analyzer

Status: implemented

## Problem

Flash limiting protects runtime effects, but test suites had no deterministic way to detect a synthetic strobe or a high-contrast stripe pattern in rendered frames before release.

## Decision

`gametest.AnalyzePhotosensitivity` accepts a frame sequence and optional frame rate. It reports repeated full-frame luminance changes exceeding the rolling three-per-second threshold and frame-local high-contrast stripe coverage. The analyzer is pure and does not depend on wall-clock state, so synthetic sequences can be tested reliably.

## Alternatives considered

Checking only runtime `postfx.Flash` calls misses flashes baked into sprites, video, or custom drawing. A recorder-specific command would duplicate the image analysis and leave ordinary rendered-frame tests uncovered.

## Consequences

Games can add a photosensitivity assertion to frame-based tests and receive a failure kind and frame range. This slice does not yet wire replay files or a `game check flashes` command into the CLI.
