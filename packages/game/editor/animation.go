package editor

import (
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// ErrNoClip is returned by animation edits when no clip is open.
var ErrNoClip = errors.New("editor: no animation clip is open")

// clipMeta is what a clip is besides its contents: the file it came from and the object it plays on.
type clipMeta struct {
	path   string
	target string
}

// Clip returns a copy of the animation clip open in the animation editor, or nil.
func (e *Editor) Clip() *resource.Animation {
	if e == nil || e.clip == nil {
		return nil
	}
	return cloneClip(e.clip)
}

// ClipPath returns the file the open clip was opened from or last saved to, or "".
func (e *Editor) ClipPath() string { return e.clipMeta.path }

// ClipTarget returns the ID of the object the open clip plays on: its tracks with an empty target drive it.
func (e *Editor) ClipTarget() string { return e.clipMeta.target }

// ClipError returns why the open clip cannot be previewed on the scene, such as a track naming an object the scene does not have, or nil.
func (e *Editor) ClipError() error { return e.clipErr }

// NewClip opens a new, empty clip named name and length long that plays on the object target (the selected object when target is empty), as an undoable command.
func (e *Editor) NewClip(target, name string, length time.Duration) error {
	if e.playing {
		return ErrPlaying
	}
	target, err := e.clipTargetID(target)
	if err != nil {
		return err
	}
	if length <= 0 {
		return fmt.Errorf("editor: clip length %v must be positive", length)
	}
	clip := &resource.Animation{Header: resource.NewHeader(resource.KindAnimation), Name: name, Length: float32(length.Seconds())}
	if err := animclip.Check(clip, e.components); err != nil {
		return err
	}
	e.swapClip("New clip", clip, clipMeta{target: target})
	return nil
}

// OpenClipYAML parses data as an animation resource file, checks it against the component registry and opens it to play on target (the selected object when empty), as an undoable command.
func (e *Editor) OpenClipYAML(data []byte, target string) error {
	return e.openClip(data, "", target)
}

// OpenClipFile reads the animation resource file at path and opens it as OpenClipYAML does; SaveClip then writes back to path.
func (e *Editor) OpenClipFile(path, target string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return e.openClip(data, path, target)
}

func (e *Editor) openClip(data []byte, path, target string) error {
	if e.playing {
		return ErrPlaying
	}
	target, err := e.clipTargetID(target)
	if err != nil {
		return err
	}
	clip, err := resource.ParseAs[*resource.Animation](data)
	if err != nil {
		if path != "" {
			return fmt.Errorf("%s: %w", path, err)
		}
		return err
	}
	if err := animclip.Check(clip, e.components); err != nil {
		if path != "" {
			return fmt.Errorf("%s: %w", path, err)
		}
		return err
	}
	label := "Open clip"
	if path != "" {
		label += " " + path
	}
	e.swapClip(label, clip, clipMeta{path: path, target: target})
	return nil
}

// CloseClip closes the open clip as an undoable command.
func (e *Editor) CloseClip() error {
	if e.playing {
		return ErrPlaying
	}
	if e.clip == nil {
		return nil
	}
	e.swapClip("Close clip", nil, clipMeta{})
	return nil
}

// SaveClipYAML encodes the open clip as deterministic YAML.
func (e *Editor) SaveClipYAML() ([]byte, error) {
	if e.clip == nil {
		return nil, ErrNoClip
	}
	return resource.Encode(e.clip)
}

// SaveClipFile writes the open clip to path, replacing the file atomically, and remembers path as ClipPath.
func (e *Editor) SaveClipFile(path string) error {
	data, err := e.SaveClipYAML()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	e.clipMeta.path = path
	e.Status = "Saved clip " + path
	e.notify()
	return nil
}

// SetClipTarget makes the open clip play on the object id, as an undoable command.
func (e *Editor) SetClipTarget(id string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.clip == nil {
		return ErrNoClip
	}
	if obj, _, _ := e.FindObject(id); obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if id == e.clipMeta.target {
		return nil
	}
	before := e.clipMeta
	e.History.Execute(&clipCmd{ed: e, label: "Animate " + id, before: cloneClip(e.clip), after: cloneClip(e.clip), beforeMeta: &before, afterMeta: &clipMeta{path: before.path, target: id}, retarget: true})
	return nil
}

// SetClipName renames the open clip as an undoable command.
func (e *Editor) SetClipName(name string) error {
	return e.editClip("Rename clip", func(c *resource.Animation) error {
		c.Name = name
		return nil
	})
}

// SetClipLength sets the open clip's length as an undoable command. It refuses a length that would leave a key or event past the end.
func (e *Editor) SetClipLength(length time.Duration) error {
	if length <= 0 {
		return fmt.Errorf("editor: clip length %v must be positive", length)
	}
	return e.editClip("Set clip length", func(c *resource.Animation) error {
		c.Length = float32(length.Seconds())
		return nil
	})
}

// SetClipLoop sets whether the open clip loops, as an undoable command.
func (e *Editor) SetClipLoop(loop bool) error {
	return e.editClip("Set clip loop", func(c *resource.Animation) error {
		c.Loop = loop
		return nil
	})
}

// ClipTrack returns the index of the open clip's track for target and property, or -1.
func (e *Editor) ClipTrack(target, property string) int {
	if e.clip == nil {
		return -1
	}
	return findTrack(e.clip, target, property)
}

func findTrack(c *resource.Animation, target, property string) int {
	for i := range c.Tracks {
		if c.Tracks[i].Target == target && c.Tracks[i].Property == property {
			return i
		}
	}
	return -1
}

// KeyProperty keys property at time at with the value it has in the scene, as an undoable command: the object's own field, or a component field's value with its registered default when unset. Target is an object ID, or empty for the clip's target. A track is added for a property the clip does not key yet, and a key already at that time takes the new value. A bool property's new key holds its value until the next key (resource.InterpStep). It returns the track and key indexes.
func (e *Editor) KeyProperty(target, property string, at time.Duration) (track, key int, err error) {
	if e.clip == nil {
		return -1, -1, ErrNoClip
	}
	if target == e.clipMeta.target {
		target = ""
	}
	id := target
	if id == "" {
		id = e.clipMeta.target
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return -1, -1, fmt.Errorf("editor: object %q not found", id)
	}
	p, err := animclip.Lookup(e.components, property)
	if err != nil {
		return -1, -1, err
	}
	v, err := p.Get(obj, e.components)
	if err != nil {
		return -1, -1, err
	}
	t, err := e.clipSeconds(at)
	if err != nil {
		return -1, -1, err
	}
	value := keyNumber(p, v)
	err = e.editClip("Key "+property, func(c *resource.Animation) error {
		track = findTrack(c, target, property)
		if track < 0 {
			c.Tracks = append(c.Tracks, resource.Track{Target: target, Property: property})
			track = len(c.Tracks) - 1
		}
		tr := &c.Tracks[track]
		key = slices.IndexFunc(tr.Keys, func(k resource.Key) bool { return k.Time == t })
		if key >= 0 {
			tr.Keys[key].Value = value
			return nil
		}
		k := resource.Key{Time: t, Value: value}
		if p.Kind == animclip.KindBool {
			k.Interp = resource.InterpStep
		}
		key = insertKey(tr, k)
		return nil
	})
	if err != nil {
		return -1, -1, err
	}
	e.selectClipKey(track, key)
	return track, key, nil
}

// SetKeyValue sets the value of key key on track track as an undoable command. The value must suit the track's property: 0 or 1 for a bool, a whole number for an int, inside the range.
func (e *Editor) SetKeyValue(track, key int, value float64) error {
	return e.editKey("Set key value", track, key, func(c *resource.Animation, tr *resource.Track) error {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("editor: key value %v must be finite", value)
		}
		p, err := animclip.Lookup(e.components, tr.Property)
		if err != nil {
			return err
		}
		tr.Keys[key].Value = keyNumber(p, value)
		return nil
	})
}

// SetKeyInterp sets how key key on track track moves to the next key, as an undoable command.
func (e *Editor) SetKeyInterp(track, key int, interp resource.Interp) error {
	return e.editKey("Set key interpolation", track, key, func(c *resource.Animation, tr *resource.Track) error {
		if interp == resource.InterpLinear {
			interp = ""
		}
		tr.Keys[key].Interp = interp
		return nil
	})
}

// MoveKey moves key key on track track to time to, as an undoable command, and returns the key's index after the track's keys are put back in time order. It refuses a time outside the clip or one another key on the track already has.
func (e *Editor) MoveKey(track, key int, to time.Duration) (int, error) {
	t, err := e.clipSeconds(to)
	if err != nil {
		return -1, err
	}
	moved := key
	err = e.editKey("Move key", track, key, func(c *resource.Animation, tr *resource.Track) error {
		for i, k := range tr.Keys {
			if i != key && k.Time == t {
				return fmt.Errorf("editor: track %q already has a key at %vs", tr.Property, t)
			}
		}
		k := tr.Keys[key]
		k.Time = t
		tr.Keys = slices.Delete(tr.Keys, key, key+1)
		moved = insertKey(tr, k)
		return nil
	})
	if err != nil {
		return -1, err
	}
	e.selectClipKey(track, moved)
	return moved, nil
}

// DeleteKey deletes key key on track track as an undoable command; deleting a track's last key removes the track.
func (e *Editor) DeleteKey(track, key int) error {
	err := e.editKey("Delete key", track, key, func(c *resource.Animation, tr *resource.Track) error {
		tr.Keys = slices.Delete(tr.Keys, key, key+1)
		if len(tr.Keys) == 0 {
			c.Tracks = slices.Delete(c.Tracks, track, track+1)
		}
		return nil
	})
	if err == nil {
		e.selKey = clipKeyRef{-1, -1}
	}
	return err
}

// RemoveClipTrack removes track track and all its keys from the open clip as an undoable command.
func (e *Editor) RemoveClipTrack(track int) error {
	err := e.editClip("Remove track", func(c *resource.Animation) error {
		if track < 0 || track >= len(c.Tracks) {
			return fmt.Errorf("editor: clip has no track %d", track)
		}
		c.Tracks = slices.Delete(c.Tracks, track, track+1)
		return nil
	})
	if err == nil {
		e.selKey = clipKeyRef{-1, -1}
	}
	return err
}

// AddClipEvent adds an event named name at time at to the open clip, after any events already at that time, as an undoable command.
func (e *Editor) AddClipEvent(at time.Duration, name string) error {
	t, err := e.clipSeconds(at)
	if err != nil {
		return err
	}
	return e.editClip("Add event "+name, func(c *resource.Animation) error {
		i, _ := slices.BinarySearchFunc(c.Events, t, func(ev resource.Event, t float32) int {
			if ev.Time <= t {
				return -1
			}
			return 1
		})
		c.Events = slices.Insert(c.Events, i, resource.Event{Time: t, Name: name})
		return nil
	})
}

// DeleteClipEvent deletes event i of the open clip as an undoable command.
func (e *Editor) DeleteClipEvent(i int) error {
	return e.editClip("Delete event", func(c *resource.Animation) error {
		if i < 0 || i >= len(c.Events) {
			return fmt.Errorf("editor: clip has no event %d", i)
		}
		c.Events = slices.Delete(c.Events, i, i+1)
		return nil
	})
}

// clipKeyRef names a key of the open clip by track and key index.
type clipKeyRef struct{ track, key int }

// SelectedClipKey returns the key selected in the animation editor and whether one is.
func (e *Editor) SelectedClipKey() (track, key int, ok bool) {
	r := e.selKey
	if e.clip == nil || r.track < 0 || r.track >= len(e.clip.Tracks) || r.key < 0 || r.key >= len(e.clip.Tracks[r.track].Keys) {
		return -1, -1, false
	}
	return r.track, r.key, true
}

// SelectClipKey selects key key on track track in the animation editor; an index out of range clears the selection.
func (e *Editor) SelectClipKey(track, key int) {
	e.selectClipKey(track, key)
	e.notify()
}

func (e *Editor) selectClipKey(track, key int) {
	e.selKey = clipKeyRef{track, key}
	if _, _, ok := e.SelectedClipKey(); !ok {
		e.selKey = clipKeyRef{-1, -1}
	}
}

// ClipTime returns the animation editor's playhead.
func (e *Editor) ClipTime() time.Duration { return e.clipTime }

// SeekClip moves the playhead to at, clamped to the clip, and shows the scene posed by the clip there.
func (e *Editor) SeekClip(at time.Duration) {
	if e.clip == nil {
		return
	}
	e.clipTime = clampDuration(at, animclip.Seconds(e.clip.Length))
	if e.clipPlayer != nil {
		e.clipPlayer.Seek(e.clipTime)
	}
	e.notify()
}

// ClipPlaying reports whether the animation editor is playing the clip.
func (e *Editor) ClipPlaying() bool { return e.clipPlaying }

// PlayClip plays the open clip on the scene from the playhead (from the start when a clip that does not loop is at its end). With a scope attached (EditorView or AttachScope) a game.Loop at PlayRate advances it; otherwise call StepClip.
func (e *Editor) PlayClip() error {
	if e.playing {
		return ErrPlaying
	}
	if e.clip == nil {
		return ErrNoClip
	}
	if e.clipPlayer == nil {
		if e.clipErr != nil {
			return e.clipErr
		}
		return errors.New("editor: the clip cannot be previewed on this scene")
	}
	if e.clipPlaying {
		return nil
	}
	if !e.clip.Loop && e.clipTime >= e.clipPlayer.Length() {
		e.clipTime = 0
		e.clipPlayer.Seek(0)
	}
	e.clipPlaying = true
	e.clipPlayer.Play()
	if e.scope != nil {
		loop := &game.Loop{
			Rate:   e.PlayRate,
			Update: e.StepClip,
		}
		e.scope.Run(func() {
			e.clipStop = loop.Run(e.scope)
		})
	}
	e.Status = "Playing clip"
	e.notify()
	return nil
}

// PauseClip stops clip playback at the playhead.
func (e *Editor) PauseClip() {
	if !e.clipPlaying {
		return
	}
	e.stopClipLoop()
	e.Status = "Paused clip"
	e.notify()
}

func (e *Editor) stopClipLoop() {
	e.clipPlaying = false
	if e.clipStop != nil {
		e.clipStop()
		e.clipStop = nil
	}
	if e.clipPlayer != nil {
		e.clipPlayer.Pause()
	}
}

// StepClip advances clip playback by dt, as the loop PlayClip starts does on every fixed step. A clip that does not loop pauses at its end.
func (e *Editor) StepClip(dt time.Duration) {
	if !e.clipPlaying || e.clipPlayer == nil {
		return
	}
	e.clipPlayer.Update(dt)
	e.clipTime = e.clipPlayer.Time()
	if !e.clipPlayer.Playing() {
		e.stopClipLoop()
		e.Status = "Clip ended"
	}
	e.notify()
}

// refreshClipPreview rebuilds the scene the viewport shows while a clip is open: a copy of the document posed by the clip at the playhead, so the edited scene itself never takes animated values.
func (e *Editor) refreshClipPreview() {
	e.clipScene, e.clipPlayer, e.clipErr = nil, nil, nil
	if e.clip == nil || e.playing || e.Doc == nil {
		e.stopClipLoop()
		return
	}
	if e.BuildErr != nil {
		e.clipErr = e.BuildErr
		e.stopClipLoop()
		return
	}
	built, player, err := e.buildClipPreview()
	if err != nil {
		e.clipErr = err
		e.stopClipLoop()
		return
	}
	e.clipTime = clampDuration(e.clipTime, player.Length())
	player.Seek(e.clipTime)
	if e.clipPlaying {
		player.Play()
	}
	e.clipScene, e.clipPlayer = built, player
}

func (e *Editor) buildClipPreview() (*sceneformat.BuiltScene, *animclip.Player, error) {
	data, err := sceneformat.Encode(e.Doc)
	if err != nil {
		return nil, nil, err
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	built, err := sceneformat.Build(doc, e.Resolver)
	if err != nil {
		return nil, nil, err
	}
	player, err := animclip.NewPlayer(cloneClip(e.clip), e.components, animclip.BindScene(built, e.clipMeta.target))
	if err != nil {
		return nil, nil, err
	}
	player.OnEvent = func(name string) { e.Status = "Clip event: " + name }
	return built, player, nil
}

func (e *Editor) clipTargetID(target string) (string, error) {
	if target == "" {
		target = e.SelectedID
	}
	if target == "" {
		return "", errors.New("editor: a clip plays on an object; select one or name it")
	}
	if obj, _, _ := e.FindObject(target); obj == nil {
		return "", fmt.Errorf("editor: object %q not found", target)
	}
	return target, nil
}

// clipSeconds converts a playhead time to the seconds a key stores, refusing one outside the open clip.
func (e *Editor) clipSeconds(at time.Duration) (float32, error) {
	if e.clip == nil {
		return 0, ErrNoClip
	}
	if at < 0 || at > animclip.Seconds(e.clip.Length) {
		return 0, fmt.Errorf("editor: time %v is outside the clip's 0 to %vs", at, e.clip.Length)
	}
	return float32(at.Seconds()), nil
}

// keyNumber stores v as a key of p: as float32, stepped inward when rounding to float32 crosses an end of p's range.
func keyNumber(p animclip.Property, v float64) float32 {
	f := float32(v)
	if p.Min != nil && animclip.KeyValue(f) < *p.Min {
		f = math.Nextafter32(f, float32(math.Inf(1)))
	}
	if p.Max != nil && animclip.KeyValue(f) > *p.Max {
		f = math.Nextafter32(f, float32(math.Inf(-1)))
	}
	return f
}

// insertKey inserts k into tr's keys in time order and returns its index.
func insertKey(tr *resource.Track, k resource.Key) int {
	i, _ := slices.BinarySearchFunc(tr.Keys, k.Time, func(a resource.Key, t float32) int {
		switch {
		case a.Time < t:
			return -1
		case a.Time > t:
			return 1
		}
		return 0
	})
	tr.Keys = slices.Insert(tr.Keys, i, k)
	return i
}

func clampDuration(d, length time.Duration) time.Duration {
	return min(max(d, 0), length)
}

// editKey runs fn on the track and key named by track and key in a copy of the open clip and records the change as editClip does.
func (e *Editor) editKey(label string, track, key int, fn func(c *resource.Animation, tr *resource.Track) error) error {
	return e.editClip(label, func(c *resource.Animation) error {
		if track < 0 || track >= len(c.Tracks) {
			return fmt.Errorf("editor: clip has no track %d", track)
		}
		tr := &c.Tracks[track]
		if key < 0 || key >= len(tr.Keys) {
			return fmt.Errorf("editor: track %q has no key %d", tr.Property, key)
		}
		return fn(c, tr)
	})
}

// editClip runs fn on a copy of the open clip, checks the result against the component registry and records it as an undoable command; a change that leaves the clip as it was records nothing.
func (e *Editor) editClip(label string, fn func(c *resource.Animation) error) error {
	if e.playing {
		return ErrPlaying
	}
	if e.clip == nil {
		return ErrNoClip
	}
	next := cloneClip(e.clip)
	if err := fn(next); err != nil {
		return err
	}
	if err := animclip.Check(next, e.components); err != nil {
		return err
	}
	if reflect.DeepEqual(next, e.clip) {
		return nil
	}
	e.History.Execute(&clipCmd{ed: e, label: label, before: cloneClip(e.clip), after: next})
	return nil
}

// swapClip records replacing the open clip, its file and its target as one undoable command.
func (e *Editor) swapClip(label string, clip *resource.Animation, meta clipMeta) {
	before, after := e.clipMeta, meta
	e.History.Execute(&clipCmd{ed: e, label: label, before: cloneClip(e.clip), after: cloneClip(clip), beforeMeta: &before, afterMeta: &after})
}

// clipCmd replaces the open clip's contents, and for a command that opens, closes or retargets a clip also its file and target. Opening and closing a clip, done or undone, stops its playback and puts the playhead back at the start; retargeting keeps both.
type clipCmd struct {
	ed                    *Editor
	label                 string
	before, after         *resource.Animation
	beforeMeta, afterMeta *clipMeta
	retarget              bool
}

func (c *clipCmd) Label() string { return c.label }

func (c *clipCmd) Do() { c.apply(c.after, c.afterMeta) }

func (c *clipCmd) Undo() { c.apply(c.before, c.beforeMeta) }

func (c *clipCmd) apply(clip *resource.Animation, meta *clipMeta) {
	e := c.ed
	e.clip = cloneClip(clip)
	if meta != nil {
		if !c.retarget {
			e.stopClipLoop()
			e.clipTime = 0
			e.selKey = clipKeyRef{-1, -1}
		}
		e.clipMeta = *meta
	}
	e.Status = c.label
}

func cloneClip(c *resource.Animation) *resource.Animation {
	if c == nil {
		return nil
	}
	out := *c
	out.Tracks = make([]resource.Track, len(c.Tracks))
	for i, tr := range c.Tracks {
		out.Tracks[i] = tr
		out.Tracks[i].Keys = slices.Clone(tr.Keys)
	}
	if c.Tracks == nil {
		out.Tracks = nil
	}
	out.Events = slices.Clone(c.Events)
	return &out
}
