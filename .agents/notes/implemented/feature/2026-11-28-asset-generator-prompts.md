# Agent Note: Asset generator prompt composition

Status: implemented

## Problem

Asset generator calls ignored the brief's art direction and reference provenance depended on mutable project paths.

## Decision

`ComposeAssetPrompt` combines the person's prompt, the deterministic style summary, and kind-specific framing in a fixed order. `AssetReferenceHash` provides the SHA-256 contract for reference bytes captured at submission time.

## Alternatives considered

Repeating style text manually in each prompt would drift across generator calls. Sending only paths would fail for remote generators and would not identify the bytes used, so the hash helper establishes the immutable payload contract.

## Consequences

Generated prompts are stable and share the brief style anchor. Full MCP image-content transport and source-sidecar schema coordination remain follow-up work with the generator job and source-sidecar issues.
