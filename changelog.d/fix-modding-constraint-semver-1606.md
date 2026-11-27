### Fixed

- `pkg/modding`: `ParseSemVer`, `ValidateConstraint`, and `SemVer.Compare` enforce SemVer 2.0.0 prerelease identifier syntax and precedence (refusing leading-zero numeric identifiers like `1.0.0-01` and empty identifiers like `1.0.0-a..b`, and ranking non-numeric identifiers such as `1.0.0-x.-1` above numeric ones like `1.0.0-x.5`) with every `mod.yaml` constraint hint example verified against `ValidateConstraint` (#1606).
