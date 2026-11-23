# Tutorial 5: Rules

Turn the falling star into a game: drop stars and rocks from a seeded `random.Stream`, decide what the basket catches with `physics` boxes, keep score and lives, end the game with the last life, and prove that the same seed always plays the same game.

The finished code is in [`examples/tutorial/ch05-rules`](../../examples/tutorial/ch05-rules/).

## The rules

All of the rules are constants at the top of `world.go`, so tuning the game means changing numbers in one place:

```go
const (
	startLives   = 3
	rockChance   = 0.25
	fallSpeed    = 90  // points a second, at a score of 0
	fallPerScore = 6   // points a second more for each star caught
	maxFallSpeed = 220 // points a second
	firstSpawn   = 600 * time.Millisecond
	slowSpawn    = 1100 * time.Millisecond
	fastSpawn    = 400 * time.Millisecond
	spawnPerStar = 25 * time.Millisecond
)
```

The world now holds a list of falling items, each a star or a rock, and the score, the lives and whether the game is over:

```go
type item struct {
	kind     itemKind
	lane     int
	y, prevY float32
	speed    float32
}

type world struct {
	basketLane     int
	basketX, prevX float32

	items   []item
	rng     *random.Stream
	spawnIn time.Duration

	score, lives int
	over         bool
	// events are what happened in the last step, oldest first.
	events []event
}
```

## Seeded spawns

`random.Stream` is the game library's random number generator. Its numbers depend only on its seed, the same on every operating system, architecture and Go version, which `math/rand` does not promise. Every spawn draws from it:

```go
func (w *world) spawn() {
	lane := w.rng.Intn(laneCount)
	kind := kindStar
	if w.rng.Chance(rockChance) {
		kind = kindRock
	}
	speed := min(float32(fallSpeed+fallPerScore*w.score), maxFallSpeed)
	w.items = append(w.items, item{kind: kind, lane: lane, y: -itemSize, prevY: -itemSize, speed: speed})
}

func (w *world) spawnInterval() time.Duration {
	return max(slowSpawn-spawnPerStar*time.Duration(w.score), fastSpawn)
}
```

The game seeds the world from the clock, so each launch plays differently, while a test passes a seed of its own:

```go
func Root(cx *core.Scope) core.Node {
	return newStarCatcher(cx, uint64(cx.WallTime().UnixNano())).view()
}
```

## Catching, missing and the end

Each step moves every item, then sorts out what it touched. `physics.TestBoxBox` reports whether two boxes overlap; an item's box is a little smaller than its picture, so a catch needs more than a touch of a point:

```go
basket := w.basketBox()
kept := w.items[:0]
for _, it := range w.items {
	it.y += it.speed * secs
	switch {
	case physics.TestBoxBox(basket, it.box()).Hit:
		if it.kind == kindStar {
			w.score++
			w.events = append(w.events, eventCaught)
		} else {
			w.lives--
			w.events = append(w.events, eventHit)
		}
		continue
	case it.y-itemSize/2 >= groundY:
		if it.kind == kindStar {
			w.lives--
			w.events = append(w.events, eventMissed)
		}
		continue
	}
	kept = append(kept, it)
}
clear(w.items[len(kept):])
w.items = kept

if w.lives <= 0 {
	w.lives = 0
	w.over = true
	w.events = append(w.events, eventOver)
}
```

- Filtering into `w.items[:0]` reuses the slice's array, so a step allocates nothing once the list has grown.
- An item moves less than four points a step at most, less than the basket is tall, so no star can pass through the basket between two steps.
- `events` record what happened in the step. The screen reads them to update the score now, and [chapter 7](tutorial-07-sound.md) plays a sound for each.

Once the game is over, `step` moves nothing until `reset` starts the next game.

## The score on screen

The score and lives are native `core.Text` views over the canvas. Each reads a signal, which the game sets from the world after a step that had events:

```go
core.Text{TestID: "score", ContentFunc: func() string { return fmt.Sprintf("Score: %d", s.score.Get()) }, Style: label},
core.Text{TestID: "lives", ContentFunc: func() string { return fmt.Sprintf("Lives: %d", s.lives.Get()) }, Style: label},
```

Drawing a falling item takes a sprite each: the twinkling star or the rock, a new frame painted into the atlas. The game keeps a slice of sprites and reuses it from frame to frame. When the game ends, a `core.Show` shows a panel until the player taps or presses Enter, which starts the next game.

## Test the rules

The rules' tests set up exactly the case they test. `quiet` makes a world that spawns nothing by itself, and `drop` places an item:

```go
func TestCatchingARockCostsALife(t *testing.T) {
	w := quiet()
	w.drop(kindRock, w.basketLane, basketY-itemSize)
	if got := stepUntil(t, w); !slices.Equal(got, []event{eventHit}) {
		t.Fatalf("events = %v, want a hit", got)
	}
	if w.score != 0 || w.lives != startLives-1 {
		t.Fatalf("score %d, lives %d; want 0, %d", w.score, w.lives, startLives-1)
	}
}
```

`random.AssertDeterministic` runs a whole minute of play three times from scratch and fails if any run ends differently from the first:

```go
func TestSameSeedPlaysTheSameGame(t *testing.T) {
	random.AssertDeterministic(t, 3, func() snapshot { return play(42, 3600) })
}
```

A fuzz test, `FuzzWorldKeepsItsRules`, plays twenty seconds of any seed and any moves, and checks after every step that lives stay between 0 and 3, the score never falls, the basket stays on the playfield and nothing falls through the ground. `nsgo test` runs its seed cases; to search for more, pass Go's fuzzing flags after `--`:

```sh
go tool nsgo test ./ch05-rules -- -run '^$' -fuzz FuzzWorldKeepsItsRules -fuzztime 1m
```

## Next

The game starts the moment the app opens and cannot be paused. [Chapter 6](tutorial-06-screens.md) adds a title screen, a pause menu and a proper end.
