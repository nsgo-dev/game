package main

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sprite"
)

const (
	// swarmSize is how many fireflies the stress swarm keeps moving, the sprite count #8's performance target names.
	swarmSize = 1000
	// swarmSeed seeds the swarm's starting positions and drifts, so every run and every test sees the same swarm.
	swarmSeed = 0xCA7E
	// swarmZ draws the fireflies over the gems and under the slimes, the hero and the effects.
	swarmZ = 12
	// swarmScale draws each firefly at half the spark frame's size, a mote of light rather than a burst.
	swarmScale = 0.5
	// swarmMargin lets a firefly drift this far past the view's edge before it wraps to the other side, so none pops in or out in view.
	swarmMargin float32 = 8
)

// firefly is one glowing mote of the stress swarm. Its position is relative to the camera's view, so all of them stay on screen wherever the explorer goes.
type firefly struct {
	x, y       float32
	prevX      float32
	prevY      float32
	vx, vy     float32
	phase      float32
	wobble     float32
	frameShift int
}

// fireflySwarm is the stress mode's swarm: swarmSize fireflies drawn through the level's sprite batch with one DrawAtlas, moved every fixed step without allocating.
type fireflySwarm struct {
	flies   []firefly
	sprites []sprite.Sprite
	frames  [3]sprite.Frame
	elapsed time.Duration
	width   float32
	height  float32
}

// newFireflySwarm returns a swarm of swarmSize fireflies spread over a width by height view, using the atlas's spark frames.
func newFireflySwarm(atlas *sprite.Atlas, width, height float32) *fireflySwarm {
	s := &fireflySwarm{
		flies:   make([]firefly, swarmSize),
		sprites: make([]sprite.Sprite, swarmSize),
		width:   width,
		height:  height,
	}
	s.frames = [3]sprite.Frame{atlas.Frames["spark_0"], atlas.Frames["spark_1"], atlas.Frames["spark_2"]}
	rng := random.New(swarmSeed)
	for i := range s.flies {
		f := &s.flies[i]
		f.x = rng.Range32(-swarmMargin, width+swarmMargin)
		f.y = rng.Range32(-swarmMargin, height+swarmMargin)
		f.prevX, f.prevY = f.x, f.y
		f.vx = rng.Range32(-14, 14)
		f.vy = rng.Range32(-9, 9)
		f.phase = rng.Range32(0, 2*math.Pi)
		f.wobble = rng.Range32(4, 12)
		f.frameShift = rng.Intn(len(s.frames))
		spr := sprite.NewSprite(s.frames[f.frameShift])
		spr.Z = swarmZ
		spr.ScaleX, spr.ScaleY = swarmScale, swarmScale
		s.sprites[i] = spr
	}
	return s
}

// update moves every firefly one fixed step: a steady drift plus a sideways wobble, wrapping at the view's edges.
func (s *fireflySwarm) update(dt time.Duration) {
	s.elapsed += dt
	secs := float32(dt.Seconds())
	t := float32(s.elapsed.Seconds())
	minX, maxX := -swarmMargin, s.width+swarmMargin
	minY, maxY := -swarmMargin, s.height+swarmMargin
	for i := range s.flies {
		f := &s.flies[i]
		f.prevX, f.prevY = f.x, f.y
		sin, cos := math.Sincos(float64(t*1.7 + f.phase))
		f.x += (f.vx + f.wobble*float32(cos)) * secs
		f.y += (f.vy + f.wobble*0.6*float32(sin)) * secs
		wrapped := false
		if f.x < minX {
			f.x += maxX - minX
			wrapped = true
		} else if f.x >= maxX {
			f.x -= maxX - minX
			wrapped = true
		}
		if f.y < minY {
			f.y += maxY - minY
			wrapped = true
		} else if f.y >= maxY {
			f.y -= maxY - minY
			wrapped = true
		}
		if wrapped {
			// A wrapped firefly jumps across the view; interpolating from its old side would streak it over the screen.
			f.prevX, f.prevY = f.x, f.y
		}
	}
}

// sync places every firefly's sprite at its interpolated position, offset by the view's top-left corner in the world, and appends the sprites to batch.
func (s *fireflySwarm) sync(batch []*sprite.Sprite, alpha float32, originX, originY float32, vp screen.Viewport) []*sprite.Sprite {
	t := float32(s.elapsed.Seconds())
	for i := range s.flies {
		f := &s.flies[i]
		spr := &s.sprites[i]
		x := f.prevX + (f.x-f.prevX)*alpha
		y := f.prevY + (f.y-f.prevY)*alpha
		spr.X, spr.Y = vp.Snap(originX+x, originY+y)
		// The glow cycles through the spark frames and twinkles. Every firefly stays below full opacity, so the whole swarm is one run of faded sprites, which the batch draws with a single DrawAtlas.
		glow := 0.5 + 0.5*float32(math.Sin(float64(t*3+f.phase)))
		spr.Frame = s.frames[(f.frameShift+int(glow*2.99))%len(s.frames)]
		spr.Alpha = 0.2 + 0.55*glow
		batch = append(batch, spr)
	}
	return batch
}
