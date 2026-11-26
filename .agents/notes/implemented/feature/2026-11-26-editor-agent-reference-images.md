# Agent Note: Reference images in the Agent composer and atomic Pin to brief

Status: implemented

## Problem

Issue #1027 implements the *Paste or drop an image* and *Pin to brief* workflows from `docs/design/editor/Sketch.dc.html` on top of the [Agent panel and composer contract](../../implemented/architecture/2026-11-26-editor-agent-connect.md). Authors need two distinct lifetimes for reference art without hidden state:

1. **One-shot attachments**: dropping, pasting, or picking a PNG, JPEG, or WebP image attaches it to the next request only, shows a chip with a hover preview, and refuses unsupported formats with a clear reason.
2. **Project-wide pinned references**: clicking **Pin to brief** copies the image into `art/reference/<name>` and adds one entry to `brief.yaml` under `art.references` as a single undoable `Editor.History` step that shows in Review/Changes, joins every subsequent request through the `brief.yaml` Always chip, and disappears completely (both file and YAML line) on Undo.

## Decision

### Strict in-memory decoding for PNG, JPEG, and WebP

- `DecodeAgentReferenceImage` validates magic bytes (`\x89PNG\r\n\x1a\n`, `\xff\xd8\xff`, and `RIFF....WEBP`) and decodes the image in memory (`image/png`, `image/jpeg`, `golang.org/x/image/webp`) under strict size, dimension, and count caps (`16 MiB`, `4096×4096`, `16M` pixels, max `8` one-shot attachments per request and `16` pinned brief references).
- Non-matching formats (GIF, SVG, BMP, PDF, text, non-regular files, remote non-`file://` URIs, or malformed image files) are refused before creating a chip, surfacing an explicit reason in `AgentAttachError` and the composer's accessible `RoleAlert` banner.
- Decoded `*image.RGBA` pixels are attached to `AgentReferenceImage` so thumbnails, turn bubbles, and hover preview cards render consistently across all three formats—including WebP—via `core.Image{Source: core.ImageSource{Pixels: ..., Version: ...}}`. Historical `AgentTurn.Images` retain thumbnail pixels without retaining raw encoded byte buffers in RAM.

### Composer drop, paste, hover preview, and one-shot clearing

- `agent-composer` wires `core.View.OnFileDragEnter`, `OnFileDragLeave`, and `OnFileDrop` to show the Spec-styled **"Drop to attach"** overlay and call `DropAgentReferenceFiles`.
- `agent-composer-input` wires `core.Input.OnPaste` (`HandleAgentComposerPaste`), which handles pasted image bytes (`PasteEvent.Files`) and copied file URLs (`PasteEvent.URIs`) without interfering with normal plain-text pastes (`PasteInsert`). Injectable `Editor.ReadClipboardImage`, `Editor.ReadClipboardFiles`, and `Editor.PickAgentReferenceFiles` hooks keep clipboard and picker flows deterministic in tests, and async picker/clipboard callbacks verify editor lifecycle and project identity before mutating state.
- `SubmitAgentPrompt` snapshots one-shot attachments (`AgentRequest.AttachedImages`) and pinned brief references (`AgentRequest.BriefImages`) into a deduplicated `AgentRequest.Images` slice and `AgentTurn.Images`, then clears `attachedImages` immediately so the next request never carries one-shot attachments.

### Schema-compatible YAML AST pinning and atomic `fallibleCommand` Undo

- `project.Brief` (`packages/game/project/brief.go` and `apps/website/public/schemas/brief.v1.json`) declares `Art *BriefArt` (`art.direction` and `art.references: [{path, note}]`) with strict validation (clean slash-separated project-relative paths, no duplicates, no control characters) so `brief.yaml` round-trips cleanly between **Pin to brief** and **Settings › Game brief** (#1026).
- `PinAgentReferenceImage` executes a `pinReferenceImageCmd` (`fallibleCommand`) on `Editor.History`, guarded against Play mode, merge conflicts, and read-only History preview.
- `appendBriefReferenceYAML` edits existing `brief.yaml` files through `yaml.Node` to preserve existing keys, comments, and formatting while adding a single `- path: art/reference/<name>` line (plus `note:` when supplied), and creates fresh `brief.yaml` files with `project.BriefSchemaComment` and `version: 1`.
- Every read, write, and Undo/Redo step enforces component-by-component `os.Lstat` symlink containment under the project root (protecting against TOCTOU symlink swaps), avoids unbounded `os.ReadFile` calls during filename disambiguation (`resolvePinImageTarget`), synchronizes `Editor.Brief()` (`reloadBriefFromDisk`), and keeps the pin card's **Undo** button (`CanUndoLastBriefPin` / `UndoLastBriefPin`) bound strictly to the top of `Editor.History` so it never undoes unrelated subsequent edits.
- `BriefFileExists`, `BriefReferences`, and `BriefReferenceImages` cache parsed metadata and decoded pinned images by `(path, modTime, size)` (pruning stale keys) so composer keystrokes and UI redraws avoid redundant disk reads and YAML parsing while staying synchronized with disk edits and Undo/Redo.

## Alternatives considered

### Copy dropped or pasted images into a temporary project folder immediately on attach

Writing one-shot attachments to `.nsgo/tmp/` or `art/reference/` on drop would let CLI runners reference a file path immediately. This was rejected because an unpinned image is explicitly ephemeral ("sent with this request only" and never dirties the project or git working tree); keeping one-shot bytes strictly in memory until the user clicks **Pin to brief** (and materializing ephemeral files only in a private OS temp directory cleaned up on exit inside `CLIAgentRunner.Run`) ensures nothing touches the project directory unless explicitly pinned.

### Re-marshal `brief.yaml` through `project.EncodeBrief` on every Pin to brief

Always re-encoding through `project.Brief` would strip author comments and rewrite unrelated sections of `brief.yaml`, producing noisy diffs on the Pin to brief confirmation card. Supporting `Art *BriefArt` in `project.Brief` for schema/Settings interop while using `yaml.Node` AST updates in `PinAgentReferenceImage` gives both strict schema compatibility and minimal single-line diffs.

## Consequences

- **Benefits**: One-shot image attachments never leak across requests or dirty the working tree; PNG, JPEG, and WebP work identically across drop, paste, and picker; Pin to brief produces clean, reviewable, schema-valid diffs in Changes/Git, stays in sync with Settings › Game brief, and reverts atomically with one Undo; all UI surfaces use `SpecPalette` tokens in both Dark and Light appearances and full EN/ZH localization.
- **Costs**: `CLIAgentRunner` writes temporary OS files for ephemeral one-shot images during subprocess execution (cleaned up immediately on exit) and validates every project-backed reference path against root containment before passing `-i` (Codex) or prompt references (Claude / resumed Codex).

## Testing

- `packages/game/editor/agent_references_test.go`: tests dropping a PNG (drag overlay, chip, hover preview, received once by scripted `AgentRunner`, absent on next request), pasting/dropping PNG/JPEG/WebP, refusing GIF/SVG/TXT/corrupt PNG with visible reasons, Pin to brief writing `art/reference/<name>` and one YAML line in `brief.yaml`, Changes/Review visibility, next request carrying the pinned image through the Always chip, Undo removing both files/lines (both with existing and absent `brief.yaml`), Redo restoring them, and Dark/Light `SpecPalette` token switching.
- `packages/game/editor/editorcmd/agent_references_screenshot_test.go`: captures real macOS `-target macos` screenshots of attached preview, Drop to attach overlay, Pin to brief YAML diff card, and unsupported-format refusal banner.
