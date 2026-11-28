# Agent Note: Deterministic gameplay cue boundary

Status: implemented

## Problem

Ability and effect state needs a presentation mapping without importing rendering or audio code into deterministic gameplay and headless server builds. Rollback replay also needs one-shot cues to avoid duplicate flashes and sounds, while active cues must be restored from gameplay state.

## Decision

`pkg/cues` owns a strict version 1 YAML mapping from dotted tags to presentation descriptions. `Player` dispatches events through injected `Hooks`, deduplicates one-shots by tag, target, instance and simulation step, and tracks while-active handlers by tag and target. `Reconcile` compares restored active state and starts or stops handlers in input order. The package has no client media dependency, so a server may parse cue data without linking presentation implementations.

The format allows `once` and `whileActive` together. Resource loading, pooling, attachment and actual playback remain host responsibilities until the tag registry, gameplay effects and sound-file formats land.

## Alternatives considered

- Put particle and sound implementations directly in `pkg/cues`. This would make the mapping convenient for one client, but it would pull platform media code into headless builds and make tests non-deterministic.
- Make one-shot suppression wall-clock based. Simulation step and event identity are stable across replay and rollback, while wall-clock timestamps are not.

## Consequences

Cue files can be checked, reviewed and round-tripped independently of a game. Clients must provide hooks and decide how assets attach to targets. The current boundary accepts arbitrary dotted tags and file strings because the shared tag registry, effect format and sound YAML format are separate dependencies. Integrating those validators can tighten checks without changing the dispatch contract.
