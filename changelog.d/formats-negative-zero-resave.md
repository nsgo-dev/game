### Fixed

- `game/tooling/hotreload`, `game/resource`, `game/project`, and other YAML format encoders: normalize IEEE `-0.0` floats to `0` on parse and encode so saving a loaded file twice produces identical bytes without rewriting `-0` to `0` (#1605).
