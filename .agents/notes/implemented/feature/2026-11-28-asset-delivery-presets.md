# Agent Note: Asset delivery presets

Status: implemented

## Problem

Export presets had no declarative mapping from asset groups to the main package, subpackages or remote delivery, and no per-preset package caps.

## Decision

`project.Preset.Delivery` stores main, named subpackage, remote group lists and byte caps. Validation rejects empty or duplicate groups, invalid subpackage names and negative caps while preserving presets that omit delivery.

## Alternatives considered

Inferring placement from directory names would couple package layout to source organization. Keeping delivery on each preset supports platform-specific caps and lets one project use different layouts.

## Consequences

Release tooling can consume a validated delivery plan. Unknown groups are not refused yet, because asset groups are still declared only in Go code (#1630 proposes declaring them in YAML), so nothing in `game.yaml` lists the valid names. Laying files out per package in `game release`, failing on a cap, and loading a subpackage through the host in `Manager.LoadGroup` remain follow-up work for #1735.

