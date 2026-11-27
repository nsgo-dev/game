# Agent Note: AI-first editor main layout with scene rail, Agent/Inspector tabs, and status bar

Status: implemented

## Problem

The previous editor shell placed over twenty tool buttons, panel toggles, and secondary controls across a multi-group top toolbar and exposed docked Files, Assets, Layers, and Library panels by default. That layout left too little space for the viewport and the AI Agent workflow, lacked designed empty, loading, and scene-open error states (failing to open a missing or invalid scene left an ambiguous blank canvas), and mixed legacy control sizing and toggle semantics instead of the `docs/design/editor/Main.dc.html` and `docs/design/editor/Spec.dc.html` contract.

## Decision

The editor main window (`packages/game/editor/main_layout_ui.go`, `ui.go`, `agent_ui.go`, `overlays_ui.go`, `scenetree.go`) adopts the five-region AI-first chrome specified in `Main.dc.html` and `Spec.dc.html` while keeping every secondary editor view reachable via commands, menus, Settings, and the status bar.

### Layout and region contract

- **44 px header (`mainHeaderView`)**: `<project> / <scene>` breadcrumb with a scene File menu and unsaved dot (`Label: "unsaved changes"`), centered icon-only tool `base.RadioGroup` (`Select V`, `Move W`, `Rotate E`, `Scale R`, `Pan Space` using 1.8 px@15 px `currentColor` Lucide strokes with `"Label · key"` tooltips), `2D / 3D` segmented radio group, `History (⌘Y)` icon button, `Search ⌘K` Command Palette trigger, and `Play (⌘P)` as the single primary button.
- **232 px left rail (`leftRailSceneView`)**: `Scene / Assets` switch, `SCENE · N nodes` header, scene tree rows showing kind dots, kind tags, and `NEW` badges for pending-review proposed objects (`IsObjectProposedNew`), and a bottom `FILES` section listing the active scene, prefabs/instanced scenes referenced by the open scene (`SceneUsedPrefabs`), and `game.yaml`. Selecting a scene tree row switches the right rail to **Inspector**.
- **Viewport (`ViewportView`, `viewportCameraCaptionView`, `viewportOverlayBarView`)**: camera frame caption (`Camera · W × H`), bottom bar (`Zoom out`, zoom `%`, `Zoom in`, `Snap` `base.Switch` with `RoleSwitch`, `Fit`, `Overlays N`), and top-left overlay legend slot.
- **380 px right panel (`RightRailView`, `AgentPanelView`)**: `Agent` (`⌘1`) and `Inspector` (`⌘2`) radio tabs paired with the live MCP connection chip (`Connected · MCP` and all five states). The header uses `MinHeight: 44` with `Wrap: core.Wrap` so single-line states stay 44 px while the multi-button `Disconnected` state wraps cleanly without clipping inside the 380 px rail.
- **32 px status bar (`mainStatusBarView`)**: muted text on `bg` displaying the active layer button (toggling Layers), live 2D pointer coordinates (`SubscribePointer`), current tool, problem count pill (opening Problems), Output, and Debugger.
- **Keyboard and Spec controls**: builds directly on [focus rings and F6 regions](./2026-11-26-editor-focus-ring-and-regions.md) and [Spec light/dark theme](./2026-11-26-editor-spec-light-theme.md). Every interactive button/radio/switch keeps a hit area of at least `28 × 28`, accessible names, and 2 px Spec focus rings. `bindShortcuts` deliberately skips registering a root-scope `"space"` key listener for `CmdToolPan` because `viewport.go` handles Space-hold panning via `core.KeyHeld(cx, "space")` and a root `"space"` handler would intercept Space before focused buttons, switches, and radios can activate.

### Workspace states

`Editor.WorkspaceState()` classifies the workspace into `WorkspaceReady`, `WorkspaceNoProject`, `WorkspaceNoScene`, `WorkspaceLoading` (`SetProjectLoading`), and `WorkspaceSceneError` (`SceneOpenError`, `RetryOpenScene`, `ClearSceneOpenError`). `OpenFile` and `OpenScene` record structured `*loaderr.Error` metadata (file path, line, column, problem, fix) on failure so the left rail, viewport (`RoleAlert` card with Retry and Dismiss), inspector, and status bar render designed recovery states instead of a blank viewport.

## Alternatives considered

- **Keeping all secondary panel buttons in a collapsible overflow toolbar**: rejected because the design contract reserves the 44 px header exclusively for breadcrumb, core manipulation tools, 2D/3D, History, Search, and Play; Command Palette (`⌘K`), Menu bar, Settings, and Status bar already provide keyboard-first discovery for every secondary panel (`CmdLayersPanel`, `CmdFileBrowser`, `CmdAssetBrowser`, `CmdLibraryPanel`, `CmdAudioMixer`, `CmdInputMap`, `CmdLocalization`, `CmdExport`, `CmdDataTable`, `CmdSearchPanel`, `CmdAnimate`, `CmdParticleEditor`, `CmdMaterialEditor`, `CmdHUDLayout`, `CmdTerrainEditor`, `CmdNavMeshPanel`).
- **Binding `CmdToolPan`'s `"space"` shortcut via `cx.OnKeyWith("space", ...)` in `bindShortcuts`**: rejected because NSGo root-scope key bindings run ahead of focused button/switch/radio Space activation, breaking keyboard accessibility across panels. Keeping `"space"` on `CmdToolPan` for tooltip/shortcut display while handling Space-hold in `viewport.go` satisfies both panning and keyboard activation.
- **Hardcoding a fixed `Height: 44` without wrap on `right-rail-header`**: rejected because `AgentStatusDisconnected` renders `Disconnected`, `Reconnect`, and `Troubleshoot` beside the `Agent` and `Inspector` tabs, overflowing 380 px when wrapping is disabled.

## Consequences

- **Benefits**:
  - Clean AI-first chrome matching `Main.dc.html` and `Spec.dc.html` in both Dark and Light appearances at `1440 × 900`.
  - Missing or malformed scene files present an actionable alert card with path, line, fix hint, Retry, and Dismiss instead of a blank viewport.
  - All five `F6` regions, radio groups, switches, and `>= 28 × 28` hit targets work consistently with keyboard navigation.
- **Costs**:
  - Tests and muscle memory that previously clicked legacy top-toolbar panel toggles now invoke the corresponding commands (`CmdSave`, `CmdUndo`, `CmdAudioMixer`, etc.) or status-bar/drawer controls.
  - Legacy toolbar builders (`toolbarView`, `view2DToolsView`, `place3DToolsView`, `statusBarView`, `sceneFileButtons`) were later removed and their remaining unreachable controls restored in [unreachable toolbar controls fix](../bug-fix/2026-11-28-editor-unreachable-toolbar-controls.md).
