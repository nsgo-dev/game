# Agent Note: 3D particle simulation selector

Status: implemented

## Problem

The 3D particle format had no way to select a future GPU simulator while retaining CPU fallback on WebGL 2 and other targets without compute.

## Decision

Add an optional `simulation` field to `Emitter3DConfig` and its YAML/JSON format. `cpu` is the default and `gpu` is accepted as a visual-only selector for compute-capable renderers. Unknown values are rejected at load.

## Consequences

One effect definition can express its intended simulator and remain readable by existing CPU paths. The compute simulator, capability negotiation and fallback particle cap remain follow-up work blocked by #1377.

## Alternatives considered

A separate GPU-only file format was rejected because it would duplicate emitter definitions and break cross-target fallback. Silently accepting arbitrary strings was rejected because a typo would select no valid simulator.
