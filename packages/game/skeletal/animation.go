package skeletal

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type trackState struct {
	anim    *Animation
	elapsed time.Duration
	loop    bool
	done    bool
}

func (t *trackState) advance(dt time.Duration) {
	if t.anim == nil || t.done || dt <= 0 {
		return
	}
	d := t.anim.Duration
	if d <= 0 {
		t.elapsed = 0
		if !t.loop {
			t.done = true
		}
		return
	}
	t.elapsed += dt
	if t.elapsed >= d {
		if t.loop {
			t.elapsed %= d
		} else {
			t.elapsed = d
			t.done = true
		}
	}
}

// Play starts playing the named animation from time 0, cancelling any active
// crossfade, applies its initial pose and updates world transforms.
func (s *Skeleton) Play(anim string, loop bool) error {
	a, err := s.lookupAnim(anim)
	if err != nil {
		return err
	}
	s.current = trackState{anim: a, loop: loop}
	s.outgoing = trackState{}
	s.outgoingSnapshot = false
	s.blending = false
	s.blendElapsed = 0
	s.blendDuration = 0
	s.sampleTrack(s.current, s.localBones, s.slotAlpha, s.slotAnimFrame, s.slotAnimHasFrame)
	s.UpdateWorldTransforms()
	return nil
}

// Crossfade smoothly blends from the current pose into the named animation over
// duration, using shortest-angle rotation blending and linear translation,
// scale and alpha blending. If duration <= 0, Crossfade switches immediately
// like Play.
func (s *Skeleton) Crossfade(anim string, duration time.Duration, loop bool) error {
	a, err := s.lookupAnim(anim)
	if err != nil {
		return err
	}
	if duration <= 0 {
		return s.Play(anim, loop)
	}

	if s.blending {
		// Snapshot the current blended pose so interrupting a crossfade never pops.
		copy(s.fromBones, s.localBones)
		copy(s.fromAlpha, s.slotAlpha)
		copy(s.fromFrame, s.slotAnimFrame)
		copy(s.fromHasFrame, s.slotAnimHasFrame)
		s.outgoing = trackState{}
		s.outgoingSnapshot = true
	} else if s.current.anim != nil {
		s.outgoing = s.current
		s.outgoingSnapshot = false
	} else {
		// No animation playing yet: blend out of the current bind pose.
		copy(s.fromBones, s.localBones)
		copy(s.fromAlpha, s.slotAlpha)
		copy(s.fromFrame, s.slotAnimFrame)
		copy(s.fromHasFrame, s.slotAnimHasFrame)
		s.outgoing = trackState{}
		s.outgoingSnapshot = true
	}

	s.current = trackState{anim: a, loop: loop}
	s.blending = true
	s.blendElapsed = 0
	s.blendDuration = duration
	s.applyBlend(0)
	s.UpdateWorldTransforms()
	return nil
}

// Update advances the active animation (and any ongoing crossfade) by dt and
// updates every bone's world transform. Call it from a game.Loop's fixed-step
// Update callback for deterministic playback.
func (s *Skeleton) Update(dt time.Duration) {
	if s.data == nil {
		return
	}
	if s.current.anim == nil {
		s.UpdateWorldTransforms()
		return
	}
	if dt > 0 {
		s.current.advance(dt)
		if s.blending {
			if !s.outgoingSnapshot {
				s.outgoing.advance(dt)
			}
			s.blendElapsed += dt
			if s.blendElapsed >= s.blendDuration {
				s.blending = false
				s.outgoing = trackState{}
				s.outgoingSnapshot = false
				s.blendElapsed = 0
				s.blendDuration = 0
			}
		}
	}

	if s.blending {
		w := float32(s.blendElapsed) / float32(s.blendDuration)
		s.applyBlend(w)
	} else {
		s.sampleTrack(s.current, s.localBones, s.slotAlpha, s.slotAnimFrame, s.slotAnimHasFrame)
	}
	s.UpdateWorldTransforms()
}

// Animation returns the name of the active (or incoming) animation, or empty
// when no animation is playing.
func (s *Skeleton) Animation() string {
	if s.current.anim == nil {
		return ""
	}
	return s.current.anim.Name
}

// Done reports whether a non-looping animation has reached its end.
func (s *Skeleton) Done() bool {
	return s.current.anim != nil && s.current.done
}

// Blending reports whether a crossfade is currently in progress.
func (s *Skeleton) Blending() bool {
	return s.blending
}

func (s *Skeleton) lookupAnim(name string) (*Animation, error) {
	if s.data == nil {
		return nil, fmt.Errorf("skeletal: unknown animation %q", name)
	}
	a, ok := s.data.Animations[name]
	if !ok || a == nil {
		return nil, fmt.Errorf("skeletal: unknown animation %q", name)
	}
	return a, nil
}

func (s *Skeleton) applyBlend(w float32) {
	if !s.outgoingSnapshot {
		s.sampleTrack(s.outgoing, s.fromBones, s.fromAlpha, s.fromFrame, s.fromHasFrame)
	}
	s.sampleTrack(s.current, s.toBones, s.toAlpha, s.toFrame, s.toHasFrame)

	for i := range s.localBones {
		fb, tb := s.fromBones[i], s.toBones[i]
		s.localBones[i] = BoneTransform{
			X:        lerp(fb.X, tb.X, w),
			Y:        lerp(fb.Y, tb.Y, w),
			Rotation: lerpAngle(fb.Rotation, tb.Rotation, w),
			ScaleX:   lerp(fb.ScaleX, tb.ScaleX, w),
			ScaleY:   lerp(fb.ScaleY, tb.ScaleY, w),
		}
	}
	for i := range s.slotAlpha {
		s.slotAlpha[i] = lerp(s.fromAlpha[i], s.toAlpha[i], w)
		if w < 0.5 {
			s.slotAnimFrame[i] = s.fromFrame[i]
			s.slotAnimHasFrame[i] = s.fromHasFrame[i]
		} else {
			s.slotAnimFrame[i] = s.toFrame[i]
			s.slotAnimHasFrame[i] = s.toHasFrame[i]
		}
	}
}

func (s *Skeleton) sampleTrack(tr trackState, bones []BoneTransform, alpha []float32, frames []string, hasFrame []bool) {
	s.resetPoseToBind(bones, alpha, frames, hasFrame)
	if tr.anim == nil {
		return
	}
	t := tr.elapsed
	for _, bt := range tr.anim.boneTimelines {
		bi := bt.boneIdx
		if len(bt.tl.Translate) > 0 {
			dx, dy := sampleVec2(bt.tl.Translate, t)
			bones[bi].X += dx
			bones[bi].Y += dy
		}
		if len(bt.tl.Rotate) > 0 {
			bones[bi].Rotation += sampleAngle(bt.tl.Rotate, t)
		}
		if len(bt.tl.Scale) > 0 {
			sx, sy := sampleVec2(bt.tl.Scale, t)
			bones[bi].ScaleX *= sx
			bones[bi].ScaleY *= sy
		}
	}
	for _, st := range tr.anim.slotTimelines {
		si := st.slotIdx
		if len(st.tl.Alpha) > 0 {
			alpha[si] = min(max(sampleFloat(st.tl.Alpha, t), 0), 1)
		}
		if len(st.tl.Attachment) > 0 {
			if f, ok := sampleAttachment(st.tl.Attachment, t); ok {
				frames[si] = f
				hasFrame[si] = true
			}
		}
	}
}

func sampleVec2(keys []Vec2Keyframe, t time.Duration) (float32, float32) {
	n := len(keys)
	if t <= keys[0].Time {
		return keys[0].X, keys[0].Y
	}
	if t >= keys[n-1].Time {
		return keys[n-1].X, keys[n-1].Y
	}
	i := sort.Search(n, func(j int) bool { return keys[j].Time > t }) - 1
	k0, k1 := keys[i], keys[i+1]
	span := k1.Time - k0.Time
	if span <= 0 {
		return k1.X, k1.Y
	}
	u := float32(t-k0.Time) / float32(span)
	p := evalCurve(k0.Curve, k0.Bezier, u)
	return lerp(k0.X, k1.X, p), lerp(k0.Y, k1.Y, p)
}

func sampleFloat(keys []FloatKeyframe, t time.Duration) float32 {
	n := len(keys)
	if t <= keys[0].Time {
		return keys[0].Value
	}
	if t >= keys[n-1].Time {
		return keys[n-1].Value
	}
	i := sort.Search(n, func(j int) bool { return keys[j].Time > t }) - 1
	k0, k1 := keys[i], keys[i+1]
	span := k1.Time - k0.Time
	if span <= 0 {
		return k1.Value
	}
	u := float32(t-k0.Time) / float32(span)
	p := evalCurve(k0.Curve, k0.Bezier, u)
	return lerp(k0.Value, k1.Value, p)
}

func sampleAngle(keys []FloatKeyframe, t time.Duration) float32 {
	n := len(keys)
	if t <= keys[0].Time {
		return keys[0].Value
	}
	if t >= keys[n-1].Time {
		return keys[n-1].Value
	}
	i := sort.Search(n, func(j int) bool { return keys[j].Time > t }) - 1
	k0, k1 := keys[i], keys[i+1]
	span := k1.Time - k0.Time
	if span <= 0 {
		return k1.Value
	}
	u := float32(t-k0.Time) / float32(span)
	p := evalCurve(k0.Curve, k0.Bezier, u)
	return lerpAngle(k0.Value, k1.Value, p)
}

func sampleAttachment(keys []AttachmentKeyframe, t time.Duration) (string, bool) {
	if t < keys[0].Time {
		return "", false
	}
	i := sort.Search(len(keys), func(j int) bool { return keys[j].Time > t }) - 1
	return keys[i].Frame, true
}

func evalCurve(ct CurveType, bz [4]float32, u float32) float32 {
	switch ct {
	case CurveStepped:
		return 0
	case CurveBezier:
		return evalBezier(bz[0], bz[1], bz[2], bz[3], u)
	default:
		return u
	}
}

// evalBezier evaluates a CSS/Spine-style cubic Bezier curve with endpoints
// (0, 0) and (1, 1) and control points (cx1, cy1) and (cx2, cy2) at x in [0, 1].
func evalBezier(cx1, cy1, cx2, cy2, x float32) float32 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	ax := float64(3*cx1 - 3*cx2 + 1)
	bx := float64(3*cx2 - 6*cx1)
	cx := float64(3 * cx1)

	target := float64(x)
	lo, hi := 0.0, 1.0
	u := target
	for range 12 {
		xu := ((ax*u+bx)*u + cx) * u
		err := xu - target
		if math.Abs(err) < 1e-7 {
			break
		}
		if err < 0 {
			lo = u
		} else {
			hi = u
		}
		d := (3*ax*u+2*bx)*u + cx
		if math.Abs(d) > 1e-7 {
			next := u - err/d
			if next > lo && next < hi {
				u = next
				continue
			}
		}
		u = 0.5 * (lo + hi)
	}

	ay := float64(3*cy1 - 3*cy2 + 1)
	by := float64(3*cy2 - 6*cy1)
	cy := float64(3 * cy1)
	return float32(((ay*u+by)*u + cy) * u)
}

func lerp(a, b, t float32) float32 {
	return a + (b-a)*t
}

func shortestAngleDelta(from, to float32) float32 {
	d := math.Mod(float64(to-from), 2*math.Pi)
	if d > math.Pi {
		d -= 2 * math.Pi
	} else if d < -math.Pi {
		d += 2 * math.Pi
	}
	return float32(d)
}

func lerpAngle(from, to, t float32) float32 {
	if t == 0 {
		return from
	}
	if t == 1 {
		return to
	}
	return float32(math.Remainder(float64(from+shortestAngleDelta(from, to)*t), 2*math.Pi))
}
