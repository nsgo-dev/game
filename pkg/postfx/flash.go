package postfx

import (
	"math"
	"sync"
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
	k := saturate(Float(u, "Amount"))
	color := vector(u, "Color", 3)
	for i := range 3 {
		color[i] = saturate(color[i])
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
	// ReduceFlashes, when set, is asked at each Trigger, and while it reports true the flash runs as a low-contrast tint: its peak is at most ReducedFlashPeak and it fades over at least ReducedFlashFade. Pass an a11y.Assist's ReduceFlashes, the player's Reduce Flashes choice, which is separate from reduced motion. When nil, flashes are not reduced.
	ReduceFlashes func() bool
	// Limiter is the rolling flash budget the flash draws on: share one FlashLimiter between every full-screen flash of a screen so that together they stay within its limit. When nil, the flash has a FlashLimiter of its own, so a single flash never runs more than MaxFlashesPerSecond times in a second.
	Limiter *FlashLimiter
}

// MaxFlashesPerSecond is the most full-screen flashes a FlashLimiter lets run in any one second: more than three flashes in a second fail the general and red flash thresholds of WCAG 2.3.1 and XAG 118.
const MaxFlashesPerSecond = 3

// ReducedFlashPeak and ReducedFlashFade bound a flash under FlashConfig.ReduceFlashes: its Amount peaks at most at ReducedFlashPeak and fades over no less than ReducedFlashFade.
const (
	ReducedFlashPeak = 0.2
	ReducedFlashFade = 100 * time.Millisecond
)

// FlashLimiter is a budget of full-screen flashes over a rolling one-second window, on the clock of the scope that triggers them. Its zero value is ready to use and allows MaxFlashesPerSecond. It is safe for concurrent use.
type FlashLimiter struct {
	// MaxPerSecond is how many flashes run in any one second: 0 or less, or more than MaxFlashesPerSecond, means MaxFlashesPerSecond.
	MaxPerSecond int

	mu    sync.Mutex
	times []time.Time
}

// Allow reports whether a flash at now fits the budget, and when it does, counts it: a flash fits while fewer than MaxPerSecond flashes the limiter allowed fall in the closed second ending at now. A nil FlashLimiter allows every flash.
func (l *FlashLimiter) Allow(now time.Time) bool {
	if l == nil {
		return true
	}
	limit := l.MaxPerSecond
	if limit <= 0 || limit > MaxFlashesPerSecond {
		limit = MaxFlashesPerSecond
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-time.Second)
	first := 0
	for first < len(l.times) && l.times[first].Before(cut) {
		first++
	}
	l.times = append(l.times[:0], l.times[first:]...)
	if len(l.times) >= limit {
		return false
	}
	l.times = append(l.times, now)
	return true
}

// Flash is a full-screen flash a game triggers: FlashEffect's Amount jumps to a peak and fades to 0 on the platform's frame clock, with no commit per frame.
type Flash struct {
	cx     *core.Scope
	cfg    FlashConfig
	amount *core.AnimatedValue
	run    *core.AnimationRun
}

// NewFlash returns a flash for FlashEffect, at rest, that lasts as long as cx's owner. A saturated red Color, one whose red is 80% or more of its red, green and blue together (the red flash of WCAG 2.3.1 and XAG 118), becomes the gray of the same relative luminance.
func NewFlash(cx *core.Scope, cfg FlashConfig) *Flash {
	cfg.Color = desaturateRed(cfg.Color)
	if cfg.Limiter == nil {
		cfg.Limiter = &FlashLimiter{}
	}
	return &Flash{cx: cx, cfg: cfg, amount: core.NewAnimatedValue(cx, 0)}
}

// Uniforms returns FlashEffect's uniforms, for its place in a View's Uniforms: Amount follows the flash.
func (f *Flash) Uniforms() core.Uniforms {
	return core.Uniforms{"Amount": f.amount, "Color": f.cfg.Color}
}

// Amount returns the node FlashEffect's Amount follows, from 0 at rest up to a Trigger's peak.
func (f *Flash) Amount() core.AnimatedNode { return f.amount }

// Trigger sets Amount to peak, clamped to 0 to 1, and fades it to 0 over fade, easing out, in place of a flash still fading. Under FlashConfig.ReduceFlashes the peak is at most ReducedFlashPeak and the fade at least ReducedFlashFade. It reports whether the flash runs: it does not under reduced motion, nor past the limiter's budget, nor for a peak of 0 or less or a fade of 0 or less, which leave a running flash alone: Stop, not Trigger(0, fade), ends a running flash.
func (f *Flash) Trigger(peak float64, fade time.Duration) bool {
	if !(peak > 0) || fade <= 0 || reduced(f.cx, f.cfg.ReduceMotion) {
		return false
	}
	if !f.cfg.Limiter.Allow(f.cx.Now()) {
		return false
	}
	if f.cfg.ReduceFlashes != nil && core.Untracked(f.cx, f.cfg.ReduceFlashes) {
		peak = min(peak, ReducedFlashPeak)
		fade = max(fade, ReducedFlashFade)
	}
	f.Stop()
	f.amount.Set(min(peak, 1))
	f.run = f.cx.StartAnimation(core.Timing(f.amount, 0, core.TimingConfig{Duration: fade, Easing: core.BezierEasing(core.EaseOut)}), nil)
	return true
}

// desaturateRed returns c, or the gray of c's relative luminance when c is a saturated red, keeping its alpha.
func desaturateRed(c core.Color) core.Color {
	r, g, b, a := c.Components()
	if sum := r + g + b; sum == 0 || r/sum < 0.8 {
		return c
	}
	y := 0.2126*srgbToLinear(r) + 0.7152*srgbToLinear(g) + 0.0722*srgbToLinear(b)
	v := uint32(math.Round(linearToSRGB(y) * 255))
	return core.RGBA(v<<16|v<<8|v, float32(a))
}

// srgbToLinear decodes an sRGB component, 0 to 1, to linear light.
func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// linearToSRGB encodes a linear light component, 0 to 1, to sRGB.
func linearToSRGB(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
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
