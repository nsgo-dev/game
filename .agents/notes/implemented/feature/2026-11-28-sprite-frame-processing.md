# Agent Note: Deterministic sprite frame processing

Status: implemented

## Problem

Raw rendered frames often contain a keyed background, inconsistent transparent bounds and a duplicate terminal frame. Feeding those files directly to an atlas creates visible loop pops and moving feet anchors, while each project reimplements cleanup differently.

## Decision

`pkg/tooling/spriteframes` owns pure-Go frame processing. It copies input pixels, applies deterministic RGB-distance chroma removal with optional softness, despill and one-pixel hole filling, rotates a sequence at its quietest adjacent transition, removes a duplicate end frame, and aligns non-transparent bounds to a shared bottom-center canvas. The `game sprite frames` command accepts sorted PNG directories and GIFs, packs the aligned frames with `atlaspack`, and writes a canonical `resource.Animation` YAML clip.

## Alternatives considered

Keeping this in `game pack` would reuse atlas flags but would make raw-frame cleanup unavailable to library callers and would couple frame sequencing to atlas directory scanning. Video decoding was considered but would add platform codecs and is outside deterministic asset-build tooling, so PNG and GIF are the supported inputs.

## Consequences

The output is reproducible and can be tested without a renderer. The processing API is intentionally small and currently supports the bottom-center anchor required by sprite-frame workflows. More anchor modes or advanced matte morphology can be added without changing atlas metadata.

## Verification

Synthetic tests cover chroma removal and hole filling, duplicate-frame seam rotation, and shared anchor dimensions. CLI reference generation and package tests run through `go tool nsgo`.
