# Agent Note: Key and gamepad bindings in game.yaml and editor Input Map panel

Status: implemented

## Problem

`game.yaml`'s `input` section previously declared only `touchRegion`, `mouseButton`, and `mouseRegion` bindings alongside `axes` and `motionAxes`, even though `packages/game/input` reads keyboard keys (`input.Key`), physical key positions (`input.KeyCode`), gamepad buttons (`input.GamepadButton`), and analog gamepad sticks (`Actions.BindGamepadAxis`). Additionally, the editor had no visual panel to author `game.yaml`'s input map, capture keys, lay out touch/mouse regions over the project's `display` size, or keep axis references valid when renaming an action.

## Decision

Extend `packages/game/project` and publish `game.v1.json` via `cmd/gameschema`, then add an Input Map panel in `packages/game/editor/input_map_panel.go`:

- **`game.yaml` input bindings and gamepad stick axes**: Add `KindKey` (`key`), `KindKeyCode` (`keyCode`), and `KindGamepadButton` (`gamepadButton`) to `project.Binding` (with `Key` and `Code` fields alongside `Button` and `Region`), and add `GamepadAxes []project.GamepadAxis` (`gamepadAxes`) to `project.Input` with `leftX`/`leftY`/`rightX`/`rightY`, `deadZone`, `axial`, `sensitivity`, and `invert`. Every binding kind refuses fields belonging to other kinds; `BindInput` applies all of them to `*input.Actions`; and `project.File` is registered in `cmd/gameschema` to publish `apps/website/public/schemas/game.v1.json` (`project.SchemaURL`) without changing `project.Encode`'s headerless canonical YAML so existing `game.yaml` files remain byte-for-byte compatible.
- **Editor Input Map panel over `EditProject`**: Mount `InputMapPanelView` below the viewport behind a toolbar **Input** toggle (`toolbar-input-map`), matching `AudioMixerPanelView`'s docking pattern ([audio bus mixer](2026-11-25-editor-audio-bus-mixer.md)). Every edit (`AddInputAction`, `RenameInputAction`, `DeleteInputAction`, `AddInputBinding`, `SetInputBinding`, `RemoveInputBinding`, `SetInputRegion`, and axis/motion/gamepad axis CRUD) executes through `Editor.EditProject`, so `project.Validate` guards every change and Undo/Redo works uniformly.
- **Atomic action rename propagation**: `RenameInputAction` updates the action's name and every `Axis.Negative` / `Axis.Positive` matching the old name inside one `EditProject` transaction, so renaming an action never leaves dangling axis references and a single Undo restores both.
- **Key capture with focused `OnKeyDown` and window-level fallback**: `BeginKeyCapture` focuses a capture banner view (`OnKeyDown`) and also watches `core.KeysDown` / `core.KeyCodesDown` (ignoring keys held at capture start) so pressing any key binds it reliably in both interactive desktop windows and headless tests.
- **Interactive region canvas over `display` size**: `DrawInputRegionCanvas` scales `Project.Display` (falling back to `Project.Window`) onto the panel's canvas and renders all `touchRegion` and `mouseRegion` rectangles. `BeginRegionDrag` / `UpdateRegionDrag` / `EndRegionDrag` support moving existing regions, resizing via the bottom-right handle, and drawing new regions on empty space, committing one `EditProject` command on drag end.

## Alternatives considered

- **Using snake_case names (`gamepad_button`, `dpad_left`, `left_x`) in `game.yaml` to match `input/storage.go`'s JSON save format** — Would reuse `input`'s internal string tables directly. Rejected because `game.yaml` consistently uses camelCase across every section (`touchRegion`, `mouseButton`, `motionAxes`, `tiltX`, `deadZone`, `maxRange`); mixing snake_case into `game.yaml` would break consistency for authors and schema users.
- **Prepending `# yaml-language-server: $schema=...` in `project.Encode`** — Would automatically attach `game.v1.json` on every save, as `sceneformat.Encode` does. Rejected because all existing `game.yaml` fixtures, examples, and `docs/{en,zh}/formats.md` canonical examples begin with `version: 1`; keeping `project.Encode` headerless preserves backwards compatibility while still publishing `game.v1.json` for editor schema mapping.
- **Silently deleting or clearing axes when deleting a referenced action** — Would allow deleting an action even if an axis uses it. Rejected because silently dropping axes risks unintended data loss; refusing the delete through `project.Validate` surfaces the exact axis name so the author can update or delete the axis explicitly.

## Consequences

- **Benefits**:
  - Games can declare keyboard, physical key-code, gamepad button, and analog stick bindings alongside touch/mouse regions and motion axes in `game.yaml` and bind all of them with one `BindInput` call.
  - Editors and AI tools get a generated `game.v1.json` schema checked in CI by `cmd/gameschema -check`.
  - Authors can visually manage actions, capture keys, draw/drag/resize touch and mouse regions against the virtual display resolution, and edit all three axis kinds with full Undo/Redo and atomic rename propagation.
- **Costs and known limits**:
  - `project.Encode` does not emit a `# yaml-language-server` header automatically; authors who want IDE validation map `game.yaml` to `https://game.nsgo.dev/schemas/game.v1.json` in editor settings or add the comment manually.
  - Gamepad button bindings are chosen from a dropdown rather than live controller capture in the panel, since editor sessions may not have a physical gamepad attached.

## Verification

- `(cd packages/game && env -u GOROOT go run ./cmd/gameschema -check)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./project/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./input/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./editor/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./cmd/gameschema/...)`
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestFormatPackagesRunConformance|TestFormatsPage|TestStabilityTable|TestAgentSkillValid')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
