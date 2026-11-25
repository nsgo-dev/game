package animclip

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

// FromSpriteAnimation converts a sprite.Animation (such as one loaded from an Aseprite tag) into a validated animation clip with one discrete sprite.frame track matching its frames, exact durations and loop flag. Frame events, when present, become clip events at their frame's start time.
func FromSpriteAnimation(name string, anim sprite.Animation) (*resource.Animation, error) {
	if len(anim.Frames) == 0 {
		return nil, errors.New("animclip: sprite animation has no frames")
	}
	frames := make([]resource.FrameKey, len(anim.Frames))
	var events []resource.Event
	var elapsed time.Duration
	for i, f := range anim.Frames {
		if f.Frame.Name == "" {
			return nil, fmt.Errorf("animclip: sprite animation frame %d has an empty name", i)
		}
		if f.Duration <= 0 {
			return nil, fmt.Errorf("animclip: sprite animation frame %d (%q) duration %v must be positive", i, f.Frame.Name, f.Duration)
		}
		t := float32(elapsed.Seconds())
		frames[i] = resource.FrameKey{Time: t, Frame: f.Frame.Name}
		if f.Event != "" {
			events = append(events, resource.Event{Time: t, Name: f.Event})
		}
		elapsed += f.Duration
	}
	clip := &resource.Animation{
		Header: resource.NewHeader(resource.KindAnimation),
		Name:   name,
		Length: float32(elapsed.Seconds()),
		Loop:   anim.Loop,
		Tracks: []resource.Track{{
			Property: PropertySpriteFrame,
			Frames:   frames,
		}},
		Events: events,
	}
	if err := clip.Validate(); err != nil {
		return nil, fmt.Errorf("animclip: build clip %q: %w", name, err)
	}
	return clip, nil
}

// FromAsepriteTag creates an animation clip from the named Aseprite frame tag in atlas, matching the tag's frame sequence (direction and repeat count), exact frame durations and loop mode.
func FromAsepriteTag(atlas *sprite.Atlas, tag string) (*resource.Animation, error) {
	if atlas == nil {
		return nil, errors.New("animclip: nil atlas")
	}
	if tag == "" {
		return nil, errors.New("animclip: empty tag name")
	}
	anim, ok := atlas.Animations[tag]
	if !ok {
		if len(atlas.Animations) == 0 {
			return nil, fmt.Errorf("animclip: atlas has no Aseprite tags (looking for %q)", tag)
		}
		names := make([]string, 0, len(atlas.Animations))
		for k := range atlas.Animations {
			names = append(names, k)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("animclip: atlas has no tag %q (available: %s)", tag, strings.Join(names, ", "))
	}
	return FromSpriteAnimation(tag, anim)
}

// FromFrames creates an animation clip that steps through frames at a fixed frame rate fps (frames per second) on a discrete sprite.frame track.
func FromFrames(name string, frames []string, fps float64, loop bool) (*resource.Animation, error) {
	if len(frames) == 0 {
		return nil, errors.New("animclip: frame list must not be empty")
	}
	if math.IsNaN(fps) || math.IsInf(fps, 0) || fps <= 0 {
		return nil, fmt.Errorf("animclip: frame rate %v must be finite and positive", fps)
	}
	keys := make([]resource.FrameKey, len(frames))
	for i, frame := range frames {
		if frame == "" {
			return nil, fmt.Errorf("animclip: frame %d has an empty name", i)
		}
		keys[i] = resource.FrameKey{
			Time:  float32(float64(i) / fps),
			Frame: frame,
		}
	}
	clip := &resource.Animation{
		Header: resource.NewHeader(resource.KindAnimation),
		Name:   name,
		Length: float32(float64(len(frames)) / fps),
		Loop:   loop,
		Tracks: []resource.Track{{
			Property: PropertySpriteFrame,
			Frames:   keys,
		}},
	}
	if err := clip.Validate(); err != nil {
		return nil, fmt.Errorf("animclip: build clip %q: %w", name, err)
	}
	return clip, nil
}
