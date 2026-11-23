# rhythm

`game/rhythm` keeps a rhythm game in time with its music: a beat map read from YAML, a music clock that tells the track's position and beat at any engine time, and a calibration of each device's audio and video latency.

```go
import (
	"github.com/nsgo-dev/game/packages/game/rhythm"
	"github.com/nsgo-dev/nsgo/audio"
)
```

The clock and the calibration run on the engine's goroutine, as `audio` does. They count on NSGo's audio clock: `audio.Now`, the time `audio.Voice.StartAt` starts a sound at, `audio.CanSchedule` and `audio.OutputLatency`.

## Beat maps

A beat map is a track's tempo map, kept as a YAML file next to the track:

```yaml
version: 1
title: Opening
offsetMs: 120
tempos:
  - beat: 0
    bpm: 120
  - beat: 62.5
    bpm: 140
meters:
  - beat: 0
    beatsPerBar: 4
  - beat: 64
    beatsPerBar: 3
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` |
| `title` | The track's name, for people reading the file |
| `offsetMs` | Where beat 0 plays, in milliseconds from the start of the audio; negative when the audio starts after beat 0 |
| `tempos` | Tempo changes in beat order, the first at beat 0; a change may start between beats |
| `meters` | Meter changes in beat order, the first at beat 0, each on a whole bar of the meter before it; none means 4 beats to a bar |

`ParseBeatMap` refuses unknown fields, a second YAML document, a fraction where a whole number belongs, and changes out of order or out of range (at most 2000 bpm, 10,000 changes and beat 1,000,000). `EncodeBeatMap` writes the fields in that fixed order with 2-space indentation, so a map loaded and saved again is unchanged.

`BeatMap.Timing` turns a map into a `Timing`, which converts between track time and beats and bars:

```go
m, err := rhythm.ParseBeatMap(data)
if err != nil {
	return err
}
timing, err := m.Timing()
if err != nil {
	return err
}
at := timing.Time(64)        // when beat 64 plays, from the start of the audio
beat := timing.Beat(at)      // 64
bar, inBar := timing.Bar(65) // bar 16, 1 beat in
```

Each beat's time is computed in `float64` from the start of its own tempo change, so the error does not grow with the length of the track: hours of music with thousands of tempo changes stay within a microsecond of the map, far inside the millisecond a rhythm game needs. A beat before 0 or after the last change keeps the tempo nearest it. A `Timing` never changes, so goroutines may share one.

## The music clock

A `Clock` reads a track's position at any engine time. Start it at the moment the track starts playing, and it extrapolates from the engine's clock, so it moves smoothly between a player's progress reports:

```go
clock := rhythm.NewClock(timing)
clock.SetOffsets(offsets) // from an OffsetStore

player := audio.NewPlayer(cx, audio.Source{URI: "asset:music/opening.m4a"}, audio.PlayerOptions{
	OnProgress: func(pos, _ time.Duration) { clock.Sync(pos, audio.Now(cx)) },
})
player.Play(func(err error) {
	if err == nil {
		clock.Start(audio.Now(cx), player.Position())
	}
})
```

For a track loaded as an `audio.Sound`, start it with `audio.Voice{StartAt: at}` and the clock with `clock.Start(at, 0)`. `Sync` moves the clock to the position a player reports only when the two differ by more than `Tolerance` (30 ms by default), as after a seek, a loop or a stall, so progress jitter never makes notes jump. `Pause` and `Resume` follow the player.

| Method | Use |
| --- | --- |
| `Track(now)` | The position the output plays, uncorrected |
| `Position(now)`, `Beat(now)` | What the player hears: the position less the audio offset; judge input by it |
| `Judge(at, beat)` | How early (negative) or late an input at `at` is for `beat` |
| `DrawPosition(now)`, `DrawBeat(now)` | Where a frame drawn now puts the hit line: the position plus the video offset less the audio offset |
| `BeatTime(beat)`, `TrackTime(pos)` | The engine time the output plays a beat, to pass to `audio.Voice.StartAt` for a hit sound or a metronome click; no offset applies, since both go through the same output |

## Calibration

Every device delays sound and picture differently: the output's buffer, Bluetooth headphones, the display and the input all add their part. Calibration measures what reaches the player, as two offsets:

- `Offsets.Audio`: how long after the engine starts a sound the player taps to it.
- `Offsets.Video`: how long after the engine draws a frame the player taps to what it shows.

A `Calibrator` matches taps with cues every `Interval` (750 ms by default) and takes the median of how late each is, after a few warm-up taps, with the median deviation as its `Spread`; a result is `Steady` when the spread is at most 30 ms. It keeps no clock of its own, so a test drives it with exact times.

A `Calibration` runs one on the engine. For `CalibrateAudio` it clicks at each cue, scheduled ahead with `audio.Voice.StartAt` where `audio.CanSchedule` reports the platform can, and otherwise played on the first frame at or after the cue, measuring from when it played. It seeds the tap matching with `audio.OutputLatency`, so a tap hundreds of milliseconds late over Bluetooth still finds its cue. For `CalibrateVideo` it plays nothing, and the screen flashes at each cue, measuring from the first frame that shows it.

`CalibrationScreen` is the screen a player calibrates on, drawn with `gameui` into a `core.Canvas`:

```go
store := rhythm.NewOffsetStore(cx, "rhythm.calibration")
device := rhythm.DeviceKey(cx)
cal := rhythm.NewCalibration(cx, rhythm.CalibrateAudio, click)
screen := rhythm.NewCalibrationScreen(cal, store, device)
screen.OnSave = func(o rhythm.Offsets) { clock.SetOffsets(o) }

core.Canvas{
	Draw:    func(d *core.Drawing, size core.Size) { screen.Draw(d, core.Bounds{Size: size}) },
	OnPress: func(ev core.PressEvent) { screen.OnPress(ev) },
}
```

Redraw the canvas every frame while the screen shows, as a `game.Loop` does. A press on Retry or Save presses the button, and any other press or key is a tap. Save is enabled once the result is steady. Set `Text` to translate the words.

## Per-device offsets

`OffsetStore` keeps each device's offsets as a YAML document in the app's storage (`core.Storage`) under one key:

```yaml
version: 1
devices:
  ios/iPhone15,2:
    audioMs: 182.5
    videoMs: 21
```

`DeviceKey` names the device by its OS and hardware model, such as `ios/iPhone15,2` or `web/unknown` where the platform names no model. NSGo does not report the audio output route, so a game that keeps separate offsets for headphones and speakers adds its own suffix to the key, such as `/bluetooth`, chosen by the player. Devices are written in key order, so the same offsets always encode to the same text. `Set` replaces a document it cannot read, such as one a later schema wrote, and fails with `*core.StorageFullError` when the storage is full, keeping what it held.
