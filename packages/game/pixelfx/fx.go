// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/fx.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Meta is what an effect says about itself: its Name and Desc, Tags, its frame Size and the Origin in it that its local (0, 0) sits at, its Duration in seconds, whether it Loops, the FPS its sheets play at, the backdrop it previews on (BG: "dark", "ground", "sky", "light" or "black"), the stock Actor it is drawn on (0 none, 1 the 12 x 19 one, 2 the 24 x 38 one), and its Options with their defaults, in order.
type Meta struct {
	Name     string
	Desc     string
	Tags     []string
	Size     [2]int
	Origin   [2]int
	Duration float64
	Loop     bool
	FPS      int
	BG       string
	Actor    int
	Options  []Option
}

// Option is one of an effect's options and its default.
type Option struct {
	Name    string
	Default Value
}

// Default returns the default of the option called name and whether the effect has it.
func (m *Meta) Default(name string) (Value, bool) {
	for _, o := range m.Options {
		if o.Name == name {
			return o.Default, true
		}
	}
	return Value{}, false
}

// ValueKind is what an option holds.
type ValueKind uint8

const (
	// KindNone is no value.
	KindNone ValueKind = iota
	// KindNumber is a number, such as a seed, a size or a speed.
	KindNumber
	// KindBool is on or off.
	KindBool
	// KindColor is one color.
	KindColor
	// KindRamp is a list of colors, dark to bright: a palette.
	KindRamp
	// KindText is a string, such as the words a UI effect shows.
	KindText
	// KindNumbers is a list of numbers.
	KindNumbers
)

// String names the kind as the game pixelfx command and its errors do.
func (k ValueKind) String() string {
	if k > KindNumbers {
		return fmt.Sprintf("ValueKind(%d)", int(k))
	}
	return [...]string{"none", "number", "bool", "color", "ramp", "text", "numbers"}[k]
}

// Value is an option's value: a number, a bool, a color, a ramp of colors, text or a list of numbers.
type Value struct {
	kind   ValueKind
	num    float64
	text   string
	colors []uint32
	nums   []float64
}

// Num returns a number value.
func Num(x float64) Value { return Value{kind: KindNumber, num: x} }

// Bool returns a bool value.
func Bool(b bool) Value {
	v := Value{kind: KindBool}
	if b {
		v.num = 1
	}
	return v
}

// Color returns a color value from "#rgb", "#rrggbb" or "#rrggbbaa" (alpha ignored), panicking on anything else as Hex does.
func Color(hex string) Value { return Value{kind: KindColor, colors: []uint32{Hex(hex)}} }

// ColorOf returns a color value.
func ColorOf(c uint32) Value { return Value{kind: KindColor, colors: []uint32{c}} }

// Palette returns a ramp value from hex colors (dark to bright), panicking on a malformed one as Hex does.
func Palette(hex ...string) Value { return Value{kind: KindRamp, colors: Ramp(hex...)} }

// RampOf returns a ramp value of colors.
func RampOf(colors []uint32) Value { return Value{kind: KindRamp, colors: slices.Clone(colors)} }

// Text returns a text value.
func Text(s string) Value { return Value{kind: KindText, text: s} }

// Nums returns a list-of-numbers value.
func Nums(xs ...float64) Value { return Value{kind: KindNumbers, nums: slices.Clone(xs)} }

// Kind returns what v holds.
func (v Value) Kind() ValueKind { return v.kind }

// String writes v as the game pixelfx command reads it: a number, true or false, #rrggbb, a comma-separated list of colors or numbers, or the text itself.
func (v Value) String() string {
	switch v.kind {
	case KindNumber:
		return formatNum(v.num)
	case KindBool:
		if v.num != 0 {
			return "true"
		}
		return "false"
	case KindColor:
		return ToHex(v.colors[0])
	case KindRamp:
		parts := make([]string, len(v.colors))
		for i, c := range v.colors {
			parts[i] = ToHex(c)
		}
		return strings.Join(parts, ",")
	case KindText:
		return v.text
	case KindNumbers:
		parts := make([]string, len(v.nums))
		for i, n := range v.nums {
			parts[i] = formatNum(n)
		}
		return strings.Join(parts, ",")
	}
	return ""
}

func formatNum(x float64) string { return fmt.Sprint(x) }

// ParseValue reads s as a value of kind, as Value.String writes it: a number must be the whole of s (surrounding space aside), a color is #rgb, #rrggbb or #rrggbbaa (whose alpha is ignored), and a ramp's colors and a list's numbers are separated by commas.
func ParseValue(kind ValueKind, s string) (Value, error) {
	s = strings.TrimSpace(s)
	switch kind {
	case KindNumber:
		x, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return Value{}, fmt.Errorf("%q is not a number", s)
		}
		return Num(x), nil
	case KindBool:
		switch s {
		case "true":
			return Bool(true), nil
		case "false":
			return Bool(false), nil
		}
		return Value{}, fmt.Errorf("%q is not true or false", s)
	case KindColor:
		c, err := ParseHex(s)
		if err != nil {
			return Value{}, err
		}
		return ColorOf(c), nil
	case KindRamp:
		var cs []uint32
		for _, part := range strings.Split(s, ",") {
			c, err := ParseHex(strings.TrimSpace(part))
			if err != nil {
				return Value{}, err
			}
			cs = append(cs, c)
		}
		return Value{kind: KindRamp, colors: cs}, nil
	case KindText:
		return Text(s), nil
	case KindNumbers:
		var xs []float64
		for _, part := range strings.Split(s, ",") {
			v, err := ParseValue(KindNumber, part)
			if err != nil {
				return Value{}, err
			}
			xs = append(xs, v.num)
		}
		return Value{kind: KindNumbers, nums: xs}, nil
	}
	return Value{}, fmt.Errorf("no value of kind %s", kind)
}

// Options are how an effect is played: X and Y place its origin in frame space, Flip mirrors it, Angle turns it (radians), Light scales its glow (0 is read as 1; below 0 draws no light), Actor is the character it wraps (the stock one its meta names when nil), and Values are its own options by name, such as "seed" and "palette", over the defaults its meta gives.
type Options struct {
	X, Y   float64
	Flip   bool
	Angle  float64
	Light  float64
	Actor  *Actor
	Values map[string]Value
}

// Set returns o with the option called name set to v. Set checks nothing: when the effect is played, a value of a kind its meta does not take for that option, or an empty ramp, is ignored and the default kept, and a name its meta does not give is passed on as it is. Check a value against Meta.Default first to report a mistake.
func (o Options) Set(name string, v Value) Options {
	m := make(map[string]Value, len(o.Values)+1)
	for k, x := range o.Values {
		m[k] = x
	}
	m[name] = v
	o.Values = m
	return o
}

// Value returns the value of the option called name and whether it is set.
func (o *Options) Value(name string) (Value, bool) {
	v, ok := o.Values[name]
	return v, ok
}

// Num returns the number option called name, 0 when it is not a number.
func (o *Options) Num(name string) float64 {
	v := o.Values[name]
	if v.kind == KindNumber {
		return v.num
	}
	return 0
}

// Bool returns the option called name as a bool: true when it is on (or a number that is not 0).
func (o *Options) Bool(name string) bool {
	v := o.Values[name]
	return (v.kind == KindBool || v.kind == KindNumber) && v.num != 0
}

// Color returns the color option called name, 0 when it is not a color.
func (o *Options) Color(name string) uint32 {
	v := o.Values[name]
	if v.kind == KindColor || (v.kind == KindRamp && len(v.colors) > 0) {
		return v.colors[0]
	}
	return 0
}

// Ramp returns the ramp option called name, nil when it is not a ramp. Don't change the colors it returns.
func (o *Options) Ramp(name string) []uint32 {
	v := o.Values[name]
	if v.kind == KindRamp || v.kind == KindColor {
		return v.colors
	}
	return nil
}

// Text returns the text option called name.
func (o *Options) Text(name string) string { return o.Values[name].text }

// Numbers returns the list-of-numbers option called name. Don't change what it returns.
func (o *Options) Numbers(name string) []float64 { return o.Values[name].nums }

// Seed returns the seed option.
func (o *Options) Seed() float64 { return o.Num("seed") }

// Palette returns the palette option.
func (o *Options) Palette() []uint32 { return o.Ramp("palette") }

// resolve returns an effect's options: its defaults with o on top. A value for an option the meta names replaces the default only when it is of the default's kind, a number standing for a bool, or a color standing for a ramp (as a ramp of that one color); otherwise, and for an empty ramp, the default stays, so an effect never reads a zero where its meta promises a value. Any ramp of a different length than its default is resampled to the default's length, so effects can index steps by number whatever a caller passes. Values under names the meta does not give are kept as they are.
func resolve(meta *Meta, o Options) *Options {
	out := o
	out.Values = make(map[string]Value, len(meta.Options)+len(o.Values))
	for _, d := range meta.Options {
		out.Values[d.Name] = d.Default
	}
	for k, v := range o.Values {
		if d, ok := meta.Default(k); ok {
			switch {
			case v.kind == d.kind:
			case d.kind == KindBool && v.kind == KindNumber:
			case d.kind == KindRamp && v.kind == KindColor:
				v = Value{kind: KindRamp, colors: v.colors}
			default:
				continue
			}
			if d.kind == KindRamp {
				if len(v.colors) == 0 {
					continue
				}
				if len(v.colors) != len(d.colors) {
					v = Value{kind: KindRamp, colors: Fit(v.colors, len(d.colors))}
				}
			}
		}
		out.Values[k] = v
	}
	if meta.Actor != 0 && out.Actor == nil {
		out.Actor = StockActor(meta.Actor)
	}
	return &out
}

// Instance is an effect being played: Update moves it on by dt seconds, DrawBack draws what goes behind a character and Draw what goes in front, both at the effect's own time t. Effects that are pure functions of t leave Update empty.
type Instance interface {
	Update(dt float64)
	DrawBack(p *Painter, t float64)
	Draw(p *Painter, t float64)
}

// Funcs is an Instance made of functions; a nil one does nothing.
type Funcs struct {
	UpdateFunc   func(dt float64)
	DrawBackFunc func(p *Painter, t float64)
	DrawFunc     func(p *Painter, t float64)
}

// Update implements Instance.
func (f *Funcs) Update(dt float64) {
	if f.UpdateFunc != nil {
		f.UpdateFunc(dt)
	}
}

// DrawBack implements Instance.
func (f *Funcs) DrawBack(p *Painter, t float64) {
	if f.DrawBackFunc != nil {
		f.DrawBackFunc(p, t)
	}
}

// Draw implements Instance.
func (f *Funcs) Draw(p *Painter, t float64) {
	if f.DrawFunc != nil {
		f.DrawFunc(p, t)
	}
}

// Effect is one pixelfx effect: its ID ("<category>/<name>", set by Register), its Meta, New, which starts playing it with resolved options, and, for one a character acts out, its Act and ActorAlpha, how visible it wants the stand-in character at time t (fully when nil).
type Effect struct {
	ID         string
	Meta       Meta
	New        func(o *Options) Instance
	Act        *Act
	ActorAlpha func(t float64, o *Options) float64
}

// actorAlpha returns how visible e wants the stand-in character at t.
func (e *Effect) actorAlpha(t float64, o *Options) float64 {
	if e.ActorAlpha == nil {
		return 1
	}
	return e.ActorAlpha(t, o)
}

var registry = struct {
	sync.RWMutex
	byID map[string]*Effect
}{byID: map[string]*Effect{}}

// Register adds e to the registry as id, "<category>/<name>" such as "fire/torch", and sets e.ID. A category package registers its effects when it is imported. It panics when id is malformed or taken.
func Register(id string, e *Effect) {
	cat, name, ok := strings.Cut(id, "/")
	if !ok || cat == "" || name == "" || strings.Contains(name, "/") {
		panic("pixelfx: effect id " + id + " is not <category>/<name>")
	}
	registry.Lock()
	defer registry.Unlock()
	if _, taken := registry.byID[id]; taken {
		panic("pixelfx: effect " + id + " registered twice")
	}
	e.ID = id
	registry.byID[id] = e
}

// Lookup returns the effect registered as id.
func Lookup(id string) (*Effect, bool) {
	registry.RLock()
	defer registry.RUnlock()
	e, ok := registry.byID[id]
	return e, ok
}

// IDs returns the ids of every registered effect, sorted.
func IDs() []string {
	registry.RLock()
	defer registry.RUnlock()
	ids := make([]string, 0, len(registry.byID))
	for id := range registry.byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// fade is how long a stopped loop takes to fade out, in seconds.
const fade = 0.3

// Player is a live instance of an effect: pixelfx's createFx.
type Player struct {
	// Effect is what is playing.
	Effect *Effect
	// Options are its resolved options; changing X, Y or Flip moves it.
	Options *Options

	inst      Instance
	t, stopAt float64
}

// NewPlayer starts playing e with o, over the defaults its meta gives: see Options.Set for which values it takes.
func NewPlayer(e *Effect, o Options) *Player {
	opts := resolve(&e.Meta, o)
	return &Player{Effect: e, Options: opts, inst: e.New(opts), stopAt: math.Inf(1)}
}

// T returns how long it has played, in seconds.
func (f *Player) T() float64 { return f.t }

// Update moves it on by dt seconds. Simulations step at 120 Hz or finer whatever the caller's frame rate, so they look the same everywhere. A negative, NaN or infinite dt does nothing. Every 1/120 s of dt costs a step, so a caller resuming after a long pause should clamp dt first.
func (f *Player) Update(dt float64) {
	if !(dt >= 0) || math.IsInf(dt, 1) {
		return
	}
	f.t += dt
	n := int(math.Max(1, math.Ceil(dt*120-1e-9)))
	for range n {
		f.inst.Update(dt / float64(n))
	}
}

// Draw draws it into frame: LayerBack what goes behind a character, LayerFront what goes in front, LayerBoth both.
func (f *Player) Draw(frame *Frame, layer Layer) {
	p := frame.Painter()
	o := f.Options
	p.Place(o.X, o.Y, o.Flip, o.Angle)
	p.Fade = 1
	if !math.IsInf(f.stopAt, 1) {
		p.Fade = Clamp(1 - (f.t-f.stopAt)/fade)
	}
	p.LightK = o.Light
	if p.LightK == 0 {
		p.LightK = 1
	}
	m := &f.Effect.Meta
	D := m.Duration
	tt := math.Min(f.t, D)
	if m.Loop {
		tt = math.Mod(f.t, D)
	}
	if layer != LayerFront {
		p.Layer = LayerBack
		f.inst.DrawBack(p, tt)
	}
	if layer != LayerBack {
		p.Layer = LayerFront
		f.inst.Draw(p, tt)
	}
}

// Stop lets it end: a loop fades out, a one-shot is cut short.
func (f *Player) Stop() {
	if math.IsInf(f.stopAt, 1) {
		f.stopAt = f.t
	}
}

// Done reports whether it has ended.
func (f *Player) Done() bool {
	return f.t-f.stopAt >= fade || (!f.Effect.Meta.Loop && f.t >= f.Effect.Meta.Duration)
}
