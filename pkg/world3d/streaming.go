package world3d

import (
	"context"
	"math"
	"slices"
	"sync"

	"github.com/nsgo-dev/game/pkg/math3d"
)

const defaultFloatingOriginThreshold = 512.0

// WorldPos64 is a double-precision 3D world-space position used for large
// worlds (tens to hundreds of kilometers) where float32 world coordinates
// would suffer noticeable precision jitter.
type WorldPos64 struct {
	X, Y, Z float64
}

// Add returns p + b.
func (p WorldPos64) Add(b WorldPos64) WorldPos64 {
	return WorldPos64{X: p.X + b.X, Y: p.Y + b.Y, Z: p.Z + b.Z}
}

// Sub returns p - b.
func (p WorldPos64) Sub(b WorldPos64) WorldPos64 {
	return WorldPos64{X: p.X - b.X, Y: p.Y - b.Y, Z: p.Z - b.Z}
}

// DistSq returns the squared Euclidean distance between p and b.
func (p WorldPos64) DistSq(b WorldPos64) float64 {
	dx := p.X - b.X
	dy := p.Y - b.Y
	dz := p.Z - b.Z
	return dx*dx + dy*dy + dz*dz
}

// Dist returns the Euclidean distance between p and b.
func (p WorldPos64) Dist(b WorldPos64) float64 {
	return math.Sqrt(p.DistSq(b))
}

// ChunkCoord returns the 2D XZ streaming chunk coordinate containing p for a
// world partitioned into square chunks of size chunkSize.
func (p WorldPos64) ChunkCoord(chunkSize float64) ChunkCoord {
	if chunkSize <= 0 {
		chunkSize = 64
	}
	return ChunkCoord{
		X: int32(math.Floor(p.X / chunkSize)),
		Z: int32(math.Floor(p.Z / chunkSize)),
	}
}

// ChunkCoord identifies a 2D streaming chunk in the XZ world plane.
type ChunkCoord struct {
	X, Z int32
}

// FloatingOrigin maintains a double-precision world origin offset so that local
// float32 physics, animation, and rendering coordinates stay near (0, 0, 0).
type FloatingOrigin struct {
	Origin    WorldPos64
	Threshold float64
}

// NewFloatingOrigin constructs a FloatingOrigin with the given rebase distance
// threshold (in world units). Non-positive thresholds default to 512.
func NewFloatingOrigin(threshold float64) *FloatingOrigin {
	if threshold <= 0 {
		threshold = defaultFloatingOriginThreshold
	}
	return &FloatingOrigin{Threshold: threshold}
}

// Update checks whether playerLocal has moved farther than Threshold from the
// local origin (0, 0, 0). When exceeded, Origin advances by playerLocal and
// Update returns (true, shift) where shift == -playerLocal is the additive
// offset to apply to every active local position/transform (pos = pos.Add(shift))
// so playerLocal rebases back to (0, 0, 0) while preserving exact WorldPos64
// coordinates.
func (f *FloatingOrigin) Update(playerLocal math3d.Vec3) (rebased bool, shift math3d.Vec3) {
	if f == nil {
		return false, math3d.Vec3{}
	}
	thresh := f.Threshold
	if thresh <= 0 {
		thresh = defaultFloatingOriginThreshold
	}
	px := float64(playerLocal.X)
	py := float64(playerLocal.Y)
	pz := float64(playerLocal.Z)
	if px*px+py*py+pz*pz <= thresh*thresh {
		return false, math3d.Vec3{}
	}
	f.Origin.X += px
	f.Origin.Y += py
	f.Origin.Z += pz
	return true, playerLocal.Neg()
}

// WorldToLocal converts double-precision world position w into local float32
// space by subtracting Origin in float64 before narrowing to float32.
func (f *FloatingOrigin) WorldToLocal(w WorldPos64) math3d.Vec3 {
	if f == nil {
		return math3d.Vec3{X: float32(w.X), Y: float32(w.Y), Z: float32(w.Z)}
	}
	return math3d.Vec3{
		X: float32(w.X - f.Origin.X),
		Y: float32(w.Y - f.Origin.Y),
		Z: float32(w.Z - f.Origin.Z),
	}
}

// LocalToWorld converts a local float32 position back into double-precision
// world coordinates.
func (f *FloatingOrigin) LocalToWorld(local math3d.Vec3) WorldPos64 {
	if f == nil {
		return WorldPos64{X: float64(local.X), Y: float64(local.Y), Z: float64(local.Z)}
	}
	return WorldPos64{
		X: f.Origin.X + float64(local.X),
		Y: f.Origin.Y + float64(local.Y),
		Z: f.Origin.Z + float64(local.Z),
	}
}

// RebaseMat4 applies a floating-origin shift to the translation column of m.
func RebaseMat4(m math3d.Mat4, shift math3d.Vec3) math3d.Mat4 {
	m[12] += shift.X
	m[13] += shift.Y
	m[14] += shift.Z
	return m
}

// ChunkStreamerConfig configures a ChunkStreamer.
type ChunkStreamerConfig[T any] struct {
	// ChunkSize is the width/depth of each square XZ chunk in world units.
	// Defaults to 64 if <= 0.
	ChunkSize float64
	// LoadRadius is the Chebyshev chunk radius around the player chunk to load.
	LoadRadius int
	// UnloadRadius is the Chebyshev chunk radius beyond which loaded and
	// in-flight chunks are unloaded/canceled. Defaults to LoadRadius + 1 if
	// smaller than LoadRadius.
	UnloadRadius int
	// MaxIntegrationsPerFrame caps how many completed chunks are integrated
	// into the active map per Update call to prevent frame-time hitches.
	// Values <= 0 allow unlimited integrations per frame.
	MaxIntegrationsPerFrame int
	// Workers sets the number of background loader goroutines. When 0, Loader
	// executes synchronously inside Update (useful for deterministic tests).
	Workers int
	// Loader loads chunk data for coord. It must respect ctx cancellation.
	Loader func(ctx context.Context, coord ChunkCoord) (T, error)
	// OnLoad is called on the Update goroutine after a chunk is integrated.
	OnLoad func(coord ChunkCoord, data T)
	// OnUnload is called on the Update/Close goroutine after a chunk is unloaded.
	OnUnload func(coord ChunkCoord, data T)
	// OnError is called on the Update goroutine when Loader returns a non-nil error.
	OnError func(coord ChunkCoord, err error)
}

type chunkJob struct {
	coord ChunkCoord
	gen   uint64
	ctx   context.Context
}

type chunkInFlight struct {
	gen    uint64
	cancel context.CancelFunc
}

type chunkLoadResult[T any] struct {
	coord ChunkCoord
	gen   uint64
	data  T
	err   error
}

type chunkEntry[T any] struct {
	coord ChunkCoord
	data  T
}

type chunkErrEntry struct {
	coord ChunkCoord
	err   error
}

// ChunkStreamer asynchronously streams 2D world chunks around a moving
// WorldPos64 with bounded worker concurrency, per-frame integration budgets,
// hysteresis unloading, and cancellation of stale in-flight loads.
//
// Build one with NewChunkStreamer. A zero ChunkStreamer is usable too: it behaves like NewChunkStreamer with a zero config (synchronous, no loader, 64-unit chunks) instead of panicking (#546).
type ChunkStreamer[T any] struct {
	cfg ChunkStreamerConfig[T]

	initOnce sync.Once

	rootCtx    context.Context
	rootCancel context.CancelFunc

	mu       sync.Mutex
	cond     *sync.Cond
	wg       sync.WaitGroup
	closed   bool
	nextGen  uint64
	loaded   map[ChunkCoord]T
	inFlight map[ChunkCoord]chunkInFlight
	pending  []chunkJob
	ready    []chunkLoadResult[T]
}

// NewChunkStreamer constructs a ChunkStreamer and starts its worker goroutines
// when cfg.Workers > 0. Call Close when finished to stop workers and unload
// remaining chunks cleanly.
func NewChunkStreamer[T any](cfg ChunkStreamerConfig[T]) *ChunkStreamer[T] {
	s := &ChunkStreamer[T]{cfg: cfg}
	s.initOnce.Do(s.setup)
	for range s.cfg.Workers {
		s.wg.Add(1)
		go s.worker()
	}
	return s
}

// setup normalizes the config and allocates the streamer's state; it runs once, from NewChunkStreamer or from the first method called on a zero ChunkStreamer.
func (s *ChunkStreamer[T]) setup() {
	cfg := &s.cfg
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 64
	}
	if cfg.LoadRadius < 0 {
		cfg.LoadRadius = 0
	}
	if cfg.UnloadRadius < cfg.LoadRadius {
		cfg.UnloadRadius = cfg.LoadRadius + 1
	}
	if cfg.Workers < 0 {
		cfg.Workers = 0
	}
	s.rootCtx, s.rootCancel = context.WithCancel(context.Background())
	s.loaded = make(map[ChunkCoord]T)
	s.inFlight = make(map[ChunkCoord]chunkInFlight)
	s.cond = sync.NewCond(&s.mu)
}

func (s *ChunkStreamer[T]) worker() {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		for !s.closed && len(s.pending) == 0 {
			s.cond.Wait()
		}
		if s.closed {
			s.mu.Unlock()
			return
		}
		job := s.pending[0]
		s.pending = s.pending[1:]
		s.mu.Unlock()

		if job.ctx.Err() != nil {
			s.dropInFlight(job.coord, job.gen)
			continue
		}

		var data T
		var err error
		if s.cfg.Loader != nil {
			data, err = s.cfg.Loader(job.ctx, job.coord)
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		cur, ok := s.inFlight[job.coord]
		if !ok || cur.gen != job.gen || job.ctx.Err() != nil {
			if ok && cur.gen == job.gen {
				delete(s.inFlight, job.coord)
			}
			s.mu.Unlock()
			continue
		}
		s.ready = append(s.ready, chunkLoadResult[T]{
			coord: job.coord,
			gen:   job.gen,
			data:  data,
			err:   err,
		})
		s.mu.Unlock()
	}
}

func (s *ChunkStreamer[T]) dropInFlight(coord ChunkCoord, gen uint64) {
	s.mu.Lock()
	if cur, ok := s.inFlight[coord]; ok && cur.gen == gen {
		delete(s.inFlight, coord)
	}
	s.mu.Unlock()
}

func chunkChebyshevDist(a, b ChunkCoord) int {
	dx := int64(a.X) - int64(b.X)
	if dx < 0 {
		dx = -dx
	}
	dz := int64(a.Z) - int64(b.Z)
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return int(dx)
	}
	return int(dz)
}

func chunkDistSq(a, b ChunkCoord) int64 {
	dx := int64(a.X) - int64(b.X)
	dz := int64(a.Z) - int64(b.Z)
	return dx*dx + dz*dz
}

// Update advances streaming for worldPos:
//  1. Unloads loaded chunks beyond UnloadRadius and cancels stale in-flight loads.
//  2. Integrates up to MaxIntegrationsPerFrame completed chunks (closest first).
//  3. Enqueues missing chunks within LoadRadius (closest first).
//
// When Workers == 0, missing chunks are loaded synchronously up to the
// remaining per-frame integration budget.
func (s *ChunkStreamer[T]) Update(worldPos WorldPos64) {
	if s == nil {
		return
	}
	s.initOnce.Do(s.setup)
	center := worldPos.ChunkCoord(s.cfg.ChunkSize)

	var unloaded []chunkEntry[T]
	var integrated []chunkEntry[T]
	var loadErrors []chunkErrEntry
	var syncQueue []ChunkCoord

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}

	// 1. Unload loaded chunks outside UnloadRadius.
	for coord, data := range s.loaded {
		if chunkChebyshevDist(coord, center) > s.cfg.UnloadRadius {
			unloaded = append(unloaded, chunkEntry[T]{coord: coord, data: data})
			delete(s.loaded, coord)
		}
	}

	// Cancel stale in-flight loads outside UnloadRadius.
	canceledAny := false
	for coord, inflight := range s.inFlight {
		if chunkChebyshevDist(coord, center) > s.cfg.UnloadRadius {
			inflight.cancel()
			delete(s.inFlight, coord)
			canceledAny = true
		}
	}
	if canceledAny && len(s.pending) > 0 {
		kept := s.pending[:0]
		for _, job := range s.pending {
			if cur, ok := s.inFlight[job.coord]; ok && cur.gen == job.gen {
				kept = append(kept, job)
			}
		}
		s.pending = kept
	}

	// Prune stale/canceled entries from ready queue.
	if len(s.ready) > 0 {
		kept := s.ready[:0]
		for _, res := range s.ready {
			cur, ok := s.inFlight[res.coord]
			if !ok || cur.gen != res.gen {
				continue
			}
			if chunkChebyshevDist(res.coord, center) > s.cfg.UnloadRadius {
				cur.cancel()
				delete(s.inFlight, res.coord)
				continue
			}
			kept = append(kept, res)
		}
		s.ready = kept
	}

	// 2. Integrate completed chunks up to MaxIntegrationsPerFrame (closest first).
	budget := s.cfg.MaxIntegrationsPerFrame
	unlimited := budget <= 0
	integratedCount := 0

	if len(s.ready) > 0 {
		slices.SortFunc(s.ready, func(a, b chunkLoadResult[T]) int {
			da := chunkDistSq(a.coord, center)
			db := chunkDistSq(b.coord, center)
			if da < db {
				return -1
			}
			if da > db {
				return 1
			}
			return compareChunkCoord(a.coord, b.coord)
		})

		take := len(s.ready)
		if !unlimited && take > budget {
			take = budget
		}
		for i := range take {
			res := s.ready[i]
			if cur, ok := s.inFlight[res.coord]; ok && cur.gen == res.gen {
				cur.cancel()
				delete(s.inFlight, res.coord)
			}
			if res.err != nil {
				loadErrors = append(loadErrors, chunkErrEntry{coord: res.coord, err: res.err})
			} else {
				s.loaded[res.coord] = res.data
				integrated = append(integrated, chunkEntry[T]{coord: res.coord, data: res.data})
				integratedCount++
			}
		}
		remaining := copy(s.ready, s.ready[take:])
		clear(s.ready[remaining:])
		s.ready = s.ready[:remaining]
	}

	// 3. Enqueue missing chunks inside LoadRadius.
	r := s.cfg.LoadRadius
	enqueued := false
	for dz := -r; dz <= r; dz++ {
		for dx := -r; dx <= r; dx++ {
			coord := ChunkCoord{X: center.X + int32(dx), Z: center.Z + int32(dz)}
			if _, ok := s.loaded[coord]; ok {
				continue
			}
			if _, ok := s.inFlight[coord]; ok {
				continue
			}
			if s.cfg.Workers == 0 {
				syncQueue = append(syncQueue, coord)
				continue
			}
			s.nextGen++
			gen := s.nextGen
			ctx, cancel := context.WithCancel(s.rootCtx)
			s.inFlight[coord] = chunkInFlight{gen: gen, cancel: cancel}
			s.pending = append(s.pending, chunkJob{coord: coord, gen: gen, ctx: ctx})
			enqueued = true
		}
	}

	if enqueued {
		slices.SortFunc(s.pending, func(a, b chunkJob) int {
			da := chunkDistSq(a.coord, center)
			db := chunkDistSq(b.coord, center)
			if da < db {
				return -1
			}
			if da > db {
				return 1
			}
			return compareChunkCoord(a.coord, b.coord)
		})
		s.cond.Broadcast()
	}
	s.mu.Unlock()

	// Synchronous loading path (Workers == 0), respecting remaining integration budget.
	if len(syncQueue) > 0 {
		slices.SortFunc(syncQueue, func(a, b ChunkCoord) int {
			da := chunkDistSq(a, center)
			db := chunkDistSq(b, center)
			if da < db {
				return -1
			}
			if da > db {
				return 1
			}
			return compareChunkCoord(a, b)
		})
		limit := len(syncQueue)
		if !unlimited {
			remBudget := max(0, budget-integratedCount)
			if limit > remBudget {
				limit = remBudget
			}
		}
		for i := range limit {
			coord := syncQueue[i]
			var data T
			var err error
			if s.cfg.Loader != nil {
				data, err = s.cfg.Loader(s.rootCtx, coord)
			}
			if err != nil {
				loadErrors = append(loadErrors, chunkErrEntry{coord: coord, err: err})
				continue
			}
			s.mu.Lock()
			if !s.closed {
				s.loaded[coord] = data
				integrated = append(integrated, chunkEntry[T]{coord: coord, data: data})
			}
			s.mu.Unlock()
		}
	}

	// Run user callbacks outside s.mu to avoid lock re-entrancy deadlocks.
	if s.cfg.OnUnload != nil {
		for _, item := range unloaded {
			s.cfg.OnUnload(item.coord, item.data)
		}
	}
	if s.cfg.OnLoad != nil {
		for _, item := range integrated {
			s.cfg.OnLoad(item.coord, item.data)
		}
	}
	if s.cfg.OnError != nil {
		for _, item := range loadErrors {
			s.cfg.OnError(item.coord, item.err)
		}
	}
}

// Get returns the loaded chunk data for coord, or (zero, false) if not loaded.
func (s *ChunkStreamer[T]) Get(coord ChunkCoord) (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}
	s.initOnce.Do(s.setup)
	s.mu.Lock()
	data, ok := s.loaded[coord]
	s.mu.Unlock()
	return data, ok
}

// LoadedChunks returns a sorted slice of all currently loaded chunk coordinates.
func (s *ChunkStreamer[T]) LoadedChunks() []ChunkCoord {
	if s == nil {
		return nil
	}
	s.initOnce.Do(s.setup)
	s.mu.Lock()
	out := make([]ChunkCoord, 0, len(s.loaded))
	for coord := range s.loaded {
		out = append(out, coord)
	}
	s.mu.Unlock()
	slices.SortFunc(out, compareChunkCoord)
	return out
}

// PendingCount returns the number of chunks currently queued or loading in workers.
func (s *ChunkStreamer[T]) PendingCount() int {
	if s == nil {
		return 0
	}
	s.initOnce.Do(s.setup)
	s.mu.Lock()
	n := len(s.inFlight)
	s.mu.Unlock()
	return n
}

// ReadyCount returns the number of completed chunks awaiting integration on
// subsequent Update calls.
func (s *ChunkStreamer[T]) ReadyCount() int {
	if s == nil {
		return 0
	}
	s.initOnce.Do(s.setup)
	s.mu.Lock()
	n := len(s.ready)
	s.mu.Unlock()
	return n
}

// Close cancels all in-flight loads, waits for worker goroutines to exit, and
// unloads all remaining loaded chunks via OnUnload. Calling Close multiple
// times is safe.
func (s *ChunkStreamer[T]) Close() {
	if s == nil {
		return
	}
	s.initOnce.Do(s.setup)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.rootCancel()
	for coord, inflight := range s.inFlight {
		inflight.cancel()
		delete(s.inFlight, coord)
	}
	s.pending = nil
	s.ready = nil

	var unloaded []chunkEntry[T]
	if s.cfg.OnUnload != nil && len(s.loaded) > 0 {
		unloaded = make([]chunkEntry[T], 0, len(s.loaded))
		for coord, data := range s.loaded {
			unloaded = append(unloaded, chunkEntry[T]{coord: coord, data: data})
		}
	}
	clear(s.loaded)
	s.cond.Broadcast()
	s.mu.Unlock()

	s.wg.Wait()

	if s.cfg.OnUnload != nil {
		for _, item := range unloaded {
			s.cfg.OnUnload(item.coord, item.data)
		}
	}
}

func compareChunkCoord(a, b ChunkCoord) int {
	if a.X != b.X {
		if a.X < b.X {
			return -1
		}
		return 1
	}
	if a.Z < b.Z {
		return -1
	}
	if a.Z > b.Z {
		return 1
	}
	return 0
}
