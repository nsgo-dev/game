# The cards2d template

`cards2d` is Suit Ladders, a single-player card game of an original design. Each suit has a ladder that climbs from 1 to 9. The player holds a hand of five cards, draws from a draw pile, climbs a ladder with a card that is the next rank there and throws the others on a discard pile, which can be turned over into a new draw pile twice a game. The game is won when every ladder reaches 9 and lost when the draw pile is empty, cannot be dealt again, and no card in the hand fits a ladder.

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template cards2d mycards
cd mycards
go tool nsgo dev
```

`nsgo init -template github.com/nsgo-dev/game/template/cards2d` writes the same project, as [Templates](templates.md) describes.

## What it shows

- [`game/cards`](cards.md): a `Board` loaded from `board.yaml`, cards dragged with the mouse or a finger and dropped where the rules allow, the others flying back, and the board's `Accept` as the one rule every key, button, tap and drop goes through.
- [`game/random`](determinism.md): deal *n* is shuffled by a `random.Stream` seeded with *n*, so every player's deal *n* is the same game and the tests can name the deals they win and lose.
- [`game/save`](save.md): the game in progress and the record of wins and losses kept in `core.Storage` after every move, checked on load so a damaged or impossible save deals a new game, and left alone when a newer version of the game wrote it.
- [`game/project`](project.md) and [`game/input`](input.md): `game.yaml`'s input map binds keys and gamepad buttons to the actions `left`, `right`, `play`, `discard`, `draw` and `deal`, and `Project.BindInput` reads it into `input.Actions`.
- [`game/screen`](screen.md): the table is laid out in a virtual resolution of 360 by 640 points and scaled into any window, with the pointer mapped back through the same viewport.
- A fixed-step `game.Loop`, card art drawn by code on a `core.Canvas`, and native views for the counts and the buttons.

## Controls

| | Keyboard | Gamepad | Touch and mouse |
| --- | --- | --- | --- |
| Select a card of the hand | Left, Right (A, D) | D-pad | Tap a card that does not fit |
| Climb with it | Enter or Up | South | Tap it, or drag it onto its ladder |
| Throw it away | X or Down | West | Drag it onto the discard pile |
| Draw, or turn the discard pile over | Space | North | Tap the draw pile, drag its top card into the hand, or press Draw |
| New deal | N | Start | New deal, or Next deal once a game is over |

Starting a new deal before a game is over counts as a loss.

## How it is organised

| File | What it holds |
| --- | --- |
| `game.yaml` | The project file: the window, the display of 360 by 640 points, the input map and the export presets. |
| `rules.yaml`, `rules.go` | The deck: its suits and their colors, the highest rank and the number of redeals, read with unknown fields refused and checked to round-trip unchanged. |
| `board.yaml` | The table as a `cards.BoardDoc`: a ladder per suit named `ladder-<suit>` that accepts only that suit, the draw pile, the discard pile, and the hand with its capacity. |
| `table.go` | The rules on the board: dealing, drawing, climbing, throwing away, redealing and judging the win and the loss. |
| `save.go` | The save slots `records` and `table`, and the checks a saved game passes before it is restored. |
| `game.go` | `Root`, the loop and the actions of the input map. |
| `render.go`, `hud.go` | The card art and the table drawn through the viewport, and the native views over it. |
| `*_test.go` | Playthroughs with the keyboard, a gamepad and touch to a win and a loss, and tests of the rules, the saves and the data files, with a fuzz test of `rules.yaml` and a benchmark of a whole game. |

## Where to change it

- The deck and the difficulty are in `rules.yaml`. `TestSomeDealsAreWon` fails when a careful player, as the tests play, wins fewer than a tenth or more than nine tenths of 200 deals, and `winDeal` and `loseDeal` in `table_test.go` are the deals the playthroughs win and lose; after changing the rules, pick them again.
- The table's layout is in `board.yaml`, in the display's coordinates. `newTable` refuses a board that lacks a slot the rules need.
- The moves are in `table.go`: `allows` says which card may go where, and `judge` when a game is won or lost.
- The controls are in `game.yaml`'s input map, which `go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check` validates.
- The card art is `drawCard` and `drawSuit` in `render.go`.

## Limits

The playthroughs drive the game with nsgotest's keyboard, gamepad and touch from a strategy that reads the table, rather than recording a human player; [`game/replay`](replay.md) records and replays real input when a game needs that.
