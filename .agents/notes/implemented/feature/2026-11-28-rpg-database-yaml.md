# Agent Note: RPG database YAML

Status: implemented

## Problem

RPG templates had separate records for classes, actors, skills, enemies and troops, leaving no shared data model or cross-reference validation.

## Decision

`pkg/rpgdata` defines one versioned YAML database with strict decoding, deterministic encoding, ID and reference validation, and a small `Progression` value for experience and level-up events.

## Alternatives considered

Keeping records in each template avoids a new package but repeats schema and validation. Reusing runtime inventory and ability types directly would couple the file format to unrelated runtime APIs, so the database stays engine-agnostic.

## Consequences

Templates can adopt common RPG records incrementally. The current package provides core records and progression while battle execution and editor integration remain follow-up work.
