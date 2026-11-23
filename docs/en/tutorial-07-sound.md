# Tutorial 7: Sound

Play a sound for each catch, hit, miss and the end of a game: synthesize the effects in Go, load them with NSGo's `audio`, play them through a `sound.Mixer`, and give the player a mute switch the game remembers.

The finished code is in [`examples/tutorial/ch07-sound`](../../examples/tutorial/ch07-sound/).

## Effects made in Go

A game with recorded sounds puts WAV, AAC or MP3 files under `resources/` and loads them by `"asset:"` URI, as [Getting Started](getting-started.md#play-sound-with-soundmixer) shows. Star Catcher makes its four effects in code instead, in `audio.go`, so the tutorial needs no audio files. Each is a few tones or a burst of noise, encoded with `sound.EncodeWAV`:

```go
func makeSounds() (map[event]audio.Source, error) {
	clips := map[event][]float64{
		eventCaught: join(tone(880, 880, 0.06, 0.5), tone(1320, 1320, 0.09, 0.5)),
		eventHit:    thud(0.22, 0.7),
		eventMissed: tone(440, 220, 0.25, 0.45),
		eventOver:   join(tone(660, 660, 0.15, 0.45), tone(495, 495, 0.15, 0.45), tone(330, 300, 0.35, 0.45)),
	}
	sources := make(map[event]audio.Source, len(clips))
	for e, samples := range clips {
		wav, err := sound.EncodeWAV(samples, sampleRate, 1)
		if err != nil {
			return nil, err
		}
		sources[e] = audio.Source{Data: wav}
	}
	return sources, nil
}
```

`tone` glides a sine wave from one pitch to another; `thud` smooths random noise into a rumble, from a `random.Stream` with a fixed seed so it sounds the same every launch. Both shape the clip with a short fade in and a fade out to silence, so it neither clicks in nor stops short.

## A mixer

`sound.Mixer` sets how loud groups of sounds play, keeps the volumes in the app's storage, and plays nothing while the app is in the background. The game makes one, and loads each effect into it:

```go
s.mixer = sound.NewMixer(cx, "starcatcher.sound")

func (s *starCatcher) loadSounds(cx *core.Scope) {
	sources, err := makeSounds()
	if err != nil {
		return
	}
	for e, src := range sources {
		audio.LoadSound(cx, src, func(snd *audio.Sound, err error) {
			if err == nil {
				s.effects[e] = s.mixer.Effect(snd, effectsGroup, 4)
			}
		})
	}
}
```

`audio.LoadSound` has the platform decode the sound once, and calls back when it has. `mixer.Effect` puts it in the `"effects"` group, playing at most four copies at once. A sound that fails to load stays silent rather than stopping the game.

The world's events from [chapter 5](tutorial-05-rules.md) say what to play:

```go
for _, e := range s.world.events {
	if fx := s.effects[e]; fx != nil {
		fx.Play(audio.Voice{Volume: 1})
	}
}
```

The effects play from the fixed step, so they are in time with what the world did, and do not play while the game is paused.

## A mute switch

The mixer's master volume is the switch. It is stored under `starcatcher.sound.master`, so a game muted last time starts muted, and reading it in a view subscribes the view to it:

```go
func (s *starCatcher) muted() bool { return s.mixer.Master() == 0 }

func (s *starCatcher) toggleSound() {
	master := float32(0)
	if s.muted() {
		master = 1
	}
	_ = s.mixer.SetMaster(master)
}

func (s *starCatcher) soundButton() core.Node {
	return buttonWith("Sound", s.toggleSound, func() string {
		if s.muted() {
			return "Sound: off"
		}
		return "Sound: on"
	})
}
```

The button is on the title screen and in the pause menu. `SetMaster` fails only when the storage is full, and then the sound stays as it was.

On the web, browsers play no sound before the page's first user gesture. The first sound in Star Catcher follows a press of Start, so it is always allowed.

## Test the sound

The test platform plays sounds only in an environment that has audio, and decodes only the sources the test gives lengths to. `withSounds` sets both up:

```go
func withSounds(t *testing.T) (nsgotest.Option, map[event]audio.Source) {
	t.Helper()
	sources, err := makeSounds()
	if err != nil {
		t.Fatal(err)
	}
	return func(a *nsgotest.App) {
		nsgotest.Environment(core.Environment{Audio: true})(a)
		a.AudioDurations = map[string]time.Duration{}
		for _, src := range sources {
			a.AudioDurations[string(src.Data)] = time.Second / 2
		}
	}, sources
}
```

`app.Sounds` then lists every sound the game loaded, with each time it played. The tests catch a star and a rock and check that the catch, hit and end sounds each played once and the miss sound not at all; start a second app with `nsgotest.Storage` holding a master volume of 0 and check that it starts muted and plays nothing; and decode every effect with `sound.DecodeWAV` to check that it is short and ends in silence.

## Next

The best score is forgotten when the app closes. [Chapter 8](tutorial-08-save.md) keeps it.
