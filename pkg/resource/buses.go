package resource

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
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
		return loaderr.Errorf("", "pass a non-nil *resource.BusLayout to Validate", "resource: nil bus layout")
	}
	if err := b.Header.validate(KindBusLayout); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(b.Buses))
	for i := range b.Buses {
		bus := &b.Buses[i]
		bPath := fmt.Sprintf("buses[%d]", i)
		if bus.Name == "" {
			return loaderr.Errorf(bPath+".name", "set name to a non-empty mixer bus name such as music or effects", "resource: bus %d has an empty name", i)
		}
		if _, dup := seen[bus.Name]; dup {
			return loaderr.Errorf(bPath+".name", "give each sound bus a unique name", "resource: duplicate bus %q", bus.Name)
		}
		seen[bus.Name] = struct{}{}
		for j := range bus.Effects {
			if err := bus.Effects[j].validateAt(fmt.Sprintf("%s.effects[%d]", bPath, j), bus.Name, j); err != nil {
				return err
			}
		}
	}
	return nil
}

func unit64(v float64) bool { return v >= 0 && v <= 1 }

func finite64(v float64) bool { return v == v && v-v == 0 }

func (e *BusEffect) validateAt(ePath, busName string, idx int) error {
	set := 0
	for _, p := range []bool{e.LowPass != nil, e.HighPass != nil, e.Delay != nil, e.Reverb != nil, e.Gain != nil, e.Bitcrush != nil} {
		if p {
			set++
		}
	}
	if set != 1 {
		return loaderr.Errorf(ePath, "set exactly one of lowPass, highPass, delay, reverb, gain, or bitcrush on each effect", "resource: bus %q effect %d: exactly one of lowPass, highPass, delay, reverb, gain or bitcrush must be set, got %d", busName, idx, set)
	}
	switch {
	case e.LowPass != nil:
		return e.LowPass.validateAt(ePath+".lowPass", "lowPass", busName, idx)
	case e.HighPass != nil:
		return e.HighPass.validateAt(ePath+".highPass", "highPass", busName, idx)
	case e.Delay != nil:
		d := e.Delay
		if !finite64(d.Seconds) || d.Seconds <= 0 || d.Seconds > 60 {
			return loaderr.Errorf(ePath+".delay.seconds", "set delay.seconds to a finite number in (0, 60]", "resource: bus %q effect %d: delay seconds %v must be in (0, 60]", busName, idx, d.Seconds)
		}
		if !finite64(d.Feedback) || d.Feedback < 0 || d.Feedback > 0.99 {
			return loaderr.Errorf(ePath+".delay.feedback", "set delay.feedback to a finite number in [0, 0.99]", "resource: bus %q effect %d: delay feedback %v must be in [0, 0.99]", busName, idx, d.Feedback)
		}
		if !unit64(d.Wet) {
			return loaderr.Errorf(ePath+".delay.wet", "set delay.wet to a finite number in [0, 1]", "resource: bus %q effect %d: delay wet %v must be in [0, 1]", busName, idx, d.Wet)
		}
	case e.Reverb != nil:
		r := e.Reverb
		if !unit64(r.RoomSize) || !unit64(r.Damping) || !unit64(r.Wet) {
			field := ePath + ".reverb.roomSize"
			if unit64(r.RoomSize) && !unit64(r.Damping) {
				field = ePath + ".reverb.damping"
			} else if unit64(r.RoomSize) && unit64(r.Damping) {
				field = ePath + ".reverb.wet"
			}
			return loaderr.Errorf(field, "set reverb roomSize, damping, and wet to numbers in [0, 1]", "resource: bus %q effect %d: reverb roomSize, damping and wet must be in [0, 1]", busName, idx)
		}
	case e.Gain != nil:
		if !finite64(e.Gain.Factor) || e.Gain.Factor < 0 {
			return loaderr.Errorf(ePath+".gain.factor", "set gain.factor to a finite non-negative number", "resource: bus %q effect %d: gain factor %v must be finite and non-negative", busName, idx, e.Gain.Factor)
		}
	case e.Bitcrush != nil:
		if e.Bitcrush.Bits < 1 || e.Bitcrush.Bits > 15 {
			return loaderr.Errorf(ePath+".bitcrush.bits", "set bitcrush.bits to an integer in [1, 15]", "resource: bus %q effect %d: bitcrush bits %d must be in [1, 15]", busName, idx, e.Bitcrush.Bits)
		}
		if e.Bitcrush.Downsample < 0 {
			return loaderr.Errorf(ePath+".bitcrush.downsample", "set bitcrush.downsample to 0 or a positive integer", "resource: bus %q effect %d: bitcrush downsample %d must not be negative", busName, idx, e.Bitcrush.Downsample)
		}
	}
	return nil
}

func (f *FilterEffect) validateAt(fPath, name, busName string, idx int) error {
	if !finite64(f.CutoffHz) || f.CutoffHz <= 0 {
		return loaderr.Errorf(fPath+".cutoffHz", fmt.Sprintf("set %s.cutoffHz to a finite positive frequency in Hz", name), "resource: bus %q effect %d: %s cutoffHz %v must be finite and positive", busName, idx, name, f.CutoffHz)
	}
	if !finite64(f.Q) || f.Q < 0 {
		return loaderr.Errorf(fPath+".q", fmt.Sprintf("set %s.q to a finite non-negative resonance value", name), "resource: bus %q effect %d: %s q %v must be finite and non-negative", busName, idx, name, f.Q)
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
