package particles_test

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/particles"
)

func sparks() particles.Config {
	return particles.Config{
		X: 100, Y: 100,
		Shape:       particles.Circle(4),
		LifetimeMin: 500 * time.Millisecond, LifetimeMax: time.Second,
		SpeedMin: 40, SpeedMax: 80,
		AngleMin: 0, AngleMax: 2 * math.Pi,
		GravityY:   200,
		AlphaCurve: particles.FloatCurve{{T: 0, Value: 1}, {T: 1, Value: 0}},
		Seed:       7,
	}
}

// An Emitter spawns particles in bursts or at a steady Rate and steps them on the game's fixed update; particles die when their lifetime runs out.
func ExampleEmitter() {
	e := particles.NewEmitter(sparks())
	e.Burst(20)
	fmt.Println(e.Count())
	for range 30 {
		e.Update(time.Second / 60)
	}
	fmt.Println(e.Count() > 0 && e.Count() <= 20)
	for range 60 {
		e.Update(time.Second / 60)
	}
	fmt.Println(e.Count())
	// Output:
	// 20
	// true
	// 0
}

// Emitters with the same configuration and seed produce identical particles, so replays and tests can rely on them.
func ExampleNewEmitter() {
	a, b := particles.NewEmitter(sparks()), particles.NewEmitter(sparks())
	for _, e := range []*particles.Emitter{a, b} {
		e.Burst(5)
		e.Update(time.Second / 10)
	}
	pa, pb := a.Particles()[3], b.Particles()[3]
	fmt.Println(pa.X == pb.X && pa.Y == pb.Y)
	// Output:
	// true
}

// A FloatCurve maps a particle's age, from 0 at birth to 1 at death, to a value such as its size or opacity.
func ExampleFloatCurve() {
	grow := particles.FloatCurve{{T: 0, Value: 0.5}, {T: 0.5, Value: 2}, {T: 1, Value: 0}}
	for _, t := range []float32{0, 0.25, 0.5, 0.75, 1} {
		fmt.Println(t, grow.At(t))
	}
	// Output:
	// 0 0.5
	// 0.25 1.25
	// 0.5 2
	// 0.75 1
	// 1 0
}

// A System steps and draws several emitters together, such as the parts of one explosion.
func ExampleSystem() {
	smoke := sparks()
	smoke.Rate = 30
	sys := particles.NewSystem(particles.NewEmitter(sparks()), particles.NewEmitter(smoke))
	sys.Emitters()[0].Burst(10)
	sys.Update(time.Second / 2)
	fmt.Println(len(sys.Emitters()), sys.TotalCount())
	// Output:
	// 2 25
}

// MarshalConfigJSON saves an emitter's configuration as text that UnmarshalConfigJSON loads back unchanged.
func ExampleMarshalConfigJSON() {
	data, err := particles.MarshalConfigJSON(sparks())
	if err != nil {
		fmt.Println(err)
		return
	}
	cfg, err := particles.UnmarshalConfigJSON(data)
	if err != nil {
		fmt.Println(err)
		return
	}
	again, _ := particles.MarshalConfigJSON(cfg)
	fmt.Println(string(again) == string(data), cfg.Seed, cfg.LifetimeMax)
	// Output:
	// true 7 1s
}
