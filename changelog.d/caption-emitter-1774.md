### Added

- `pkg/a11y`, `pkg/sound`, `pkg/cutscene`: `sound.Effect.SetCaption` reports the label and listener pan of every positional play (`PlayAt`, `PlayAt3D`, their `With` forms and positional `Cue` plays), `a11y.CaptionEmitter` turns those reports into directional `CaptionTrack` captions without repeating a label within `MinInterval`, and `cutscene.CaptionBridge` shows timeline dialogue on a `CaptionTrack` for as long as each line plays (#1774).
