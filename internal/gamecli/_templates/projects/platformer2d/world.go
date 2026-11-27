package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/platformer"
	"github.com/nsgo-dev/game/pkg/rooms"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// The player's collision box, in world points. The sprite is drawn 16 points square with its bottom center on the box's.
const (
	playerW float32 = 10
	playerH float32 = 14
)

// outcome is where a run stands.
type outcome uint8

const (
	// running means the run is still being played.
	running outcome = iota
	// won means the player reached the goal beacon.
	won
	// lost means the player ran out of lives.
	lost
)

// String returns "running", "won" or "lost".
func (o outcome) String() string {
	switch o {
	case won:
		return "won"
	case lost:
		return "lost"
	default:
		return "running"
	}
}

// spark is a collectible.
type spark struct {
	col, row int
	taken    bool
}

// checkpoint is a lamp that, once lit, is where the player comes back after being hurt.
type checkpoint struct {
	col, row int
	lit      bool
}

// world is one run of the level: the tile grid the player collides with, the player's body and controller, the sensors for spikes, sparks, checkpoints and the goal, the camera and the rooms it moves between, and the score. Step advances it by one fixed step; it knows nothing about devices or drawing.
type world struct {
	level *Level
	tile  float32
	cols  int
	rows  int

	// grid holds the solid and one-way tiles the controller moves the body against.
	grid *physics.Grid
	// sensors holds the player's body and a trigger per spike, spark, checkpoint and the goal; it has no gravity of its own, since the controller applies it.
	sensors physics.World
	body    *physics.Body
	ctrl    *platformer.Controller

	camera   tilemap.Camera
	director *rooms.Director

	sparks      []spark
	checkpoints []checkpoint
	goal        [2]int
	start       core.Point
	respawn     core.Point

	lives     int
	collected int
	state     outcome
	// facing is -1 when the player last moved left and 1 when right.
	facing int
	// steps counts the fixed steps played, frozen ones included.
	steps int
	// lastJump is the kind of jump the last step started, for the sprite and the tests.
	lastJump platformer.JumpKind

	// Set by the sensors during a step and applied after them, so a respawn never moves the body while the triggers are being tested.
	hurt, reached bool
}

// newWorld builds a run of d's level, its camera viewWidth by viewHeight world points.
func newWorld(d gameData, viewWidth, viewHeight float32) (*world, error) {
	l := d.level
	rows := l.rows()
	w := &world{
		level:  l,
		tile:   float32(l.TileSize),
		cols:   len(rows[0]),
		rows:   len(rows),
		lives:  l.Lives,
		facing: 1,
		camera: tilemap.Camera{Width: viewWidth, Height: viewHeight},
	}
	w.grid = physics.NewGrid(w.cols, w.rows, w.tile, w.tile)
	for r, row := range rows {
		for c := 0; c < len(row); c++ {
			switch row[c] {
			case glyphGround:
				w.grid.Set(c, r, physics.TileSolid)
			case glyphLedge:
				w.grid.Set(c, r, physics.TileOneWay)
			case glyphSpikes:
				w.addSensor(spikeBox(w.tile, c, r), func() { w.hurt = true })
			case glyphSpark:
				i := len(w.sparks)
				w.sparks = append(w.sparks, spark{col: c, row: r})
				w.addSensor(sparkBox(w.tile, c, r), func() { w.collect(i) })
			case glyphCheckpoint:
				i := len(w.checkpoints)
				w.checkpoints = append(w.checkpoints, checkpoint{col: c, row: r})
				w.addSensor(physics.Box{X: float32(c) * w.tile, Y: float32(r-1) * w.tile, W: w.tile, H: 2 * w.tile}, func() { w.light(i) })
			case glyphGoal:
				w.goal = [2]int{c, r}
				w.addSensor(physics.Box{X: float32(c) * w.tile, Y: float32(r-1) * w.tile, W: w.tile, H: 2 * w.tile}, func() { w.reached = true })
			case glyphStart:
				w.start = w.standingAt(c, r)
			}
		}
	}
	if err := checkRooms(d.rooms, float32(w.cols)*w.tile, float32(w.rows)*w.tile, w.start); err != nil {
		return nil, err
	}
	w.respawn = w.start
	w.body = physics.NewBoxBody(float32(w.start.X), float32(w.start.Y), playerW, playerH)
	w.body.Tag = "player"
	w.sensors.Add(w.body)
	ctrl, err := platformer.NewController(physics.NewCharacterBody(w.body), d.player)
	if err != nil {
		return nil, err
	}
	w.ctrl = ctrl
	w.director = rooms.NewDirector(d.rooms, &w.camera)
	w.director.Start(w.center())
	return w, nil
}

// checkRooms reports a room that leaves the level, or a start that no room holds.
func checkRooms(m *rooms.Map, width, height float32, start core.Point) error {
	for _, r := range m.Rooms {
		if r.X < 0 || r.Y < 0 || r.X+r.Width > width || r.Y+r.Height > height {
			return fmt.Errorf("data/rooms.yaml: room %q leaves the %vx%v level", r.ID, width, height)
		}
	}
	if m.RoomAt(start) == nil {
		return errors.New("data/rooms.yaml: no room holds the start")
	}
	return nil
}

// addSensor adds a trigger over box that calls enter when the player's body starts touching it.
func (w *world) addSensor(box physics.Box, enter func()) {
	w.sensors.AddTrigger(&physics.Trigger{Shape: physics.BoxShape(box), OnEnter: func(*physics.Body) { enter() }})
}

// spikeBox is the part of a spike tile that hurts: its lower half, inset from the sides, so grazing a spike's tip is safe.
func spikeBox(tile float32, c, r int) physics.Box {
	return physics.Box{X: float32(c)*tile + tile/8, Y: float32(r)*tile + tile/2, W: tile * 3 / 4, H: tile / 2}
}

// sparkBox is the part of a spark tile that collects it.
func sparkBox(tile float32, c, r int) physics.Box {
	return physics.Box{X: float32(c)*tile + tile/4, Y: float32(r)*tile + tile/4, W: tile / 2, H: tile / 2}
}

// standingAt returns the top left of the player's box standing on the bottom of tile (c, r), centered in it.
func (w *world) standingAt(c, r int) core.Point {
	return core.Point{X: core.Points(float32(c)*w.tile + (w.tile-playerW)/2), Y: core.Points(float32(r+1)*w.tile - playerH)}
}

// center returns the middle of the player's box, which the camera follows.
func (w *world) center() core.Point {
	b := w.body.Bounds()
	cx, cy := b.Center()
	return core.Point{X: core.Points(cx), Y: core.Points(cy)}
}

// sparksTotal returns how many sparks the level has.
func (w *world) sparksTotal() int { return len(w.sparks) }

// step advances the run by one fixed step of dt with in. The player is frozen while the camera slides into a new room and once the run has ended.
func (w *world) step(in platformer.Input, dt time.Duration) {
	w.steps++
	w.lastJump = platformer.JumpNone
	if w.state != running {
		return
	}
	if w.director.Transitioning() {
		w.director.Update(w.center(), dt)
		return
	}
	res := w.ctrl.Step(in, w.grid, nil, nil, dt)
	w.lastJump = res.Jump
	switch {
	case in.Move < -0.1:
		w.facing = -1
	case in.Move > 0.1:
		w.facing = 1
	}

	w.sensors.StepTriggers()
	if w.body.Bounds().Top() > float32(w.rows)*w.tile {
		w.hurt = true
	}
	if w.reached {
		w.reached = false
		w.state = won
	} else if w.hurt {
		w.hurt = false
		w.lives--
		if w.lives <= 0 {
			w.state = lost
		} else {
			w.placePlayer(w.respawn)
		}
	}
	w.director.Update(w.center(), dt)
}

// collect takes spark i, once.
func (w *world) collect(i int) {
	if !w.sparks[i].taken {
		w.sparks[i].taken = true
		w.collected++
	}
}

// light lights checkpoint i and makes it where the player comes back.
func (w *world) light(i int) {
	cp := &w.checkpoints[i]
	if cp.lit {
		return
	}
	for j := range w.checkpoints {
		w.checkpoints[j].lit = false
	}
	cp.lit = true
	w.respawn = w.standingAt(cp.col, cp.row)
}

// placePlayer moves the player to p at rest, forgets the controller's timers and cuts the camera there.
func (w *world) placePlayer(p core.Point) {
	w.body.Shape = physics.BoxShape(physics.Box{X: float32(p.X), Y: float32(p.Y), W: playerW, H: playerH})
	w.body.VX, w.body.VY = 0, 0
	w.ctrl.Reset()
	w.director.Start(w.center())
}
