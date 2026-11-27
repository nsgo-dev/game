# Fighting Game

A one-on-one 2D fighting game built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game): two original fighters, special moves from motion inputs, combos, guards, rounds and a timer, against a CPU opponent or a second player on the same device.

It shows:

- Motion inputs, charge moves and combos recognized from a per-step input buffer (`fighting`), with move sets in YAML.
- Hitboxes, hurtboxes, hitstun, blockstun, cancels and knockback resolved once per step from frame data in YAML (`framedata`), which is also what the fighters are drawn from.
- Two players on one keyboard or on two game controllers, and touch for player 1 (`input`), from one input map in `game.yaml`.
- A CPU opponent that plays through the same input pipeline as a person.
- Sound effects from the CC0 placeholder pack (`placeholder`, `sound`), a fixed-step `game.Loop`, a virtual resolution (`screen`) and a native HUD.
- Headless `nsgotest` playthroughs that win and lose a match.

## Controls

| | Player 1 | Player 2 | Game controller | Touch |
| --- | --- | --- | --- | --- |
| Move, jump, crouch | W A S D | Arrows | D-pad or left stick | Left half of the screen: a stick |
| Light | J | . | X / Square, A / Cross | L |
| Heavy | K | / | Y / Triangle, B / Circle | H |
| Start, pause | Enter | Right Shift | Start | Pause |

Hold back, away from the opponent, to guard; hold down-back to guard low. A low (a crouching attack, Basalt's quake) must be guarded crouching and an overhead (a jumping attack) standing. Against the CPU, player 1 may use either side of the keyboard. On the title screen, each player's left and right pick a fighter, player 1's Start fights the CPU and player 2's starts a versus match; with two controllers connected, the first goes to player 1 and the second to player 2, and each player keeps its assigned controller throughout the match. If an assigned controller disconnects during a versus match, the fight pauses and prompts that player to reconnect it, press a button on a free controller, or continue on the keyboard. F1 shows the hitboxes and hurtboxes in debug builds.

| Fighter | Moves |
| --- | --- |
| Kestrel, quick | Gale, a projectile: down, down-forward, forward + light or heavy. Talon, a rising strike, invulnerable as it starts: forward, down, down-forward + light or heavy. Dash: forward, forward. |
| Basalt, sturdy | Quake, a low ground wave: down, down-forward, forward + light or heavy. Ram, a charging shoulder: hold back, then forward + light or heavy. Backstep: back, back. |

Light cancels into heavy and both into the special moves when they connect. A combo's later hits deal less damage, and a guarded special still deals a little.

## Running and testing

```sh
go tool nsgo dev                      # the desktop, reloading on save
go tool nsgo dev -target web          # the browser
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
go tool nsgo test ./...               # headless tests
go tool nsgo check -target macos,windows,linux,android,web
go tool game release                  # release builds (passes GOFLAGS=-tags=release to strip F1 hitboxes)
```

If you invoke `nsgo build` directly, pass `GOFLAGS=-tags=release go tool nsgo build ...` so release builds compile out the F1 hitbox overlay.

Edit the name and identifier in `game.yaml` before shipping, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/game project sync
go run github.com/nsgo-dev/game/cmd/game project check
```

## Layout

- `game.yaml`: the project file, with the window, the 480 by 270 virtual resolution, the input map of both players (keys, controller buttons and sticks, and the touch regions of the attack buttons), the sound bus and the export presets.
- `data/match.yaml`: the rules (rounds, timer, stage, gravity, hitstop, chip damage, combo scaling), the CPU opponent's tuning and the roster. For each fighter it names the move set and the frame data and adds what frame data does not say: a move's hit level, whether it is special, how it moves the fighter and the projectile it throws, and the CPU's input scripts.
- `data/<fighter>.moves.yaml`: the fighter's move set, the inputs `fighting` recognizes.
- `data/<fighter>.frames.yaml`: the fighter's frame data, the boxes and properties of every move, `framedata`'s format.
- `data.go`: reads and checks the data; `match.yaml`'s format is defined here.
- `match.go`: a match: rounds, the timer, hits, damage, projectiles and spacing, stepped with each player's input and knowing nothing of devices or the screen.
- `player.go`: one fighter: turning recognized moves into frame data moves standing, crouching or in the air, stances, guards, walking, jumping and falling.
- `cpu.go`: the CPU opponent.
- `controls.go`: the input devices of both players.
- `game.go`, `hud.go`, `draw.go`: the loop, the screens, the native HUD and the Canvas drawing.
- `*_test.go`: data checks, rules tests and `nsgotest` playthroughs.

## Where to change it

- A move's speed, reach or damage: its boxes in `data/<fighter>.frames.yaml`. A fighter is drawn from its boxes, its first hurtbox as its head, so the art follows.
- A move's input: `data/<fighter>.moves.yaml`. A move named there plays the frame data move of the same name, or `crouch_<name>` while crouching and `air_<name>` in the air.
- A new fighter: two data files and an entry in `data/match.yaml`'s `fighters`; the title screen lists it.
- The rules or the CPU's difficulty: `data/match.yaml`.
- Keys and buttons: `game.yaml`'s `input`, whose actions start with `p1_` or `p2_`.
- Art: replace `draw.go`'s shapes with sprites, keyed by the move and frame each fighter plays.

The sounds are the placeholder pack's, dedicated to the public domain (CC0 1.0); its `CREDITS.md` lists them.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
