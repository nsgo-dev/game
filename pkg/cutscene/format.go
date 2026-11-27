package cutscene

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

const (
	fixCutscene = "write the cutscene as version 1 (video) or version 2 (video or timeline) YAML"
	fixDuration = `write a Go duration string such as "1.5s" or "250ms"`
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
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("a duration is a string such as \"1.5s\", not %q", n.Value), Fix: fixDuration}
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: err.Error(), Fix: fixDuration, Err: err}
	}
	*d = Duration(v)
	return nil
}

// Parse reads a cutscene file and checks it as Validate does. It refuses unknown keys, more than one document and a version outside [MinVersion, CurrentVersion].
func Parse(data []byte) (Cutscene, error) {
	c, err := parseCutscene(data)
	if err != nil {
		return Cutscene{}, loaderr.Locate(err, "", data, fixCutscene)
	}
	return c, nil
}

func parseCutscene(data []byte) (Cutscene, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Cutscene{}, loaderr.Errorf("", fixCutscene, "cutscene: empty document")
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
			return Cutscene{}, loaderr.Errorf("", "keep one YAML document in the cutscene file; remove the trailing --- document", "cutscene: more than one YAML document")
		}
		return Cutscene{}, fmt.Errorf("cutscene: decode: %w", err)
	}
	if err := Validate(c); err != nil {
		return Cutscene{}, err
	}
	yamlzero.Normalize(&c)
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
			return &loaderr.Error{
				Line:    v.Line,
				Column:  v.Column,
				Field:   "version",
				Problem: fmt.Sprintf("cutscene: line %d: version is an integer, not %q", v.Line, v.Value),
				Fix:     fmt.Sprintf("write version as an integer (%d or %d)", MinVersion, CurrentVersion),
			}
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
	if err := enc.Encode(yamlzero.Clean(c)); err != nil {
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
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d or %d", MinVersion, CurrentVersion), "cutscene: unsupported version %d (want %d..%d)", c.Version, MinVersion, CurrentVersion)
	}
	if c.Version == 1 {
		if c.Kind != "" {
			return loaderr.Errorf("kind", fmt.Sprintf("set version to %d to use kind, or remove kind", CurrentVersion), "cutscene: version 1 does not support kind %q (use version %d)", c.Kind, CurrentVersion)
		}
		if c.Scene != "" || c.Length != 0 || len(c.Tracks) > 0 {
			field := "tracks"
			if c.Scene != "" {
				field = "scene"
			} else if c.Length != 0 {
				field = "length"
			}
			return loaderr.Errorf(field, fmt.Sprintf("set version to %d to use timeline fields, or remove scene, length and tracks", CurrentVersion), "cutscene: version 1 does not support timeline fields (use version %d)", CurrentVersion)
		}
	}
	return check(c)
}

// check checks what playback needs of c, for either a video or timeline cutscene.
func check(c Cutscene) error {
	if c.SkipAfter < 0 {
		return loaderr.Errorf("skipAfter", "set skipAfter to a non-negative duration such as 1.5s", "cutscene: negative skipAfter %v", time.Duration(c.SkipAfter))
	}
	switch c.Kind {
	case "", KindVideo, KindTimeline:
	default:
		return loaderr.Errorf("kind", fmt.Sprintf("set kind to %q or %q", KindVideo, KindTimeline), "cutscene: unknown kind %q (want %q or %q)", c.Kind, KindVideo, KindTimeline)
	}
	switch c.EffectiveKind() {
	case KindVideo:
		return checkVideo(c)
	case KindTimeline:
		return checkTimeline(c)
	default:
		return loaderr.Errorf("kind", fmt.Sprintf("set kind to %q or %q", KindVideo, KindTimeline), "cutscene: unknown kind %q", c.Kind)
	}
}

func checkVideo(c Cutscene) error {
	if strings.TrimSpace(c.Source) == "" {
		return loaderr.Errorf("source", "set source to a video path or asset URI", "cutscene: no source")
	}
	if c.Scene != "" || c.Length != 0 || len(c.Tracks) > 0 {
		field := "tracks"
		if c.Scene != "" {
			field = "scene"
		} else if c.Length != 0 {
			field = "length"
		}
		return loaderr.Errorf(field, "remove scene, length and tracks from a video cutscene, or change kind to timeline", "cutscene: video cutscene cannot set scene, length or tracks")
	}
	switch c.Fit {
	case "", FitContain, FitCover, FitStretch:
	default:
		return loaderr.Errorf("fit", fmt.Sprintf("set fit to %q, %q or %q", FitContain, FitCover, FitStretch), "cutscene: unknown fit %q (want %q, %q or %q)", c.Fit, FitContain, FitCover, FitStretch)
	}
	return nil
}

func checkTimeline(c Cutscene) error {
	if strings.TrimSpace(c.Source) != "" || c.Fit != "" || c.Transparent {
		field := "transparent"
		if strings.TrimSpace(c.Source) != "" {
			field = "source"
		} else if c.Fit != "" {
			field = "fit"
		}
		return loaderr.Errorf(field, "remove source, fit and transparent from a timeline cutscene", "cutscene: timeline cutscene cannot set video fields (source, fit or transparent)")
	}
	if c.Length <= 0 {
		return loaderr.Errorf("length", "set length to a positive duration such as 12s", "cutscene: timeline length must be positive, got %v", time.Duration(c.Length))
	}
	if c.Scene != "" && strings.TrimSpace(c.Scene) == "" {
		return loaderr.Errorf("scene", "set scene to a project-relative scene path or omit it", "cutscene: scene path cannot be blank")
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
	tPath := loaderr.Path("tracks", ti)
	if tr.ID != "" {
		if strings.TrimSpace(tr.ID) == "" {
			return loaderr.Errorf(loaderr.Join(tPath, "id"), "set a non-blank track id or omit it", "cutscene: track %d: id cannot be blank", ti)
		}
		if trackIDs[tr.ID] {
			return loaderr.Errorf(loaderr.Join(tPath, "id"), "give each track a unique id", "cutscene: track %d: duplicate track id %q", ti, tr.ID)
		}
		trackIDs[tr.ID] = true
	}
	switch tr.Kind {
	case TrackCamera, TrackActor, TrackDialogue, TrackSound, TrackMusic:
	default:
		return loaderr.Errorf(loaderr.Join(tPath, "kind"), "set kind to camera, actor, dialogue, sound or music", "cutscene: track %d: unknown kind %q (want camera, actor, dialogue, sound or music)", ti, tr.Kind)
	}
	if tr.Kind == TrackActor {
		if strings.TrimSpace(tr.Target) == "" {
			return loaderr.Errorf(loaderr.Join(tPath, "target"), "set target to the scene object id this actor track drives", "cutscene: track %d: actor track requires target", ti)
		}
		if actorTargets[tr.Target] {
			return loaderr.Errorf(loaderr.Join(tPath, "target"), "keep one actor track per target object id", "cutscene: track %d: duplicate actor track for target %q", ti, tr.Target)
		}
		actorTargets[tr.Target] = true
	} else if tr.Target != "" {
		return loaderr.Errorf(loaderr.Join(tPath, "target"), "remove target from non-actor tracks", "cutscene: track %d (%s): target is only valid on actor tracks", ti, tr.Kind)
	}
	if tr.Group != "" && tr.Kind != TrackSound && tr.Kind != TrackMusic {
		return loaderr.Errorf(loaderr.Join(tPath, "group"), "remove group from non-audio tracks", "cutscene: track %d (%s): group is only valid on sound and music tracks", ti, tr.Kind)
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
			cStartPath := loaderr.Join(tPath, "clips", ci, "start")
			if start < prevStart {
				return loaderr.Errorf(cStartPath, "order clips in non-decreasing start order", "cutscene: track %d clip %d: start %v is earlier than previous clip start %v", ti, ci, start, prevStart)
			}
			if tr.Kind != TrackSound && start < prevEnd {
				return loaderr.Errorf(cStartPath, "move start to at or after the previous clip's end, or shorten the previous clip", "cutscene: track %d clip %d: start %v overlaps previous clip ending at %v", ti, ci, start, prevEnd)
			}
		}
		prevStart, prevEnd = start, end
	}
	return nil
}

func checkClip(ti, ci int, kind TrackKind, cl *Clip, length time.Duration, clipIDs map[string]bool) error {
	cPath := loaderr.Path("tracks", ti, "clips", ci)
	prefix := fmt.Sprintf("cutscene: track %d clip %d", ti, ci)
	if cl.ID != "" {
		if strings.TrimSpace(cl.ID) == "" {
			return loaderr.Errorf(loaderr.Join(cPath, "id"), "set a non-blank clip id or omit it", "%s: id cannot be blank", prefix)
		}
		if clipIDs[cl.ID] {
			return loaderr.Errorf(loaderr.Join(cPath, "id"), "give each clip a unique id", "%s: duplicate clip id %q", prefix, cl.ID)
		}
		clipIDs[cl.ID] = true
	}
	start := time.Duration(cl.Start)
	dur := time.Duration(cl.Duration)
	if start < 0 {
		return loaderr.Errorf(loaderr.Join(cPath, "start"), "set start to a non-negative duration", "%s: negative start %v", prefix, start)
	}
	if dur < 0 {
		return loaderr.Errorf(loaderr.Join(cPath, "duration"), "set duration to a non-negative duration", "%s: negative duration %v", prefix, dur)
	}
	if kind != TrackSound && dur == 0 {
		return loaderr.Errorf(loaderr.Join(cPath, "duration"), "set duration to a positive duration", "%s (%s): duration must be positive", prefix, kind)
	}
	if start > length || dur > length-start {
		return loaderr.Errorf(loaderr.Join(cPath, "duration"), "keep start + duration within the cutscene length, or increase length", "%s: clip [%v, %v] extends past cutscene length %v", prefix, start, start+dur, length)
	}
	switch cl.Easing {
	case "", EaseLinear, EaseIn, EaseOut, EaseInOut, EaseHold:
	default:
		return loaderr.Errorf(loaderr.Join(cPath, "easing"), "set easing to linear, easeIn, easeOut, easeInOut or hold", "%s: unknown easing %q (want linear, easeIn, easeOut, easeInOut or hold)", prefix, cl.Easing)
	}
	if err := checkFinitePtr(cPath, prefix, "fromX", cl.FromX); err != nil {
		return err
	}
	if err := checkFinitePtr(cPath, prefix, "fromY", cl.FromY); err != nil {
		return err
	}
	if err := checkFinitePtr(cPath, prefix, "toX", cl.ToX); err != nil {
		return err
	}
	if err := checkFinitePtr(cPath, prefix, "toY", cl.ToY); err != nil {
		return err
	}
	if err := checkFinite(cPath, prefix, "offsetX", cl.OffsetX); err != nil {
		return err
	}
	if err := checkFinite(cPath, prefix, "offsetY", cl.OffsetY); err != nil {
		return err
	}
	if err := checkFinitePtr(cPath, prefix, "zoom", cl.Zoom); err != nil {
		return err
	}
	if err := checkFinitePtr(cPath, prefix, "speed", cl.Speed); err != nil {
		return err
	}
	if err := checkFinitePtr(cPath, prefix, "volume", cl.Volume); err != nil {
		return err
	}

	switch kind {
	case TrackCamera:
		return checkCameraClip(cPath, prefix, cl)
	case TrackActor:
		return checkActorClip(cPath, prefix, cl)
	case TrackDialogue:
		return checkDialogueClip(cPath, prefix, cl)
	case TrackSound:
		return checkSoundClip(cPath, prefix, cl)
	case TrackMusic:
		return checkMusicClip(cPath, prefix, cl)
	}
	return nil
}

func checkCameraClip(cPath, prefix string, cl *Clip) error {
	if cl.State != "" || cl.Speed != nil || cl.Line != "" || cl.Speaker != "" || cl.Source != "" || cl.Group != "" || cl.Volume != nil || cl.FadeIn != 0 || cl.FadeOut != 0 || cl.Loop {
		return loaderr.Errorf(cPath, "remove fields that do not belong on a camera clip", "%s (camera): uses fields belonging to another track kind", prefix)
	}
	if cl.Zoom != nil && *cl.Zoom <= 0 {
		return loaderr.Errorf(loaderr.Join(cPath, "zoom"), "set zoom to a positive number", "%s (camera): zoom must be positive, got %v", prefix, *cl.Zoom)
	}
	switch cl.Action {
	case ActionHold:
		if cl.FromX != nil || cl.FromY != nil {
			field := "fromX"
			if cl.FromX == nil {
				field = "fromY"
			}
			return loaderr.Errorf(loaderr.Join(cPath, field), "remove fromX and fromY from a hold clip, or change action to pan", "%s (camera hold): fromX and fromY are only valid on pan", prefix)
		}
		if strings.TrimSpace(cl.Target) == "" && cl.ToX == nil && cl.ToY == nil {
			return loaderr.Errorf(cPath, "set target or toX/toY on the camera hold clip", "%s (camera hold): requires target or toX/toY", prefix)
		}
	case ActionPan:
		if strings.TrimSpace(cl.Target) == "" && cl.ToX == nil && cl.ToY == nil && cl.Zoom == nil {
			return loaderr.Errorf(cPath, "set target, toX/toY or zoom on the camera pan clip", "%s (camera pan): requires target, toX/toY or zoom", prefix)
		}
	case ActionFollow:
		if cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil {
			return loaderr.Errorf(cPath, "remove fromX, fromY, toX and toY from a follow clip", "%s (camera follow): coordinate fields are not valid on follow", prefix)
		}
		if strings.TrimSpace(cl.Target) == "" {
			return loaderr.Errorf(loaderr.Join(cPath, "target"), "set target to the scene object id to follow", "%s (camera follow): requires target", prefix)
		}
	default:
		return loaderr.Errorf(loaderr.Join(cPath, "action"), "set action to hold, pan or follow", "%s (camera): unknown action %q (want hold, pan or follow)", prefix, cl.Action)
	}
	return nil
}

func checkActorClip(cPath, prefix string, cl *Clip) error {
	if cl.Target != "" || cl.OffsetX != 0 || cl.OffsetY != 0 || cl.Zoom != nil || cl.Line != "" || cl.Speaker != "" || cl.Source != "" || cl.Group != "" || cl.Volume != nil || cl.FadeIn != 0 || cl.FadeOut != 0 || cl.Loop {
		return loaderr.Errorf(cPath, "remove fields that do not belong on an actor clip", "%s (actor): uses fields belonging to another track kind", prefix)
	}
	if cl.Speed != nil && *cl.Speed <= 0 {
		return loaderr.Errorf(loaderr.Join(cPath, "speed"), "set speed to a positive number", "%s (actor): speed must be positive, got %v", prefix, *cl.Speed)
	}
	switch cl.Action {
	case ActionWalk:
		if cl.Speed != nil {
			return loaderr.Errorf(loaderr.Join(cPath, "speed"), "remove speed from a walk clip, or change action to patrol", "%s (actor walk): speed is only valid on patrol", prefix)
		}
		if cl.ToX == nil && cl.ToY == nil {
			return loaderr.Errorf(cPath, "set toX or toY on the actor walk clip", "%s (actor walk): requires toX or toY", prefix)
		}
	case ActionState:
		if cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil || cl.Speed != nil {
			return loaderr.Errorf(cPath, "remove motion fields (fromX, fromY, toX, toY, speed) from a state clip", "%s (actor state): motion fields are not valid on state", prefix)
		}
		if strings.TrimSpace(cl.State) == "" {
			return loaderr.Errorf(loaderr.Join(cPath, "state"), "set state to an animation state name", "%s (actor state): requires state", prefix)
		}
	case ActionDrop:
		if cl.Speed != nil {
			return loaderr.Errorf(loaderr.Join(cPath, "speed"), "remove speed from a drop clip, or change action to patrol", "%s (actor drop): speed is only valid on patrol", prefix)
		}
		if cl.ToY == nil {
			return loaderr.Errorf(loaderr.Join(cPath, "toY"), "set toY on the actor drop clip", "%s (actor drop): requires toY", prefix)
		}
	case ActionPatrol:
		if cl.ToX == nil && cl.ToY == nil {
			return loaderr.Errorf(cPath, "set toX or toY on the actor patrol clip", "%s (actor patrol): requires toX or toY", prefix)
		}
	default:
		return loaderr.Errorf(loaderr.Join(cPath, "action"), "set action to walk, state, drop or patrol", "%s (actor): unknown action %q (want walk, state, drop or patrol)", prefix, cl.Action)
	}
	return nil
}

func checkDialogueClip(cPath, prefix string, cl *Clip) error {
	if cl.Easing != "" || cl.Action != "" || cl.Target != "" || cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil || cl.OffsetX != 0 || cl.OffsetY != 0 || cl.Zoom != nil || cl.State != "" || cl.Speed != nil || cl.Source != "" || cl.Group != "" || cl.Volume != nil || cl.FadeIn != 0 || cl.FadeOut != 0 || cl.Loop {
		return loaderr.Errorf(cPath, "remove fields that do not belong on a dialogue clip", "%s (dialogue): uses fields belonging to another track kind", prefix)
	}
	if strings.TrimSpace(cl.Line) == "" {
		return loaderr.Errorf(loaderr.Join(cPath, "line"), "set line to a non-empty localization key", "%s (dialogue): requires line id", prefix)
	}
	return nil
}

func checkSoundClip(cPath, prefix string, cl *Clip) error {
	if cl.Easing != "" || cl.Action != "" || cl.Target != "" || cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil || cl.OffsetX != 0 || cl.OffsetY != 0 || cl.Zoom != nil || cl.State != "" || cl.Speed != nil || cl.Line != "" || cl.Speaker != "" || cl.FadeIn != 0 || cl.FadeOut != 0 || cl.Loop {
		return loaderr.Errorf(cPath, "remove fields that do not belong on a sound clip", "%s (sound): uses fields belonging to another track kind", prefix)
	}
	if strings.TrimSpace(cl.Source) == "" {
		return loaderr.Errorf(loaderr.Join(cPath, "source"), "set source to an audio file path or URI", "%s (sound): requires source", prefix)
	}
	if cl.Volume != nil && (*cl.Volume < 0 || *cl.Volume > 1) {
		return loaderr.Errorf(loaderr.Join(cPath, "volume"), "set volume to a number in [0, 1]", "%s (sound): volume %v must be in [0, 1]", prefix, *cl.Volume)
	}
	return nil
}

func checkMusicClip(cPath, prefix string, cl *Clip) error {
	if cl.Easing != "" || cl.Action != "" || cl.Target != "" || cl.FromX != nil || cl.FromY != nil || cl.ToX != nil || cl.ToY != nil || cl.OffsetX != 0 || cl.OffsetY != 0 || cl.Zoom != nil || cl.State != "" || cl.Speed != nil || cl.Line != "" || cl.Speaker != "" {
		return loaderr.Errorf(cPath, "remove fields that do not belong on a music clip", "%s (music): uses fields belonging to another track kind", prefix)
	}
	if strings.TrimSpace(cl.Source) == "" {
		return loaderr.Errorf(loaderr.Join(cPath, "source"), "set source to an audio file path or URI", "%s (music): requires source", prefix)
	}
	if cl.Volume != nil && (*cl.Volume < 0 || *cl.Volume > 1) {
		return loaderr.Errorf(loaderr.Join(cPath, "volume"), "set volume to a number in [0, 1]", "%s (music): volume %v must be in [0, 1]", prefix, *cl.Volume)
	}
	if cl.FadeIn < 0 {
		return loaderr.Errorf(loaderr.Join(cPath, "fadeIn"), "set fadeIn to a non-negative duration", "%s (music): negative fadeIn %v", prefix, time.Duration(cl.FadeIn))
	}
	if cl.FadeOut < 0 {
		return loaderr.Errorf(loaderr.Join(cPath, "fadeOut"), "set fadeOut to a non-negative duration", "%s (music): negative fadeOut %v", prefix, time.Duration(cl.FadeOut))
	}
	dur := time.Duration(cl.Duration)
	fadeIn := time.Duration(cl.FadeIn)
	fadeOut := time.Duration(cl.FadeOut)
	if fadeIn > dur || fadeOut > dur-fadeIn {
		return loaderr.Errorf(loaderr.Join(cPath, "fadeOut"), "shorten fadeIn and fadeOut so their sum does not exceed duration", "%s (music): fadeIn (%v) + fadeOut (%v) exceeds duration (%v)", prefix, fadeIn, fadeOut, dur)
	}
	return nil
}

func checkFinite(cPath, prefix, field string, v float32) error {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return loaderr.Errorf(loaderr.Join(cPath, field), fmt.Sprintf("set %s to a finite number", field), "%s: %s must be finite", prefix, field)
	}
	return nil
}

func checkFinitePtr(cPath, prefix, field string, p *float32) error {
	if p == nil {
		return nil
	}
	return checkFinite(cPath, prefix, field, *p)
}
