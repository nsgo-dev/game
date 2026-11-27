package render3d

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	// DefaultUploadBytesPerFrame is the default per-frame GPU upload budget (4 MiB) used by [NewUploader] when [UploaderOptions.BytesPerFrame] is <= 0.
	DefaultUploadBytesPerFrame = 4 * mib

	// DefaultUploadChunkBytes is the default per-write chunk size (256 KiB) used to split large buffers and textures smoothly across frames.
	DefaultUploadChunkBytes = 256 * 1024
)

var (
	// ErrUploaderClosed is returned when queueing on a closed [Uploader] or when an unfinished upload is aborted because the uploader or its scope closed.
	ErrUploaderClosed = errors.New("render3d: uploader closed")

	// ErrUploadCanceled is wrapped by errors returned when an upload is canceled via [UploadHandle.Cancel] or its caller context.
	ErrUploadCanceled = errors.New("render3d: upload canceled")

	// ErrUploadQueueFull is returned when queueing an upload would exceed [UploaderOptions.MaxPending].
	ErrUploadQueueFull = errors.New("render3d: upload queue full")
)

// UploadPriority orders queued decodes and GPU uploads inside an [Uploader]. Higher priorities preempt lower ones; equal priorities preserve FIFO order.
type UploadPriority int

const (
	// PriorityLow is intended for background prefetching and distant assets.
	PriorityLow UploadPriority = -1
	// PriorityNormal is the default priority for standard asset streaming.
	PriorityNormal UploadPriority = 0
	// PriorityHigh is intended for immediately visible hero assets.
	PriorityHigh UploadPriority = 1
)

// UploadState reports the lifecycle stage of an [UploadHandle].
type UploadState uint8

const (
	// UploadQueued means the upload is waiting for a decode worker goroutine.
	UploadQueued UploadState = iota
	// UploadDecoding means a background worker is running the decode callback and preparing packed CPU staging bytes, or the decoded asset is waiting for the [Uploader.Step] that starts its upload.
	UploadDecoding
	// UploadUploading means the GPU resource is allocated and being streamed across frames by [Uploader.Step].
	UploadUploading
	// UploadReady means all bytes have been uploaded; onReady is called right after the state changes.
	UploadReady
	// UploadFailed means decoding, validation, or GPU upload failed.
	UploadFailed
	// UploadCanceled means the upload was canceled before completion.
	UploadCanceled
)

// String returns a human-readable name for s.
func (s UploadState) String() string {
	switch s {
	case UploadQueued:
		return "queued"
	case UploadDecoding:
		return "decoding"
	case UploadUploading:
		return "uploading"
	case UploadReady:
		return "ready"
	case UploadFailed:
		return "failed"
	case UploadCanceled:
		return "canceled"
	default:
		return fmt.Sprintf("UploadState(%d)", s)
	}
}

// UploaderOptions configures [NewUploader]. Zero values receive sensible defaults.
type UploaderOptions struct {
	// BytesPerFrame caps how many bytes [Uploader.Step] writes to the GPU per call. Defaults to [DefaultUploadBytesPerFrame] (4 MiB) when <= 0. Adjustable at runtime via [Uploader.SetBytesPerFrame].
	BytesPerFrame int

	// ChunkBytes caps each individual buffer or texture write chunk so large assets split cleanly across frames. Defaults to [DefaultUploadChunkBytes] (256 KiB) when <= 0, and is clamped to BytesPerFrame during [Uploader.Step].
	ChunkBytes int

	// Workers is the number of background goroutines executing decode callbacks and CPU data packing. Defaults to min(runtime.GOMAXPROCS(0), 4) (at least 1) when <= 0.
	Workers int

	// MaxPending optionally limits the number of unfinished uploads (queued, decoding, and uploading). When > 0 and reached, Queue* fails immediately with [ErrUploadQueueFull].
	MaxPending int
}

// RawBufferUpload describes a raw GPU buffer payload returned by the decode callback of [Uploader.QueueRawBuffer].
type RawBufferUpload struct {
	Label    string
	Usage    gpu.BufferUsage // gpu.UsageCopyDst is always added
	Data     []byte
	Retained bool
}

// TextureMipData is one mip level (and optional array layer / cubemap face) of texel or compressed block bytes for [RawTextureUpload].
type TextureMipData struct {
	MipLevel    uint32
	ArrayLayer  uint32
	Data        []byte
	BytesPerRow uint32 // 0 means tightly packed for the mip width and format
}

// RawTextureUpload describes a GPU texture and pre-encoded mip payloads returned by the decode callback of [Uploader.QueueRawTexture].
type RawTextureUpload struct {
	Desc gpu.TextureDesc // Usage defaults to TextureSampled | TextureCopyDst; TextureCopyDst is always added
	Mips []TextureMipData
}

// UploadFrameStats summarizes the work performed by one [Uploader.Step] call.
type UploadFrameStats struct {
	// BytesUploaded is the exact number of bytes written to the GPU during this Step.
	BytesUploaded int
	// Completed is the number of uploads that finished and invoked onReady with a resource.
	Completed int
	// Failed is the number of uploads that failed during this Step.
	Failed int
	// Canceled is the number of uploads that were canceled during this Step.
	Canceled int
	// Pending is the number of unfinished uploads remaining after this Step.
	Pending int
	// InFlightDecodes is the number of decode callbacks currently running on worker goroutines.
	InFlightDecodes int
	// FrameDuration is the wall-clock time spent inside this Step call.
	FrameDuration time.Duration
}

// UploadHandle tracks one queued upload and allows inspecting its progress or canceling it from any goroutine.
type UploadHandle struct {
	uploader *Uploader
	job      *uploadJob
	state    atomic.Uint32
	uploaded atomic.Int64
	total    atomic.Int64
	canceled atomic.Bool
	errPtr   atomic.Pointer[error]
	done     chan struct{}
	doneOnce sync.Once
}

func newUploadHandle(u *Uploader, job *uploadJob) *UploadHandle {
	h := &UploadHandle{
		uploader: u,
		job:      job,
		done:     make(chan struct{}),
	}
	h.state.Store(uint32(UploadQueued))
	return h
}

func newImmediateErrorHandle(err error) *UploadHandle {
	h := &UploadHandle{
		done: make(chan struct{}),
	}
	h.finish(UploadFailed, err)
	return h
}

func (h *UploadHandle) finish(st UploadState, err error) {
	if err != nil {
		e := err
		h.errPtr.Store(&e)
	}
	h.state.Store(uint32(st))
	h.doneOnce.Do(func() {
		close(h.done)
	})
}

// State returns the current lifecycle state of the upload.
func (h *UploadHandle) State() UploadState {
	if h == nil {
		return UploadFailed
	}
	return UploadState(h.state.Load())
}

// Progress returns (uploaded, total) bytes for the GPU transfer stage. Both values are 0 while the asset is still queued or decoding.
func (h *UploadHandle) Progress() (uploaded, total int64) {
	if h == nil {
		return 0, 0
	}
	return h.uploaded.Load(), h.total.Load()
}

// Err returns the terminal error once the upload has failed or been canceled, or nil while in progress or after reaching [UploadReady].
func (h *UploadHandle) Err() error {
	if h == nil {
		return ErrUploaderClosed
	}
	if p := h.errPtr.Load(); p != nil {
		return *p
	}
	return nil
}

// Done returns a channel that is closed when the upload reaches a terminal state ([UploadReady], [UploadFailed], or [UploadCanceled]).
func (h *UploadHandle) Done() <-chan struct{} {
	if h == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return h.done
}

// Cancel requests cancellation of the upload. It is safe to call from any goroutine and safe to call more than once. Any partially created GPU resources are released on the next [Uploader.Step] (or [Uploader.Close]).
func (h *UploadHandle) Cancel() {
	if h == nil || h.uploader == nil || h.job == nil {
		return
	}
	h.canceled.Store(true)
	h.uploader.cancelJob(h.job)
}

type uploadKind uint8

const (
	kindMesh uploadKind = iota
	kindTexture
	kindRawBuffer
	kindRawTexture
)

type jobLocation uint8

const (
	locDecodeHeap jobLocation = iota
	locDecoding
	locUploadHeap
	locStepping
	locFinishQueue
	locDone
)

type packedMip struct {
	mipLevel     uint32
	arrayLayer   uint32
	width        uint32
	height       uint32
	bytesPerRow  uint32 // tightly packed row (or block-row) byte stride
	blockHeight  uint32 // 1 for uncompressed; compression block height for compressed
	bytesPerUnit uint32 // bytes per texel (uncompressed) or bytes per block (compressed)
	compressed   bool
	data         []byte
}

type uploadJob struct {
	kind     uploadKind
	priority UploadPriority
	seq      uint64
	handle   *UploadHandle

	ctx        context.Context
	cancelCtx  context.CancelFunc
	stopRootFn func() bool

	// Decode callbacks (exactly one populated by kind).
	decodeMesh    func(context.Context) (Mesh, error)
	decodeTexture func(context.Context) (PixelBuffer, error)
	decodeRawBuf  func(context.Context) (RawBufferUpload, error)
	decodeRawTex  func(context.Context) (RawTextureUpload, error)

	// Per-item options.
	meshOpts MeshUpload
	texOpts  TextureUpload

	// Completion callbacks (exactly one populated by kind).
	onMeshReady   func(*GPUMesh, error)
	onTexReady    func(*GPUTexture, error)
	onRawTexReady func(*gpu.Texture, error)
	onRawBufReady func(*gpu.Buffer, error)

	// Decode error (if decode/validation failed).
	decodeErr error

	// Staging data produced on the worker goroutine, consumed on engine goroutine. Mesh / raw buffer staging:
	vertexBytes []byte
	indexBytes  []byte
	vertexCount uint32
	indexCount  uint32
	indexFormat gpu.IndexFormat
	bounds      math3d.AABB
	rawBufDesc  gpu.BufferDesc
	rawBufBytes []byte

	// Texture staging:
	texDesc gpu.TextureDesc
	mips    []packedMip

	// GPU upload state (touched only on engine goroutine in Step / Close).
	allocated  bool
	gpuMesh    *GPUMesh
	gpuBuf     *gpu.Buffer
	gpuTex     *gpu.Texture
	stage      int    // for mesh: 0 = vertex buffer, 1 = index buffer
	byteOffset int    // byte offset in current buffer
	mipIdx     int    // index into mips
	curRow     uint32 // current texel row (uncompressed) or block-row (compressed)
	curX       uint32 // current texel column within curRow (uncompressed sub-row split)

	// Queue bookkeeping (protected by Uploader.mu).
	loc     jobLocation
	heapIdx int
}

func (j *uploadJob) cleanupContext() {
	if j.stopRootFn != nil {
		j.stopRootFn()
		j.stopRootFn = nil
	}
	if j.cancelCtx != nil {
		j.cancelCtx()
	}
}

func (j *uploadJob) releaseStaging() {
	j.vertexBytes = nil
	j.indexBytes = nil
	j.rawBufBytes = nil
	j.mips = nil
	j.decodeMesh = nil
	j.decodeTexture = nil
	j.decodeRawBuf = nil
	j.decodeRawTex = nil
}

func (j *uploadJob) closePartialGPU() {
	if j.gpuMesh != nil {
		_ = j.gpuMesh.Close()
		j.gpuMesh = nil
	}
	if j.gpuBuf != nil {
		_ = j.gpuBuf.Close()
		j.gpuBuf = nil
	}
	if j.gpuTex != nil {
		_ = j.gpuTex.Close()
		j.gpuTex = nil
	}
	j.allocated = false
}

func (j *uploadJob) invokeError(err error) {
	switch j.kind {
	case kindMesh:
		if j.onMeshReady != nil {
			j.onMeshReady(nil, err)
		}
	case kindTexture:
		if j.onTexReady != nil {
			j.onTexReady(nil, err)
		}
	case kindRawTexture:
		if j.onRawTexReady != nil {
			j.onRawTexReady(nil, err)
		}
	case kindRawBuffer:
		if j.onRawBufReady != nil {
			j.onRawBufReady(nil, err)
		}
	}
}

func (j *uploadJob) invokeReady() {
	switch j.kind {
	case kindMesh:
		res := j.gpuMesh
		j.gpuMesh = nil
		if j.onMeshReady != nil {
			j.onMeshReady(res, nil)
		}
	case kindTexture:
		res := &GPUTexture{tex: j.gpuTex, width: j.texDesc.Width, height: j.texDesc.Height, mipLevels: j.texDesc.MipLevels, format: j.texDesc.Format}
		j.gpuTex = nil
		if j.onTexReady != nil {
			j.onTexReady(res, nil)
		}
	case kindRawTexture:
		res := j.gpuTex
		j.gpuTex = nil
		if j.onRawTexReady != nil {
			j.onRawTexReady(res, nil)
		}
	case kindRawBuffer:
		res := j.gpuBuf
		j.gpuBuf = nil
		if j.onRawBufReady != nil {
			j.onRawBufReady(res, nil)
		}
	}
}

func (j *uploadJob) totalBytes() int64 {
	switch j.kind {
	case kindMesh:
		return int64(len(j.vertexBytes) + len(j.indexBytes))
	case kindRawBuffer:
		return int64(len(j.rawBufBytes))
	case kindTexture, kindRawTexture:
		var total int64
		for _, m := range j.mips {
			total += int64(len(m.data))
		}
		return total
	default:
		return 0
	}
}

func (j *uploadJob) uploadDone() bool {
	switch j.kind {
	case kindMesh:
		if j.stage == 0 {
			return j.byteOffset >= len(j.vertexBytes) && len(j.indexBytes) == 0
		}
		return j.byteOffset >= len(j.indexBytes)
	case kindRawBuffer:
		return j.byteOffset >= len(j.rawBufBytes)
	case kindTexture, kindRawTexture:
		return j.mipIdx >= len(j.mips)
	default:
		return true
	}
}

type uploadJobHeap []*uploadJob

func (h uploadJobHeap) Len() int { return len(h) }

func (h uploadJobHeap) Less(i, j int) bool {
	if h[i].priority != h[j].priority {
		return h[i].priority > h[j].priority
	}
	return h[i].seq < h[j].seq
}

func (h uploadJobHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIdx = i
	h[j].heapIdx = j
}

func (h *uploadJobHeap) Push(x any) {
	job := x.(*uploadJob)
	job.heapIdx = len(*h)
	*h = append(*h, job)
}

func (h *uploadJobHeap) Pop() any {
	old := *h
	n := len(old)
	job := old[n-1]
	old[n-1] = nil
	job.heapIdx = -1
	*h = old[:n-1]
	return job
}

// Uploader decodes meshes, textures, and raw buffers on background goroutines and streams them onto a [gpu.Device] during [Uploader.Step] without exceeding a configurable per-frame byte budget.
//
// Call [Uploader.Step] once per frame from the engine goroutine (for example, inside [gpu.ViewProps.OnFrame]). All GPU resource creation, chunked writes, and onReady callbacks run on the goroutine calling Step, or on the one calling [Uploader.Close], which must be the same. The Queue methods, [UploadHandle] methods, [Uploader.SetBytesPerFrame], [Uploader.SetChunkBytes], [Uploader.Pending] and [Uploader.InFlightDecodes] are safe to call from any goroutine.
type Uploader struct {
	scope      *core.Scope
	dev        *gpu.Device
	maxPending int

	bytesPerFrame atomic.Int64
	chunkBytes    atomic.Int64
	closedFlag    atomic.Bool

	rootCtx    context.Context
	rootCancel context.CancelFunc

	mu              sync.Mutex
	cond            *sync.Cond
	closed          bool
	nextSeq         uint64
	pending         int
	inFlightDecodes int
	decodeHeap      uploadJobHeap
	uploadHeap      uploadJobHeap
	finishQueue     []*uploadJob

	wg sync.WaitGroup
}

// NewUploader creates a budgeted asynchronous GPU uploader bound to dev and scoped to cx. When cx is non-nil, disposing cx automatically closes the uploader and joins its worker goroutines. Pass cx == nil in headless tests and call [Uploader.Close] explicitly.
func NewUploader(cx *core.Scope, dev *gpu.Device, opts UploaderOptions) *Uploader {
	budget := opts.BytesPerFrame
	if budget <= 0 {
		budget = DefaultUploadBytesPerFrame
	}
	chunk := opts.ChunkBytes
	if chunk <= 0 {
		chunk = DefaultUploadChunkBytes
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = min(runtime.GOMAXPROCS(0), 4)
		if workers < 1 {
			workers = 1
		}
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())
	u := &Uploader{
		scope:      cx,
		dev:        dev,
		maxPending: opts.MaxPending,
		rootCtx:    rootCtx,
		rootCancel: rootCancel,
	}
	u.bytesPerFrame.Store(int64(budget))
	u.chunkBytes.Store(int64(chunk))
	u.cond = sync.NewCond(&u.mu)

	if cx != nil && cx.Disposed() {
		u.closed = true
		u.closedFlag.Store(true)
		rootCancel()
		return u
	}

	u.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go u.workerLoop()
	}

	if cx != nil {
		cx.Cleanup(func() {
			_ = u.Close()
		})
	}
	return u
}

// BytesPerFrame returns the current per-frame GPU upload byte budget.
func (u *Uploader) BytesPerFrame() int {
	return int(u.bytesPerFrame.Load())
}

// SetBytesPerFrame adjusts the per-frame GPU upload byte budget at runtime. Values <= 0 restore [DefaultUploadBytesPerFrame].
func (u *Uploader) SetBytesPerFrame(n int) {
	if n <= 0 {
		n = DefaultUploadBytesPerFrame
	}
	u.bytesPerFrame.Store(int64(n))
}

// ChunkBytes returns the maximum size of a single GPU write chunk.
func (u *Uploader) ChunkBytes() int {
	return int(u.chunkBytes.Load())
}

// SetChunkBytes adjusts the per-write chunk granularity at runtime. Values <= 0 restore [DefaultUploadChunkBytes].
func (u *Uploader) SetChunkBytes(n int) {
	if n <= 0 {
		n = DefaultUploadChunkBytes
	}
	u.chunkBytes.Store(int64(n))
}

// Pending returns the number of unfinished uploads (queued, decoding, or actively streaming).
func (u *Uploader) Pending() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.pending
}

// InFlightDecodes returns the number of decode callbacks currently executing on background worker goroutines.
func (u *Uploader) InFlightDecodes() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.inFlightDecodes
}

// QueueMesh runs decode on a background worker, validates and packs the mesh there, and uploads its vertex and index buffers across [Uploader.Step] calls within the per-frame byte budget. opts are the same [MeshUpload] options [UploadMesh] takes, so a streamed mesh is the same [GPUMesh] a synchronous upload returns, and priority orders it against the other queued uploads.
//
// onReady runs on the goroutine that calls [Uploader.Step] once every byte is uploaded, or with an error when decoding, validation or the upload fails or is canceled, or when [Uploader.Close] aborts it. Errors the call can report at once (a nil decode, a closed uploader, or a full queue under [UploaderOptions.MaxPending]) call onReady on the caller's goroutine before QueueMesh returns. onReady may be nil.
func (u *Uploader) QueueMesh(ctx context.Context, priority UploadPriority, decode func(context.Context) (Mesh, error), onReady func(*GPUMesh, error), opts MeshUpload) *UploadHandle {
	job := &uploadJob{
		kind:        kindMesh,
		priority:    priority,
		decodeMesh:  decode,
		meshOpts:    opts,
		onMeshReady: onReady,
		heapIdx:     -1,
	}
	if decode == nil {
		return u.reject(job, fmt.Errorf("%w: nil decode function", ErrInvalidMesh))
	}
	return u.enqueue(ctx, job)
}

// QueueTexture runs decode on a background worker, converts the image there exactly as [UploadTexture] does ([PixelBufferFromImage], so translucent images upload with straight alpha), builds the mip chain there when opts.Mipmaps is set (box-filtered in linear light for sRGB textures, as [UploadTexturePixels] does), and streams mip levels and row bands to the GPU across [Uploader.Step] calls within the per-frame byte budget. The [GPUTexture] it delivers holds the same texels a synchronous [UploadTexture] with the same opts would, so it works with [BindTexture] and [GPUTexture.ByteSize].
//
// onReady is called as [Uploader.QueueMesh] describes.
func (u *Uploader) QueueTexture(ctx context.Context, priority UploadPriority, decode func(context.Context) (image.Image, error), onReady func(*GPUTexture, error), opts TextureUpload) *UploadHandle {
	if decode == nil {
		return u.QueueTexturePixels(ctx, priority, nil, onReady, opts)
	}
	return u.QueueTexturePixels(ctx, priority, func(ctx context.Context) (PixelBuffer, error) {
		img, err := decode(ctx)
		if err != nil {
			return PixelBuffer{}, err
		}
		return PixelBufferFromImage(img)
	}, onReady, opts)
}

// QueueTexturePixels is [Uploader.QueueTexture] for raw [PixelBuffer] data, matching [UploadTexturePixels]: the buffer is validated, and RG8 or R8 data keeps its format. The decoded buffer's Pixels must not change until the handle is done.
//
// onReady is called as [Uploader.QueueMesh] describes.
func (u *Uploader) QueueTexturePixels(ctx context.Context, priority UploadPriority, decode func(context.Context) (PixelBuffer, error), onReady func(*GPUTexture, error), opts TextureUpload) *UploadHandle {
	job := &uploadJob{
		kind:          kindTexture,
		priority:      priority,
		decodeTexture: decode,
		texOpts:       opts,
		onTexReady:    onReady,
		heapIdx:       -1,
	}
	if decode == nil {
		return u.reject(job, fmt.Errorf("%w: nil decode function", ErrInvalidTexture))
	}
	return u.enqueue(ctx, job)
}

// QueueRawBuffer runs decode on a background worker and streams the raw GPU buffer bytes it returns across [Uploader.Step] calls within the per-frame byte budget.
//
// onReady is called as [Uploader.QueueMesh] describes.
func (u *Uploader) QueueRawBuffer(ctx context.Context, priority UploadPriority, decode func(context.Context) (RawBufferUpload, error), onReady func(*gpu.Buffer, error)) *UploadHandle {
	job := &uploadJob{
		kind:          kindRawBuffer,
		priority:      priority,
		decodeRawBuf:  decode,
		onRawBufReady: onReady,
		heapIdx:       -1,
	}
	if decode == nil {
		return u.reject(job, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: nil decode function"})
	}
	return u.enqueue(ctx, job)
}

// QueueRawTexture runs decode on a background worker and streams the pre-encoded uncompressed or block-compressed mip levels, array layers or cube faces it returns across [Uploader.Step] calls within the per-frame byte budget.
//
// onReady is called as [Uploader.QueueMesh] describes.
func (u *Uploader) QueueRawTexture(ctx context.Context, priority UploadPriority, decode func(context.Context) (RawTextureUpload, error), onReady func(*gpu.Texture, error)) *UploadHandle {
	job := &uploadJob{
		kind:          kindRawTexture,
		priority:      priority,
		decodeRawTex:  decode,
		onRawTexReady: onReady,
		heapIdx:       -1,
	}
	if decode == nil {
		return u.reject(job, fmt.Errorf("%w: nil decode function", ErrInvalidTexture))
	}
	return u.enqueue(ctx, job)
}

// reject fails job at once on the caller's goroutine.
func (u *Uploader) reject(job *uploadJob, err error) *UploadHandle {
	job.invokeError(err)
	return newImmediateErrorHandle(err)
}

func (u *Uploader) enqueue(ctx context.Context, job *uploadJob) *UploadHandle {
	if u == nil {
		return u.reject(job, ErrUploaderClosed)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return u.reject(job, ErrUploaderClosed)
	}
	if u.maxPending > 0 && u.pending >= u.maxPending {
		u.mu.Unlock()
		return u.reject(job, ErrUploadQueueFull)
	}

	job.seq = u.nextSeq
	u.nextSeq++
	job.handle = newUploadHandle(u, job)

	jobCtx, cancel := context.WithCancel(ctx)
	job.ctx = jobCtx
	job.cancelCtx = cancel
	job.stopRootFn = context.AfterFunc(u.rootCtx, cancel)

	u.pending++
	if err := ctx.Err(); err != nil {
		// Caller context is already canceled/expired; skip worker scheduling and deliver cleanly on the next Step.
		job.decodeErr = contextUploadError(err, false)
		job.loc = locFinishQueue
		u.finishQueue = append(u.finishQueue, job)
		u.mu.Unlock()
		return job.handle
	}

	job.loc = locDecodeHeap
	heap.Push(&u.decodeHeap, job)
	u.cond.Signal()
	u.mu.Unlock()
	return job.handle
}

func (u *Uploader) cancelJob(job *uploadJob) {
	// Cancel decode context promptly without holding u.mu.
	if job.cancelCtx != nil {
		job.cancelCtx()
	}

	u.mu.Lock()
	switch job.loc {
	case locDecodeHeap:
		heap.Remove(&u.decodeHeap, job.heapIdx)
		job.decodeErr = ErrUploadCanceled
		job.loc = locFinishQueue
		u.finishQueue = append(u.finishQueue, job)
	case locUploadHeap:
		heap.Remove(&u.uploadHeap, job.heapIdx)
		job.decodeErr = ErrUploadCanceled
		job.loc = locFinishQueue
		u.finishQueue = append(u.finishQueue, job)
	case locDecoding, locStepping, locFinishQueue, locDone:
		// Worker or Step observes handle.canceled and handles cleanup cleanly.
	}
	u.mu.Unlock()
}

func (u *Uploader) workerLoop() {
	defer u.wg.Done()
	for {
		u.mu.Lock()
		for !u.closed && len(u.decodeHeap) == 0 {
			u.cond.Wait()
		}
		if u.closed {
			u.mu.Unlock()
			return
		}
		job := heap.Pop(&u.decodeHeap).(*uploadJob)
		job.loc = locDecoding
		u.inFlightDecodes++
		job.handle.state.Store(uint32(UploadDecoding))
		u.mu.Unlock()

		decodeErr := u.runDecode(job)

		u.mu.Lock()
		u.inFlightDecodes--
		if u.closed {
			// Close joins the workers before it drains finishQueue, so it reports this job and calls its onReady.
			job.releaseStaging()
			job.decodeErr = ErrUploaderClosed
			job.loc = locFinishQueue
			u.finishQueue = append(u.finishQueue, job)
			u.mu.Unlock()
			return
		}
		if job.handle.canceled.Load() {
			job.releaseStaging()
			job.decodeErr = ErrUploadCanceled
			job.loc = locFinishQueue
			u.finishQueue = append(u.finishQueue, job)
		} else if ctxErr := job.ctx.Err(); ctxErr != nil {
			job.releaseStaging()
			job.decodeErr = contextUploadError(ctxErr, false)
			job.loc = locFinishQueue
			u.finishQueue = append(u.finishQueue, job)
		} else if decodeErr != nil {
			job.releaseStaging()
			job.decodeErr = decodeErr
			job.loc = locFinishQueue
			u.finishQueue = append(u.finishQueue, job)
		} else {
			job.handle.total.Store(job.totalBytes())
			job.loc = locUploadHeap
			heap.Push(&u.uploadHeap, job)
		}
		u.mu.Unlock()
	}
}

func (u *Uploader) runDecode(job *uploadJob) error {
	if job.handle.canceled.Load() {
		return ErrUploadCanceled
	}
	if err := job.ctx.Err(); err != nil {
		return contextUploadError(err, false)
	}

	switch job.kind {
	case kindMesh:
		m, err := job.decodeMesh(job.ctx)
		if err != nil {
			return err
		}
		if err := job.ctx.Err(); err != nil {
			return contextUploadError(err, job.handle.canceled.Load())
		}
		if err := m.Validate(); err != nil {
			return err
		}
		job.vertexCount = uint32(len(m.Positions))
		job.indexCount = uint32(len(m.Indices))
		job.indexFormat = m.IndexFormat()
		job.bounds = m.Bounds()
		job.vertexBytes = m.AppendVertexData(nil)
		if m.Indexed() {
			if err := job.ctx.Err(); err != nil {
				return contextUploadError(err, job.handle.canceled.Load())
			}
			job.indexBytes = m.AppendIndexData(nil)
		}
		return nil

	case kindTexture:
		buf, err := job.decodeTexture(job.ctx)
		if err != nil {
			return err
		}
		if err := job.ctx.Err(); err != nil {
			return contextUploadError(err, job.handle.canceled.Load())
		}
		desc, mips, err := packPixelTexture(job.ctx, buf, job.texOpts)
		if err != nil {
			return err
		}
		job.texDesc = desc
		job.mips = mips
		return nil

	case kindRawBuffer:
		raw, err := job.decodeRawBuf(job.ctx)
		if err != nil {
			return err
		}
		if err := job.ctx.Err(); err != nil {
			return contextUploadError(err, job.handle.canceled.Load())
		}
		if raw.Usage == 0 {
			return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: raw buffer requires at least one Usage flag"}
		}
		if len(raw.Data) == 0 {
			return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: raw buffer data must not be empty"}
		}
		dataCopy := make([]byte, len(raw.Data))
		copy(dataCopy, raw.Data)
		job.rawBufDesc = gpu.BufferDesc{
			Label:    raw.Label,
			Size:     uint64(len(dataCopy)),
			Usage:    raw.Usage | gpu.UsageCopyDst,
			Retained: raw.Retained,
		}
		job.rawBufBytes = dataCopy
		return nil

	case kindRawTexture:
		raw, err := job.decodeRawTex(job.ctx)
		if err != nil {
			return err
		}
		if err := job.ctx.Err(); err != nil {
			return contextUploadError(err, job.handle.canceled.Load())
		}
		desc, mips, err := packRawTexture(job.ctx, raw)
		if err != nil {
			return err
		}
		job.texDesc = desc
		job.mips = mips
		return nil

	default:
		return errors.New("render3d: unknown upload kind")
	}
}

// Step advances GPU uploads on the engine goroutine without exceeding the configured per-frame byte budget. Call once per frame (for example inside [gpu.ViewProps.OnFrame]).
func (u *Uploader) Step() UploadFrameStats {
	start := time.Now()
	var stats UploadFrameStats
	if u == nil {
		stats.FrameDuration = time.Since(start)
		return stats
	}

	budget := int(u.bytesPerFrame.Load())
	if budget <= 0 {
		budget = DefaultUploadBytesPerFrame
	}
	chunkMax := int(u.chunkBytes.Load())
	if chunkMax <= 0 || chunkMax > budget {
		chunkMax = budget
	}
	remaining := budget

	// 1. Drain completed errors / cancellations first (0 GPU upload bytes).
	for {
		u.mu.Lock()
		if u.closed || len(u.finishQueue) == 0 {
			u.mu.Unlock()
			break
		}
		job := u.finishQueue[0]
		u.finishQueue[0] = nil
		u.finishQueue = u.finishQueue[1:]
		if len(u.finishQueue) == 0 {
			u.finishQueue = u.finishQueue[:0]
		}
		job.loc = locDone
		u.pending--
		u.mu.Unlock()
		finishQueuedJob(job, &stats)
	}

	// 2. Stream ready payloads in priority order within remaining byte budget.
	for remaining > 0 {
		u.mu.Lock()
		if u.closed || len(u.uploadHeap) == 0 {
			u.mu.Unlock()
			break
		}
		job := heap.Pop(&u.uploadHeap).(*uploadJob)
		job.loc = locStepping
		u.mu.Unlock()

		// Re-check closure and cancellation before touching GPU resources.
		if u.closedFlag.Load() {
			u.abortClosedSteppedJob(job)
			break
		}
		if canceled, st, err := checkJobCancellation(job); canceled {
			u.finishSteppedJob(job, st, err, &stats)
			continue
		}

		// Allocate GPU resource on first upload step.
		if !job.allocated {
			if err := u.allocateGPU(job); err != nil {
				u.finishSteppedJob(job, UploadFailed, err, &stats)
				continue
			}
			job.allocated = true
			job.handle.state.Store(uint32(UploadUploading))
		}

		// Stream chunks until the job finishes, budget runs out, or an error occurs.
		hitBudget := false
		closedDuringStep := false
		var stepErr error
		stepState := UploadReady
		for !job.uploadDone() {
			if u.closedFlag.Load() {
				closedDuringStep = true
				break
			}
			if canceled, st, err := checkJobCancellation(job); canceled {
				stepState = st
				stepErr = err
				break
			}
			if !u.jobGPUValid(job) {
				stepState = UploadFailed
				stepErr = gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: device lost during upload"}
				break
			}
			if remaining <= 0 {
				hitBudget = true
				break
			}
			written, stalled := u.uploadOneChunk(job, remaining, chunkMax, stats.BytesUploaded == 0)
			if written > 0 {
				remaining -= written
				stats.BytesUploaded += written
				job.handle.uploaded.Add(int64(written))
			}
			if stalled {
				hitBudget = true
				break
			}
		}

		if closedDuringStep {
			u.abortClosedSteppedJob(job)
			break
		}
		if stepErr != nil {
			u.finishSteppedJob(job, stepState, stepErr, &stats)
			continue
		}

		if job.uploadDone() {
			// Final validation before handing the resource to caller.
			if u.closedFlag.Load() {
				u.abortClosedSteppedJob(job)
				break
			}
			if canceled, st, err := checkJobCancellation(job); canceled {
				u.finishSteppedJob(job, st, err, &stats)
				continue
			}
			if !u.jobGPUValid(job) {
				u.finishSteppedJob(job, UploadFailed, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: device lost during upload"}, &stats)
				continue
			}
			u.completeSteppedJob(job, &stats)
			continue
		}

		// Job is partially uploaded and waiting for next frame's budget.
		u.mu.Lock()
		if u.closed {
			job.loc = locDone
			u.pending--
			u.mu.Unlock()
			job.closePartialGPU()
			job.releaseStaging()
			job.cleanupContext()
			job.handle.finish(UploadFailed, ErrUploaderClosed)
			job.invokeError(ErrUploaderClosed)
			break
		}
		if job.handle.canceled.Load() {
			job.loc = locDone
			u.pending--
			u.mu.Unlock()
			job.closePartialGPU()
			job.releaseStaging()
			job.cleanupContext()
			stats.Canceled++
			job.handle.finish(UploadCanceled, ErrUploadCanceled)
			job.invokeError(ErrUploadCanceled)
			if hitBudget {
				break
			}
			continue
		}
		job.loc = locUploadHeap
		heap.Push(&u.uploadHeap, job)
		u.mu.Unlock()

		if hitBudget {
			break
		}
	}

	u.mu.Lock()
	stats.Pending = u.pending
	stats.InFlightDecodes = u.inFlightDecodes
	u.mu.Unlock()
	stats.FrameDuration = time.Since(start)
	return stats
}

// finishQueuedJob reports a job taken off finishQueue as failed or canceled with the error it carries and calls its onReady.
func finishQueuedJob(job *uploadJob, stats *UploadFrameStats) {
	job.closePartialGPU()
	job.releaseStaging()
	job.cleanupContext()

	err := job.decodeErr
	st := UploadFailed
	if job.handle.canceled.Load() || errors.Is(err, ErrUploadCanceled) {
		st = UploadCanceled
		if err == nil {
			err = ErrUploadCanceled
		}
		stats.Canceled++
	} else {
		if err == nil {
			err = errors.New("render3d: upload failed")
		}
		stats.Failed++
	}
	job.handle.finish(st, err)
	job.invokeError(err)
}

func (u *Uploader) abortClosedSteppedJob(job *uploadJob) {
	u.mu.Lock()
	job.loc = locDone
	u.pending--
	u.mu.Unlock()

	job.closePartialGPU()
	job.releaseStaging()
	job.cleanupContext()
	job.handle.finish(UploadFailed, ErrUploaderClosed)
	job.invokeError(ErrUploaderClosed)
}

func (u *Uploader) finishSteppedJob(job *uploadJob, st UploadState, err error, stats *UploadFrameStats) {
	u.mu.Lock()
	job.loc = locDone
	u.pending--
	u.mu.Unlock()

	job.closePartialGPU()
	job.releaseStaging()
	job.cleanupContext()
	if st == UploadCanceled {
		stats.Canceled++
	} else {
		stats.Failed++
	}
	job.handle.finish(st, err)
	job.invokeError(err)
}

func (u *Uploader) completeSteppedJob(job *uploadJob, stats *UploadFrameStats) {
	u.mu.Lock()
	job.loc = locDone
	u.pending--
	u.mu.Unlock()

	job.releaseStaging()
	job.cleanupContext()
	stats.Completed++
	job.handle.finish(UploadReady, nil)
	job.invokeReady()
}

func checkJobCancellation(job *uploadJob) (bool, UploadState, error) {
	if job.handle.canceled.Load() {
		return true, UploadCanceled, ErrUploadCanceled
	}
	if err := job.ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return true, UploadCanceled, contextUploadError(err, false)
		}
		return true, UploadFailed, contextUploadError(err, false)
	}
	return false, UploadReady, nil
}

func contextUploadError(err error, explicitCancel bool) error {
	if explicitCancel {
		return ErrUploadCanceled
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %w", ErrUploadCanceled, err)
	}
	return err
}

func (u *Uploader) allocateGPU(job *uploadJob) error {
	if u.dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if u.dev.IsLost() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: device is lost"}
	}

	switch job.kind {
	case kindMesh:
		vb, err := u.dev.CreateBuffer(u.scope, gpu.BufferDesc{
			Label:    bufferLabel(job.meshOpts.Label, "vertices"),
			Size:     uint64(len(job.vertexBytes)),
			Usage:    gpu.UsageVertex | gpu.UsageCopyDst,
			Retained: job.meshOpts.Retained,
		})
		if err != nil {
			return err
		}
		var ib *gpu.Buffer
		if len(job.indexBytes) > 0 {
			ib, err = u.dev.CreateBuffer(u.scope, gpu.BufferDesc{
				Label:    bufferLabel(job.meshOpts.Label, "indices"),
				Size:     uint64(len(job.indexBytes)),
				Usage:    gpu.UsageIndex | gpu.UsageCopyDst,
				Retained: job.meshOpts.Retained,
			})
			if err != nil {
				_ = vb.Close()
				return err
			}
		}
		job.gpuMesh = &GPUMesh{
			vertices:    vb,
			indices:     ib,
			indexFormat: job.indexFormat,
			vertexCount: job.vertexCount,
			indexCount:  job.indexCount,
			bounds:      job.bounds,
		}
		return nil

	case kindRawBuffer:
		buf, err := u.dev.CreateBuffer(u.scope, job.rawBufDesc)
		if err != nil {
			return err
		}
		job.gpuBuf = buf
		return nil

	case kindTexture, kindRawTexture:
		tex, err := u.dev.CreateTexture(u.scope, job.texDesc)
		if err != nil {
			return err
		}
		job.gpuTex = tex
		return nil

	default:
		return errors.New("render3d: unknown upload kind")
	}
}

func (u *Uploader) jobGPUValid(job *uploadJob) bool {
	if u.dev == nil || u.dev.IsLost() {
		return false
	}
	switch job.kind {
	case kindMesh:
		return job.gpuMesh != nil && job.gpuMesh.Valid()
	case kindRawBuffer:
		return job.gpuBuf != nil && job.gpuBuf.Valid()
	case kindTexture, kindRawTexture:
		return job.gpuTex != nil && job.gpuTex.Valid()
	default:
		return false
	}
}

// uploadOneChunk writes at most min(remaining, chunkMax) bytes for job. If the remaining budget cannot fit the minimum alignment unit and work has already been uploaded in this frame (allowForce == false), it returns (0, true) so Step stops without exceeding BytesPerFrame.
func (u *Uploader) uploadOneChunk(job *uploadJob, remaining, chunkMax int, allowForce bool) (written int, stalled bool) {
	maxWrite := min(remaining, chunkMax)

	switch job.kind {
	case kindMesh:
		if job.stage == 0 {
			n, st := writeBufferChunk(u.dev, job.gpuMesh.vertices, job.vertexBytes, &job.byteOffset, maxWrite, allowForce)
			if job.byteOffset >= len(job.vertexBytes) && len(job.indexBytes) > 0 {
				job.stage = 1
				job.byteOffset = 0
			}
			return n, st
		}
		return writeBufferChunk(u.dev, job.gpuMesh.indices, job.indexBytes, &job.byteOffset, maxWrite, allowForce)

	case kindRawBuffer:
		return writeBufferChunk(u.dev, job.gpuBuf, job.rawBufBytes, &job.byteOffset, maxWrite, allowForce)

	case kindTexture, kindRawTexture:
		return writeTextureChunk(u.dev, job.gpuTex, job.mips, &job.mipIdx, &job.curRow, &job.curX, maxWrite, allowForce)

	default:
		return 0, true
	}
}

func writeBufferChunk(dev *gpu.Device, buf *gpu.Buffer, data []byte, offset *int, maxWrite int, allowForce bool) (int, bool) {
	left := len(data) - *offset
	if left <= 0 {
		return 0, false
	}
	n := min(left, maxWrite)
	// Keep intermediate writes 4-byte aligned for WebGPU WriteBuffer portability.
	if n < left {
		n &^= 3
		if n == 0 {
			if !allowForce {
				return 0, true
			}
			n = min(left, 4)
		}
	}
	off := *offset
	dev.WriteBuffer(buf, uint64(off), data[off:off+n])
	*offset = off + n
	return n, false
}

func writeTextureChunk(
	dev *gpu.Device,
	tex *gpu.Texture,
	mips []packedMip,
	mipIdx *int,
	curRow *uint32,
	curX *uint32,
	maxWrite int,
	allowForce bool,
) (int, bool) {
	if *mipIdx >= len(mips) {
		return 0, false
	}
	m := &mips[*mipIdx]

	if m.compressed {
		totalBlockRows := (m.height + m.blockHeight - 1) / m.blockHeight
		if *curRow >= totalBlockRows {
			*mipIdx++
			*curRow = 0
			*curX = 0
			return 0, false
		}
		rowBytes := int(m.bytesPerRow)
		rows := min(int(totalBlockRows-*curRow), maxWrite/rowBytes)
		if rows == 0 {
			if !allowForce {
				return 0, true
			}
			rows = 1
		}
		n := rows * rowBytes
		start := int(*curRow) * rowBytes
		dev.WriteTexture(tex, gpu.TextureCopy{
			Texture:    tex,
			MipLevel:   m.mipLevel,
			ArrayLayer: m.arrayLayer,
			OriginX:    0,
			OriginY:    *curRow * m.blockHeight,
		}, m.data[start:start+n], gpu.DataLayout{
			BytesPerRow:  m.bytesPerRow,
			RowsPerImage: uint32(rows),
		})
		*curRow += uint32(rows)
		if *curRow >= totalBlockRows {
			*mipIdx++
			*curRow = 0
			*curX = 0
		}
		return n, false
	}

	// Uncompressed texture: prefer full row bands; split a single row horizontally when a full row does not fit in maxWrite so even tiny budgets are respected.
	if *curRow >= m.height {
		*mipIdx++
		*curRow = 0
		*curX = 0
		return 0, false
	}
	bpp := int(m.bytesPerUnit)
	rowBytes := int(m.bytesPerRow)

	if *curX == 0 && maxWrite >= rowBytes {
		rows := min(int(m.height-*curRow), maxWrite/rowBytes)
		n := rows * rowBytes
		start := int(*curRow) * rowBytes
		dev.WriteTextureRegion(gpu.TextureWrite{
			Texture:    tex,
			MipLevel:   m.mipLevel,
			ArrayLayer: m.arrayLayer,
			OriginX:    0,
			OriginY:    *curRow,
			Width:      m.width,
			Height:     uint32(rows),
		}, m.data[start:start+n], gpu.DataLayout{
			BytesPerRow:  m.bytesPerRow,
			RowsPerImage: uint32(rows),
		})
		*curRow += uint32(rows)
		if *curRow >= m.height {
			*mipIdx++
			*curRow = 0
			*curX = 0
		}
		return n, false
	}

	cols := min(int(m.width-*curX), maxWrite/bpp)
	if cols == 0 {
		if !allowForce {
			return 0, true
		}
		cols = 1
	}
	n := cols * bpp
	start := int(*curRow)*rowBytes + int(*curX)*bpp
	dev.WriteTextureRegion(gpu.TextureWrite{
		Texture:    tex,
		MipLevel:   m.mipLevel,
		ArrayLayer: m.arrayLayer,
		OriginX:    *curX,
		OriginY:    *curRow,
		Width:      uint32(cols),
		Height:     1,
	}, m.data[start:start+n], gpu.DataLayout{
		BytesPerRow:  uint32(n),
		RowsPerImage: 1,
	})
	*curX += uint32(cols)
	if *curX >= m.width {
		*curX = 0
		*curRow++
		if *curRow >= m.height {
			*mipIdx++
			*curRow = 0
		}
	}
	return n, false
}

// Close cancels every unfinished upload, waits for the decode workers to exit, releases partially uploaded GPU resources, and calls the onReady of each unfinished upload with [ErrUploaderClosed] (or with the error it had already failed with). Resources already delivered stay owned by their scope or caller. Call Close on the goroutine that calls [Uploader.Step], as it releases GPU resources and runs onReady callbacks; disposing the scope passed to [NewUploader] does. Close may be called more than once, and from an onReady callback.
func (u *Uploader) Close() error {
	if u == nil {
		return nil
	}

	u.mu.Lock()
	alreadyClosed := u.closed
	u.closed = true
	u.closedFlag.Store(true)
	u.mu.Unlock()

	if !alreadyClosed {
		u.rootCancel()
		u.mu.Lock()
		u.cond.Broadcast()
		u.mu.Unlock()
	}

	// Wait for all worker goroutines to exit (zero goroutine leaks).
	u.wg.Wait()

	u.mu.Lock()
	var drained []*uploadJob
	for len(u.decodeHeap) > 0 {
		j := heap.Pop(&u.decodeHeap).(*uploadJob)
		j.decodeErr = ErrUploaderClosed
		drained = append(drained, j)
	}
	for len(u.uploadHeap) > 0 {
		j := heap.Pop(&u.uploadHeap).(*uploadJob)
		j.decodeErr = ErrUploaderClosed
		drained = append(drained, j)
	}
	drained = append(drained, u.finishQueue...)
	u.finishQueue = nil
	for _, j := range drained {
		j.loc = locDone
		u.pending--
	}
	u.mu.Unlock()

	var stats UploadFrameStats
	for _, j := range drained {
		finishQueuedJob(j, &stats)
	}
	return nil
}

// packPixelTexture validates buf and builds the texture description and mip levels [UploadTexturePixels] would upload for it: the same format, label, mip count and CPU mip chain.
func packPixelTexture(ctx context.Context, buf PixelBuffer, opts TextureUpload) (gpu.TextureDesc, []packedMip, error) {
	if err := buf.Validate(); err != nil {
		return gpu.TextureDesc{}, nil, err
	}
	format, err := resolveTextureFormat(buf.Format, opts.SRGB)
	if err != nil {
		return gpu.TextureDesc{}, nil, err
	}
	w, h := buf.Width, buf.Height
	mipLevels := uint32(1)
	if opts.Mipmaps {
		mipLevels = gpu.MaxMipLevels(uint32(w), uint32(h))
		if opts.MaxMipLevels > 0 && opts.MaxMipLevels < mipLevels {
			mipLevels = opts.MaxMipLevels
		}
	}
	label := opts.Label
	if label == "" {
		label = "render3d.texture"
	}
	desc := gpu.TextureDesc{
		Label:     label,
		Dimension: gpu.Dim2D,
		Width:     uint32(w),
		Height:    uint32(h),
		Layers:    1,
		MipLevels: mipLevels,
		Format:    format,
		Usage:     gpu.TextureSampled | gpu.TextureCopyDst,
		Retained:  opts.Retained,
	}
	bpp := pixelBytesPerPixel(format)
	stride := buf.Stride
	if stride == 0 {
		stride = w * bpp
	}
	srgb := format.IsSRGB()
	mips := make([]packedMip, 0, mipLevels)
	cur := packPixelRows(buf.Pixels, w, h, bpp, stride)
	curW, curH := w, h
	for lvl := range mipLevels {
		if err := ctx.Err(); err != nil {
			return gpu.TextureDesc{}, nil, err
		}
		if lvl > 0 {
			nextW, nextH := max(1, curW>>1), max(1, curH>>1)
			cur = downsamplePixelsBox(cur, curW, curH, nextW, nextH, bpp, srgb)
			curW, curH = nextW, nextH
		}
		mips = append(mips, packedMip{
			mipLevel:     lvl,
			width:        uint32(curW),
			height:       uint32(curH),
			bytesPerRow:  uint32(curW * bpp),
			blockHeight:  1,
			bytesPerUnit: uint32(bpp),
			data:         cur,
		})
	}
	return desc, mips, nil
}

func packRawTexture(ctx context.Context, raw RawTextureUpload) (gpu.TextureDesc, []packedMip, error) {
	desc := raw.Desc
	if desc.Width == 0 || desc.Height == 0 {
		return gpu.TextureDesc{}, nil, fmt.Errorf("%w: non-positive dimensions %dx%d", ErrInvalidTexture, desc.Width, desc.Height)
	}
	if desc.Format == gpu.FormatUndefined {
		return gpu.TextureDesc{}, nil, fmt.Errorf("%w: format must be specified", ErrInvalidTexture)
	}
	if desc.Format.IsDepthStencil() {
		return gpu.TextureDesc{}, nil, fmt.Errorf("%w: depth/stencil formats cannot be uploaded as raw textures", ErrInvalidTexture)
	}
	if len(raw.Mips) == 0 {
		return gpu.TextureDesc{}, nil, fmt.Errorf("%w: raw texture must include at least one mip", ErrInvalidTexture)
	}
	if desc.Usage == 0 {
		desc.Usage = gpu.TextureSampled | gpu.TextureCopyDst
	} else {
		desc.Usage |= gpu.TextureCopyDst
	}
	if desc.Layers == 0 {
		if desc.Dimension == gpu.DimCube {
			desc.Layers = 6
		} else {
			desc.Layers = 1
		}
	}
	if desc.MipLevels == 0 {
		maxSeen := uint32(1)
		for _, m := range raw.Mips {
			if m.MipLevel+1 > maxSeen {
				maxSeen = m.MipLevel + 1
			}
		}
		desc.MipLevels = maxSeen
	}
	if maxMips := gpu.MaxMipLevels(desc.Width, desc.Height); desc.MipLevels > maxMips {
		return gpu.TextureDesc{}, nil, fmt.Errorf("%w: mip levels %d exceed maximum %d", ErrInvalidTexture, desc.MipLevels, maxMips)
	}

	bw, bh := desc.Format.BlockSize()
	bpb := desc.Format.BytesPerBlock()
	if bw == 0 || bh == 0 || bpb == 0 {
		return gpu.TextureDesc{}, nil, fmt.Errorf("%w: unsupported format %s", ErrInvalidTexture, desc.Format)
	}
	compressed := desc.Format.IsCompressed()

	mips := make([]packedMip, 0, len(raw.Mips))
	for i, m := range raw.Mips {
		if err := ctx.Err(); err != nil {
			return gpu.TextureDesc{}, nil, err
		}
		if m.MipLevel >= desc.MipLevels {
			return gpu.TextureDesc{}, nil, fmt.Errorf("%w: mip entry %d has mip level %d >= %d", ErrInvalidTexture, i, m.MipLevel, desc.MipLevels)
		}
		if m.ArrayLayer >= desc.Layers {
			return gpu.TextureDesc{}, nil, fmt.Errorf("%w: mip entry %d has array layer %d >= %d", ErrInvalidTexture, i, m.ArrayLayer, desc.Layers)
		}
		mw, mh := gpu.MipSize(desc.Width, desc.Height, m.MipLevel)
		unitsX := (mw + bw - 1) / bw
		unitsY := (mh + bh - 1) / bh
		tightRow := unitsX * bpb
		srcRow := m.BytesPerRow
		if srcRow == 0 {
			srcRow = tightRow
		}
		if srcRow < tightRow {
			return gpu.TextureDesc{}, nil, fmt.Errorf("%w: mip %d bytesPerRow %d smaller than tight row %d", ErrInvalidTexture, m.MipLevel, srcRow, tightRow)
		}
		minLen := uint64(unitsY-1)*uint64(srcRow) + uint64(tightRow)
		if uint64(len(m.Data)) < minLen {
			return gpu.TextureDesc{}, nil, fmt.Errorf("%w: mip %d data length %d smaller than required %d", ErrInvalidTexture, m.MipLevel, len(m.Data), minLen)
		}
		packed := make([]byte, uint64(unitsY)*uint64(tightRow))
		if srcRow == tightRow {
			copy(packed, m.Data[:len(packed)])
		} else {
			for y := uint32(0); y < unitsY; y++ {
				si := uint64(y) * uint64(srcRow)
				di := uint64(y) * uint64(tightRow)
				copy(packed[di:di+uint64(tightRow)], m.Data[si:si+uint64(tightRow)])
			}
		}
		mips = append(mips, packedMip{
			mipLevel:     m.MipLevel,
			arrayLayer:   m.ArrayLayer,
			width:        mw,
			height:       mh,
			bytesPerRow:  tightRow,
			blockHeight:  bh,
			bytesPerUnit: bpb,
			compressed:   compressed,
			data:         packed,
		})
	}
	return desc, mips, nil
}
