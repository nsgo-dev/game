# Agent Note: Player control and camera settings

Status: implemented

## Problem

Player settings had no shared representation for look sensitivity, stick filtering, inversion, vibration strength, or camera presentation preferences. Games therefore had to keep these values in template-specific constants.

## Decision

`save.Settings` stores validated `ControlSettings` and `CameraSettings` sections. The sections use stable JSON names, preserve defaults when loading older files, and compact default values when saving. `input.ApplyControlSettings` updates existing gamepad axis bindings in place, while `input.Haptics.SetVibrationScale` applies the player vibration preference to rumble and impact output.

## Alternatives considered

Keeping the values in each template avoids changing the settings schema, but duplicates accessibility behavior and prevents a settings store from applying preferences consistently. Adding a dependency from `save` to `input` would create a package cycle, so the save package exposes data and the input package consumes it.

## Consequences

Existing settings files continue to load with defaults for the new sections, and default saves stay small enough for existing storage quotas. Gamepad bindings can be changed after applying settings, so applying a snapshot does not remove a game's binding-specific configuration. Camera controllers still need to opt into `CameraSettings` through their own targets.

