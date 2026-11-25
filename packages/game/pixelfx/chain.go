// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/chain.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

import "math"

// Moves: several effects played as one. A cue says which effect, when, where and for how long; a projectile is just an effect whose x is a function of time. It's an ordinary effect from the outside: it plays, recolors and exports.

// Cue is one effect in a chain: FX starts at T (seconds into the move) and is cut off at Until (never when 0), at (X, Y) in local space, or at XFn(t), YFn(t) of the move's time when they are set, mirrored when Flip, with Options laid over the effect's own (its seed is the move's seed plus the cue's index).
type Cue struct {
	FX       *Effect
	T, Until float64
	X, Y     float64
	XFn, YFn func(t float64) float64
	Flip     bool
	Options  map[string]Value
}

type cue struct {
	Cue
	until float64
	opts  Options
	inst  *Player
	over  bool
}

// Chain returns an effect's New for a list of cues.
func Chain(cues []Cue) func(o *Options) Instance {
	return func(o *Options) Instance {
		seed := 1.0
		if v, ok := o.Values["seed"]; ok && v.kind == KindNumber {
			seed = v.num
		}
		list := make([]*cue, len(cues))
		for i, c := range cues {
			values := make(map[string]Value, len(c.Options)+1)
			for k, v := range c.Options {
				values[k] = v
			}
			values["seed"] = Num(seed + float64(i))
			until := c.Until
			if until == 0 {
				until = math.Inf(1)
			}
			list[i] = &cue{Cue: c, until: until, opts: Options{Values: values}}
		}
		t := 0.0
		at := func(v float64, fn func(float64) float64) float64 {
			if fn != nil {
				return fn(t)
			}
			return v
		}
		each := func(p *Painter, layer Layer) {
			frame, tx, ty, sign, cs, sn, fd, lightK := p.Frame, p.TX, p.TY, p.FX, p.CS, p.SN, p.Fade, p.LightK
			for _, c := range list {
				if c.inst == nil || c.over {
					continue
				}
				c.inst.Options.X = tx + sign*at(c.X, c.XFn)
				c.inst.Options.Y = ty + at(c.Y, c.YFn)
				c.inst.Options.Flip = (sign < 0) != c.Flip
				c.inst.Draw(frame, layer)
			}
			// each one places the painter for itself: put it back for whoever draws next
			p.TX, p.TY, p.FX, p.CS, p.SN, p.Fade, p.LightK = tx, ty, sign, cs, sn, fd, lightK
		}
		return &Funcs{
			UpdateFunc: func(dt float64) {
				t += dt
				for _, c := range list {
					if c.over || t < c.T {
						continue
					}
					if c.inst == nil {
						c.inst = NewPlayer(c.FX, c.opts)
					} else {
						c.inst.Update(dt)
					}
					if t >= c.until || (c.inst.Done() && !c.FX.Meta.Loop) {
						c.over = true
					}
				}
			},
			DrawBackFunc: func(p *Painter, _ float64) { each(p, LayerBack) },
			DrawFunc:     func(p *Painter, _ float64) { each(p, LayerFront) },
		}
	}
}

// ShiftKeys reuses an act's keys in a longer move: later by dt, moved along by dx, with over laid over each pose.
func ShiftKeys(keys []Key, dt, dx float64, over Pose) []Key {
	out := make([]Key, len(keys))
	for i, k := range keys {
		p := k.Pose.Merge(over)
		out[i] = Key{T: k.T + dt, Pose: p.With(X.Is(k.Pose.Or(X, 0) + dx)), How: k.How}
	}
	return out
}

// ShiftHits reuses an act's hits in a longer move, later by dt.
func ShiftHits(hits []Hit, dt float64) []Hit {
	out := make([]Hit, len(hits))
	for i, h := range hits {
		h.T += dt
		out[i] = h
	}
	return out
}
