# Agent Note: Object-by-object Git merge driver for scene YAML files

Status: implemented

## Problem

Line-based Git merges fail or corrupt YAML when two branches edit different objects or different fields of the same `*.scene.yaml` file. Even non-overlapping changes to adjacent objects or nested maps collide on context lines, and standard `<<<<<<<` conflict markers produce invalid YAML that neither the engine nor an editor conflict resolver can parse.

## Decision

Implement an identity-based three-way merge engine and a versioned YAML conflict report format in `pkg/sceneformat`, exposed to Git as `go tool game merge` (`internal/gamecli/merge.go`) and registered for `*.scene.yaml` and `*.scene.yml` via `.gitattributes` across every project template plus `go tool game merge -setup` and `game new`.

## Merge contract and conflict format

- **Identity matching**: `sceneformat.Merge(base, ours, theirs)` matches top-level `tilemaps`, `layerOverrides`, and `overrides` by key, `layers` by `Name`, `objects` globally by `ID` across all layers and nesting depths, and `connections` by `(From, Event, To, Handler)` (preserving occurrence indices if a 4-tuple repeats).
- **Parent-relative object placement**: Each object's tree location is tracked as a `parentRef` — either `layer:<name>` for a top-level object or `object:<parentID>` for a child. Moving a parent subtree to another layer never triggers false move conflicts on descendants whose own parent did not change; reparenting conflicts, deleted parents/layers, and combined reparenting cycles (`a -> b -> a`) are detected explicitly.
- **Monotonic schema version merging**: Because the editor raises a scene's `version` only on demand and to different target versions depending on the feature edited (e.g. `2` for instanced overrides, `4` for parallax/3D/environment), `SceneDoc.Version` merges monotonically as `max(base, ours, theirs)` (never dropping below `base`) instead of as a conflicting scalar. Schema versions `1..4` are strictly additive, so the combined document is validated at the resulting version by the post-merge validation gate rather than failing on `scene.version`.
- **Field-by-field and recursive map merging**: Every other field of `SceneDoc`, `CameraDoc`, `BoundsDoc`, `LayerDoc`, `TilemapDoc`, `ObjectDoc`, `SpriteRef`, `LayerOverride`, `ObjectOverride`, and `ConnectionDoc` merges independently, with recursive key-by-key merging for `Components`, `Properties`, and `ConnectionDoc.Args`. A reflection test (`TestMergeCoversAllDocumentFields`) guards against future struct fields being omitted from `Merge`.
- **Validation gate**: After merging, a scene with zero structural conflicts must pass `sceneformat.Validate`; otherwise a `ConflictValidation` conflict is emitted (catching cross-field invariants such as `mesh` + `model`, connections to deleted objects, unknown tilemap refs, or sheared child sprites).
- **Conflict file contract (`ConflictVersion = 1`)**: On conflict, `go tool game merge` writes a strict, versioned, deterministic YAML `ConflictDoc` (`ParseConflict` / `EncodeConflict`, conformance-tested with `formattest.Run` as `sceneformat#conflict`) directly to `<ours>` (Git's `%A`, unless `-conflict-out` overrides it) and exits 1 so Git marks the path unmerged. `Base`, `Ours`, and `Theirs` omit `omitempty` so `0`, `false`, `""`, and `null` (absent/deleted) remain explicit, and whole-number floats preserve `.0` via `floatValue`. Because `ConflictDoc` uses top-level keys (`conflicts`, `merged`, `path`) rejected by `sceneformat.Parse`'s `KnownFields(true)`, an unresolved conflict can never silently load as a scene.
- **Git registration**: Every template under `template/` and `cmd/game/_templates/projects/` ships `.gitattributes` mapping `*.scene.yaml` and `*.scene.yml` to `merge=nsgo-scene`. `game new` configures local `merge.nsgo-scene.*` when the target directory already contains `.git`, and `go tool game merge -setup [dir]` configures local Git config and `.gitattributes` idempotently in any repository.

## Alternatives considered

### Emit standard `<<<<<<<` / `=======` / `>>>>>>>` text conflict markers inside the scene file

Standard text markers work with existing text merge tools without custom parsing. This was rejected because markers inside indented YAML break YAML parsers and prevent the editor's object-level conflict resolver (#1059) and coding agents from inspecting base, ours, and theirs values structurally per object and field.

### Write conflict details to a separate sidecar file beside `%P`

Writing `scenes/level.scene.yaml.conflict.yaml` while leaving `<ours>` unchanged keeps `<ours>` parseable as a scene during a conflict. This was rejected because Git merge drivers own `%A` alone: `git merge --abort` restores `%P` but leaves untracked sidecar files behind, and leaving valid `<ours>` YAML in `%P` risks loading or committing an unmerged scene without noticing the conflict report. Callers that explicitly want a separate report file can pass `-conflict-out`.

### Track each object's location as `(layer, parentID)` instead of `parentRef`

Storing both layer and parent on every object simplifies tree lookup. This was rejected because moving a parent object to a different layer changes every descendant's effective layer, which would cause false `ConflictMove` conflicts whenever the other branch reparents a child of that parent.

## Consequences

- **Benefits**: Divergent branches moving different objects, editing different fields or nested component keys on the same object, or reparenting independently merge cleanly into canonical `sceneformat.Encode` YAML. Real conflicts produce a deterministic, schema-checked YAML report that lists cleanly merged items alongside every conflict's base/ours/theirs values for humans, agents, and the editor conflict UI (#1059).
- **Costs**: Adding a new field to `SceneDoc` or its sub-structs requires updating `merge.go` (enforced by `TestMergeCoversAllDocumentFields`). Git security prevents `.gitattributes` from activating a merge driver automatically on fresh clones, so contributors run `go tool game merge -setup` once per clone (or let `game new` configure it when initializing inside an existing Git repository).

## Verification

Verified with `env -u GOROOT go tool nsgo test ./sceneformat`, `./cmd/game` (including `TestReference`, `TestTemplatesMatchTemplateModules`, and real end-to-end `git merge` tests in `TestMergeEndToEndWithGit`), `.` (`TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable|TestAgentSkillValid`), cross-platform `nsgo check` (`macos,windows,linux,android,web` and `ios`), `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.
