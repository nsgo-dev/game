# Suit Ladders

A single-player card game made with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game/packages/game`](https://github.com/nsgo-dev/game), from the game layer's `cards2d` template.

## How to play

Each suit has a ladder that climbs from 1 to 9, one rank at a time. Your hand holds five cards. A card in the hand climbs its suit's ladder when it is the next rank there; any card can be thrown on the discard pile to make room. Draw from the draw pile while the hand has room. When the draw pile runs out, drawing turns the discard pile over into a new one, twice a game. You win when every ladder reaches 9 and lose when the draw pile is empty, cannot be dealt again, and no card in the hand fits a ladder.

| | Keyboard | Gamepad | Touch and mouse |
| --- | --- | --- | --- |
| Select a card of the hand | Left, Right (A, D) | D-pad | Tap a card that does not fit |
| Climb with it | Enter or Up | South (A) | Tap it, or drag it onto its ladder |
| Throw it away | X or Down | West (X) | Drag it onto the discard pile |
| Draw, or turn the discard pile over | Space | North (Y) | Tap the draw pile, drag its top card into the hand, or press Draw |
| New deal | N | Start | New deal, or Next deal once a game is over |

Deal *n* shuffles the same way for every player, so a deal can be shared and replayed. Starting a new deal before a game is over counts as a loss.

## Layout

- `game.yaml` is the project file: the name, id and version, the window, the virtual resolution of 360 by 640 points the table is laid out in, the input map that binds keys and gamepad buttons to the actions `left`, `right`, `play`, `discard`, `draw` and `deal`, and the export presets. `project.go` embeds it as `Project`.
- `rules.yaml` is the deck: its suits and their colors, the highest rank and how many redeals a game has. `rules.go` reads and checks it.
- `board.yaml` is the table's layout as a `cards.BoardDoc`: a ladder per suit, the draw pile, the discard pile and the hand, with the hand's capacity and the slots that pointers may take cards from.
- `table.go` holds the rules on a `cards.Board`: dealing, drawing, climbing, throwing away, redealing, and the win and the loss. The board is the only record of where each card is.
- `save.go` keeps the game in progress and the record of wins and losses with `save.Store`, after every move, so a game survives closing the app.
- `game.go` is `Root`: a fixed-step `game.Loop` that reads the input map and steps the cards' moves. `render.go` draws the cards by code on a `core.Canvas` through the `screen.Viewport` of `game.yaml`, and `hud.go` lays the native counts and buttons over it.
- `*_test.go` play the game through `nsgotest` with the keyboard, a gamepad and touch to a win and a loss, check the rules, the saves and the data files, and fuzz the rules file.
- `nsgo.yaml` is NSGo's app metadata; its name, id, version and build targets follow `game.yaml`.

## Where to change it

- New rules, such as more ranks or fewer redeals: edit `rules.yaml`. `TestSomeDealsAreWon` keeps the game winnable for a careful player, and `winDeal` and `loseDeal` in `table_test.go` name the deals the playthroughs win and lose.
- A different table: move the slots in `board.yaml`, in the coordinates of `game.yaml`'s display. `newTable` refuses a board without the slots the rules need.
- Other moves, such as climbing from the discard pile: change `allows` in `table.go`, which is both the board's drop rule and the check behind every key and button, and `judge`, which decides when a game is over.
- Other controls: edit the input map in `game.yaml`, then run `go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check`.
- Card art: `drawCard` and `drawSuit` in `render.go`, or `sprite.LoadAtlas` once you have images.

Edit the name and identifier in `game.yaml` before shipping, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync
go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check
```

## Running and testing

```sh
go tool nsgo dev                      # this machine, reloading on save
go tool nsgo dev -target web          # the browser
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target android
go tool nsgo test ./...
go tool nsgo check -target macos,windows,linux,android,web
go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
```

Builds go to `dist/`.
