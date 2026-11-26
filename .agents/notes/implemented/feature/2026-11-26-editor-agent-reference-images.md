# Agent Note: Reference images in the Agent composer and atomic Pin to brief

Status: implemented

## Problem

Issue #1027 implements the *Paste or drop an image* and *Pin to brief* workflows from `docs/design/editor/Sketch.dc.html` on top of the [Agent panel and composer contract](../../implemented/architecture/2026-11-26-editor-agent-connect.md). Authors need two distinct lifetimes for reference art without hidden state:

1. **One-shot attachments**: dropping, pasting, or picking a PNG, JPEG, or WebP image attaches it to the next request only, shows a chip with a hover preview, and refuses unsupported formats with a clear reason.
2. **Project-wide pinned references**: clicking **Pin to brief** copies the image into `art/reference/<name>` and adds one entry to `brief.yaml` under `art.references` as a single undoable `Editor.History` step that shows in Review/Changes, joins every subsequent request through the `brief.yaml` Always chip, and disappears completely (both file and YAML line) on Undo.

## Decision

### Strict in-memory decoding for PNG, JPEG, and WebP

- `DecodeAgentReferenceImage` validates magic bytes (`\x89PNG\r\n\x1a\n`, `\xff\xd8\xff`, and `RIFF....WEBP`) and decodes the image in memory (`image/png`, `image/jpeg`, `golang.org/x/image/webp`) under strict size and pixel caps (`16 MiB`, `4096×4096`, `16M` pixels).
- Non-matching formats (GIF, SVG, BMP, PDF, text, or malformed image files) are refused before creating a chip, surfacing an explicit reason in `AgentAttachError` and the composer's accessible `RoleAlert` banner.
- Decoded `*image.RGBA` pixels are attached to `AgentReferenceImage` so thumbnails, turn bubbles, and hover preview cards render consistently across all three formats—including WebP—via `core.Image{Source: core.ImageSource{Pixels: ..., Version: ...}}`.

### Composer drop, paste, hover preview, and one-shot clearing

- `agent-composer` wires `core.View.OnFileDragEnter`, `OnFileDragLeave`, and `OnFileDrop` to show the Spec-styled **"Drop to attach"** overlay and call `DropAgentReferenceFiles`.
- `agent-composer-input` wires `core.Input.OnPaste` (`HandleAgentComposerPaste`), which handles pasted image bytes (`PasteEvent.Files`) and copied file URLs (`PasteEvent.URIs`) without interfering with normal plain-text pastes (`PasteInsert`). Injectable `Editor.ReadClipboardImage`, `Editor.ReadClipboardFiles`, and `Editor.PickAgentReferenceFiles` hooks keep clipboard and picker flows deterministic in tests.
- `SubmitAgentPrompt` snapshots one-shot attachments (`AgentRequest.AttachedImages`) and pinned brief references (`AgentRequest.BriefImages`) into a deduplicated `AgentRequest.Images` slice and `AgentTurn.Images`, then clears `attachedImages` immediately so the next request never carries one-shot attachments.

### YAML AST pinning and atomic `fallibleCommand` Undo

- `PinAgentReferenceImage` executes a `pinReferenceImageCmd` (`fallibleCommand`) on `Editor.History`.
- `appendBriefReferenceYAML` edits `brief.yaml` through `yaml.Node` rather than re-marshaling into a fixed struct. This preserves existing keys, comments, and formatting across both `asset_gen.go`'s style fields and future `brief.yaml` schema additions (#1026), adding a single `- path: art/reference/<name>` line (plus `note:` when supplied).
- If `art/reference/<name>` already exists with different bytes, `resolvePinImageTarget` picks a unique suffixed filename (`<stem>_2<ext>`) so pinning never overwrites unrelated artwork. If `brief.yaml` did not exist beforehand, `undo()` removes both `brief.yaml` and `art/reference/<name>` (plus empty `art/reference` / `art` directories created by the command); otherwise it restores the exact prior `brief.yaml` bytes and removes the copied image.
- `BriefReferenceImages` caches decoded pinned images by `(path, modTime, size)` so UI redraws and hover previews of the `brief.yaml` Always chip avoid repeated disk decoding while staying synchronized with disk edits and Undo/Redo.

## Alternatives considered

### Copy dropped or pasted images into a temporary project folder immediately on attach

Writing one-shot attachments to `.nsgo/tmp/` or `art/reference/` on drop would let CLI runners reference a file path immediately. This was rejected because an unpinned image is explicitly ephemeral ("sent with this request only" and never dirties the project or git working tree); keeping one-shot bytes strictly in memory until the user clicks **Pin to brief** ensures nothing touches disk unless explicitly pinned.

### Decode and re-encode `brief.yaml` through a fixed Go struct

A typed Go struct for `brief.yaml` would provide compile-time field names, but `brief.yaml`'s full schema is owned by #1026 (in flight) while `asset_gen.go` already reads `art:` style properties. Using `yaml.Node` AST updates guarantees byte-preserving edits to existing `brief.yaml` files and adds exactly one YAML line for `art.references` without clashing with unrelated sections.

## Consequences

- **Benefits**: One-shot image attachments never leak across requests or dirty the working tree; PNG, JPEG, and WebP work identically across drop, paste, and picker; Pin to brief produces clean, reviewable diffs in Changes/Git and reverts atomically with one Undo; all UI surfaces use `SpecPalette` tokens in both Dark and Light appearances.
- **Costs**: `CLIAgentRunner` currently forwards project-backed (pinned) reference paths to external CLIs (`-i` for Codex, reference list for Claude) while in-memory one-shot image bytes are delivered directly via `AgentRequest.AttachedImages` / `AgentRequest.Images` for scripted, in-process, or multimodal API runners.

## Testing

- `packages/game/editor/agent_references_test.go`: tests dropping a PNG (drag overlay, chip, hover preview, received once by scripted `AgentRunner`, absent on next request), pasting/dropping PNG/JPEG/WebP, refusing GIF/SVG/TXT/corrupt PNG with visible reasons, Pin to brief writing `art/reference/<name>` and one YAML line in `brief.yaml`, Changes/Review visibility, next request carrying the pinned image through the Always chip, Undo removing both files/lines (both with existing and absent `brief.yaml`), Redo restoring them, and Dark/Light `SpecPalette` token switching.
- `packages/game/editor/editorcmd/agent_references_screenshot_test.go`: captures real macOS `-target macos` screenshots of attached preview, Drop to attach overlay, Pin to brief YAML diff card, and unsupported-format refusal banner.
