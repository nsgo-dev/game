# undo

`game/undo` records the reversible moves of puzzle and turn-based games, with undo and redo, and gives in-game editors a move limit, merging and a saved marker.

```go
import "github.com/nsgo-dev/game/pkg/undo"
```

Nothing in `undo` reads the clock, spawns goroutines or keeps package-level state.

## Moves

A `Move[S]` is one reversible change to the game's state `S`, usually a pointer to the board:

```go
type Move[S any] interface {
	Apply(state S)
	Revert(state S)
}
```

`Revert(s)` after `Apply(s)` must leave `s` exactly as it was. A move may store what its `Apply` changed in its own fields (a move with a pointer receiver) and read it back in `Revert`, such as whether a step pushed a crate:

```go
type Push struct {
	Dir string `yaml:"dir"`

	moved, pushed bool // recorded by Apply, read by Revert
}

func (p *Push) Apply(b *Board) {
	p.moved, p.pushed = b.Step(p.Dir)
}

func (p *Push) Revert(b *Board) {
	b.Unstep(p.Dir, p.moved, p.pushed)
}
```

Two helpers build moves without a type of their own:

- `Func[S]{Do: ..., Undo: ...}` is a move made of two functions.
- `Group[S]{a, b, c}` undoes and redoes several moves as one, such as a turn that moves a piece and captures another. It applies them in order and reverts them in reverse order. A nil or empty group is a move that changes nothing; `Do` panics, before applying anything, on a group that holds a nil move.

## History

`New[S, M](state)` returns an empty `History` over the game's starting state. `M` is the type of its moves: `Move[S]` for moves of several kinds, or one concrete type such as `*Push`, which also lets the history be saved.

```go
board := level.Board()
h := undo.New[*Board, *Push](board)

h.Do(&Push{Dir: "left"}) // applies the move and records it
h.Undo()                 // reverts the latest applied move
h.Redo()                 // applies the latest undone move again
h.Seek(0)                // back to the starting state
```

- `Do(m)` applies `m` and records it. A move made after an undo discards the moves that could have been redone.
- `Undo` and `Redo` report whether there was a move to revert or apply, and `CanUndo` and `CanRedo` say so beforehand, for a game's buttons.
- `Seek(n)` undoes or redoes moves until `n` are applied: `Seek(0)` restarts the level and `Seek(h.Len())` redoes every move.
- `Len` is the number of moves recorded, `Applied` the number applied, and `Moves` a copy of them in order.
- `Clear` forgets every move and keeps the state, which becomes the new starting state. A log saved after `Clear` holds only the later moves, so save the state at the `Clear` with it and restore onto that state.
- `OnChange`, when set, is called after each call that applies, reverts or forgets moves.

History is unlimited by default: every move is kept until `Clear`. Because each move reverts its own change, undoing every move returns the state to exactly the starting state, and redoing them returns it to exactly the latest one. The package's tests check this on random walks of hundreds of moves and fuzz arbitrary sequences of `Do`, `Undo`, `Redo`, `Seek` and `Clear` against a copy of the state after every move.

A move's methods must not call the `History` that holds it, and a `History` is not safe for concurrent use.

## In-game editors

A level maker, a base builder or a character creator edits for a long session, and three tools keep that usable.

`Limit`, when positive, bounds how many moves are kept: as a new move is recorded past the limit, the oldest applied moves are forgotten. A forgotten move's effect stays in the state, but it can no longer be undone, and `Seek` and a saved log count from the oldest kept move.

`DoMerge` is `Do` for a stream of small changes of the same kind, such as the steps of dragging a slider or an object: after applying the move it folds it into the latest applied move when that move is a `Merger` whose `Merge` accepts it, so the whole drag is one move to undo. `EndMerge` ends the run, so the next drag of the same slider starts a move of its own:

```go
type Slide struct {
	To  int `yaml:"to"`
	old int // recorded by Apply
}

func (s *Slide) Apply(v *int)  { s.old = *v; *v = s.To }
func (s *Slide) Revert(v *int) { *v = s.old }

// Merge folds the next step of the drag into this one: keep the old
// value, take the new target.
func (s *Slide) Merge(next *Slide) bool { s.To = next.To; return true }
```

`MarkSaved` records the current point and `Dirty` reports whether the state differs from it, for an "unsaved changes" mark. `Dirty` compares positions, so undoing back to the saved point reports clean; once `Limit` forgets moves past the saved point, or a new move discards the branch it was on, the history stays dirty until the next `MarkSaved`. `MarkSaved` also ends the merge run, so the first `DoMerge` after a save cannot rewrite the move the saved point rests on. `Version` identifies the current point and changes on every `Do`, `Undo`, `Redo`, `Seek` and `Clear`; a `DoMerge` that folds into the latest move keeps it.

## Saving a history

A history whose moves are one concrete type can be saved as a `Log` and restored, so a player who quits mid-level can still undo after loading. The log is YAML text:

```yaml
version: 2
applied: 2
forgotten: 0
moves:
  - dir: right
  - dir: down
  - dir: down
```

- `version` is `LogVersion` (2); `applied` is how many of the `moves` are applied, and the rest are the moves `Redo` can bring back. `forgotten` is how many moves a `Limit` forgot before the first kept one. Version 1 logs, from before `Limit`, are still read, and save again in the current form.
- `h.Log()` returns the log and `EncodeLog` writes it in this field order with 2-space indentation, so the same history always writes the same text.
- `DecodeLog[M](data)` reads it back, refusing a missing `version`, `applied` or `moves` key, a null `moves`, unknown fields (in the log or in a move), a newer version, an `applied` outside the moves, a negative `forgotten`, a null move and trailing YAML documents.
- `Restore(start, log)` returns a `History` over the starting state the log was recorded from, which is the state given to `New` or, after a `Clear`, the state at that `Clear`: it applies the first `applied` moves in order and keeps the rest for `Redo`. It refuses a log with forgotten moves, whose effects the starting state lacks: `Clear` the history first and save the state at the `Clear` with the log, so the next log forgets nothing.

Keep what a move records during `Apply` in unexported fields or fields tagged `yaml:"-"`, so the log holds only what the player chose; `Restore` applies the moves again, which records it anew. Store the log with the game's save, for example in a [save](save.md) slot or a file of its own.

```go
data, err := undo.EncodeLog(h.Log())
// ...later, with the board the history started from (the level's start, or the board saved at the last Clear):
log, err := undo.DecodeLog[*Push](data)
h, err = undo.Restore(level.Board(), log)
```
