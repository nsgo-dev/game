package editor

import (
	"errors"
	"fmt"
	"image"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ModelThumbnailSize is the width and height in pixels of the thumbnails ModelThumbnail renders.
const ModelThumbnailSize = 64

// modelThumb is a rendered thumbnail, or why it could not be rendered.
type modelThumb struct {
	img *image.RGBA
	err error
}

// modelThumbJob is a model built on the UI goroutine for the worker to render: the build belongs to the job alone, so the worker reads it without sharing it.
type modelThumbJob struct {
	key    string
	gen    uint64
	built  *scenemodel.Built
	bounds math3d.AABB
	// post runs a function on the UI goroutine (core.Scope.Post), nil without a UI.
	post func(func())
}

// modelThumbs holds the asset browser's model thumbnails, which one worker goroutine renders off the UI goroutine on a software GPU, one model at a time.
type modelThumbs struct {
	mu sync.Mutex
	// key is the project and the asset revision the thumbnails were rendered at, and gen counts its changes, so a thumbnail of a stale model is dropped.
	key     string
	gen     uint64
	done    map[string]modelThumb
	queued  map[string]bool
	jobs    []modelThumbJob
	running bool
	// worker counts the running worker, for tests that wait for it.
	worker sync.WaitGroup
	// listeners run on the UI goroutine after a thumbnail is rendered; mounted thumbnail views subscribe to redraw.
	nextListener uint64
	listeners    map[uint64]func()
}

// subscribe registers fn to run on the UI goroutine whenever a model thumbnail finishes rendering, and returns an unsubscribe function.
func (t *modelThumbs) subscribe(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	t.mu.Lock()
	t.nextListener++
	id := t.nextListener
	if t.listeners == nil {
		t.listeners = make(map[uint64]func())
	}
	t.listeners[id] = fn
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		delete(t.listeners, id)
		t.mu.Unlock()
	}
}

func (t *modelThumbs) notifyReady() {
	t.mu.Lock()
	ids := make([]uint64, 0, len(t.listeners))
	for id := range t.listeners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	fns := make([]func(), 0, len(ids))
	for _, id := range ids {
		fns = append(fns, t.listeners[id])
	}
	t.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// ModelThumbnail returns a picture of m, ModelThumbnailSize pixels square: its geometry framed from above, in front and to the right, lit by DefaultLights3D on a transparent background. The first call for a model loads it on the calling goroutine, the UI goroutine, as everything the Resolver serves loads, and leaves the rendering, through scenemodel on NSGo's software GPU (gpu.SoftwareRenderer), to a worker goroutine: it returns nil and no error, and the asset browser redraws when the picture is ready. The model loads apart from the 3D build's cache, which keeps only what the scene uses. It returns an error when m's model does not load or draws no geometry. The thumbnails are kept until the open project changes or its built assets reload (ReloadAssets).
func (e *Editor) ModelThumbnail(m ModelEntry) (*image.RGBA, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	t := &e.thumbs
	key := e.ProjectPath + "\x00" + strconv.FormatUint(e.assetsRev, 10)
	t.mu.Lock()
	if t.key != key || t.done == nil {
		t.key, t.gen = key, t.gen+1
		t.done, t.queued, t.jobs = make(map[string]modelThumb), make(map[string]bool), nil
	}
	if th, ok := t.done[m.Key()]; ok {
		t.mu.Unlock()
		return th.img, th.err
	}
	if t.queued[m.Key()] {
		t.mu.Unlock()
		return nil, nil
	}
	gen := t.gen
	t.mu.Unlock()

	// The model loads here, on the UI goroutine, as everything the Resolver serves does; only the rendering runs on the worker. It loads through files of its own rather than the 3D build's cache, so browsing the list keeps no model the scene does not use, and they are dropped with the build once the thumbnail is rendered.
	assets := scene3DAssets{models: e.ModelResolver, resolver: e.Resolver}
	if e.ProjectPath != "" {
		assets.files = scenemodel.NewAssets(newConfinedProjectDirFS(filepath.Dir(e.ProjectPath)))
	}
	built, bounds, err := buildModelWith3D(m, assets)
	if err == nil && bounds.Empty() {
		err = fmt.Errorf("editor: %s draws no geometry", m.Key())
	}
	var post func(func())
	if e.scope != nil {
		post = e.scope.Post
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.gen != gen {
		return nil, nil
	}
	if err != nil {
		t.done[m.Key()] = modelThumb{err: err}
		return nil, err
	}
	t.queued[m.Key()] = true
	t.jobs = append(t.jobs, modelThumbJob{key: m.Key(), gen: gen, built: built, bounds: bounds, post: post})
	if !t.running {
		t.running = true
		t.worker.Add(1)
		go e.renderModelThumbs()
	}
	return nil, nil
}

// renderModelThumbs renders the queued thumbnails one by one until none is left.
func (e *Editor) renderModelThumbs() {
	t := &e.thumbs
	defer t.worker.Done()
	for {
		t.mu.Lock()
		if len(t.jobs) == 0 {
			t.running = false
			t.mu.Unlock()
			return
		}
		job := t.jobs[0]
		t.jobs = t.jobs[1:]
		t.mu.Unlock()

		img, err := renderModelThumbnail(job.built, job.bounds)
		t.mu.Lock()
		if job.gen == t.gen {
			t.done[job.key] = modelThumb{img: img, err: err}
			delete(t.queued, job.key)
		}
		t.mu.Unlock()
		if job.post != nil {
			job.post(func() {
				t.notifyReady()
			})
		}
	}
}

// WaitModelThumbnails blocks until all model thumbnails queued so far have finished rendering offscreen.
func (e *Editor) WaitModelThumbnails() {
	if e == nil {
		return
	}
	e.thumbs.worker.Wait()
}

// waitModelThumbnails blocks until the thumbnails queued so far are rendered.
func (e *Editor) waitModelThumbnails() {
	e.WaitModelThumbnails()
}

// thumbnailCamera3D returns an orbit camera that frames bounds from the default view's direction, with clip planes around them.
func thumbnailCamera3D(bounds math3d.AABB) OrbitCamera3D {
	c := bounds.Center()
	ext := bounds.Size()
	radius := math.Max(0.5*math.Sqrt(float64(ext.X*ext.X+ext.Y*ext.Y+ext.Z*ext.Z)), 1e-3)
	fov := math.Pi / 4
	dist := radius / math.Sin(fov/2) * 1.05
	cam := DefaultOrbitCamera3D()
	cam.Target = [3]float32{c.X, c.Y, c.Z}
	cam.FOV = float32(fov)
	cam.Distance = float32(dist)
	cam.Near = float32(math.Max(dist-radius*1.5, dist*1e-3))
	cam.Far = float32(dist + radius*1.5)
	return cam
}

// renderModelThumbnail draws built, whose geometry lies in bounds, into a new image ModelThumbnailSize pixels square on a software GPU of its own.
func renderModelThumbnail(built *scenemodel.Built, bounds math3d.AABB) (*image.RGBA, error) {
	sr := gpu.NewSoftwareRenderer(ModelThumbnailSize, ModelThumbnailSize, viewport3DDepthFormat)
	defer sr.Close()
	dev := sr.Device()
	r, err := scenemodel.NewRenderer(nil, dev, built, scenemodel.RendererOptions{Label: "editor.thumbnail", DepthFormat: viewport3DDepthFormat})
	if err != nil {
		return nil, fmt.Errorf("editor: thumbnail: %w", err)
	}
	defer func() { _ = r.Close() }()
	cam := thumbnailCamera3D(bounds).SceneCamera(ModelThumbnailSize, ModelThumbnailSize)
	if _, err := r.Prepare(cam, DefaultLights3D()); err != nil {
		return nil, fmt.Errorf("editor: thumbnail: %w", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "editor.thumbnail",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.Color(0)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	r.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		return nil, fmt.Errorf("editor: thumbnail: %w", err)
	}
	return sr.Image(), nil
}
