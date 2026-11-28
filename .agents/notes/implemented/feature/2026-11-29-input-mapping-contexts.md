# Agent Note: Input mapping contexts and vectors

Status: implemented

## Problem

Games with menus, vehicles and gameplay had to bind and unbind individual actions manually. Flat bindings also let multiple modes react to the same physical input, and digital movement vectors exceeded unit length.

## Decision

`input.Contexts` stores named binding sets with priorities and active stack state. `Binding` returns the highest-priority active binding for an action, so mode contexts consume inputs deterministically. `NormalizeVector` clamps 2D action vectors to unit length without changing shorter vectors.

## Alternatives considered

Keeping one flat map forces every game to implement mode precedence and risks stale bindings. Normalizing only in each movement system duplicates a rule that applies to every vector action.

## Consequences

Games can switch control modes with explicit context ownership and share normalized movement behavior. Full trigger phases, persistence schema and editor context UI remain follow-up integration layers.

## Verification

Tests cover priority consumption, fallback after popping a context and vector normalization. `go tool nsgo test` and `go tool nsgo check` pass for `pkg/input`.
