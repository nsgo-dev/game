# Agent Note: YAML AI state trees

Status: implemented

## Problem

State-based AI logic existed only as Go-built flow machines, so designers could not author phases and transitions alongside behavior-tree data.

## Decision

`pkg/ai` parses a strict version 1 YAML StateTree with nested states, enter conditions, tasks, completion modes and transition triggers/targets. Validation rejects unknown fields, duplicate state IDs and unsupported trigger names.

## Alternatives considered

Keeping all state machines in Go was rejected because designers and agents cannot review or edit phases as data. Reusing behavior-tree YAML without explicit transitions was rejected because state completion and event routing need distinct semantics.

## Consequences

The AI inspector and a deterministic runtime can consume one validated format. Full stepping, snapshot and binding integration remain follow-up work.
