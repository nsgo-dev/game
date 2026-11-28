# Agent Note: Steam Deck input kind

Status: implemented

## Problem

Steam Deck controllers were classified as generic gamepads, so prompts could not select a Deck-specific glyph set or identify the handheld (#1765).

## Decision

`input.DeviceGamepadSteamDeck` is a gamepad device kind named `steam_deck`. `DetectGamepadKind` recognizes `steam deck`, `steamdeck` and `valve handheld` in a controller's name before the other brand checks. `PromptTable` resolves it through Deck, then Xbox, then generic, then keyboard prompts, because the Deck's face buttons use the Xbox layout (A south, B east); without its own fallback case the kind fell into the default chain and showed keyboard prompts.

## Alternatives considered

Treating the Deck as Xbox preserves existing glyphs but cannot distinguish a Deck for Deck glyphs or later Steam integrations. Detecting the Deck from the platform rather than the controller name would be unavailable to replayed and synthetic controller tests, and needs `steamworks.Client.RunningOnSteamDeck` (#1763), which does not exist yet.

## Consequences

Prompt tables that have no Deck entries show Xbox prompts on a Deck instead of generic ones. When Steam Input presents the Deck as a virtual Xbox pad, the name says Xbox and the kind stays Xbox. The rest of #1765 (the Steam keyboard for text fields, a 1280x800 full-screen default in `game.yaml`, and `ISteamInput` action sets) is not part of this change.
