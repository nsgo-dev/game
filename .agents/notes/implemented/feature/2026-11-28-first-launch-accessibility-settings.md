# Agent Note: First-launch accessibility settings

Status: implemented

## Problem

New games need an accessible entry point before their first sound plays, while the full settings menu may not yet be reachable. Templates also need one persistence contract so the prompt does not return after every launch.

## Decision

`pkg/gameui` provides `FirstLaunchSettings`, a small overlay that reads and writes the existing `save.DefaultSettingsStore`. The prompt enables captions when the player continues, preserves other settings, and records completion in `Settings.Custom["first_launch_accessibility_complete"]`. The starter template mounts the overlay at the root and uses the same storage namespace for later settings screens.

The prompt accepts localized copy and an `OnMoreOptions` callback so a template can open its full Accessibility tab. `Required` is derived from persisted settings, which keeps the overlay hidden on later launches without a second store or process-global state.

## Alternatives considered

Using a process-global first-launch boolean was rejected because it disappears on relaunch and cannot be shared by templates. Adding a separate accessibility-intro file was rejected because it would split the player's settings across storage namespaces and complicate migrations. Requiring every template to duplicate the overlay was rejected because the layouts and completion behavior would drift.

## Consequences

Templates gain a reusable first-launch contract and tests can assert the exact TestIDs for the prompt and its one-action Continue button. The completion marker is an additional custom settings key, so older settings files remain readable and unknown custom keys continue to round-trip. The current starter template demonstrates the integration. Other templates can mount the same component while wiring their own settings menu and localized text.
