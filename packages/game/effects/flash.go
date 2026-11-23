package effects

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// flashSource is FlashEffect's program.
const flashSource = `package main

// Amount is how far the frame turns to Color, 0 to 1.
var Amount float

// Color is the flash's color, not premultiplied: black when left out.
var Color vec3

func Effect(pos vec2) vec4 {
	c := Input(pos)
	return c + (vec4(clamp(Color, 0.0, 1.0), 1)-c)*clamp(Amount, 0.0, 1.0)
}
`

// FlashEffect returns an effect that turns the whole frame toward an opaque color, as a hit, a bolt of lightning or a camera's flash does. Its uniforms are Amount, from 0, which draws the frame as it is, to 1, which draws only the color, and Color, a core.Color, black when left out. A Flash animates them on the frame clock. The effect has a Reference, so a chain of it draws a reference picture.
func FlashEffect() Effect {
	return Effect{Name: "flash", Source: flashSource, Reference: flashReference}
}

// flashReference is flashSource on the CPU.
func flashReference(in Input, u core.Uniforms, x, y float64) [4]float64 {
	k := clamp01(Float(u, "Amount"))
	color := vector(u, "Color", 3)
	for i := range 3 {
		color[i] = clamp01(color[i])
	}
	color[3] = 1
	c := in.At(x, y)
	for i := range c {
		c[i] += (color[i] - c[i]) * k
	}
	return c
}

// FlashConfig configures a Flash.
type FlashConfig struct {
	// Color is the flash's color; the zero Color is black.
	Color core.Color
	// ReduceMotion, when set, is asked at each Trigger, and the flash does not run while it reports true: pass an a11y.Assist's ReduceMotion, which follows the player's choice and else the platform's. When nil, the platform's reduced motion setting is asked.
	ReduceMotion func() bool
}

// Flash is a full-screen flash a game triggers: FlashEffect's Amount jumps to a peak and fades to 0 on the platform's frame clock, with no commit per frame.
type Flash struct {
	cx     *core.Scope
	cfg    FlashConfig
	amount *core.AnimatedValue
	run    *core.AnimationRun
}

// NewFlash returns a flash for FlashEffect, at rest, that lasts as long as cx's owner.
func NewFlash(cx *core.Scope, cfg FlashConfig) *Flash {
	return &Flash{cx: cx, cfg: cfg, amount: core.NewAnimatedValue(cx, 0)}
}

// Uniforms returns FlashEffect's uniforms, for its place in a View's Uniforms: Amount follows the flash.
func (f *Flash) Uniforms() core.Uniforms {
	return core.Uniforms{"Amount": f.amount, "Color": f.cfg.Color}
}

// Amount returns the node FlashEffect's Amount follows, from 0 at rest up to a Trigger's peak.
func (f *Flash) Amount() core.AnimatedNode { return f.amount }

// Trigger sets Amount to peak, clamped to 0 to 1, and fades it to 0 over fade, easing out, in place of a flash still fading. It reports whether the flash runs: it does not under reduced motion, nor for a peak of 0 or less or a fade of 0 or less, which leave a running flash alone: Stop, not Trigger(0, fade), ends a running flash.
func (f *Flash) Trigger(peak float64, fade time.Duration) bool {
	if !(peak > 0) || fade <= 0 || reduced(f.cx, f.cfg.ReduceMotion) {
		return false
	}
	f.Stop()
	f.amount.Set(min(peak, 1))
	f.run = f.cx.StartAnimation(core.Timing(f.amount, 0, core.TimingConfig{Duration: fade, Easing: core.BezierEasing(core.EaseOut)}), nil)
	return true
}

// Stop ends a running flash at once, with Amount at 0.
func (f *Flash) Stop() {
	if f.run != nil {
		f.run.Stop()
		f.run = nil
	}
	f.amount.Set(0)
}

// reduced reports whether motion is reduced: by ask when it is set, else by the platform's setting, read without subscribing the caller.
func reduced(cx *core.Scope, ask func() bool) bool {
	if ask == nil {
		ask = cx.Env().ReduceMotion
	}
	return core.Untracked(cx, ask)
}
