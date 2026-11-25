# Agent Note: Editor Play mode marks, rolling replay, frozen-frame ask, and panic replay

Status: implemented

## Problem

While playing a game in the editor (`packages/game/editor`), authors had no automatic rolling input recording, no way to drop lightweight marks during live play and send them together as one thread on Stop, no way to freeze a problematic frame and hand the agent the frame image, input replay, and live entity state over MCP, and no way in the Output panel to triage a Go panic stack trace with folded runtime frames and replay up to one frame before the panic.

## Decision

Extend Play mode (`play.go`, `play_marks.go`, `play_panic.go`, `play_marks_ui.go`), the Output panel (`output_panel.go`), and the editor MCP server (`editormcp/play_frozen.go`) following `docs/design/editor/PlayMode.dc.html` and `Spec.dc.html`:

- **Bounded 60-second rolling replay (`playRollingRecorder`)**: Every Play step samples `Editor.PlayInputSource` (watching `PlayInputActions` / `PlayInputAxes`, defaulting to `Project.Input.Actions`) into a sliding window capped at `60 * PlayRateHz()` frames (3,600 frames at 60 Hz). Within 60 seconds (`windowStart == 0`), event steps match exact Play frames; once the window slides past 60 seconds, older events fold into `baseAction` / `baseAxis` and emit an entering-state step 0 event so `replay.Recording.Encode` always emits valid deterministic version 2 YAML under `replays/<scene>-<frame>.replay.yaml`.
- **Marks and single-thread dispatch (`CmdPlayMark` / `alt+f`)**: `AddPlayMark` saves the rolling replay atomically under `replays/` without stopping or pausing Play, records the mark (time, frame, `"replay attached"`), pins a flag on `ProfilerStripView`, and lists it under **MARKS · N** in the Inspector's Play-mode **Agent** tab. `Stop()` calls `SendPlayMarks()` automatically when unsent marks exist (and **Send marks now** sends early), bundling all unsent marks into one `PlayMarksThread` passed to `Editor.OnPlayMarksThread` and retained in `LastPlayMarksThread()`.
- **Frozen-frame questions (`CmdAskFrozenFrame` / `mod+l`) and MCP replay stepping**: `OpenAskFrozenFrame` pauses Play, captures a PNG of the viewport (`DrawViewport` + `mcp.RenderPNG`), and opens **"Ask about frame N"** with the frame preview, attachment chips, prompt input, and `"A fix comes back as a review"`. `SubmitAskFrozenFrame` saves the rolling replay, attaches the frame PNG and live entity states, and invokes `Editor.OnAskFrozenFrame`. `editormcp` exposes `play_frozen_context` (returning the PNG `ImageContent`, prompt, version 2 `replayYaml`, live entities, marks, and panic) and `play_replay_step` (calling `Editor.AgentStepPlayReplay` to step frames A–B while keeping Play paused and rendering the viewport `"agent stepping frames A–B"` pill and `"Stepped frames A–B with game_loop_step…"` log).
- **Output panic card and `ReplayUpToPanic`**: `ParsePanicTrace` parses Go panic messages and stack traces from streamed run output or recovered Play panics, separating game frames (rendered as clickable `file:line` links via `OpenOutputEntry`) from runtime/engine frames (folded behind `"N frames in the game runtime · Show"`). `ReplayUpToPanic` loads the saved panic replay, resets the scene from the clean pre-play snapshot (calling `Editor.OnPlayReset` when set), steps deterministically through `PlayActiveInput()` to `panicFrame - 1`, and leaves Play active and paused without triggering the panic.
- **No edit-toolbar bloat**: All Play-mode mark/ask controls live in `playModeBarView` / `remotePlayBarView`, `InspectorView`'s Play-mode Agent tab, `ViewportView`'s Play overlays, and shortcuts (`alt+f`, `mod+l`), leaving the 2D `editor-toolbar` completely unchanged.

## Alternatives considered

- **Add Mark and Ask buttons directly to the main 2D `editor-toolbar`** — Would make them visible in one bar across all modes. Rejected because the 2D toolbar must stay within 2 rows at `1120×680`, and mark/ask controls only apply during Play mode (matching `PlayMode.dc.html`'s dedicated Play bar).
- **Keep an unbounded input buffer in memory for the entire Play session** — Simpler than a sliding window because events never need rebasing. Rejected because long play sessions would grow memory unboundedly and violate the design contract (`"Last 60 s kept · 3 600 frames"`).
- **Store marks metadata inside the `.replay.yaml` file** — Would keep marks and inputs in one file. Rejected because `packages/game/replay` version 2 YAML strictly refuses unknown keys (`KnownFields(true)`); keeping `.replay.yaml` strictly schema-valid version 2 YAML ensures every saved mark replay loads with `replay.LoadFile` and `game replay` unchanged.

## Consequences

- **Benefits**:
  - Pressing `alt+f` multiple times during Play and stopping sends exactly one thread with valid, deterministically replayable version 2 `.replay.yaml` files.
  - Agents connected to `editormcp` inspect the frozen frame image, version 2 replay, and live entities directly and step frames visibly while keeping the game paused.
  - `ReplayUpToPanic` stops reproducibly one frame before a panic so authors and agents can inspect state right before the crash.
- **Costs and known limits**:
  - When a Play session exceeds 60 seconds, replays reflect the last 60 seconds of input from window entry rather than from frame 0; deterministic replay from frame 0 requires a run within the 60-second window (or a game snapshot at window entry).
  - External `go tool game dev` runs report a panic frame in `OutputPanic` only when the game's log emits a frame/step annotation or when Play mode captures the panic directly.

## Verification

- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor/editormcp)`
- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor/editormcp)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios ./editor)`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target ios ./editor/editormcp)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
