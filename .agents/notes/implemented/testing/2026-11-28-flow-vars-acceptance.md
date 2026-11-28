# Agent Note: Shared flow variable acceptance coverage

Status: implemented

## Problem

The shared `flow.Vars` implementation already supported declarations, bounds, change listeners, snapshots and dialogue integration, but the acceptance suite did not explicitly exercise expression assignment at the shared-store boundary.

## Decision

Keep the existing `Vars` contract and add a focused regression test for `SetFromExpr`. It verifies `$` normalization, integer conversion, range enforcement and the undeclared-variable error. Documentation now points callers to the same authoritative store and checkpoint APIs.

## Alternatives considered

Changing the runtime implementation would expand a working API for an acceptance gap. A full RPG template migration would couple this refresh to unrelated template behavior, so the test and docs slice is sufficient.

## Consequences

Future expression evaluators have an explicit test guarding type and range semantics. The broader scene-rule and RPG-template migration remains separate work.

## Verification

`go tool nsgo test ./pkg/flow -- -run TestVarsSetFromExprKeepsDeclaredKindAndBounds` and `go tool nsgo check ./pkg/flow` pass.
