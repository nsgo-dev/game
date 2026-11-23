# cards

`game/cards` drags cards between the slots of a board in the game view and lays them out as fanned hands, decks, piles and grids. A card dropped on a slot that accepts it snaps into place; one dropped anywhere else flies back to where it came from. Every move is a `game/tween` advanced on `game.Loop`'s fixed steps, and the mouse and touch take the same path.

```go
import "github.com/nsgo-dev/game/packages/game/cards"
```

## Boards, slots and cards

A `Board` holds `Slot`s in the order they are added (later slots sit on top), and each slot holds an ordered list of `Card`s, the first at the bottom. A slot's `Rect` is both where its `Layout` places its cards and where a pointer drops onto it. The zero `Board` is ready to use.

```go
board := &cards.Board{CardSize: math2d.V2(63, 88), Curve: core.EaseOut}

hand := &cards.Slot{ID: "hand", Rect: math2d.Rect{X: 40, Y: 400, W: 560, H: 120},
	Layout: cards.Fan{Arc: 24, Sag: 14}}
play := &cards.Slot{ID: "play", Rect: math2d.Rect{X: 40, Y: 160, W: 560, H: 200},
	Layout: cards.Grid{Gap: math2d.V2(8, 8)}, Accepts: []string{"spell"}, Capacity: 5}
deck := &cards.Slot{ID: "deck", Rect: math2d.Rect{X: 620, Y: 400, W: 63, H: 88},
	Layout: cards.Stack{Offset: math2d.V2(0, -0.5), Depth: 8}, Grab: cards.GrabTop}
for _, s := range []*cards.Slot{hand, play, deck} {
	board.AddSlot(s)
}

for _, c := range opening {
	board.Add(&cards.Card{ID: c.Name, Tags: []string{c.Kind}, Data: c}, hand, -1)
}
```

- `Add` places a card at once, for the opening position; `Move` tweens it to another slot, as dealing or an undo does; `Remove` takes it off and closes the gap. None of them checks the slot's rules.
- `Card.Pose()` is where to draw a card now: its center `Pos` and its rotation `Rot` in radians. `Slot.Cards()`, `Slot.Top()`, `Card.Slot()` and `Card.Index()` say where it belongs.
- `Card.Locked` keeps a card in place, and `Slot.Grab` says which cards of a slot a pointer may take: `GrabAny` (a hand), `GrabTop` (a deck or a discard pile) or `GrabNone`.

## Layouts

| Layout | Arranges | Fields |
| --- | --- | --- |
| `Fan` | A hand in a row centered in the slot, tilting and dropping toward its ends | `Spacing` between centers (0 is 60% of the card's width), `Arc` in degrees for a full-width hand, `Sag` in points at its ends |
| `Stack` | A deck or a pile from the slot's top-left corner | `Offset` from each card to the next, `Depth` capping how many offsets show |
| `Grid` | Rows of cards from the slot's top-left corner | `Columns` (0 fits the slot's width), `Gap` between cards |

A fan closes up when the hand would not fit the slot, and its cards tilt by how far they sit from the middle, so a short hand stays nearly flat and a full one reaches `±Arc/2`. A layout also decides where a dropped card goes: a fan inserts it at the place nearest its center, so dragging within a hand reorders it, a grid at the cell under it and a stack on top. A game can write its own `Layout` with `Arrange` and `InsertIndex`.

After changing a slot's `Rect` or `Layout`, `board.Relayout(slot)` tweens its cards to their new places.

## Dragging with the mouse and touch

NSGo reports the primary mouse button as finger 0 of `core.TouchEvent` on the desktops and every pointer as a finger on the web, so one handler takes the mouse and touch alike. Attach the board to the view that holds the game's `Canvas`, and step it in the loop:

```go
view := core.View{Children: []core.Node{core.Canvas{Draw: draw}}}
board.Attach(&view) // chains any OnTouch the view already has

loop.Update = func(dt time.Duration) { board.Update(dt) }
```

Touch positions arrive in the view's coordinates; set `board.MapPoint` when the board is drawn through a camera or a scaled viewport, returning false for a point outside it. A game that reads pointers some other way calls `PointerDown`, `PointerMove`, `PointerUp` and `PointerCancel` with its own pointer IDs.

- A press takes the topmost card under it, following each card's rotation. The card starts following once the pointer moves more than `DragSlop` (6 points by default), keeping where it was held and turning upright; a release before that is a tap (`OnTap`).
- Each finger drags its own card, so two hands can play at once; a card already pressed by one pointer is not taken by another.
- On release, the card drops on the topmost slot under the pointer, or failing that the slot its body overlaps most. It snaps in over `SnapDuration` when `CanDrop` allows it: its own slot always does, another needs room under `Capacity`, one of the card's tags in `Accepts` when that is set, and `Slot.Accept` and `Board.Accept` when they are set. Otherwise, or over no slot, or when the touch is cancelled, it flies back to its place over `ReturnDuration`.
- `OnPickUp`, `OnDrop` (with the slots and indices it moved between) and `OnReturn` (with the slot that refused it, if any) report each step, and `DropTarget(card)` says during a drag which slot is under the card and whether it would accept it, for highlighting.

```go
board.Accept = func(c *cards.Card, from, to *cards.Slot) bool {
	return to != play || mana >= c.Data.(Spell).Cost
}
board.OnDrop = func(e cards.DropEvent) {
	if e.To == play {
		cast(e.Card.Data.(Spell))
	}
}
```

## Drawing

`DrawOrder(dst)` lists the cards bottom first: each slot's cards in slot order, then the cards being dragged or flying, in the order they were lifted. Draw each at its pose:

```go
order = board.DrawOrder(order[:0])
for _, c := range order {
	p := c.Pose()
	d.Save()
	d.Translate(p.Pos.X, p.Pos.Y)
	d.Rotate(p.Rot)
	drawCard(d, c, board.Size())
	d.Restore()
}
```

`DrawOrder`, `Update` and the pointer calls allocate nothing once the board's slices have grown.

## Board files

A board's card size and slots can be written as YAML and loaded with `ParseBoardDoc` and `LoadBoard`. Unknown fields are refused, the version must be `1`, and `EncodeBoardDoc` writes the fields in a fixed order, so a file read and written again is unchanged. `board.Doc()` turns a board built in code back into a `BoardDoc`; a slot's `Accept` function and the board's callbacks and timings stay in code.

```yaml
version: 1
card_size:
  w: 63
  h: 88
slots:
  - id: hand
    rect: {x: 40, "y": 400, w: 560, h: 120}
    layout:
      fan: {arc: 24, sag: 14}
  - id: deck
    rect: {x: 620, "y": 400, w: 63, h: 88}
    layout:
      stack:
        offset: {x: 0, "y": -0.5}
        depth: 8
    accepts: [spell]
    capacity: 40
    grab: top
```

Each slot names exactly one of `fan`, `stack` or `grid`; `grab` is `any` (the default), `top` or `none`. The encoder quotes the `y` key, which YAML 1.1 reads as a boolean, so other tools read the file the same way.
