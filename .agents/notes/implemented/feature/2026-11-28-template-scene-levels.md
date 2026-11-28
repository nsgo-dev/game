# Agent Note: Editor-openable 2D template levels

Status: implemented

## Problem

The 2D templates only exposed private character-grid data, leaving the editor with no first level to open or inspect.

## Decision

Platformer2D and RPG2D now ship versioned `*.scene.yaml` entry scenes with named terrain, actor, NPC, item and exit objects. Existing data files remain the runtime source during incremental migration, while scene files give the editor a stable, reviewable entry point and canonical scene-format tests.

## Alternatives considered

Deleting existing grid formats immediately was rejected because gameplay tests and collision rules still depend on them. Generating binary editor artifacts was rejected because repository formats are text and need deterministic review.

## Consequences

New projects open meaningful scene documents in the editor and can evolve object placement there. Runtime migration of every template's bespoke rules remains follow-up work tied to scene behaviors and spawning issues #1355, #1751 and #1752.
