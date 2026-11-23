package main

import "time"

// The playfield is a virtual canvas of virtualWidth by virtualHeight points, split into laneCount lanes that stars fall down.
const (
	virtualWidth  = 240
	virtualHeight = 320
	groundHeight  = 24
	laneCount     = 5
	laneWidth     = virtualWidth / laneCount

	basketY      = virtualHeight - groundHeight - basketHeight
	basketWidth  = 40
	basketHeight = 14

	basketSpeed = 480 // points a second

	itemSize  = 20
	fallSpeed = 90 // points a second
)

// laneX is the x of the middle of lane.
func laneX(lane int) float32 { return laneWidth*float32(lane) + laneWidth/2 }

// world is the game's state. Only step changes it, so tests drive it without a window.
type world struct {
	// basketLane is the lane the basket heads for, and basketX and prevX where it is now and was a step ago.
	basketLane     int
	basketX, prevX float32

	starLane     int
	starY, prevY float32
}

func newWorld() *world {
	w := &world{basketLane: laneCount / 2}
	w.basketX = laneX(w.basketLane)
	w.prevX = w.basketX
	w.respawn(0)
	return w
}

// respawn puts the star above the top of the playfield, in lane.
func (w *world) respawn(lane int) {
	w.starLane = lane
	w.starY = -itemSize
	w.prevY = w.starY
}

// step moves the world on by dt. move is -1 to send the basket a lane to the left, 1 a lane to the right and 0 to keep its lane; the basket glides to its lane at basketSpeed. The star falls, and once it has left the bottom it starts again two lanes over.
func (w *world) step(dt time.Duration, move int) {
	secs := float32(dt.Seconds())
	w.basketLane = min(max(w.basketLane+move, 0), laneCount-1)
	w.prevX = w.basketX
	target := laneX(w.basketLane)
	if d := target - w.basketX; d > 0 {
		w.basketX = min(w.basketX+basketSpeed*secs, target)
	} else if d < 0 {
		w.basketX = max(w.basketX-basketSpeed*secs, target)
	}

	w.prevY = w.starY
	w.starY += fallSpeed * float32(dt.Seconds())
	if w.starY > virtualHeight+itemSize {
		w.respawn((w.starLane + 2) % laneCount)
	}
}

// starAt is the star's y between the last step and the one before, alpha of the way from the older one.
func (w *world) starAt(alpha float32) float32 { return lerp(w.prevY, w.starY, alpha) }

// basketAt is the basket's x between the last step and the one before, as starAt is the star's y.
func (w *world) basketAt(alpha float32) float32 { return lerp(w.prevX, w.basketX, alpha) }

func lerp(a, b, t float32) float32 { return a + (b-a)*t }
