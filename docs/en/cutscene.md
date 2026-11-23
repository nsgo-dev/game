# cutscene

`game/cutscene` plays video cutscenes in a game, in a scene or between scenes. The video plays in NSGo's own video view (`core.Video`) over the game's view, the game's `Loop` and `Mixer` pause while it plays, the player can skip it, and the game is called back when it ends.

```go
import "github.com/nsgo-dev/game/packages/game/cutscene"
```

## Cutscene files

A cutscene is a `Cutscene`, and a game's author writes one as a YAML file:

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
| `version` | The file format's version, `1` (`cutscene.CurrentVersion`). |
| `source` | The video: an `asset:` path in the app's resources, a file path or a URL, as `core.Video` takes. Required. |
| `group` | The mixer group whose volume, times the master volume, the video plays at. Left out: the master volume alone. |
| `skippable` | Whether the player can skip it. Left out: `false`. |
| `skipAfter` | How far into the video a skippable cutscene must play before a skip works, written as Go writes durations (`500ms`, `2s`), so a press meant for the game just before it does not skip it at once. Left out: from the start. |
| `fit` | How the picture fits the screen: `contain` (the default, with bars), `cover` (cropped) or `stretch` (distorted). |
| `transparent` | Leave the screen behind the picture clear rather than black, so the game shows in the bars and through a video with an alpha channel where the platform keeps it. |

`cutscene.Parse(data)` reads a file and refuses unknown keys, a second YAML document, a version other than `1`, a missing source, an unknown fit and a negative or non-string `skipAfter`. `cutscene.Encode(c)` writes one with its keys in that fixed order and zero values left out, so a file reads and writes back unchanged. A cutscene made in code may leave `Version` at 0, which `Player.Play` ignores.

## Playing a cutscene

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

`done` receives a `Result`: its `Reason` (`Finished`, `Skipped`, `Stopped` or `Failed`), the `Position` the video reached as the platform last reported it, and for `Failed` the platform's `*core.MediaError` in `Err`. The loop and the mixer are back by the time `done` runs, so it can switch scenes or play the next cutscene. `Play` returns an error, and plays nothing, for a cutscene with no source, an unknown fit or a negative `skipAfter`; playing a cutscene while another plays stops the first, whose `done` is told `Stopped`. `Stop` ends the one playing, skippable or not. Disposing the player's scope stops a cutscene without calling `done`, and still sets the loop and the mixer back.

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

Leaving the scene some other way before the cutscene ends stops it without calling `done`.

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

When the platform pauses the video itself, for a call or headphones unplugged, a press or a skip key plays it on rather than skipping it.

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
