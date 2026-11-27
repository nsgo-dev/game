package main

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/random"
)

const step = time.Second / 60

// quiet is a world that spawns nothing by itself, so a test places the items it needs.
func quiet() *world {
	w := newWorld(1)
	w.spawnIn = time.Hour
	return w
}

// drop puts an item of kind in lane, its middle at y, falling at fallSpeed.
func (w *world) drop(kind itemKind, lane int, y float32) {
	w.items = append(w.items, item{kind: kind, lane: lane, y: y, prevY: y, speed: fallSpeed})
}

// stepUntil steps w without moving the basket until it reports an event, and returns the events.
func stepUntil(t *testing.T, w *world) []event {
	t.Helper()
	for range 600 {
		w.step(step, 0)
		if len(w.events) > 0 {
			return w.events
		}
	}
	t.Fatal("no event in ten seconds")
	return nil
}

func TestCatchingAStarScores(t *testing.T) {
	w := quiet()
	w.drop(kindStar, w.basketLane, basketY-itemSize)
	if got := stepUntil(t, w); !slices.Equal(got, []event{eventCaught}) {
		t.Fatalf("events = %v, want a catch", got)
	}
	if w.score != 1 || w.lives != startLives || len(w.items) != 0 {
		t.Fatalf("score %d, lives %d, %d items; want 1, %d, 0", w.score, w.lives, len(w.items), startLives)
	}
}

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

func TestMissingAStarCostsALife(t *testing.T) {
	w := quiet()
	w.drop(kindStar, 0, groundY-itemSize)
	if got := stepUntil(t, w); !slices.Equal(got, []event{eventMissed}) {
		t.Fatalf("events = %v, want a miss", got)
	}
	if w.lives != startLives-1 || len(w.items) != 0 {
		t.Fatalf("lives %d, %d items; want %d, 0", w.lives, len(w.items), startLives-1)
	}
}

func TestDodgedRocksAreFree(t *testing.T) {
	w := quiet()
	w.drop(kindRock, 0, groundY-itemSize)
	for range 60 {
		w.step(step, 0)
		if len(w.events) > 0 {
			t.Fatalf("a dodged rock made events %v", w.events)
		}
	}
	if w.lives != startLives || len(w.items) != 0 {
		t.Fatalf("lives %d, %d items; want %d, 0", w.lives, len(w.items), startLives)
	}
}

func TestLastLifeEndsTheGame(t *testing.T) {
	w := quiet()
	w.lives = 1
	w.drop(kindStar, 0, groundY-itemSize)
	w.drop(kindStar, 4, -itemSize)
	if got := stepUntil(t, w); !slices.Equal(got, []event{eventMissed, eventOver}) {
		t.Fatalf("events = %v, want a miss and the end", got)
	}
	if !w.over || w.lives != 0 {
		t.Fatalf("over %v with %d lives, want over with 0", w.over, w.lives)
	}

	// Nothing moves once the game is over.
	y, x := w.items[0].y, w.basketX
	w.step(step, -1)
	if w.items[0].y != y || w.basketX != x || w.items[0].prevY != y || w.prevX != x {
		t.Fatal("the world moved after the game ended")
	}
}

func TestResetStartsAFreshGame(t *testing.T) {
	w := quiet()
	w.score, w.lives, w.over = 9, 0, true
	w.drop(kindRock, 1, 100)
	w.reset()
	if w.score != 0 || w.lives != startLives || w.over || len(w.items) != 0 || w.basketLane != laneCount/2 {
		t.Fatalf("reset left score %d, lives %d, over %v, %d items, lane %d", w.score, w.lives, w.over, len(w.items), w.basketLane)
	}
}

func TestGameSpeedsUpWithTheScore(t *testing.T) {
	w := quiet()
	if got := w.spawnInterval(); got != slowSpawn {
		t.Fatalf("spawn interval at 0 = %v, want %v", got, slowSpawn)
	}
	w.score = 100
	if got := w.spawnInterval(); got != fastSpawn {
		t.Fatalf("spawn interval at 100 = %v, want %v", got, fastSpawn)
	}
	w.spawn()
	if got := w.items[0].speed; got != maxFallSpeed {
		t.Fatalf("fall speed at 100 = %v, want %v", got, maxFallSpeed)
	}
}

func TestSpawnsFillEveryLaneWithStarsAndRocks(t *testing.T) {
	w := newWorld(7)
	var lanes [laneCount]int
	var rocks, stars int
	for range 400 {
		w.spawn()
	}
	for _, it := range w.items {
		lanes[it.lane]++
		if it.kind == kindRock {
			rocks++
		} else {
			stars++
		}
	}
	for lane, n := range lanes {
		if n == 0 {
			t.Fatalf("lane %d got no items in 400 spawns", lane)
		}
	}
	if rocks < 60 || rocks > 140 {
		t.Fatalf("%d rocks in 400 spawns, want about %d", rocks, int(400*rockChance))
	}
	if stars == 0 {
		t.Fatal("no stars spawned")
	}
}

// snapshot is what a run of the world ends in.
type snapshot struct {
	score, lives int
	over         bool
	items        []item
}

// play runs a world of seed for steps, moving the basket by a fixed pattern.
func play(seed uint64, steps int) snapshot {
	w := newWorld(seed)
	for i := range steps {
		w.step(step, i/37%3-1)
	}
	return snapshot{w.score, w.lives, w.over, slices.Clone(w.items)}
}

func TestSameSeedPlaysTheSameGame(t *testing.T) {
	random.AssertDeterministic(t, 3, func() snapshot { return play(42, 3600) })
	if a, b := play(1, 600), play(2, 600); slices.Equal(a.items, b.items) {
		t.Fatal("seeds 1 and 2 dropped the same items")
	}
}

func FuzzWorldKeepsItsRules(f *testing.F) {
	f.Add(uint64(1), []byte{0, 1, 2, 1, 0})
	f.Add(uint64(99), []byte{2, 2, 2, 2, 0, 0, 0})
	f.Fuzz(func(t *testing.T, seed uint64, moves []byte) {
		w := newWorld(seed)
		for i := range 1200 {
			move := 0
			if len(moves) > 0 {
				move = int(moves[i%len(moves)]%3) - 1
			}
			score := w.score
			w.step(step, move)
			if w.lives < 0 || w.lives > startLives {
				t.Fatalf("lives = %d", w.lives)
			}
			if w.score < score {
				t.Fatalf("score fell from %d to %d", score, w.score)
			}
			if w.basketLane < 0 || w.basketLane >= laneCount || w.basketX < laneX(0) || w.basketX > laneX(laneCount-1) {
				t.Fatalf("basket at lane %d, x %v", w.basketLane, w.basketX)
			}
			if w.over != (w.lives == 0) {
				t.Fatalf("over %v with %d lives", w.over, w.lives)
			}
			for _, it := range w.items {
				if it.y-itemSize/2 >= groundY {
					t.Fatalf("an item at y %v is under the ground", it.y)
				}
			}
		}
	})
}
