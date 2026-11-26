package editor

import (
	"errors"
	"fmt"
	"image"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/anim3d"
	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	modelPreviewMaxRenderDim = 320
	modelPreviewMinRenderDim = 32
	modelPreviewDefaultW     = 240
	modelPreviewDefaultH     = 180
)

// ModelNodeInfo describes one glTF node shown in the model import preview.
type ModelNodeInfo struct {
	// Index is the 0-based index of the node in gltf.Document.Nodes.
	Index int
	// Name is the authored glTF node name (may be empty).
	Name string
	// Parent is the parent node index, or -1 for a root node.
	Parent int
	// Depth is the nesting depth in the scene graph (0 for root nodes).
	Depth int
	// Mesh is the referenced mesh index, or -1 when the node has no mesh.
	Mesh int
	// MeshName is the referenced mesh's name (or empty).
	MeshName string
	// Skin is the referenced skin index, or -1 when unskinned.
	Skin int
	// Triangles is the triangle count of the node's mesh, or 0 when Mesh < 0.
	Triangles int
	// Bounds is the world-space axis-aligned bounding box of this node's mesh in rest pose, or empty when Mesh < 0.
	Bounds math3d.AABB
}

// DisplayName returns Name when non-empty, or "Node <Index>".
func (n ModelNodeInfo) DisplayName() string {
	if n.Name != "" {
		return n.Name
	}
	return fmt.Sprintf("Node %d", n.Index)
}

// ModelMeshInfo describes one glTF mesh shown in the model import preview.
type ModelMeshInfo struct {
	// Index is the 0-based index of the mesh in gltf.Document.Meshes.
	Index int
	// Name is the authored glTF mesh name (may be empty).
	Name string
	// Primitives is the number of primitives in the mesh.
	Primitives int
	// Triangles is the total triangle count across all triangle primitives of the mesh.
	Triangles int
	// Vertices is the total vertex count across all primitives of the mesh.
	Vertices int
	// MorphTargets is the maximum number of morph targets on any primitive of the mesh.
	MorphTargets int
	// Bounds is the local-space axis-aligned bounding box of the mesh.
	Bounds math3d.AABB
}

// DisplayName returns Name when non-empty, or "Mesh <Index>".
func (m ModelMeshInfo) DisplayName() string {
	if m.Name != "" {
		return m.Name
	}
	return fmt.Sprintf("Mesh %d", m.Index)
}

// ModelMaterialInfo describes one glTF material shown in the model import preview.
type ModelMaterialInfo struct {
	// Index is the 0-based index of the material in gltf.Document.Materials.
	Index int
	// Name is the authored glTF material name (may be empty).
	Name string
	// AlphaMode is the glTF alpha mode (OPAQUE, MASK, or BLEND).
	AlphaMode gltf.AlphaMode
	// Metallic and Roughness are the PBR metallic-roughness factors.
	Metallic, Roughness float32
	// DoubleSided reports whether back-face culling is disabled.
	DoubleSided bool
	// Unlit reports whether KHR_materials_unlit is enabled.
	Unlit bool
	// HasBaseColorTexture reports whether the material binds a base color texture.
	HasBaseColorTexture bool
	// BaseColor is the linear RGBA base color factor.
	BaseColor math3d.Vec4
}

// DisplayName returns Name when non-empty, or "Material <Index>".
func (m ModelMaterialInfo) DisplayName() string {
	if m.Name != "" {
		return m.Name
	}
	return fmt.Sprintf("Material %d", m.Index)
}

// ModelAnimationInfo describes one glTF animation clip shown in the model import preview.
type ModelAnimationInfo struct {
	// Index is the 0-based index of the animation in gltf.Document.Animations.
	Index int
	// Name is the authored glTF animation name (may be empty).
	Name string
	// Duration is the clip's duration.
	Duration time.Duration
	// Channels is the number of animation channels in the clip.
	Channels int
}

// DisplayName returns Name when non-empty, or "Animation <Index>".
func (a ModelAnimationInfo) DisplayName() string {
	if a.Name != "" {
		return a.Name
	}
	return fmt.Sprintf("Animation %d", a.Index)
}

// ModelPreviewInfo summarizes a glTF or GLB model's contents, geometry counts,
// and bounds (both unscaled and at the effective import scale).
type ModelPreviewInfo struct {
	// Path is the clean slash-separated model path relative to the project root.
	Path string
	// Scale is the effective import scale from game.yaml or the sidecar.
	Scale float64
	// Bounds is the axis-aligned bounding box of the model in unscaled space.
	Bounds math3d.AABB
	// ScaledBounds is Bounds multiplied by Scale.
	ScaledBounds math3d.AABB
	// TotalTriangles is the total rendered triangle count across the model's active scene.
	TotalTriangles int
	// TotalVertices is the total rendered vertex count across the model's active scene.
	TotalVertices int

	Nodes      []ModelNodeInfo
	Meshes     []ModelMeshInfo
	Materials  []ModelMaterialInfo
	Animations []ModelAnimationInfo
}

type modelPreviewPartSource struct {
	drawableIdx int
	partIdx     int
	nodeIdx     int
	meshIdx     int
	primIdx     int
	uvRef       *gltf.TextureRef
	skinned     bool
	morphed     bool
}

type modelPreviewState struct {
	// File cache key
	path        string
	projectPath string
	modTime     time.Time
	size        int64
	assetsRev   uint64
	importRev   uint64
	loadRev     uint64
	loadErr     error

	doc    *gltf.Document
	info   ModelPreviewInfo
	player *anim3d.Player
	scale  float64

	// Orbit camera state
	camera  OrbitCamera3D
	camInit bool
	camPath string
	radius  float64

	// Animation playback state
	animIndex   int
	animTime    time.Duration
	playing     bool
	loop        bool
	loopInited  bool
	stopLoop    func()
	poseRev     uint64
	renderRev   uint64
	deformRev   uint64
	hasDeform   bool
	deformParts []modelPreviewPartSource

	// Scene graph and reusable software GPU renderer
	scene          *scene3d.Scene
	scaleRoot      *scene3d.Node
	sceneNodes     []*scene3d.Node
	built          *scenemodel.Built
	sr             *gpu.SoftwareRenderer
	srW, srH       int
	renderer       *scenemodel.Renderer
	rendererBuilds uint64
	renderErr      error

	// Last rendered RGBA snapshot
	rendered    *image.RGBA
	renderedRev uint64
	renderedW   int
	renderedH   int
	imageVer    atomic.Uint64
}

func (e *Editor) ensureModelPreviewLoopDefault() {
	if !e.modelPreview.loopInited {
		e.modelPreview.loop = true
		e.modelPreview.loopInited = true
	}
}

// stopModelPreviewLoop halts any active model preview animation timer without clearing the cached model or playhead.
func (e *Editor) stopModelPreviewLoop() {
	if e == nil {
		return
	}
	p := &e.modelPreview
	p.playing = false
	if p.stopLoop != nil {
		p.stopLoop()
		p.stopLoop = nil
	}
	if p.player != nil {
		p.player.Pause()
	}
}

// closeModelPreview stops playback and releases cached GPU resources held by the model import preview.
func (e *Editor) closeModelPreview() {
	if e == nil {
		return
	}
	e.stopModelPreviewLoop()
	p := &e.modelPreview
	p.closeGPU()
	p.path = ""
	p.projectPath = ""
	p.doc = nil
	p.player = nil
	p.scene = nil
	p.scaleRoot = nil
	p.sceneNodes = nil
	p.built = nil
	p.deformParts = nil
	p.rendered = nil
	p.loadErr = nil
	p.renderErr = nil
	p.animIndex = -1
	p.animTime = 0
	p.camInit = false
	p.camPath = ""
}

func (p *modelPreviewState) closeGPU() {
	if p.renderer != nil {
		_ = p.renderer.Close()
		p.renderer = nil
	}
	if p.sr != nil {
		p.sr.Close()
		p.sr = nil
		p.srW, p.srH = 0, 0
	}
}

type confinedDirFS struct {
	rootDir string
	fsys    fs.FS
}

func (c confinedDirFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name != "." {
		abs := filepath.Join(c.rootDir, filepath.FromSlash(name))
		if err := verifyFileWithinRoot(c.rootDir, abs, name); err != nil {
			return nil, err
		}
	}
	return c.fsys.Open(name)
}

func (p *modelPreviewState) setLoadError(clean, projectPath string, stat os.FileInfo, assetsRev, importRev uint64, effectiveScale float64, err error) {
	p.path = clean
	p.projectPath = projectPath
	p.modTime = stat.ModTime()
	p.size = stat.Size()
	p.assetsRev = assetsRev
	p.importRev = importRev
	p.loadRev++
	p.loadErr = err
	p.doc = nil
	p.player = nil
	p.scene = nil
	p.scaleRoot = nil
	p.sceneNodes = nil
	p.built = nil
	p.deformParts = nil
	p.hasDeform = false
	p.info = ModelPreviewInfo{Path: clean, Scale: effectiveScale}
	p.animIndex = -1
	p.animTime = 0
}

// loadModelPreview loads (or refreshes from cache) the glTF/GLB model at relPath for inspection and rendering.
func (e *Editor) loadModelPreview(relPath string) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	e.ensureModelPreviewLoopDefault()
	clean, err := normalizeProjectAssetPath(relPath)
	if err != nil {
		return err
	}
	if e.AssetImportKind(clean) != AssetImportModel {
		return fmt.Errorf("editor: %q is not an importable glTF model", relPath)
	}

	rootDir := filepath.Dir(e.ProjectPath)
	abs := filepath.Join(rootDir, filepath.FromSlash(clean))
	if err := verifyFileWithinRoot(rootDir, abs, relPath); err != nil {
		return err
	}
	stat, err := os.Stat(abs)
	if err != nil {
		return err
	}

	readEffectiveScale := func() float64 {
		effectiveScale := 1.0
		if importInfo, err := e.InspectAssetImport(clean); err == nil {
			if s, _ := importInfo.EffectiveModelScale(); s > 0 {
				effectiveScale = s
			}
		}
		return effectiveScale
	}

	p := &e.modelPreview
	sameFile := p.path == clean &&
		p.projectPath == e.ProjectPath &&
		p.size == stat.Size() &&
		p.modTime.Equal(stat.ModTime()) &&
		p.assetsRev == e.assetsRev

	if sameFile {
		if p.loadErr != nil {
			return p.loadErr
		}
		if p.importRev == e.importRev {
			return nil
		}
		effectiveScale := readEffectiveScale()
		p.importRev = e.importRev
		if p.scale != effectiveScale {
			p.scale = effectiveScale
			p.info.Scale = effectiveScale
			p.info.ScaledBounds = scaleAABB(p.info.Bounds, float32(effectiveScale))
			p.radius = modelBoundsRadius(p.info.ScaledBounds)
			if p.scaleRoot != nil {
				s := float32(effectiveScale)
				p.scaleRoot.SetLocal(math3d.Transform{
					Rot:   math3d.QuatIdentity(),
					Scale: math3d.Vec3{X: s, Y: s, Z: s},
				})
				p.scene.UpdateTransforms()
			}
			p.renderRev++
			p.rendered = nil
		}
		return nil
	}

	effectiveScale := readEffectiveScale()

	// File changed or first load: stop any previous animation loop and reload.
	if p.path != clean {
		e.stopModelPreviewLoop()
	}
	if p.renderer != nil {
		_ = p.renderer.Close()
		p.renderer = nil
	}
	p.rendered = nil
	p.renderErr = nil

	doc, err := gltf.LoadFS(confinedDirFS{rootDir: rootDir, fsys: os.DirFS(rootDir)}, clean)
	if err != nil {
		p.setLoadError(clean, e.ProjectPath, stat, e.assetsRev, e.importRev, effectiveScale, err)
		return err
	}

	player, err := anim3d.NewPlayer(doc)
	if err != nil {
		p.setLoadError(clean, e.ProjectPath, stat, e.assetsRev, e.importRev, effectiveScale, err)
		return err
	}
	player.SetLoop(p.loop)

	info := buildModelPreviewInfo(clean, doc, player, effectiveScale)
	scene, scaleRoot, sceneNodes, built, deformParts, err := buildModelPreviewScene(doc, player, effectiveScale)
	if err != nil {
		p.setLoadError(clean, e.ProjectPath, stat, e.assetsRev, e.importRev, effectiveScale, err)
		return err
	}

	p.path = clean
	p.projectPath = e.ProjectPath
	p.modTime = stat.ModTime()
	p.size = stat.Size()
	p.assetsRev = e.assetsRev
	p.importRev = e.importRev
	p.loadRev++
	p.loadErr = nil
	p.doc = doc
	p.info = info
	p.player = player
	p.scale = effectiveScale
	p.scene = scene
	p.scaleRoot = scaleRoot
	p.sceneNodes = sceneNodes
	p.built = built
	p.deformParts = deformParts
	p.hasDeform = len(deformParts) > 0
	p.radius = modelBoundsRadius(info.ScaledBounds)

	if len(doc.Animations) > 0 {
		if p.camPath != clean || p.animIndex < 0 || p.animIndex >= len(doc.Animations) {
			p.animIndex = 0
			p.animTime = 0
		}
	} else {
		p.animIndex = -1
		p.animTime = 0
		p.playing = false
	}

	if !p.camInit || p.camPath != clean {
		p.camera = frameModelPreviewCamera(info.ScaledBounds)
		p.camInit = true
		p.camPath = clean
	}

	p.poseRev++
	p.deformRev = p.poseRev
	p.renderRev++
	return nil
}

// InspectModelImport loads (or reads from cache) the glTF/GLB model at relPath and returns its node hierarchy, meshes, materials, animations, bounds, and triangle counts.
func (e *Editor) InspectModelImport(relPath string) (ModelPreviewInfo, error) {
	if err := e.loadModelPreview(relPath); err != nil {
		return ModelPreviewInfo{}, err
	}
	return cloneModelPreviewInfo(e.modelPreview.info), nil
}

func cloneModelPreviewInfo(src ModelPreviewInfo) ModelPreviewInfo {
	out := src
	if len(src.Nodes) > 0 {
		out.Nodes = append([]ModelNodeInfo(nil), src.Nodes...)
	}
	if len(src.Meshes) > 0 {
		out.Meshes = append([]ModelMeshInfo(nil), src.Meshes...)
	}
	if len(src.Materials) > 0 {
		out.Materials = append([]ModelMaterialInfo(nil), src.Materials...)
	}
	if len(src.Animations) > 0 {
		out.Animations = append([]ModelAnimationInfo(nil), src.Animations...)
	}
	return out
}

// ModelPreviewCamera returns the current orbit camera of the model import preview.
func (e *Editor) ModelPreviewCamera() OrbitCamera3D {
	if e == nil || !e.modelPreview.camInit {
		return DefaultOrbitCamera3D()
	}
	return e.modelPreview.camera
}

// ResetModelPreviewCamera frames the model preview orbit camera on relPath's bounds from the default angle.
func (e *Editor) ResetModelPreviewCamera(relPath string) {
	if e == nil {
		return
	}
	if relPath != "" {
		_ = e.loadModelPreview(relPath)
	}
	p := &e.modelPreview
	p.camera = frameModelPreviewCamera(p.info.ScaledBounds)
	p.camInit = true
	p.camPath = p.path
	p.renderRev++
	e.notify()
}

// OrbitModelPreview rotates the model preview orbit camera by dyaw and dpitch in radians.
func (e *Editor) OrbitModelPreview(dyaw, dpitch float32) {
	if e == nil || !isFiniteFloat32(dyaw) || !isFiniteFloat32(dpitch) {
		return
	}
	p := &e.modelPreview
	if !p.camInit {
		p.camera = frameModelPreviewCamera(p.info.ScaledBounds)
		p.camInit = true
	}
	p.camera.Orbit(dyaw, dpitch)
	p.renderRev++
	e.notify()
}

// PanModelPreview translates the model preview camera target parallel to the view plane by (dx, dy) pixels in a canvas canvasH pixels tall.
func (e *Editor) PanModelPreview(dx, dy, canvasH float32) {
	if e == nil || !isFiniteFloat32(dx) || !isFiniteFloat32(dy) {
		return
	}
	p := &e.modelPreview
	if !p.camInit {
		p.camera = frameModelPreviewCamera(p.info.ScaledBounds)
		p.camInit = true
	}
	if !isFiniteFloat32(canvasH) || canvasH <= 0 {
		canvasH = modelPreviewDefaultH
	}
	wpp := float32(p.camera.worldPerPixel(canvasH))
	p.camera.Pan(-dx*wpp, dy*wpp)
	p.updatePreviewClipPlanes()
	p.renderRev++
	e.notify()
}

// ZoomModelPreview scales the model preview camera distance by dividing by factor (>1 zooms in, <1 zooms out), clamped relative to the model's size so models of any scale stay comfortably inspectable.
func (e *Editor) ZoomModelPreview(factor float32) {
	if e == nil || !(factor > 0) || math.IsInf(float64(factor), 0) || math.IsNaN(float64(factor)) {
		return
	}
	p := &e.modelPreview
	if !p.camInit {
		p.camera = frameModelPreviewCamera(p.info.ScaledBounds)
		p.camInit = true
	}
	radius := p.radius
	if radius <= 0 {
		radius = 0.5
	}
	minDist := math.Max(radius*0.2, 1e-4)
	maxDist := math.Max(radius*60, 10)
	dist := float64(p.camera.Distance) / float64(factor)
	if dist < minDist {
		dist = minDist
	}
	if dist > maxDist {
		dist = maxDist
	}
	p.camera.Distance = float32(dist)
	p.updatePreviewClipPlanes()
	p.renderRev++
	e.notify()
}

func (p *modelPreviewState) updatePreviewClipPlanes() {
	radius := p.radius
	if radius <= 0 {
		radius = 0.5
	}
	dist := float64(p.camera.Distance)
	if dist <= 0 {
		dist = radius * 3
	}
	near := math.Max(dist-radius*4, dist*1e-3)
	if near < 1e-5 {
		near = 1e-5
	}
	far := math.Max(dist+radius*6, near+radius*4)
	p.camera.Near = float32(near)
	p.camera.Far = float32(far)
}

// ModelPreviewAnimation returns the 0-based index of the active animation in the model import preview, or -1 when the model has no animations.
func (e *Editor) ModelPreviewAnimation() int {
	if e == nil || e.modelPreview.doc == nil || len(e.modelPreview.doc.Animations) == 0 {
		return -1
	}
	return e.modelPreview.animIndex
}

// ModelPreviewPlaying reports whether the model import preview is actively playing an animation.
func (e *Editor) ModelPreviewPlaying() bool {
	return e != nil && e.modelPreview.playing
}

// ModelPreviewTime returns the current animation playhead timestamp in the model import preview.
func (e *Editor) ModelPreviewTime() time.Duration {
	if e == nil {
		return 0
	}
	return e.modelPreview.animTime
}

// ModelPreviewDuration returns the duration of the active animation in the model import preview, or 0 when none is active.
func (e *Editor) ModelPreviewDuration() time.Duration {
	if e == nil || e.modelPreview.player == nil {
		return 0
	}
	if e.modelPreview.animIndex < 0 || e.modelPreview.animIndex >= len(e.modelPreview.info.Animations) {
		return 0
	}
	return e.modelPreview.info.Animations[e.modelPreview.animIndex].Duration
}

// ModelPreviewLoop reports whether model preview animation playback wraps at the end of the clip.
func (e *Editor) ModelPreviewLoop() bool {
	if e == nil {
		return true
	}
	e.ensureModelPreviewLoopDefault()
	return e.modelPreview.loop
}

// SetModelPreviewLoop sets whether model preview animation playback loops.
func (e *Editor) SetModelPreviewLoop(loop bool) {
	if e == nil {
		return
	}
	e.ensureModelPreviewLoopDefault()
	if e.modelPreview.loop == loop {
		return
	}
	e.modelPreview.loop = loop
	if e.modelPreview.player != nil {
		e.modelPreview.player.SetLoop(loop)
	}
	e.notify()
}

// SelectModelPreviewAnimation selects animation index on relPath and evaluates its pose at time 0 (continuing playback if already playing, or remaining paused otherwise).
func (e *Editor) SelectModelPreviewAnimation(relPath string, index int) error {
	if err := e.loadModelPreview(relPath); err != nil {
		return err
	}
	p := &e.modelPreview
	if p.doc == nil || len(p.doc.Animations) == 0 {
		return errors.New("editor: model has no animations")
	}
	if index < 0 || index >= len(p.doc.Animations) {
		return fmt.Errorf("editor: animation index %d out of range (count %d)", index, len(p.doc.Animations))
	}
	wasPlaying := p.playing
	if err := p.player.Play(index); err != nil {
		return err
	}
	p.player.Seek(0)
	if !wasPlaying {
		p.player.Pause()
	}
	p.animIndex = index
	p.animTime = 0
	p.syncPoseToScene()
	e.notify()
	return nil
}

// PlayModelPreviewAnimation starts or resumes playback of animation index (or the active animation when index < 0) on relPath.
func (e *Editor) PlayModelPreviewAnimation(relPath string, index int) error {
	if e != nil && e.Playing() {
		return ErrPlaying
	}
	if err := e.loadModelPreview(relPath); err != nil {
		return err
	}
	p := &e.modelPreview
	if p.doc == nil || len(p.doc.Animations) == 0 {
		return errors.New("editor: model has no animations")
	}
	if index < 0 {
		index = p.animIndex
		if index < 0 {
			index = 0
		}
	}
	if index >= len(p.doc.Animations) {
		return fmt.Errorf("editor: animation index %d out of range (count %d)", index, len(p.doc.Animations))
	}
	e.ensureModelPreviewLoopDefault()
	p.player.SetLoop(p.loop)
	if err := p.player.Play(index); err != nil {
		return err
	}
	p.animIndex = index
	p.animTime = p.player.Time()
	p.playing = true
	p.syncPoseToScene()
	if cx := e.scope; cx != nil && p.stopLoop == nil {
		loop := &game.Loop{
			Rate:   e.PlayRate,
			Update: e.StepModelPreviewAnimation,
		}
		cx.Run(func() {
			if p.playing && p.stopLoop == nil && e.scope == cx && !e.Playing() {
				p.stopLoop = loop.Run(cx)
			}
		})
	}
	e.notify()
	return nil
}

// PauseModelPreviewAnimation halts model preview animation playback at the current playhead and pose.
func (e *Editor) PauseModelPreviewAnimation() {
	if e == nil || !e.modelPreview.playing {
		return
	}
	e.stopModelPreviewLoop()
	e.notify()
}

// StopModelPreviewAnimation halts model preview animation playback, resets the playhead to 0, and restores the model's bind pose.
func (e *Editor) StopModelPreviewAnimation() {
	if e == nil {
		return
	}
	e.stopModelPreviewLoop()
	p := &e.modelPreview
	if p.player != nil {
		p.player.Stop()
	}
	p.animTime = 0
	p.syncPoseToScene()
	e.notify()
}

// SeekModelPreviewAnimation scrubs the active model preview animation on relPath to timestamp t and evaluates the pose immediately.
func (e *Editor) SeekModelPreviewAnimation(relPath string, t time.Duration) error {
	if err := e.loadModelPreview(relPath); err != nil {
		return err
	}
	p := &e.modelPreview
	if p.doc == nil || len(p.doc.Animations) == 0 || p.player == nil {
		return errors.New("editor: model has no animations")
	}
	if p.animIndex < 0 || p.animIndex >= len(p.doc.Animations) {
		p.animIndex = 0
	}
	if p.player.AnimationIndex() != p.animIndex {
		wasPlaying := p.playing
		if err := p.player.Play(p.animIndex); err != nil {
			return err
		}
		if !wasPlaying {
			p.player.Pause()
		}
	}
	p.player.SetLoop(p.loop)
	p.player.Seek(t)
	p.animTime = p.player.Time()
	p.syncPoseToScene()
	e.notify()
	return nil
}

// StepModelPreviewAnimation advances active model preview animation playback by dt and updates the posed scene.
func (e *Editor) StepModelPreviewAnimation(dt time.Duration) {
	if e == nil {
		return
	}
	p := &e.modelPreview
	if !p.playing || p.player == nil || dt <= 0 {
		return
	}
	p.player.Step(dt)
	p.animTime = p.player.Time()
	p.syncPoseToScene()
	if !p.player.Playing() {
		e.stopModelPreviewLoop()
	}
	e.notify()
}

// ModelPreviewNodeWorldTransform returns the current evaluated world matrix of glTF node nodeIndex from the active model preview pose.
func (e *Editor) ModelPreviewNodeWorldTransform(nodeIndex int) (math3d.Mat4, bool) {
	if e == nil || e.modelPreview.player == nil || nodeIndex < 0 || nodeIndex >= e.modelPreview.player.NodeCount() {
		return math3d.Mat4Identity(), false
	}
	return e.modelPreview.player.WorldTransform(nodeIndex), true
}

func (p *modelPreviewState) syncPoseToScene() {
	if p.scene == nil || p.player == nil {
		return
	}
	if len(p.sceneNodes) > 0 {
		p.player.ApplyToNodes(p.sceneNodes)
	}
	p.scene.UpdateTransforms()
	p.poseRev++
	p.renderRev++
}

// RenderModelPreview renders relPath at width x height pixels with the current orbit camera, effective import scale, and active animation pose using NSGo's software GPU renderer.
func (e *Editor) RenderModelPreview(relPath string, width, height int) (*image.RGBA, error) {
	if err := e.loadModelPreview(relPath); err != nil {
		return nil, err
	}
	rw, rh := clampPreviewRenderSize(width, height)
	p := &e.modelPreview
	if p.rendered != nil && p.renderedRev == p.renderRev && p.renderedW == rw && p.renderedH == rh {
		return p.rendered, nil
	}

	if p.sr == nil || p.srW != rw || p.srH != rh {
		p.closeGPU()
		p.sr = gpu.NewSoftwareRenderer(rw, rh, viewport3DDepthFormat)
		p.srW, p.srH = rw, rh
	}

	if p.hasDeform && p.deformRev != p.poseRev {
		if err := p.updateDeformedMeshes(); err != nil {
			p.renderErr = err
			return nil, err
		}
		if p.renderer != nil {
			_ = p.renderer.Close()
			p.renderer = nil
		}
		p.deformRev = p.poseRev
	}

	dev := p.sr.Device()
	if p.renderer == nil && p.built != nil {
		r, err := scenemodel.NewRenderer(nil, dev, p.built, scenemodel.RendererOptions{
			Label:            "editor.model_preview",
			DepthFormat:      viewport3DDepthFormat,
			DisableOcclusion: true,
		})
		if err != nil {
			p.renderErr = err
			return nil, fmt.Errorf("editor: model preview renderer: %w", err)
		}
		p.renderer = r
		p.rendererBuilds++
	}

	if p.renderer != nil {
		cam := p.camera.SceneCamera(float32(rw), float32(rh))
		if _, err := p.renderer.Prepare(cam, DefaultLights3D()); err != nil {
			p.renderErr = err
			return nil, fmt.Errorf("editor: model preview prepare: %w", err)
		}
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: "editor.model_preview",
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: e.activeSpecPalette().Viewport,
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1,
		},
	})
	if p.renderer != nil {
		p.renderer.Draw(pass)
	}
	pass.End()
	dev.Submit(enc.Finish())
	p.sr.WaitIdle()
	if err := p.sr.LastError(); err != nil {
		p.renderErr = err
		return nil, fmt.Errorf("editor: model preview: %w", err)
	}

	src := p.sr.Image()
	out := image.NewRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	p.rendered = out
	p.renderedRev = p.renderRev
	p.renderedW = rw
	p.renderedH = rh
	p.renderErr = nil
	p.imageVer.Add(1)
	return out, nil
}

// DrawModelPreview draws the 3D orbit-camera model preview for relPath onto d, including a corner orientation triad.
func (e *Editor) DrawModelPreview(d *core.Drawing, size core.Size, relPath string) {
	cw, ch := float32(size.Width), float32(size.Height)
	if !isFiniteFloat32(cw) || !isFiniteFloat32(ch) || cw <= 0 || ch <= 0 {
		return
	}
	spec := e.activeSpecPalette()
	d.SetFillColor(spec.Viewport).FillRect(0, 0, cw, ch)
	if e == nil {
		return
	}
	img, err := e.RenderModelPreview(relPath, int(math.Round(float64(size.Width))), int(math.Round(float64(size.Height))))
	if err != nil || img == nil {
		return
	}
	iw, ih := float32(img.Bounds().Dx()), float32(img.Bounds().Dy())
	ver := e.modelPreview.imageVer.Load()
	d.DrawImageFrom(core.ImageSource{
		URI:     "model-preview:" + e.modelPreview.path,
		Pixels:  img,
		Version: ver,
	}, 0, 0, iw, ih, 0, 0, cw, ch)

	drawModelPreviewTriad(d, e.ModelPreviewCamera(), cw, ch, spec)
}

func drawModelPreviewTriad(d *core.Drawing, cam OrbitCamera3D, cw, ch float32, spec SpecPalette) {
	if cw < 48 || ch < 48 {
		return
	}
	right, up, _ := cam.Basis()
	ox, oy := float32(22), ch-22
	const axisLen float32 = 14
	axes := []struct {
		dir   [3]float32
		color core.Color
	}{
		{[3]float32{1, 0, 0}, spec.AxisX},
		{[3]float32{0, 1, 0}, spec.AxisY},
		{[3]float32{0, 0, 1}, spec.AxisZ},
	}
	for _, a := range axes {
		px := a.dir[0]*right[0] + a.dir[1]*right[1] + a.dir[2]*right[2]
		py := a.dir[0]*up[0] + a.dir[1]*up[1] + a.dir[2]*up[2]
		d.SetStrokeColor(a.color).SetLineWidth(1.75)
		d.BeginPath().MoveTo(ox, oy).LineTo(ox+px*axisLen, oy-py*axisLen).Stroke()
	}
}

func clampPreviewRenderSize(w, h int) (int, int) {
	if w <= 0 || h <= 0 {
		return modelPreviewDefaultW, modelPreviewDefaultH
	}
	if w > modelPreviewMaxRenderDim || h > modelPreviewMaxRenderDim {
		if w >= h {
			h = max(int(math.Round(float64(h)*float64(modelPreviewMaxRenderDim)/float64(w))), modelPreviewMinRenderDim)
			w = modelPreviewMaxRenderDim
		} else {
			w = max(int(math.Round(float64(w)*float64(modelPreviewMaxRenderDim)/float64(h))), modelPreviewMinRenderDim)
			h = modelPreviewMaxRenderDim
		}
	}
	if w < modelPreviewMinRenderDim {
		w = modelPreviewMinRenderDim
	}
	if h < modelPreviewMinRenderDim {
		h = modelPreviewMinRenderDim
	}
	return w, h
}

func scaleAABB(b math3d.AABB, s float32) math3d.AABB {
	if b.Empty() {
		return b
	}
	if s <= 0 {
		s = 1
	}
	return math3d.AABB{
		Min: b.Min.Scale(s),
		Max: b.Max.Scale(s),
	}
}

func modelBoundsRadius(b math3d.AABB) float64 {
	if b.Empty() {
		return 0.5
	}
	ext := b.Size()
	return math.Max(0.5*math.Sqrt(float64(ext.X*ext.X+ext.Y*ext.Y+ext.Z*ext.Z)), 1e-4)
}

func frameModelPreviewCamera(bounds math3d.AABB) OrbitCamera3D {
	if bounds.Empty() {
		bounds = math3d.AABB{
			Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5},
			Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5},
		}
	}
	c := bounds.Center()
	radius := modelBoundsRadius(bounds)
	fov := math.Pi / 4
	dist := radius / math.Sin(fov/2) * 1.25
	near := math.Max(dist-radius*4, dist*1e-3)
	if near < 1e-5 {
		near = 1e-5
	}
	far := math.Max(dist+radius*6, near+radius*4)
	cam := DefaultOrbitCamera3D()
	cam.Target = [3]float32{c.X, c.Y, c.Z}
	cam.FOV = float32(fov)
	cam.Distance = float32(dist)
	cam.Near = float32(near)
	cam.Far = float32(far)
	return cam
}

func modelPreviewSceneRoots(doc *gltf.Document) []int {
	if doc == nil {
		return nil
	}
	si := doc.Scene
	if si < 0 || si >= len(doc.Scenes) {
		si = 0
	}
	if si < len(doc.Scenes) {
		return doc.Scenes[si].Roots
	}
	var roots []int
	for i := range doc.Nodes {
		if doc.Nodes[i].Parent < 0 {
			roots = append(roots, i)
		}
	}
	return roots
}

func primitiveTriangleCount(p *gltf.Primitive) int {
	n := len(p.Indices)
	if n == 0 {
		n = len(p.Positions)
	}
	switch p.Mode {
	case gltf.ModeTriangles:
		return n / 3
	case gltf.ModeTriangleStrip, gltf.ModeTriangleFan:
		if n < 3 {
			return 0
		}
		return n - 2
	default:
		return 0
	}
}

func primitiveBounds(p *gltf.Primitive) math3d.AABB {
	if !p.Bounds.Empty() {
		return p.Bounds
	}
	b := math3d.AABBEmpty()
	for _, pos := range p.Positions {
		b = b.ExpandPoint(pos)
	}
	return b
}

func buildModelPreviewInfo(relPath string, doc *gltf.Document, player *anim3d.Player, scale float64) ModelPreviewInfo {
	info := ModelPreviewInfo{
		Path:   relPath,
		Scale:  scale,
		Bounds: math3d.AABBEmpty(),
	}

	// Meshes
	meshInfos := make([]ModelMeshInfo, len(doc.Meshes))
	for i := range doc.Meshes {
		gm := &doc.Meshes[i]
		b := math3d.AABBEmpty()
		tris := 0
		verts := 0
		morphs := 0
		for pi := range gm.Primitives {
			prim := &gm.Primitives[pi]
			tris += primitiveTriangleCount(prim)
			verts += len(prim.Positions)
			if len(prim.Targets) > morphs {
				morphs = len(prim.Targets)
			}
			pb := primitiveBounds(prim)
			if !pb.Empty() {
				b = b.Union(pb)
			}
		}
		meshInfos[i] = ModelMeshInfo{
			Index:        i,
			Name:         gm.Name,
			Primitives:   len(gm.Primitives),
			Triangles:    tris,
			Vertices:     verts,
			MorphTargets: morphs,
			Bounds:       b,
		}
	}
	info.Meshes = meshInfos

	// Materials
	matInfos := make([]ModelMaterialInfo, len(doc.Materials))
	for i := range doc.Materials {
		gm := &doc.Materials[i]
		mode := gm.AlphaMode
		if mode == "" {
			mode = gltf.AlphaOpaque
		}
		matInfos[i] = ModelMaterialInfo{
			Index:               i,
			Name:                gm.Name,
			AlphaMode:           mode,
			Metallic:            gm.MetallicFactor,
			Roughness:           gm.RoughnessFactor,
			DoubleSided:         gm.DoubleSided,
			Unlit:               gm.Unlit,
			HasBaseColorTexture: gm.BaseColorTexture != nil,
			BaseColor:           gm.BaseColorFactor,
		}
	}
	info.Materials = matInfos

	// Animations
	animInfos := make([]ModelAnimationInfo, len(doc.Animations))
	for i := range doc.Animations {
		ga := &doc.Animations[i]
		durSec := ga.Duration
		for ci := range ga.Channels {
			ch := &ga.Channels[ci]
			if len(ch.Times) > 0 && ch.Times[len(ch.Times)-1] > durSec {
				durSec = ch.Times[len(ch.Times)-1]
			}
		}
		dur := secondsToDurationClamped(float64(durSec))
		animInfos[i] = ModelAnimationInfo{
			Index:    i,
			Name:     ga.Name,
			Duration: dur,
			Channels: len(ga.Channels),
		}
	}
	info.Animations = animInfos

	// Nodes: walk active scene hierarchy first so depth and order match the scene tree,
	// then include any remaining unreferenced nodes.
	visited := make([]bool, len(doc.Nodes))
	var walkNode func(ni, depth int, inScene bool)
	walkNode = func(ni, depth int, inScene bool) {
		if ni < 0 || ni >= len(doc.Nodes) || visited[ni] {
			return
		}
		visited[ni] = true
		gn := &doc.Nodes[ni]
		niInfo := ModelNodeInfo{
			Index:  ni,
			Name:   gn.Name,
			Parent: gn.Parent,
			Depth:  depth,
			Mesh:   gn.Mesh,
			Skin:   gn.Skin,
			Bounds: math3d.AABBEmpty(),
		}
		if gn.Mesh >= 0 && gn.Mesh < len(meshInfos) {
			mi := meshInfos[gn.Mesh]
			niInfo.MeshName = mi.DisplayName()
			niInfo.Triangles = mi.Triangles
			if !mi.Bounds.Empty() {
				world := player.WorldTransform(ni)
				wb := mi.Bounds.Transform(world)
				niInfo.Bounds = wb
				if inScene {
					info.Bounds = info.Bounds.Union(wb)
				}
			}
			if inScene {
				info.TotalTriangles += mi.Triangles
				info.TotalVertices += mi.Vertices
			}
		}
		info.Nodes = append(info.Nodes, niInfo)
		for _, c := range gn.Children {
			walkNode(c, depth+1, inScene)
		}
	}

	roots := modelPreviewSceneRoots(doc)
	for _, r := range roots {
		walkNode(r, 0, true)
	}
	for i := range doc.Nodes {
		if !visited[i] {
			walkNode(i, 0, len(roots) == 0)
		}
	}
	if len(doc.Nodes) == 0 {
		for _, mi := range meshInfos {
			info.TotalTriangles += mi.Triangles
			info.TotalVertices += mi.Vertices
			if !mi.Bounds.Empty() {
				info.Bounds = info.Bounds.Union(mi.Bounds)
			}
		}
	}

	info.ScaledBounds = scaleAABB(info.Bounds, float32(scale))
	return info
}

func buildModelPreviewScene(
	doc *gltf.Document,
	player *anim3d.Player,
	scale float64,
) (*scene3d.Scene, *scene3d.Node, []*scene3d.Node, *scenemodel.Built, []modelPreviewPartSource, error) {
	scene := scene3d.NewScene()
	s := float32(scale)
	if s <= 0 {
		s = 1
	}
	scaleRoot := scene3d.NewNode("preview-scale")
	scaleRoot.SetLocal(math3d.Transform{
		Rot:   math3d.QuatIdentity(),
		Scale: math3d.Vec3{X: s, Y: s, Z: s},
	})
	if err := scene.Root().AddChild(scaleRoot); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Detect which nodes have active morph target channels so we only mark meshes deformed when needed.
	morphAnimated := make(map[int]bool)
	for ai := range doc.Animations {
		for ci := range doc.Animations[ai].Channels {
			ch := &doc.Animations[ai].Channels[ci]
			if ch.Path == gltf.PathWeights && ch.TargetNode >= 0 {
				morphAnimated[ch.TargetNode] = true
			}
		}
	}

	built := &scenemodel.Built{}
	type texKey struct{ img, samp int }
	texCache := make(map[texKey]*scenemodel.Texture)
	matCache := make(map[int]scenemodel.Material)
	matIDs := make(map[int]uint32)
	nextMeshID := uint32(0)

	resolveTex := func(ti int) (*scenemodel.Texture, error) {
		if ti < 0 || ti >= len(doc.Textures) {
			return nil, nil
		}
		t := &doc.Textures[ti]
		if t.Source < 0 || t.Source >= len(doc.Images) {
			return nil, nil
		}
		key := texKey{t.Source, t.Sampler}
		if cached, ok := texCache[key]; ok {
			return cached, nil
		}
		var samp gltf.Sampler
		if t.Sampler >= 0 && t.Sampler < len(doc.Samplers) {
			samp = doc.Samplers[t.Sampler]
		}
		opts, mips, err := render3d.SamplerOptionsFromGLTF(samp)
		if err != nil {
			opts, mips = render3d.SamplerLinearRepeat(), false
		}
		img, err := doc.DecodeImage(t.Source)
		if err != nil {
			// Fall back to untextured base color if an unsupported image format (e.g., KTX2-only without PNG) fails decode.
			return nil, nil
		}
		name := fmt.Sprintf("image %d", t.Source)
		if n := doc.Images[t.Source].Name; n != "" {
			name = n
		}
		tex := &scenemodel.Texture{Name: name, Image: img, Sampler: opts, Mipmaps: mips}
		texCache[key] = tex
		return tex, nil
	}

	resolveMat := func(mi int) (scenemodel.Material, uint32, error) {
		if mi < 0 || mi >= len(doc.Materials) {
			mi = -1
		}
		if id, ok := matIDs[mi]; ok {
			return matCache[mi], id, nil
		}
		m := scenemodel.Material{
			BaseColor:   math3d.Vec4{X: 1, Y: 1, Z: 1, W: 1},
			Metallic:    1,
			Roughness:   1,
			AlphaMode:   render3d.AlphaOpaque,
			AlphaCutoff: 0.5,
		}
		if mi >= 0 {
			gm := &doc.Materials[mi]
			mode, err := render3d.AlphaModeFromGLTF(gm.AlphaMode)
			if err != nil {
				mode = render3d.AlphaOpaque
			}
			strength, err := gm.EmissiveStrength()
			if err != nil {
				strength = 1
			}
			m = scenemodel.Material{
				BaseColor:   gm.BaseColorFactor,
				Metallic:    gm.MetallicFactor,
				Roughness:   gm.RoughnessFactor,
				Emissive:    gm.EmissiveFactor.Scale(strength),
				AlphaMode:   mode,
				AlphaCutoff: gm.AlphaCutoff,
				DoubleSided: gm.DoubleSided,
			}
			if gm.BaseColorTexture != nil {
				tex, _ := resolveTex(gm.BaseColorTexture.TextureIndex)
				m.BaseColorTexture = tex
			}
		}
		if uint64(len(matIDs)) > math.MaxUint32 {
			return scenemodel.Material{}, 0, errors.New("editor: too many materials in model preview")
		}
		id := uint32(len(matIDs))
		matIDs[mi] = id
		matCache[mi] = m
		return m, id, nil
	}

	var deformParts []modelPreviewPartSource

	attachMesh := func(sn *scene3d.Node, nodeIdx, meshIdx int) error {
		gm := &doc.Meshes[meshIdx]
		var weights []float32
		if nodeIdx >= 0 {
			weights = player.MorphWeights(nodeIdx)
		}
		if len(weights) == 0 {
			weights = gm.Weights
		}
		var jointMats []math3d.Mat4
		skinnedNode := nodeIdx >= 0 && nodeIdx < len(doc.Nodes) && doc.Nodes[nodeIdx].Skin >= 0 && doc.Nodes[nodeIdx].Skin < len(doc.Skins)
		if skinnedNode {
			jm, err := player.SkinMatricesForNode(nodeIdx)
			if err == nil && len(jm) > 0 {
				jointMats = jm
			}
		}

		var parts []scenemodel.Part
		var partSources []modelPreviewPartSource
		for pi := range gm.Primitives {
			prim := &gm.Primitives[pi]
			if prim.Mode != gltf.ModeTriangles && prim.Mode != gltf.ModeTriangleStrip && prim.Mode != gltf.ModeTriangleFan {
				continue
			}
			mat, matID, err := resolveMat(prim.Material)
			if err != nil {
				return err
			}
			var uvRef *gltf.TextureRef
			if prim.Material >= 0 && prim.Material < len(doc.Materials) {
				if ref := doc.Materials[prim.Material].BaseColorTexture; ref != nil && (ref.EffectiveTexCoord() != 0 || ref.Transform != nil) {
					uvRef = ref
				}
			}
			isSkinned := skinnedNode && len(prim.Joints0) == len(prim.Positions) && len(prim.Weights0) == len(prim.Positions) && len(jointMats) > 0
			isMorphed := len(prim.Targets) > 0 && nodeIdx >= 0 && morphAnimated[nodeIdx]
			mesh, err := buildPreviewPrimitiveMesh(doc, prim, uvRef, weights, jointMats, isSkinned)
			if err != nil {
				return err
			}
			meshID := nextMeshID
			nextMeshID++
			parts = append(parts, scenemodel.Part{
				Mesh:       mesh,
				MeshID:     meshID,
				Material:   mat,
				MaterialID: matID,
			})
			if isSkinned || isMorphed {
				partSources = append(partSources, modelPreviewPartSource{
					partIdx: len(parts) - 1,
					nodeIdx: nodeIdx,
					meshIdx: meshIdx,
					primIdx: pi,
					uvRef:   uvRef,
					skinned: isSkinned,
					morphed: isMorphed,
				})
			}
		}
		if len(parts) == 0 {
			return nil
		}
		bounds := math3d.AABBEmpty()
		for i := range parts {
			bounds = bounds.Union(parts[i].Mesh.Bounds())
		}
		drawableIdx := len(built.Drawables)
		d := &scenemodel.Drawable{
			Node:   sn,
			Object: sn.Name,
			Parts:  parts,
		}
		sn.SetLocalBounds(bounds)
		sn.SetAttachment(scenemodel.AttachmentKey, d)
		built.Drawables = append(built.Drawables, d)
		for _, ps := range partSources {
			ps.drawableIdx = drawableIdx
			deformParts = append(deformParts, ps)
		}
		return nil
	}

	sceneNodes := make([]*scene3d.Node, len(doc.Nodes))
	if len(doc.Nodes) > 0 {
		visited := make([]bool, len(doc.Nodes))
		var addNode func(ni int, parent *scene3d.Node) error
		addNode = func(ni int, parent *scene3d.Node) error {
			if ni < 0 || ni >= len(doc.Nodes) || visited[ni] {
				return nil
			}
			visited[ni] = true
			gn := &doc.Nodes[ni]
			name := gn.Name
			if name == "" {
				name = fmt.Sprintf("node_%d", ni)
			}
			sn := scene3d.NewNode(name)
			sn.SetLocal(player.LocalTransform(ni))
			if err := parent.AddChild(sn); err != nil {
				return err
			}
			sceneNodes[ni] = sn
			if gn.Mesh >= 0 && gn.Mesh < len(doc.Meshes) {
				if err := attachMesh(sn, ni, gn.Mesh); err != nil {
					return err
				}
			}
			for _, c := range gn.Children {
				if err := addNode(c, sn); err != nil {
					return err
				}
			}
			return nil
		}
		for _, r := range modelPreviewSceneRoots(doc) {
			if err := addNode(r, scaleRoot); err != nil {
				return nil, nil, nil, nil, nil, err
			}
		}
	} else {
		for mi := range doc.Meshes {
			sn := scene3d.NewNode(fmt.Sprintf("mesh_%d", mi))
			if err := scaleRoot.AddChild(sn); err != nil {
				return nil, nil, nil, nil, nil, err
			}
			if err := attachMesh(sn, -1, mi); err != nil {
				return nil, nil, nil, nil, nil, err
			}
		}
	}

	scene.UpdateTransforms()
	built.Meshes = int(nextMeshID)
	built.Materials = len(matIDs)
	return scene, scaleRoot, sceneNodes, built, deformParts, nil
}

func (p *modelPreviewState) updateDeformedMeshes() error {
	if p.doc == nil || p.player == nil || p.built == nil {
		return nil
	}
	skinCache := make(map[int][]math3d.Mat4)
	for _, ps := range p.deformParts {
		if ps.drawableIdx >= len(p.built.Drawables) {
			continue
		}
		d := p.built.Drawables[ps.drawableIdx]
		if ps.partIdx >= len(d.Parts) {
			continue
		}
		gm := &p.doc.Meshes[ps.meshIdx]
		prim := &gm.Primitives[ps.primIdx]
		weights := p.player.MorphWeights(ps.nodeIdx)
		if len(weights) == 0 {
			weights = gm.Weights
		}
		var jointMats []math3d.Mat4
		if ps.skinned {
			jm, ok := skinCache[ps.nodeIdx]
			if !ok {
				var err error
				jm, err = p.player.SkinMatricesForNode(ps.nodeIdx)
				if err == nil {
					skinCache[ps.nodeIdx] = jm
				}
			}
			jointMats = jm
		}
		mesh, err := buildPreviewPrimitiveMesh(p.doc, prim, ps.uvRef, weights, jointMats, ps.skinned && len(jointMats) > 0)
		if err != nil {
			return err
		}
		d.Parts[ps.partIdx].Mesh = mesh
		bounds := math3d.AABBEmpty()
		for i := range d.Parts {
			bounds = bounds.Union(d.Parts[i].Mesh.Bounds())
		}
		d.Node.SetLocalBounds(bounds)
	}
	return nil
}

func buildPreviewPrimitiveMesh(
	doc *gltf.Document,
	prim *gltf.Primitive,
	uvRef *gltf.TextureRef,
	weights []float32,
	jointMats []math3d.Mat4,
	skinned bool,
) (render3d.Mesh, error) {
	pos := prim.Positions
	norm := prim.Normals
	if len(prim.Targets) > 0 && previewAnyNonZero(weights) {
		pos = gltf.EvaluateMorphPositions(*prim, weights, nil)
		if len(norm) > 0 {
			norm = gltf.EvaluateMorphNormals(*prim, weights, nil)
		}
	}
	if skinned && len(jointMats) > 0 && len(prim.Joints0) == len(pos) && len(prim.Weights0) == len(pos) {
		sp := make([]math3d.Vec3, len(pos))
		var sn []math3d.Vec3
		hasNorm := len(norm) == len(pos)
		if hasNorm {
			sn = make([]math3d.Vec3, len(pos))
		}
		for i := range pos {
			var n math3d.Vec3
			if hasNorm {
				n = norm[i]
			}
			pOut, nOut := render3d.SkinVertex(pos[i], n, prim.Joints0[i], prim.Weights0[i], jointMats)
			sp[i] = pOut
			if hasNorm {
				sn[i] = nOut
			}
		}
		pos = sp
		if hasNorm {
			norm = sn
		}
	}

	m := render3d.Mesh{
		Positions: pos,
		Normals:   norm,
		UVs:       prim.TexCoords0,
	}
	if uvRef != nil {
		uvs, err := doc.TransformTexCoords(*prim, uvRef, nil)
		if err == nil {
			m.UVs = uvs
		}
	}
	indices, err := previewTriangleList(prim.Mode, prim.Indices, len(m.Positions))
	if err != nil {
		return render3d.Mesh{}, err
	}
	m.Indices = indices
	if len(m.Normals) == 0 {
		m = previewFlatShaded(m)
	}
	if err := m.Validate(); err != nil {
		return render3d.Mesh{}, err
	}
	return m, nil
}

func previewAnyNonZero(weights []float32) bool {
	for _, w := range weights {
		if w != 0 {
			return true
		}
	}
	return false
}

func secondsToDurationClamped(secs float64) time.Duration {
	if math.IsNaN(secs) || math.IsInf(secs, -1) || secs <= 0 {
		return 0
	}
	const maxSecs = float64(math.MaxInt64) / float64(time.Second)
	if math.IsInf(secs, 1) || secs >= maxSecs {
		return time.Duration(math.MaxInt64)
	}
	ns := math.Round(secs * float64(time.Second))
	if ns >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ns)
}

func previewTriangleList(mode gltf.PrimitiveMode, indices []uint32, count int) ([]uint32, error) {
	if mode == gltf.ModeTriangles {
		return indices, nil
	}
	if len(indices) == 0 && uint64(count) > math.MaxUint32 {
		return nil, fmt.Errorf("vertex count %d exceeds uint32 index limit", count)
	}
	at := func(i int) uint32 {
		if i < 0 || uint64(i) > math.MaxUint32 {
			return 0
		}
		return uint32(i)
	}
	n := count
	if len(indices) > 0 {
		at = func(i int) uint32 { return indices[i] }
		n = len(indices)
	}
	if n < 3 {
		return nil, fmt.Errorf("a triangle strip or fan needs 3 vertices, not %d", n)
	}
	out := make([]uint32, 0, 3*(n-2))
	for i := 0; i+2 < n; i++ {
		switch {
		case mode == gltf.ModeTriangleFan:
			out = append(out, at(0), at(i+1), at(i+2))
		case i%2 == 0:
			out = append(out, at(i), at(i+1), at(i+2))
		default:
			out = append(out, at(i+1), at(i), at(i+2))
		}
	}
	return out, nil
}

func previewFlatShaded(m render3d.Mesh) render3d.Mesh {
	idx := m.Indices
	if len(idx) == 0 {
		n := len(m.Positions) - len(m.Positions)%3
		if n <= 0 || uint64(n) > math.MaxUint32 {
			return m
		}
		idx = make([]uint32, n)
		for i := range idx {
			idx[i] = uint32(i)
		}
	}
	triCount := len(idx) - len(idx)%3
	if triCount == 0 {
		return m
	}
	out := render3d.Mesh{
		Positions: make([]math3d.Vec3, triCount),
		Normals:   make([]math3d.Vec3, triCount),
	}
	if len(m.UVs) == len(m.Positions) {
		out.UVs = make([]math3d.Vec2, triCount)
	}
	for t := 0; t+2 < triCount; t += 3 {
		var p [3]math3d.Vec3
		for k := range 3 {
			v := idx[t+k]
			if uint64(v) >= uint64(len(m.Positions)) {
				return m
			}
			p[k] = m.Positions[v]
			out.Positions[t+k] = p[k]
			if out.UVs != nil {
				out.UVs[t+k] = m.UVs[v]
			}
		}
		n := p[1].Sub(p[0]).Cross(p[2].Sub(p[0]))
		if l := n.Len(); l > 0 && !math.IsInf(float64(l), 0) {
			n = n.Scale(1 / l)
		} else {
			n = math3d.Vec3{Y: 1}
		}
		out.Normals[t], out.Normals[t+1], out.Normals[t+2] = n, n, n
	}
	return out
}
