# Agent Note: Gameplay tags

Status: implemented

## Problem

Inventory, ability, AI and effect systems used unrelated free-form strings for state and category tags.

## Decision

`pkg/tags` defines a strict versioned registry, interns dotted parent tags, tracks reference-counted grants in containers, and evaluates all/any/none queries with parent matching.

## Alternatives considered

Keeping strings avoids a registry but cannot catch typos or share hierarchy semantics. A global mutable tag table would make tests and replays order-dependent, so registries own interned IDs.

## Consequences

Systems can adopt a shared vocabulary incrementally while existing string fields remain compatible. Query names must be resolved once before runtime matching.
