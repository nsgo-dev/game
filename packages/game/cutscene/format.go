package cutscene

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

const (
	// MinVersion is the oldest cutscene file format version this package reads and writes (video cutscenes only).
	MinVersion = 1
	// CurrentVersion is the newest cutscene file format version this package reads and writes. Version 2 adds timeline cutscenes alongside video cutscenes.
	CurrentVersion = 2

	// SchemaURL is where the JSON Schema of version 1 cutscene files is published.
	SchemaURL = "https://game.nsgo.dev/schemas/cutscene.v1.json"
	// SchemaComment is a YAML language server comment pointing at SchemaURL.
	SchemaComment = "# yaml-language-server: $schema=" + SchemaURL
	// SchemaURLV2 is where the JSON Schema of version 2 cutscene files is published.
	SchemaURLV2 = "https://game.nsgo.dev/schemas/cutscene.v2.json"
	// SchemaCommentV2 is the first line Encode writes for a version 2 cutscene file, pointing a YAML language server at SchemaURLV2.
	SchemaCommentV2 = "# yaml-language-server: $schema=" + SchemaURLV2
)

// SchemaCommentFor returns the YAML language server schema comment for version: SchemaComment for version 1 and SchemaCommentV2 for version 2.
func SchemaCommentFor(version int) string {
	if version >= 2 {
		return SchemaCommentV2
	}
	return SchemaComment
}

// Kind selects whether a cutscene plays a video or an in-engine timeline.
type Kind string

const (
	// KindVideo plays a pre-rendered video over the game view through core.Video.
	KindVideo Kind = "video"
	// KindTimeline runs camera, actor, dialogue, sound and music tracks against a scene on game.Loop's fixed steps.
	KindTimeline Kind = "timeline"
)

// Fit says how a video cutscene's picture fits the screen.
type Fit string

const (
	// FitContain shows the whole picture, with bars where its shape differs from the screen's. It is the default.
	FitContain Fit = "contain"
	// FitCover fills the screen, cropping what overflows.
	FitCover Fit = "cover"
	// FitStretch fills the screen, distorting the picture.
	FitStretch Fit = "stretch"
)

// resizeMode is f as core.Video takes it.
func (f Fit) resizeMode() core.ResizeMode {
	switch f {
	case FitCover:
		return core.Cover
	case FitStretch:
		return core.Stretch
	default:
		return core.Contain
	}
}

// TrackKind selects what a timeline track's clips drive.
type TrackKind string

const (
	// TrackCamera drives the scene camera (hold on an object or point, pan to, follow).
	TrackCamera TrackKind = "camera"
	// TrackActor drives a named scene object (walk, play an animation state, drop, patrol).
	TrackActor TrackKind = "actor"
	// TrackDialogue drives timed localized dialogue lines by l10n key.
	TrackDialogue TrackKind = "dialogue"
	// TrackSound triggers one-shot sound effects.
	TrackSound TrackKind = "sound"
	// TrackMusic plays background music tracks with optional fade-in and fade-out.
	TrackMusic TrackKind = "music"
)

// Action selects what a camera or actor clip does.
type Action string

const (
	// ActionHold holds the camera on a target object or fixed coordinates.
	ActionHold Action = "hold"
	// ActionPan pans the camera smoothly to a target object or fixed coordinates.
	ActionPan Action = "pan"
	// ActionFollow continuously tracks a target object with the camera.
	ActionFollow Action = "follow"
	// ActionWalk moves an actor between coordinates over the clip's duration.
	ActionWalk Action = "walk"
	// ActionState plays a named animation state on an actor while holding its position.
	ActionState Action = "state"
	// ActionDrop drops an actor vertically from one coordinate to another.
	ActionDrop Action = "drop"
	// ActionPatrol moves an actor back and forth between two coordinates.
	ActionPatrol Action = "patrol"
)

// Easing selects the interpolation curve of a timeline clip.
type Easing string

const (
	// EaseLinear interpolates at a constant rate. It is the default when omitted.
	EaseLinear Easing = "linear"
	// EaseIn starts slowly and accelerates using core.EaseIn.
	EaseIn Easing = "easeIn"
	// EaseOut starts quickly and decelerates using core.EaseOut.
	EaseOut Easing = "easeOut"
	// EaseInOut eases smoothly at both ends using core.EaseInOut.
	EaseInOut Easing = "easeInOut"
	// EaseHold holds the starting value until the end of the clip, then steps to the target value.
	EaseHold Easing = "hold"
)

// At evaluates e at normalized progress u in [0, 1].
func (e Easing) At(u float64) float64 {
	if u <= 0 {
		return 0
	}
	if u >= 1 {
		return 1
	}
	switch e {
	case EaseIn:
		return core.EaseIn.At(u)
	case EaseOut:
		return core.EaseOut.At(u)
	case EaseInOut:
		return core.EaseInOut.At(u)
	case EaseHold:
		return 0
	default:
		return u
	}
}

// Track is one lane on a timeline cutscene (camera, actor, dialogue, sound or music) holding timed clips.
type Track struct {
	// ID optionally identifies the track for tools and editors; non-empty track IDs must be unique within the cutscene.
	ID string `yaml:"id,omitempty"`
	// Name is an optional human-readable display label for the track.
	Name string `yaml:"name,omitempty"`
	// Kind selects what the track's clips drive: "camera", "actor", "dialogue", "sound" or "music".
	Kind TrackKind `yaml:"kind" schema:"required,enum=camera|actor|dialogue|sound|music"`
	// Target is the scene object ID an actor track drives; required for "actor" tracks and omitted on other track kinds.
	Target string `yaml:"target,omitempty"`
	// Group is the default mixer group for audio clips on a "sound" or "music" track (defaults to "sfx" for sound and "music" for music).
	Group string `yaml:"group,omitempty"`
	// Clips are the timed clips on this track, in non-decreasing start order.
	Clips []Clip `yaml:"clips,omitempty"`
}

// Clip is one timed action or cue on a cutscene timeline track.
type Clip struct {
	// ID optionally identifies the clip for tools and editors; non-empty clip IDs must be unique within the cutscene.
	ID string `yaml:"id,omitempty"`
	// Label is an optional human-readable label for the clip.
	Label string `yaml:"label,omitempty"`
	// Start is when the clip begins on the timeline, as a Go duration string such as "1.6s".
	Start Duration `yaml:"start" schema:"required"`
	// Duration is how long the clip lasts, as a Go duration string such as "2.4s".
	Duration Duration `yaml:"duration" schema:"required"`
	// Easing is the interpolation curve for motion and camera pans: "linear" (default), "easeIn", "easeOut", "easeInOut" or "hold".
	Easing Easing `yaml:"easing,omitempty" schema:"enum=linear|easeIn|easeOut|easeInOut|hold"`
	// Action selects the operation on a camera track ("hold", "pan", "follow") or actor track ("walk", "state", "drop", "patrol").
	Action Action `yaml:"action,omitempty" schema:"enum=hold|pan|follow|walk|state|drop|patrol"`
	// Target is the scene object ID a camera clip holds on, pans to or follows.
	Target string `yaml:"target,omitempty"`
	// FromX is the optional starting X coordinate for a camera pan or an actor walk, drop or patrol clip.
	FromX *float32 `yaml:"fromX,omitempty"`
	// FromY is the optional starting Y coordinate for a camera pan or an actor walk, drop or patrol clip.
	FromY *float32 `yaml:"fromY,omitempty"`
	// ToX is the destination X coordinate for a camera hold/pan or an actor walk, drop or patrol clip.
	ToX *float32 `yaml:"toX,omitempty"`
	// ToY is the destination Y coordinate for a camera hold/pan or an actor walk, drop or patrol clip.
	ToY *float32 `yaml:"toY,omitempty"`
	// OffsetX is an optional horizontal camera offset applied when holding on, panning to or following a target object.
	OffsetX float32 `yaml:"offsetX,omitempty"`
	// OffsetY is an optional vertical camera offset applied when holding on, panning to or following a target object.
	OffsetY float32 `yaml:"offsetY,omitempty"`
	// Zoom is the optional target camera zoom factor (positive) on a camera clip.
	Zoom *float32 `yaml:"zoom,omitempty" schema:"exclusiveMinimum=0"`
	// State is the animation state name played by an actor clip (required for "state" clips, optional for "walk", "drop" and "patrol").
	State string `yaml:"state,omitempty"`
	// Speed is the optional travel speed in world units per second for an actor "patrol" clip; when omitted, patrol completes one full round-trip over Duration.
	Speed *float32 `yaml:"speed,omitempty" schema:"exclusiveMinimum=0"`
	// Line is the localization key (for example "intro.knight.01") shown by a dialogue clip; text lives in the l10n tables, not in the cutscene file.
	Line string `yaml:"line,omitempty"`
	// Speaker optionally names who speaks a dialogue clip.
	Speaker string `yaml:"speaker,omitempty"`
	// Source is the audio path or URI for a sound or music clip (for example "audio/door_slam.wav").
	Source string `yaml:"source,omitempty"`
	// Group overrides the track's mixer group for a sound or music clip.
	Group string `yaml:"group,omitempty"`
	// Volume is the optional volume multiplier in [0, 1] for a sound or music clip; defaults to 1 when omitted.
	Volume *float32 `yaml:"volume,omitempty" schema:"minimum=0,maximum=1"`
	// FadeIn is the fade-in duration for a music clip, as a Go duration string such as "2s".
	FadeIn Duration `yaml:"fadeIn,omitempty"`
	// FadeOut is the fade-out duration for a music clip, as a Go duration string such as "1s".
	FadeOut Duration `yaml:"fadeOut,omitempty"`
	// Loop repeats a music clip until its duration ends.
	Loop bool `yaml:"loop,omitempty"`
}

// Cutscene is one video or timeline cutscene, and the document a cutscene file holds.
//
// Version 1 holds a video cutscene:
//
//	version: 1
//	source: asset:cutscenes/intro.mp4
//	group: cutscenes
//	skippable: true
//	skipAfter: 1.5s
//	fit: cover
//
// Version 2 holds either a video cutscene or an in-engine timeline cutscene with camera, actor, dialogue, sound and music tracks:
//
//	# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
//	version: 2
//	kind: timeline
//	scene: scenes/level_01.scene.yaml
//	length: 12s
//	skippable: true
//	tracks:
//	  - kind: camera
//	    clips:
//	      - start: 0s
//	        duration: 1.6s
//	        easing: hold
//	        action: hold
//	        target: door
//
// Parse reads such a file and Encode writes one, keys in the order of the fields below and zero values left out, so a file Encode wrote reads back and writes again unchanged.
type Cutscene struct {
	// Version is the file format's version, from MinVersion (1) to CurrentVersion (2), which Parse and Encode check. Player.Play and NewTimelinePlayer ignore it, so a cutscene made in code may leave it 0.
	Version int `yaml:"version" schema:"required"`
	// Kind selects whether a version 2 cutscene plays a video ("video") or an in-engine timeline ("timeline"). Version 1 files leave it empty.
	Kind Kind `yaml:"kind,omitempty" schema:"enum=video|timeline"`
	// Source is the video for a video cutscene: an "asset:" path in the app's resources, a file path or a URL, as core.Video takes. Omitted on timeline cutscenes.
	Source string `yaml:"source,omitempty"`
	// Scene optionally names the scene file a timeline cutscene plays against, as a slash-separated path from the project root (for example "scenes/level_01.scene.yaml"). Used by game check and refs to validate object IDs. Version 2 timeline cutscenes only.
	Scene string `yaml:"scene,omitempty"`
	// Length is the total duration of a timeline cutscene, written as a Go duration string such as "12s". Version 2 timeline cutscenes only.
	Length Duration `yaml:"length,omitempty"`
	// Group is the mixer group whose volume, times the master volume, the cutscene's audio plays at; empty for the master volume alone (video) or per-track defaults (timeline).
	Group string `yaml:"group,omitempty"`
	// Skippable lets the player skip the cutscene by a press, a skip key or back.
	Skippable bool `yaml:"skippable,omitempty"`
	// SkipAfter is how far into the cutscene a skippable cutscene must play before it can be skipped, so a press meant for the game before it does not skip it at once; 0 for from the start.
	SkipAfter Duration `yaml:"skipAfter,omitempty"`
	// Fit is how a video cutscene's picture fits the screen, FitContain when empty. Video cutscenes only.
	Fit Fit `yaml:"fit,omitempty" schema:"enum=contain|cover|stretch"`
	// Transparent leaves the screen behind a video cutscene clear rather than black, so the game shows in the bars and through a video with an alpha channel, where the platform keeps it (core.Video). Video cutscenes only.
	Transparent bool `yaml:"transparent,omitempty"`
	// Tracks are the ordered timeline tracks (camera, actor, dialogue, sound and music) of a timeline cutscene. Version 2 timeline cutscenes only.
	Tracks []Track `yaml:"tracks,omitempty"`
}

// EffectiveKind reports whether c is a video cutscene or a timeline cutscene.
func (c Cutscene) EffectiveKind() Kind {
	if c.Kind != "" {
		return c.Kind
	}
	if strings.TrimSpace(c.Source) == "" && (c.Length > 0 || len(c.Tracks) > 0 || strings.TrimSpace(c.Scene) != "") {
		return KindTimeline
	}
	return KindVideo
}

// Duration is a time.Duration written in a cutscene file as Go writes durations, such as "1.5s" or "250ms".
type Duration time.Duration

// MarshalYAML writes d as time.Duration.String does.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// UnmarshalYAML reads a duration time.ParseDuration takes, refusing any other value.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return fmt.Errorf("line %d: a duration is a string such as \"1.5s\", not %q", n.Line, n.Value)
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*d = Duration(v)
	return nil
}

// Parse reads a cutscene file and checks it as Validate does. It refuses unknown keys, more than one document and a version outside [MinVersion, CurrentVersion].
func Parse(data []byte) (Cutscene, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Cutscene{}, errors.New("cutscene: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Cutscene
	if err := dec.Decode(&c); err != nil {
		return Cutscene{}, fmt.Errorf("cutscene: decode: %w", err)
	}
	if err := checkVersionTag(data); err != nil {
		return Cutscene{}, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Cutscene{}, errors.New("cutscene: more than one YAML document")
		}
		return Cutscene{}, fmt.Errorf("cutscene: decode: %w", err)
	}
	if err := Validate(c); err != nil {
		return Cutscene{}, err
	}
	return c, nil
}

// checkVersionTag refuses a version that is not written as an integer, which the decoder would otherwise truncate into one, reading "1.5" as 1. data has decoded into a Cutscene already, so it is one mapping document.
func checkVersionTag(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("cutscene: decode: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k, v := m.Content[i], m.Content[i+1]; k.Value == "version" && (v.Kind != yaml.ScalarNode || v.ShortTag() != "!!int") {
			return fmt.Errorf("cutscene: line %d: version is an integer, not %q", v.Line, v.Value)
		}
	}
	return nil
}

// Encode checks c as Validate does and writes it as a cutscene file, with 2-space indents and its keys in a fixed order. Version 2 files begin with SchemaCommentV2; version 1 files are written unchanged without a header comment.
func Encode(c Cutscene) ([]byte, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if c.Version >= 2 {
		buf.WriteString(SchemaCommentV2 + "\n")
	}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("cutscene: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("cutscene: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks c as a cutscene file: its version is in [MinVersion, CurrentVersion], version 1 uses only video fields, and the cutscene is valid for playback.
func Validate(c Cutscene) error {
	if c.Version < MinVersion || c.Version > CurrentVersion {
		return fmt.Errorf("cutscene: unsupported version %d (want %d..%d)", c.Version, MinVersion, CurrentVersion)
	}
	if c.Version == 1 {
		if c.Kind != "" {
			return fmt.Errorf("cutscene: version 1 does not support kind %q (use version %d)", c.Kind, CurrentVersion)
		}
		if c.Scene != "" || c.Length != 0 || len(c.Tracks) > 0 {
			return fmt.Errorf("cutscene: version 1 does not support timeline fields (use version %d)", CurrentVersion)
		}
	}
	return check(c)
}

// check checks what playback needs of c, for either a video or timeline cutscene.
func check(c Cutscene) error {
	if c.SkipAfter < 0 {
		return fmt.Errorf("cutscene: negative skipAfter %v", time.Duration(c.SkipAfter))
	}
	switch c.Kind {
	case "", KindVideo, KindTimeline:
	default:
		return fmt.Errorf("cutscene: unknown kind %q (want %q or %q)", c.Kind, KindVideo, KindTimeline)
	}
	switch c.EffectiveKind() {
	case KindVideo:
		return checkVideo(c)
	case KindTimeline:
		return checkTimeline(c)
	default:
		return fmt.Errorf("cutscene: unknown kind %q", c.Kind)
	}
}

func checkVideo(c Cutscene) error {
	if strings.TrimSpace(c.Source) == "" {
		return errors.New("cutscene: no source")
	}
	if c.Scene != "" || c.Length != 0 || len(c.Tracks) > 0 {
		return errors.New("cutscene: video cutscene cannot set scene, length or tracks")
	}
	switch c.Fit {
	case "", FitContain, FitCover, FitStretch:
	default:
		return fmt.Errorf("cutscene: unknown fit %q (want %q, %q or %q)", c.Fit, FitContain, FitCover, FitStretch)
	}
	return nil
}

func checkTimeline(c Cutscene) error {
	if strings.TrimSpace(c.Source) != "" || c.Fit != "" || c.Transparent {
		return errors.New("cutscene: timeline cutscene cannot set video fields (source, fit or transparent)")
	}
	if c.Length <= 0 {
		return fmt.Errorf("cutscene: timeline length must be positive, got %v", time.Duration(c.Length))
	}
	if c.Scene != "" && strings.TrimSpace(c.Scene) == "" {
		return errors.New("cutscene: scene path cannot be blank")
	}
	trackIDs := make(map[string]bool)
	clipIDs := make(map[string]bool)
	actorTargets := make(map[string]bool)
	for ti := range c.Tracks {
		tr := &c.Tracks[ti]
		if err := checkTrack(ti, tr, time.Duration(c.Length), trackIDs, clipIDs, actorTargets); err != nil {
			return err
		}
	}
	return nil
}

func checkTrack(ti int, tr *Track, length time.Duration, trackIDs, clipIDs, actorTargets map[string]bool) error {
	if tr.ID != "" {
		if strings.TrimSpace(tr.ID) == "" {
			return fmt.Errorf("cutscene: track %d: id cannot be blank", ti)
		}
		if trackIDs[tr.ID] {
			return fmt.Errorf("cutscene: track %d: duplicate track id %q", ti, tr.ID)
		}
		trackIDs[tr.ID] = true
	}
	switch tr.Kind {
	case TrackCamera, TrackActor, TrackDialogue, TrackSound, TrackMusic:
	default:
		return fmt.Errorf("cutscene: track %d: unknown kind %q (want camera, actor, dialogue, sound or music)", ti, tr.Kind)
	}
	if tr.Kind == TrackActor {
		if strings.TrimSpace(tr.Target) == "" {
			return fmt.Errorf("cutscene: track %d: actor track requires target", ti)
		}
		if actorTargets[tr.Target] {
			return fmt.Errorf("cutscene: track %d: duplicate actor track for target %q", ti, tr.Target)
		}
		actorTargets[tr.Target] = true
	} else if tr.Target != "" {
		return fmt.Errorf("cutscene: track %d (%s): target is only valid on actor tracks", ti, tr.Kind)
	}
	if tr.Group != "" && tr.Kind != TrackSound && tr.Kind != TrackMusic {
		return fmt.Errorf("cutscene: track %d (%s): group is only valid on sound and music tracks", ti, tr.Kind)
	}

	var prevStart, prevEnd time.Duration
	for ci := range tr.Clips {
		cl := &tr.Clips[ci]
		if err := checkClip(ti, ci, tr.Kind, cl, length, clipIDs); err != nil {
			return err
		}
		start := time.Duration(cl.Start)
		end := start + time.Duration(cl.Duration)
		if ci > 0 {
			if start < prevStart {
				return fmt.Errorf("cutscene: track %d clip %d: start %v is earlier than previous clip start %v", ti, ci, start, prevStart)
			}
			if tr.Kind != TrackSound && start < prevEnd {
				return fmt.Errorf("cutscene: track %d clip %d: start %v overlaps previous clip ending at %v", ti, ci, start, prevEnd)
			}
		}
		prevStart, prevEnd = start, end
	}
	return nil
}

func checkClip(ti, ci int, kind TrackKind, cl *Clip, length time.Duration, clipIDs map[string]bool) error {
	prefix := fmt.Sprintf("cutscene: track %d clip %d", ti, ci)
	if cl.ID != "" {
		if strings.TrimSpace(cl.ID) == "" {
			return fmt.Errorf("%s: id cannot be blank", prefix)
		}
		if clipIDs[cl.ID] {
			return fmt.Errorf("%s: duplicate clip id %q", prefix, cl.ID)
		}
		clipIDs[cl.ID] = true
	}
	start := time.Duration(cl.Start)
	dur := time.Duration(cl.Duration)
	if start < 0 {
		return fmt.Errorf("%s: negative start %v", prefix, start)
	}
	if dur < 0 {
		return fmt.Errorf("%s: negative duration %v", prefix, dur)
	}
	if kind != TrackSound && dur == 0 {
		return fmt.Errorf("%s (%s): duration must be positive", prefix, kind)
	}
	if start > length || dur > length-start {
		return fmt.Errorf("%s: clip [%v, %v] extends past cutscene length %v", prefix, start, start+dur, length)
	}
	switch cl.Easing {
	case "", EaseLinear, EaseIn, EaseOut, EaseInOut, EaseHold:
	default:
		return fmt.Errorf("%s: unknown easing %q (want linear, easeIn, easeOut, easeInOut or hold)", prefix, cl.Easing)
	}
	if err := checkFinitePtr(prefix, "fromX", cl.FromX); err != nil {
		return err
	}
	if err := checkFinitePtr(prefix, "fromY", cl.FromY); err != nil {
		return err
	}
	if err := checkFinitePtr(prefix, "toX", cl.ToX); err != nil {
		return err
	}
	if err := checkFinitePtr(prefix, "toY", cl.ToY); err != nil {
		return err
	}
	if err := checkFinite(prefix, "offsetX", cl.OffsetX); err != nil {
		return err
	}
	if err := checkFinite(prefix, "offsetY", cl.OffsetY); err != nil {
		return err
	}
	if err := checkFinitePtr(prefix, "zoom", cl.Zoom); err != nil {
		return err
	}
	if err := checkFinitePtr(prefix, "speed", cl.Speed); err != nil {
		return err
	}
	if err := checkFinitePtr(prefix, "volume", cl.Volume); err != nil {
		return err
	}

	switch kind {
	case TrackCamera:
		return checkCameraClip(prefix, cl)
	case TrackActor:
		return checkActorClip(prefix, cl)
	case TrackDialogue:
		return checkDialogueClip(prefix, cl)
	case TrackSound:
		return checkSoundClip(prefix, cl)
	case TrackMusic:
		return checkMusicClip(prefix, cl)
	}
	return nil
}

func checkCameraClip(prefix string, cl *Clip) error {
	if cl.State != "" || cl.Speed != nil || cl.Line != "" || cl.Speaker != "" || cl.Source != "" || cl.Group != "" || cl.Volume != nil || cl.FadeIn != 0 || cl.FadeOut != 0 || cl.Loop {
		return fmt.Errorf("%s (camera): uses fields belonging to another track kind", prefix)
	}
	if cl.Zoom != nil && *cl.Zoom <= 0 {
		return fmt.Errorf("%s (camera): zoom must be positive, got %v", prefix, *cl.Zoom)
	}
	switch cl.Action {
	case ActionHold:
		if cl.FromX != nil || cl.FromY != nil {
			return fmt.Errorf("%s (camera hold): fromX and fromY are only valid on pan", prefix)
		}
		if strings.TrimSpace(cl.Target) == "" && cl.ToX == nil && cl.ToY == nil {
			return fmt.Errorf("%s (camera hold): requires target or toX/toY", prefix)
		}
	case ActionPan:
		if strings.TrimSpace(cl.Target) == "" && cl.ToX == nil && cl.ToY == nil && cl.Zoom == nil {
			return fmt.Errorf("%s (camera pan): requires target, toX/toY or zoom", prefix)
		}
	case ActionFollow:
		if cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil {
			return fmt.Errorf("%s (camera follow): coordinate fields are not valid on follow", prefix)
		}
		if strings.TrimSpace(cl.Target) == "" {
			return fmt.Errorf("%s (camera follow): requires target", prefix)
		}
	default:
		return fmt.Errorf("%s (camera): unknown action %q (want hold, pan or follow)", prefix, cl.Action)
	}
	return nil
}

func checkActorClip(prefix string, cl *Clip) error {
	if cl.Target != "" || cl.OffsetX != 0 || cl.OffsetY != 0 || cl.Zoom != nil || cl.Line != "" || cl.Speaker != "" || cl.Source != "" || cl.Group != "" || cl.Volume != nil || cl.FadeIn != 0 || cl.FadeOut != 0 || cl.Loop {
		return fmt.Errorf("%s (actor): uses fields belonging to another track kind", prefix)
	}
	if cl.Speed != nil && *cl.Speed <= 0 {
		return fmt.Errorf("%s (actor): speed must be positive, got %v", prefix, *cl.Speed)
	}
	switch cl.Action {
	case ActionWalk:
		if cl.Speed != nil {
			return fmt.Errorf("%s (actor walk): speed is only valid on patrol", prefix)
		}
		if cl.ToX == nil && cl.ToY == nil {
			return fmt.Errorf("%s (actor walk): requires toX or toY", prefix)
		}
	case ActionState:
		if cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil || cl.Speed != nil {
			return fmt.Errorf("%s (actor state): motion fields are not valid on state", prefix)
		}
		if strings.TrimSpace(cl.State) == "" {
			return fmt.Errorf("%s (actor state): requires state", prefix)
		}
	case ActionDrop:
		if cl.Speed != nil {
			return fmt.Errorf("%s (actor drop): speed is only valid on patrol", prefix)
		}
		if cl.ToY == nil {
			return fmt.Errorf("%s (actor drop): requires toY", prefix)
		}
	case ActionPatrol:
		if cl.ToX == nil && cl.ToY == nil {
			return fmt.Errorf("%s (actor patrol): requires toX or toY", prefix)
		}
	default:
		return fmt.Errorf("%s (actor): unknown action %q (want walk, state, drop or patrol)", prefix, cl.Action)
	}
	return nil
}

func checkDialogueClip(prefix string, cl *Clip) error {
	if cl.Easing != "" || cl.Action != "" || cl.Target != "" || cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil || cl.OffsetX != 0 || cl.OffsetY != 0 || cl.Zoom != nil || cl.State != "" || cl.Speed != nil || cl.Source != "" || cl.Group != "" || cl.Volume != nil || cl.FadeIn != 0 || cl.FadeOut != 0 || cl.Loop {
		return fmt.Errorf("%s (dialogue): uses fields belonging to another track kind", prefix)
	}
	if strings.TrimSpace(cl.Line) == "" {
		return fmt.Errorf("%s (dialogue): requires line id", prefix)
	}
	return nil
}

func checkSoundClip(prefix string, cl *Clip) error {
	if cl.Easing != "" || cl.Action != "" || cl.Target != "" || cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil || cl.OffsetX != 0 || cl.OffsetY != 0 || cl.Zoom != nil || cl.State != "" || cl.Speed != nil || cl.Line != "" || cl.Speaker != "" || cl.FadeIn != 0 || cl.FadeOut != 0 || cl.Loop {
		return fmt.Errorf("%s (sound): uses fields belonging to another track kind", prefix)
	}
	if strings.TrimSpace(cl.Source) == "" {
		return fmt.Errorf("%s (sound): requires source", prefix)
	}
	if cl.Volume != nil && (*cl.Volume < 0 || *cl.Volume > 1) {
		return fmt.Errorf("%s (sound): volume %v must be in [0, 1]", prefix, *cl.Volume)
	}
	return nil
}

func checkMusicClip(prefix string, cl *Clip) error {
	if cl.Easing != "" || cl.Action != "" || cl.Target != "" || cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil || cl.OffsetX != 0 || cl.OffsetY != 0 || cl.Zoom != nil || cl.State != "" || cl.Speed != nil || cl.Line != "" || cl.Speaker != "" {
		return fmt.Errorf("%s (music): uses fields belonging to another track kind", prefix)
	}
	if strings.TrimSpace(cl.Source) == "" {
		return fmt.Errorf("%s (music): requires source", prefix)
	}
	if cl.Volume != nil && (*cl.Volume < 0 || *cl.Volume > 1) {
		return fmt.Errorf("%s (music): volume %v must be in [0, 1]", prefix, *cl.Volume)
	}
	if cl.FadeIn < 0 {
		return fmt.Errorf("%s (music): negative fadeIn %v", prefix, time.Duration(cl.FadeIn))
	}
	if cl.FadeOut < 0 {
		return fmt.Errorf("%s (music): negative fadeOut %v", prefix, time.Duration(cl.FadeOut))
	}
	dur := time.Duration(cl.Duration)
	fadeIn := time.Duration(cl.FadeIn)
	fadeOut := time.Duration(cl.FadeOut)
	if fadeIn > dur || fadeOut > dur-fadeIn {
		return fmt.Errorf("%s (music): fadeIn (%v) + fadeOut (%v) exceeds duration (%v)", prefix, fadeIn, fadeOut, dur)
	}
	return nil
}

func checkFinite(prefix, field string, v float32) error {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return fmt.Errorf("%s: %s must be finite", prefix, field)
	}
	return nil
}

func checkFinitePtr(prefix, field string, p *float32) error {
	if p == nil {
		return nil
	}
	return checkFinite(prefix, field, *p)
}
