### Added

- `game/scene`: add `Controller.View`, `Manager.View` (`HostOptions`), `Manager.Invalidate`, and the shared `Manager.Actions` and `Manager.KeepInputOnSwitch` fields to switch and stack 2D canvas scenes and 3D `gpu.View` scenes with fade, crossfade, wipe, and custom transitions, keeping mounted 3D views alive under pushed 2D overlays and input wired across switches; a `sound.Mixer` on the Manager's scope keeps music playing through every switch (#126).
