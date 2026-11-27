### Added

- `game/editor`: Command Palette (`⌘K`) now searches open-scene nodes (`@` scope, selecting and framing on pick), Settings pages, recent items (`PaletteRecent`), command/file scopes (`>` / `#`), and natural-language "Ask the agent" prompts.
- `game/editor`: Scene Tree left rail adds a quick filter (`SceneTreeFilter` / `SetSceneTreeFilterOpen`) and a quick-add `+` menu (`AddSceneRailObject`, `AddSceneRailLayer`) for creating undoable empty objects, triggers, lights, and layers.
- `game/editor`: 2D edit viewport draws the Spec 16×16 neutral dot-grid surround (`ViewportDot`) and neutral markers for visible non-sprite objects, and updates the `Camera · W × H` caption reactively on canvas layout changes (`SubscribeCanvasSize`).

### Changed

- `game/editor`: Migrated all remaining editor screens and panels (HUD Layout, Build & Release, Changes, Playtest Report, Asset Drawer, Asset Generator, Profiler, References/Usages, Merge Conflicts, Cutscene, States, Tileset Editor, Tile Review, Shortcuts, Start Screen swatches, and Viewport Overlays) off dark-only package-level color aliases onto reactive `SpecPalette` tokens with an AST guardrail (`TestSpecNoDarkOnlyPackageColorVarsInEditor`), and localized remaining hardcoded desktop menu items in English and Chinese.

### Fixed

- `game/editor`: Added visible `Text` labels to checkboxes in the Inspector (Flip X/Y, multi-select Flip X/Y, Layer Repeat X/Y), Snap settings popover, Material editor, and Particle editor.
- `game/editor`: Fixed Scene Tree row clipping of lock icons in the 232 px left rail, First Run Connect step text overflowing its card border, initial `Camera · W × H` caption overlap with the top-left overlay legend, and low contrast on the idle `"No tasks"` status bar dot.
