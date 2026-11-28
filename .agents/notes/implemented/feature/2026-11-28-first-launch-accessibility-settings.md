# Agent Note: First-launch accessibility settings

Status: implemented

## Problem

New games need an accessible entry point before their first sound plays, while the full settings menu may not yet be reachable. Templates also need one persistence contract so the prompt does not return after every launch.

## Decision

`pkg/gameui` provides `FirstLaunchSettings`, a small overlay that reads and writes the existing `save.DefaultSettingsStore`. The prompt enables captions when the player continues, preserves other settings, and records completion in `Settings.Custom["first_launch_accessibility_complete"]`. Templates build against the game module version their `go.mod.tmpl` pins, not this checkout, so a template cannot mount the overlay until a release containing it is pinned; the template integration follows in a separate change after that release.

The prompt accepts localized copy and an `OnMoreOptions` callback so a template can open its full Accessibility tab. `Required` is derived from persisted settings, which keeps the overlay hidden on later launches without a second store or process-global state. `View` wraps the overlay in a `core.Show` driven by a signal that `Complete` clears, so a mounted prompt unmounts itself on Continue instead of relying on the caller to rebuild its tree; the More options button is only rendered when `OnMoreOptions` is set, so the prompt never offers a control that does nothing.

## Alternatives considered

Using a process-global first-launch boolean was rejected because it disappears on relaunch and cannot be shared by templates. Adding a separate accessibility-intro file was rejected because it would split the player's settings across storage namespaces and complicate migrations. Requiring every template to duplicate the overlay was rejected because the layouts and completion behavior would drift.

## Consequences

Templates gain a reusable first-launch contract and tests can assert the exact TestIDs for the prompt and its one-action Continue button. The completion marker is an additional custom settings key, so older settings files remain readable and unknown custom keys continue to round-trip. Templates mount the same component, with their own settings menu and localized text, once a release that contains it is pinned.
