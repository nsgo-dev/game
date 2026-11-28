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
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// ErrNoParticleEmitter is returned by particle emitter edits when no emitter is open.
var ErrNoParticleEmitter = errors.New("editor: no particle emitter is open")

// ParticleCurveKind selects which curve the Particle Emitter panel's curve widget edits.
type ParticleCurveKind string

const (
	ParticleCurveSize       ParticleCurveKind = "size"
	ParticleCurveAlpha      ParticleCurveKind = "alpha"
	ParticleCurveRotation   ParticleCurveKind = "rotation"
	ParticleCurveColor      ParticleCurveKind = "color"
	ParticleCurveTrailWidth ParticleCurveKind = "trailWidth"
	ParticleCurveTrailColor ParticleCurveKind = "trailColor"
)

type particleCurveDrag struct {
	kind       ParticleCurveKind
	index      int
	beforeRoot particles.Config
	changed    bool
}

type particleEditorState struct {
	cfg            particles.Config
	path           string
	saved          []byte
	dirty          bool
	dirtyValid     bool
	subPath        []int
	preview        *particles.Emitter
	previewPlaying bool
	previewElapsed time.Duration
	previewStop    func()
	previewAtlas   string
	previewFloor   bool
	activeCurve    ParticleCurveKind
	selectedStop   int
	curveDrag      *particleCurveDrag
}

func defaultParticleConfig() particles.Config {
	return particles.Config{
		Space:        particles.SpaceWorld,
		Shape:        particles.Circle(6),
		Rate:         30,
		MaxParticles: 128,
		LifetimeMin:  400 * time.Millisecond,
		LifetimeMax:  800 * time.Millisecond,
		SpeedMin:     30,
		SpeedMax:     80,
		AngleMin:     -2.3561945,
		AngleMax:     -0.7853982,
		GravityY:     40,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 1, Value: 0.2},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 1, Value: 0},
		},
		ColorCurve: particles.ColorCurve{
			{T: 0, Color: core.Color(0xffd166ff)},
			{T: 1, Color: core.Color(0xef476f00)},
		},
		Seed: 1,
	}
}

func cloneParticleConfig(cfg particles.Config) particles.Config {
	out := cfg
	if len(cfg.Bursts) > 0 {
		out.Bursts = slices.Clone(cfg.Bursts)
	}
	if len(cfg.SizeCurve) > 0 {
		out.SizeCurve = slices.Clone(cfg.SizeCurve)
	}
	if len(cfg.AlphaCurve) > 0 {
		out.AlphaCurve = slices.Clone(cfg.AlphaCurve)
	}
	if len(cfg.RotationCurve) > 0 {
		out.RotationCurve = slices.Clone(cfg.RotationCurve)
	}
	if len(cfg.ColorCurve) > 0 {
		out.ColorCurve = slices.Clone(cfg.ColorCurve)
	}
	if len(cfg.FrameNames) > 0 {
		out.FrameNames = slices.Clone(cfg.FrameNames)
	}
	if cfg.Trail != nil {
		tr := *cfg.Trail
		if len(cfg.Trail.WidthCurve) > 0 {
			tr.WidthCurve = slices.Clone(cfg.Trail.WidthCurve)
		}
		if len(cfg.Trail.ColorGradient) > 0 {
			tr.ColorGradient = slices.Clone(cfg.Trail.ColorGradient)
		}
		out.Trail = &tr
	}
	if len(cfg.SubEmitters) > 0 {
		out.SubEmitters = make([]particles.SubEmitterRule, len(cfg.SubEmitters))
		for i, sub := range cfg.SubEmitters {
			sub.Config = cloneParticleConfig(sub.Config)
			out.SubEmitters[i] = sub
		}
	}
	return out
}

// isParticleFilePath reports whether rel names a particle emitter file by convention (*.particles.yaml, *.particles.yml, *.emitter.yaml, *.emitter.yml).
func isParticleFilePath(rel string) bool {
	lower := strings.ToLower(rel)
	return strings.HasSuffix(lower, ".particles.yaml") ||
		strings.HasSuffix(lower, ".particles.yml") ||
		strings.HasSuffix(lower, ".emitter.yaml") ||
		strings.HasSuffix(lower, ".emitter.yml")
}

// ParticleEmitterOpen reports whether the Particle Emitter panel has an emitter open.
func (e *Editor) ParticleEmitterOpen() bool {
	return e != nil && e.particles != nil
}

// ParticleEmitter returns a deep copy of the root particles.Config open in the Particle Emitter panel, or nil when closed.
func (e *Editor) ParticleEmitter() *particles.Config {
	if e == nil || e.particles == nil {
		return nil
	}
	cp := cloneParticleConfig(e.particles.cfg)
	return &cp
}

// ParticleActiveConfig returns a deep copy of the currently targeted emitter Config (the root config or the selected nested sub-emitter), or nil when closed.
func (e *Editor) ParticleActiveConfig() *particles.Config {
	if e == nil || e.particles == nil {
		return nil
	}
	target := resolveParticleTarget(&e.particles.cfg, e.particles.subPath)
	if target == nil {
		target = &e.particles.cfg
	}
	cp := cloneParticleConfig(*target)
	return &cp
}

// ParticleTargetPath returns a copy of the nested sub-emitter index path currently edited (empty for the root emitter).
func (e *Editor) ParticleTargetPath() []int {
	if e == nil || e.particles == nil || len(e.particles.subPath) == 0 {
		return nil
	}
	return slices.Clone(e.particles.subPath)
}

// SetParticleTargetPath selects either the root emitter (empty path) or a nested sub-emitter config by index path for editing in the panel.
func (e *Editor) SetParticleTargetPath(subPath []int) error {
	if e == nil || e.particles == nil {
		return ErrNoParticleEmitter
	}
	e.cancelParticleCurveDrag()
	if resolveParticleTarget(&e.particles.cfg, subPath) == nil {
		return fmt.Errorf("editor: invalid sub-emitter path %v", subPath)
	}
	e.particles.subPath = slices.Clone(subPath)
	e.particles.selectedStop = -1
	e.clampParticleActiveCurve()
	e.notify()
	return nil
}

func resolveParticleTarget(root *particles.Config, subPath []int) *particles.Config {
	cur := root
	for _, idx := range subPath {
		if idx < 0 || idx >= len(cur.SubEmitters) {
			return nil
		}
		cur = &cur.SubEmitters[idx].Config
	}
	return cur
}

func (e *Editor) clampParticleSubPath() {
	if e.particles == nil {
		return
	}
	cur := &e.particles.cfg
	valid := 0
	for _, idx := range e.particles.subPath {
		if idx < 0 || idx >= len(cur.SubEmitters) {
			break
		}
		cur = &cur.SubEmitters[idx].Config
		valid++
	}
	if valid < len(e.particles.subPath) {
		e.particles.subPath = slices.Clone(e.particles.subPath[:valid])
	}
}

func (e *Editor) clampParticleActiveCurve() {
	if e.particles == nil {
		return
	}
	if e.particles.activeCurve == "" {
		e.particles.activeCurve = ParticleCurveSize
	}
	target := resolveParticleTarget(&e.particles.cfg, e.particles.subPath)
	if target != nil && target.Trail == nil && (e.particles.activeCurve == ParticleCurveTrailWidth || e.particles.activeCurve == ParticleCurveTrailColor) {
		e.particles.activeCurve = ParticleCurveSize
		e.particles.selectedStop = -1
	}
}

// ParticleEmitterPath returns the disk path of the open particle emitter file, or "".
func (e *Editor) ParticleEmitterPath() string {
	if e == nil || e.particles == nil {
		return ""
	}
	return e.particles.path
}

// ParticleEmitterDirty reports whether the open particle emitter differs from the YAML bytes it was opened from or last saved to.
func (e *Editor) ParticleEmitterDirty() bool {
	if e == nil || e.particles == nil {
		return false
	}
	if !e.particles.dirtyValid {
		data, err := particles.MarshalConfigYAML(e.particles.cfg)
		e.particles.dirty = err != nil || !bytes.Equal(data, e.particles.saved)
		e.particles.dirtyValid = true
	}
	return e.particles.dirty
}

// NewParticleEmitter opens a new starter 2D particle emitter in the Particle Emitter panel as an undoable command.
func (e *Editor) NewParticleEmitter() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	cfg := defaultParticleConfig()
	saved, err := particles.MarshalConfigYAML(cfg)
	if err != nil {
		return err
	}
	e.swapParticleEmitter("New particle emitter", &cfg, "", saved, "")
	return nil
}

// OpenParticleEmitterYAML parses data as a versioned 2D particle emitter YAML file and opens it in the Particle Emitter panel as an undoable command.
func (e *Editor) OpenParticleEmitterYAML(data []byte) error {
	return e.openParticleEmitter(data, "", "")
}

// OpenParticleEmitterFile reads a versioned 2D particle emitter YAML file from path and opens it in the Particle Emitter panel as an undoable command.
func (e *Editor) OpenParticleEmitterFile(filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return e.openParticleEmitter(data, filePath, "")
}

// OpenParticleEmitterProjectFile opens relPath (slash-separated from the open project's root) in the Particle Emitter panel as an undoable command.
func (e *Editor) OpenParticleEmitterProjectFile(relPath string) error {
	return e.openParticleEmitterProjectFile(relPath, "")
}

func (e *Editor) openParticleEmitterProjectFile(relPath, atlasHint string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	abs, err := e.projectFile(relPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	return e.openParticleEmitter(data, abs, atlasHint)
}

func (e *Editor) openParticleEmitter(data []byte, filePath, atlasHint string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	cfg, err := particles.UnmarshalConfigYAML(data)
	if err != nil {
		if filePath != "" {
			return fmt.Errorf("%s: %w", filePath, err)
		}
		return err
	}
	saved, err := particles.MarshalConfigYAML(cfg)
	if err != nil {
		return err
	}
	label := "Open particle emitter"
	if filePath != "" {
		label += " " + filepath.Base(filePath)
	}
	e.swapParticleEmitter(label, &cfg, filePath, saved, atlasHint)
	return nil
}

// CloseParticleEmitter closes the open particle emitter panel as an undoable command.
func (e *Editor) CloseParticleEmitter() error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	if e.particles == nil {
		return nil
	}
	e.swapParticleEmitter("Close particle emitter", nil, "", nil, "")
	return nil
}

// SaveParticleEmitterYAML encodes the open particle emitter as deterministic versioned YAML.
func (e *Editor) SaveParticleEmitterYAML() ([]byte, error) {
	if e == nil || e.particles == nil {
		return nil, ErrNoParticleEmitter
	}
	return particles.MarshalConfigYAML(e.particles.cfg)
}

// SaveParticleEmitterFile encodes the open particle emitter to deterministic YAML, writes it atomically to filePath, remembers filePath, and hot-reloads matching live scene emitters.
func (e *Editor) SaveParticleEmitterFile(filePath string) error {
	if e == nil || e.particles == nil {
		return ErrNoParticleEmitter
	}
	if e.playing {
		return ErrPlaying
	}
	e.endParticleCurveDrag()
	data, err := e.SaveParticleEmitterYAML()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(filePath, data); err != nil {
		return err
	}
	e.particles.path = filePath
	e.particles.saved = data
	e.particles.dirty = false
	e.particles.dirtyValid = true
	e.filesRev++
	e.invalidateProjectIndex()
	if rel, ok := e.projectRel(filePath); ok {
		e.hotReloadSceneEmitters(rel, e.particles.cfg)
	}
	e.Status = "Saved emitter " + filePath
	e.notify()
	e.CheckProblems()
	return nil
}

func (e *Editor) saveParticleEmitterFromPanel() error {
	if e == nil || e.particles == nil {
		return ErrNoParticleEmitter
	}
	if e.playing {
		return ErrPlaying
	}
	e.endParticleCurveDrag()
	if e.OnSaveParticleEmitter != nil {
		data, err := e.SaveParticleEmitterYAML()
		if err != nil {
			return err
		}
		if err := e.OnSaveParticleEmitter(data); err != nil {
			return err
		}
		e.particles.saved = data
		e.particles.dirty = false
		e.particles.dirtyValid = true
		e.Status = "Saved emitter"
		e.notify()
		return nil
	}
	if e.particles.path == "" {
		return errors.New("editor: emitter has no file path; pass one to SaveParticleEmitterFile or set OnSaveParticleEmitter")
	}
	return e.SaveParticleEmitterFile(e.particles.path)
}

// EditParticleEmitter applies edit to a copy of the currently targeted particle emitter config (root or active sub-emitter), validates it, records an undoable command, and reloads the live preview without resetting running particles.
func (e *Editor) EditParticleEmitter(label string, edit func(cfg *particles.Config) error) error {
	if e == nil || e.particles == nil {
		return ErrNoParticleEmitter
	}
	if e.playing {
		return ErrPlaying
	}
	before := cloneParticleConfig(e.particles.cfg)
	after := cloneParticleConfig(e.particles.cfg)
	target := resolveParticleTarget(&after, e.particles.subPath)
	if target == nil {
		return fmt.Errorf("editor: invalid sub-emitter path %v", e.particles.subPath)
	}
	if err := edit(target); err != nil {
		return err
	}
	if _, err := particles.MarshalConfigYAML(after); err != nil {
		return err
	}
	if reflect.DeepEqual(before, after) {
		return nil
	}
	e.History.Execute(&particleEditCmd{
		ed:     e,
		label:  label,
		before: before,
		after:  after,
	})
	return nil
}

func (e *Editor) swapParticleEmitter(label string, cfg *particles.Config, filePath string, saved []byte, atlasHint string) {
	var beforeCfg *particles.Config
	var beforePath string
	var beforeSaved []byte
	var beforeAtlas string
	if e.particles != nil {
		cp := cloneParticleConfig(e.particles.cfg)
		beforeCfg = &cp
		beforePath = e.particles.path
		beforeSaved = slices.Clone(e.particles.saved)
		beforeAtlas = e.particles.previewAtlas
	}
	var afterCfg *particles.Config
	if cfg != nil {
		cp := cloneParticleConfig(*cfg)
		afterCfg = &cp
	}
	afterAtlas := atlasHint
	if afterAtlas == "" && afterCfg != nil {
		afterAtlas = e.defaultParticleAtlas(*afterCfg)
	}
	e.History.Execute(&particleSwapCmd{
		ed:          e,
		label:       label,
		beforeCfg:   beforeCfg,
		beforePath:  beforePath,
		beforeSaved: beforeSaved,
		beforeAtlas: beforeAtlas,
		afterCfg:    afterCfg,
		afterPath:   filePath,
		afterSaved:  slices.Clone(saved),
		afterAtlas:  afterAtlas,
	})
}

func (e *Editor) defaultParticleAtlas(cfg particles.Config) string {
	names := e.ParticleAtlasNames()
	if len(names) == 0 {
		return ""
	}
	// Prefer an atlas that already contains the emitter's first configured frame name.
	if len(cfg.FrameNames) > 0 && e.Resolver != nil {
		want := cfg.FrameNames[0]
		for _, name := range names {
			if a, err := e.Resolver.Atlas(name); err == nil && a != nil {
				if _, ok := a.Frames[want]; ok {
					return name
				}
			}
		}
	}
	return names[0]
}

type particleSwapCmd struct {
	ed          *Editor
	label       string
	beforeCfg   *particles.Config
	beforePath  string
	beforeSaved []byte
	beforeAtlas string
	afterCfg    *particles.Config
	afterPath   string
	afterSaved  []byte
	afterAtlas  string
}

func (c *particleSwapCmd) Label() string { return c.label }
func (c *particleSwapCmd) Do() {
	c.apply(c.afterCfg, c.afterPath, c.afterSaved, c.afterAtlas)
}
func (c *particleSwapCmd) Undo() {
	c.apply(c.beforeCfg, c.beforePath, c.beforeSaved, c.beforeAtlas)
}

func (c *particleSwapCmd) apply(cfg *particles.Config, filePath string, saved []byte, atlasName string) {
	e := c.ed
	e.stopParticlePreviewLoop()
	if cfg == nil {
		e.particles = nil
		e.Status = c.label
		return
	}
	cloned := cloneParticleConfig(*cfg)
	em := particles.NewEmitter(cloned)
	st := &particleEditorState{
		cfg:            cloned,
		path:           filePath,
		saved:          slices.Clone(saved),
		preview:        em,
		previewPlaying: true,
		previewAtlas:   atlasName,
		activeCurve:    ParticleCurveSize,
		selectedStop:   -1,
	}
	e.particles = st
	e.bindParticlePreviewAtlas()
	e.syncParticlePreviewCollider()
	e.startParticlePreviewLoop()
	e.Status = c.label
}

type particleEditCmd struct {
	ed     *Editor
	label  string
	before particles.Config
	after  particles.Config
}

func (c *particleEditCmd) Label() string { return c.label }
func (c *particleEditCmd) Do()           { c.apply(c.after) }
func (c *particleEditCmd) Undo()         { c.apply(c.before) }

func (c *particleEditCmd) apply(cfg particles.Config) {
	e := c.ed
	if e.particles == nil {
		return
	}
	e.particles.cfg = cloneParticleConfig(cfg)
	e.particles.dirtyValid = false
	e.particles.curveDrag = nil
	e.clampParticleSubPath()
	e.clampParticleActiveCurve()
	e.syncParticlePreviewConfig()
	if e.particles.path != "" {
		if rel, ok := e.projectRel(e.particles.path); ok {
			e.hotReloadSceneEmitters(rel, e.particles.cfg)
		}
	}
	e.Status = c.label
}

// syncParticlePreviewConfig applies the edited root Config to the live preview Emitter WITHOUT resetting live particles, PRNG state, or elapsed time.
func (e *Editor) syncParticlePreviewConfig() {
	if e == nil || e.particles == nil || e.particles.preview == nil {
		return
	}
	e.bindParticlePreviewAtlas()
	e.particles.preview.ReloadConfig(e.particles.cfg)
	e.syncParticlePreviewFrames()
	e.syncParticlePreviewCollider()
}

func (e *Editor) bindParticlePreviewAtlas() {
	if e == nil || e.particles == nil || e.particles.preview == nil {
		return
	}
	if e.Resolver == nil || e.particles.previewAtlas == "" {
		e.particles.preview.Atlas = nil
		e.particles.preview.Frames = nil
		return
	}
	atlas, err := e.Resolver.Atlas(e.particles.previewAtlas)
	if err != nil || atlas == nil {
		e.particles.preview.Atlas = nil
		e.particles.preview.Frames = nil
		return
	}
	e.particles.preview.Atlas = atlas
	e.syncParticlePreviewFrames()
}

func (e *Editor) syncParticlePreviewFrames() {
	if e == nil || e.particles == nil || e.particles.preview == nil {
		return
	}
	em := e.particles.preview
	if em.Atlas == nil || len(e.particles.cfg.FrameNames) == 0 {
		em.Frames = nil
		return
	}
	frames := make([]sprite.Frame, 0, len(e.particles.cfg.FrameNames))
	for _, fn := range e.particles.cfg.FrameNames {
		if f, ok := em.Atlas.Frames[fn]; ok {
			frames = append(frames, f)
		}
	}
	em.Frames = frames
}

func (e *Editor) syncParticlePreviewCollider() {
	if e == nil || e.particles == nil || e.particles.preview == nil {
		return
	}
	em := e.particles.preview
	if e.particles.previewFloor {
		em.Collision.Collider = &particles.RectCollider{
			X: em.X - 400,
			Y: em.Y + 60,
			W: 800,
			H: 40,
		}
	} else {
		em.Collision.Collider = nil
	}
}

// ParticlePreview returns the live preview particles.Emitter for the open emitter, or nil when closed.
func (e *Editor) ParticlePreview() *particles.Emitter {
	if e == nil || e.particles == nil {
		return nil
	}
	return e.particles.preview
}

// ParticlePreviewPlaying reports whether the live particle preview is currently running.
func (e *Editor) ParticlePreviewPlaying() bool {
	return e != nil && e.particles != nil && e.particles.previewPlaying
}

// ParticlePreviewElapsed returns the elapsed simulation time of the live particle preview since its last Restart.
func (e *Editor) ParticlePreviewElapsed() time.Duration {
	if e == nil || e.particles == nil {
		return 0
	}
	return e.particles.previewElapsed
}

// PlayParticlePreview starts or resumes the live particle preview loop.
func (e *Editor) PlayParticlePreview() error {
	if e == nil || e.particles == nil {
		return ErrNoParticleEmitter
	}
	if e.particles.previewPlaying {
		return nil
	}
	e.particles.previewPlaying = true
	e.startParticlePreviewLoop()
	e.Status = "Playing particle preview"
	e.notify()
	return nil
}

// PauseParticlePreview pauses the live particle preview while keeping all live particles visible.
func (e *Editor) PauseParticlePreview() {
	if e == nil || e.particles == nil || !e.particles.previewPlaying {
		return
	}
	e.stopParticlePreviewLoop()
	e.Status = "Paused particle preview"
	e.notify()
}

// RestartParticlePreview resets the live preview emitter and its PRNG so playback replays from t=0.
func (e *Editor) RestartParticlePreview() {
	if e == nil || e.particles == nil || e.particles.preview == nil {
		return
	}
	e.particles.preview.Reset()
	e.particles.previewElapsed = 0
	e.Status = "Restarted particle preview"
	e.notify()
}

// StepParticlePreview advances the live preview emitter by dt (even while paused).
func (e *Editor) StepParticlePreview(dt time.Duration) {
	if e == nil || e.particles == nil || e.particles.preview == nil || dt <= 0 {
		return
	}
	e.particles.preview.Update(dt)
	e.particles.previewElapsed += dt
	e.notify()
}

// BurstParticlePreview emits count particles immediately into the live preview emitter.
func (e *Editor) BurstParticlePreview(count int) {
	if e == nil || e.particles == nil || e.particles.preview == nil || count <= 0 {
		return
	}
	e.particles.preview.Burst(count)
	e.notify()
}

// ParticlePreviewAtlas returns the atlas name bound to the live particle preview and atlas frame picker.
func (e *Editor) ParticlePreviewAtlas() string {
	if e == nil || e.particles == nil {
		return ""
	}
	return e.particles.previewAtlas
}

// SetParticlePreviewAtlas binds the named atlas from the editor's Resolver to the live particle preview and atlas frame picker.
func (e *Editor) SetParticlePreviewAtlas(atlasName string) {
	if e == nil || e.particles == nil {
		return
	}
	e.particles.previewAtlas = atlasName
	e.bindParticlePreviewAtlas()
	e.notify()
}

// ParticlePreviewFloor reports whether the preview includes a test floor collider below the emitter.
func (e *Editor) ParticlePreviewFloor() bool {
	return e != nil && e.particles != nil && e.particles.previewFloor
}

// SetParticlePreviewFloor toggles the preview's horizontal test floor collider for inspecting collision response.
func (e *Editor) SetParticlePreviewFloor(on bool) {
	if e == nil || e.particles == nil || e.particles.previewFloor == on {
		return
	}
	e.particles.previewFloor = on
	e.syncParticlePreviewCollider()
	e.notify()
}

// ParticleAtlasNames returns all atlas names available through the editor's Resolver in sorted order.
func (e *Editor) ParticleAtlasNames() []string {
	entries := e.Assets()
	if len(entries) == 0 {
		return nil
	}
	var names []string
	for _, ent := range entries {
		if len(names) == 0 || names[len(names)-1] != ent.Atlas {
			names = append(names, ent.Atlas)
		}
	}
	return names
}

// ParticleAtlasFrames returns the frames of atlasName in sorted order (or of ParticlePreviewAtlas when atlasName is empty).
func (e *Editor) ParticleAtlasFrames(atlasName string) []AssetEntry {
	if atlasName == "" {
		atlasName = e.ParticlePreviewAtlas()
	}
	if atlasName == "" {
		return nil
	}
	var out []AssetEntry
	for _, ent := range e.Assets() {
		if ent.Atlas == atlasName {
			out = append(out, ent)
		}
	}
	return out
}

func (e *Editor) startParticlePreviewLoop() {
	if e == nil || e.particles == nil || !e.particles.previewPlaying || e.scope == nil || e.particles.previewStop != nil {
		return
	}
	loop := &game.Loop{
		Rate: e.PlayRate,
		Update: func(dt time.Duration) {
			if e.particles == nil || !e.particles.previewPlaying {
				return
			}
			e.StepParticlePreview(dt)
		},
	}
	e.scope.Run(func() {
		e.particles.previewStop = loop.Run(e.scope)
	})
}

func (e *Editor) stopParticlePreviewLoop() {
	if e == nil || e.particles == nil {
		return
	}
	e.particles.previewPlaying = false
	if e.particles.previewStop != nil {
		e.particles.previewStop()
		e.particles.previewStop = nil
	}
}

// ---- Individual undoable field setters ----

// SetParticleSpace sets Config.Space to particles.SpaceWorld or particles.SpaceLocal.
func (e *Editor) SetParticleSpace(space particles.SimulationSpace) error {
	return e.EditParticleEmitter("Set particle space", func(cfg *particles.Config) error {
		cfg.Space = space
		return nil
	})
}

// SetParticlePosition sets Config.X and Config.Y in points; both coordinates must be finite.
func (e *Editor) SetParticlePosition(x, y float32) error {
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return errors.New("editor: particle position must be finite")
	}
	return e.EditParticleEmitter("Set particle position", func(cfg *particles.Config) error {
		cfg.X, cfg.Y = x, y
		return nil
	})
}

// SetParticleRotation sets Config.Rotation in radians clockwise on the screen; rot must be finite.
func (e *Editor) SetParticleRotation(rot float32) error {
	if !isFiniteFloat32(rot) {
		return errors.New("editor: particle rotation must be finite")
	}
	return e.EditParticleEmitter("Set particle rotation", func(cfg *particles.Config) error {
		cfg.Rotation = rot
		return nil
	})
}

// SetParticleZ sets Config.Z to any integer sprite Z order.
func (e *Editor) SetParticleZ(z int) error {
	return e.EditParticleEmitter("Set particle Z", func(cfg *particles.Config) error {
		cfg.Z = z
		return nil
	})
}

// SetParticleSeed sets Config.Seed to any uint64 seed for deterministic emission and motion.
func (e *Editor) SetParticleSeed(seed uint64) error {
	return e.EditParticleEmitter("Set particle seed", func(cfg *particles.Config) error {
		cfg.Seed = seed
		return nil
	})
}

// SetParticleShape sets Config.Shape; its radius, width, and height in points must be finite and non-negative.
func (e *Editor) SetParticleShape(shape particles.Shape) error {
	if r := shape.Radius(); !isFiniteFloat32(r) || r < 0 {
		return errors.New("editor: particle shape radius must be finite and non-negative")
	}
	if w := shape.Width(); !isFiniteFloat32(w) || w < 0 {
		return errors.New("editor: particle shape width must be finite and non-negative")
	}
	if h := shape.Height(); !isFiniteFloat32(h) || h < 0 {
		return errors.New("editor: particle shape height must be finite and non-negative")
	}
	return e.EditParticleEmitter("Set particle shape", func(cfg *particles.Config) error {
		cfg.Shape = shape
		return nil
	})
}

// SetParticleRate sets Config.Rate to a finite, non-negative number of particles per second; zero disables continuous emission.
func (e *Editor) SetParticleRate(rate float32) error {
	if !isFiniteFloat32(rate) || rate < 0 {
		return errors.New("editor: particle rate must be finite and non-negative")
	}
	return e.EditParticleEmitter("Set particle rate", func(cfg *particles.Config) error {
		cfg.Rate = rate
		return nil
	})
}

// SetParticleMaxParticles sets Config.MaxParticles to a non-negative live-particle cap; zero selects the emitter's default.
func (e *Editor) SetParticleMaxParticles(n int) error {
	if n < 0 {
		return errors.New("editor: maxParticles must be non-negative")
	}
	return e.EditParticleEmitter("Set max particles", func(cfg *particles.Config) error {
		cfg.MaxParticles = n
		return nil
	})
}

// AddParticleBurst appends b to Config.Bursts; Time and Interval are non-negative durations and Count is non-negative.
func (e *Editor) AddParticleBurst(b particles.Burst) error {
	if b.Time < 0 || b.Count < 0 || b.Interval < 0 {
		return errors.New("editor: burst time, count and interval must be non-negative")
	}
	return e.EditParticleEmitter("Add particle burst", func(cfg *particles.Config) error {
		cfg.Bursts = append(cfg.Bursts, b)
		return nil
	})
}

// SetParticleBurst replaces Config.Bursts[index] with b; index must exist, Time and Interval are non-negative durations, and Count is non-negative.
func (e *Editor) SetParticleBurst(index int, b particles.Burst) error {
	if b.Time < 0 || b.Count < 0 || b.Interval < 0 {
		return errors.New("editor: burst time, count and interval must be non-negative")
	}
	return e.EditParticleEmitter("Edit particle burst", func(cfg *particles.Config) error {
		if index < 0 || index >= len(cfg.Bursts) {
			return fmt.Errorf("editor: burst index %d out of range", index)
		}
		cfg.Bursts[index] = b
		return nil
	})
}

// RemoveParticleBurst removes Config.Bursts[index]; index must be an existing zero-based burst index.
func (e *Editor) RemoveParticleBurst(index int) error {
	return e.EditParticleEmitter("Remove particle burst", func(cfg *particles.Config) error {
		if index < 0 || index >= len(cfg.Bursts) {
			return fmt.Errorf("editor: burst index %d out of range", index)
		}
		cfg.Bursts = slices.Delete(cfg.Bursts, index, index+1)
		if len(cfg.Bursts) == 0 {
			cfg.Bursts = nil
		}
		return nil
	})
}

// SetParticleLifetime sets Config.LifetimeMin and Config.LifetimeMax to non-negative durations.
func (e *Editor) SetParticleLifetime(minLife, maxLife time.Duration) error {
	if minLife < 0 || maxLife < 0 {
		return errors.New("editor: particle lifetime must be non-negative")
	}
	return e.EditParticleEmitter("Set particle lifetime", func(cfg *particles.Config) error {
		cfg.LifetimeMin, cfg.LifetimeMax = minLife, maxLife
		return nil
	})
}

// SetParticleSpeed sets Config.SpeedMin and Config.SpeedMax in points per second; both speeds must be finite.
func (e *Editor) SetParticleSpeed(minSpeed, maxSpeed float32) error {
	if !isFiniteFloat32(minSpeed) || !isFiniteFloat32(maxSpeed) {
		return errors.New("editor: particle speed must be finite")
	}
	return e.EditParticleEmitter("Set particle speed", func(cfg *particles.Config) error {
		cfg.SpeedMin, cfg.SpeedMax = minSpeed, maxSpeed
		return nil
	})
}

// SetParticleAngle sets Config.AngleMin and Config.AngleMax in radians clockwise from +X; both angles must be finite.
func (e *Editor) SetParticleAngle(minAngle, maxAngle float32) error {
	if !isFiniteFloat32(minAngle) || !isFiniteFloat32(maxAngle) {
		return errors.New("editor: particle angle must be finite")
	}
	return e.EditParticleEmitter("Set particle angle", func(cfg *particles.Config) error {
		cfg.AngleMin, cfg.AngleMax = minAngle, maxAngle
		return nil
	})
}

// SetParticleGravity sets Config.GravityX and Config.GravityY in points per second squared; both components must be finite.
func (e *Editor) SetParticleGravity(gx, gy float32) error {
	if !isFiniteFloat32(gx) || !isFiniteFloat32(gy) {
		return errors.New("editor: particle gravity must be finite")
	}
	return e.EditParticleEmitter("Set particle gravity", func(cfg *particles.Config) error {
		cfg.GravityX, cfg.GravityY = gx, gy
		return nil
	})
}

// SetParticleInitialRotation sets Config.InitialRotationMin and Config.InitialRotationMax in radians; both rotations must be finite.
func (e *Editor) SetParticleInitialRotation(minRot, maxRot float32) error {
	if !isFiniteFloat32(minRot) || !isFiniteFloat32(maxRot) {
		return errors.New("editor: particle initial rotation must be finite")
	}
	return e.EditParticleEmitter("Set initial rotation", func(cfg *particles.Config) error {
		cfg.InitialRotationMin, cfg.InitialRotationMax = minRot, maxRot
		return nil
	})
}

// SetParticleRotationSpeed sets Config.RotationSpeedMin and Config.RotationSpeedMax in radians per second; both speeds must be finite.
func (e *Editor) SetParticleRotationSpeed(minSpeed, maxSpeed float32) error {
	if !isFiniteFloat32(minSpeed) || !isFiniteFloat32(maxSpeed) {
		return errors.New("editor: particle rotation speed must be finite")
	}
	return e.EditParticleEmitter("Set rotation speed", func(cfg *particles.Config) error {
		cfg.RotationSpeedMin, cfg.RotationSpeedMax = minSpeed, maxSpeed
		return nil
	})
}

// SetParticleFrameNames replaces Config.FrameNames with a copy of names; an empty slice clears the frame selection.
func (e *Editor) SetParticleFrameNames(names []string) error {
	return e.EditParticleEmitter("Set particle frames", func(cfg *particles.Config) error {
		if len(names) == 0 {
			cfg.FrameNames = nil
		} else {
			cfg.FrameNames = slices.Clone(names)
		}
		return nil
	})
}

// ToggleParticleFrame adds a non-empty frameName to Config.FrameNames or removes it when already present.
func (e *Editor) ToggleParticleFrame(frameName string) error {
	if frameName == "" {
		return errors.New("editor: frame name must not be empty")
	}
	return e.EditParticleEmitter("Toggle particle frame "+frameName, func(cfg *particles.Config) error {
		if i := slices.Index(cfg.FrameNames, frameName); i >= 0 {
			cfg.FrameNames = slices.Delete(cfg.FrameNames, i, i+1)
			if len(cfg.FrameNames) == 0 {
				cfg.FrameNames = nil
			}
			return nil
		}
		cfg.FrameNames = append(cfg.FrameNames, frameName)
		return nil
	})
}

// SetParticleCollision sets Config.Collision with finite Bounce and Friction factors and a finite Radius in points; Friction clamps to [0, 1] during response and Collider is cleared.
func (e *Editor) SetParticleCollision(col particles.CollisionConfig) error {
	if !isFiniteFloat32(col.Bounce) || !isFiniteFloat32(col.Friction) || !isFiniteFloat32(col.Radius) {
		return errors.New("editor: particle collision values must be finite")
	}
	return e.EditParticleEmitter("Set particle collision", func(cfg *particles.Config) error {
		col.Collider = nil
		cfg.Collision = col
		return nil
	})
}

// SetParticleTrailEnabled enables Config.Trail with defaults of 24 vertices, 0.4-second vertex lifetime, 2-point minimum distance, and 4-point width, or sets it to nil when disabled.
func (e *Editor) SetParticleTrailEnabled(enabled bool) error {
	return e.EditParticleEmitter("Toggle particle trail", func(cfg *particles.Config) error {
		if !enabled {
			cfg.Trail = nil
			return nil
		}
		if cfg.Trail == nil {
			cfg.Trail = &particles.TrailConfig{
				MaxPoints:   24,
				Lifetime:    0.4,
				MinDistance: 2,
				Width:       4,
				Color:       core.Color(0xffffffff),
			}
		}
		return nil
	})
}

// SetParticleTrail copies trail to Config.Trail or clears it with nil; MaxPoints is non-negative, Lifetime is non-negative finite seconds, and MinDistance and Width are non-negative finite points.
func (e *Editor) SetParticleTrail(trail *particles.TrailConfig) error {
	if trail != nil {
		if trail.MaxPoints < 0 {
			return errors.New("editor: trail maxPoints must be non-negative")
		}
		if !isFiniteFloat32(trail.Lifetime) || trail.Lifetime < 0 {
			return errors.New("editor: trail lifetime must be finite and non-negative")
		}
		if !isFiniteFloat32(trail.MinDistance) || trail.MinDistance < 0 {
			return errors.New("editor: trail minDistance must be finite and non-negative")
		}
		if !isFiniteFloat32(trail.Width) || trail.Width < 0 {
			return errors.New("editor: trail width must be finite and non-negative")
		}
	}
	return e.EditParticleEmitter("Set particle trail", func(cfg *particles.Config) error {
		if trail == nil {
			cfg.Trail = nil
			return nil
		}
		cp := *trail
		cp.WidthCurve = slices.Clone(trail.WidthCurve)
		cp.ColorGradient = slices.Clone(trail.ColorGradient)
		cfg.Trail = &cp
		return nil
	})
}

// AddParticleSubEmitter appends a deep copy of rule to Config.SubEmitters; BurstCount <= 0 defaults to one child per trigger and InheritVelocity is a parent-velocity multiplier.
func (e *Editor) AddParticleSubEmitter(rule particles.SubEmitterRule) error {
	return e.EditParticleEmitter("Add sub-emitter", func(cfg *particles.Config) error {
		r := rule
		r.Config = cloneParticleConfig(rule.Config)
		cfg.SubEmitters = append(cfg.SubEmitters, r)
		return nil
	})
}

// SetParticleSubEmitter sets Trigger, BurstCount, and InheritVelocity on Config.SubEmitters[index]; index must exist, burstCount must be non-negative, and inheritVelocity must be a finite parent-velocity multiplier.
func (e *Editor) SetParticleSubEmitter(index int, trigger particles.SubEmitterTrigger, burstCount int, inheritVelocity float32) error {
	if !isFiniteFloat32(inheritVelocity) {
		return errors.New("editor: sub-emitter inheritVelocity must be finite")
	}
	if burstCount < 0 {
		return errors.New("editor: sub-emitter burstCount must be non-negative")
	}
	return e.EditParticleEmitter("Edit sub-emitter", func(cfg *particles.Config) error {
		if index < 0 || index >= len(cfg.SubEmitters) {
			return fmt.Errorf("editor: sub-emitter index %d out of range", index)
		}
		cfg.SubEmitters[index].Trigger = trigger
		cfg.SubEmitters[index].BurstCount = burstCount
		cfg.SubEmitters[index].InheritVelocity = inheritVelocity
		return nil
	})
}

// RemoveParticleSubEmitter removes Config.SubEmitters[index]; index must be an existing zero-based sub-emitter index.
func (e *Editor) RemoveParticleSubEmitter(index int) error {
	return e.EditParticleEmitter("Remove sub-emitter", func(cfg *particles.Config) error {
		if index < 0 || index >= len(cfg.SubEmitters) {
			return fmt.Errorf("editor: sub-emitter index %d out of range", index)
		}
		cfg.SubEmitters = slices.Delete(cfg.SubEmitters, index, index+1)
		if len(cfg.SubEmitters) == 0 {
			cfg.SubEmitters = nil
		}
		return nil
	})
}

// ---- Curve editing ----

// ParticleActiveCurve returns the selected ParticleCurveKind, defaulting to ParticleCurveSize when no emitter is open or no curve is selected.
func (e *Editor) ParticleActiveCurve() ParticleCurveKind {
	if e == nil || e.particles == nil || e.particles.activeCurve == "" {
		return ParticleCurveSize
	}
	return e.particles.activeCurve
}

// SetParticleActiveCurve selects one of the ParticleCurve* kinds and clears the selected curve stop.
func (e *Editor) SetParticleActiveCurve(kind ParticleCurveKind) {
	if e == nil || e.particles == nil {
		return
	}
	if e.particles.activeCurve == kind {
		return
	}
	e.cancelParticleCurveDrag()
	e.particles.activeCurve = kind
	e.particles.selectedStop = -1
	e.notify()
}

// ParticleSelectedStop returns the selected zero-based curve stop index, or -1 when no stop is selected.
func (e *Editor) ParticleSelectedStop() int {
	if e == nil || e.particles == nil {
		return -1
	}
	return e.particles.selectedStop
}

// SelectParticleStop selects a zero-based curve stop index; -1 clears the selection.
func (e *Editor) SelectParticleStop(index int) {
	if e == nil || e.particles == nil {
		return
	}
	e.particles.selectedStop = index
	e.notify()
}

// SetParticleSizeCurve sets Config.SizeCurve, sorting stops by normalized age T in [0, 1]; values are scale multipliers and an empty curve scales by 1.
func (e *Editor) SetParticleSizeCurve(c particles.FloatCurve) error {
	return e.EditParticleEmitter("Set size curve", func(cfg *particles.Config) error {
		cfg.SizeCurve = sortFloatCurve(c)
		return nil
	})
}

// SetParticleAlphaCurve sets Config.AlphaCurve, sorting stops by normalized age T in [0, 1]; values are opacity multipliers and an empty curve keeps opacity at 1.
func (e *Editor) SetParticleAlphaCurve(c particles.FloatCurve) error {
	return e.EditParticleEmitter("Set alpha curve", func(cfg *particles.Config) error {
		cfg.AlphaCurve = sortFloatCurve(c)
		return nil
	})
}

// SetParticleRotationCurve sets Config.RotationCurve, sorting stops by normalized age T in [0, 1]; values are added radians and an empty curve adds 0.
func (e *Editor) SetParticleRotationCurve(c particles.FloatCurve) error {
	return e.EditParticleEmitter("Set rotation curve", func(cfg *particles.Config) error {
		cfg.RotationCurve = sortFloatCurve(c)
		return nil
	})
}

// SetParticleColorCurve sets Config.ColorCurve, sorting stops by normalized age T in [0, 1]; values are RGBA colors and an empty curve keeps opaque white.
func (e *Editor) SetParticleColorCurve(c particles.ColorCurve) error {
	return e.EditParticleEmitter("Set color curve", func(cfg *particles.Config) error {
		cfg.ColorCurve = sortColorCurve(c)
		return nil
	})
}

func floatCurveRef(cfg *particles.Config, kind ParticleCurveKind) (*particles.FloatCurve, error) {
	switch kind {
	case ParticleCurveSize:
		return &cfg.SizeCurve, nil
	case ParticleCurveAlpha:
		return &cfg.AlphaCurve, nil
	case ParticleCurveRotation:
		return &cfg.RotationCurve, nil
	case ParticleCurveTrailWidth:
		if cfg.Trail == nil {
			return nil, errors.New("editor: trail is not enabled")
		}
		return &cfg.Trail.WidthCurve, nil
	default:
		return nil, fmt.Errorf("editor: %q is not a float curve", kind)
	}
}

func colorCurveRef(cfg *particles.Config, kind ParticleCurveKind) (*particles.ColorCurve, error) {
	switch kind {
	case ParticleCurveColor:
		return &cfg.ColorCurve, nil
	case ParticleCurveTrailColor:
		if cfg.Trail == nil {
			return nil, errors.New("editor: trail is not enabled")
		}
		return &cfg.Trail.ColorGradient, nil
	default:
		return nil, fmt.Errorf("editor: %q is not a color curve", kind)
	}
}

func sortFloatCurve(c particles.FloatCurve) particles.FloatCurve {
	if len(c) == 0 {
		return nil
	}
	out := slices.Clone(c)
	slices.SortStableFunc(out, func(a, b particles.FloatStop) int {
		if a.T < b.T {
			return -1
		}
		if a.T > b.T {
			return 1
		}
		return 0
	})
	return out
}

func sortColorCurve(c particles.ColorCurve) particles.ColorCurve {
	if len(c) == 0 {
		return nil
	}
	out := slices.Clone(c)
	slices.SortStableFunc(out, func(a, b particles.ColorStop) int {
		if a.T < b.T {
			return -1
		}
		if a.T > b.T {
			return 1
		}
		return 0
	})
	return out
}

// AddParticleFloatStop inserts a control point (t, val) into the named float curve in T order and returns its index.
func (e *Editor) AddParticleFloatStop(kind ParticleCurveKind, t, val float32) (int, error) {
	if !isFiniteFloat32(t) || !isFiniteFloat32(val) {
		return -1, errors.New("editor: curve stop must be finite")
	}
	t = clamp01f32(t)
	if kind == ParticleCurveAlpha {
		val = clamp01f32(val)
	}
	inserted := -1
	err := e.EditParticleEmitter("Add curve stop", func(cfg *particles.Config) error {
		ref, err := floatCurveRef(cfg, kind)
		if err != nil {
			return err
		}
		stop := particles.FloatStop{T: t, Value: val}
		idx := len(*ref)
		for i, s := range *ref {
			if t < s.T {
				idx = i
				break
			}
		}
		*ref = slices.Insert(*ref, idx, stop)
		inserted = idx
		return nil
	})
	if err == nil && e.particles != nil {
		e.particles.selectedStop = inserted
		e.notify()
	}
	return inserted, err
}

// SetParticleFloatStop updates the control point at index in the named float curve, keeping stops sorted by T, and returns its new index.
func (e *Editor) SetParticleFloatStop(kind ParticleCurveKind, index int, t, val float32) (int, error) {
	if !isFiniteFloat32(t) || !isFiniteFloat32(val) {
		return -1, errors.New("editor: curve stop must be finite")
	}
	t = clamp01f32(t)
	if kind == ParticleCurveAlpha {
		val = clamp01f32(val)
	}
	newIdx := index
	err := e.EditParticleEmitter("Move curve stop", func(cfg *particles.Config) error {
		ref, err := floatCurveRef(cfg, kind)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(*ref) {
			return fmt.Errorf("editor: curve stop %d out of range", index)
		}
		stops := slices.Delete(slices.Clone(*ref), index, index+1)
		stop := particles.FloatStop{T: t, Value: val}
		idx := len(stops)
		for i, s := range stops {
			if t < s.T {
				idx = i
				break
			}
		}
		*ref = slices.Insert(stops, idx, stop)
		newIdx = idx
		return nil
	})
	if err == nil && e.particles != nil {
		e.particles.selectedStop = newIdx
		e.notify()
	}
	return newIdx, err
}

// RemoveParticleFloatStop removes the control point at index from the named float curve.
func (e *Editor) RemoveParticleFloatStop(kind ParticleCurveKind, index int) error {
	err := e.EditParticleEmitter("Remove curve stop", func(cfg *particles.Config) error {
		ref, err := floatCurveRef(cfg, kind)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(*ref) {
			return fmt.Errorf("editor: curve stop %d out of range", index)
		}
		*ref = slices.Delete(*ref, index, index+1)
		if len(*ref) == 0 {
			*ref = nil
		}
		return nil
	})
	if err == nil && e.particles != nil {
		e.particles.selectedStop = -1
		e.notify()
	}
	return err
}

// AddParticleColorStop inserts a color stop (t, col) into the named color curve in T order and returns its index.
func (e *Editor) AddParticleColorStop(kind ParticleCurveKind, t float32, col core.Color) (int, error) {
	if !isFiniteFloat32(t) {
		return -1, errors.New("editor: color stop time must be finite")
	}
	t = clamp01f32(t)
	inserted := -1
	err := e.EditParticleEmitter("Add color stop", func(cfg *particles.Config) error {
		ref, err := colorCurveRef(cfg, kind)
		if err != nil {
			return err
		}
		stop := particles.ColorStop{T: t, Color: col}
		idx := len(*ref)
		for i, s := range *ref {
			if t < s.T {
				idx = i
				break
			}
		}
		*ref = slices.Insert(*ref, idx, stop)
		inserted = idx
		return nil
	})
	if err == nil && e.particles != nil {
		e.particles.selectedStop = inserted
		e.notify()
	}
	return inserted, err
}

// SetParticleColorStop updates the color stop at index in the named color curve, keeping stops sorted by T, and returns its new index.
func (e *Editor) SetParticleColorStop(kind ParticleCurveKind, index int, t float32, col core.Color) (int, error) {
	if !isFiniteFloat32(t) {
		return -1, errors.New("editor: color stop time must be finite")
	}
	t = clamp01f32(t)
	newIdx := index
	err := e.EditParticleEmitter("Edit color stop", func(cfg *particles.Config) error {
		ref, err := colorCurveRef(cfg, kind)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(*ref) {
			return fmt.Errorf("editor: color stop %d out of range", index)
		}
		stops := slices.Delete(slices.Clone(*ref), index, index+1)
		stop := particles.ColorStop{T: t, Color: col}
		idx := len(stops)
		for i, s := range stops {
			if t < s.T {
				idx = i
				break
			}
		}
		*ref = slices.Insert(stops, idx, stop)
		newIdx = idx
		return nil
	})
	if err == nil && e.particles != nil {
		e.particles.selectedStop = newIdx
		e.notify()
	}
	return newIdx, err
}

// RemoveParticleColorStop removes the color stop at index from the named color curve.
func (e *Editor) RemoveParticleColorStop(kind ParticleCurveKind, index int) error {
	err := e.EditParticleEmitter("Remove color stop", func(cfg *particles.Config) error {
		ref, err := colorCurveRef(cfg, kind)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(*ref) {
			return fmt.Errorf("editor: color stop %d out of range", index)
		}
		*ref = slices.Delete(*ref, index, index+1)
		if len(*ref) == 0 {
			*ref = nil
		}
		return nil
	})
	if err == nil && e.particles != nil {
		e.particles.selectedStop = -1
		e.notify()
	}
	return err
}

// Interactive curve canvas drag (updates live preview on move, commits single Undo step on release).
func (e *Editor) beginParticleCurveDrag(kind ParticleCurveKind, index int) {
	if e == nil || e.particles == nil || e.playing {
		return
	}
	e.particles.selectedStop = index
	e.particles.curveDrag = &particleCurveDrag{
		kind:       kind,
		index:      index,
		beforeRoot: cloneParticleConfig(e.particles.cfg),
	}
	e.notify()
}

func (e *Editor) updateParticleCurveDrag(t, val float32) {
	if e == nil || e.particles == nil || e.particles.curveDrag == nil {
		return
	}
	if !isFiniteFloat32(t) || !isFiniteFloat32(val) {
		return
	}
	d := e.particles.curveDrag
	target := resolveParticleTarget(&e.particles.cfg, e.particles.subPath)
	if target == nil {
		return
	}
	ref, err := floatCurveRef(target, d.kind)
	if err != nil || d.index < 0 || d.index >= len(*ref) {
		return
	}
	t = clamp01f32(t)
	if d.kind == ParticleCurveAlpha {
		val = clamp01f32(val)
	}
	stops := slices.Delete(slices.Clone(*ref), d.index, d.index+1)
	stop := particles.FloatStop{T: t, Value: val}
	idx := len(stops)
	for i, s := range stops {
		if t < s.T {
			idx = i
			break
		}
	}
	*ref = slices.Insert(stops, idx, stop)
	d.index = idx
	d.changed = true
	e.particles.selectedStop = idx
	e.particles.dirtyValid = false
	e.syncParticlePreviewConfig()
	e.notify()
}

func (e *Editor) endParticleCurveDrag() {
	if e == nil || e.particles == nil || e.particles.curveDrag == nil {
		return
	}
	d := e.particles.curveDrag
	e.particles.curveDrag = nil
	if !d.changed {
		return
	}
	after := cloneParticleConfig(e.particles.cfg)
	if reflect.DeepEqual(d.beforeRoot, after) {
		return
	}
	e.History.Execute(&particleEditCmd{
		ed:     e,
		label:  "Edit curve stop",
		before: d.beforeRoot,
		after:  after,
	})
}

func (e *Editor) cancelParticleCurveDrag() {
	if e == nil || e.particles == nil || e.particles.curveDrag == nil {
		return
	}
	d := e.particles.curveDrag
	e.particles.curveDrag = nil
	e.particles.cfg = d.beforeRoot
	e.particles.dirtyValid = false
	e.syncParticlePreviewConfig()
	e.notify()
}

// ---- Hex color helpers for UI ----

func formatParticleHexColor(c core.Color) string {
	return fmt.Sprintf("#%08x", uint32(c))
}

func parseParticleHexColor(s string) (core.Color, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return 0, fmt.Errorf("editor: color %q must start with #", s)
	}
	h := s[1:]
	if len(h) != 6 && len(h) != 8 {
		return 0, fmt.Errorf("editor: color %q must be #rrggbb or #rrggbbaa", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("editor: invalid hex color %q: %w", s, err)
	}
	if len(h) == 6 {
		return core.Color((uint32(v) << 8) | 0xff), nil
	}
	return core.Color(uint32(v)), nil
}

// ---- Scene integration: placing emitters on objects, hot-reloading, and edit-viewport preview ----

// SetObjectParticles binds objectID to a 2D particle emitter YAML file and optional atlas (or clears it when ref is nil) through History.
func (e *Editor) SetObjectParticles(objectID string, ref *sceneformat.ParticlesRef) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if ref != nil {
		if ref.File == "" || !fs.ValidPath(ref.File) || ref.File == "." {
			return fmt.Errorf("editor: particles file %q must be a slash-separated path relative to the project root", ref.File)
		}
		if e.Resolver != nil {
			if pr, ok := e.Resolver.(sceneformat.ParticlesResolver); ok {
				if _, err := pr.Particles(ref.File); err != nil {
					return fmt.Errorf("editor: load particles %q: %w", ref.File, err)
				}
			} else if fr, ok := e.Resolver.(sceneformat.FileResolver); ok {
				data, err := fr.ReadFile(ref.File)
				if err != nil {
					return fmt.Errorf("editor: read particles %q: %w", ref.File, err)
				}
				if _, err := particles.UnmarshalConfigYAML(data); err != nil {
					return fmt.Errorf("editor: parse particles %q: %w", ref.File, err)
				}
			} else {
				return errors.New("editor: the resolver cannot load particle files (it does not implement sceneformat.ParticlesResolver or sceneformat.FileResolver)")
			}
			if ref.Atlas != "" {
				atlas, err := e.Resolver.Atlas(ref.Atlas)
				if err != nil || atlas == nil {
					return fmt.Errorf("editor: resolve atlas %q: %w", ref.Atlas, err)
				}
			}
		}
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Set particles on "+objectID, 2, func(ov *sceneformat.ObjectOverride, resolved, base *sceneformat.ObjectDoc) error {
				if particlesRefEqual(resolved.Particles, ref) {
					return nil
				}
				if ref == nil {
					if base != nil && base.Particles != nil {
						return fmt.Errorf("editor: cannot clear inherited particles on %q; revert override or edit the source scene", objectID)
					}
					ov.Particles = nil
					return nil
				}
				ov.Particles = cloneParticlesRef(ref)
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	if particlesRefEqual(obj.Particles, ref) {
		return nil
	}
	e.History.Execute(&setParticlesCmd{
		ed:          e,
		id:          objectID,
		old:         cloneParticlesRef(obj.Particles),
		new:         cloneParticlesRef(ref),
		prevVersion: e.Doc.Version,
	})
	return nil
}

type setParticlesCmd struct {
	ed          *Editor
	id          string
	old         *sceneformat.ParticlesRef
	new         *sceneformat.ParticlesRef
	prevVersion int
}

func (c *setParticlesCmd) Label() string { return "Set particles of " + c.id }

func (c *setParticlesCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		if c.new != nil && c.ed.Doc.Version < sceneformat.CurrentVersion {
			c.ed.Doc.Version = sceneformat.CurrentVersion
		}
		obj.Particles = cloneParticlesRef(c.new)
		c.ed.Status = "Updated " + c.id
	}
}

func (c *setParticlesCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Particles = cloneParticlesRef(c.old)
		c.ed.Doc.Version = c.prevVersion
	}
}

// PlaceParticles places a new object with the given particle emitter file and optional atlas at (x, y) on layer (ActiveLayer when empty) as an undoable command.
func (e *Editor) PlaceParticles(layer, file, atlas string, x, y float32) (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	if layer == "" {
		layer = e.ActiveLayer
	}
	name := path.Base(file)
	for _, suf := range []string{".particles.yaml", ".particles.yml", ".emitter.yaml", ".emitter.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(strings.ToLower(name), suf) {
			name = name[:len(name)-len(suf)]
			break
		}
	}
	return e.PlaceObject(layer, sceneformat.ObjectDoc{
		Name: name,
		X:    x,
		Y:    y,
		Particles: &sceneformat.ParticlesRef{
			File:  file,
			Atlas: atlas,
		},
	})
}

func (e *Editor) canLoadParticles() bool {
	if e == nil || e.Resolver == nil {
		return false
	}
	_, hasPR := e.Resolver.(sceneformat.ParticlesResolver)
	_, hasFR := e.Resolver.(sceneformat.FileResolver)
	return hasPR || hasFR
}

func (e *Editor) projectParticles() []string {
	files, err := e.ProjectFiles()
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files {
		if !f.Dir && isParticleFilePath(f.Path) {
			out = append(out, f.Path)
		}
	}
	return out
}

func particlesRefEqual(a, b *sceneformat.ParticlesRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func cloneParticlesRef(p *sceneformat.ParticlesRef) *sceneformat.ParticlesRef {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func (e *Editor) followParticlesMove(moveFile func(*string)) {
	if e == nil || e.particles == nil {
		return
	}
	moveFile(&e.particles.path)
}

func (e *Editor) hotReloadSceneEmitters(relPath string, cfg particles.Config) {
	if e == nil || e.Built == nil {
		return
	}
	for _, l := range e.Built.Layers {
		for _, bo := range l.Emitters {
			if bo.Doc != nil && bo.Doc.Particles != nil && bo.Doc.Particles.File == relPath && bo.Emitter != nil {
				x, y, rot := bo.Emitter.X, bo.Emitter.Y, bo.Emitter.Rotation
				bo.Emitter.ReloadConfig(cfg)
				bo.Emitter.X, bo.Emitter.Y, bo.Emitter.Rotation = x, y, rot
			}
		}
	}
}

func (e *Editor) sceneHasBuiltEmitters() bool {
	if e == nil || e.Built == nil {
		return false
	}
	for _, l := range e.Built.Layers {
		if len(l.Emitters) > 0 {
			return true
		}
	}
	return false
}

// StepSceneEmitters advances every placed particle emitter in the edit BuiltScene by dt so the editor viewport previews them live without entering Play mode.
func (e *Editor) StepSceneEmitters(dt time.Duration) {
	if e == nil || e.playing || e.Built == nil || dt <= 0 {
		return
	}
	if !e.sceneHasBuiltEmitters() {
		return
	}
	for _, l := range e.Built.Layers {
		for _, bo := range l.Emitters {
			if bo.Emitter != nil {
				bo.Emitter.Update(dt)
			}
		}
	}
	e.notify()
}

func (e *Editor) syncSceneEmitterLoop() {
	if e == nil {
		return
	}
	if e.playing || e.scope == nil || !e.sceneHasBuiltEmitters() {
		e.stopSceneEmitterLoop()
		return
	}
	if e.sceneEmitterStop != nil {
		return
	}
	loop := &game.Loop{
		Rate: e.PlayRate,
		Update: func(dt time.Duration) {
			if e.playing || !e.sceneHasBuiltEmitters() {
				return
			}
			e.StepSceneEmitters(dt)
		},
	}
	e.scope.Run(func() {
		e.sceneEmitterStop = loop.Run(e.scope)
	})
}

func (e *Editor) stopSceneEmitterLoop() {
	if e == nil || e.sceneEmitterStop == nil {
		return
	}
	e.sceneEmitterStop()
	e.sceneEmitterStop = nil
}

// drawParticleEmitterOverlays draws emitter origin markers and shape outlines in the 2D edit viewport so placed emitters are visible and easy to pick even when paused or empty.
func (e *Editor) drawParticleEmitterOverlays(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e == nil || d == nil || scene == nil || e.playing {
		return
	}
	for _, layer := range scene.Layers {
		if !layer.Visible || len(layer.Emitters) == 0 {
			continue
		}
		m := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY)
		for _, bo := range layer.Emitters {
			if bo == nil || bo.Emitter == nil || viewFlagged(e.hidden, bo) {
				continue
			}
			p := m.TransformPoint(math2d.Vec2{X: bo.Emitter.X, Y: bo.Emitter.Y})
			cx, cy := p.X, p.Y
			spec := e.activePalette()
			d.Save()
			d.SetStrokeColor(ui.Alpha(spec.Warning.Fg, 0.9)).SetLineWidth(1.5)
			// Draw small diamond origin marker.
			const r float32 = 6
			d.BeginPath().
				MoveTo(cx, cy-r).
				LineTo(cx+r, cy).
				LineTo(cx, cy+r).
				LineTo(cx-r, cy).
				ClosePath().Stroke()
			// Draw spawn shape preview scaled by view zoom.
			zoom := strokeScale(m)
			sh := bo.Emitter.Shape
			d.SetStrokeColor(ui.Alpha(spec.Warning.Fg, 0.45)).SetLineDash(4, 3).SetLineWidth(1)
			switch sh.Kind() {
			case "circle", "ring":
				if rad := sh.Radius() * zoom; rad > 1 {
					d.BeginPath().Arc(cx, cy, rad, 0, 2*math.Pi, false).Stroke()
				}
			case "rect":
				w, h := sh.Width()*zoom, sh.Height()*zoom
				if w > 1 || h > 1 {
					d.BeginPath().Rect(cx-w/2, cy-h/2, w, h).Stroke()
				}
			}
			d.Restore()
		}
	}
}
