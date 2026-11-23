# fighting

`github.com/nsgo-dev/game/packages/game/fighting` buffers fighting-game input one fixed step at a time and recognizes motion inputs (quarter circles, dragon punches, charges, double taps), button presses, chords and combos described in a YAML move set.

## Numpad notation

Directions use numpad notation, relative to a character facing right:

```text
7 8 9     up-back    up    up-forward
4 5 6     back     neutral  forward
1 2 3     down-back  down  down-forward
```

`Direction` holds one digit, `DirectionOf(x, y)` builds one from signs, and `Mirror` swaps left and right. When a character faces left (`FaceLeft`), the recognizer mirrors every direction before matching, so `6` always means toward the opponent and the same move set works on both sides.

## Move sets

A move set is a versioned YAML file. Unknown fields are refused, and `Encode` writes a canonical form (block style, fields in schema order, motion entries as quoted strings), so parsing `Encode`'s output and encoding it again reproduces it byte for byte.

```yaml
version: 1
input:
  up: up
  down: down
  left: left
  right: right
  axisX: move_x
  axisY: move_y
moves:
  - name: lp
    press:
      - lp
  - name: hadoken
    motion:
      - "2"
      - "3"
      - "6"
    window: 12
    press:
      - lp
      - hp
    buffer: 8
    priority: 20
  - name: sonic_boom
    motion:
      - "6"
    charge:
      dirs: "147"
      steps: 40
      grace: 6
    press:
      - lp
    buffer: 6
    priority: 25
  - name: forward_dash
    motion:
      - "6"
      - "5"
      - "6"
    window: 12
    priority: 10
  - name: throw
    press:
      - lp
      - lk
    pressAll: true
    pressWindow: 2
    priority: 40
combos:
  - name: jab_fireball
    sequence:
      - lp
      - hadoken
    gap: 20
```

`input` names the actions and axes that make up the stick. Digital actions add ±1 to their axis and analog axes add their value; an axis whose total reaches `deadzone` (default `0.5`) points that way, so holding left and right together is neutral. `axisY` is positive down unless `yUp` is set.

Each move has a `name` and a `motion`, a `press`, or both:

| Field | Meaning |
| --- | --- |
| `motion` | Direction entries matched on distinct, increasing steps. An entry is a string of one or more digits, any of which satisfies it, so `"23"` accepts down or down-forward. |
| `window` | Most steps from the first to the last motion entry; only for motions of 2 or more entries. |
| `charge` | `dirs` held for at least `steps` consecutive steps, ending no more than `grace` steps before the step that precedes the first motion entry. |
| `press` | Button actions that finish the move; any one of them, pressed on the current step. |
| `pressAll`, `pressWindow` | Require every `press` button within `pressWindow` steps (a chord). |
| `buffer` | Most steps the finishing press may come after the last motion entry; needs both `motion` and `press`. |
| `priority` | Orders moves finished on the same step, highest first. |

All step counts are inclusive and at most `MaxSteps` (600). A motion without `press` finishes on the first step it fits, so holding the last direction does not repeat it. Inputs that finished a move cannot finish it again: a button pressed within `buffer` after a finished quarter circle is not a second fireball, and the next one needs a fresh motion. A move set may name up to 32 buttons.

A combo lists at least two moves; each must follow the previous one within `gap` steps, and any other move in between breaks it.

## Recognizing input

```go
set, err := fighting.LoadFile("moves.yaml")
if err != nil {
	return err
}
rec, err := fighting.NewRecognizer(set)
if err != nil {
	return err
}

loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		actions.Step()
		res := rec.Update(actions, player.Facing)
		if len(res.Moves) > 0 {
			player.Perform(res.Moves[0].Name)
		}
		for _, c := range res.Combos {
			hud.Announce(c.Name)
		}
	},
}
```

Call `Update` once per fixed step, after the source has stepped. `Result.Moves` holds every move finished on that step, highest priority first, so `Moves[0]` is the one to perform; only that move advances combos. Each `Match` carries the step its inputs started on and the step it finished. The slices are reused by the next call and allocate nothing.

`Feed(raw, held, facing)` records a step from an already decoded direction and button mask (bit `i` for `Buttons()[i]`), such as inputs from netcode. Recognition depends only on the frames fed, so a re-simulation from the same inputs gives the same results. For rollback, `AppendState(dst)` appends the recognizer's complete state (buffered frames and the progress of every move and combo) to a snapshot, and `LoadState` restores it on a recognizer built from the same move set before re-simulating; it refuses state from a different move set and leaves the recognizer unchanged.

The history itself is a `Buffer`, a ring of `Frame`s (absolute and relative direction, facing, held and newly pressed buttons) sized by `MoveSet.Lookback`, reachable through `Recognizer.Buffer` for input displays and training modes.

## Testing with replays

`Recognizer.Update` takes any `Source` (`Pressed` and `Axis`), which `*input.Actions`, `*replay.Recorder` and `*replay.Player` all satisfy, so recorded inputs from [replay](replay.md) drive it directly:

```go
recording, _ := replay.LoadFile("testdata/session.replay.json")
p := replay.NewPlayer(recording)
for !p.Done() {
	p.Step()
	res := rec.Update(p, fighting.FaceRight)
	// assert on res.Step and res.Moves
}
```

The package's own tests record scripted inputs this way and check every motion one step inside and one step outside its window, buffer, charge time, grace and combo gap.
