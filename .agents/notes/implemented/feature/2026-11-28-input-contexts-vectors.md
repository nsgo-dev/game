# Agent Note: Input contexts and vectors

Status: implemented

## Problem

Actions had one flat binding map and games repeatedly combined two axes and normalized movement themselves when switching modes.

## Decision

`Actions` exposes named enabled contexts with priorities and `BindVector`/`Vector` for normalized 2D actions. Existing flat bindings remain unchanged when no contexts or vectors are configured.

## Alternatives considered

Replacing the flat map would break saved bindings and callers, so contexts layer alongside it. Normalizing every axis would alter aim and analog semantics, so normalization applies only to declared vectors.

## Consequences

Games can manage mode metadata and vector math through shared APIs. Physical binding consumption and trigger phases remain follow-up work for the full context runtime.
