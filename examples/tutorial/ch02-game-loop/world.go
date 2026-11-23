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

	itemSize  = 20
	fallSpeed = 90 // points a second
)

// laneX is the x of the middle of lane.
func laneX(lane int) float32 { return laneWidth*float32(lane) + laneWidth/2 }

// world is the game's state. Only step changes it, so tests drive it without a window.
type world struct {
	basketX float32

	starLane     int
	starY, prevY float32
}

func newWorld() *world {
	w := &world{basketX: laneX(laneCount / 2)}
	w.respawn(0)
	return w
}

// respawn puts the star above the top of the playfield, in lane.
func (w *world) respawn(lane int) {
	w.starLane = lane
	w.starY = -itemSize
	w.prevY = w.starY
}

// step moves the world on by dt: the star falls, and once it has left the bottom it starts again two lanes over.
func (w *world) step(dt time.Duration) {
	w.prevY = w.starY
	w.starY += fallSpeed * float32(dt.Seconds())
	if w.starY > virtualHeight+itemSize {
		w.respawn((w.starLane + 2) % laneCount)
	}
}

// starAt is the star's y between the last step and the one before, alpha of the way from the older one.
func (w *world) starAt(alpha float32) float32 { return w.prevY + (w.starY-w.prevY)*alpha }
