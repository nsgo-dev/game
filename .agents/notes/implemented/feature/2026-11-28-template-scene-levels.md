# Agent Note: Editor-openable 2D template levels

Status: implemented

## Problem

The 2D templates only exposed private character-grid data, leaving the editor with no first level to open or inspect.

## Decision

Platformer2D and RPG2D now ship versioned `*.scene.yaml` entry scenes that `game.yaml` autoloads as the main scene, so the editor opens on them. Platformer2D's scene marks the player start, the lamp and the beacon; RPG2D's has one object per person, item and exit of the town. Existing data files remain the runtime source during incremental migration. Each template tests that its scene is in the form `sceneformat.Encode` writes and that every object sits on the cell its data file gives it, so the two copies of a position cannot drift apart while both exist.

## Alternatives considered

Deleting existing grid formats immediately was rejected because gameplay tests and collision rules still depend on them. Generating binary editor artifacts was rejected because repository formats are text and need deterministic review.

## Consequences

New projects open a scene document in the editor that shows where the level's markers are. Editing a position in the scene alone fails the drift test until the data file changes too, because the game does not read the scene yet; tiles are not in the scene at all. Runtime migration of every template's bespoke rules remains follow-up work tied to scene behaviors and spawning issues #1355, #1751 and #1752.
