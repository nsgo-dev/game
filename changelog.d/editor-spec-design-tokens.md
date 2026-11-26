### Added

- `game/editor`: added Spec typography, spacing, radius, height, border, and reduced-motion constants (`SpecFont*`, `SpecSpace*`, `SpecRadius*`, `SpecRowHeight`, `SpecButtonHeight`, `SpecHeaderHeight`, `SpecMotion*`, `SpecMotionDuration`, `SpecAgentPulseOpacity`), 3D axis tokens (`AxisX`, `AxisY`, `AxisZ`) from `Viewport3D.dc.html` in `spec_theme.yaml`, an AST test refusing `core.RGB`/`core.RGBA` and hardcoded UI hex color literals outside `spec_theme.go`, a Design tokens section in `docs/en/editor.md` and `docs/zh/editor.md`, and the Spec editor PR acceptance checklist in `.github/PULL_REQUEST_TEMPLATE.md` (#1002).

### Changed

- `game/editor`: replaced all hardcoded UI `core.RGB`/`core.RGBA` and hex color literals across the editor's panels, 2D/3D viewports, gizmos, handles, and debug overlays with `SpecPalette` tokens from `spec_theme.yaml`, and updated the toolbar scene title to Spec `14 / 600` (`SpecFontTitleSize`, `SpecFontTitleWeight`) (#1002).
