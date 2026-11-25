package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// actorsVersion is the actors file schema version this game reads and writes.
const actorsVersion = 1

// The clips every actor plays, which an actors file must define.
const (
	clipIdle   = "idle"
	clipRun    = "run"
	clipJump   = "jump"
	clipAttack = "attack"
	clipHurt   = "hurt"
	clipFall   = "fall"
)

// requiredClips lists the clips an actors file must define, in the order the state machine uses them.
var requiredClips = []string{clipIdle, clipRun, clipJump, clipAttack, clipHurt, clipFall}

// hitEvent is the clip event that lands a blow: the attack clip must fire it once.
const hitEvent = "hit"

// The paint slots a rig part is drawn with; each actor's Paint gives their colors.
const (
	paintBody = "body"
	paintTrim = "trim"
	paintSkin = "skin"
)

// Actors is the actors file, data/actors.yaml: the hero's and the enemies' tuning, the rig of boxes both are built from, and the animation clips the rig plays. Parse it with parseActors and write it with encodeActors, which gives back the same bytes for a file it wrote.
type Actors struct {
	// Version is the schema version; it must be 1.
	Version int `yaml:"version"`
	// Gravity is the downward acceleration of every actor in meters per second squared.
	Gravity float32 `yaml:"gravity"`
	// Hero tunes the player's character.
	Hero Fighter `yaml:"hero"`
	// Enemy tunes every enemy.
	Enemy Fighter `yaml:"enemy"`
	// Rig is the skeleton both kinds of actor share, parents before children.
	Rig []Joint `yaml:"rig"`
	// Clips are the animations the rig plays; every name in requiredClips must be there.
	Clips []Clip `yaml:"clips"`
}

// Fighter tunes one kind of actor: its body, how it moves and how it fights.
type Fighter struct {
	// Health is how many hits the actor takes before it falls, from 1 to 99.
	Health int `yaml:"health"`
	// Radius and Height size the actor's collision capsule in meters.
	Radius float32 `yaml:"radius"`
	Height float32 `yaml:"height"`
	// Speed is the running speed in meters per second.
	Speed float32 `yaml:"speed"`
	// TurnSpeed is how fast the actor turns to face where it runs, in degrees per second.
	TurnSpeed float32 `yaml:"turnSpeed"`
	// JumpSpeed is the upward speed of a jump in meters per second; 0 never jumps.
	JumpSpeed float32 `yaml:"jumpSpeed,omitempty"`
	// Sight is how close the hero must come before an enemy gives chase, in meters; the hero ignores it.
	Sight float32 `yaml:"sight,omitempty"`
	// Leash is how far an enemy follows the hero from where it started before it walks back, in meters; the hero ignores it.
	Leash float32 `yaml:"leash,omitempty"`
	// Reach is how far in front of the actor its blow lands, in meters.
	Reach float32 `yaml:"reach"`
	// Arc is the width of the cone a blow lands in, in degrees.
	Arc float32 `yaml:"arc"`
	// Damage is the health a blow takes.
	Damage int `yaml:"damage"`
	// AttackSpeed scales the attack clip: below 1 swings slower, so a blow can be seen coming.
	AttackSpeed float32 `yaml:"attackSpeed"`
	// Cooldown is the pause after a swing before the next, in seconds.
	Cooldown float32 `yaml:"cooldown,omitempty"`
	// Grace is how long the actor cannot be hit again after a hit, in seconds.
	Grace float32 `yaml:"grace"`
	// Knockback is the speed a hit pushes the actor away at, in meters per second.
	Knockback float32 `yaml:"knockback"`
	// Paint holds the colors of the rig's paint slots, as CSS colors.
	Paint Paint `yaml:"paint"`
}

// Paint holds the CSS colors of a rig's paint slots.
type Paint struct {
	// Body colors the torso and limbs.
	Body string `yaml:"body"`
	// Trim colors the belt, boots and blade.
	Trim string `yaml:"trim"`
	// Skin colors the head.
	Skin string `yaml:"skin"`
}

// Joint is one bone of the rig with the box drawn on it.
type Joint struct {
	// Name identifies the joint; clip tracks name it.
	Name string `yaml:"name"`
	// Parent names an earlier joint this one hangs from; empty is the actor's root, at its feet.
	Parent string `yaml:"parent,omitempty"`
	// At is the joint's pivot in its parent's space, in meters.
	At []float32 `yaml:"at,flow"`
	// Size is the box's width, height and depth in meters.
	Size []float32 `yaml:"size,flow"`
	// Offset moves the box's center away from the pivot, in the joint's space.
	Offset []float32 `yaml:"offset,omitempty,flow"`
	// Paint is the slot the box is colored with: body, trim or skin.
	Paint string `yaml:"paint"`
}

// Clip is one animation: rotations, and optionally shifts, of joints over time.
type Clip struct {
	// Name identifies the clip.
	Name string `yaml:"name"`
	// Length is the clip's duration in seconds.
	Length float32 `yaml:"length"`
	// Loop plays the clip again from the start when it ends.
	Loop bool `yaml:"loop,omitempty"`
	// Events fire as playback passes their times.
	Events []ClipEvent `yaml:"events,omitempty"`
	// Tracks animate joints, at most one track per joint.
	Tracks []Track `yaml:"tracks"`
}

// ClipEvent is a named moment in a clip, such as the hit of a swing.
type ClipEvent struct {
	// Name is what the game listens for.
	Name string `yaml:"name"`
	// At is the event's time in seconds, from 0 to the clip's length.
	At float32 `yaml:"at"`
}

// Track is the keys of one joint.
type Track struct {
	// Joint names the animated joint.
	Joint string `yaml:"joint"`
	// Keys are the joint's poses in ascending time.
	Keys []Key `yaml:"keys"`
}

// Key is a joint's pose at one time, between which playback interpolates.
type Key struct {
	// At is the key's time in seconds, from 0 to the clip's length.
	At float32 `yaml:"at"`
	// Turn rotates the joint about its X, Y and Z axes, in degrees, applied X first.
	Turn []float32 `yaml:"turn,flow"`
	// Shift moves the joint from its rest pivot, in meters; empty does not move it.
	Shift []float32 `yaml:"shift,omitempty,flow"`
}

// parseActors decodes and validates an actors file, refusing unknown fields and trailing documents.
func parseActors(data []byte) (*Actors, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("actors: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var a Actors
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("actors: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("actors: unexpected second YAML document")
		}
		return nil, fmt.Errorf("actors: %w", err)
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	return &a, nil
}

// encodeActors validates a and writes it as 2-space YAML with its fields in a fixed order, so parseActors reads back the same Actors and encoding that again gives the same bytes.
func encodeActors(a *Actors) ([]byte, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(a); err != nil {
		return nil, fmt.Errorf("actors: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("actors: %w", err)
	}
	return buf.Bytes(), nil
}

// The ceilings on the file's gravity, in meters per second squared, and on a clip's length, in seconds.
const (
	maxGravity    = 1000
	maxClipLength = 60
)

// validate reports the first way a breaks the actors schema.
func (a *Actors) validate() error {
	if a.Version != actorsVersion {
		return fmt.Errorf("actors: unsupported version %d (want %d)", a.Version, actorsVersion)
	}
	if !positive(a.Gravity) || a.Gravity > maxGravity {
		return fmt.Errorf("actors: gravity %v is not above 0 and at most %v", a.Gravity, maxGravity)
	}
	if err := a.Hero.validate("hero", false); err != nil {
		return err
	}
	if err := a.Enemy.validate("enemy", true); err != nil {
		return err
	}
	joints, err := validateRig(a.Rig)
	if err != nil {
		return err
	}
	clips := map[string]bool{}
	for i := range a.Clips {
		c := &a.Clips[i]
		if clips[c.Name] {
			return fmt.Errorf("actors: clip %q is defined twice", c.Name)
		}
		clips[c.Name] = true
		if err := c.validate(joints); err != nil {
			return err
		}
	}
	for _, name := range requiredClips {
		if !clips[name] {
			return fmt.Errorf("actors: clip %q is missing", name)
		}
	}
	attack := a.clip(clipAttack)
	hits := 0
	for _, e := range attack.Events {
		if e.Name == hitEvent {
			hits++
		}
	}
	if hits != 1 {
		return fmt.Errorf("actors: clip %q fires %q %d times, want once", clipAttack, hitEvent, hits)
	}
	return nil
}

// clip returns the clip named name, or nil.
func (a *Actors) clip(name string) *Clip {
	for i := range a.Clips {
		if a.Clips[i].Name == name {
			return &a.Clips[i]
		}
	}
	return nil
}

func (f *Fighter) validate(who string, enemy bool) error {
	if f.Health < 1 || f.Health > 99 {
		return fmt.Errorf("actors: %s: health %d is outside 1 to 99", who, f.Health)
	}
	// Each value has a ceiling well past any playable tuning, so the game's arithmetic on it (a swing's length divided by attackSpeed, seconds turned into a time.Duration) cannot overflow.
	for _, v := range []struct {
		name          string
		value, lo, hi float32
	}{
		{"radius", f.Radius, 0, 5}, {"height", f.Height, 0, 10}, {"speed", f.Speed, 0, 100}, {"turnSpeed", f.TurnSpeed, 0, 3600},
		{"reach", f.Reach, 0, 20}, {"arc", f.Arc, 0, 360}, {"attackSpeed", f.AttackSpeed, 0.1, 10}, {"grace", f.Grace, 0, 10}, {"knockback", f.Knockback, 0, 100},
	} {
		if !positive(v.value) || v.value < v.lo || v.value > v.hi {
			if v.lo == 0 {
				return fmt.Errorf("actors: %s: %s %v is not above 0 and at most %v", who, v.name, v.value, v.hi)
			}
			return fmt.Errorf("actors: %s: %s %v is not from %v to %v", who, v.name, v.value, v.lo, v.hi)
		}
	}
	if f.Height < 2*f.Radius {
		return fmt.Errorf("actors: %s: height %v is less than twice the radius %v", who, f.Height, f.Radius)
	}
	if f.Damage < 1 {
		return fmt.Errorf("actors: %s: damage %d is less than 1", who, f.Damage)
	}
	for _, v := range []struct {
		name      string
		value, hi float32
	}{{"jumpSpeed", f.JumpSpeed, 100}, {"sight", f.Sight, 1000}, {"leash", f.Leash, 1000}, {"cooldown", f.Cooldown, 60}} {
		if !finite(v.value) || v.value < 0 || v.value > v.hi {
			return fmt.Errorf("actors: %s: %s %v is outside 0 to %v", who, v.name, v.value, v.hi)
		}
	}
	if enemy && (f.Sight == 0 || f.Leash == 0) {
		return fmt.Errorf("actors: %s: sight and leash must be set", who)
	}
	if !enemy && f.JumpSpeed == 0 {
		return fmt.Errorf("actors: %s: jumpSpeed must be set", who)
	}
	for _, c := range []struct{ slot, css string }{{paintBody, f.Paint.Body}, {paintTrim, f.Paint.Trim}, {paintSkin, f.Paint.Skin}} {
		if _, err := core.ParseColor(c.css); err != nil {
			return fmt.Errorf("actors: %s: paint %s: %w", who, c.slot, err)
		}
	}
	return nil
}

// validateRig checks the rig and returns each joint's index by name.
func validateRig(rig []Joint) (map[string]int, error) {
	if len(rig) == 0 {
		return nil, errors.New("actors: the rig has no joints")
	}
	index := make(map[string]int, len(rig))
	for i, j := range rig {
		if j.Name == "" {
			return nil, fmt.Errorf("actors: rig joint %d has no name", i)
		}
		if _, dup := index[j.Name]; dup {
			return nil, fmt.Errorf("actors: rig joint %q is defined twice", j.Name)
		}
		if _, ok := index[j.Parent]; j.Parent != "" && !ok {
			return nil, fmt.Errorf("actors: rig joint %q: parent %q is not an earlier joint", j.Name, j.Parent)
		}
		if err := vec3("at", j.At, false); err != nil {
			return nil, fmt.Errorf("actors: rig joint %q: %w", j.Name, err)
		}
		if err := vec3("size", j.Size, false); err != nil {
			return nil, fmt.Errorf("actors: rig joint %q: %w", j.Name, err)
		}
		if !positive(j.Size[0]) || !positive(j.Size[1]) || !positive(j.Size[2]) {
			return nil, fmt.Errorf("actors: rig joint %q: size %v is not positive", j.Name, j.Size)
		}
		if err := vec3("offset", j.Offset, true); err != nil {
			return nil, fmt.Errorf("actors: rig joint %q: %w", j.Name, err)
		}
		if !slices.Contains([]string{paintBody, paintTrim, paintSkin}, j.Paint) {
			return nil, fmt.Errorf("actors: rig joint %q: paint %q is not body, trim or skin", j.Name, j.Paint)
		}
		index[j.Name] = i
	}
	return index, nil
}

func (c *Clip) validate(joints map[string]int) error {
	if c.Name == "" {
		return errors.New("actors: a clip has no name")
	}
	if !positive(c.Length) || c.Length > maxClipLength {
		return fmt.Errorf("actors: clip %q: length %v is not above 0 and at most %v seconds", c.Name, c.Length, maxClipLength)
	}
	inClip := func(t float32) bool { return finite(t) && t >= 0 && t <= c.Length }
	for _, e := range c.Events {
		if e.Name == "" {
			return fmt.Errorf("actors: clip %q: an event has no name", c.Name)
		}
		if !inClip(e.At) {
			return fmt.Errorf("actors: clip %q: event %q at %v is outside 0 to %v", c.Name, e.Name, e.At, c.Length)
		}
	}
	animated := map[string]bool{}
	for _, tr := range c.Tracks {
		if _, ok := joints[tr.Joint]; !ok {
			return fmt.Errorf("actors: clip %q: track joint %q is not in the rig", c.Name, tr.Joint)
		}
		if animated[tr.Joint] {
			return fmt.Errorf("actors: clip %q: joint %q has two tracks", c.Name, tr.Joint)
		}
		animated[tr.Joint] = true
		if len(tr.Keys) == 0 {
			return fmt.Errorf("actors: clip %q: track %q has no keys", c.Name, tr.Joint)
		}
		for i, k := range tr.Keys {
			if !inClip(k.At) {
				return fmt.Errorf("actors: clip %q: track %q: key at %v is outside 0 to %v", c.Name, tr.Joint, k.At, c.Length)
			}
			if i > 0 && k.At <= tr.Keys[i-1].At {
				return fmt.Errorf("actors: clip %q: track %q: key at %v is not after the key before it", c.Name, tr.Joint, k.At)
			}
			if err := vec3("turn", k.Turn, false); err != nil {
				return fmt.Errorf("actors: clip %q: track %q: %w", c.Name, tr.Joint, err)
			}
			if err := vec3("shift", k.Shift, true); err != nil {
				return fmt.Errorf("actors: clip %q: track %q: %w", c.Name, tr.Joint, err)
			}
		}
	}
	return nil
}

// vec3 checks that v holds three finite numbers, or is empty when optional.
func vec3(name string, v []float32, optional bool) error {
	if optional && len(v) == 0 {
		return nil
	}
	if len(v) != 3 {
		return fmt.Errorf("%s has %d numbers, want 3", name, len(v))
	}
	for _, x := range v {
		if !finite(x) {
			return fmt.Errorf("%s %v is not finite", name, v)
		}
	}
	return nil
}

func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

func positive(v float32) bool { return finite(v) && v > 0 }
