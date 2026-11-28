# Agent Note: YAML map event interpreter

Status: implemented

## Problem

Map events such as one-time chests, signs and touch triggers were hard-coded by each template. Flow had no text format or resumable command list.

## Decision

`pkg/flow` parses strict version 1 map-event YAML with ordered pages and four trigger names. `EventRunner` executes a small typed command set on fixed updates and exposes a serializable `EventSnapshot` for mid-event saves. Unknown commands and malformed pages fail during parsing.

## Alternatives considered

Keeping event commands in Go was rejected because editors and mods cannot inspect or author them. Reusing dialogue scripts was rejected because map events need page conditions and self switches, while dialogue has different lifetime and presentation semantics.

## Consequences

Templates can adopt deterministic data-driven events incrementally, and saves can resume waits and command positions. The current command vocabulary is intentionally small, with game-specific commands and variable/switch integration remaining follow-up work tied to #1754 and #1752.
