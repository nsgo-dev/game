package resource

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/sound"
)

// BusLayout lays out a game's sound buses: the groups a sound.Mixer plays through and the DSP effect chain on each.
type BusLayout struct {
	Header `yaml:",inline"`
	// Name is an optional human-readable name.
	Name string `yaml:"name,omitempty"`
	// Buses are the layout's buses, each name once.
	Buses []Bus `yaml:"buses,omitempty"`
}

// Bus is one sound bus: a sound.Mixer group and its effects, applied in order.
type Bus struct {
	// Name is the mixer group's name, such as "music" or "effects".
	Name string `yaml:"name"`
	// Effects is the bus's effect chain, applied in order.
	Effects []BusEffect `yaml:"effects,omitempty"`
}

// BusEffect is one effect on a bus. Exactly one field must be set.
type BusEffect struct {
	// LowPass attenuates frequencies above its cutoff (sound.LowPass).
	LowPass *FilterEffect `yaml:"lowPass,omitempty"`
	// HighPass attenuates frequencies below its cutoff (sound.HighPass).
	HighPass *FilterEffect `yaml:"highPass,omitempty"`
	// Delay is a feedback echo (sound.Delay).
	Delay *DelayEffect `yaml:"delay,omitempty"`
	// Reverb is a room reverb (sound.Reverb).
	Reverb *ReverbEffect `yaml:"reverb,omitempty"`
	// Gain scales every sample (sound.Gain).
	Gain *GainEffect `yaml:"gain,omitempty"`
	// Bitcrush lowers bit depth and sample rate (sound.Bitcrush).
	Bitcrush *BitcrushEffect `yaml:"bitcrush,omitempty"`
}

// FilterEffect is a low-pass or high-pass biquad filter.
type FilterEffect struct {
	// CutoffHz is the cutoff frequency in hertz; it must be positive.
	CutoffHz float64 `yaml:"cutoffHz"`
	// Q is the filter's resonance; 0 is Butterworth (1/sqrt(2)).
	Q float64 `yaml:"q,omitempty"`
}

// DelayEffect is a feedback delay line.
type DelayEffect struct {
	// Seconds is the echo spacing; it must be positive.
	Seconds float64 `yaml:"seconds"`
	// Feedback is how much of each echo feeds the next, from 0 to 0.99.
	Feedback float64 `yaml:"feedback,omitempty"`
	// Wet is the dry/wet mix from 0 to 1.
	Wet float64 `yaml:"wet"`
}

// ReverbEffect is a comb and allpass room reverb.
type ReverbEffect struct {
	// RoomSize, Damping and Wet are each from 0 to 1.
	RoomSize float64 `yaml:"roomSize"`
	Damping  float64 `yaml:"damping,omitempty"`
	Wet      float64 `yaml:"wet"`
}

// GainEffect scales every sample by Factor.
type GainEffect struct {
	// Factor is the scale; it must be finite and non-negative.
	Factor float64 `yaml:"factor"`
}

// BitcrushEffect quantizes samples and holds frames for a lo-fi sound.
type BitcrushEffect struct {
	// Bits is the bit depth from 1 to 15.
	Bits int `yaml:"bits"`
	// Downsample holds every Downsample-th frame; 0 or 1 does not downsample.
	Downsample int `yaml:"downsample,omitempty"`
}

// Kind returns KindBusLayout.
func (b *BusLayout) Kind() Kind { return KindBusLayout }

// Validate checks the header, bus names and every effect's parameters.
func (b *BusLayout) Validate() error {
	if b == nil {
		return fmt.Errorf("resource: nil bus layout")
	}
	if err := b.Header.validate(KindBusLayout); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(b.Buses))
	for i := range b.Buses {
		bus := &b.Buses[i]
		if bus.Name == "" {
			return fmt.Errorf("resource: bus %d has an empty name", i)
		}
		if _, dup := seen[bus.Name]; dup {
			return fmt.Errorf("resource: duplicate bus %q", bus.Name)
		}
		seen[bus.Name] = struct{}{}
		for j := range bus.Effects {
			if err := bus.Effects[j].validate(); err != nil {
				return fmt.Errorf("resource: bus %q effect %d: %w", bus.Name, j, err)
			}
		}
	}
	return nil
}

func unit64(v float64) bool { return v >= 0 && v <= 1 }

func finite64(v float64) bool { return v == v && v-v == 0 }

func (e *BusEffect) validate() error {
	set := 0
	for _, p := range []bool{e.LowPass != nil, e.HighPass != nil, e.Delay != nil, e.Reverb != nil, e.Gain != nil, e.Bitcrush != nil} {
		if p {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("exactly one of lowPass, highPass, delay, reverb, gain or bitcrush must be set, got %d", set)
	}
	switch {
	case e.LowPass != nil:
		return e.LowPass.validate("lowPass")
	case e.HighPass != nil:
		return e.HighPass.validate("highPass")
	case e.Delay != nil:
		d := e.Delay
		if !finite64(d.Seconds) || d.Seconds <= 0 || d.Seconds > 60 {
			return fmt.Errorf("delay seconds %v must be in (0, 60]", d.Seconds)
		}
		if !finite64(d.Feedback) || d.Feedback < 0 || d.Feedback > 0.99 {
			return fmt.Errorf("delay feedback %v must be in [0, 0.99]", d.Feedback)
		}
		if !unit64(d.Wet) {
			return fmt.Errorf("delay wet %v must be in [0, 1]", d.Wet)
		}
	case e.Reverb != nil:
		r := e.Reverb
		if !unit64(r.RoomSize) || !unit64(r.Damping) || !unit64(r.Wet) {
			return fmt.Errorf("reverb roomSize, damping and wet must be in [0, 1]")
		}
	case e.Gain != nil:
		if !finite64(e.Gain.Factor) || e.Gain.Factor < 0 {
			return fmt.Errorf("gain factor %v must be finite and non-negative", e.Gain.Factor)
		}
	case e.Bitcrush != nil:
		if e.Bitcrush.Bits < 1 || e.Bitcrush.Bits > 15 {
			return fmt.Errorf("bitcrush bits %d must be in [1, 15]", e.Bitcrush.Bits)
		}
		if e.Bitcrush.Downsample < 0 {
			return fmt.Errorf("bitcrush downsample %d must not be negative", e.Bitcrush.Downsample)
		}
	}
	return nil
}

func (f *FilterEffect) validate(name string) error {
	if !finite64(f.CutoffHz) || f.CutoffHz <= 0 {
		return fmt.Errorf("%s cutoffHz %v must be finite and positive", name, f.CutoffHz)
	}
	if !finite64(f.Q) || f.Q < 0 {
		return fmt.Errorf("%s q %v must be finite and non-negative", name, f.Q)
	}
	return nil
}

// Effect returns the sound.BusEffect the entry describes, or nil when no field is set.
func (e *BusEffect) Effect() sound.BusEffect {
	switch {
	case e == nil:
		return nil
	case e.LowPass != nil:
		return sound.LowPass(e.LowPass.CutoffHz, e.LowPass.Q)
	case e.HighPass != nil:
		return sound.HighPass(e.HighPass.CutoffHz, e.HighPass.Q)
	case e.Delay != nil:
		return sound.Delay(time.Duration(e.Delay.Seconds*float64(time.Second)), e.Delay.Feedback, e.Delay.Wet)
	case e.Reverb != nil:
		return sound.Reverb(e.Reverb.RoomSize, e.Reverb.Damping, e.Reverb.Wet)
	case e.Gain != nil:
		return sound.Gain(e.Gain.Factor)
	case e.Bitcrush != nil:
		return sound.Bitcrush(e.Bitcrush.Bits, e.Bitcrush.Downsample)
	}
	return nil
}

// Chain returns the bus's effects as a sound.EffectChain, in order.
func (bus *Bus) Chain() sound.EffectChain {
	if bus == nil || len(bus.Effects) == 0 {
		return nil
	}
	chain := make(sound.EffectChain, 0, len(bus.Effects))
	for i := range bus.Effects {
		if fx := bus.Effects[i].Effect(); fx != nil {
			chain = append(chain, fx)
		}
	}
	return chain
}

// Apply sets each bus's effect chain on m with sound.Mixer.SetGroupEffects; a bus with no effects clears its group's chain. Groups the layout does not name keep their chains, and volumes are left to the player's settings.
func (b *BusLayout) Apply(m *sound.Mixer) {
	if b == nil || m == nil {
		return
	}
	for i := range b.Buses {
		bus := &b.Buses[i]
		m.SetGroupEffects(bus.Name, bus.Chain()...)
	}
}
