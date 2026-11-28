# Agent Note: One-switch menu scanning

Status: implemented

## Problem

Menus required directional navigation and a separate confirmation action, which prevented players using one switch from reaching settings and other game UI.

## Decision

`gameui.SpatialNavigator` exposes `ScanOptions`, `SetScanOptions`, `Step`, and `ScanSelect`. The fixed-step scanner advances the root focus order at a validated interval, skips disabled widgets through the existing navigator, and pauses after a configured number of completed passes. `a11y.AccessibleMenu` accepts equivalent scan properties and schedules the same deterministic interval through the scope clock, while preserving keyboard and screen-reader activation paths.

## Alternatives considered

Native platform switch access was not sufficient because game-canvas widgets are not native accessibility nodes on every target.

## Consequences

Games can opt into one-switch scanning without changing widget definitions. The scanner adds a timer callback for native menus and requires the game loop to call `SpatialNavigator.Step` for gameui screens.
