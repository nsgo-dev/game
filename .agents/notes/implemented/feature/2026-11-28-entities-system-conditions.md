# Agent Note: Entity system conditions

Status: implemented

## Problem

Every registered entity system ran every step, forcing state checks inside system bodies and obscuring whether a system actually executed.

## Decision

`RunIf` is a scheduler option storing a world predicate. The predicate is evaluated immediately before the system in resolved order, and false predicates skip only that system.

## Alternatives considered

Removing systems at runtime would invalidate ordering and allocation guarantees. Keeping conditions in the system body duplicates guards and hides scheduler behavior from tests and diagnostics.

## Consequences

Existing systems remain unconditional. Conditional systems keep deterministic order and zero scheduler allocations per step.
