# Agent Note: Steam Deck input kind

Status: implemented

## Problem

Steam Deck controllers were classified as generic or Xbox devices, so prompts could not select a Deck-specific glyph set or identify the handheld consistently.

## Decision

`input.DeviceGamepadSteamDeck` is a gamepad device kind. `DetectGamepadKind` recognizes Steam Deck and Valve handheld controller names before the other brand checks, and its canonical name is `steam_deck`.

## Alternatives considered

Treating Deck as Xbox preserves existing glyphs but cannot distinguish a Deck for keyboard, resolution, or Steam Input integrations. Adding platform detection outside the device name would be unavailable to replayed and synthetic controller tests.

## Consequences

Prompt providers can opt into Deck glyphs without changing generic gamepad behavior. Runtime integrations can use the device kind as a capability hint while still receiving standard gamepad axes and buttons.

Steam keyboard invocation, action-set files, and fullscreen defaults remain platform integration work.

