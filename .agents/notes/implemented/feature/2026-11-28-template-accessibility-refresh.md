# Agent Note: Template accessibility refresh contract

Status: implemented

## Problem

Templates expose reusable accessibility APIs but their authoring guidance did not state when to mount the first-launch gate, how to share settings with `a11y.Assist`, or how the same settings path should remain reachable from a pause menu.

## Decision

Template guidance requires `gameui.FirstLaunchSettings` before the first sound, a shared `save.DefaultSettingsStore` for the first-launch gate, `SettingsMenu`, and `a11y.NewAssistFromSettings`, plus a More options route to Accessibility and a keyboard, gamepad, and touch path from pause. The gate enables captions and persists completion before the title screen proceeds.

## Alternatives considered

Leaving each template to invent its own first-launch flow would produce inconsistent captions and settings persistence. Showing the full settings screen before every launch would add friction, so the one-action gate remains the default and the full tab is available on demand.

## Consequences

Template authors have one documented contract for accessible startup and settings wiring. This refresh documents the integration boundary; wiring every independently pinned template module to the newly published package remains dependent on the next game-layer release.
