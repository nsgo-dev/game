# vn

`github.com/nsgo-dev/game/pkg/vn` plays a dialogue script as a visual novel: character portraits with expressions, a text box with name plates and a typewriter, choices, a backlog, auto and skip modes, and voice lines.

A `Presenter` runs a Yarn Spinner script with [`dialogue`](dialogue.md)'s `Runner` and a cast read from a YAML cast file. `Update` moves it on by [`game.Loop`](loop.md)'s fixed step and `Draw` draws it into a `Canvas`, so a scene plays alike every time and under `nsgotest`.

## Cast file

A cast file names the stage slots portraits stand in and the characters, each with a name plate and a portrait frame per expression:

```yaml
version: 1
slots:
  - name: left
    x: 0.25
  - name: center
    x: 0.5
  - name: right
    x: 0.75
characters:
  - id: aiko
    name: Aiko
    color: '#e07a5f'
    slot: left
    expressions:
      - name: neutral
        frame: aiko_neutral
      - name: smile
        frame: aiko_smile
```

- `version` must be `1`; unknown fields and a second YAML document are refused.
- A slot's `x` is the portrait's center as a fraction of the stage's width. Without `slots`, the cast uses left (0.2), center (0.5) and right (0.8).
- A character's `id` is what commands name; a line's speaker matches the `id` first, then `name`. `name` is the name plate's text (the `id` when empty) and `color` the plate's color as a CSS color.
- `slot` is where the portrait enters (the middle slot when empty) and `defaultExpression` the expression it enters with (the first when empty).
- Each expression's `frame` names a frame in the portrait atlas; `Cast.CheckFrames(atlas)` reports a missing one as the game loads. An expression may not share a slot's name, so `<<show>>` reads its arguments one way.

`ParseCast` reads and validates a file and `EncodeCast` writes one with fields in schema order and lists in their order, so a load and a save change nothing.

## Scripts

Lines steer the stage with tags after the line:

| Tag | Effect |
| --- | --- |
| `#expr:smile` | Sets the speaker's expression |
| `#at:center` | Moves the speaker's portrait to a slot |
| `#voice:aiko_001` | Plays a voice line |
| `#offstage` | Speaks the line without bringing the speaker's portrait on the stage |

A line by a cast member brings their portrait on the stage and marks it speaking; the others dim. A narration line has no name plate. A speaker not in the cast shows the name as written and takes no `#expr:` or `#at:` tag.

Three commands are the presenter's own; every other command reaches `dialogue.Options.OnCommand` as before, and all of them reach `dialogue.Options.Bus`:

```text
<<show aiko>>                 // enters in its slot with its default expression
<<show ren right doubt>>      // slot and expression in either order
<<expr aiko worry>>
<<hide aiko>>
<<hide>>                      // every portrait
```

A portrait shown in a slot another character holds sends that character off the stage. An unknown character, slot or expression is an error the call that ran it returns.

```yarn
title: Start
---
<<show aiko>>
<<show ren right doubt>>
Aiko: Welcome to the old observatory. #line:obs_01 #voice:aiko_01
Ren: It is smaller than I pictured. #line:obs_02
Aiko: Small, but the sky is the same size. #expr:smile #line:obs_03
-> Look through the telescope #line:obs_c1
    Aiko: Careful, the lens is old. #expr:worry #line:obs_05
-> Ask about the logbook #line:obs_c2
    Ren: Who kept this logbook? #at:center #line:obs_07
===
```

## Presenter

```go
p := vn.New(script, cast, vn.Options{
	Voices:       voices,
	AutoDelay:    time.Second,
	SkipInterval: 80 * time.Millisecond,
})
if err := p.Start("Start"); err != nil { ... }

loop := &game.Loop{Update: func(dt time.Duration) {
	if err := p.Update(dt); err != nil { ... }
}}
```

- `Advance` is the player's tap: it shows the rest of a line still appearing, or goes on. It stops skip mode, and returns `dialogue.ErrChoiceRequired` while choices wait.
- `Choices` lists the options waiting and `Choose(index)` picks one.
- `Line` is the line shown with its name plate's `Name` and `Color`, its `Voice`, and `Read`, whether the player had read it before.
- `Portraits` lists the portraits on the stage in the order to draw them, each with its slot, expression, frame and whether its character speaks.
- `Runner` gives the `dialogue.Runner` for variables and its save state.
- `Start` on a node the script lacks returns `dialogue.ErrUnknownNode` and leaves the stage and the line as they were.

`Options.CharsPerSecond` sets the typewriter's speed (40 when 0; negative shows a line at once).

## Auto and skip

`SetAuto(true)` goes on by itself once a line's text has appeared, its voice line has ended and `AutoDelay` has passed (1.5 s when 0), plus `AutoDelayPerRune` for each character. It waits at choices.

`SetSkip(true)` shows a line every `SkipInterval` (each `Update` when 0) at once and without its voice. It stops at choices, at the end, on `Advance` and on a line the player had not read before, which shows as usual; `SkipUnread` lets it run through those too.

A line counts as read once it has been shown. Its key is its `#line:` id, or the node, speaker and text of an untagged line, taken after string table lookup and `{$var}` interpolation, so an untagged line counts as unread again after a language switch or with a changed variable; give lines `#line:` ids to keep skip working across both. `ReadKeys` returns the keys for a save to keep and `MarkRead` restores them, so skip knows read lines across sessions.

## Backlog

`Backlog` returns the lines shown and the options chosen (`Choice` set), oldest first, up to `BacklogSize` entries (200 when 0; negative keeps none). `ReplayVoice(i)` plays entry `i`'s voice line again.

## Voice lines

A `VoicePlayer` plays a voice line by id and returns how long it plays, which auto mode waits for. Going on stops the line playing. `EffectVoices` plays lines through a [`sound.Mixer`](sound.md):

```go
voices := &vn.EffectVoices{Clips: map[string]vn.VoiceClip{}}
audio.LoadSound(cx, src, func(snd *audio.Sound, err error) {
	if err == nil {
		voices.Clips["aiko_01"] = vn.VoiceClip{Effect: mixer.Effect(snd, "voice", 1), Length: snd.Duration()}
	}
})
```

With `VoiceFromLineID`, a line without a `#voice:` tag plays its `#line:` id.

## Drawing

```go
style := vn.Style{Atlas: portraits, PortraitHeight: 300, InactiveAlpha: 0.55}

core.Canvas{Draw: func(d *core.Drawing, size core.Size) {
	if showLog {
		p.DrawBacklog(d, size, style, scroll)
		return
	}
	p.Draw(d, size, style)
}}
```

`Draw` draws the portraits standing on the stage's bottom edge, the text box with the name plate and the revealed text, and the choice buttons. A zero `Style` field uses a default; `Box` takes a `sprite.NineSlice` for the text box and `Font` a bitmap font for the text, which is platform text otherwise. `DrawBacklog` draws the backlog newest at the bottom, skipping the newest `scroll` entries; an entry taller than the stage is drawn alone from the top and cut off at the bottom. The choice buttons are drawn whenever choices wait, also in a node that opens with them before any line.

`Style.Layout(size, n)` returns the text box's and the choice buttons' bounds, and `ChoiceAt(size, style, point)` the choice under a tap:

```go
if i, ok := p.ChoiceAt(size, style, ev.Position); ok {
	err = p.Choose(i)
}
```

## Sample

`examples/observatory` plays a short scene with two characters whose portraits and voices are drawn and synthesized in code. Tap to go on, pick a choice, and use Auto, Skip and Log; watch the scene again and Skip runs through the lines already read.

```sh
go tool nsgo dev ./examples/observatory
```
