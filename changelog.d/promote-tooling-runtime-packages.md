### Changed

- `ktx2`, `importsettings`, `datatable` and `jsonschema` move out of `tooling`, since a release build links their code: import `github.com/nsgo-dev/game/pkg/ktx2`, `github.com/nsgo-dev/game/pkg/importsettings`, `github.com/nsgo-dev/game/pkg/datatable` and `github.com/nsgo-dev/game/pkg/jsonschema` instead of `github.com/nsgo-dev/game/pkg/tooling/ktx2`, `github.com/nsgo-dev/game/pkg/tooling/importsettings`, `github.com/nsgo-dev/game/pkg/tooling/datatable` and `github.com/nsgo-dev/game/pkg/tooling/jsonschema`.
- The File Formats page's entries for import settings and data tables are now `importsettings`, `importsettings#source` and `datatable`.
