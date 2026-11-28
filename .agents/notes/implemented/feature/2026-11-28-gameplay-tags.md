# Agent Note: Hierarchical gameplay tag registry

Status: implemented

## Problem

Gameplay packages exchanged unvalidated strings for statuses, categories and ability conditions. Misspelled tags were invisible, parent categories could not be queried, and multiple systems granting the same tag could remove one another's state.

## Decision

`pkg/tags` stores a strict version 1 YAML registry and interns declared names in a `Registry`. Dotted parent names are implied in memory, so files stay concise while `Container.Has` walks parent links. Containers count each grant and remove one reference at a time. `Query` evaluates all, any and none predicates, with nested composition available through `AllOf`, `AnyOf` and `NoneOf`. Name-based data is resolved with `Registry.QueryNames`, which rejects unknown tags before gameplay starts.

The registry preserves declaration order for explicit tags and derives missing parent IDs deterministically from first appearance. Matching uses the container's existing maps and does not allocate.

## Alternatives considered

- Require every parent to be listed in YAML. That would make parent relationships obvious, but repeats boilerplate and makes a rename easy to leave inconsistent.
- Keep string slices on each gameplay object. That preserves simple serialization, but cannot provide interned identity, reference-counted grants or allocation-free parent matching.

## Consequences

Tags can be shared by abilities, effects, cues and editor tooling after those packages adopt the registry. Current integrations must convert names through `Resolve` and keep a registry on their containers. The package does not yet alter existing inventory, cards or AI string fields, so older files continue to load while migration can happen incrementally.
