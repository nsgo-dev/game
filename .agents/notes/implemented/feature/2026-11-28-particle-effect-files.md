# Agent Note: Particle effect files

Status: implemented

## Problem

Multi-emitter effects and runtime variation required several scene objects and Go-built configs, preventing designers from tuning a complete effect as one asset.

## Decision

`particles.Effect` groups inline or file-backed emitters and declares typed parameters. Strict version 1 parse/encode keeps the file deterministic, while `SetParam` validates runtime overrides.

## Alternatives considered

Keeping one emitter per scene object was rejected because timing and authoring drift across objects. Using untyped maps for parameters was rejected because misspelled or incompatible values would fail only during play.

## Consequences

Particle authoring gains a text-first effect container and runtime parameter boundary. Event delivery, culling and editor preview integration remain follow-up work.
