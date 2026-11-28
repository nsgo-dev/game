# cutscene

`game/cutscene` plays both pre-rendered video cutscenes and in-engine timeline cutscenes in a game, in a scene or between scenes. Video cutscenes play in NSGo's own video view (`core.Video`) over the game's view while pausing `Loop` and `Mixer`. Timeline cutscenes run deterministically on `game.Loop` fixed steps against a `sceneformat.BuiltScene` or `sceneformat.SceneDoc`, driving camera, actor, localized dialogue, sound effect and music tracks. Both kinds are skippable and call the game back when they end.

```go
import "github.com/nsgo-dev/game/pkg/cutscene"
```

## Cutscene files

A cutscene is a `Cutscene`, written in YAML as either version 1 (video only, `cutscene.MinVersion`) or version 2 (video or timeline, `cutscene.CurrentVersion`). Published JSON Schemas live at `https://game.nsgo.dev/schemas/cutscene.v1.json` and `https://game.nsgo.dev/schemas/cutscene.v2.json`.

### Video cutscenes (version 1 and 2)

```yaml
version: 1
source: asset:cutscenes/intro.mp4
group: cutscenes
skippable: true
skipAfter: 1.5s
fit: cover
```

| Key | Meaning |
| --- | --- |
| `version` | `1` (`cutscene.MinVersion`) or `2` (`cutscene.CurrentVersion`). |
| `kind` | Optional in version 2: `video` (default when `source` is set) or `timeline`. Forbidden in version 1. |
| `source` | The video: an `asset:` path in the app's resources, a project-relative path or a URL, as `core.Video` takes. Required for video cutscenes. |
| `group` | The mixer group whose volume, times the master volume, the video plays at. Left out: the master volume alone. |
| `skippable` | Whether the player can skip it. Left out: `false`. |
| `skipAfter` | How far into the cutscene a skippable cutscene must play before a skip works, written as Go writes durations (`500ms`, `2s`), so a press meant for the game just before it does not skip it at once. Left out: from the start. |
| `fit` | How the picture fits the screen: `contain` (the default, with bars), `cover` (cropped) or `stretch` (distorted). |
| `transparent` | Leave the screen behind the picture clear rather than black, so the game shows in the bars and through a video with an alpha channel where the platform keeps it. |

### Timeline cutscenes (version 2)

A version 2 timeline cutscene (`*.cutscene.yaml`) sets `kind: timeline`, `length` and ordered `tracks` of clips, each with a `start`, `duration` and optional `easing` (`linear`, `easeIn`, `easeOut`, `easeInOut`, `hold`):

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 12s
skippable: true
skipAfter: 1s
tracks:
  - id: camera
    name: Camera
    kind: camera
    clips:
      - id: cam1
        start: 0s
        duration: 1.6s
        easing: hold
        action: hold
        target: door
      - id: cam2
        start: 1.6s
        duration: 2.4s
        easing: easeInOut
        action: pan
        target: knight
      - id: cam3
        start: 4s
        duration: 8s
        action: follow
        target: knight
  - id: knight
    name: Knight
    kind: actor
    target: knight
    clips:
      - id: kn1
        start: 1s
        duration: 2.4s
        easing: easeOut
        action: walk
        fromX: 60
        toX: 220
        state: run
      - id: kn2
        start: 3.4s
        duration: 4.6s
        action: state
        state: idle
  - id: slime
    name: slime_01
    kind: actor
    target: slime_01
    clips:
      - id: sl1
        start: 6s
        duration: 800ms
        easing: easeIn
        action: drop
        fromY: -40
        toY: 276
      - id: sl2
        start: 6.8s
        duration: 5.2s
        action: patrol
        fromX: 420
        toX: 520
        speed: 40
  - id: dialogue
    name: Dialogue
    kind: dialogue
    clips:
      - id: dl1
        start: 3.6s
        duration: 2.4s
        line: intro.knight.01
  - id: sfx
    name: Sound
    kind: sound
    group: sfx
    clips:
      - id: sx1
        start: 200ms
        duration: 600ms
        source: audio/door_slam.wav
  - id: music
    name: Music
    kind: music
    group: music
    clips:
      - id: mu1
        start: 2s
        duration: 10s
        source: audio/cellar_theme.ogg
        fadeIn: 2s
```

| Track kind | Track fields | Clip actions & fields |
| --- | --- | --- |
| `camera` | `id`, `name`, `kind`, `clips` | `action: hold` (`target` or `toX`/`toY`, optional `offsetX`/`offsetY`, `zoom`), `action: pan` (`target` or `toX`/`toY`, optional `fromX`/`fromY`, `offsetX`/`offsetY`, `zoom`, `easing`), `action: follow` (`target`, optional `offsetX`/`offsetY`, `zoom`). |
| `actor` | `id`, `name`, `kind`, `target` (scene object ID), `clips` | `action: walk` (`toX`/`toY`, optional `fromX`/`fromY`, `state`, `easing`), `action: state` (`state`), `action: drop` (`toY`, optional `fromY`/`fromX`/`toX`, `state`, `easing`), `action: patrol` (`toX`/`toY`, optional `fromX`/`fromY`, `speed`, `state`, `easing`). |
| `dialogue` | `id`, `name`, `kind`, `clips` | `line` (required l10n key — text lives in the localization tables, never inline), optional `speaker`. |
| `sound` | `id`, `name`, `kind`, optional `group` (default `sfx`), `clips` | `source` (audio path or URI), optional `group`, `volume` (`[0, 1]`). |
| `music` | `id`, `name`, `kind`, optional `group` (default `music`), `clips` | `source`, optional `group`, `volume` (`[0, 1]`), `fadeIn`, `fadeOut`, `loop`. |

`cutscene.Parse(data)` reads both versions strictly, refusing unknown keys, a second YAML document, unsupported versions, timeline fields in version 1, mixing video and timeline fields, overlapping clips on non-sound tracks, non-finite numbers and negative or non-string durations. `cutscene.Encode(c)` writes canonical 2-space YAML (prefixing version 2 files with `SchemaCommentV2` and keeping version 1 output unchanged) so files round-trip byte for byte. A cutscene constructed in code may leave `Version` at 0.

## Playing a timeline cutscene

`NewTimelinePlayer(c, target)` runs a timeline cutscene deterministically on `game.Loop`'s fixed steps against a `*sceneformat.BuiltScene` (or `*sceneformat.SceneDoc`), resolving dialogue line IDs through an `*l10n.Catalog` and triggering sound/music cues:

```go
tl, err := cutscene.NewTimelinePlayer(c, cutscene.TimelineTarget{
	Scene:   builtScene,
	Catalog: catalog,
	Locale:  "en",
	Mixer:   mixer,
	OnActorState: func(actorID, state string) { setAnim(actorID, state) },
	OnSound:      func(clip cutscene.Clip, group string, vol float32) { playSFX(clip.Source, group, vol) },
	OnDone:       func(r cutscene.Result) { resumeGameplay() },
})
if err != nil {
	return err
}
tl.Play()

// Inside game.Loop.Update:
tl.Update(dt)
```

`tl.Seek(at)` scrubs directly to any timestamp in `[0, Length]` and evaluates actor, camera and dialogue states purely without firing sound, music or completion callbacks. `tl.Reset()` rewinds to time 0 and restores initial actor and camera positions.

To wire the same skip keys, press-to-skip, back handling, `CanSkip()`, reactive `Dialogue()` and between-scene transitions (`TimelineScene`) as video cutscenes, play through `Player`:

```go
_, err := player.PlayTimeline(c, cutscene.TimelineTarget{Scene: builtScene, Catalog: catalog}, func(r cutscene.Result) {
	resumeGameplay()
})
// Inside game.Loop.Update:
player.Step(dt)
```

Unlike video cutscenes, `PlayTimeline` leaves `Options.Loop` and `Options.Mixer` running so the scene continues drawing and playing cutscene audio while `Player.View()` captures skip input.

## Validation with `game check` and `tooling/refs`

`game check` (package `tooling/assetcheck`) validates every `*.cutscene.yaml` / `*.cutscene.yml` file in a project:

- Parses and validates the cutscene schema.
- Verifies that `scene` (when set) and every project-relative video or audio `source` exists.
- Verifies that every actor track `target` and camera clip `target` names an object in the referenced scene (including inherited and instanced scenes).
- Verifies that every dialogue clip `line` ID exists in the project's YAML localization string tables (`l10n.ParseYAML`), reporting missing IDs with file and line.

`tooling/refs` (`game mv` and `game index`) tracks `scene`, project-relative video `source` and timeline audio `source` paths in `*.cutscene.yaml` files so moving a scene or audio asset rewrites cutscene references in place.

## Playing a video cutscene

Make one `Player` for the game and mount its `View` over the game's view, as the last child of the view that holds the canvas:

```go
loop := &game.Loop{Update: update, Draw: draw}
loop.Run(cx)
mixer := sound.NewMixer(cx, "sound")
player := cutscene.NewPlayer(cx, cutscene.Options{Loop: loop, Mixer: mixer})

return core.View{Class: "flex-1", Children: []core.Node{
	core.Canvas{Class: "flex-1" /* ... */},
	player.View(),
}}
```

`View` shows nothing while no cutscene plays. `player.Play(c, done)` plays one, from a game's `Update` or anywhere else on the engine's goroutine:

```go
if bossDoorOpened {
	player.Play(bossIntro, func(r cutscene.Result) {
		startBossFight()
	})
}
```

While it plays:

- `Options.Loop` is paused (`Loop.SetPaused`), so `Update` stops and `Draw` goes on under the video. When the cutscene ends the loop is set back as it was, so one the game had paused stays paused.
- `Options.Mixer` is suspended (`Mixer.Suspend`, see [sound](sound.md)): effects stop and music pauses, then plays on from there.
- The display is kept awake, and the video pauses while the app is in the background and plays on as it returns.

`done` receives a `Result`: its `Reason` (`Finished`, `Skipped`, `Stopped` or `Failed`), the `Position` the video reached as the platform last reported it, and for `Failed` the platform's `*core.MediaError` in `Err`. The loop and the mixer are back by the time `done` runs, so it can switch scenes or play the next cutscene. `Play` returns an error, and plays nothing, for a cutscene with no source, an unknown fit or a negative `skipAfter`; playing a cutscene while another plays stops the first, whose `done` is told `Stopped`. If that `done` plays a cutscene of its own, that one plays instead, and `Play` returns `cutscene.ErrReplaced`, playing nothing and never calling its own `done`. `Stop` ends the one playing, skippable or not. Disposing the player's scope stops a cutscene without calling `done`, and still sets the loop and the mixer back.

## Between scenes

`player.Scene(c, done)` makes a cutscene a [scene](scene.md) of its own: it plays as the scene enters, and `done` switches on from it.

```go
intro, err := player.Scene(c, func(cutscene.Result) {
	scenes.Switch(level1, scene.Crossfade(500*time.Millisecond))
})
if err != nil {
	return err
}
scenes.Switch(intro, nil)
```

Leaving the scene some other way before the cutscene ends stops it without calling `done`. That holds when switching from one cutscene scene to another of the same player with a transition too, although the new scene enters before the old one exits.

## Skipping

A `skippable` cutscene is skipped, once it has played to `skipAfter`, by:

- a press on the video;
- a skip key, `Enter` or `Space` unless `Options.SkipKeys` names others (an empty slice for none);
- back: `Escape`, the back button or gesture, or the browser's back;
- `player.Skip()`, for a game that reads its own input.

Back is taken while any cutscene plays, so it does not navigate the game away under the video; on one that cannot be skipped it does nothing. `player.CanSkip()` reports whether a skip works now and, read in a view, updates it as that changes, so a game can show its own skip prompt over the video:

```go
core.Show{When: player.CanSkip, Then: func(*core.Scope) core.Node {
	return core.Text{Class: "absolute bottom-6 right-6 text-white", Content: "Skip ›"}
}}
```

When the platform pauses the video itself, for a call or headphones unplugged, a press or a skip key plays it on rather than skipping it. It stays paused until then, also when the window loses or regains focus; only returning from the background plays it on by itself.

## Audio routing

The platform's player decodes the video and plays its sound, which does not go through the game's mixer:

- The mixer's master volume times the cutscene's `group` volume sets how loud the video plays, and follows a settings menu as it changes; a volume of 0 mutes it.
- The mixer's bus effects (`EffectChain`) and anything else that processes samples do not apply to it.
- Nothing the game draws can go between the video's picture and the screen; only NSGo views over `player.View()` show on top of it.

Drawing decoded frames into the game, with the sound in the mixer, needs a frame-decoding API NSGo does not have yet.

## Testing with nsgotest

`nsgotest` reports what a platform's player would: `app.Play` loads the video, `app.Progress` plays it to a position (and to its end at its duration), `app.PauseVideo` and `app.PlayVideo` pause and play it as the platform does, and `app.Report` sends an `EventError`. The video's test ID is `cutscene`, or `Options.TestID`.

```go
app.Play("id:cutscene", core.VideoInfo{Duration: 3 * time.Second})
app.Progress("id:cutscene", 3*time.Second, 3*time.Second) // done gets Finished
```

## Captions for dialogue

`cutscene.NewCaptionBridge(player, captions)` mirrors the timeline's active dialogue line into an `a11y.CaptionTrack`, so cutscene lines get the player's caption settings in `CaptionsView`. Call its `Update` after `Player.Step` on each fixed update. It shows each line once with its speaker, and dismisses the caption when the dialogue clip ends or the next line replaces it, so the caption lasts as long as the clip and a replayed line shows again.

Signal tracks emit authored events when playback crosses their markers. Seeking never emits signals.
