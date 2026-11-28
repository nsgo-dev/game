# Agent Note: RPG database YAML

Status: implemented

## Problem

RPG templates each invent their own records for classes, levels, enemies and groups of enemies (rpg2d's `foes:` in `template/rpg2d/template/data/world.yaml`, roguelike2d's experience rule in `template/roguelike2d/template/dungeon.go`), so there is no shared data model, no cross-reference validation and nothing standard for the editor or an agent to edit (#1756).

## Decision

`pkg/rpgdata` defines one versioned YAML file holding classes, actors, skills, enemies and troops. `Parse` decodes with unknown fields refused and a single document, `Validate` checks IDs and references, and `Encode` writes deterministic YAML (struct field order, list order, yaml.v3's sorted map keys for stat curves) so the format passes `internal/formattest`. Every validation error is a `*loaderr.Error` with the field path, such as `troops[0].members[1].enemy`, so the line is reported.

- IDs are unique within each category, not across the file: an actor and its class commonly share a name such as `hero`, and references always say which category they point at.
- Validation runs skills before classes so a class's learned skills can be checked against the skill list in one pass.
- Experience follows the linear rule roguelike2d already uses (level n to n+1 takes n × `levelXP`), and `Progression` returns each level reached so a caller can apply `Class.SkillsLearnedAt` and `Class.StatAt`.

## Alternatives considered

- One file per category, as the issue proposes: rejected for this first step because cross-references between files need a loader that resolves a set of files, which no caller needs yet; one file keeps `Parse` self-contained.
- Reusing `inventory`, `ability` and `stats` types inside the records: rejected because it would couple the file format to runtime APIs before the battle runner (#1757) defines what it needs.
- A single ID namespace across categories: rejected, see above.

## Consequences

This delivers the records, validation and progression only. Skill scopes and formulas are uninterpreted strings, there are no equipment, traits, drops or enemy action patterns, `Progression` has no `Snapshot`/`Restore`, there is no formula-based experience curve (#1364), and rpg2d, roguelike2d and the data table editor do not use the package yet; those remain open on #1756.
