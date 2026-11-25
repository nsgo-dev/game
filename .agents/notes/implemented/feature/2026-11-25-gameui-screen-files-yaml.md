# Agent Note: Game UI *.screen.yaml format, runtime builder, checks, and reference tracking

Status: implemented

## Problem

`gameui` previously built HUDs and menus only from Go code (`Panel`, `VBox`, `HBox`, `Label`, `Button`, `Viewport`, `SpatialNavigator`). That left no plain-text, reviewable format for human authors, coding agents, or the planned HUD layout editor (#1047) to edit HUDs and menus with anchors, safe areas, touch targets, device-class conditions, focus order, and localization keys, and no automated `game check` or `refs`/`game mv` coverage for broken touch sizes, localized text overflows (especially wide CJK glyphs), missing locale keys, low text contrast, or renamed sprites and text keys.

## Decision

Add a versioned `*.screen.yaml` format (`ScreenFormatVersion = 1`, published schema `https://game.nsgo.dev/schemas/screen.v1.json`) directly in `packages/game/gameui`, paired with a runtime builder (`BuildScreen`), a shared static checker (`CheckScreen`), `tooling/assetcheck` integration (`game check`), and `refs` / `game mv` tracking.

### Format and schema

- `ScreenDoc` (`packages/game/gameui/screen_format.go`) holds `version`, optional `uid` and `name`, positive `designWidth` and `designHeight`, optional `scaleMode`, and a recursive `nodes` tree supporting all nine node kinds (`vbox`, `hbox`, `label`, `button`, `sprite_row`, `color_rect`, `touch_stick`, `touch_button`, `container`).
- Every node supports `id`, `type`, `anchor` (9 points or `fill`), `offset` (positive `x`/`y` inset inward from right/bottom edges, matching `docs/design/editor/HUDLayout.dc.html`), `safeArea` (defaults to `true` for non-fill anchors and `false` for `fill`), `sizeMode` (`fixed` or `fit_text`), `width`, `height`, `padding`, `spacing`, `textKey`, `focusOrder`, `touchHitSize`, `deviceClasses` (`phone`, `tablet`, `desktop`, `ultrawide`), `sprite`/`spriteFrame`/`spriteCount`, `fontSize`, `textColor`, `background`, `cornerRadius`, and `children`.
- `ParseScreen` enforces `KnownFields(true)`, rejects trailing documents, YAML anchors/aliases/merge keys, duplicate node IDs, duplicate positive focus orders, invalid enums/colors, non-finite numbers, and children on leaf nodes, wrapping errors with `loaderr.Locate`.
- `EncodeScreen` writes canonical 2-space YAML prefixed with `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/screen.v1.json`, dropping zero `offset`/`padding` structs and sorting `deviceClasses` in canonical order (`phone`, `tablet`, `desktop`, `ultrawide`) for deterministic lossless round-trips checked by `formattest.Run`.
- `validScreenUID` validates 13-character lowercase Crockford base-32 UIDs inline in `gameui` rather than importing `packages/game/resource`, because `resource/theme.go` imports `gameui`.

### Runtime building, layout, and focus

- New `gameui` widgets (`ColorRect`, `SpriteRow`, `TouchStick`, `TouchButton`, and `ScreenCanvas`) implement `Widget` directly so `BuildScreen` returns a standard `*Root` and `*SpatialNavigator` (wrapped in `*BuiltScreen`).
- `ScreenCanvas` lays out children against full bounds or `SafeArea`-inset bounds; unsized `container` nodes act as pass-through groups that propagate `SafeArea` to their anchored children while applying `deviceClasses` visibility filtering.
- `Root.syncFocusables` stably orders widgets implementing an unexported `focusOrder() int` interface ahead of unordered widgets, and `BuildScreen` wires cyclic `Up`/`Down` `NavOverrides` on `SpatialNavigator` for explicit `focusOrder` nodes so both `Tab`/`Shift+Tab` and D-pad/arrow navigation follow declared order.
- `TouchButton`, `TouchStick`, and `Button` expand pointer hit-testing to `touchHitSize` when larger than visual bounds.

### Checks and references

- `CheckScreen` (`packages/game/gameui/screen_check.go`) is exported from `gameui` so both `tooling/assetcheck` (`game check`) and the upcoming editor HUD screen (#1047) share identical diagnostics: touch targets under `44 pt` on phones and tablets, text overflow across the four reference profiles (`Phone 19.5:9 (notch)`, `Tablet 4:3`, `Desktop 16:9`, `Ultrawide 21:9`) using CJK-aware `MeasureScreenText` and real `BuildScreen` layout, missing `textKey` entries per project locale, and text under `4.5:1` WCAG contrast (`a11y.ContrastRatio`) against its fill.
- `refs.Scan` recognizes `*.screen.yaml`/`*.screen.yml` (`KindScreen`) and `l10n` YAML string tables (`KindStringTable`), recording `sprite` paths in `Project.Refs` (`FieldScreenSprite`, rewritten in place by `PlanMove` / `game mv`) and localization keys in `Project.TextKeys` (`FieldTextKey`, `FieldStringKey`, `FieldPluralKey`), with `refs.PlanRenameTextKey` and `game mv -text-key <old> <new>` rewriting keys in place across screens and locale tables without collisions.

## Alternatives considered

1. **Creating a separate `packages/game/screenformat` package.** Separating the YAML types from `gameui` would mirror `sceneformat`/`scene`, but `BuildScreen` and `CheckScreen` tightly depend on `gameui.Root`, `Viewport`, `SpatialNavigator`, and the new touch/anchored widgets, while `gameui` is already experimental and has no stable callers. Keeping `*.screen.yaml` in `gameui` avoids a second package and keeps one import path for runtime and editor callers.
2. **Checking text overflow with a standalone width formula instead of `BuildScreen` layout.** A flat width check would miss text nodes sized by parent `vbox`/`hbox` containers or constrained by safe-area widths on notched phones. Running `BuildScreen` + `Layout` per `(locale, reference device)` catches both fixed-width and container/safe-area overflows while staying fast and matching runtime geometry exactly.
3. **Treating `textKey` as a file `Ref` in `refs.Project.Refs`.** `Project.Refs` represents file-path references checked for filesystem existence by `Project.Check`, `assetcheck.checkRefs`, and `PlanMove`. Mixing non-path string keys into `Refs` would require special-casing every existing consumer. Adding `Project.TextKeys` (`TextKeyRef`) and `PlanRenameTextKey` keeps file-reference semantics clean while giving `refs` and `game mv -text-key` full in-place refactoring over screen files and `l10n` string tables.

## Consequences

- **Benefits:** HUDs and menus are plain-text, schema-validated, diff-friendly YAML files; games load them directly into `gameui.Root` with safe-area placement, viewport scaling, and declared focus order; `game check` catches CJK overflow, small touch targets, missing translations, and low contrast in CI with file, line, and node; `game mv` safely updates sprite paths and text keys in place; and #1047 can reuse `CheckScreen`, `ReferenceDevicesFor`, and `BuildScreen` directly.
- **Costs:** `gameui` now depends on `l10n`, `a11y`, `loaderr`, and `go.yaml.in/yaml/v3` (all compatible with `gameui`'s experimental status). Text overflow checks use deterministic CJK-aware glyph estimates (`MeasureScreenText`) rather than native font shaping so `game check` runs headlessly and identically across platforms; games using unusually wide custom fonts should supply a `Theme.TextMeasurer` when calling `CheckScreen` directly.
