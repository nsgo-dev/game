package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/cutscene"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoCutscene is returned by cutscene timeline edits when no cutscene document is open.
var ErrNoCutscene = errors.New("editor: no cutscene is open")

// ErrNoCutsceneReview is returned by cutscene review actions when no review is open.
var ErrNoCutsceneReview = errors.New("editor: no cutscene review is open")

// CutsceneEntry describes one cutscene file shown in the cutscene timeline's left rail.
type CutsceneEntry struct {
	Path     string
	Name     string
	Kind     cutscene.Kind
	Length   time.Duration
	InReview bool
}

// CutsceneLineEntry describes one localized dialogue line shown in the left rail and dialogue inspector.
type CutsceneLineEntry struct {
	ID    string
	En    string
	Zh    string
	Added bool
}

// CutsceneChangeKind classifies how an agent review changed a cutscene track or clip.
type CutsceneChangeKind int

const (
	CutsceneChangeNone CutsceneChangeKind = iota
	CutsceneChangeAdded
	CutsceneChangeChanged
	CutsceneChangeRemoved
)

// CutsceneChange is one independently revertible/restorable change inside an open cutscene review.
type CutsceneChange struct {
	ID         string
	Kind       CutsceneChangeKind
	Sign       string // "+", "~", or "-"
	Title      string
	Detail     string
	TrackKey   string
	ClipKeys   []string
	FocusTrack int
	FocusClip  int
	MetaChange bool
	Reverted   bool
}

// CutsceneReviewView selects whether the cutscene review displays the Before or After state.
type CutsceneReviewView int

const (
	CutsceneReviewAfter CutsceneReviewView = iota
	CutsceneReviewBefore
)

// CutsceneReviewRequest opens an agent cutscene review comparing Base against Proposed.
type CutsceneReviewRequest struct {
	// Prompt is the user request that produced the agent's changes (for example "make the floor 1 intro reveal a slime and hint at the lever").
	Prompt string
	// Path is the file path on disk (or empty for an in-memory cutscene).
	Path string
	// Base is the cutscene before the agent's changes; when nil, the currently open cutscene is used.
	Base *cutscene.Cutscene
	// Proposed is the agent's proposed cutscene document (required).
	Proposed *cutscene.Cutscene
	// Changes optionally supplies custom grouped changes; when empty, DiffCutsceneChanges computes them automatically.
	Changes []CutsceneChange
}

// CutsceneThumbActor is one actor's sampled position in a timeline strip thumbnail.
type CutsceneThumbActor struct {
	ID   string
	X, Y float32
}

// CutsceneThumbSample holds the sampled camera and actor poses for one frame in the timeline thumbnail strip.
type CutsceneThumbSample struct {
	Time   time.Duration
	CamX   float32
	CamY   float32
	Zoom   float32
	Actors []CutsceneThumbActor
}

type cutsceneClipEdit struct {
	start    cutscene.Duration
	duration cutscene.Duration
	easing   cutscene.Easing
}

type cutsceneReviewState struct {
	prompt     string
	base       *cutscene.Cutscene
	proposed   *cutscene.Cutscene
	changes    []CutsceneChange
	view       CutsceneReviewView
	peekBefore bool
	edits      map[string]cutsceneClipEdit
	editUndo   []map[string]cutsceneClipEdit
	editRedo   []map[string]cutsceneClipEdit
	composed   *cutscene.Cutscene
	prevDoc    *cutscene.Cutscene
	prevMeta   cutsceneMeta
	prevSaved  []byte
	prevDirty  bool
}

func (r *cutsceneReviewState) invalidateComposed() {
	if r != nil {
		r.composed = nil
	}
}

func (r *cutsceneReviewState) composedDoc() *cutscene.Cutscene {
	if r == nil {
		return nil
	}
	if r.composed == nil {
		r.composed = composeCutsceneReview(r.base, r.proposed, r.changes, r.edits)
	}
	return r.composed
}

type cutsceneMeta struct {
	path string
}

type cutsceneEditorState struct {
	doc             *cutscene.Cutscene
	meta            cutsceneMeta
	saved           []byte
	dirty           bool
	structRev       uint64
	time            time.Duration
	playing         bool
	stop            func()
	scene           *sceneformat.BuiltScene
	player          *cutscene.TimelinePlayer
	err             error
	selTrack        int
	selClip         int
	thumbs          []CutsceneThumbSample
	review          *cutsceneReviewState
	locEnPath       string
	locZhPath       string
	locCatalog      *l10n.Catalog
	locProjectPath  string
	locFilesRev     uint64
	locLoaded       bool
	projCutscenes   []CutsceneEntry
	projCutPath     string
	projCutFilesRev uint64
	projCutLoaded   bool
}

const cutsceneThumbCount = 24

// CutsceneOpen reports whether a cutscene document is open in the cutscene timeline editor.
func (e *Editor) CutsceneOpen() bool {
	return e != nil && e.cutscene.doc != nil
}

// Cutscene returns a copy of the cutscene currently displayed by the timeline editor (the effective Before/After/edited review document while a review is open, or the open cutscene otherwise), or nil when none is open.
func (e *Editor) Cutscene() *cutscene.Cutscene {
	if e == nil || e.cutscene.doc == nil {
		return nil
	}
	return cloneCutscene(e.effectiveCutsceneDoc())
}

// CutsceneBase returns a copy of the underlying cutscene document before any uncommitted review changes, or nil.
func (e *Editor) CutsceneBase() *cutscene.Cutscene {
	if e == nil || e.cutscene.doc == nil {
		return nil
	}
	if e.cutscene.review != nil && e.cutscene.review.base != nil {
		return cloneCutscene(e.cutscene.review.base)
	}
	return cloneCutscene(e.cutscene.doc)
}

// CutscenePath returns the file path the open cutscene was loaded from or saved to, or "".
func (e *Editor) CutscenePath() string {
	if e == nil {
		return ""
	}
	return e.cutscene.meta.path
}

// CutsceneError returns why the open cutscene cannot be previewed on the scene, or nil.
func (e *Editor) CutsceneError() error {
	if e == nil {
		return nil
	}
	return e.cutscene.err
}

// CutsceneDirty reports whether the open cutscene differs from its last opened or saved YAML bytes, or has an uncommitted agent review open.
func (e *Editor) CutsceneDirty() bool {
	if e == nil || e.cutscene.doc == nil {
		return false
	}
	if e.cutscene.review != nil {
		return true
	}
	return e.cutscene.dirty
}

func (e *Editor) updateCutsceneDirty() {
	if e == nil || e.cutscene.doc == nil {
		if e != nil {
			e.cutscene.dirty = false
		}
		return
	}
	data, err := cutscene.Encode(*e.cutscene.doc)
	e.cutscene.dirty = err != nil || !bytes.Equal(data, e.cutscene.saved)
}

// CutsceneTime returns the cutscene timeline's current playhead position.
func (e *Editor) CutsceneTime() time.Duration {
	if e == nil {
		return 0
	}
	return e.cutscene.time
}

// CutsceneLength returns the length of the currently displayed timeline cutscene, or 0 for none/video.
func (e *Editor) CutsceneLength() time.Duration {
	doc := e.effectiveCutsceneDoc()
	if doc == nil || doc.EffectiveKind() != cutscene.KindTimeline {
		return 0
	}
	return time.Duration(doc.Length)
}

// CutscenePlaying reports whether cutscene timeline preview playback is active.
func (e *Editor) CutscenePlaying() bool {
	return e != nil && e.cutscene.playing
}

// CutscenePreviewScene returns the BuiltScene posed by the cutscene TimelinePlayer at CutsceneTime, or nil.
func (e *Editor) CutscenePreviewScene() *sceneformat.BuiltScene {
	if e == nil {
		return nil
	}
	return e.cutscene.scene
}

// CutscenePreviewPlayer returns the active cutscene.TimelinePlayer used to pose the preview scene, or nil.
func (e *Editor) CutscenePreviewPlayer() *cutscene.TimelinePlayer {
	if e == nil {
		return nil
	}
	return e.cutscene.player
}

// CutsceneActiveDialogue returns the dialogue line active at the current playhead time.
func (e *Editor) CutsceneActiveDialogue() cutscene.DialogueLine {
	if e == nil || e.cutscene.player == nil {
		return cutscene.DialogueLine{}
	}
	return e.cutscene.player.ActiveDialogue()
}

// CutsceneThumbs returns the sampled thumbnail frames across the cutscene timeline for the scrub strip.
func (e *Editor) CutsceneThumbs() []CutsceneThumbSample {
	if e == nil || len(e.cutscene.thumbs) == 0 {
		return nil
	}
	return slices.Clone(e.cutscene.thumbs)
}

// CutsceneCameraLabel returns the preview stage's camera caption (for example "Camera · pan to knight · 640 × 360").
func (e *Editor) CutsceneCameraLabel() string {
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return "Camera"
	}
	vw, vh := float32(640), float32(360)
	if e.cutscene.scene != nil && e.cutscene.scene.Doc != nil && e.cutscene.scene.Doc.VirtualWidth > 0 && e.cutscene.scene.Doc.VirtualHeight > 0 {
		vw, vh = e.cutscene.scene.Doc.VirtualWidth, e.cutscene.scene.Doc.VirtualHeight
	} else if e.Doc != nil && e.Doc.VirtualWidth > 0 && e.Doc.VirtualHeight > 0 {
		vw, vh = e.Doc.VirtualWidth, e.Doc.VirtualHeight
	}
	if doc.EffectiveKind() == cutscene.KindVideo {
		return fmt.Sprintf("Video · %s", doc.Source)
	}
	t := e.cutscene.time
	length := time.Duration(doc.Length)
	activeLabel := ""
	for ti := range doc.Tracks {
		tr := &doc.Tracks[ti]
		if tr.Kind != cutscene.TrackCamera {
			continue
		}
		for ci := range tr.Clips {
			cl := &tr.Clips[ci]
			start := time.Duration(cl.Start)
			end := start + time.Duration(cl.Duration)
			if t >= start && (t < end || (t == length && end == length)) {
				activeLabel = strings.ToLower(cutsceneClipDisplayLabel(tr.Kind, *cl))
				break
			}
		}
		if activeLabel != "" {
			break
		}
	}
	if activeLabel == "" {
		return fmt.Sprintf("Camera · %.0f × %.0f", vw, vh)
	}
	return fmt.Sprintf("Camera · %s · %.0f × %.0f", activeLabel, vw, vh)
}

// checkCutsceneProjectSymlink verifies that absPath does not escape the open project's root via symlinks.
// When mustContain is true (project-relative reads), absPath must resolve strictly inside the project root.
// When mustContain is false (writes), containment is enforced whenever absPath is lexically inside the project root.
func (e *Editor) checkCutsceneProjectSymlink(absPath string, mustContain bool) error {
	if e == nil || e.ProjectPath == "" {
		return nil
	}
	root := filepath.Clean(filepath.Dir(e.ProjectPath))
	cleanPath := filepath.Clean(absPath)
	if !mustContain {
		lexRel, err := filepath.Rel(root, cleanPath)
		if err != nil || lexRel == ".." || strings.HasPrefix(lexRel, ".."+string(filepath.Separator)) || filepath.IsAbs(lexRel) {
			return nil
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	var realPath string
	if mustContain {
		realPath, err = filepath.EvalSymlinks(cleanPath)
		if err != nil {
			return err
		}
	} else {
		realPath, err = evalSymlinksOrParent(cleanPath)
		if err != nil {
			return err
		}
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("editor: cutscene path %q resolves outside the project", absPath)
	}
	return nil
}

func (e *Editor) writeCutsceneFileAtomic(filePath string, data []byte) error {
	if err := e.checkCutsceneProjectSymlink(filePath, false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	if err := e.checkCutsceneProjectSymlink(filePath, false); err != nil {
		return err
	}
	return writeFileAtomic(filePath, data)
}

// OpenCutsceneYAML parses data as a cutscene YAML document and opens it in the cutscene timeline editor as an undoable command.
func (e *Editor) OpenCutsceneYAML(data []byte) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	return e.openCutscene(data, "")
}

// OpenCutsceneFile reads a *.cutscene.yaml file from disk and opens it in the cutscene timeline editor.
func (e *Editor) OpenCutsceneFile(filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkCutsceneProjectSymlink(filePath, false); err != nil {
		return err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return e.openCutscene(data, filePath)
}

// OpenCutsceneProjectFile opens a project-relative *.cutscene.yaml file in the cutscene timeline editor.
func (e *Editor) OpenCutsceneProjectFile(relPath string) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	abs, err := e.projectFile(relPath)
	if err != nil {
		return err
	}
	if err := e.checkCutsceneProjectSymlink(abs, true); err != nil {
		return err
	}
	return e.OpenCutsceneFile(abs)
}

func (e *Editor) openCutscene(data []byte, filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	doc, err := cutscene.Parse(data)
	if err != nil {
		if filePath != "" {
			return fmt.Errorf("%s: %w", filePath, err)
		}
		return err
	}
	canonical, err := cutscene.Encode(doc)
	if err != nil {
		return err
	}
	label := "Open cutscene"
	if filePath != "" {
		label += " " + path.Base(strings.ReplaceAll(filePath, `\`, "/"))
	}
	beforeMeta := e.cutscene.meta
	e.History.Execute(&cutsceneCmd{
		ed:          e,
		label:       label,
		before:      cloneCutscene(e.cutscene.doc),
		after:       cloneCutscene(&doc),
		beforeMeta:  &beforeMeta,
		afterMeta:   &cutsceneMeta{path: filePath},
		beforeSaved: slices.Clone(e.cutscene.saved),
		afterSaved:  canonical,
	})
	return nil
}

// CloseCutscene closes the open cutscene timeline editor (and any open review) as an undoable command.
func (e *Editor) CloseCutscene() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.cutscene.doc == nil {
		return nil
	}
	beforeMeta := e.cutscene.meta
	e.History.Execute(&cutsceneCmd{
		ed:          e,
		label:       "Close cutscene",
		before:      cloneCutscene(e.cutscene.doc),
		after:       nil,
		beforeMeta:  &beforeMeta,
		afterMeta:   &cutsceneMeta{},
		beforeSaved: slices.Clone(e.cutscene.saved),
		afterSaved:  nil,
	})
	return nil
}

// SaveCutsceneYAML encodes the open cutscene as canonical deterministic YAML.
func (e *Editor) SaveCutsceneYAML() ([]byte, error) {
	if e == nil || e.cutscene.doc == nil {
		return nil, ErrNoCutscene
	}
	return cutscene.Encode(*e.cutscene.doc)
}

// SaveCutsceneFile writes the open cutscene atomically to filePath and updates CutscenePath.
func (e *Editor) SaveCutsceneFile(filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	data, err := e.SaveCutsceneYAML()
	if err != nil {
		return err
	}
	if err := e.writeCutsceneFileAtomic(filePath, data); err != nil {
		return err
	}
	e.cutscene.meta.path = filePath
	e.cutscene.saved = data
	e.cutscene.dirty = false
	e.cutscene.structRev++
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved cutscene " + filePath
	e.notify()
	e.CheckProblems()
	return nil
}

func (e *Editor) saveCutsceneFromPanel() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.cutscene.review != nil {
		return errors.New("editor: keep or revert the open cutscene review before saving")
	}
	if e.OnSaveCutscene != nil {
		data, err := e.SaveCutsceneYAML()
		if err != nil {
			return err
		}
		if err := e.OnSaveCutscene(data); err != nil {
			return err
		}
		e.cutscene.saved = data
		e.cutscene.dirty = false
		e.cutscene.structRev++
		e.Status = "Saved cutscene"
		e.notify()
		return nil
	}
	if e.cutscene.meta.path == "" {
		return errors.New("editor: no cutscene path; open a *.cutscene.yaml file or set OnSaveCutscene")
	}
	return e.SaveCutsceneFile(e.cutscene.meta.path)
}

// PlayCutscene starts fixed-step preview playback of the open timeline cutscene on game.Loop.
func (e *Editor) PlayCutscene() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return ErrNoCutscene
	}
	if doc.EffectiveKind() != cutscene.KindTimeline {
		return errors.New("editor: video cutscenes do not use the timeline player")
	}
	if e.cutscene.player == nil {
		if e.cutscene.err != nil {
			return e.cutscene.err
		}
		return errors.New("editor: cutscene cannot be previewed on this scene")
	}
	if e.cutscene.playing {
		return nil
	}
	if e.cutscene.time >= time.Duration(doc.Length) {
		e.cutscene.time = 0
		e.cutscene.player.Reset()
		e.cutscene.player.Seek(0)
	}
	e.cutscene.playing = true
	e.cutscene.player.Play()
	if e.scope != nil {
		loop := &game.Loop{
			Rate:   e.PlayRate,
			Update: e.StepCutscene,
		}
		e.scope.Run(func() {
			e.cutscene.stop = loop.Run(e.scope)
		})
	}
	e.Status = "Playing cutscene"
	e.notify()
	return nil
}

// PauseCutscene pauses fixed-step preview playback of the open timeline cutscene.
func (e *Editor) PauseCutscene() {
	if e == nil || !e.cutscene.playing {
		return
	}
	e.stopCutsceneLoop()
	e.Status = "Paused cutscene"
	e.notify()
}

func (e *Editor) stopCutsceneLoop() {
	if e == nil {
		return
	}
	e.cutscene.playing = false
	if e.cutscene.stop != nil {
		e.cutscene.stop()
		e.cutscene.stop = nil
	}
	if e.cutscene.player != nil {
		e.cutscene.player.Pause()
	}
}

// StepCutscene advances the cutscene timeline preview by one fixed step dt.
func (e *Editor) StepCutscene(dt time.Duration) {
	if e == nil || e.cutscene.player == nil || dt <= 0 {
		return
	}
	length := e.cutscene.player.Length()
	if dt >= length || e.cutscene.time >= length-dt {
		e.cutscene.time = length
		e.cutscene.player.Seek(length)
		e.stopCutsceneLoop()
		e.notify()
		return
	}
	next := e.cutscene.time + dt
	e.cutscene.time = next
	e.cutscene.player.Seek(next)
	e.notify()
}

// SeekCutscene scrubs the cutscene timeline playhead to at (clamped to [0, Length]) and poses the preview scene deterministically using cutscene.TimelinePlayer.
func (e *Editor) SeekCutscene(at time.Duration) {
	if e == nil || e.cutscene.doc == nil {
		return
	}
	length := e.CutsceneLength()
	if at < 0 {
		at = 0
	}
	if length > 0 && at > length {
		at = length
	}
	e.cutscene.time = at
	if e.cutscene.player != nil {
		e.cutscene.player.Seek(at)
	}
	e.notify()
}

// SelectedCutsceneClip returns the currently selected track index, clip index, clip, and track on the displayed cutscene.
func (e *Editor) SelectedCutsceneClip() (trackIdx, clipIdx int, clip cutscene.Clip, track cutscene.Track, ok bool) {
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return -1, -1, cutscene.Clip{}, cutscene.Track{}, false
	}
	ti, ci := e.cutscene.selTrack, e.cutscene.selClip
	if ti < 0 || ti >= len(doc.Tracks) || ci < 0 || ci >= len(doc.Tracks[ti].Clips) {
		return -1, -1, cutscene.Clip{}, cutscene.Track{}, false
	}
	return ti, ci, doc.Tracks[ti].Clips[ci], doc.Tracks[ti], true
}

// SelectCutsceneClip selects the clip at (trackIdx, clipIdx) on the displayed cutscene timeline.
func (e *Editor) SelectCutsceneClip(trackIdx, clipIdx int) {
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return
	}
	if trackIdx < 0 || trackIdx >= len(doc.Tracks) || clipIdx < 0 || clipIdx >= len(doc.Tracks[trackIdx].Clips) {
		return
	}
	e.cutscene.selTrack = trackIdx
	e.cutscene.selClip = clipIdx
	e.cutscene.structRev++
	e.notify()
}

// SelectCutsceneClipByID selects the clip with the given clip ID on the displayed cutscene timeline.
func (e *Editor) SelectCutsceneClipByID(clipID string) bool {
	doc := e.effectiveCutsceneDoc()
	if doc == nil || clipID == "" {
		return false
	}
	for ti := range doc.Tracks {
		for ci := range doc.Tracks[ti].Clips {
			if doc.Tracks[ti].Clips[ci].ID == clipID {
				e.cutscene.selTrack = ti
				e.cutscene.selClip = ci
				e.cutscene.structRev++
				e.notify()
				return true
			}
		}
	}
	return false
}

// AdjustCutsceneClipStart nudges the selected clip's Start by delta (rounded to 0.1 s increments when delta is ±100ms).
func (e *Editor) AdjustCutsceneClipStart(delta time.Duration) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	ti, ci, cl, _, ok := e.SelectedCutsceneClip()
	if !ok {
		return ErrNoCutscene
	}
	cur := time.Duration(cl.Start)
	var sum time.Duration
	if delta > 0 && cur > time.Duration(math.MaxInt64)-delta {
		sum = time.Duration(math.MaxInt64)
	} else if delta < 0 && cur < time.Duration(math.MinInt64)-delta {
		sum = 0
	} else {
		sum = cur + delta
	}
	nextStart := roundTenthSecond(sum)
	if nextStart < 0 {
		nextStart = 0
	}
	return e.SetCutsceneClipTiming(ti, ci, nextStart, time.Duration(cl.Duration))
}

// AdjustCutsceneClipDuration nudges the selected clip's Duration by delta (clamped to >= 0.1 s).
func (e *Editor) AdjustCutsceneClipDuration(delta time.Duration) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	ti, ci, cl, _, ok := e.SelectedCutsceneClip()
	if !ok {
		return ErrNoCutscene
	}
	cur := time.Duration(cl.Duration)
	var sum time.Duration
	if delta > 0 && cur > time.Duration(math.MaxInt64)-delta {
		sum = time.Duration(math.MaxInt64)
	} else if delta < 0 && cur < time.Duration(math.MinInt64)-delta {
		sum = 0
	} else {
		sum = cur + delta
	}
	nextDur := roundTenthSecond(sum)
	if nextDur < 100*time.Millisecond {
		nextDur = 100 * time.Millisecond
	}
	return e.SetCutsceneClipTiming(ti, ci, time.Duration(cl.Start), nextDur)
}

func roundTenthSecond(d time.Duration) time.Duration {
	const maxTenths = float64(math.MaxInt64/int64(100*time.Millisecond)) - 1
	tenths := math.Round(d.Seconds() * 10)
	if math.IsNaN(tenths) || tenths <= 0 {
		return 0
	}
	if tenths >= maxTenths {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(tenths) * 100 * time.Millisecond
}

func findClipByKey(doc *cutscene.Cutscene, tk, ck string) (trackIdx, clipIdx int, ok bool) {
	if doc == nil {
		return -1, -1, false
	}
	for ti := range doc.Tracks {
		if trackIdentity(doc.Tracks, ti) != tk {
			continue
		}
		for ci := range doc.Tracks[ti].Clips {
			if clipIdentity(doc.Tracks[ti], doc.Tracks[ti].Clips[ci], ci) == ck {
				return ti, ci, true
			}
		}
	}
	return -1, -1, false
}

// SetCutsceneClipTiming updates the Start and Duration of the clip at (trackIdx, clipIdx).
// While a review is open, the edit joins the review and can be undone with Undo (⌘Z); otherwise it executes an ordinary undoable command on History.
func (e *Editor) SetCutsceneClipTiming(trackIdx, clipIdx int, start, dur time.Duration) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if start < 0 {
		return errors.New("editor: cutscene clip start must be non-negative")
	}
	if dur <= 0 {
		return errors.New("editor: cutscene clip duration must be positive")
	}
	if start > time.Duration(math.MaxInt64)-dur {
		return errors.New("editor: cutscene clip end overflows duration")
	}
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return ErrNoCutscene
	}
	if trackIdx < 0 || trackIdx >= len(doc.Tracks) || clipIdx < 0 || clipIdx >= len(doc.Tracks[trackIdx].Clips) {
		return fmt.Errorf("editor: cutscene clip (%d, %d) out of range", trackIdx, clipIdx)
	}
	if e.cutscene.review != nil {
		tk := trackIdentity(doc.Tracks, trackIdx)
		ck := clipIdentity(doc.Tracks[trackIdx], doc.Tracks[trackIdx].Clips[clipIdx], clipIdx)
		afterDoc := e.cutscene.review.composedDoc()
		ati, aci, found := findClipByKey(afterDoc, tk, ck)
		if !found {
			return fmt.Errorf("editor: cutscene clip (%d, %d) not present in After view", trackIdx, clipIdx)
		}
		cl := afterDoc.Tracks[ati].Clips[aci]
		key := tk + "\x00" + ck
		nextEdits := cloneClipEdits(e.cutscene.review.edits)
		nextEdits[key] = cutsceneClipEdit{
			start:    cutscene.Duration(start),
			duration: cutscene.Duration(dur),
			easing:   cl.Easing,
		}
		candidate := composeCutsceneReview(e.cutscene.review.base, e.cutscene.review.proposed, e.cutscene.review.changes, nextEdits)
		if err := cutscene.Validate(*candidate); err != nil {
			return err
		}
		e.cutscene.review.view = CutsceneReviewAfter
		e.cutscene.review.peekBefore = false
		e.pushCutsceneReviewEdit(nextEdits)
		e.cutscene.review.composed = candidate
		e.selectClipByKey(candidate, tk, ck)
		e.cutscene.structRev++
		e.refreshCutscenePreview()
		e.notify()
		return nil
	}
	next := cloneCutscene(e.cutscene.doc)
	next.Tracks[trackIdx].Clips[clipIdx].Start = cutscene.Duration(start)
	next.Tracks[trackIdx].Clips[clipIdx].Duration = cutscene.Duration(dur)
	// Keep clips sorted by Start (important for sound tracks where reordering is valid) and track the moved clip's new index.
	type indexedClip struct {
		clip   cutscene.Clip
		target bool
	}
	wrapped := make([]indexedClip, len(next.Tracks[trackIdx].Clips))
	for i, c := range next.Tracks[trackIdx].Clips {
		wrapped[i] = indexedClip{clip: c, target: i == clipIdx}
	}
	slices.SortStableFunc(wrapped, func(a, b indexedClip) int {
		if a.clip.Start < b.clip.Start {
			return -1
		}
		if a.clip.Start > b.clip.Start {
			return 1
		}
		return 0
	})
	newClipIdx := clipIdx
	for i, w := range wrapped {
		next.Tracks[trackIdx].Clips[i] = w.clip
		if w.target {
			newClipIdx = i
		}
	}
	if err := cutscene.Validate(*next); err != nil {
		return err
	}
	if reflect.DeepEqual(next, e.cutscene.doc) {
		return nil
	}
	e.History.Execute(&cutsceneCmd{
		ed:          e,
		label:       "Retime cutscene clip",
		targetPath:  e.cutscene.meta.path,
		before:      cloneCutscene(e.cutscene.doc),
		after:       next,
		beforeSaved: slices.Clone(e.cutscene.saved),
		afterSaved:  slices.Clone(e.cutscene.saved),
	})
	e.cutscene.selTrack = trackIdx
	e.cutscene.selClip = newClipIdx
	e.cutscene.structRev++
	return nil
}

// SetCutsceneClipEasing updates the Easing of the clip at (trackIdx, clipIdx) on a camera or actor track.
func (e *Editor) SetCutsceneClipEasing(trackIdx, clipIdx int, easing cutscene.Easing) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return ErrNoCutscene
	}
	if trackIdx < 0 || trackIdx >= len(doc.Tracks) || clipIdx < 0 || clipIdx >= len(doc.Tracks[trackIdx].Clips) {
		return fmt.Errorf("editor: cutscene clip (%d, %d) out of range", trackIdx, clipIdx)
	}
	tr := doc.Tracks[trackIdx]
	if tr.Kind != cutscene.TrackCamera && tr.Kind != cutscene.TrackActor {
		return fmt.Errorf("editor: easing is only valid on camera and actor tracks, not %s", tr.Kind)
	}
	if e.cutscene.review != nil {
		tk := trackIdentity(doc.Tracks, trackIdx)
		ck := clipIdentity(tr, tr.Clips[clipIdx], clipIdx)
		afterDoc := e.cutscene.review.composedDoc()
		ati, aci, found := findClipByKey(afterDoc, tk, ck)
		if !found {
			return fmt.Errorf("editor: cutscene clip (%d, %d) not present in After view", trackIdx, clipIdx)
		}
		atr := afterDoc.Tracks[ati]
		if atr.Kind != cutscene.TrackCamera && atr.Kind != cutscene.TrackActor {
			return fmt.Errorf("editor: easing is only valid on camera and actor tracks, not %s", atr.Kind)
		}
		cl := atr.Clips[aci]
		key := tk + "\x00" + ck
		nextEdits := cloneClipEdits(e.cutscene.review.edits)
		nextEdits[key] = cutsceneClipEdit{
			start:    cl.Start,
			duration: cl.Duration,
			easing:   easing,
		}
		candidate := composeCutsceneReview(e.cutscene.review.base, e.cutscene.review.proposed, e.cutscene.review.changes, nextEdits)
		if err := cutscene.Validate(*candidate); err != nil {
			return err
		}
		e.cutscene.review.view = CutsceneReviewAfter
		e.cutscene.review.peekBefore = false
		e.pushCutsceneReviewEdit(nextEdits)
		e.cutscene.review.composed = candidate
		e.selectClipByKey(candidate, tk, ck)
		e.cutscene.structRev++
		e.refreshCutscenePreview()
		e.notify()
		return nil
	}
	next := cloneCutscene(e.cutscene.doc)
	next.Tracks[trackIdx].Clips[clipIdx].Easing = easing
	if err := cutscene.Validate(*next); err != nil {
		return err
	}
	if reflect.DeepEqual(next, e.cutscene.doc) {
		return nil
	}
	e.History.Execute(&cutsceneCmd{
		ed:          e,
		label:       "Set cutscene clip easing",
		targetPath:  e.cutscene.meta.path,
		before:      cloneCutscene(e.cutscene.doc),
		after:       next,
		beforeSaved: slices.Clone(e.cutscene.saved),
		afterSaved:  slices.Clone(e.cutscene.saved),
	})
	return nil
}

// OpenCutsceneReview opens a review of an agent's proposed changes to a cutscene without writing to disk until KeepCutsceneReview is called.
func (e *Editor) OpenCutsceneReview(req CutsceneReviewRequest) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if req.Proposed == nil {
		return errors.New("editor: cutscene review requires a proposed cutscene")
	}
	if err := cutscene.Validate(*req.Proposed); err != nil {
		return err
	}
	if req.Path != "" {
		if err := e.checkCutsceneProjectSymlink(req.Path, false); err != nil {
			return err
		}
	}
	prevDoc := cloneCutscene(e.cutscene.doc)
	prevMeta := e.cutscene.meta
	prevSaved := slices.Clone(e.cutscene.saved)
	prevDirty := e.cutscene.dirty
	if e.cutscene.review != nil {
		prevDoc = cloneCutscene(e.cutscene.review.prevDoc)
		prevMeta = e.cutscene.review.prevMeta
		prevSaved = slices.Clone(e.cutscene.review.prevSaved)
		prevDirty = e.cutscene.review.prevDirty
	}

	base := req.Base
	if base == nil && (req.Path == "" || req.Path == e.cutscene.meta.path) {
		base = e.cutscene.doc
	}
	if base != nil {
		if err := cutscene.Validate(*base); err != nil {
			return err
		}
	} else {
		// New cutscene with no prior base on disk: empty timeline baseline of the same length.
		base = &cutscene.Cutscene{
			Version:   req.Proposed.Version,
			Kind:      req.Proposed.Kind,
			Scene:     req.Proposed.Scene,
			Length:    req.Proposed.Length,
			Group:     req.Proposed.Group,
			Skippable: req.Proposed.Skippable,
			SkipAfter: req.Proposed.SkipAfter,
		}
	}
	changes := req.Changes
	if len(changes) == 0 {
		changes = DiffCutsceneChanges(base, req.Proposed)
	} else {
		changes = slices.Clone(changes)
	}

	e.stopCutsceneLoop()
	e.cutscene.doc = cloneCutscene(base)
	if req.Path != "" {
		e.cutscene.meta.path = req.Path
	}
	if encoded, err := cutscene.Encode(*base); err == nil {
		e.cutscene.saved = encoded
	}
	e.cutscene.dirty = false
	e.cutscene.review = &cutsceneReviewState{
		prompt:    strings.TrimSpace(req.Prompt),
		base:      cloneCutscene(base),
		proposed:  cloneCutscene(req.Proposed),
		changes:   changes,
		view:      CutsceneReviewAfter,
		edits:     make(map[string]cutsceneClipEdit),
		prevDoc:   prevDoc,
		prevMeta:  prevMeta,
		prevSaved: prevSaved,
		prevDirty: prevDirty,
	}
	e.cancelAutoSaveTimer()
	e.cutscene.structRev++
	// Select a sensible default clip (first clip of the first change or first clip in doc).
	e.cutscene.selTrack, e.cutscene.selClip = -1, -1
	if eff := e.effectiveCutsceneDoc(); eff != nil {
		if len(changes) > 0 && changes[0].FocusTrack >= 0 && changes[0].FocusTrack < len(eff.Tracks) &&
			changes[0].FocusClip >= 0 && changes[0].FocusClip < len(eff.Tracks[changes[0].FocusTrack].Clips) {
			e.cutscene.selTrack = changes[0].FocusTrack
			e.cutscene.selClip = changes[0].FocusClip
		} else {
			e.selectFirstCutsceneClip(eff)
		}
	}
	e.refreshCutscenePreview()
	e.Status = fmt.Sprintf("Reviewing %d cutscene changes", len(changes))
	e.notify()
	return nil
}

// CutsceneReviewOpen reports whether a cutscene agent review is currently open.
func (e *Editor) CutsceneReviewOpen() bool {
	return e != nil && e.cutscene.review != nil
}

// CutsceneReviewPrompt returns the prompt string associated with the open cutscene review, or "".
func (e *Editor) CutsceneReviewPrompt() string {
	if e == nil || e.cutscene.review == nil {
		return ""
	}
	return e.cutscene.review.prompt
}

// CutsceneReviewChanges returns a copy of the changes in the open cutscene review.
func (e *Editor) CutsceneReviewChanges() []CutsceneChange {
	if e == nil || e.cutscene.review == nil {
		return nil
	}
	out := make([]CutsceneChange, len(e.cutscene.review.changes))
	for i, c := range e.cutscene.review.changes {
		c.ClipKeys = slices.Clone(c.ClipKeys)
		out[i] = c
	}
	return out
}

// CutsceneReviewKeptCount returns how many changes are currently kept out of total in the open review.
func (e *Editor) CutsceneReviewKeptCount() (kept, total int) {
	if e == nil || e.cutscene.review == nil {
		return 0, 0
	}
	total = len(e.cutscene.review.changes)
	for _, c := range e.cutscene.review.changes {
		if !c.Reverted {
			kept++
		}
	}
	return kept, total
}

// CutsceneReviewViewMode returns whether the review toggle is set to Before or After.
func (e *Editor) CutsceneReviewViewMode() CutsceneReviewView {
	if e == nil || e.cutscene.review == nil {
		return CutsceneReviewAfter
	}
	return e.cutscene.review.view
}

// SetCutsceneReviewViewMode switches the open review between Before and After.
func (e *Editor) SetCutsceneReviewViewMode(mode CutsceneReviewView) {
	if e == nil || e.cutscene.review == nil || e.cutscene.review.view == mode {
		return
	}
	e.cutscene.review.view = mode
	e.cutscene.structRev++
	e.refreshCutscenePreview()
	e.notify()
}

// CutsceneReviewPeekingBefore reports whether the user is holding B to peek at Before.
func (e *Editor) CutsceneReviewPeekingBefore() bool {
	return e != nil && e.cutscene.review != nil && e.cutscene.review.peekBefore
}

// SetCutsceneReviewPeekBefore sets the temporary hold-B peek state during an open review.
func (e *Editor) SetCutsceneReviewPeekBefore(peek bool) {
	if e == nil || e.cutscene.review == nil || e.cutscene.review.peekBefore == peek {
		return
	}
	e.cutscene.review.peekBefore = peek
	e.cutscene.structRev++
	e.refreshCutscenePreview()
	e.notify()
}

// CutsceneReviewShowingBefore reports whether the cutscene preview and timeline are currently displaying the Before state (either toggled to Before or peeking with B).
func (e *Editor) CutsceneReviewShowingBefore() bool {
	if e == nil || e.cutscene.review == nil {
		return false
	}
	return e.cutscene.review.view == CutsceneReviewBefore || e.cutscene.review.peekBefore
}

// ToggleCutsceneReviewChange toggles change index between kept (Revert button) and reverted (Restore button).
func (e *Editor) ToggleCutsceneReviewChange(index int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.cutscene.review == nil {
		return ErrNoCutsceneReview
	}
	if index < 0 || index >= len(e.cutscene.review.changes) {
		return fmt.Errorf("editor: cutscene review change %d out of range", index)
	}
	e.cutscene.review.changes[index].Reverted = !e.cutscene.review.changes[index].Reverted
	// Ensure the composed cutscene remains valid; if a manual edit now conflicts, validate cleanly.
	cand := composeCutsceneReview(e.cutscene.review.base, e.cutscene.review.proposed, e.cutscene.review.changes, e.cutscene.review.edits)
	if err := cutscene.Validate(*cand); err != nil {
		e.cutscene.review.changes[index].Reverted = !e.cutscene.review.changes[index].Reverted
		return err
	}
	e.cutscene.review.composed = cand
	e.cutscene.structRev++
	e.keepCutsceneSelectionValid(cand)
	e.refreshCutscenePreview()
	e.notify()
	return nil
}

// FocusCutsceneReviewChange selects the primary clip of change index and scrubs the playhead to its start time.
func (e *Editor) FocusCutsceneReviewChange(index int) {
	if e == nil || e.cutscene.review == nil {
		return
	}
	if index < 0 || index >= len(e.cutscene.review.changes) {
		return
	}
	ch := e.cutscene.review.changes[index]
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return
	}
	// Locate the track and focus clip in the effective document.
	for ti := range doc.Tracks {
		if trackIdentity(doc.Tracks, ti) != ch.TrackKey {
			continue
		}
		if len(doc.Tracks[ti].Clips) == 0 {
			break
		}
		ci := 0
		if len(ch.ClipKeys) > 0 {
			for k := range doc.Tracks[ti].Clips {
				if clipIdentity(doc.Tracks[ti], doc.Tracks[ti].Clips[k], k) == ch.ClipKeys[0] {
					ci = k
					break
				}
			}
		}
		e.cutscene.selTrack = ti
		e.cutscene.selClip = ci
		e.cutscene.structRev++
		e.SeekCutscene(time.Duration(doc.Tracks[ti].Clips[ci].Start))
		return
	}
	e.notify()
}

// CutsceneReviewHasManualEdits reports whether the person has edited any clip during the open review.
func (e *Editor) CutsceneReviewHasManualEdits() bool {
	return e != nil && e.cutscene.review != nil && len(e.cutscene.review.edits) > 0
}

// CutsceneClipManuallyEdited reports whether the clip at (trackIdx, clipIdx) has a manual edit joined to the open review.
func (e *Editor) CutsceneClipManuallyEdited(trackIdx, clipIdx int) bool {
	if e == nil || e.cutscene.review == nil {
		return false
	}
	doc := e.effectiveCutsceneDoc()
	if doc == nil || trackIdx < 0 || trackIdx >= len(doc.Tracks) || clipIdx < 0 || clipIdx >= len(doc.Tracks[trackIdx].Clips) {
		return false
	}
	key := trackIdentity(doc.Tracks, trackIdx) + "\x00" + clipIdentity(doc.Tracks[trackIdx], doc.Tracks[trackIdx].Clips[clipIdx], clipIdx)
	_, ok := e.cutscene.review.edits[key]
	return ok
}

// CutsceneClipReviewStatus returns whether the clip at (trackIdx, clipIdx) was added, changed, or unchanged in the active review view.
func (e *Editor) CutsceneClipReviewStatus(trackIdx, clipIdx int) CutsceneChangeKind {
	if e == nil || e.cutscene.review == nil || e.CutsceneReviewShowingBefore() {
		return CutsceneChangeNone
	}
	doc := e.effectiveCutsceneDoc()
	if doc == nil || trackIdx < 0 || trackIdx >= len(doc.Tracks) || clipIdx < 0 || clipIdx >= len(doc.Tracks[trackIdx].Clips) {
		return CutsceneChangeNone
	}
	tk := trackIdentity(doc.Tracks, trackIdx)
	ck := clipIdentity(doc.Tracks[trackIdx], doc.Tracks[trackIdx].Clips[clipIdx], clipIdx)
	if _, edited := e.cutscene.review.edits[tk+"\x00"+ck]; edited {
		return CutsceneChangeChanged
	}
	for _, ch := range e.cutscene.review.changes {
		if ch.Reverted || ch.TrackKey != tk {
			continue
		}
		if len(ch.ClipKeys) == 0 || slices.Contains(ch.ClipKeys, ck) {
			return ch.Kind
		}
	}
	return CutsceneChangeNone
}

// CutsceneChangeFocused reports whether change index owns the currently selected clip.
func (e *Editor) CutsceneChangeFocused(index int) bool {
	if e == nil || e.cutscene.review == nil || index < 0 || index >= len(e.cutscene.review.changes) {
		return false
	}
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return false
	}
	ti, ci := e.cutscene.selTrack, e.cutscene.selClip
	if ti < 0 || ti >= len(doc.Tracks) || ci < 0 || ci >= len(doc.Tracks[ti].Clips) {
		return false
	}
	ch := e.cutscene.review.changes[index]
	if trackIdentity(doc.Tracks, ti) != ch.TrackKey {
		return false
	}
	if len(ch.ClipKeys) == 0 {
		return true
	}
	ck := clipIdentity(doc.Tracks[ti], doc.Tracks[ti].Clips[ci], ci)
	return slices.Contains(ch.ClipKeys, ck)
}

// RevertAllCutsceneReview discards all pending review changes and manual review edits, closing the review and keeping the original cutscene untouched.
func (e *Editor) RevertAllCutsceneReview() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.cutscene.review == nil {
		return ErrNoCutsceneReview
	}
	rev := e.cutscene.review
	e.cutscene.review = nil
	if rev.prevMeta.path != e.cutscene.meta.path || rev.prevDoc == nil {
		e.cutscene.doc = cloneCutscene(rev.prevDoc)
		e.cutscene.meta = rev.prevMeta
		e.cutscene.saved = slices.Clone(rev.prevSaved)
		e.cutscene.dirty = rev.prevDirty
	} else {
		e.cutscene.doc = cloneCutscene(rev.base)
		e.updateCutsceneDirty()
	}
	e.cutscene.structRev++
	e.keepCutsceneSelectionValid(e.cutscene.doc)
	e.refreshCutscenePreview()
	e.scheduleAutoSaveIfNeeded()
	e.Status = "Reverted all cutscene changes"
	e.notify()
	return nil
}

// KeepCutsceneReview applies all kept review changes (and any manual edits joined to the review) in one undo step, writing the cutscene file to disk when CutscenePath is set and closing the review.
func (e *Editor) KeepCutsceneReview() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.cutscene.review == nil {
		return ErrNoCutsceneReview
	}
	rev := e.cutscene.review
	finalDoc := rev.composedDoc()
	if err := cutscene.Validate(*finalDoc); err != nil {
		return err
	}
	afterYAML, err := cutscene.Encode(*finalDoc)
	if err != nil {
		return err
	}
	baseYAML, err := cutscene.Encode(*rev.base)
	if err != nil {
		return err
	}

	kept, total := e.CutsceneReviewKeptCount()
	if reflect.DeepEqual(finalDoc, rev.base) {
		e.cutscene.review = nil
		if rev.prevMeta.path != e.cutscene.meta.path || rev.prevDoc == nil {
			e.cutscene.doc = cloneCutscene(rev.prevDoc)
			e.cutscene.meta = rev.prevMeta
			e.cutscene.saved = slices.Clone(rev.prevSaved)
			e.cutscene.dirty = rev.prevDirty
		} else {
			e.cutscene.doc = cloneCutscene(rev.base)
			e.updateCutsceneDirty()
		}
		e.cutscene.structRev++
		e.keepCutsceneSelectionValid(e.cutscene.doc)
		e.refreshCutscenePreview()
		e.scheduleAutoSaveIfNeeded()
		e.Status = fmt.Sprintf("Kept 0 of %d cutscene changes", total)
		e.notify()
		return nil
	}

	filePath := e.cutscene.meta.path
	var beforeDisk []byte
	var hadFile bool
	if filePath != "" {
		if err := e.checkCutsceneProjectSymlink(filePath, false); err != nil {
			return err
		}
		if raw, err := os.ReadFile(filePath); err == nil {
			beforeDisk = raw
			hadFile = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	beforeDoc := cloneCutscene(rev.base)
	beforeMeta := e.cutscene.meta
	beforeSaved := slices.Clone(e.cutscene.saved)
	if !hadFile && filePath != "" {
		// Brand-new cutscene created by this review: undoing removes the file and restores whatever was open before.
		beforeDoc = cloneCutscene(rev.prevDoc)
		beforeMeta = rev.prevMeta
		beforeSaved = slices.Clone(rev.prevSaved)
	} else if rev.prevMeta.path != filePath && rev.prevDoc != nil {
		// Review switched from a different open cutscene: undoing restores both disk and the previously open cutscene.
		beforeDoc = cloneCutscene(rev.prevDoc)
		beforeMeta = rev.prevMeta
		beforeSaved = slices.Clone(rev.prevSaved)
	}
	// Only mark saved bytes equal to afterYAML when written to disk; an in-memory cutscene stays dirty until saved.
	afterSaved := slices.Clone(e.cutscene.saved)
	if filePath != "" {
		afterSaved = afterYAML
	}

	label := fmt.Sprintf("Keep %d of %d cutscene changes", kept, total)
	if rev.prompt != "" {
		label = fmt.Sprintf("Keep %d of %d cutscene changes (%s)", kept, total, rev.prompt)
	}
	cmd := &cutsceneCommitCmd{
		ed:          e,
		label:       label,
		path:        filePath,
		beforeMeta:  beforeMeta,
		afterMeta:   cutsceneMeta{path: filePath},
		beforeDoc:   beforeDoc,
		afterDoc:    cloneCutscene(finalDoc),
		beforeDisk:  beforeDisk,
		afterDisk:   afterYAML,
		hadFile:     hadFile,
		beforeSaved: beforeSaved,
		afterSaved:  afterSaved,
		baseYAML:    baseYAML,
	}
	summary := rev.prompt
	if summary == "" {
		summary = "cutscene edits"
	}
	var touched []string
	if filePath != "" {
		touched = []string{e.displayHistoryPath(filePath)}
	}
	prevMeta := e.historyPanel.pendingMeta
	e.historyPanel.pendingMeta = &HistoryMeta{
		Author:      HistoryAuthorAgent,
		Summary:     summary,
		Detail:      formatAgentTurnDetail("", total, kept),
		Files:       touched,
		ChangeCount: total,
		KeptCount:   kept,
	}
	err = e.History.execute(cmd)
	e.historyPanel.pendingMeta = prevMeta
	return err
}

// CutsceneHandleKeyEvent handles cutscene timeline keyboard shortcuts:
// hold B to peek Before during an open review, Space to play/pause, Left/Right arrows to scrub ±0.1 s (Shift ±0.5 s).
// It ignores events during Play mode and never swallows modifier chords (Cmd/Ctrl/Alt).
func (e *Editor) CutsceneHandleKeyEvent(ev core.KeyEvent, down bool) bool {
	if e == nil || e.playing || e.cutscene.doc == nil {
		return false
	}
	if ev.Modifiers.Platform || ev.Modifiers.Control || ev.Modifiers.Alt {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(ev.Key))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(ev.Code))
	}
	switch key {
	case "b", "keyb":
		if e.cutscene.review == nil {
			return false
		}
		e.SetCutsceneReviewPeekBefore(down)
		return true
	case " ", "space":
		if !down {
			return true
		}
		if e.cutscene.playing {
			e.PauseCutscene()
		} else {
			e.report(e.PlayCutscene())
		}
		return true
	case "arrowleft", "left":
		if !down {
			return true
		}
		step := 100 * time.Millisecond
		if ev.Modifiers.Shift {
			step = 500 * time.Millisecond
		}
		e.SeekCutscene(e.cutscene.time - step)
		return true
	case "arrowright", "right":
		if !down {
			return true
		}
		step := 100 * time.Millisecond
		if ev.Modifiers.Shift {
			step = 500 * time.Millisecond
		}
		e.SeekCutscene(e.cutscene.time + step)
		return true
	default:
		return false
	}
}

func (e *Editor) canUndoCutsceneReviewEdit() bool {
	return e != nil && e.cutscene.review != nil && len(e.cutscene.review.editUndo) > 0
}

func (e *Editor) canRedoCutsceneReviewEdit() bool {
	return e != nil && e.cutscene.review != nil && len(e.cutscene.review.editRedo) > 0
}

func (e *Editor) pushCutsceneReviewEdit(next map[string]cutsceneClipEdit) {
	rev := e.cutscene.review
	if rev == nil {
		return
	}
	rev.editUndo = append(rev.editUndo, cloneClipEdits(rev.edits))
	rev.editRedo = nil
	rev.edits = next
	rev.invalidateComposed()
}

func (e *Editor) undoCutsceneReviewEdit() bool {
	if !e.canUndoCutsceneReviewEdit() {
		return false
	}
	rev := e.cutscene.review
	last := rev.editUndo[len(rev.editUndo)-1]
	rev.editUndo = rev.editUndo[:len(rev.editUndo)-1]
	rev.editRedo = append(rev.editRedo, cloneClipEdits(rev.edits))
	rev.edits = last
	rev.invalidateComposed()
	e.cutscene.structRev++
	e.keepCutsceneSelectionValid(e.effectiveCutsceneDoc())
	e.refreshCutscenePreview()
	e.Status = "Undo cutscene review edit"
	e.notify()
	return true
}

func (e *Editor) redoCutsceneReviewEdit() bool {
	if !e.canRedoCutsceneReviewEdit() {
		return false
	}
	rev := e.cutscene.review
	last := rev.editRedo[len(rev.editRedo)-1]
	rev.editRedo = rev.editRedo[:len(rev.editRedo)-1]
	rev.editUndo = append(rev.editUndo, cloneClipEdits(rev.edits))
	rev.edits = last
	rev.invalidateComposed()
	e.cutscene.structRev++
	e.keepCutsceneSelectionValid(e.effectiveCutsceneDoc())
	e.refreshCutscenePreview()
	e.Status = "Redo cutscene review edit"
	e.notify()
	return true
}

// ProjectCutscenes returns all *.cutscene.yaml / *.cutscene.yml files discovered in the open project (plus the currently open cutscene if it has no project file), with name, kind, length and review status.
// Disk scanning is cached by (ProjectPath, filesRev) so high-frequency timeline playback never walks or parses project files on disk.
func (e *Editor) ProjectCutscenes() []CutsceneEntry {
	if e == nil {
		return nil
	}
	openPath := ""
	if e.cutscene.meta.path != "" {
		openPath = e.cutscene.meta.path
		if e.ProjectPath != "" {
			if rel, err := filepath.Rel(filepath.Dir(e.ProjectPath), openPath); err == nil && !strings.HasPrefix(rel, "..") {
				openPath = filepath.ToSlash(rel)
			}
		}
	}

	if e.Project != nil && e.ProjectPath != "" {
		if !e.cutscene.projCutLoaded || e.cutscene.projCutPath != e.ProjectPath || e.cutscene.projCutFilesRev != e.filesRev {
			var cached []CutsceneEntry
			root := filepath.Dir(e.ProjectPath)
			if files, err := e.ProjectFiles(); err == nil {
				for _, f := range files {
					if f.Dir || !isCutsceneFile(f.Path) {
						continue
					}
					entry := CutsceneEntry{
						Path: f.Path,
						Name: cutsceneDisplayName(f.Path),
					}
					if raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path))); err == nil {
						if parsed, err := cutscene.Parse(raw); err == nil {
							entry.Kind = parsed.EffectiveKind()
							entry.Length = time.Duration(parsed.Length)
						}
					}
					cached = append(cached, entry)
				}
			}
			e.cutscene.projCutscenes = cached
			e.cutscene.projCutPath = e.ProjectPath
			e.cutscene.projCutFilesRev = e.filesRev
			e.cutscene.projCutLoaded = true
		}
	} else {
		e.cutscene.projCutscenes = nil
		e.cutscene.projCutPath = ""
		e.cutscene.projCutLoaded = false
	}

	out := make([]CutsceneEntry, 0, len(e.cutscene.projCutscenes)+1)
	seen := make(map[string]bool, len(e.cutscene.projCutscenes))
	for _, entry := range e.cutscene.projCutscenes {
		seen[entry.Path] = true
		entry.InReview = e.cutscene.review != nil && entry.Path == openPath
		if entry.Path == openPath && e.cutscene.doc != nil {
			eff := e.effectiveCutsceneDoc()
			entry.Kind = eff.EffectiveKind()
			entry.Length = time.Duration(eff.Length)
		}
		out = append(out, entry)
	}
	if e.cutscene.doc != nil && (openPath == "" || !seen[openPath]) {
		eff := e.effectiveCutsceneDoc()
		name := "untitled"
		if openPath != "" {
			name = cutsceneDisplayName(openPath)
		}
		out = append([]CutsceneEntry{{
			Path:     openPath,
			Name:     name,
			Kind:     eff.EffectiveKind(),
			Length:   time.Duration(eff.Length),
			InReview: e.cutscene.review != nil,
		}}, out...)
	}
	return out
}

// CutsceneLocPaths returns the display paths for English and Chinese localization tables (defaulting to "loc/en.yaml" and "loc/zh.yaml").
func (e *Editor) CutsceneLocPaths() (enPath, zhPath string) {
	enPath, zhPath = "loc/en.yaml", "loc/zh.yaml"
	if e != nil {
		if e.cutscene.locEnPath != "" {
			enPath = e.cutscene.locEnPath
		}
		if e.cutscene.locZhPath != "" {
			zhPath = e.cutscene.locZhPath
		}
	}
	return enPath, zhPath
}

// CutsceneLineText resolves the English and Chinese text for a dialogue line ID from the localization catalog.
func (e *Editor) CutsceneLineText(id string) (en, zh string) {
	if e == nil || id == "" {
		return "", ""
	}
	cat := e.effectiveCutsceneCatalog()
	if cat == nil {
		return "", ""
	}
	if tbl, ok := cat.StringTable("en"); ok {
		if t, ok := tbl.Strings[id]; ok {
			en = t
		}
	}
	for _, loc := range []string{"zh", "zh-Hans", "zh-CN"} {
		if tbl, ok := cat.StringTable(loc); ok {
			if t, ok := tbl.Strings[id]; ok {
				zh = t
				break
			}
		}
	}
	return en, zh
}

// CutsceneLines returns the localized dialogue lines referenced by the open cutscene (or its active review), each with its ID, English text, Chinese text, and whether it was added by the open review.
func (e *Editor) CutsceneLines() []CutsceneLineEntry {
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		return nil
	}
	var out []CutsceneLineEntry
	seen := make(map[string]bool)
	for ti := range doc.Tracks {
		tr := &doc.Tracks[ti]
		if tr.Kind != cutscene.TrackDialogue {
			continue
		}
		for ci := range tr.Clips {
			id := tr.Clips[ci].Line
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			en, zh := e.CutsceneLineText(id)
			added := e.CutsceneClipReviewStatus(ti, ci) == CutsceneChangeAdded
			out = append(out, CutsceneLineEntry{
				ID:    id,
				En:    en,
				Zh:    zh,
				Added: added,
			})
		}
	}
	// If in Before mode during a review, also include lines that exist in Proposed so the left rail remains informative.
	if e.cutscene.review != nil && e.CutsceneReviewShowingBefore() && e.cutscene.review.proposed != nil {
		for ti := range e.cutscene.review.proposed.Tracks {
			tr := &e.cutscene.review.proposed.Tracks[ti]
			if tr.Kind != cutscene.TrackDialogue {
				continue
			}
			for ci := range tr.Clips {
				id := tr.Clips[ci].Line
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				en, zh := e.CutsceneLineText(id)
				out = append(out, CutsceneLineEntry{
					ID: id,
					En: en,
					Zh: zh,
				})
			}
		}
	}
	return out
}

func (e *Editor) effectiveCutsceneCatalog() *l10n.Catalog {
	if e == nil {
		return nil
	}
	if e.CutsceneCatalog != nil {
		return e.CutsceneCatalog
	}
	if e.l10n != nil && len(e.l10n.locales) > 0 {
		cat := l10n.NewCatalog("en")
		for _, loc := range e.l10n.locales {
			for k, v := range e.l10n.strings[loc] {
				if v != "" {
					cat.Set(loc, k, v)
				}
			}
		}
		return cat
	}
	if e.cutscene.locCatalog != nil {
		return e.cutscene.locCatalog
	}
	return nil
}

func (e *Editor) loadProjectCutsceneCatalog() {
	if e.ProjectPath == "" {
		e.cutscene.locCatalog = nil
		e.cutscene.locEnPath = ""
		e.cutscene.locZhPath = ""
		e.cutscene.locProjectPath = ""
		e.cutscene.locLoaded = false
		return
	}
	if e.cutscene.locLoaded && e.cutscene.locProjectPath == e.ProjectPath && e.cutscene.locFilesRev == e.filesRev {
		return
	}
	e.cutscene.locCatalog = nil
	e.cutscene.locEnPath = ""
	e.cutscene.locZhPath = ""
	e.cutscene.locProjectPath = e.ProjectPath
	e.cutscene.locFilesRev = e.filesRev
	e.cutscene.locLoaded = true
	root := filepath.Dir(e.ProjectPath)
	cat := l10n.NewCatalog("en")
	loaded := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			return nil
		}
		if strings.HasSuffix(lower, ".scene.yaml") || strings.HasSuffix(lower, ".scene.yml") ||
			strings.HasSuffix(lower, ".cutscene.yaml") || strings.HasSuffix(lower, ".cutscene.yml") ||
			strings.HasSuffix(lower, ".states.yaml") || strings.HasSuffix(lower, ".states.yml") ||
			strings.HasSuffix(lower, ".animation.yaml") || strings.HasSuffix(lower, ".animation.yml") ||
			lower == "game.yaml" || lower == "nsgo.yaml" {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		tbl, err := l10n.ParseYAML(raw)
		if err != nil || tbl == nil {
			return nil
		}
		loaded++
		for k, v := range tbl.Strings {
			cat.Set(tbl.Locale, k, v)
		}
		if rel, err := filepath.Rel(root, p); err == nil {
			slashed := filepath.ToSlash(rel)
			norm := strings.ToLower(l10n.NormalizeLocale(tbl.Locale))
			if norm == "en" || strings.HasPrefix(norm, "en-") {
				if e.cutscene.locEnPath == "" {
					e.cutscene.locEnPath = slashed
				}
			}
			if norm == "zh" || strings.HasPrefix(norm, "zh-") {
				if e.cutscene.locZhPath == "" {
					e.cutscene.locZhPath = slashed
				}
			}
		}
		return nil
	})
	if loaded > 0 {
		e.cutscene.locCatalog = cat
	}
}

func (e *Editor) effectiveCutsceneDoc() *cutscene.Cutscene {
	if e == nil || e.cutscene.doc == nil {
		return nil
	}
	if e.cutscene.review != nil {
		if e.CutsceneReviewShowingBefore() {
			return e.cutscene.review.base
		}
		return e.cutscene.review.composedDoc()
	}
	return e.cutscene.doc
}

func (e *Editor) refreshCutscenePreview() {
	e.cutscene.scene, e.cutscene.player, e.cutscene.err = nil, nil, nil
	e.cutscene.thumbs = nil
	e.cutscene.structRev++
	if e.cutscene.doc == nil || e.playing {
		e.stopCutsceneLoop()
		return
	}
	e.loadProjectCutsceneCatalog()
	doc := e.effectiveCutsceneDoc()
	if doc == nil {
		e.stopCutsceneLoop()
		return
	}
	e.keepCutsceneSelectionValid(doc)
	if doc.EffectiveKind() != cutscene.KindTimeline {
		e.stopCutsceneLoop()
		return
	}
	length := time.Duration(doc.Length)
	if e.cutscene.time > length {
		e.cutscene.time = length
	}
	if e.cutscene.time < 0 {
		e.cutscene.time = 0
	}

	sceneDoc, err := e.cutsceneTargetSceneDoc(doc)
	if err != nil {
		e.cutscene.err = err
		e.stopCutsceneLoop()
		return
	}
	if sceneDoc == nil {
		e.cutscene.err = errors.New("editor: no scene available for cutscene preview")
		e.stopCutsceneLoop()
		return
	}

	// Build a fresh isolated BuiltScene for the timeline preview.
	encoded, err := sceneformat.Encode(sceneDoc)
	if err != nil {
		e.cutscene.err = err
		e.stopCutsceneLoop()
		return
	}
	parsedScene, err := sceneformat.Parse(encoded)
	if err != nil {
		e.cutscene.err = err
		e.stopCutsceneLoop()
		return
	}
	if parsedScene.RefersToScenes() {
		if scenes, ok := e.Resolver.(sceneformat.SceneResolver); ok {
			if resolved, rerr := sceneformat.Resolve(parsedScene, scenes); rerr == nil {
				parsedScene = resolved
			}
		}
	}
	built, err := sceneformat.Build(parsedScene, e.Resolver)
	if err != nil {
		e.cutscene.err = err
		e.stopCutsceneLoop()
		return
	}
	cat := e.effectiveCutsceneCatalog()
	player, err := cutscene.NewTimelinePlayer(*doc, cutscene.TimelineTarget{
		Scene:   built,
		Catalog: cat,
		Locale:  "en",
	})
	if err != nil {
		e.cutscene.err = err
		e.stopCutsceneLoop()
		return
	}
	player.Seek(e.cutscene.time)
	e.cutscene.scene = built
	e.cutscene.player = player

	// Sample 24 thumbnail poses cleanly on a separate SceneDoc copy without touching the live preview BuiltScene.
	if thumbScene, err := sceneformat.Parse(encoded); err == nil {
		if thumbScene.RefersToScenes() {
			if scenes, ok := e.Resolver.(sceneformat.SceneResolver); ok {
				if resolved, rerr := sceneformat.Resolve(thumbScene, scenes); rerr == nil {
					thumbScene = resolved
				}
			}
		}
		if thumbPlayer, err := cutscene.NewTimelinePlayer(*doc, cutscene.TimelineTarget{Doc: thumbScene}); err == nil {
			var actorIDs []string
			for _, tr := range doc.Tracks {
				if tr.Kind == cutscene.TrackActor && tr.Target != "" {
					actorIDs = append(actorIDs, tr.Target)
				}
			}
			thumbs := make([]CutsceneThumbSample, cutsceneThumbCount)
			for i := range cutsceneThumbCount {
				tt := time.Duration(float64(i) / float64(cutsceneThumbCount) * float64(length))
				thumbPlayer.Seek(tt)
				cx, cy, cz, _ := thumbPlayer.CameraState()
				actors := make([]CutsceneThumbActor, 0, len(actorIDs))
				for _, id := range actorIDs {
					if ax, ay, ok := thumbPlayer.ActorPosition(id); ok {
						actors = append(actors, CutsceneThumbActor{ID: id, X: ax, Y: ay})
					}
				}
				thumbs[i] = CutsceneThumbSample{
					Time:   tt,
					CamX:   cx,
					CamY:   cy,
					Zoom:   cz,
					Actors: actors,
				}
			}
			e.cutscene.thumbs = thumbs
		}
	}
}

func (e *Editor) cutsceneTargetSceneDoc(c *cutscene.Cutscene) (*sceneformat.SceneDoc, error) {
	// If the cutscene names a project scene file different from the open scene, prefer loading it when it exists and e.Doc lacks the cutscene's actors.
	if c != nil && c.Scene != "" && e.Project != nil && e.ProjectPath != "" && c.Scene != e.ScenePath {
		if e.Doc == nil || !sceneHasCutsceneTargets(e.Doc, c) {
			abs, err := e.projectFile(c.Scene)
			if err != nil {
				return nil, fmt.Errorf("cutscene scene %s: %w", c.Scene, err)
			}
			if err := e.checkCutsceneProjectSymlink(abs, true); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("cutscene scene %s: %w", c.Scene, err)
				}
				return e.Doc, nil
			}
			if raw, err := os.ReadFile(abs); err == nil {
				parsed, perr := sceneformat.ParseWith(raw, sceneformat.ParseOptions{Components: e.components})
				if perr != nil {
					return nil, fmt.Errorf("cutscene scene %s: %w", c.Scene, perr)
				}
				return parsed, nil
			}
		}
	}
	return e.Doc, nil
}

func sceneHasCutsceneTargets(doc *sceneformat.SceneDoc, c *cutscene.Cutscene) bool {
	if doc == nil || c == nil {
		return false
	}
	ids := make(map[string]bool)
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			ids[o.ID] = true
			return true
		})
	}
	for _, tr := range c.Tracks {
		if tr.Kind == cutscene.TrackActor && !ids[tr.Target] {
			return false
		}
		if tr.Kind == cutscene.TrackCamera {
			for _, cl := range tr.Clips {
				if cl.Target != "" && !ids[cl.Target] {
					return false
				}
			}
		}
	}
	return true
}

func (e *Editor) selectFirstCutsceneClip(doc *cutscene.Cutscene) {
	e.cutscene.selTrack, e.cutscene.selClip = -1, -1
	if doc == nil {
		return
	}
	for ti := range doc.Tracks {
		if len(doc.Tracks[ti].Clips) > 0 {
			e.cutscene.selTrack = ti
			e.cutscene.selClip = 0
			return
		}
	}
}

func (e *Editor) keepCutsceneSelectionValid(doc *cutscene.Cutscene) {
	if doc == nil {
		e.cutscene.selTrack, e.cutscene.selClip = -1, -1
		return
	}
	if e.cutscene.selTrack >= 0 && e.cutscene.selTrack < len(doc.Tracks) &&
		e.cutscene.selClip >= 0 && e.cutscene.selClip < len(doc.Tracks[e.cutscene.selTrack].Clips) {
		return
	}
	e.selectFirstCutsceneClip(doc)
}

func (e *Editor) selectClipByKey(doc *cutscene.Cutscene, tk, ck string) {
	if doc == nil {
		return
	}
	for ti := range doc.Tracks {
		if trackIdentity(doc.Tracks, ti) != tk {
			continue
		}
		for ci := range doc.Tracks[ti].Clips {
			if clipIdentity(doc.Tracks[ti], doc.Tracks[ti].Clips[ci], ci) == ck {
				e.cutscene.selTrack = ti
				e.cutscene.selClip = ci
				return
			}
		}
	}
}

func (e *Editor) resetCutsceneState() {
	e.stopCutsceneLoop()
	e.cutscene = cutsceneEditorState{selTrack: -1, selClip: -1}
}

// isCutsceneFile reports whether relPath is a cutscene file (*.cutscene.yaml or *.cutscene.yml).
func isCutsceneFile(relPath string) bool {
	lower := strings.ToLower(relPath)
	return strings.HasSuffix(lower, ".cutscene.yaml") || strings.HasSuffix(lower, ".cutscene.yml")
}

func cutsceneDisplayName(relPath string) string {
	base := path.Base(strings.ReplaceAll(relPath, `\`, "/"))
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, ".cutscene.yaml"):
		return base[:len(base)-len(".cutscene.yaml")]
	case strings.HasSuffix(lower, ".cutscene.yml"):
		return base[:len(base)-len(".cutscene.yml")]
	default:
		return strings.TrimSuffix(base, path.Ext(base))
	}
}

// DiffCutsceneChanges computes the ordered, independently revertible changes between before and after cutscenes.
// Adjacent or overlapping timing edits on the same non-sound track are grouped into one atomic change so reverting any subset always leaves a valid, non-overlapping cutscene.
func DiffCutsceneChanges(before, after *cutscene.Cutscene) []CutsceneChange {
	if before == nil || after == nil {
		return nil
	}
	var changes []CutsceneChange

	// Check top-level metadata differences.
	if before.Length != after.Length || before.Scene != after.Scene || before.Skippable != after.Skippable || before.SkipAfter != after.SkipAfter || before.Group != after.Group {
		detail := ""
		if before.Length != after.Length {
			detail = fmt.Sprintf("length %s → %s", formatCutsceneSec(time.Duration(before.Length)), formatCutsceneSec(time.Duration(after.Length)))
		} else {
			detail = "cutscene settings updated"
		}
		changes = append(changes, CutsceneChange{
			ID:         "meta",
			Kind:       CutsceneChangeChanged,
			Sign:       "~",
			Title:      "Cutscene settings changed",
			Detail:     detail,
			MetaChange: true,
			FocusTrack: -1,
			FocusClip:  -1,
		})
	}

	beforeTracks := make(map[string]int, len(before.Tracks))
	for i := range before.Tracks {
		beforeTracks[trackIdentity(before.Tracks, i)] = i
	}
	afterTracks := make(map[string]int, len(after.Tracks))
	for i := range after.Tracks {
		afterTracks[trackIdentity(after.Tracks, i)] = i
	}

	for ati := range after.Tracks {
		atr := after.Tracks[ati]
		tk := trackIdentity(after.Tracks, ati)
		bti, existed := beforeTracks[tk]
		if !existed {
			clipKeys := make([]string, len(atr.Clips))
			for ci := range atr.Clips {
				clipKeys[ci] = clipIdentity(atr, atr.Clips[ci], ci)
			}
			title, detail := describeAddedTrack(atr)
			changes = append(changes, CutsceneChange{
				ID:         "track-add-" + tk,
				Kind:       CutsceneChangeAdded,
				Sign:       "+",
				Title:      title,
				Detail:     detail,
				TrackKey:   tk,
				ClipKeys:   clipKeys,
				FocusTrack: ati,
				FocusClip:  0,
			})
			continue
		}

		btr := before.Tracks[bti]
		trackChanges := diffTrackClips(tk, ati, btr, atr)
		changes = append(changes, trackChanges...)
	}

	for bti := range before.Tracks {
		tk := trackIdentity(before.Tracks, bti)
		if _, exists := afterTracks[tk]; !exists {
			btr := before.Tracks[bti]
			clipKeys := make([]string, len(btr.Clips))
			for ci := range btr.Clips {
				clipKeys[ci] = clipIdentity(btr, btr.Clips[ci], ci)
			}
			changes = append(changes, CutsceneChange{
				ID:         "track-rem-" + tk,
				Kind:       CutsceneChangeRemoved,
				Sign:       "-",
				Title:      fmt.Sprintf("%s track removed", trackDisplayName(btr)),
				Detail:     fmt.Sprintf("%d clips removed", len(btr.Clips)),
				TrackKey:   tk,
				ClipKeys:   clipKeys,
				FocusTrack: -1,
				FocusClip:  -1,
			})
		}
	}
	return changes
}

type clipDiffItem struct {
	key        string
	kind       CutsceneChangeKind
	before     *cutscene.Clip
	after      *cutscene.Clip
	afterIndex int
	minStart   time.Duration
	maxEnd     time.Duration
}

func diffTrackClips(tk string, ati int, btr, atr cutscene.Track) []CutsceneChange {
	bMap := make(map[string]cutscene.Clip, len(btr.Clips))
	for i, cl := range btr.Clips {
		bMap[clipIdentity(btr, cl, i)] = cl
	}
	aMap := make(map[string]int, len(atr.Clips))
	for i, cl := range atr.Clips {
		aMap[clipIdentity(atr, cl, i)] = i
	}

	var items []clipDiffItem
	for i, acl := range atr.Clips {
		ck := clipIdentity(atr, acl, i)
		bcl, ok := bMap[ck]
		if !ok {
			st := time.Duration(acl.Start)
			en := st + time.Duration(acl.Duration)
			aclCopy := cloneCutsceneClip(acl)
			items = append(items, clipDiffItem{
				key:        ck,
				kind:       CutsceneChangeAdded,
				after:      &aclCopy,
				afterIndex: i,
				minStart:   st,
				maxEnd:     en,
			})
			continue
		}
		if !clipsEqual(bcl, acl) {
			bCopy := cloneCutsceneClip(bcl)
			aCopy := cloneCutsceneClip(acl)
			bs, be := time.Duration(bcl.Start), time.Duration(bcl.Start)+time.Duration(bcl.Duration)
			as, ae := time.Duration(acl.Start), time.Duration(acl.Start)+time.Duration(acl.Duration)
			items = append(items, clipDiffItem{
				key:        ck,
				kind:       CutsceneChangeChanged,
				before:     &bCopy,
				after:      &aCopy,
				afterIndex: i,
				minStart:   min(bs, as),
				maxEnd:     max(be, ae),
			})
		}
	}
	for i, bcl := range btr.Clips {
		ck := clipIdentity(btr, bcl, i)
		if _, ok := aMap[ck]; !ok {
			bCopy := cloneCutsceneClip(bcl)
			bs, be := time.Duration(bcl.Start), time.Duration(bcl.Start)+time.Duration(bcl.Duration)
			items = append(items, clipDiffItem{
				key:        ck,
				kind:       CutsceneChangeRemoved,
				before:     &bCopy,
				afterIndex: -1,
				minStart:   bs,
				maxEnd:     be,
			})
		}
	}
	if len(items) == 0 {
		return nil
	}

	// Group clips on non-sound tracks whose before/after intervals overlap, so reverting any single group can never violate cutscene.Validate's non-overlap constraint.
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			if ra < rb {
				parent[rb] = ra
			} else {
				parent[ra] = rb
			}
		}
	}
	if atr.Kind != cutscene.TrackSound {
		for i := range items {
			for j := i + 1; j < len(items); j++ {
				if items[i].minStart < items[j].maxEnd && items[j].minStart < items[i].maxEnd {
					union(i, j)
				}
			}
		}
	}

	groupOrder := make([]int, 0, len(items))
	groups := make(map[int][]clipDiffItem)
	for i, it := range items {
		r := find(i)
		if _, seen := groups[r]; !seen {
			groupOrder = append(groupOrder, r)
		}
		groups[r] = append(groups[r], it)
	}

	out := make([]CutsceneChange, 0, len(groupOrder))
	for _, r := range groupOrder {
		grp := groups[r]
		kind := grp[0].kind
		for _, it := range grp[1:] {
			if it.kind != kind {
				kind = CutsceneChangeChanged
				break
			}
		}
		sign := "~"
		switch kind {
		case CutsceneChangeAdded:
			sign = "+"
		case CutsceneChangeRemoved:
			sign = "-"
		}
		clipKeys := make([]string, len(grp))
		focusClip := -1
		for i, it := range grp {
			clipKeys[i] = it.key
			if focusClip < 0 && it.afterIndex >= 0 {
				focusClip = it.afterIndex
			}
		}
		title, detail := describeClipChangeGroup(atr, grp, kind)
		out = append(out, CutsceneChange{
			ID:         fmt.Sprintf("%s:%s", tk, clipKeys[0]),
			Kind:       kind,
			Sign:       sign,
			Title:      title,
			Detail:     detail,
			TrackKey:   tk,
			ClipKeys:   clipKeys,
			FocusTrack: ati,
			FocusClip:  focusClip,
		})
	}
	return out
}

func describeAddedTrack(tr cutscene.Track) (title, detail string) {
	name := trackDisplayName(tr)
	if len(tr.Clips) == 0 {
		return name + " track added", "empty track"
	}
	first := tr.Clips[0]
	if tr.Kind == cutscene.TrackActor && first.Action == cutscene.ActionDrop {
		title = name + " drops into scene"
		detail = "at " + formatCutsceneSec(time.Duration(first.Start))
		if len(tr.Clips) > 1 {
			detail += ", then " + strings.ToLower(cutsceneClipDisplayLabel(tr.Kind, tr.Clips[1]))
		}
		return title, detail
	}
	title = fmt.Sprintf("%s: %s", name, cutsceneClipDisplayLabel(tr.Kind, first))
	detail = fmt.Sprintf("at %s (%d clips)", formatCutsceneSec(time.Duration(first.Start)), len(tr.Clips))
	return title, detail
}

func describeClipChangeGroup(tr cutscene.Track, grp []clipDiffItem, kind CutsceneChangeKind) (title, detail string) {
	first := grp[0]
	switch kind {
	case CutsceneChangeAdded:
		cl := *first.after
		switch tr.Kind {
		case cutscene.TrackDialogue:
			return "Line " + cl.Line, "at " + formatCutsceneSec(time.Duration(cl.Start))
		case cutscene.TrackSound:
			bus := cl.Group
			if bus == "" {
				bus = tr.Group
			}
			if bus == "" {
				bus = "sfx"
			}
			return cutsceneClipDisplayLabel(tr.Kind, cl) + " sound", fmt.Sprintf("at %s, bus %s", formatCutsceneSec(time.Duration(cl.Start)), bus)
		case cutscene.TrackMusic:
			return cutsceneClipDisplayLabel(tr.Kind, cl) + " music", "at " + formatCutsceneSec(time.Duration(cl.Start))
		default:
			return fmt.Sprintf("%s: %s", trackDisplayName(tr), cutsceneClipDisplayLabel(tr.Kind, cl)), "at " + formatCutsceneSec(time.Duration(cl.Start))
		}
	case CutsceneChangeRemoved:
		cl := *first.before
		return fmt.Sprintf("Removed %s", cutsceneClipDisplayLabel(tr.Kind, cl)), "was at " + formatCutsceneSec(time.Duration(cl.Start))
	default:
		if tr.Kind == cutscene.TrackCamera && first.before != nil && first.after != nil {
			bDur := time.Duration(first.before.Duration)
			aDur := time.Duration(first.after.Duration)
			if len(grp) > 1 && bDur != aDur {
				return fmt.Sprintf("Camera %s retimed", strings.ToLower(cutsceneClipDisplayLabel(tr.Kind, *first.after))),
					fmt.Sprintf("%s → %s, %d clips adjusted", formatCutsceneSec(bDur), formatCutsceneSec(aDur), len(grp))
			}
		}
		if tr.Kind == cutscene.TrackMusic && first.before != nil && first.after != nil {
			if first.after.FadeIn > 0 && first.before.FadeIn == 0 {
				return fmt.Sprintf("Music fades in at %s", formatCutsceneSec(time.Duration(first.after.Start))),
					fmt.Sprintf("was full volume from %s", formatCutsceneSec(time.Duration(first.before.Start)))
			}
		}
		if first.before != nil && first.after != nil {
			return fmt.Sprintf("%s: %s", trackDisplayName(tr), cutsceneClipDisplayLabel(tr.Kind, *first.after)),
				fmt.Sprintf("%s → %s", formatCutsceneSec(time.Duration(first.before.Start)), formatCutsceneSec(time.Duration(first.after.Start)))
		}
		return fmt.Sprintf("%s updated", trackDisplayName(tr)), fmt.Sprintf("%d clips changed", len(grp))
	}
}

func composeCutsceneReview(base, proposed *cutscene.Cutscene, changes []CutsceneChange, edits map[string]cutsceneClipEdit) *cutscene.Cutscene {
	if base == nil {
		return cloneCutscene(proposed)
	}
	if proposed == nil {
		return cloneCutscene(base)
	}

	out := cloneCutscene(base)
	out.Tracks = nil

	// Check if top-level metadata change is kept.
	for _, ch := range changes {
		if ch.MetaChange && !ch.Reverted {
			out.Length = proposed.Length
			out.Scene = proposed.Scene
			out.Skippable = proposed.Skippable
			out.SkipAfter = proposed.SkipAfter
			out.Group = proposed.Group
		}
	}

	// Build change lookup by track and clip key.
	trackChange := make(map[string]*CutsceneChange)
	clipChange := make(map[string]*CutsceneChange)
	for i := range changes {
		ch := &changes[i]
		if ch.MetaChange {
			continue
		}
		if len(ch.ClipKeys) == 0 {
			trackChange[ch.TrackKey] = ch
			continue
		}
		for _, ck := range ch.ClipKeys {
			clipChange[ch.TrackKey+"\x00"+ck] = ch
		}
		// Track-level add/remove records all its clip keys as well as TrackKey.
		if strings.HasPrefix(ch.ID, "track-add-") || strings.HasPrefix(ch.ID, "track-rem-") {
			trackChange[ch.TrackKey] = ch
		}
	}

	beforeByKey := make(map[string]cutscene.Track, len(base.Tracks))
	for i := range base.Tracks {
		beforeByKey[trackIdentity(base.Tracks, i)] = base.Tracks[i]
	}
	afterByKey := make(map[string]cutscene.Track, len(proposed.Tracks))
	for i := range proposed.Tracks {
		afterByKey[trackIdentity(proposed.Tracks, i)] = proposed.Tracks[i]
	}

	// Walk proposed tracks first to preserve proposed ordering, then any removed base tracks whose removal was reverted.
	usedTracks := make(map[string]bool)
	for i := range proposed.Tracks {
		tk := trackIdentity(proposed.Tracks, i)
		usedTracks[tk] = true
		atr := proposed.Tracks[i]
		btr, existed := beforeByKey[tk]
		if !existed {
			if ch := trackChange[tk]; ch != nil && ch.Reverted {
				continue
			}
			tr := cloneCutsceneTrack(atr)
			applyManualClipEdits(&tr, tk, edits)
			out.Tracks = append(out.Tracks, tr)
			continue
		}
		tr := composeTrack(tk, btr, atr, clipChange)
		applyManualClipEdits(&tr, tk, edits)
		out.Tracks = append(out.Tracks, tr)
	}
	for i := range base.Tracks {
		tk := trackIdentity(base.Tracks, i)
		if usedTracks[tk] {
			continue
		}
		// Track existed only in base (was removed in proposed).
		if ch := trackChange[tk]; ch == nil || !ch.Reverted {
			continue
		}
		tr := cloneCutsceneTrack(base.Tracks[i])
		applyManualClipEdits(&tr, tk, edits)
		out.Tracks = append(out.Tracks, tr)
	}
	return out
}

func composeTrack(tk string, btr, atr cutscene.Track, clipChange map[string]*CutsceneChange) cutscene.Track {
	tr := cloneCutsceneTrack(atr)
	tr.Clips = nil

	bClips := make(map[string]cutscene.Clip, len(btr.Clips))
	for i, cl := range btr.Clips {
		bClips[clipIdentity(btr, cl, i)] = cl
	}
	seen := make(map[string]bool)
	for i, acl := range atr.Clips {
		ck := clipIdentity(atr, acl, i)
		seen[ck] = true
		bcl, existed := bClips[ck]
		ch := clipChange[tk+"\x00"+ck]
		if !existed {
			if ch != nil && ch.Reverted {
				continue
			}
			tr.Clips = append(tr.Clips, cloneCutsceneClip(acl))
			continue
		}
		if ch != nil && ch.Reverted {
			tr.Clips = append(tr.Clips, cloneCutsceneClip(bcl))
		} else {
			tr.Clips = append(tr.Clips, cloneCutsceneClip(acl))
		}
	}
	for i, bcl := range btr.Clips {
		ck := clipIdentity(btr, bcl, i)
		if seen[ck] {
			continue
		}
		// Clip existed only in base (was removed in proposed).
		if ch := clipChange[tk+"\x00"+ck]; ch != nil && ch.Reverted {
			tr.Clips = append(tr.Clips, cloneCutsceneClip(bcl))
		}
	}
	slices.SortStableFunc(tr.Clips, func(a, b cutscene.Clip) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		return 0
	})
	return tr
}

func applyManualClipEdits(tr *cutscene.Track, tk string, edits map[string]cutsceneClipEdit) {
	if len(edits) == 0 {
		return
	}
	changed := false
	for ci := range tr.Clips {
		ck := clipIdentity(*tr, tr.Clips[ci], ci)
		if ed, ok := edits[tk+"\x00"+ck]; ok {
			tr.Clips[ci].Start = ed.start
			tr.Clips[ci].Duration = ed.duration
			if tr.Kind == cutscene.TrackCamera || tr.Kind == cutscene.TrackActor {
				tr.Clips[ci].Easing = ed.easing
			}
			changed = true
		}
	}
	if changed {
		slices.SortStableFunc(tr.Clips, func(a, b cutscene.Clip) int {
			if a.Start < b.Start {
				return -1
			}
			if a.Start > b.Start {
				return 1
			}
			return 0
		})
	}
}

func trackIdentity(tracks []cutscene.Track, idx int) string {
	tr := tracks[idx]
	if tr.ID != "" {
		return "id:" + tr.ID
	}
	if tr.Kind == cutscene.TrackActor && tr.Target != "" {
		return "actor:" + tr.Target
	}
	occ := 0
	for i := range idx {
		if tracks[i].ID == "" && tracks[i].Kind == tr.Kind {
			occ++
		}
	}
	if occ == 0 {
		return "kind:" + string(tr.Kind)
	}
	return fmt.Sprintf("kind:%s:%d", tr.Kind, occ)
}

func clipIdentity(tr cutscene.Track, cl cutscene.Clip, idx int) string {
	if cl.ID != "" {
		return "id:" + cl.ID
	}
	switch tr.Kind {
	case cutscene.TrackDialogue:
		return "line:" + cl.Line
	case cutscene.TrackSound, cutscene.TrackMusic:
		return fmt.Sprintf("audio:%s:%d", cl.Source, idx)
	default:
		return fmt.Sprintf("idx:%d", idx)
	}
}

func trackDisplayName(tr cutscene.Track) string {
	if tr.Name != "" {
		return tr.Name
	}
	if tr.Kind == cutscene.TrackActor && tr.Target != "" {
		return tr.Target
	}
	switch tr.Kind {
	case cutscene.TrackCamera:
		return "Camera"
	case cutscene.TrackActor:
		return "Actor"
	case cutscene.TrackDialogue:
		return "Dialogue"
	case cutscene.TrackSound:
		return "Sound"
	case cutscene.TrackMusic:
		return "Music"
	default:
		return string(tr.Kind)
	}
}

func cutsceneClipDisplayLabel(kind cutscene.TrackKind, cl cutscene.Clip) string {
	if kind == cutscene.TrackDialogue && cl.Line != "" {
		return cl.Line
	}
	if cl.Label != "" {
		if kind == cutscene.TrackMusic && cl.FadeIn > 0 {
			return fmt.Sprintf("%s · fade in %s", cl.Label, formatCutsceneSecCompact(time.Duration(cl.FadeIn)))
		}
		if kind == cutscene.TrackActor && cl.Action == cutscene.ActionWalk && cl.FromX != nil && cl.ToX != nil {
			return fmt.Sprintf("%s · x %.0f → %.0f", cl.Label, *cl.FromX, *cl.ToX)
		}
		if kind == cutscene.TrackActor && cl.Action == cutscene.ActionPatrol && cl.FromX != nil && cl.ToX != nil {
			return fmt.Sprintf("%s %.0f → %.0f", cl.Label, *cl.FromX, *cl.ToX)
		}
		return cl.Label
	}
	switch kind {
	case cutscene.TrackCamera:
		switch cl.Action {
		case cutscene.ActionHold:
			if cl.Target != "" {
				return "Hold on " + cl.Target
			}
			return "Hold"
		case cutscene.ActionPan:
			if cl.Target != "" {
				return "Pan to " + cl.Target
			}
			return "Pan"
		case cutscene.ActionFollow:
			return "Follow " + cl.Target
		}
	case cutscene.TrackActor:
		switch cl.Action {
		case cutscene.ActionWalk:
			if cl.FromX != nil && cl.ToX != nil {
				return fmt.Sprintf("Walk · x %.0f → %.0f", *cl.FromX, *cl.ToX)
			}
			return "Walk"
		case cutscene.ActionState:
			if cl.State != "" {
				return strings.ToUpper(cl.State[:1]) + cl.State[1:]
			}
			return "State"
		case cutscene.ActionDrop:
			return "Drop"
		case cutscene.ActionPatrol:
			if cl.FromX != nil && cl.ToX != nil {
				return fmt.Sprintf("Patrol %.0f → %.0f", *cl.FromX, *cl.ToX)
			}
			return "Patrol"
		}
	case cutscene.TrackSound:
		return strings.TrimSuffix(path.Base(cl.Source), path.Ext(cl.Source))
	case cutscene.TrackMusic:
		base := strings.TrimSuffix(path.Base(cl.Source), path.Ext(cl.Source))
		if cl.FadeIn > 0 {
			return fmt.Sprintf("%s · fade in %s", base, formatCutsceneSecCompact(time.Duration(cl.FadeIn)))
		}
		return base
	}
	return cl.ID
}

func formatCutsceneSec(d time.Duration) string {
	return fmt.Sprintf("%.1f s", d.Seconds())
}

func formatCutsceneSecCompact(d time.Duration) string {
	s := d.Seconds()
	if math.Abs(s-math.Round(s)) < 1e-6 {
		return fmt.Sprintf("%.0f s", s)
	}
	return fmt.Sprintf("%.1f s", s)
}

func cloneCutscene(c *cutscene.Cutscene) *cutscene.Cutscene {
	if c == nil {
		return nil
	}
	cp := *c
	if c.Tracks != nil {
		cp.Tracks = make([]cutscene.Track, len(c.Tracks))
		for i := range c.Tracks {
			cp.Tracks[i] = cloneCutsceneTrack(c.Tracks[i])
		}
	}
	return &cp
}

func cloneCutsceneTrack(tr cutscene.Track) cutscene.Track {
	cp := tr
	if tr.Clips != nil {
		cp.Clips = make([]cutscene.Clip, len(tr.Clips))
		for i := range tr.Clips {
			cp.Clips[i] = cloneCutsceneClip(tr.Clips[i])
		}
	}
	return cp
}

func cloneCutsceneClip(cl cutscene.Clip) cutscene.Clip {
	cp := cl
	cp.FromX = cloneFloat32Ptr(cl.FromX)
	cp.FromY = cloneFloat32Ptr(cl.FromY)
	cp.ToX = cloneFloat32Ptr(cl.ToX)
	cp.ToY = cloneFloat32Ptr(cl.ToY)
	cp.Zoom = cloneFloat32Ptr(cl.Zoom)
	cp.Speed = cloneFloat32Ptr(cl.Speed)
	cp.Volume = cloneFloat32Ptr(cl.Volume)
	return cp
}

func cloneClipEdits(m map[string]cutsceneClipEdit) map[string]cutsceneClipEdit {
	cp := make(map[string]cutsceneClipEdit, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func clipsEqual(a, b cutscene.Clip) bool {
	return reflect.DeepEqual(a, b)
}

// cutsceneCmd applies and undoes ordinary cutscene open, close, and edit operations.
type cutsceneCmd struct {
	ed                    *Editor
	label                 string
	targetPath            string
	before, after         *cutscene.Cutscene
	beforeMeta, afterMeta *cutsceneMeta
	beforeSaved           []byte
	afterSaved            []byte
}

func (c *cutsceneCmd) Label() string { return c.label }

func (c *cutsceneCmd) Do() { c.apply(c.after, c.afterMeta, c.afterSaved) }

func (c *cutsceneCmd) Undo() { c.apply(c.before, c.beforeMeta, c.beforeSaved) }

func (c *cutsceneCmd) apply(doc *cutscene.Cutscene, meta *cutsceneMeta, saved []byte) {
	e := c.ed
	e.cutscene.doc = cloneCutscene(doc)
	if meta != nil {
		e.stopCutsceneLoop()
		e.cutscene.meta = *meta
		e.cutscene.saved = slices.Clone(saved)
		e.cutscene.review = nil
		e.cutscene.time = 0
		e.selectFirstCutsceneClip(e.cutscene.doc)
	} else {
		// Scope clip edits back to their originating cutscene path so undo/redo across cutscene switches never overwrites another file.
		e.cutscene.review = nil
		if e.cutscene.meta.path != c.targetPath {
			e.stopCutsceneLoop()
			e.cutscene.meta.path = c.targetPath
			e.cutscene.saved = slices.Clone(saved)
			e.keepCutsceneSelectionValid(e.cutscene.doc)
		}
	}
	e.cutscene.structRev++
	e.updateCutsceneDirty()
	e.Status = c.label
}

// cutsceneCommitCmd commits a kept cutscene review (both in memory and on disk when path is set) in one atomic undoable step.
type cutsceneCommitCmd struct {
	ed          *Editor
	label       string
	path        string
	beforeMeta  cutsceneMeta
	afterMeta   cutsceneMeta
	beforeDoc   *cutscene.Cutscene
	afterDoc    *cutscene.Cutscene
	beforeDisk  []byte
	afterDisk   []byte
	hadFile     bool
	beforeSaved []byte
	afterSaved  []byte
	baseYAML    []byte
}

func (c *cutsceneCommitCmd) Label() string { return c.label }
func (c *cutsceneCommitCmd) Do()           { _ = c.do() }
func (c *cutsceneCommitCmd) Undo()         { _ = c.undo() }

func (c *cutsceneCommitCmd) do() error {
	e := c.ed
	if c.path != "" {
		if err := e.writeCutsceneFileAtomic(c.path, c.afterDisk); err != nil {
			return err
		}
	}
	e.cutscene.doc = cloneCutscene(c.afterDoc)
	e.cutscene.meta = c.afterMeta
	e.cutscene.saved = slices.Clone(c.afterSaved)
	e.cutscene.review = nil
	e.cutscene.structRev++
	e.keepCutsceneSelectionValid(e.cutscene.doc)
	e.updateCutsceneDirty()
	e.Status = c.label
	if c.path != "" {
		e.filesRev++
		e.invalidateProjectIndex()
		e.CheckProblems()
	}
	return nil
}

func (c *cutsceneCommitCmd) undo() error {
	e := c.ed
	if c.path != "" {
		if err := e.checkCutsceneProjectSymlink(c.path, false); err != nil {
			return err
		}
		if c.hadFile {
			if err := e.writeCutsceneFileAtomic(c.path, c.beforeDisk); err != nil {
				return err
			}
		} else {
			if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	e.cutscene.doc = cloneCutscene(c.beforeDoc)
	e.cutscene.meta = c.beforeMeta
	e.cutscene.saved = slices.Clone(c.beforeSaved)
	e.cutscene.review = nil
	e.cutscene.structRev++
	e.keepCutsceneSelectionValid(e.cutscene.doc)
	e.updateCutsceneDirty()
	if c.path != "" {
		e.filesRev++
		e.invalidateProjectIndex()
		e.CheckProblems()
	}
	return nil
}
