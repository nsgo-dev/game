# The fighting game template

`fighting2d` is a one-on-one 2D fighting game to start a fighter from: two original characters, special moves from motion inputs, cancels and combos, guarding high and low, best-of-three rounds on a timer, a CPU opponent, and a second player on the same keyboard, on a second game controller or, against the CPU, on touch. It is built mainly on [fighting](fighting.md), which recognizes the inputs, and [framedata](framedata.md), which resolves the hits, with [input](input.md), [sound](sound.md), [screen](screen.md) and the [placeholder pack](placeholder.md)'s sounds.

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template fighting2d myfighter
cd myfighter
go tool nsgo dev
```

`nsgo init -template github.com/nsgo-dev/game/template/fighting2d` writes the same project ([Templates](templates.md)).

## What it shows

- **Motion inputs and combos.** Each fighter's move set (`data/<fighter>.moves.yaml`) is a `fighting.MoveSet`: Kestrel's gale is a quarter circle forward, its talon a dragon punch, Basalt's ram a charge back then forward, and both have a double-tap dash or backstep. A `fighting.Recognizer` per player reads the input every step, mirrored as the fighter turns, and its combos name the strings that land.
- **Frame data is the game and the art.** Every move's hurtboxes, hitboxes, damage, hitstun, blockstun, knockback and cancel windows are a `framedata.Doc` (`data/<fighter>.frames.yaml`), stepped by a `framedata.World` that finds the hits. The fighters are drawn from the same boxes, the first hurtbox as the head, so what is seen is what can hit and be hit; F1 shows the boxes themselves in debug builds.
- **Guarding.** Holding back while the opponent attacks within reach, or a projectile closes in, guards; holding down-back guards crouching. Lows must be guarded crouching and overheads standing, and a guarded special still deals chip damage.
- **Rounds.** A round ends on a knockout or when the 60 second timer runs out, when the fighter with the larger share of its health left wins it, so the sturdier fighter gains nothing from its larger health bar. A double knockout or a time-up with equal shares is a drawn round, which counts as a win for both fighters: the first to two rounds wins the match, so a draw after one fighter has won a round ends the match in that fighter's favour, and a draw that brings both to two wins makes the match a draw. Hitstop, combo damage scaling and pushback against the wall are rules in `data/match.yaml`.
- **A CPU opponent** that decides every few steps from the distance, the opponent's attacks and jumps, and plays its decisions as input through the same recognizer and rules as a person, from per-fighter input scripts, with a seeded random source so a match replays the same way.
- **Two local players.** Both players' keys, game controller buttons and sticks are one input map in `game.yaml`, its actions prefixed `p1_` and `p2_`. Each player has an `input.Actions` of its own; with two controllers connected, the first goes to player 1 and the second to player 2. Touch gives player 1 an on-screen stick and two attack buttons, whose regions are also in `game.yaml`.
- **Tests that play it.** `nsgotest` playthroughs lose a match against the CPU by standing still, win one from the keyboard with special moves, guards and combos, win a versus match with two game controllers, and play with touch; unit tests check each rule and that every data file is canonical.

## Controls

| | Player 1 | Player 2 | Game controller | Touch |
| --- | --- | --- | --- | --- |
| Move, jump, crouch | W A S D | Arrows | D-pad or left stick | A stick on the left half of the screen |
| Light | J | . | X / Square, A / Cross | L |
| Heavy | K | / | Y / Triangle, B / Circle | H |
| Start, pause | Enter | Right Shift | Start | Pause |

On the title screen, each player's left and right pick a fighter, player 1's Start fights the CPU and player 2's starts a versus match. Escape pauses, and on the result screen Enter asks for a rematch and Escape goes back to the title screen. Against the CPU, player 1 may use either side of the keyboard.

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: the window, the 480 by 270 virtual resolution, both players' input map, the sound bus and the export presets. |
| `data/match.yaml` | The rules, the CPU's tuning and the roster. For each fighter: its name, colors, health, speeds, its two data files, and what frame data does not say about some moves: their hit level, whether they are special, how they move the fighter, the projectile they throw, and the CPU's input scripts. |
| `data/<fighter>.moves.yaml` | The fighter's move set (`fighting`). |
| `data/<fighter>.frames.yaml` | The fighter's frame data (`framedata`). |
| `data.go` | Reads and checks the data; `match.yaml`'s format, versioned, refusing unknown fields and written back unchanged, is defined here. |
| `match.go` | A match: rounds, the timer, hits and damage, projectiles and spacing, stepped with both players' input and knowing nothing of devices or the screen. |
| `player.go` | One fighter: turning recognized moves into frame data moves standing, crouching or in the air, stances, guards, walking, jumping and falling. |
| `cpu.go` | The CPU opponent. |
| `controls.go` | Both players' input devices. |
| `game.go`, `hud.go`, `draw.go` | The loop and the screens, the native HUD and menus, and the Canvas drawing. |

A move named in a move set plays the frame data move of the same name, `crouch_<name>` while crouching if the frame data has one, and `air_<name>` in the air. The stance moves `idle`, `walk`, `crouch`, `jump`, `guard`, `crouch_guard`, `hurt` and `ko` loop, and every fighter has them; the game checks all of this when it loads the data, and its tests do too.

## Where to change it

- **A move's speed, reach and damage**: its boxes in `data/<fighter>.frames.yaml`. Startup is the first frame a hitbox is active on, recovery the frames after the last, and `cancels` the windows where it may be cut short.
- **A move's input**: `data/<fighter>.moves.yaml`, such as a longer `window` for an easier motion or a `charge` for a charge move.
- **How a move moves, its hit level and its projectile**: its entry in `data/match.yaml`'s `moves`.
- **A new fighter**: a move set, frame data and an entry in `data/match.yaml`'s `fighters`, with the CPU scripts it plays that fighter with. The title screen offers every fighter of the roster.
- **The rules and the CPU's difficulty**: `data/match.yaml`'s `rules` and `cpu`, such as `roundsToWin`, `roundSeconds`, `hitstop` or the CPU's `block` chance and `reaction` time.
- **Keys and buttons**: the `input` of `game.yaml`.
- **Art**: `draw.go` draws shapes from the boxes; replace them with sprites chosen by the move and frame each fighter plays, which `framedata.Move.Animation` can name.

## What it leaves to the game

`framedata` resolves hits but leaves movement and hit levels to the game, so `data/match.yaml` adds them: `motion` moves a fighter on a range of a move's frames, `level` makes a move low or overhead, and `projectile` throws a looping move of the same frame data as a fighter of its own on the thrower's team. A game that grows throws, knockdowns or a super meter adds them the same way: frame data for the boxes, and the match file for the rest.
