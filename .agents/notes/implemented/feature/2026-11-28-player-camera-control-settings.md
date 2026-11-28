# Agent Note: Player camera and control settings

Status: implemented

## Problem

`pkg/input` has per-binding sensitivity, dead zone and inversion, and `pkg/scene3d` cameras take a vertical field of view, but none of it is a player setting: `save.Settings` had no controls or camera section, the settings menu's Controls tab only rebinds actions, and templates hard-code turn speed, mouse sensitivity and field of view. Each game reinvents the setting, the menu rows and the wiring, and the Game Accessibility Guidelines' basic sensitivity and haptics options are missing (#1780).

## Decision

`save.Settings` carries two new sections, `Controls` (`save.ControlSettings`) and `Camera` (`save.CameraSettings`), dispatched through `SettingsTargets.ApplyControls` and `SettingsTargets.ApplyCamera`. The packages that act on them consume `save` types, so `save` stays free of `input` and `scene3d`.

### Settings format

- `controls`: `mouse_sensitivity`, `stick_sensitivity`, `touch_sensitivity`, `gyro_sensitivity` (multipliers in [`save.MinLookSensitivity`, `save.MaxLookSensitivity`] = [0.1, 5], default 1, 0 reads as 1), `invert_x`, `invert_y`, `stick_dead_zone` ([0, `save.MaxStickDeadZone`] = [0, 0.95], default 0.15, 0 means off), `trigger_threshold` ([0, 1], default 0.5), `vibration` ([0, 1], default 1, 0 means off).
- `camera`: `field_of_view` (vertical, degrees, [`save.MinFieldOfView`, `save.MaxFieldOfView`] = [40, 120], default 90), `camera_bob`, `camera_sway` ([0, 1], default 1), `motion_blur`.
- Every field is always written, in struct order, so a saved file is deterministic and shows the player's full choice. The settings store unmarshals onto `DefaultSettings()`, so a file written before these sections existed loads the defaults; no envelope version bump is needed.
- Zero is a real player choice for the dead zone, vibration, bob and sway and must survive `Validate`; only the sensitivities and field of view treat 0 as unset, because 0 would freeze the camera or collapse the projection.

### Applying look settings

`input.ApplyControlSettings(actions, s)` stores the preferences on `Actions`, and they are applied as each stick, pointer and motion axis is read: the binding's declared sensitivity is multiplied by the device's look sensitivity, the player's inversion flips (XORs) the declared `Invert`, and the player's dead zone replaces stick bindings' (0 maps to the "off" value -1 that `gamepadAxisValue` expects). The bindings' own options are never rewritten, so:

- a game's declared sensitivity, such as radians per pixel for mouselook, keeps its units and 1 means "as the game ships";
- `Actions.Save` keeps persisting the game's values rather than baking the player's settings into the bindings file;
- applying again replaces the previous preferences instead of compounding them, and bindings made or loaded after `ApplyControlSettings` follow it too.

Vertical axes are `AxisLeftY`/`AxisRightY`, `PointerY`, `TiltY` and `GyroX` (pitch is rotation around the screen X axis); `TiltX`, `GyroY` and `GyroZ` are horizontal; `TiltZ` is left alone. Touch has no binding, so camera code reads `input.LookScale(s, input.LookTouch)`. `TriggerThreshold` is stored for games that read analog triggers themselves; `pkg/input` treats triggers as buttons today.

### Vibration

`Haptics.SetVibrationScale` keeps an unexported attenuation (1 minus the scale) so a zero-value or `NewHaptics` instance plays at full strength. `PlayPattern`, and through it `Rumble`, multiply step strengths by the scale; at 0 nothing is emitted and an active rumble stops. `input.ApplyControlSettingsToHaptics` bridges the setting because `Actions` does not own a `Haptics`.

### Cameras and menu

`scene3d.Camera.ApplyCameraSettings` converts the field of view to a perspective camera's `FOVY` in radians and copies `MotionBlur`, which `Camera.Lerp` carries over from the target camera so interpolated draws keep it. `FPSController` and `ThirdPersonController` store `CameraBob` and `CameraSway` for the game's own bob and sway code; the controllers do not bob or sway themselves. `gameui.SettingsMenu` adds the nine look and vibration rows after the motor-assist rows and before **Reset Controls to Defaults**, and the four camera rows only with `SettingsMenuConfig.ShowCameraSettings`, which a game with a 3D camera sets. Slider values round to two decimals before they are stored, and `ResetControls` restores both sections.

## Alternatives considered

- Rewrite each binding's options in place: simplest, and it matches the issue's wording "updates every gamepad axis binding's options", but it replaces a declared mouse sensitivity of 0.003 rad/px with 1, compounds or loses state on a second apply or a later `BindGamepadAxis`, and leaks player settings into `Actions.Save`.
- Write only non-default fields (a custom `MarshalJSON` per section): keeps the payload small for tight storage quotas, but a map-based encoder that treats 0 as "default" drops explicit zero choices, and a partial section hides what the player picked from a reviewer. The full section costs about 300 bytes.
- Keep sensitivity in each template: no settings format change, but every game duplicates validation and non-template games get no player setting.
- An exported `Haptics.VibrationScale` field: direct, but a `Haptics` built as a struct literal would read 0 and go silent.

## Consequences

- Games wire two more targets; `docs/en` and `docs/zh` `save.md`, `input.md`, `gameui.md` and `scene3d.md` show how.
- The settings file grows by the two sections; `TestSettingsFailedWriteLeavesPreviousIntactAndDeleteIsolated` raises its storage quota fixture from 600 to 1200 bytes to fit them.
- The field of view range [40, 120] is fixed in `save`; a game that needs a narrower range clamps in its `ApplyCamera` target.
- Templates (fps3d, action3d, racing3d) do not use the settings yet; that wiring is separate work under #1780.
- Tests cover validation, a store round trip with zero choices, legacy files, axis output under the settings, idempotent re-apply, haptics scaling, camera projection and `Lerp`, and the menu rows' layout, apply, save and reset.
