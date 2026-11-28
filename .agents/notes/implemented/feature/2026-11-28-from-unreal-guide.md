# Agent Note: Coming from Unreal Engine guide

Status: implemented

## Problem

Unreal developers had migration guides for Godot, Ebitengine and Unity but no guide mapping levels, actors, Blueprints, data assets, Enhanced Input, GAS, AI, Niagara, UMG, replication, saves and packaging to this layer.

## Decision

Keep equivalent `docs/en/from-unreal.md` and `docs/zh/from-unreal.md` pages with an at-a-glance table followed by detailed mappings. Link the guide from both language indexes and the website's coming-from group. Describe Unreal concepts at the API level and link to the corresponding package or format page.

## Alternatives considered

Adding Unreal terminology to the existing Unity guide would obscure both audiences and weaken search navigation. A separate guide keeps the migration vocabulary explicit and parallels the existing engine guides.

## Consequences

Readers can find package mappings from either language and from the website catalog. The guide documents where this layer differs, such as Go and YAML replacing visual Blueprints and right-handed metre units replacing Unreal's centimetres.

## Verification

The bilingual pages, indexes and website group contain the same `from-unreal` slug. Documentation and website checks are the validation path for this content-only change.
