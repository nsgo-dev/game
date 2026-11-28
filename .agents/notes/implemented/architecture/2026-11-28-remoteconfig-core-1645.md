# Agent Note: remote configuration evaluator

Status: implemented

## Problem

Games had no shared typed defaults, staged activation or stable experiment assignment for remote configuration.

## Decision

Add a core-free evaluator with bool, int, float and string values, ordered percentage overrides, SHA-256 bucketing modulo 10000, and separate staged and active documents.

## Consequences

Clients and servers can share deterministic assignment and change values only at explicit activation boundaries. Fetching, Ed25519 signature verification, YAML loading and HTTP ETags remain host integrations.

## Alternatives considered

Random assignment was rejected because it changes variants between launches. Applying fetched values immediately was rejected because it can change gameplay mid-level.
