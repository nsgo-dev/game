### Added

- `game/pkg/sceneformat`: a scene connection's optional `when` condition, such as `score >= 10` or `has_key`, runs its handler only while it holds, reading the values a game passes to `Signals.SetRuleContext`; loading refuses a condition it cannot parse, and `EvaluateWhen` evaluates one for tools (#1753).
