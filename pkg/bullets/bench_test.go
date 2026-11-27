package bullets_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/bullets"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// benchScene returns a field holding at least n bullets in a steady state: ten emitters fire spirals of n/100 bullets every 100ms that live 1.05s, about ten shots each, so as many bullets expire each step as spawn. Every other emitter's bullets face their heading, so the batch draws both plain and turned sprites.
func benchScene(tb testing.TB, n int) (*bullets.Field, []*bullets.Emitter, *core.Drawing) {
	tb.Helper()
	const emitterCount = 10
	f := bullets.NewField(testAtlas(), n+n/5)
	f.Bounds = math2d.Rect{X: 0, Y: 0, W: 1920, H: 1080}
	emitters := make([]*bullets.Emitter, emitterCount)
	for i := range emitters {
		p := bullets.Pattern{
			Name: "bench", Kind: bullets.Spiral, Frame: "orb", Count: n / emitterCount / 10, Speed: 120,
			SpeedJitter: 10, AngleJitter: 3, Spin: 9, Shots: 0, Interval: 100 * time.Millisecond,
			Delay: time.Duration(i) * 10 * time.Millisecond, Lifetime: 1050 * time.Millisecond, Radius: 4,
			Turn: 20, FaceHeading: i%2 == 1,
		}
		if p.FaceHeading {
			p.Frame = "dart"
		}
		e := mustEmitter(tb, f, p, uint64(i+1))
		e.X, e.Y = float32(200+i*150), 540
		emitters[i] = e
	}
	d := &core.Drawing{}
	for range 180 {
		frame(f, emitters, d)
	}
	if f.Len() < n {
		tb.Fatalf("steady state holds %d bullets, want at least %d", f.Len(), n)
	}
	return f, emitters, d
}

// frame runs one fixed step and draws it: the field moves, the emitters fire, and the field draws its batch.
func frame(f *bullets.Field, emitters []*bullets.Emitter, d *core.Drawing) {
	f.Update(step)
	for _, e := range emitters {
		e.Update(step)
	}
	d.Ops = d.Ops[:0]
	f.Draw(d)
}

// BenchmarkFrame5000 measures one 60 fps frame's work for 5000 bullets: moving, expiring, spawning and drawing them. A frame has 16.7ms.
func BenchmarkFrame5000(b *testing.B) {
	f, emitters, d := benchScene(b, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		frame(f, emitters, d)
	}
	b.ReportMetric(float64(f.Len()), "bullets")
}

// BenchmarkUpdate5000 measures moving and culling 5000 bullets for one step, without spawning or drawing.
func BenchmarkUpdate5000(b *testing.B) {
	f, _, _ := benchScene(b, 5000)
	f.Bounds = math2d.Rect{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		f.Update(time.Nanosecond)
	}
}

// BenchmarkDraw5000 measures drawing 5000 bullets through the field's sprite batch.
func BenchmarkDraw5000(b *testing.B) {
	f, _, d := benchScene(b, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		d.Ops = d.Ops[:0]
		f.Draw(d)
	}
}
