### Added

- `pkg/project`: an export preset's `delivery` places asset groups in the main package, named subpackages or the remote origin and sets byte caps for the main package and all packages together; `Parse` refuses a group in two places, an empty group name, a subpackage name with spaces and a negative cap (#1735).
