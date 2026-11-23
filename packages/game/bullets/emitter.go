package bullets

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

const degToRad = math.Pi / 180

// Emitter fires one pattern's shots into a field at the pattern's times. It draws the pattern's jitter from its own random stream, so two emitters with the same pattern and seed, moved and stepped alike, spawn the same bullets.
//
// An Emitter is not safe for concurrent use.
type Emitter struct {
	// X and Y are where its shots start, in the field's coordinates; move them with the thing that shoots.
	X, Y float32
	// TargetX and TargetY are what an Aimed pattern aims at, such as the player.
	TargetX, TargetY float32
	// Stopped, when true, pauses the emitter: Update neither fires nor runs its clock, and Done reports true. Once Stopped is false again, the emitter picks up its pattern where it paused, owing no shots for the time it was stopped.
	Stopped bool

	field   *Field
	pattern Pattern
	sprite  sprite.Sprite
	rng     *random.Stream
	elapsed time.Duration
	shot    int
}

// NewEmitter returns an emitter that fires a copy of p into f, drawing jitter from a stream seeded with seed. It returns an error when p is invalid, or names a frame that f's atlas does not have.
func NewEmitter(f *Field, p *Pattern, seed uint64) (*Emitter, error) {
	if f == nil {
		return nil, fmt.Errorf("bullets: nil field")
	}
	if p == nil {
		return nil, fmt.Errorf("bullets: nil pattern")
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("bullets: %w", err)
	}
	e := &Emitter{field: f, pattern: *p, rng: random.New(seed)}
	if p.Frame != "" {
		if f.Atlas == nil {
			return nil, fmt.Errorf("bullets: pattern %q: frame %q needs a field with an atlas", p.Name, p.Frame)
		}
		frame, ok := f.Atlas.Frames[p.Frame]
		if !ok {
			return nil, fmt.Errorf("bullets: pattern %q: the atlas has no frame %q", p.Name, p.Frame)
		}
		e.sprite = bulletSprite(frame, p.Z)
	}
	return e, nil
}

// bulletSprite returns a sprite of frame turning about the frame's pivot, or about its center when it has none, so a bullet's position is the middle of its picture.
func bulletSprite(frame sprite.Frame, z int) sprite.Sprite {
	s := sprite.NewSprite(frame)
	if frame.PivotX == 0 && frame.PivotY == 0 {
		w, h := frame.Width, frame.Height
		if w == 0 && h == 0 {
			w, h = frame.W, frame.H
		}
		s.OriginX, s.OriginY = w/2, h/2
	}
	s.Z = z
	return s
}

// Pattern returns the emitter's copy of its pattern.
func (e *Emitter) Pattern() Pattern { return e.pattern }

// Seed returns the seed of the emitter's random stream.
func (e *Emitter) Seed() uint64 { return e.rng.Seed() }

// Shots returns how many shots the emitter has fired since it was made or reset.
func (e *Emitter) Shots() int { return e.shot }

// Elapsed returns how long the emitter has run since it was made or reset, not counting the time it was stopped.
func (e *Emitter) Elapsed() time.Duration { return e.elapsed }

// Done reports whether the emitter fires no more shots: it is stopped, or has fired all of a pattern's Shots.
func (e *Emitter) Done() bool {
	return e.Stopped || (e.pattern.Shots > 0 && e.shot >= e.pattern.Shots)
}

// Reset starts the emitter over from its first shot, with its random stream rewound to its seed, and lets it fire again if it was stopped. It leaves bullets already fired in the field.
func (e *Emitter) Reset() {
	e.rng.Reset()
	e.elapsed = 0
	e.shot = 0
	e.Stopped = false
}

// Update runs the emitter's clock on by dt and fires each shot due in that time into its field, returning the number of bullets it spawned. A shot due partway through dt spawns its bullets where they are at its end, so update the field before its emitters.
//
// A shot past the field's Limit spawns only as many bullets as fit; it still counts as fired and draws the same random numbers, so the bullets that do spawn are the same as with no limit.
//
// One call fires at most MaxCount bullets' worth of shots, and at least one shot. When more shots are due, as after a long dt from a hitch, it fires the latest of them and skips the earlier ones: they count as fired but spawn nothing and draw no random numbers. This bounds the work of a step whatever the pattern's Interval and dt.
func (e *Emitter) Update(dt time.Duration) int {
	if dt <= 0 || e.Stopped {
		return 0
	}
	end := e.elapsed + dt
	due := e.due(end)
	if budget := max(1, MaxCount/e.pattern.Count); due-e.shot > budget {
		e.shot = due - budget
	}
	spawned := 0
	for ; e.shot < due; e.shot++ {
		at := e.pattern.Delay + time.Duration(e.shot)*e.pattern.Interval
		spawned += e.fire(end - at)
	}
	e.elapsed = end
	return spawned
}

// due returns how many of the pattern's shots are due before end: those whose time, Delay plus Interval for each shot before them, is earlier than end, up to the pattern's Shots.
func (e *Emitter) due(end time.Duration) int {
	p := &e.pattern
	if end <= p.Delay {
		return 0
	}
	n := 1
	if p.Interval > 0 {
		n = int((end-p.Delay-1)/p.Interval) + 1
	}
	if p.Shots > 0 {
		n = min(n, p.Shots)
	}
	return n
}

// fire spawns the current shot's bullets, each moved on by late, the time since the shot was due.
func (e *Emitter) fire(late time.Duration) int {
	p := &e.pattern
	base := float64(p.Angle) + float64(e.shot)*float64(p.Spin)
	step := 360 / float64(p.Count)
	if p.Kind == Aimed {
		dx, dy := float64(e.TargetX-e.X), float64(e.TargetY-e.Y)
		if dx != 0 || dy != 0 {
			base += math.Atan2(dy, dx) / degToRad
		}
		if p.Count > 1 {
			if p.Spread < 360 {
				step = float64(p.Spread) / float64(p.Count-1)
			}
			base -= step * float64(p.Count-1) / 2
		}
	}
	spawned := 0
	for i := range p.Count {
		angle := base + float64(i)*step
		if p.AngleJitter > 0 {
			angle += float64(e.rng.Range32(-p.AngleJitter, p.AngleJitter))
		}
		speed := p.Speed
		if p.SpeedJitter > 0 {
			speed = max(0, speed+e.rng.Range32(-p.SpeedJitter, p.SpeedJitter))
		}
		b := e.field.spawn()
		if b == nil {
			continue
		}
		*b = Bullet{
			X:        e.X,
			Y:        e.Y,
			Speed:    speed,
			Radius:   p.Radius,
			Lifetime: p.Lifetime,
			Pattern:  p.Name,
			Shot:     e.shot,
			Sprite:   e.sprite,
			accel:    p.Accel,
			turn:     float32(float64(p.Turn) * degToRad),
			face:     p.FaceHeading,
		}
		b.setHeading(float32(math.Remainder(angle, 360) * degToRad))
		b.Sprite.X, b.Sprite.Y = b.X, b.Y
		if late > 0 {
			b.advance(late)
		}
		spawned++
	}
	return spawned
}
