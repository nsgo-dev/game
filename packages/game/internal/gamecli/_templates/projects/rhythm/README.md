# Pulse Lanes

A four-lane rhythm game built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game/packages/game`](https://github.com/nsgo-dev/game), made from the `rhythm` template.

Notes fall down four lanes to the beat of the song; hit each lane as its note reaches the line. A hit close to the note is Perfect, a little further Great or Good, and a note no hit reaches in time is a Miss. Hits build a combo and fill the energy bar, misses break the combo and drain it: reach the end of the song with energy left to clear the chart, and run out to fail it. The score counts every hit against a perfect one, out of 1,000,000, and earns a rank from S to D; the menu keeps the best clear of each chart. Calibrate the audio and video latency of each device from the menu, and charts are judged by what the player hears and sees there.

It demonstrates:

- A song list and its charts in a versioned YAML file, `songs.yaml`, read strictly (unknown fields refused) and kept in the canonical form its encoder writes, with each song's tempo map in a `rhythm` beat map file beside it.
- A music clock, `rhythm.Clock`, that keeps the notes in time with the music on every platform, from the engine's clock and the music player's reports, looping the track when it is shorter than the song.
- Hits judged by when the input went down, not by the game step that reads it: keys and touches by the time of their event, a controller by the step that reads it.
- Latency calibration with `rhythm.CalibrationScreen`, kept for each device with `rhythm.OffsetStore`, and played through the same input as the charts.
- The rules in a type of their own, `Run`, that a test drives exactly, with no clock or input.
- Records saved with `save.Store`, sounds played with `sound.Mixer`, and input from the map in `game.yaml` bound into `input.Actions`, with each lane's column bound to touches and the mouse.
- Native NSGo views for the menu, the score, combo and energy, the pause menu and the result, over a playfield drawn on a `core.Canvas` with no image files; the music and the click are the placeholder pack's.
- Headless tests that play the charts through `nsgotest` to a clear and to a fail with the keyboard, a game controller, touch and the mouse, and calibrate both offsets.

## Controls

| | Keyboard | Game controller | Touch and mouse |
| --- | --- | --- | --- |
| Hit lanes 1 to 4 | D F J K, or ← ↓ ↑ → | D-pad left, D-pad down, South, East, or the shoulders and triggers | Tap or click the lane |
| Move in a menu | ↑ ↓ or W S | D-pad up and down | |
| Pick, resume, save a calibration | Enter or Space | South | The button |
| Back to the menu | Escape or Backspace | East, or Back (Select); only Back in a calibration, where East is a lane | Menu, Back |
| Pause | Escape or P | Start | Pause |

The keyboard and game controller bindings are the input map in `game.yaml`; edit them there. The lanes' touch and mouse regions are bound in `layoutField`, from the playfield's layout. South and East are lanes as well as confirm and back, so a calibration takes them as taps until it is done, and the result ignores a lane tap on them for a moment after it shows, so a player still tapping as a chart fails reads the result first.

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check
```

## Running and testing

Run with hot reload on your desktop (macOS, Windows or Linux):

```sh
go tool nsgo dev
```

Run on other targets:

```sh
go tool nsgo dev -target web
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
```

Run the tests, which play the charts and check every chart is fair:

```sh
go tool nsgo test ./...
```

Vet and check across platforms:

```sh
go tool nsgo check -target macos,windows,linux,android,web
go tool nsgo check -target ios,ios_simulator   # on macOS with Xcode
```

Build release artifacts into `dist/`:

```sh
go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
go tool nsgo build -target ios,android_arm64
```

## The songs file

`songs.yaml` holds the rules every chart is played by and the songs:

| Field | What it is |
| --- | --- |
| `version` | The format's version, 1. |
| `judgement` | `perfectMs`, `greatMs` and `goodMs`: how early or late a hit may be, in milliseconds, for each grade; each wider than the one before, up to 500. A note no hit lands within `goodMs` of is a miss. |
| `energy` | `start`, the energy a chart starts with, 1 to 100; `perfect`, `great` and `good`, what those hits add; `miss`, what a miss takes, a negative number. Energy stays from 0 to 100, and a chart fails as it reaches 0. |
| `songs` | The songs, in the order the menu lists them. |

Each song has an `id`, which the records are kept by, so keep it when renaming or moving the song; a `title`; its `music`, a file in `songs/` such as `my-song.m4a` (WAV, AAC or MP3) or `placeholder:<name>` for a sound of the placeholder pack; `loop: true` when the music is shorter than the song and repeats; `beatMap`, its tempo map, a file in `songs/` ending in `.beatmap.yaml`; `lengthBeats`, where it ends; and its `charts`, easiest first.

Each chart has an `id` within its song, a difficulty `name`, a `level` from 1 to 20 and its `notes`, each `{beat: 4, lane: 0}`: when to hit it, in beats of the beat map, and the lane, 0 to 3 from the left. Notes are listed in time order, and in lane order at one beat; a beat may be a fraction, such as 7.5 for an eighth note.

The beat map is the `rhythm` package's format (see the game layer's File Formats page): where beat 0 falls in the audio (`offsetMs`), the tempo from each beat on and the meter from each bar on. To chart a new song, measure where its first beat falls and its tempo, write them into its beat map, and place notes on its beats. `TestSongsFileIsCanonical` prints the canonical form of `songs.yaml` when it is not in it, and `TestChartsAreFair` fails when two notes of a lane are too close to hit apart.

## Calibration

Every device delays sound and picture by its own amount, and Bluetooth headphones by much more. Calibrate audio plays a click every 750 milliseconds and Calibrate video flashes a disc; tap any lane along with them, 16 times after 4 to find the beat, and Save keeps the median offset for this device, which the menu shows. Charts are then judged by the audio offset and drawn ahead by the video offset. The offsets are kept by `rhythm.OffsetStore` under the device's model, so moving the game's storage to another device calibrates that one anew.

## Layout

- `game.yaml`: the project file, with the game's name, id and version, its window, the input map and the export presets.
- `songs.yaml`: the songs and the rules. `songs.go` defines its format: `ParseSongs`, `EncodeSongs`, the checks a song and a chart must pass, and `LoadSongs`, which reads each song's beat map and music.
- `songs/`: each song's beat map, and the music of songs that bring their own.
- `run.go`: the rules. `Run` is a chart being played: it judges hits, misses the notes gone past, and keeps the score, the combo and the energy.
- `game.go`: the game: the loop, the input and when it went down, the music and its clock, the pause, the result and the calibrations.
- `draw.go`: how the playfield is laid out and drawn on the Canvas.
- `ui.go`: the native views: the menu, the score, combo and energy, the pause menu, the result and the calibration screen.
- `records.go`: the saved best clears, and where the calibration is kept.
- `project.go`, `main.go`: the parsed `game.yaml` and the entry point.
- `*_test.go`: the tests. `game_test.go` plays the game through `nsgotest`; `run_test.go` tests the rules and benchmarks a chart; `songs_test.go` checks the songs and fuzzes their format.
- `nsgo.yaml`: NSGo app metadata and dev settings; its name, id, version and build output and targets follow `game.yaml`.
