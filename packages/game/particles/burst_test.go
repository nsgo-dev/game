package particles_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/particles"
)

func TestShapeAccessors(t *testing.T) {
	p := particles.Point()
	if p.Kind() != "point" || p.Radius() != 0 || p.Width() != 0 || p.Height() != 0 {
		t.Fatalf("Point = (%q, r=%v, w=%v, h=%v)", p.Kind(), p.Radius(), p.Width(), p.Height())
	}
	c := particles.Circle(12.5)
	if c.Kind() != "circle" || c.Radius() != 12.5 || c.Width() != 0 || c.Height() != 0 {
		t.Fatalf("Circle = (%q, r=%v, w=%v, h=%v)", c.Kind(), c.Radius(), c.Width(), c.Height())
	}
	r := particles.Ring(8)
	if r.Kind() != "ring" || r.Radius() != 8 {
		t.Fatalf("Ring = (%q, r=%v)", r.Kind(), r.Radius())
	}
	rc := particles.Rect(24, 10)
	if rc.Kind() != "rect" || rc.Width() != 24 || rc.Height() != 10 || rc.Radius() != 0 {
		t.Fatalf("Rect = (%q, r=%v, w=%v, h=%v)", rc.Kind(), rc.Radius(), rc.Width(), rc.Height())
	}
}

func TestScheduledBurstsAndRoundTrip(t *testing.T) {
	cfg := particles.Config{
		Space:        particles.SpaceWorld,
		Shape:        particles.Point(),
		Rate:         0,
		MaxParticles: 64,
		LifetimeMin:  5 * time.Second,
		LifetimeMax:  5 * time.Second,
		Bursts: []particles.Burst{
			{Time: 0, Count: 4, Cycles: 1},
			{Time: 100 * time.Millisecond, Count: 3, Cycles: 3, Interval: 100 * time.Millisecond},
		},
		Seed: 7,
	}

	// YAML round-trip preserves bursts deterministically.
	data, err := particles.MarshalConfigYAML(cfg)
	if err != nil {
		t.Fatalf("MarshalConfigYAML: %v", err)
	}
	if !strings.Contains(string(data), "bursts:\n") {
		t.Fatalf("YAML lacks bursts:\n%s", data)
	}
	got, err := particles.UnmarshalConfigYAML(data)
	if err != nil {
		t.Fatalf("UnmarshalConfigYAML: %v", err)
	}
	again, err := particles.MarshalConfigYAML(got)
	if err != nil || string(again) != string(data) {
		t.Fatalf("bursts round-trip changed bytes (%v):\n%s\n---\n%s", err, data, again)
	}

	em := particles.NewEmitter(got)
	// First step fires the t=0 burst (4 particles).
	em.Update(50 * time.Millisecond)
	if em.Count() != 4 {
		t.Fatalf("at 50ms Count = %d, want 4", em.Count())
	}
	// At 100ms the second burst fires cycle 1 (+3 = 7).
	em.Update(50 * time.Millisecond)
	if em.Count() != 7 {
		t.Fatalf("at 100ms Count = %d, want 7", em.Count())
	}
	// At 200ms cycle 2 (+3 = 10), at 300ms cycle 3 (+3 = 13), at 400ms done (still 13).
	em.Update(100 * time.Millisecond)
	if em.Count() != 10 {
		t.Fatalf("at 200ms Count = %d, want 10", em.Count())
	}
	em.Update(100 * time.Millisecond)
	if em.Count() != 13 {
		t.Fatalf("at 300ms Count = %d, want 13", em.Count())
	}
	em.Update(200 * time.Millisecond)
	if em.Count() != 13 {
		t.Fatalf("at 500ms Count = %d, want 13", em.Count())
	}

	// Reset replays bursts identically.
	em.Reset()
	em.Update(50 * time.Millisecond)
	if em.Count() != 4 {
		t.Fatalf("after Reset Count = %d, want 4", em.Count())
	}

	// Negative burst fields are refused on decode.
	for _, bad := range []string{
		"version: 1\nbursts:\n  - time: -10ms\n    count: 2\n",
		"version: 1\nbursts:\n  - count: -1\n",
		"version: 1\nbursts:\n  - count: 2\n    cycles: 2\n    interval: -100ms\n",
	} {
		if _, err := particles.UnmarshalConfigYAML([]byte(bad)); err == nil {
			t.Fatalf("expected error for invalid burst YAML:\n%s", bad)
		}
	}

	// Negative burst fields are also refused on marshal.
	for _, badBurst := range []particles.Burst{
		{Time: -10 * time.Millisecond, Count: 2},
		{Time: 0, Count: -1},
		{Time: 0, Count: 2, Cycles: 2, Interval: -50 * time.Millisecond},
	} {
		badCfg := cfg
		badCfg.Bursts = []particles.Burst{badBurst}
		if _, err := particles.MarshalConfigYAML(badCfg); err == nil {
			t.Fatalf("expected error marshaling invalid burst %+v", badBurst)
		}
	}
}

func TestScheduledBurstsTinyIntervalAndHugeElapsedDoNotHang(t *testing.T) {
	// Infinite cycles with a 1ns interval and huge dt must complete in O(1) without overflow or hang.
	cfg := particles.Config{
		Space:        particles.SpaceWorld,
		Shape:        particles.Point(),
		MaxParticles: 16,
		LifetimeMin:  time.Hour,
		LifetimeMax:  time.Hour,
		Bursts: []particles.Burst{
			{Time: 0, Count: 4, Cycles: -1, Interval: time.Nanosecond},
		},
		Seed: 1,
	}
	em := particles.NewEmitter(cfg)
	em.Update(time.Second)
	if em.Count() != 16 {
		t.Fatalf("after 1s with 1ns interval: Count = %d, want 16 (MaxParticles)", em.Count())
	}
	// Advancing by an extreme duration must not overflow elapsed or burst counters.
	em.Update(time.Duration(1<<62) * time.Nanosecond)
	if em.Count() > 16 {
		t.Fatalf("after huge elapsed: Count = %d, want <= 16", em.Count())
	}
}
