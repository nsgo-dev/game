### Added

- `game/editor`: add the anchored `⌘L` in-place agent composer (`AskInPlaceComposerView`, `CmdAskAgent`), contextual suggestions, viewport/scene-tree context menu wiring, and the right-panel Agent thread (`RightPanelView`, `AgentPanelView`) with `SkipPlan` tagging (#1037).
- `game/editor/editormcp`: add the `editor://agent/turn` resource (JSON metadata + captured viewport PNG) and `agent_turn_get` / `agent_turn_reply` tools to deliver in-place selection, scene, and viewport-image context to connected agents (#1037).
