# Agent Note: The cards2d template, a patience game on game/cards

Status: implemented

## Problem

Issue #796, under the per-genre template umbrella #786, asks for a `cards2d` template: a single-player card game with drag and drop, a deck, a hand and a discard pile, rules enforced in code and a win condition, built mainly on `game/cards`, `game/random` and `game/save`, playable with the keyboard, a gamepad and touch, and played to a win and a loss by an nsgotest test in CI. It has to be an original design, small enough to read in an afternoon, and it ships through the template infrastructure of #787: a module under `template/cards2d`, a byte-for-byte copy under `packages/game/cmd/game/_templates/projects/cards2d`, a registry entry, and a CI job that tests it with `GOWORK=off` against the `packages/game` version its `go.mod.tmpl` pins.

## Decision

- **Game**: Suit Ladders, a patience game of our own design. Each suit of a 36-card deck (four suits, ranks 1 to 9) has a ladder that takes its cards in rank order. The hand holds five cards; a card climbs when it is the next rank of its ladder, any card can be thrown on the discard pile, and an empty draw pile is refilled by turning the discard pile over, twice a game. Won when every ladder is complete, lost when the draw pile is empty, no redeal is left and no card of the hand fits. Both ends are reachable by rules alone, so the loss is not a timer or a forfeit.
- **The board is the only state**: every card sits in a `cards.Slot` of one `cards.Board`; `table.go` reads the rules off the slots and never keeps a parallel model. `Board.Accept` is `table.allows`, the rule every drop and every play or throw of a hand card passes through, whether by key, gamepad button, tap or drag, so a drag cannot put a card where a key cannot and vice versa. `play` and `throw` check `allows` before `Board.Move`, which ignores slot rules. Drawing goes through `canDraw` instead: `draw`, which the Space key, the gamepad's North button, the Draw button and a tap on the draw pile all call, checks that the game is on, the hand has room and the draw pile has a card or can be made from the discard pile, and only `draw` turns the discard pile over (`canRedeal`). Dragging the draw pile's top card onto the hand passes `allows`, whose deck-to-hand case leaves the hand's room to `Board.CanDrop`'s capacity check.
- **Data in YAML**: `board.yaml` is a `cards.BoardDoc` (slot IDs `deck`, `discard`, `hand`, `ladder-<suit>`, the hand's capacity, each ladder's `accepts` tag), and `rules.yaml` is the template's own versioned schema (suits and colors, ranks, redeals) read with `KnownFields(true)`, trailing documents refused, and a canonical 2-space encoding the tests compare byte for byte; `FuzzParseRules` checks that whatever parses round-trips. `newTable` cross-checks the two files: a ladder per suit that accepts exactly that suit, and no other slots.
- **Numbered deals**: deal *n* shuffles with `random.New(uint64(n))`, so deals are shareable and the tests name the deal a careful player wins (`winDeal`) and the one it loses (`loseDeal`) instead of searching at run time. `TestSomeDealsAreWon` keeps the rules between a tenth and nine tenths winnable for that player over 200 deals (63% at the time of writing), so a rules edit that makes the game hopeless or trivial fails.
- **Saves**: `save.Store` over `core.Storage` with two slots, `records` (next deal, wins, losses) and `table` (every pile by card ID), written after every move. A table is restored only after checking it is a position the rules can reach (each card once, ladders in order, hand within capacity, redeals in range, not over); anything else deals the next game. A slot from a newer save version makes the game read-only for storage, as the tutorial's records do.
- **Controls**: `game.yaml`'s input map binds the actions `left`, `right`, `play`, `discard`, `draw` and `deal` to keys and gamepad buttons, read with `Project.BindInput`; touch and the mouse go through `Board.Attach` on the view holding the Canvas, mapped through the `screen.Viewport` of `game.yaml`'s display. A Draw button covers turning the discard pile over on touch screens, where no card is left to tap.
- **Pin**: `go.mod.tmpl` pins `packages/game` at `af10f8ca` on main (v0.0.0-20261125054131-af10f8ca9ebe), the first commit with key and gamepad bindings in `game.yaml` (#908); the starter keeps its older pin.

## Alternatives considered

- **A deckbuilder against an opponent**: needs a card market, effects, an enemy and balance data before the first win; too large to read in an afternoon, and its loss is a health total rather than the deck running out, which shows less of `game/cards`' piles.
- **A known patience game (Klondike, FreeCell, Spider)**: refused by #786's rule of original designs.
- **A separate rules model with the board as a view**: two sources of truth that a template's reader would have to keep in step; reading slot lengths is cheap at 36 cards.
- **Play from the top of the discard pile**: the discard pile would become a second hand and a careful player would rarely lose; keeping discards dead until a redeal is what gives the redeal count its weight. This was judged, not measured; `TestSomeDealsAreWon` is the measurement for the rules as they are.
- **Searching for a winning deal at test time**: hides a rules change behind a different deal number; fixed deal numbers with a separate win-rate test fail loudly instead.
- **Keys bound in code**: the starter-era pin had no key or gamepad bindings in `game.yaml`; bumping the pin to main lets the template show the project file's input map instead.

## Consequences

- **Benefits**: one rule path for every input, deterministic deals and playthroughs, a save that survives restarts and refuses impossible positions, and data files a person edits and reviews as text.
- **Costs and known limits**: the playthroughs drive nsgotest's keyboard, gamepad and touch from a strategy that reads the table rather than replaying recorded input. `rules.yaml` is a format of the template, not of a package, so it is not listed in `docs/*/formats.md` or the JSON schemas. The template's docs page (`docs/*/template-cards2d.md`) is linked from `templates.md` but not named in the website's sidebar groups, which every template would edit at once; it lands in the sidebar's "other" group. `game/cards` has no keyboard focus or selection of its own, so the template keeps the selected hand index itself and draws its highlight.

## Verification

- `go tool nsgo test ./template/cards2d/template` and `go tool nsgo check -target linux,web ./template/cards2d/template` from the repository root.
- The CI job's steps by hand: copy the template out of the workspace, render `go.mod` for `example.com/cards2dgame`, and run `GOWORK=off go tool nsgo test ./...`.
- `go tool nsgo test ./template/cards2d/template -run XXX -fuzz FuzzParseRules -fuzztime 30s` from the repository root; `nsgo test` passes `-fuzz` through to `go test`.
- `cd packages/game && go tool nsgo test ./cmd/game` (registry, embedded copy, docs rows, `game new` for every template, CLI reference) and `go tool nsgo test .`.
- `go run ./tools/changelog check` and `bun run verify-notes` from the repository root.
