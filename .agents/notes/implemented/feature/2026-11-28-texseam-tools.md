# Agent Note: Deterministic texture seam and map tools

Status: implemented

## Problem

The renderer consumed normal maps and roughness-related texture data, but the repository had no deterministic way to check tile seams or derive maps from an ordinary image.

## Decision

`pkg/tooling/texseam` provides a ratio-based wrap check with a `1.3` default threshold, feathered edge repair, center-cross merge for edited tiles, luminance height conversion, Sobel-like finite-difference normals, and inverted contrast-stretched roughness. Normal-map output follows the renderer's `(128, 128, 255)` flat-vector convention and exposes explicit wrap or clamp sampling.

## Alternatives considered

Leaving seam repair and map authoring to external image tools would make output vary by platform and would not support headless builds. Asking a generator for separate normal maps would add another nondeterministic call, so maps are derived locally from the same height image.

## Consequences

Texture processing is reproducible and usable without an AI service. The `game tex` command flags, generator texture-kind integration, and material sidecar fields remain follow-up slices coordinated with issues #1748 and #1384.
