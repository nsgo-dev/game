### Added

- `pkg/tooling/playtest`: `playtest.yaml` accepts an optional `baselines` section (`Config.Baselines`) declaring the idle, reference and degraded outcome policies, validated and round-tripped as version 1 YAML and published in the `playtest.v1.json` schema, and `playtest.report.yaml` accepts a matching `baselines` list (`Report.Baselines`). The runner does not execute or gate on the policies yet (#1958).
