# Agent Note: Entity simulation LOD

Status: implemented

## Problem

Crowd systems ran every entity at full rate even when far from every viewer, and a boundary could oscillate between levels.

## Decision

`pkg/entities` exposes a serializable `LOD` component shape, distance bands, nearest-viewer calculation and hysteresis-aware resolution. The helpers are deterministic and leave scheduler ordering and component storage under the game's control.

## Alternatives considered

Implicitly applying LOD to every system was rejected because replication and broadphase systems must still see every entity. Random level changes were rejected because replays require stable results.

## Consequences

Games can opt systems into distance LOD incrementally and preserve fixed-step replay behavior. Query filters, caps and scheduler time slicing remain follow-up work.
