# Agent Note: Deterministic turn battle runner

Status: implemented

## Problem

The only repository battle logic lived inside the RPG template and mixed state mutation with presentation. It supported one foe and no reusable turn ordering, scopes or replay checkpoint.

## Decision

`pkg/battle` owns a small deterministic party-and-troop model. It sorts living battlers by agility with stable index ties, validates skill scopes and costs, resolves hit and damage through a seeded random source, emits presentation-neutral events, and exposes immutable-copy snapshots for restore. A caller may supply a damage formula while the default uses attack plus power minus defense with a minimum of one.

## Alternatives considered

Extending the rpg2d template would keep the first game simple but would prevent tactics, card and other games from sharing the rules. Coupling the runner to UI or animation would make replay and headless testing depend on presentation timing.

## Consequences

Games can animate events independently and reproduce seeded outcomes. States, enemy AI, rewards and template migration remain follow-up layers, so this slice establishes the core contract without pretending those systems are implemented.

## Verification

Tests cover stable agility ties, deterministic damage, scope validation, multi-target resolution and snapshot restoration. `go tool nsgo test` and `go tool nsgo check` pass for `pkg/battle`.
