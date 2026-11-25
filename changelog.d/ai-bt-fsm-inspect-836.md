### Added

- `game/ai`, `game/flow`, `game/debug`: add optional behavior tree node names (`ai.Named`, preserved from `*.bt.yaml`) and zero-allocation read-only tree inspection (`ai.BTInspector`, `WalkInto`, `RunningPathInto`, `FormatBTTrace`), bounded transition history and `MachineInspector` snapshots/formatting on `flow.Machine`, and `-tags release`-stripped AI inspector overlay panels and steering debug draw helpers (`debug.AISnapshot`, `Overlay.SetAI`, `Overlay.DrawSteering`) (#836).
