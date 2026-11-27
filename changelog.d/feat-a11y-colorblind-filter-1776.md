### Added

- `game/postfx`: `ColorVisionEffect`, `ColorVisionMode`, `ParseColorVisionMode`, and `ColorVisionMatrix` provide GPU Kage and CPU reference color-vision deficiency simulation and daltonization correction passes (#1776).
- `game/a11y`, `game/save`, `game/gameui`: `a11y.ColorVisionFilter`, `a11y.ColorVisionSimulation`, `a11y.SimulateAt`, `a11y.Daltonize`, `a11y.ColorDistance`, and a persisted `ColorBlindSeverity` strength slider in `SettingsMenu` connect the Color-Blind Mode setting to the frame effect pipeline (#1776).
- `game/gametest`: `RenderOptions.ColorVision`, `SimulateColorVision`, `ApplyColorVision`, `AssertColorDistinguishable`, and `AssertRegionsDistinguishable` render goldens under deficiency simulation and verify color separability in headless tests (#1776).
- `game/editor`: 2D scene viewport (Overlays popover) and HUD Layout (**Color Vision & Contrast** panel) preview scenes and HUD screens under Protanopia, Deuteranopia, and Tritanopia with live simulated contrast ratios (#1776).
