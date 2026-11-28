# Agent Note: 2D engine migration guides

Status: implemented

## Problem

Users moving from GameMaker, Construct and RPG Maker had no page that mapped rooms, object events, event sheets, behaviors and the RPG Maker database to this repository's packages, and a newcomer could not tell a missing feature from one under a different name.

## Decision

Each source tool has its own page in `docs/en` and `docs/zh` (`from-gamemaker.md`, `from-construct.md`, `from-rpgmaker.md`) in the shape of `from-godot.md`: an "At a glance" table from the tool's concept to a package, type or built-in scene component that exists on main, with the guide that covers it, and a "First project" paragraph naming the template to start from (`platformer2d` or `rpg2d`) and the editor that replaces the tool's room, layout or map editor.

Rows for features that are not merged say "Not yet" and link the tracking issue instead of naming a package: Construct's motion behaviors (#1751) and input-driven controllers (#1752), and RPG Maker's database of actors, classes, enemies and troops (#1756), map events (#1755) and battles (#1757). When one of those lands, its row changes to the package in both languages.

The pages are linked from the "Coming from another engine" paragraph of `docs/en/index.md` and `docs/zh/index.md`, the README's migration row and the website's `comingFrom` sidebar group in `website/src/content/groups.ts`.

## Alternatives considered

One page for all three tools would shorten navigation, but their vocabularies do not overlap (rooms and alarms, event sheets and behaviors, a database and map events), so a combined table would be three tables under one heading and harder to keep equivalent in two languages. Separate pages match the existing per-engine guides.

## Consequences

A package rename or move must update these six pages along with the other migration guides, and the "Not yet" rows must be revisited when #1751, #1752, #1755, #1756 or #1757 merges. The pages add no compatibility shims or runtime APIs.
