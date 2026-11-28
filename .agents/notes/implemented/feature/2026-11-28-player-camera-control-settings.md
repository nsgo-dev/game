# Agent Note: Player camera and control settings

Status: implemented

## Problem

The input package exposed sensitivity and dead-zone primitives, while player settings and the settings menu could not persist or apply them. 3D cameras also had no shared contract for field of view or motion preferences.

## Decision

`save.Settings` owns validated `ControlSettings` and `CameraSettings`. `input.ApplyControlSettings` updates existing gamepad, pointer, and motion bindings in place, and a separate helper applies vibration scale. `scene3d.Camera` and camera controllers accept the validated camera section, converting degrees to the renderer's radians. `gameui.SettingsMenu` exposes these values after the existing motor-assist rows so existing focus order remains stable.

## Alternatives considered

Keeping sensitivity in each template avoids extending the settings format, but duplicates validation and leaves non-template games without a player setting. A renderer-only camera option would not apply to input bindings, so the settings are dispatched through `save.SettingsTargets` instead.

## Consequences

Older settings files load with defaults for the new sections. The JSON payload is larger and callers must connect `ApplyControls` and `ApplyCamera` to their runtime systems. Tests cover round trips, clamping, menu layout, binding updates, haptics, and camera projection updates.
