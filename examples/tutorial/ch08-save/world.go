package main

import (
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/random"
)

// The playfield is a virtual canvas of virtualWidth by virtualHeight points, split into laneCount lanes that stars and rocks fall down.
const (
	virtualWidth  = 240
	virtualHeight = 320
	groundHeight  = 24
	groundY       = virtualHeight - groundHeight
	laneCount     = 5
	laneWidth     = virtualWidth / laneCount

	basketY      = groundY - basketHeight
	basketWidth  = 40
	basketHeight = 14
	basketSpeed  = 480 // points a second

	itemSize = 20
	// itemInset shrinks an item's box on each side, so a catch needs more than a touch of a point.
	itemInset = 3
)

// The rules: how many lives a game starts with, how often a rock falls instead of a star, and how falling speeds up and spawning quickens as the score grows.
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

// itemKind is what falls: a star to catch or a rock to dodge.
type itemKind uint8

const (
	kindStar itemKind = iota
	kindRock
)

// item is a star or a rock falling down its lane; y is its middle.
type item struct {
	kind     itemKind
	lane     int
	y, prevY float32
	speed    float32
}

// event is something that happened in a step, which the screen and later the speakers react to.
type event uint8

const (
	eventCaught event = iota // the basket caught a star
	eventHit                 // the basket caught a rock
	eventMissed              // a star reached the ground
	eventOver                // the last life went
)

// world is the game's state. Only step and reset change it, so tests drive it without a window, and the same seed and moves always play the same game.
type world struct {
	// basketLane is the lane the basket heads for, and basketX and prevX where it is now and was a step ago.
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

func newWorld(seed uint64) *world {
	w := &world{rng: random.New(seed)}
	w.reset()
	return w
}

// reset starts a new game. The random stream goes on, so each game falls differently.
func (w *world) reset() {
	w.basketLane = laneCount / 2
	w.basketX = laneX(w.basketLane)
	w.prevX = w.basketX
	w.items = w.items[:0]
	w.spawnIn = firstSpawn
	w.score, w.lives, w.over = 0, startLives, false
	w.events = w.events[:0]
}

// step moves the world on by dt. move is -1 to send the basket a lane to the left, 1 a lane to the right and 0 to keep its lane. Once the game is over, nothing moves until reset.
func (w *world) step(dt time.Duration, move int) {
	w.events = w.events[:0]
	w.prevX = w.basketX
	for i := range w.items {
		w.items[i].prevY = w.items[i].y
	}
	if w.over {
		return
	}
	secs := float32(dt.Seconds())

	w.basketLane = min(max(w.basketLane+move, 0), laneCount-1)
	target := laneX(w.basketLane)
	if d := target - w.basketX; d > 0 {
		w.basketX = min(w.basketX+basketSpeed*secs, target)
	} else if d < 0 {
		w.basketX = max(w.basketX-basketSpeed*secs, target)
	}

	w.spawnIn -= dt
	if w.spawnIn <= 0 {
		w.spawn()
		w.spawnIn += w.spawnInterval()
	}

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
}

// spawn drops a star, or now and then a rock, at the top of a random lane, falling faster the higher the score.
func (w *world) spawn() {
	lane := w.rng.Intn(laneCount)
	kind := kindStar
	if w.rng.Chance(rockChance) {
		kind = kindRock
	}
	speed := min(float32(fallSpeed+fallPerScore*w.score), maxFallSpeed)
	w.items = append(w.items, item{kind: kind, lane: lane, y: -itemSize, prevY: -itemSize, speed: speed})
}

// spawnInterval is the time to the next spawn, shorter the higher the score.
func (w *world) spawnInterval() time.Duration {
	return max(slowSpawn-spawnPerStar*time.Duration(w.score), fastSpawn)
}

func (w *world) basketBox() physics.Box {
	return physics.Box{X: w.basketX - basketWidth/2, Y: basketY, W: basketWidth, H: basketHeight}
}

func (it item) box() physics.Box {
	return physics.Box{
		X: laneX(it.lane) - itemSize/2 + itemInset,
		Y: it.y - itemSize/2 + itemInset,
		W: itemSize - 2*itemInset,
		H: itemSize - 2*itemInset,
	}
}

// at is the item's y between the last step and the one before, alpha of the way from the older one.
func (it item) at(alpha float32) float32 { return lerp(it.prevY, it.y, alpha) }

// basketAt is the basket's x between the last step and the one before, as item.at is an item's y.
func (w *world) basketAt(alpha float32) float32 { return lerp(w.prevX, w.basketX, alpha) }

// laneX is the x of the middle of lane.
func laneX(lane int) float32 { return laneWidth*float32(lane) + laneWidth/2 }

func lerp(a, b, t float32) float32 { return a + (b-a)*t }
