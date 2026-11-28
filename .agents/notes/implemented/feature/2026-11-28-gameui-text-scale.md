# Agent Note: Game UI text scale

Status: implemented

## Problem

Game UI text used fixed theme and widget sizes, and `CheckScreen` measured text only at scale 1, so a screen whose localized text overflows at the 200% text size that XAG 101 asks for passed every check (#1772).

## Decision

`Theme.TextScale` and `Root.TextScale` hold one multiplier, clamped to `MinTextScale`–`MaxTextScale` (1–2). It is applied in exactly one place, `resolveFont`, which every text-bearing widget's measure and draw goes through, so the theme's default font and explicit per-widget or screen-file `fontSize` scale the same way and only once. `Root.TextScale` replaces the theme's when it is non-zero; `NewRoot` leaves it zero so a theme's own scale is honored. `ScreenCheckOptions.TextScale` adds a second overflow pass at the larger scale; an overflow already found at the theme's scale is reported once, and one found only at the larger scale names it in its message.

## Alternatives considered

Pre-multiplying `Theme.Font.Size` in `Root.theme` as well as in `resolveFont`, as the first draft did, squared the scale for default-font widgets and left the check measuring explicit `fontSize` unscaled. Scaling only the default theme would leave explicit screen-file font sizes unchanged. Applying a canvas transform would scale controls and hit targets together, changing interaction geometry, so text sizes are scaled at font resolution instead. A new `ScreenIssue` field for the scale was rejected in favor of the message suffix, to keep the issue shape unchanged.

## Consequences

Layouts recompute text metrics at the selected scale while explicit widget bounds and touch targets stay put; `fit_text` nodes grow. The settings menu's Text Scale row, `Env.FontScale()` as the default, a HUD scale and a minimum text size check are not part of this change and remain open under #1772.
