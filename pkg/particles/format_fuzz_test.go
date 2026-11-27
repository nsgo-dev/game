package particles_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/particles"
)

func FuzzUnmarshalConfigJSON(f *testing.F) {
	fullBytes, err := particles.MarshalConfigJSON(fullTestConfig())
	if err != nil {
		f.Fatalf("MarshalConfigJSON seed: %v", err)
	}
	minimalBytes, err := particles.MarshalConfigJSON(particles.Config{})
	if err != nil {
		f.Fatalf("MarshalConfigJSON minimal seed: %v", err)
	}

	seeds := [][]byte{
		fullBytes,
		minimalBytes,
		[]byte(``),
		[]byte(`{}`),
		[]byte(`{"version": 1}`),
		[]byte(`{"version": 1, "space": "local", "shape": {"kind": "circle", "radius": 10}, "rate": 25, "lifetimeMin": "500ms", "lifetimeMax": 1000000000}`),
		[]byte(`{"version": 1, "colorCurve": [{"t": 0, "color": "#ff00aa"}, {"t": 1, "color": 4294967295}]}`),
		[]byte(`{"version": 2}`),
		[]byte(`{"version": 1, "shape": {"kind": "unknown"}}`),
		[]byte(`{"version": 1, "collision": {"enabled": true, "mode": "bounce", "bounce": 0.8, "friction": 0.2, "radius": 2}}`),
		[]byte(`{"version": 1, "trail": {"maxPoints": 16, "lifetime": 0.5, "width": 4, "color": "#11223344"}}`),
		[]byte(`{"version": 1, "subEmitters": [{"trigger": "death", "burstCount": 2, "config": {"rate": 10}}]}`),
		[]byte(`{"version": 1, "lifetimeMin": "not-a-duration"}`),
		[]byte(`{"version": 1, "colorCurve": [{"t": 0, "color": "#zzzzzz"}]}`),
		[]byte(`[[[malformed`),
		[]byte(`null`),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := particles.UnmarshalConfigJSON(data)
		if err != nil {
			return
		}
		// Any config accepted by UnmarshalConfigJSON must re-marshal and re-unmarshal cleanly without panicking.
		out, err := particles.MarshalConfigJSON(cfg)
		if err != nil {
			t.Fatalf("MarshalConfigJSON failed on valid unmarshaled config: %v", err)
		}
		if _, err := particles.UnmarshalConfigJSON(out); err != nil {
			t.Fatalf("second UnmarshalConfigJSON failed: %v", err)
		}

		// Clamp capacities before stepping an Emitter so fuzzed values cannot allocate huge buffers.
		safeCfg := clampFuzzConfig(cfg, 0)
		e := particles.NewEmitter(safeCfg)
		e.Burst(2)
		e.Update(16 * time.Millisecond)
	})
}

func clampFuzzConfig(cfg particles.Config, depth int) particles.Config {
	if cfg.MaxParticles <= 0 || cfg.MaxParticles > 32 {
		cfg.MaxParticles = 32
	}
	if cfg.Rate > 100 || cfg.Rate < 0 {
		cfg.Rate = 10
	}
	if cfg.Trail != nil {
		tc := *cfg.Trail
		if tc.MaxPoints <= 0 || tc.MaxPoints > 16 {
			tc.MaxPoints = 16
		}
		cfg.Trail = &tc
	}
	if depth >= 2 {
		cfg.SubEmitters = nil
	} else if len(cfg.SubEmitters) > 0 {
		if len(cfg.SubEmitters) > 2 {
			cfg.SubEmitters = cfg.SubEmitters[:2]
		}
		subs := make([]particles.SubEmitterRule, len(cfg.SubEmitters))
		for i, r := range cfg.SubEmitters {
			if r.BurstCount <= 0 || r.BurstCount > 4 {
				r.BurstCount = 2
			}
			r.Config = clampFuzzConfig(r.Config, depth+1)
			subs[i] = r
		}
		cfg.SubEmitters = subs
	}
	return cfg
}

func FuzzUnmarshalConfigYAML(f *testing.F) {
	fullBytes, err := particles.MarshalConfigYAML(fullTestConfig())
	if err != nil {
		f.Fatalf("MarshalConfigYAML seed: %v", err)
	}
	minimalBytes, err := particles.MarshalConfigYAML(particles.Config{})
	if err != nil {
		f.Fatalf("MarshalConfigYAML minimal seed: %v", err)
	}

	seeds := []string{
		string(fullBytes),
		string(minimalBytes),
		fireYAML,
		"",
		"version: 1\n",
		"version: 1\nspace: local\nshape: {kind: circle, radius: 10}\nrate: 25\nlifetimeMin: 500ms\nlifetimeMax: 1s\n",
		"version: 1\ncolorCurve:\n  - {t: 0, color: \"#ff00aa\"}\n  - {t: 1, color: \"#ffffffff\"}\n",
		"version: 2\n",
		"version: 1\nshape:\n  kind: unknown\n",
		"version: 1\ncollision: {enabled: true, mode: bounce, bounce: 0.8, friction: 0.2, radius: 2}\n",
		"version: 1\ntrail: {maxPoints: 16, lifetime: 0.5, width: 4, color: \"#11223344\"}\n",
		"version: 1\nsubEmitters:\n  - trigger: death\n    burstCount: 2\n    config: {rate: 10}\n",
		"version: 1\nlifetimeMin: not-a-duration\n",
		"version: 1\ncolorCurve:\n  - t: 0\n    color: #zzzzzz\n",
		"version: 1\nframeNames: &names [a, b]\nsubEmitters:\n  - config: {frameNames: *names}\n",
		"version: 1\n---\nversion: 1\n",
		"[[[malformed",
		"null",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := particles.UnmarshalConfigYAML(data)
		if err != nil {
			return
		}
		// Any accepted definition saves, loads back, and saves again to the same bytes.
		out, err := particles.MarshalConfigYAML(cfg)
		if err != nil {
			t.Fatalf("MarshalConfigYAML failed on an accepted config: %v", err)
		}
		back, err := particles.UnmarshalConfigYAML(out)
		if err != nil {
			t.Fatalf("UnmarshalConfigYAML failed on its own output: %v\n%s", err, out)
		}
		again, err := particles.MarshalConfigYAML(back)
		if err != nil {
			t.Fatalf("second MarshalConfigYAML failed: %v", err)
		}
		if !bytes.Equal(out, again) {
			t.Fatalf("save is not stable:\n%s\n---\n%s", out, again)
		}

		e := particles.NewEmitter(clampFuzzConfig(cfg, 0))
		e.Burst(2)
		e.Update(16 * time.Millisecond)
	})
}
