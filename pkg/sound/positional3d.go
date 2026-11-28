package sound

import (
	"math"

	"github.com/nsgo-dev/nsgo/audio"
)

// DefaultRearGain is the default head-shadow gain multiplier a Listener3D applies to a sound
// directly behind the listener when Listener3D.RearGain is zero, negative, or NaN.
const DefaultRearGain float32 = 0.7

// Vec3 is a 3D point or direction in right-handed Y-up space (+X right, +Y up, -Z forward),
// matching math3d.Vec3 and scene3d.Camera so callers can convert directly with sound.Vec3(v).
type Vec3 struct {
	X, Y, Z float32
}

// V3 returns a Vec3 with the given coordinates.
func V3(x, y, z float32) Vec3 { return Vec3{X: x, Y: y, Z: z} }

// Listener3D hears effects placed in a right-handed Y-up 3D world (+X right, +Y up, -Z forward)
// from where the 3D camera looks: an effect to the left of the camera plays panned left, one to
// the right panned right, one further away plays quieter, and one behind the camera plays quieter
// than an equidistant effect in front while still panned to its side.
//
// Its zero value sits at the origin facing -Z with +Y up, disables distance attenuation, pans by
// lateral azimuth, and attenuates effects behind the listener using DefaultRearGain.
type Listener3D struct {
	// Position is where the listener is in the world, such as sound.Vec3(cam.Pos).
	Position Vec3
	// Forward is the direction the listener faces, such as sound.Vec3(cam.Forward()).
	// A zero or non-finite Forward defaults to (0, 0, -1).
	Forward Vec3
	// Up is the listener's up direction, such as sound.Vec3(cam.Up()).
	// A zero, non-finite, or collinear Up defaults to (0, 1, 0) (or (0, 0, 1) when facing along ±Y).
	Up Vec3
	// Near is the distance up to which an effect plays at full distance gain; a negative value or NaN counts as 0.
	Near float32
	// Far is the distance at and beyond which an effect is silent. A Far not greater than Near,
	// infinite, or NaN turns distance attenuation off.
	Far float32
	// Rolloff is how distance gain falls between Near and Far.
	Rolloff Rolloff
	// RolloffFactor shapes the steepness of RolloffInverse (default 1 when <= 0 or non-finite)
	// and RolloffExponential (default 2 when <= 0 or non-finite). Ignored by RolloffLinear.
	RolloffFactor float32
	// RearGain is the gain multiplier for a sound directly behind the listener (dot(dir, forward) == -1),
	// modeling head-shadow attenuation. Effects in front or at the sides receive a multiplier of 1,
	// and effects behind interpolate linearly from 1 at the lateral plane down to RearGain directly behind.
	// Zero, negative, or NaN uses DefaultRearGain (0.7); values above 1 clamp to 1 (disabling rear attenuation).
	RearGain float32
	// PanWidth, when positive and finite, computes pan as the source's lateral offset along the
	// listener's right axis divided by PanWidth, clamped to [-1, 1], matching Listener2D.
	// When 0, negative, or non-finite, pan uses the 3D directional azimuth dot(dir, right),
	// which is 0 directly in front, behind, above, or below, -1 directly left, and +1 directly right.
	PanWidth float32
}

// SetPose updates l's Position, Forward, and Up without changing its attenuation or pan settings.
func (l *Listener3D) SetPose(pos, forward, up Vec3) {
	if l == nil {
		return
	}
	l.Position = pos
	l.Forward = forward
	l.Up = up
}

// LookAt orients l at its current Position toward target with world up direction up.
// If target equals Position or is non-finite, Forward is left unchanged.
func (l *Listener3D) LookAt(target, up Vec3) {
	if l == nil {
		return
	}
	dx := float64(target.X) - float64(l.Position.X)
	dy := float64(target.Y) - float64(l.Position.Y)
	dz := float64(target.Z) - float64(l.Position.Z)
	if fwd, ok := norm3(dx, dy, dz); ok {
		l.Forward = Vec3{X: float32(fwd.x), Y: float32(fwd.y), Z: float32(fwd.z)}
	}
	l.Up = up
}

// Spatial3DOptions customizes 3D spatialization for a single sound source, such as per-source
// distance attenuation overrides or a directional emission cone. Its zero value represents an
// omnidirectional point source using the listener's attenuation settings.
type Spatial3DOptions struct {
	// Near overrides Listener3D.Near when > 0 and finite.
	Near float32
	// Far overrides Listener3D.Far when > 0 and finite.
	Far float32
	// HasRolloff selects Rolloff over Listener3D.Rolloff when true.
	HasRolloff bool
	// Rolloff overrides Listener3D.Rolloff when HasRolloff is true.
	Rolloff Rolloff
	// RolloffFactor overrides Listener3D.RolloffFactor when > 0 and finite.
	RolloffFactor float32

	// SourceForward is the direction a directional sound source emits toward.
	// When zero or non-finite, or when OuterConeAngle <= 0, the source is omnidirectional.
	SourceForward Vec3
	// InnerConeAngle is the full cone angle in radians (in [0, 2*Pi]) within which the directional
	// source plays at full cone gain (1). Clamped to [0, OuterConeAngle].
	InnerConeAngle float32
	// OuterConeAngle is the full cone angle in radians (in [0, 2*Pi]) beyond which the directional
	// source plays at OuterGain. Between InnerConeAngle and OuterConeAngle, cone gain interpolates
	// linearly from 1 to OuterGain. When <= 0 or NaN, directional cone attenuation is disabled.
	OuterConeAngle float32
	// OuterGain is the cone gain multiplier outside OuterConeAngle, clamped to [0, 1].
	OuterGain float32
}

// Spatial3D holds the computed 3D spatialization for a source relative to a Listener3D.
type Spatial3D struct {
	// Gain is the combined spatial attenuation in [0, 1] (distance * rear * source cone).
	Gain float32
	// Pan is the stereo pan in [-1, 1] (-1 full left, 0 centered, +1 full right).
	Pan float32
	// Distance is the Euclidean distance from the listener to the source in world units (0 when NaN).
	Distance float32
}

// Spatialize3D computes 3D spatialization for a source at pos relative to l with opts.
func Spatialize3D(l Listener3D, pos Vec3, opts Spatial3DOptions) Spatial3D {
	return l.Spatialize(pos, opts)
}

// Pan returns where an effect at pos plays from, -1 for fully left through 0 for centered to
// 1 for fully right, as audio.Voice.Pan takes it. Effects at the listener, straight ahead,
// straight behind, straight above/below, or with a NaN coordinate play centered.
func (l Listener3D) Pan(pos Vec3) float32 {
	return l.Spatialize(pos, Spatial3DOptions{}).Pan
}

// Gain returns the combined spatial gain for an effect at pos in [0, 1]: distance attenuation
// from Near to Far times rear head-shadow attenuation when behind the listener. A pos with a
// NaN coordinate is silent.
func (l Listener3D) Gain(pos Vec3) float32 {
	return l.Spatialize(pos, Spatial3DOptions{}).Gain
}

// Spatialize computes distance attenuation, rear head-shadow attenuation, optional directional
// source cone attenuation, and stereo pan for a source at pos relative to l.
func (l Listener3D) Spatialize(pos Vec3, opts Spatial3DOptions) Spatial3D {
	dx := float64(pos.X) - float64(l.Position.X)
	dy := float64(pos.Y) - float64(l.Position.Y)
	dz := float64(pos.Z) - float64(l.Position.Z)
	if math.IsNaN(dx) || math.IsNaN(dy) || math.IsNaN(dz) {
		return Spatial3D{}
	}

	d, dir, hasDir, isInf := offset3(dx, dy, dz)
	right, _, fwd := l.basis()

	// Stereo pan in [-1, 1].
	var pan float64
	if hasDir {
		pw := float64(l.PanWidth)
		if pw > 0 && !math.IsInf(pw, 1) {
			if isInf {
				rd := dir.dot(right)
				switch {
				case rd > 0:
					pan = 1
				case rd < 0:
					pan = -1
				}
			} else {
				pan = (dx*right.x + dy*right.y + dz*right.z) / pw
			}
		} else {
			pan = dir.dot(right)
		}
		if math.IsNaN(pan) {
			pan = 0
		} else {
			pan = min(max(pan, -1), 1)
		}
	}

	// Distance attenuation in [0, 1].
	near, far, rolloff, factor, attenOn := l.effectiveAttenuation(opts)
	distGain := 1.0
	if attenOn {
		switch {
		case d <= near:
			distGain = 1
		case d >= far:
			distGain = 0
		default:
			distGain = rolloffGain(d, near, far, rolloff, factor)
		}
	}

	// Rear head-shadow attenuation when behind the listener.
	rearFactor := 1.0
	if hasDir && distGain > 0 {
		fwdDot := dir.dot(fwd)
		if fwdDot < 0 {
			rg := l.effectiveRearGain()
			t := min(max(-fwdDot, 0), 1)
			rearFactor = 1 - (1-rg)*t
		}
	}

	// Optional directional sound source cone attenuation.
	coneFactor := 1.0
	if hasDir && distGain > 0 {
		coneFactor = sourceConeGain(dir, opts)
	}

	gain := distGain * rearFactor * coneFactor
	if math.IsNaN(gain) {
		gain = 0
	} else {
		gain = min(max(gain, 0), 1)
	}

	return Spatial3D{
		Gain:     float32(gain),
		Pan:      float32(pan),
		Distance: float32(d),
	}
}

// Voice returns v with its volume scaled by l.Gain(pos) and its pan set to l.Pan(pos).
// Where the effect is silent, its volume is 0 and it is Muted, since audio plays a volume of 0 as full volume.
func (l Listener3D) Voice(pos Vec3, v audio.Voice) audio.Voice {
	return l.VoiceWithOptions(pos, v, Spatial3DOptions{})
}

// VoiceWithOptions returns v with its volume scaled by l.Spatialize(pos, opts).Gain and its pan
// set to l.Spatialize(pos, opts).Pan. Where the effect is silent, its volume is 0 and it is Muted.
func (l Listener3D) VoiceWithOptions(pos Vec3, v audio.Voice, opts Spatial3DOptions) audio.Voice {
	sp := l.Spatialize(pos, opts)
	v.Volume = voiceVolume(v.Volume) * sp.Gain
	if !(v.Volume > 0) {
		v.Muted = true
	}
	v.Pan = sp.Pan
	return v
}

func (l Listener3D) effectiveRearGain() float64 {
	rg := float64(l.RearGain)
	if !(rg > 0) || math.IsNaN(rg) {
		return float64(DefaultRearGain)
	}
	if rg > 1 {
		return 1
	}
	return rg
}

func (l Listener3D) effectiveAttenuation(opts Spatial3DOptions) (near, far float64, rolloff Rolloff, factor float64, ok bool) {
	near = float64(l.Near)
	if on := float64(opts.Near); on > 0 && !math.IsInf(on, 1) {
		near = on
	}
	if !(near > 0) || math.IsInf(near, 1) {
		near = 0
	}

	far = float64(l.Far)
	if of := float64(opts.Far); of > 0 && !math.IsInf(of, 1) {
		far = of
	}
	if !(far > near) || math.IsInf(far, 1) {
		return 0, 0, l.Rolloff, 0, false
	}

	rolloff = l.Rolloff
	if opts.HasRolloff {
		rolloff = opts.Rolloff
	}

	factor = float64(l.RolloffFactor)
	if of := float64(opts.RolloffFactor); of > 0 && !math.IsInf(of, 1) {
		factor = of
	}
	return near, far, rolloff, factor, true
}

// basis returns a guaranteed-finite, right-handed orthonormal frame (right, up, forward) for l.
func (l Listener3D) basis() (right, up, fwd vec3f64) {
	var ok bool
	fwd, ok = norm3(float64(l.Forward.X), float64(l.Forward.Y), float64(l.Forward.Z))
	if !ok {
		fwd = vec3f64{0, 0, -1}
	}

	rawUp, ok := norm3(float64(l.Up.X), float64(l.Up.Y), float64(l.Up.Z))
	if !ok {
		rawUp = vec3f64{0, 1, 0}
	}

	right, ok = normCross(fwd, rawUp)
	if !ok {
		// Forward and Up are collinear: fall back to +Y, then +Z.
		right, ok = normCross(fwd, vec3f64{0, 1, 0})
		if !ok {
			right, _ = normCross(fwd, vec3f64{0, 0, 1})
		}
	}
	up, _ = normCross(right, fwd)
	return right, up, fwd
}

// sourceConeGain returns the directional emission cone gain in [0, 1].
// dir is the unit vector from listener to source, so -dir points from source to listener.
func sourceConeGain(dir vec3f64, opts Spatial3DOptions) float64 {
	outer := float64(opts.OuterConeAngle)
	if !(outer > 0) || math.IsNaN(outer) {
		return 1
	}
	srcFwd, ok := norm3(float64(opts.SourceForward.X), float64(opts.SourceForward.Y), float64(opts.SourceForward.Z))
	if !ok {
		return 1
	}
	if outer > 2*math.Pi {
		outer = 2 * math.Pi
	}
	inner := float64(opts.InnerConeAngle)
	if !(inner > 0) || math.IsNaN(inner) {
		inner = 0
	} else if inner > outer {
		inner = outer
	}

	outerGain := float64(opts.OuterGain)
	if !(outerGain > 0) || math.IsNaN(outerGain) {
		outerGain = 0
	} else if outerGain > 1 {
		outerGain = 1
	}

	toListener := vec3f64{-dir.x, -dir.y, -dir.z}
	cosAngle := min(max(srcFwd.dot(toListener), -1), 1)
	angle := math.Acos(cosAngle)

	innerHalf := inner * 0.5
	outerHalf := outer * 0.5
	switch {
	case angle <= innerHalf:
		return 1
	case angle >= outerHalf:
		return outerGain
	default:
		t := (angle - innerHalf) / (outerHalf - innerHalf)
		return min(max(1+(outerGain-1)*t, 0), 1)
	}
}

type vec3f64 struct{ x, y, z float64 }

func (a vec3f64) dot(b vec3f64) float64 {
	return a.x*b.x + a.y*b.y + a.z*b.z
}

func (a vec3f64) cross(b vec3f64) vec3f64 {
	return vec3f64{
		x: a.y*b.z - a.z*b.y,
		y: a.z*b.x - a.x*b.z,
		z: a.x*b.y - a.y*b.x,
	}
}

func norm3(x, y, z float64) (vec3f64, bool) {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) ||
		math.IsInf(x, 0) || math.IsInf(y, 0) || math.IsInf(z, 0) {
		return vec3f64{}, false
	}
	m := max(math.Abs(x), max(math.Abs(y), math.Abs(z)))
	if m <= 1e-12 {
		return vec3f64{}, false
	}
	sx, sy, sz := x/m, y/m, z/m
	n := math.Sqrt(sx*sx + sy*sy + sz*sz)
	if !(n > 0) || math.IsNaN(n) {
		return vec3f64{}, false
	}
	return vec3f64{sx / n, sy / n, sz / n}, true
}

func normCross(a, b vec3f64) (vec3f64, bool) {
	c := a.cross(b)
	return norm3(c.x, c.y, c.z)
}

// offset3 computes overflow/underflow-safe Euclidean distance and unit direction for (dx, dy, dz),
// assuming none of the components is NaN.
func offset3(dx, dy, dz float64) (dist float64, dir vec3f64, hasDir, isInf bool) {
	m := max(math.Abs(dx), max(math.Abs(dy), math.Abs(dz)))
	if m == 0 {
		return 0, vec3f64{}, false, false
	}
	if math.IsInf(m, 1) {
		var ix, iy, iz float64
		if math.IsInf(dx, 1) {
			ix = 1
		} else if math.IsInf(dx, -1) {
			ix = -1
		}
		if math.IsInf(dy, 1) {
			iy = 1
		} else if math.IsInf(dy, -1) {
			iy = -1
		}
		if math.IsInf(dz, 1) {
			iz = 1
		} else if math.IsInf(dz, -1) {
			iz = -1
		}
		n := math.Sqrt(ix*ix + iy*iy + iz*iz)
		return math.Inf(1), vec3f64{ix / n, iy / n, iz / n}, true, true
	}
	sx, sy, sz := dx/m, dy/m, dz/m
	n := math.Sqrt(sx*sx + sy*sy + sz*sz)
	return m * n, vec3f64{sx / n, sy / n, sz / n}, true, false
}

// PlayAt3D plays the effect at pos in the 3D world as l hears it: panned by l.Pan (adjusted by the
// mixer's mono and balance settings) and at v's volume times l.Gain, then the group's and the master
// volume, as Play does, and reports whether it played. It plays nothing where the effect is silent,
// beyond l's Far, and replaces v's pan. The pan and volume hold for the playback's whole length;
// use Effect.Play3D with Mixer.SetListener3D for playbacks whose spatial gain tracks a moving camera
// or moving source.
func (s *Effect) PlayAt3D(l Listener3D, pos Vec3, v audio.Voice) bool {
	return s.PlayAt3DWith(l, pos, v, PlayOptions{})
}

// PlayAt3DWith behaves like PlayAt3D while applying per-call concurrency, owner, and priority overrides from opts.
// Distance from l.Position to pos is recorded for ResolveFarthest concurrency groups.
func (s *Effect) PlayAt3DWith(l Listener3D, pos Vec3, v audio.Voice, opts PlayOptions) bool {
	if s == nil || s.m == nil || s.sound == nil {
		return false
	}
	sp := l.Spatialize(pos, Spatial3DOptions{})
	v.Volume = voiceVolume(v.Volume) * sp.Gain
	if !(v.Volume > 0) {
		return false
	}
	v.Pan = sp.Pan
	_, ok := s.playInternal(v, sp.Distance, nil, opts)
	if ok {
		s.emitCaption(sp.Pan)
	}
	return ok
}

// PlayAt3D picks the cue's next variant, as Play does, and plays it at pos in the 3D world as l
// hears it, as Effect.PlayAt3D does: at the variant's volume times l.Gain, panned by l.Pan, at
// the variant's pitch. It reports the pick and whether it played; beyond l's Far it plays nothing,
// though the pick still counts towards the cue's repeat avoidance.
func (c *Cue) PlayAt3D(l Listener3D, pos Vec3) (CuePick, bool) {
	return c.PlayAt3DWith(l, pos, PlayOptions{})
}

// PlayAt3DWith behaves like PlayAt3D while applying per-call concurrency, owner, and priority overrides from opts.
func (c *Cue) PlayAt3DWith(l Listener3D, pos Vec3, opts PlayOptions) (CuePick, bool) {
	if c == nil {
		return CuePick{Index: -1}, false
	}
	pick := c.Pick()
	if !pick.Valid() || pick.Clip.Effect == nil {
		return pick, false
	}
	vol := float32(pick.Volume)
	if vol <= 0 {
		return pick, false
	}
	return pick, pick.Clip.Effect.PlayAt3DWith(l, pos, audio.Voice{Volume: vol, Rate: float32(pick.Pitch)}, c.mergePlayOptions(pick.Clip.Effect, opts))
}

// SetListener3D sets the mixer's active 3D listener and refreshes every live Playback3D started
// through Effect.Play3D or Cue.Play3D: each playback recomputes its 3D spatialization against l
// and updates its underlying audio.Playback volume immediately (respecting master/group volumes,
// ducking, pause, suspend, and background muting).
//
// Because NSGo's audio.Playback cannot change stereo pan after a playback starts, Pan() on a live
// Playback3D reflects the latest computed pan while the backend's active pan stays at its initial
// value; live distance, rear, and source-cone attenuation update continuously via SetVolume.
func (m *Mixer) SetListener3D(l Listener3D) {
	m.listener3D = l
	for s := range m.sounds {
		if s.prune() == 0 {
			delete(m.sounds, s)
			continue
		}
		g := m.gain(s.Group)
		for _, p := range s.live {
			if p.spatial == nil {
				continue
			}
			p.spatial.spatial = l.Spatialize(p.spatial.pos, p.spatial.opts)
			p.playback.SetVolume(p.effectiveVolume(g))
			setPlaybackPan(p.playback, m.effectivePan(p.spatial.spatial.Pan))
		}
	}
	for _, f := range m.fading {
		if f.spatial == nil || ended(m.cx, f.playback) {
			continue
		}
		f.spatial.spatial = l.Spatialize(f.spatial.pos, f.spatial.opts)
		g := m.gain(f.effect.Group)
		f.playback.SetVolume(f.effectiveVolume(g))
		setPlaybackPan(f.playback, m.effectivePan(f.spatial.spatial.Pan))
	}
}

// Listener3D returns the mixer's active 3D listener.
func (m *Mixer) Listener3D() Listener3D { return m.listener3D }

// Playback3D is a live 3D-positioned playback started by Effect.Play3D or Cue.Play3D.
// Moving the source via SetPosition, updating its directional cone via SetOptions, or moving the
// listener via Mixer.SetListener3D recomputes its spatialization and updates its underlying
// audio.Playback volume live while honoring the mixer's group volume, master volume, ducking, pause,
// suspend, and background states.
//
// All methods on a nil or ended Playback3D are safe no-ops.
type Playback3D struct {
	effect     *Effect
	playback   *audio.Playback
	entry      *playback
	fading     *fadingPlayback
	baseVolume float32
	pos        Vec3
	opts       Spatial3DOptions
	spatial    Spatial3D
}

// Play3D starts a live 3D playback of s at pos using s's mixer's Listener3D and opts, returning a
// Playback3D handle. Like Effect.Play, it starts nothing when the voice is muted, effective gain
// is 0 (beyond Far, outside a zero-gain cone, zero group/master volume, paused, suspended, or in
// the background), or refused by a concurrency group, retrigger cooldown, or global voice budget,
// returning an ended Playback3D so silent or refused sources never take voice slots or count against s.Limit.
func (s *Effect) Play3D(pos Vec3, v audio.Voice, opts Spatial3DOptions) *Playback3D {
	return s.Play3DWith(pos, v, opts, PlayOptions{})
}

// Play3DWith behaves like Play3D while applying per-call concurrency, owner, and priority overrides from playOpts.
func (s *Effect) Play3DWith(pos Vec3, v audio.Voice, opts Spatial3DOptions, playOpts PlayOptions) *Playback3D {
	if s == nil || s.m == nil || s.sound == nil {
		return &Playback3D{pos: pos, opts: opts}
	}
	baseVol := voiceVolume(v.Volume)
	if v.Muted {
		baseVol = 0
	}
	sp := s.m.listener3D.Spatialize(pos, opts)
	pb := &Playback3D{
		effect:     s,
		baseVolume: baseVol,
		pos:        pos,
		opts:       opts,
		spatial:    sp,
	}
	s.playInternal(v, sp.Distance, pb, playOpts)
	return pb
}

// Play3D picks the cue's next variant, as Play does, and starts a live 3D playback of its effect
// at pos using that effect's mixer's Listener3D and opts. It returns the pick and the Playback3D
// handle (whose Ended reports true when nothing played).
func (c *Cue) Play3D(pos Vec3, opts Spatial3DOptions) (CuePick, *Playback3D) {
	return c.Play3DWith(pos, opts, PlayOptions{})
}

// Play3DWith behaves like Play3D while applying per-call concurrency, owner, and priority overrides from playOpts.
func (c *Cue) Play3DWith(pos Vec3, opts Spatial3DOptions, playOpts PlayOptions) (CuePick, *Playback3D) {
	if c == nil {
		return CuePick{Index: -1}, &Playback3D{pos: pos, opts: opts}
	}
	pick := c.Pick()
	if !pick.Valid() || pick.Clip.Effect == nil {
		return pick, &Playback3D{pos: pos, opts: opts}
	}
	vol := float32(pick.Volume)
	if vol <= 0 {
		return pick, &Playback3D{pos: pos, opts: opts}
	}
	return pick, pick.Clip.Effect.Play3DWith(pos, audio.Voice{Volume: vol, Rate: float32(pick.Pitch)}, opts, c.mergePlayOptions(pick.Clip.Effect, playOpts))
}

// Position returns the playback's current 3D world position.
func (p *Playback3D) Position() Vec3 {
	if p == nil {
		return Vec3{}
	}
	return p.pos
}

// SetPosition moves the playback to pos, recomputes its 3D spatialization against its mixer's
// Listener3D, and updates the underlying audio.Playback volume immediately if still playing.
func (p *Playback3D) SetPosition(pos Vec3) {
	if p == nil {
		return
	}
	p.pos = pos
	p.refresh()
}

// Options returns the playback's current per-source 3D spatialization options.
func (p *Playback3D) Options() Spatial3DOptions {
	if p == nil {
		return Spatial3DOptions{}
	}
	return p.opts
}

// SetOptions updates the playback's per-source 3D spatialization options (such as a rotating
// directional cone), recomputes spatialization, and updates the underlying audio.Playback volume.
func (p *Playback3D) SetOptions(opts Spatial3DOptions) {
	if p == nil {
		return
	}
	p.opts = opts
	p.refresh()
}

// Spatial returns the playback's latest computed 3D spatialization (Gain, Pan, and Distance).
func (p *Playback3D) Spatial() Spatial3D {
	if p == nil {
		return Spatial3D{}
	}
	return p.spatial
}

// Gain returns the playback's latest computed spatial gain in [0, 1].
func (p *Playback3D) Gain() float32 {
	if p == nil {
		return 0
	}
	return p.spatial.Gain
}

// Pan returns the playback's latest computed stereo pan in [-1, 1].
func (p *Playback3D) Pan() float32 {
	if p == nil {
		return 0
	}
	return p.spatial.Pan
}

// SetRate updates the playback's playback rate on the underlying audio.Playback when still playing.
func (p *Playback3D) SetRate(rate float32) {
	if p.Ended() {
		return
	}
	p.playback.SetRate(rate)
}

// Stop stops the playback immediately. Calling Stop on a nil or already-ended Playback3D is a no-op.
func (p *Playback3D) Stop() {
	if p.Ended() {
		return
	}
	p.playback.Stop()
	p.effect.m.onPlaybackEnded()
}

// Ended reports whether the playback has finished, was stopped, or never started.
func (p *Playback3D) Ended() bool {
	if p == nil || p.playback == nil || p.effect == nil || p.effect.m == nil {
		return true
	}
	return ended(p.effect.m.cx, p.playback)
}

// Playing reports whether the playback is currently active.
func (p *Playback3D) Playing() bool { return !p.Ended() }

func (p *Playback3D) refresh() {
	if p == nil || p.effect == nil || p.effect.m == nil {
		return
	}
	p.spatial = p.effect.m.listener3D.Spatialize(p.pos, p.opts)
	if p.playback == nil || ended(p.effect.m.cx, p.playback) {
		return
	}
	g := p.effect.m.gain(p.effect.Group)
	var vol float32
	switch {
	case p.fading != nil:
		vol = p.fading.effectiveVolume(g)
	case p.entry != nil:
		vol = p.entry.effectiveVolume(g)
	default:
		vol = p.baseVolume * p.spatial.Gain * g
	}
	p.playback.SetVolume(vol)
	setPlaybackPan(p.playback, p.effect.m.effectivePan(p.spatial.Pan))
}
