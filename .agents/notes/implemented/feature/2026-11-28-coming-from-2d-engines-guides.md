# Agent Note: 2D engine migration guides

Status: implemented

## Problem

Users moving from GameMaker, Construct and RPG Maker lacked a guide that mapped familiar rooms, event sheets, behaviors and RPG data to NSGo packages.

## Decision

Add one English and one Chinese guide for each source engine. Each guide maps the source concepts to existing scene, input, loop, UI, data and save packages and points users to the repository's validation workflow.

## Alternatives considered

Combining all three engines into one page would shorten navigation but make the mappings harder to scan and localize. Separate pages keep each guide focused and parallel the existing `from-godot` and `from-unity` pages.

## Consequences

The guides document current package names and workflows without adding compatibility shims or new runtime APIs. Future package moves require updating both language versions.

