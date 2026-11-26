# Agent Note: Editor @-mentions, context chips, and context meter in the agent composer

Status: implemented

## Problem

The editor's agent composer (`docs/design/editor/Threads.dc.html`, issue #1020) promises: *"You always see what the agent is sent."* Without a unified, single-source-of-truth composer pipeline, three bugs creep in immediately:

1. Hover previews drift from the actual prompt context delivered to the agent (different line ranges, missing comments, or silently attached selection state).
2. Selecting an object in the viewport or scene tree silently leaks into requests even when the author did not add it.
3. Long sessions fill the context window with no visible gauge, no per-category breakdown, and no way to fold earlier requests or start fresh with a summary.

## Decision

Implement `AgentComposerView` inside the unified right-rail **Agent** panel (`RightRailView` / `AgentPanelView`, `⌘1` / `⌘2` alongside `Inspector`, defaulting to `RightTabInspector` so existing editor layouts and tests stay unchanged) in `packages/game/editor/agent_composer.go` and `packages/game/editor/agent_composer_ui.go` on four strict invariants:

- **Single source of truth for hover and send (`ResolveChipPayload`)**: Both `HoveredChipPayload()` and `AssembleAgentRequest(prompt)` call `ResolveChipPayload(chip)`. For scene objects, `extractSceneObjectBlock` reads the on-disk scene bytes when clean (matching `editormcp`'s `scene_read` rule so line numbers and comments agree) or `SaveYAML()` when dirty, returning exact 1-based `# <scene> · lines <start>–<end>` headers and dedented YAML lines. Image chips (`ChipKindAsset` images and `ChipKindViewportImage`) resolve to real PNG bytes and decoded `*image.RGBA` previews.
- **Explicit opt-in for selection (`SuggestedChip` vs `ComposerChips`)**: Selecting an object surfaces only a dashed `+ <Name> · selected` suggestion chip; `AssembleAgentRequest` includes ONLY `AlwaysChips()` (`brief.yaml`, `AGENTS.md` inside the project root, symlink-checked), explicitly attached chips, and the toggleable `ViewportImageChip()` when enabled. Pressing `⌘L` (`CmdAskAgent` / `TriggerAskAgentShortcut`) with an object selected attaches that object chip and focuses the composer; pressing `⌘L` with nothing selected attaches the whole open scene (`ChipKindWholeScene`).
- **Live `@`-mention picker over `search.go`**: Typing `@<query>` in the composer opens a picker backed by `ensureProjectIndex` (`search.go`), `Editor.Problems`, `PlaytestSession` / `AddComposerPlaytestRun`, and `Editor.History`, grouped across all 8 sections (Objects, Files, Prefabs, Assets, Problems, Playtest runs, History steps, Rules) with 9 tabs (`All` + 8 groups), match count, `↑↓` move, `↵` add, `Tab` / `Shift+Tab` cycle group, and `Esc` close. Already-attached items are excluded from the picker.
- **Context ring, breakdown, compaction, and fresh start**: `ContextMeter()` computes usage percentage and warning level (`<=70%` normal, `71–89%` warning, `>=90%` critical `"compact now"`) from `SetAgentTokenUsage` / `AgentResponse.Usage` (falling back to live estimates when no external agent has reported usage) across the 6 breakdown categories (`Conversation`, `Open scene`, `Attached`, `Always`, `Tools and instructions`, `Viewport image`). `CompactAgentThread()` folds earlier turns (`turns[:len-1]`) into a summary while keeping the last turn whole and dropping conversation tokens; `StartFreshAgentThread()` creates a new thread ID seeded with that summary and clears transient attached chips.
- **Stable input mounting in NSGo**: `core.Input` is mounted outside `core.Dynamic` blocks subscribed to `rev` so typing `@sli` never remounts the native text control or drops focus mid-keystroke.

## Alternatives considered

- **Defaulting the right rail to `RightRailAgent`**: Matches the AI-first framing of `Threads.dc.html`, but would hide `id:editor-inspector` by default and break existing inspector and localization tests (`i18n_settings_test.go`, `inspector_sections_test.go`). Defaulting to `RightRailInspector` with a `RightRailSwitchView` (`Agent` | `Inspector`), `⌘1` / `⌘2`, and automatic switch-on-`⌘L` preserves full backward compatibility while keeping Agent one keypress away.
- **Assembling request text separately from hover preview formatting**: Easier to add extra wrapper prose for the model, but violates the core guarantee of #1020 ("What a request sends is assembled from the chips and nothing else, and the same content is what the hover shows"). Routing both through `ResolveChipPayload` makes divergence structurally impossible.
- **Rendering the context ring via dynamically generated SVG strings**: Fragile across platforms if `stroke-dasharray` or SVG rotation differs. Drawing the 20×20 ring directly with `core.Canvas` and `core.CapRound` using `SpecPalette` tokens guarantees crisp, identical rendering across macOS, iOS, and desktop hosts in both Dark and Light themes.

## Consequences

- **Benefits**: Authors can inspect the exact YAML lines or image bytes sent for every chip; selection never silently leaks into prompts; `@`-mentions reuse the existing `search.go` project index without duplicating filesystem walkers; context compaction and fresh-thread handoff are deterministic and testable with scripted agents via `Editor.OnSendAgentComposerRequest`, `Editor.OnCompactAgentThread`, and `Editor.OnStartFreshAgentThread`.
- **Costs**: Adding `CmdAgentTab`, `CmdInspectorTab`, and `CmdAskAgent` adds 3 global shortcuts (`mod+1`, `mod+2`, `mod+l`) and 62 localized strings each in `locales/en.yaml` and `locales/zh.yaml`; `mod+l` was moved from `context_menu_ui.go` into the unified command registry in `commands.go` so it appears in the Command Palette and Shortcuts settings without double-firing.
- **Related notes**: Complements [editor focus rings and keyboard regions](./2026-11-26-editor-focus-ring-and-regions.md) (`SetAgentPanelRef` / `RegionInspector` / `:focus-within` composer card) and [editor Spec light theme](./2026-11-26-editor-spec-light-theme.md) (`SpecPalette` Dark and Light tokens).
