### Added

- `particles.Effect` is a version 1 YAML effect file that groups several emitters, each an inline config or a path to an emitter definition with an offset, `delay`, `duration` and `minQuality`, and declares typed `float`, `int`, `color` and `vec2` parameters; `particles.UnmarshalEffectYAML` and `particles.MarshalEffectYAML` read and write it deterministically, and `Effect.SetParam` validates a parameter's new value. Playing an effect and binding its parameters to emitter fields are not part of it yet (#1730).
