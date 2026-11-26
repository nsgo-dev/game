# Agent Note: Versioned `brief.yaml` format, Settings Agent pages, and MCP context exposure

Status: implemented

## Problem

The NSGo Game Editor's Settings design (`docs/design/editor/Settings.dc.html`) specifies two shared project-root files that the agent reads before every request: `brief.yaml` (pitch, genre and tags, platforms, art direction, tone and audio, and feel and difficulty with playtest targets) and `AGENTS.md` (plain Markdown project rules shared with Claude Code and Codex). Previously, `playtest.yaml` goals already referenced `source: brief.yaml`, but no `brief.yaml` format, JSON Schema, Settings pages, reverted-review rule suggestion flow, or MCP context surface existed in `game/project`, `game/playtest`, `game/editor`, or `game/editor/editormcp`.

## Decision

We implement `brief.yaml` as a strict versioned YAML format in `game/project`, connect it to `game/playtest`, add Agent › Game brief and Agent › Project rules pages to `EditorSettingsDialogView`, and expose both files through `game/editor/editormcp`:

- **Format ownership in `game/project` (`project/brief.go`)**: `Brief` (`version: 1`, `BriefSchemaURL = "https://game.nsgo.dev/schemas/brief.v1.json"`) lives beside `project.File` (`game.yaml`) so `game/playtest`, `game/editor`, `game/editor/editormcp`, and `cmd/gameschema` can all import it without introducing cycles or pulling editor UI dependencies into headless playtest packages. `ParseBrief` decodes strictly with `KnownFields(true)`, refuses duplicate keys, YAML anchors/aliases/merge keys, trailing documents, and non-canonical or unsupported versions with 1-based line numbers via `loaderr.Locate`; `EncodeBrief` writes deterministic 2-space YAML prefixed with `# yaml-language-server: $schema=...` that round-trips byte-for-byte unchanged (`formattest.Run` conformance).
- **Playtest target bridge in `game/playtest` (`playtest/brief.go`)**: `Brief.Feel.Targets` (`BriefPlaytestTarget`) mirrors `playtest.Goal` (`metric`, optional `scene`, `summary`, `unit`, `min`, `max`, `equal`) and bridges directly into `playtest.Config` via `GoalFromBriefTarget`, `TargetsFromBrief`, `ApplyBriefTargets`, and `LoadBriefTargets`, updating only unset goals or goals with `source: brief.yaml`.
- **Undoable human edits vs. reviewable agent/suggested edits (`editor/brief_rules.go`)**: Manual edits to `brief.yaml` (`EditBrief`) and `AGENTS.md` (`SetProjectRules`) execute fallible commands through `Editor.History` that atomically persist to disk and cleanly restore (or delete newly created files) on Undo. Agent edits via MCP (`brief_edit`, `rules_edit`) and clicked suggestions from consistently reverted reviews (`SuggestedProjectRules` / `ProposeSuggestedRule`) create pending `DocReview` entries with unified diffs without touching disk until the person clicks **Keep** (`KeepDocReview`), which then applies as one undoable step in `Editor.History`.
- **Settings UI and Agent dispatch (`editor/settings_ui.go`, `editor/brief_rules_ui.go`)**: `EditorSettingsDialogView` adds **AGENT** pages (`Game brief` with `"Empty"` / `"1 gap"` / `"N gaps"` badge and `Project rules` with bulleted rule count) above **EDITOR · THIS COMPUTER** (`General`, `Shortcuts`), adds the top Settings search / agent bar, and renders the right-hand **Agent on this page** card on Agent pages using only `Spec.dc.html` tokens (`SpecPalette`). Clicking a suggested ask or submitting an agent request dispatches `SettingsAgentRequest` via `AskAgentFromSettings`, updating `ComposerDraft` and invoking `Editor.OnSettingsAgentRequest` and `Editor.OnComposerPrompt`.
- **MCP context surface (`editor/editormcp/context.go`, `stream.go`)**: Extends [the editor's MCP server](../../implemented/architecture/2026-11-25-editor-mcp-server.md) with `project_context`, `brief_edit`, and `rules_edit` tools plus `editor://brief`, `editor://rules`, and `editor://context` resources (all mirrored through `Proxy` and subscribed for coalesced change-stream notifications).

## Alternatives considered

### Defining `brief.yaml` inside `game/editor`

Keeping `Brief` next to `EditorSettings` ([Settings › Shortcuts](./2026-11-26-editor-settings-shortcuts.md)) would keep all Settings data types in one package. It is rejected because `brief.yaml` is a shared project file committed to Git alongside `game.yaml` (not per-computer editor state), and `game/playtest` needs to read playtest targets from `brief.yaml` without importing `game/editor` (which already imports `game/playtest`).

### Auto-applying suggested rules or MCP edits directly to `AGENTS.md` / `brief.yaml`

Applying `brief_edit`, `rules_edit`, or "Add as a rule" directly through `EditBrief` / `SetProjectRules` would be simpler and still undoable. It is rejected because `Spec.dc.html` ("Nothing lands unseen") and issue #1026 require that agent edits to either file and suggested rules from reverted reviews land as reviewable changes first, leaving files on disk untouched until the person keeps them.

### Storing playtest targets only as free-form prose in `feel`

Keeping `feel` as a single string would match the minimal visual box in `Settings.dc.html` without nested target fields. It is rejected because headless playtest runners (`game/playtest`, see [playtest YAML](./2026-11-25-playtest-yaml-headless.md)) need deterministic, schema-validated numeric bounds (`min`, `max`, `equal`) rather than fragile prose scraping, while `feel.summary` still holds the human-readable guidance shown in the Settings form.

## Consequences

- **Benefits**: `brief.yaml` is a first-class, schema-validated, byte-for-byte round-trippable project format whose feel and difficulty targets feed `game/playtest` directly; `AGENTS.md` remains plain Markdown shared verbatim with Claude Code and Codex; every connected MCP agent receives both files via `project_context` and `editor://context`/`brief`/`rules` resources; and every UI surface uses Spec tokens with full EN/ZH localization.
- **Costs**: `editormcp` adds 3 built-in tools and 3 resources (updating tool/resource snapshots in `server_test.go`, `proxy_test.go`, and `example_test.go`). `EditorSettingsDialogView` widens from 780 pt to 1100 pt on Agent pages (860 pt on General/Shortcuts) to accommodate the 3-column Settings layout with the **Agent on this page** card while staying within 1200×760 and 1440×900 desktop windows.
