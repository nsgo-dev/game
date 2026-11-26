# Agent Note: Dockable, resizable editor panels with per-user saved layout

Status: implemented

## Problem

`EditorView` previously placed the Scene tree, Layers, Asset Browser, Files, Library, Viewport, Inspector (and right-rail views), plugin panels, and bottom panels (Problems, Output, Profiler, Animation, States, Tileset, Terrain, 2D Rig, Particles, Material, Scene Environment, Data Table, Search, Localization, 3D NavMesh) at fixed widths and heights in a static flexbox tree. As new panels and plugins accumulated, the workspace could not be resized, tabbed, hidden, or rearranged, and users had no per-user layout persistence outside project repositories.

## Decision

1. **`ui.DockArea` workspace with a two-layer layout model (`packages/game/editor/dock_layout.go`, `packages/game/editor/dock_ui.go`)**:
   - `EditorView` renders the workspace through NSGo's `ui.DockArea` inside a single `dragdrop.Area` so asset and model drags from docked panels onto the viewport keep working across dock regions.
   - `Editor` maintains a user layout **blueprint** (`dockLayoutState.blueprint` + `hidden` set) separate from the currently active `dock.Layout` projected for `ui.DockArea`. Projecting from the blueprint filters out currently unavailable contextual panels (e.g. `files`/`problems` without an open project, `animation` without an open clip, or inactive plugin panels) and user-hidden panels while remembering their slots across edits and sessions.
   - Every docked panel renders inside an absolutely-positioned fill wrapper (`Top: 0, Right: 0, Bottom: 0, Left: 0`) with `dockedPanelKey` set so `ui.ScrollView` content inside panels never inflates `base/dock`'s `FlexGrow: 1` wrappers past the window height. Standalone calls to panel views outside `EditorView` keep their fallback dimensions.
   - The default blueprint aligns with the AI-first main layout (`#1007`): `Left` opens at `232pt` hosting `PanelSceneTree` (`leftRailSceneView`) and auto-expands cleanly when auxiliary panels (`Layers`, `Assets`, `Files`, `Library`) are opened on demand until user-resized; `Inspector` (hosting the `380pt` right rail with Agent, Inspector, Tasks, History, Problem Fix, and Tile Review views) sits in a horizontal split beside `Viewport` in `Center` (default `380pt`, adapting for narrower right-rail views until user-resized), and `DockRight` plugin panels split beside it on first use. Auxiliary panels opened and moved by the user persist in `layout.yaml` and restore their open state and saved active tab index on restart. Because `nsgo@v0.3.0/base/dock`'s `leaves()` refuses dragging or closing the sole visible panel of a region, keeping `Inspector` and `DockRight` plugin panels alongside `Viewport` in `Center` guarantees every panel (including `Inspector` and single plugin panels) is immediately mouse-draggable, tabbable, resizable, and closable while also giving the `Bottom` dock wider horizontal space.
   - Before the user manually resizes the `Bottom` dock or loads a saved layout, `Bottom` adapts to the tallest active bottom panel's preferred height (`140pt` for compact panels like Problems/Output, `284–404pt` for Animation, Profiler, and States); once resized by the user or loaded from disk, the user's saved height takes precedence. `states_ui.go` scales its state-machine graph to fit the laid-out `states-graph` bounds as the dock resizes.
2. **Strict versioned per-user YAML layout format (`editor#layout`)**:
   - `LayoutFile` (`LayoutVersion = 1`) stores `version`, `center`, optional `left`/`right`/`bottom` regions (`size`, `closed`, `root`), and sorted `hidden` panel keys in `<UserConfigDir>/nsgo-game/layout.yaml` (`DefaultLayoutPath`), outside any project directory.
   - `ParseLayout` enforces `KnownFields(true)` and rejects missing/zero/future `version`, duplicate keys, trailing YAML documents, mixed or empty nodes, invalid axes, out-of-range active tab indices, duplicate panel keys, non-finite/negative sizes, and hiding `viewport`. `EncodeLayout` emits deterministic 2-space YAML verified by `formattest.Run` (`editor#layout` in `docs/en/formats.md` and `docs/zh/formats.md`, registered in `packages/game/formats_test.go`).
   - On restore (`SetLayoutPath` / `LoadLayout`), panel keys that no longer exist in the editor (unknown built-in keys or unregistered plugins) are dropped from `DockLayout` and omitted on the next save. Registered plugin panels appear at their declared `Panel.Dock` on first use and at their saved location afterwards.
   - `Editor.LayoutPath` defaults to empty in `editor.New` so headless tests never touch the developer's host config directory; `cmd/game/editor.go` and `editor/editorcmd/editorcmd.go` wire `DefaultLayoutPath()` for interactive sessions.
3. **Menus and commands (`packages/game/editor/menubar.go`, `packages/game/editor/commands.go`)**:
   - Preserving the existing top-level desktop menus (`File`, `Edit`, `View`, `Project`, `Help`), `View > Panels` exposes checkable show/hide items for every built-in panel and active plugin panel plus **Reset Layout**, alongside `View > Reset Layout` and the `CmdResetLayout` (`view.resetLayout`) command in the Command Palette.
   - No buttons were added to the toolbar so the default `1120×680` layout preserves its toolbar height and keeps `file-browser` rows visible without scrolling.

## Alternatives considered

- **Placing `Inspector` alone in `dock.Right` by default**: Matches the nominal right-dock name, but `nsgo@v0.3.0`'s `base/dock` `leaves()` returns `false` when a region has only one group with one visible panel, which would prevent dragging `Inspector` out of `Right` onto `Files` with the mouse and prevent closing it from its tab button, and would also narrow `Bottom` to only the viewport's width (`404pt` at `1120×680`). Placing `Inspector` in a horizontal split beside `Viewport` in `Center` avoids both issues while `LayoutFile` and `ui.DockArea` still fully support `right` when populated.
- **Persisting `dock.Layout`'s built-in gpui-base JSON (`Layout.MarshalJSON`)**: Would avoid writing a YAML codec, but violates the repository rule in `AGENTS.md` that editor-defined formats are readable, strictly validated, versioned YAML checked by `formattest.Run` and documented on `docs/{en,zh}/formats.md`.
- **Adding a top-level `Window` menu or toolbar layout buttons**: Would break the top-level desktop menu bar contract tested in `commands_test.go` and risk wrapping the toolbar onto an extra row at `1120×680`, pushing `file-browser` rows off-screen. Putting panel toggles and reset under `View > Panels` and `View > Reset Layout` preserves both constraints.

## Consequences

- **Benefits**:
  - Every built-in and plugin panel can be resized, dragged, tabbed, shown/hidden from `View > Panels`, reset to default, and restored across restarts from `<UserConfigDir>/nsgo-game/layout.yaml`.
  - All 444 existing editor tests continue to pass alongside new conformance and mounted UI tests covering tab-dragging, plugin panel persistence, menu toggling, resize/reset, and unknown-panel pruning.
- **Costs and known limits**:
  - `nsgo@v0.3.0`'s `base/dock` still disallows dragging out the sole remaining visible panel of a dock region (`Left`/`Right`/`Bottom`) via pointer drag (programmatic `MovePanel` and `View > Panels` toggles still work); lifting that upstream restriction requires a change in `nsgo`.
  - The removed internal `plugin-dock-{left,right,bottom}` wrapper TestIDs are superseded by `ui.DockArea` and the documented `plugin-panel-<plugin>-<id>` container TestIDs.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor ./editor/editorcmd ./cmd/game)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios ./editor ./editor/editorcmd)`
- `(cd packages/game && env -u GOROOT TMPDIR=/tmp go tool nsgo test ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./editor/editorcmd)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./cmd/game)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -run TestFormatsPage)`
- `env -u GOROOT go run ./tools/changelog check`
- `bun run verify-notes`
