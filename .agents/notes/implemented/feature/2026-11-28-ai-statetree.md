# Agent Note: YAML AI state trees

Status: implemented

## Problem

State-based AI logic existed only as Go-built flow machines, so designers could not author phases and transitions alongside behavior-tree data.

## Decision

`pkg/ai` parses and canonically encodes a strict version 1 YAML StateTree with nested states, enter conditions, tasks, completion modes and transition triggers/targets. Validation rejects unknown fields, trailing documents, duplicate state IDs, malformed conditions, negative delays and unsupported versions. `LoadStateTreeFS` provides the asset-filesystem entry point.

## Alternatives considered

Keeping all state machines in Go was rejected because designers and agents cannot review or edit phases as data. Reusing behavior-tree YAML without explicit transitions was rejected because state completion and event routing need distinct semantics.

## Consequences

The AI inspector and a deterministic runtime can consume one validated format. Tests cover round trips, schema failures, nested trees, filesystem loading and fuzz parsing. Full stepping, snapshot, binding and inspector integration remain follow-up work, so this slice does not claim the issue's runtime acceptance cases.
