# Agent Note: Full player text scale range

Status: implemented

## Problem

The settings model accepted text scales through 300%, but `SettingsMenu` clamped its theme to 135%, so larger choices had no visible effect.

## Decision

The menu applies the validated `save.MinCaptionScale` through `save.MaxCaptionScale` range directly. Screen files and custom widgets consume the same validated setting at construction time, keeping text-scale ownership in the shared settings model.

## Alternatives considered

Raising only the menu's hard-coded upper bound would leave custom HUDs inconsistent. A separate UI scale would duplicate the player preference and would not follow platform font settings.

## Consequences

Large text settings now affect menu typography up to 300%. Layouts with fixed-height rows may need larger viewport space, and screen authors must apply the shared scale when constructing custom text.
