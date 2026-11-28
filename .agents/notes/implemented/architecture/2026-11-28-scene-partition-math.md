# Agent Note: Deterministic scene partition math

Status: implemented

## Problem

Large scenes need stable cell assignment and hysteresis before background loading or data-layer runtime integration can be added. Assigning by object origin misplaces objects that cross cell boundaries, and one radius causes load/unload thrashing.

## Decision

`sceneformat.Partition` validates cell size and load/unload radii, assigns objects to every touched cell by bounds, keeps `AlwaysLoaded` objects outside the grid, and computes deterministic row-major `Needed` and `Retained` cells for multiple streaming sources with radius scaling. The larger retained radius prevents boundary churn.

## Alternatives considered

Origin-only assignment is cheaper but breaks long bridges and large objects. A single radius simplifies configuration but thrashes when a source sits on a boundary.

## Consequences

Build and runtime streamers can share the same cell math and produce reproducible generated output. Scene-file partition declarations, background instancing and data-layer state remain follow-up layers.

## Verification

Existing sceneformat partition tests cover bounds spanning cells, always-loaded objects, source ordering, radius hysteresis and validation. Targeted tests and `go tool nsgo check` pass.
