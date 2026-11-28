# Agent Note: Wider entity queries

Status: implemented

## Problem

Entity systems reading four components had to iterate three and perform sparse lookups for the fourth in their hot loop.

## Decision

`Each4` drives the smallest of four sparse stores and checks the other memberships in O(1), preserving deterministic dense order and pointer validity rules of existing queries.

## Alternatives considered

Reflection-based arbitrary tuples would reduce generated code but add runtime overhead and allocations to a hot ECS path. A generated fixed-width API matches the existing Each1–Each3 contract.

## Consequences

Systems can read four components without manual lookups. Optional components and typed resources remain separate additions to avoid changing existing snapshot contracts in this focused increment.
