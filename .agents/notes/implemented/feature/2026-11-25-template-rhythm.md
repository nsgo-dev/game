# Agent Note: The rhythm template, Pulse Lanes

Status: implemented

## Problem

Issue #802, under #786, asks for a `rhythm` template: a rhythm game with a chart in YAML, note lanes, timing judgements, combo and score, and latency calibration, built mainly on `rhythm`, `sound` and `input`, original, playable with the keyboard, a game controller and touch, and tested through `nsgotest` to a win and a loss. It has to stay small enough to read in an afternoon, follow the template infrastructure of #787 (`docs/en/templates.md`), and keep every file its author writes in text.

## Decision

The template is the module `github.com/nsgo-dev/game/template/rhythm`, with the project in `template/rhythm/template/`, its copy in `packages/game/cmd/game/_templates/projects/rhythm`, one registry entry in `templates.go`, a `go.work` entry and a row and section in `docs/{en,zh}/templates.md`. CI's `templates` matrix finds it from its directory.

The game, Pulse Lanes, has four lanes. Notes fall to a hit line; a hit within 45, 90 or 135 ms is Perfect, Great or Good, and a note no hit reaches within the good window is a Miss. Hits build a combo and add energy, misses reset it and take 15; reaching the song's end with energy left clears the chart (the win) and running out fails it (the loss). The score is each grade's weight (1000, 700, 400, 0 thousandths) against a perfect, out of 1,000,000, with a rank from S to D.

- **Songs format** (`songs.yaml`, `songs.go`): a template-local YAML file with `version: 1`, decoded with `KnownFields(true)`, one document only, anchors and aliases refused, and every whole-number field checked to be a YAML integer, because yaml.v3 cuts `lane: 1.5` to 1 and the file would neither mean what it says nor round-trip (the same guard `rhythm.ParseBeatMap` has). It holds the judgement windows, the energy rules and the songs; each song names a `rhythm` beat map file in `songs/` for its tempo and its music, either a file in `songs/` or `placeholder:<name>`. Notes are written one per line in flow style (`- {beat: 4, lane: 0}`) through `Note.MarshalYAML`, so a chart reads as a list of hits in review; `EncodeSongs` writes the canonical form and `TestSongsFileIsCanonical` keeps the file in it. Song and chart IDs key the saved records.
- **Rules apart from time** (`run.go`): `Run` takes track positions, never a clock or input, judges a hit against its lane's next note (missing the ones whose window is past first), sweeps misses in time order in `Advance`, and ends as `Cleared` or `Failed`. Its tests and benchmark drive it exactly.
- **Music clock**: a chart starts a `rhythm.Clock` at `-leadIn` (2 s) and plays the music through an `audio.Player` once the clock reaches 0; the player's Play callback and `OnProgress` feed `Clock.Sync`. The music is the placeholder pack's 16-beat 150 BPM loop, so the song loops it, and `unwrapLoop` adds the track lengths the player's position has wrapped through before `Sync`. Where the platform plays no audio (headless tests, a failed player) the clock runs on the engine's clock alone, so the game and its tests behave the same.
- **Input timing**: hits are judged by when the input went down, not by the fixed step that reads it. A `cx.Effect` on `core.KeysDown` and `core.KeyCodesDown` stamps each lane a bound key goes down on, the field view's `OnTouch` and `OnPressDown` stamp the lane a finger or a click lands in, and the step uses the stamp when `Actions.JustPressed` fires for the lane; a controller, read once a step (`stepRate` 120), uses the step's time. Judging uses `Clock.Position(at)`, and drawing `Clock.DrawPosition`.
- **Calibration**: the menu runs `rhythm.CalibrationScreen` for the audio or the video offset, saved per device with `rhythm.OffsetStore` under `rhythm.DeviceKey`. Lane presses tap the calibration through `Calibrator.Tap` with the same stamps a chart uses, so the input path calibrated is the one judged; touches and the screen's own buttons go through the screen's `OnPressDown` and `OnPress`.
- **Input map**: `game.yaml` binds the four lanes (D F J K and the arrows, D-pad left and down, South and East, the shoulders and triggers), menu movement, confirm, back and pause through `project.File.BindInput`; `layoutField` adds each lane's column as an `input.TouchRegion` and an `input.MouseRegion` whenever the playfield's view is laid out at a new size, and triggers again any lane stamped since the last step, because `input.Actions.Bind` drops the edge it latched. On a controller South and East are lanes as well as confirm and back, so a calibration treats a step a lane is hit in as a tap only (Select leaves it), and the result ignores confirm and back in a lane step for `resultGuard` after it shows.
- **Pause**: the pause action, the Pause button, or the window leaving `core.LifecycleActive` pauses the clock and the music; resuming re-anchors the clock and plays the music on.
- **Records** (`records.go`): `save.Store` over `core.Storage` with schema version 1 keeps the best clear of each chart as JSON, a file the program writes for itself.
- **Pin**: `go.mod.tmpl` pins `packages/game` at `v0.0.0-20261125054131-af10f8ca9ebe`, a `main` commit that has `rhythm` and `project.File.BindInput`, following the rule in docs/en/templates.md that a template requires a published version with every package it uses; `go.yaml.in/yaml/v3` is a direct requirement at the version `packages/game` uses.

## Gaps in the packages, worked around here

- `rhythm.Clock.Sync` takes the song's position, but a looping `audio.Player` reports the track's position, which goes back to 0 at each loop; the template unwraps it (`unwrapLoop`). A `Clock` that knew a loop length would save every looping game this code.
- `sound.Music` plays through its own `audio.Player` and exposes no position or progress callback, so music played through a `sound.Mixer` cannot feed `rhythm.Clock.Sync`; the template plays its music on an `audio.Player` directly and uses the mixer for the hit click only.
- `rhythm` has a tempo map but no note chart or judgement type, so each rhythm game defines its own; the template's `songs.yaml` and `Run` are that layer.
- `input.Actions` records no time with an edge, so a game that judges by input time stamps the inputs itself, as `keysChanged`, `touched` and `pressedDown` do.

## Alternatives considered

### Judging hits by the step that reads them

Strongest argument: `input.Actions.JustPressed` in `Loop.Update` is how every other template reads input, with no extra code. Not chosen because it moves every hit to the next step, up to a frame late at 60 Hz and late on average, while `rhythm.CalibrationScreen` measures taps as they go down; the calibration would not match the judging, and the perfect window is only 45 ms.

### One file per chart, or the notes in the beat map file

Strongest argument: a chart per file is how many rhythm games ship, and keeping the notes in the beat map puts all of a song's timing in one place. Not chosen because `rhythm.BeatMap` is the package's format with unknown fields refused, so notes cannot join it, and a single small `songs.yaml` with the rules and two charts reads in one sitting; a game that grows can split charts out by giving `Song` a file field.

### Music through `sound.Music`

Strongest argument: it brings crossfades and the mixer's volume settings for free. Not chosen because it hides the player, so the clock could not follow the music (see the gaps above).

## Consequences

- A new template a game author can start from with `game new -template rhythm`, with a songs format whose tests print its canonical form and check every chart is fair.
- The template carries the loop unwrapping and input stamping that the packages lack; if `rhythm` or `input` gain them, the template's code shrinks to calls.
- The songs format is local to the template, documented in its README rather than in `docs/en/formats.md`, which covers the formats `packages/game` defines.
