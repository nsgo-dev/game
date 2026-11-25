package tilemap

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

// ChunkFormat selects the plain-text encoding used when persisting modified chunks.
type ChunkFormat int

const (
	// ChunkFormatYAML encodes persisted chunks as deterministic YAML (default).
	ChunkFormatYAML ChunkFormat = iota
	// ChunkFormatJSON encodes persisted chunks as deterministic JSON.
	ChunkFormatJSON
)

// EncodeChunk encodes c in format (ChunkFormatYAML or ChunkFormatJSON).
func EncodeChunk(c *Chunk, format ChunkFormat) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("tilemap: encode chunk: nil chunk")
	}
	doc := c.ToDoc()
	switch format {
	case ChunkFormatJSON:
		return EncodeChunkJSON(doc)
	case ChunkFormatYAML:
		return EncodeChunkYAML(doc)
	default:
		return nil, fmt.Errorf("tilemap: encode chunk: unknown format %d", format)
	}
}

// DecodeChunk decodes raw YAML or JSON bytes in format into a clean Chunk (Dirty=true, Modified=false).
func DecodeChunk(data []byte, format ChunkFormat) (*Chunk, error) {
	var (
		doc *ChunkDoc
		err error
	)
	switch format {
	case ChunkFormatJSON:
		doc, err = DecodeChunkJSON(data)
	case ChunkFormatYAML:
		doc, err = DecodeChunkYAML(data)
	default:
		return nil, fmt.Errorf("tilemap: decode chunk: unknown format %d", format)
	}
	if err != nil {
		return nil, err
	}
	return ChunkFromDoc(doc)
}

// ChunkStore loads and saves serialized plain-text chunk documents keyed by ChunkCoord.
// Implementations used with concurrent loaders (such as world3d.TilemapStreamer with Workers > 0) must be safe for concurrent use.
type ChunkStore interface {
	LoadChunk(coord ChunkCoord) (data []byte, found bool, err error)
	SaveChunk(coord ChunkCoord, data []byte) error
}

// MemoryChunkStore is an in-memory, concurrency-safe ChunkStore storing plain-text chunk bytes.
type MemoryChunkStore struct {
	mu   sync.RWMutex
	data map[ChunkCoord][]byte
}

// NewMemoryChunkStore returns an empty MemoryChunkStore.
func NewMemoryChunkStore() *MemoryChunkStore {
	return &MemoryChunkStore{data: make(map[ChunkCoord][]byte)}
}

// LoadChunk returns a copy of the saved bytes for coord, or found=false if absent.
func (s *MemoryChunkStore) LoadChunk(coord ChunkCoord) ([]byte, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	s.mu.RLock()
	raw, ok := s.data[coord]
	s.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	return slices.Clone(raw), true, nil
}

// SaveChunk stores a copy of data under coord.
func (s *MemoryChunkStore) SaveChunk(coord ChunkCoord, data []byte) error {
	if s == nil {
		return fmt.Errorf("tilemap: memory chunk store: nil store")
	}
	s.mu.Lock()
	if s.data == nil {
		s.data = make(map[ChunkCoord][]byte)
	}
	s.data[coord] = slices.Clone(data)
	s.mu.Unlock()
	return nil
}

// Has reports whether coord has a saved entry in s.
func (s *MemoryChunkStore) Has(coord ChunkCoord) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	_, ok := s.data[coord]
	s.mu.RUnlock()
	return ok
}

// Bytes returns a copy of the raw saved bytes for coord, or nil if absent.
func (s *MemoryChunkStore) Bytes(coord ChunkCoord) []byte {
	raw, ok, _ := s.LoadChunk(coord)
	if !ok {
		return nil
	}
	return raw
}

// Count returns the number of saved chunks in s.
func (s *MemoryChunkStore) Count() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	n := len(s.data)
	s.mu.RUnlock()
	return n
}

// Delete removes any saved entry for coord.
func (s *MemoryChunkStore) Delete(coord ChunkCoord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.data, coord)
	s.mu.Unlock()
}

// DirChunkStore is a directory-backed ChunkStore that saves each chunk as a plain-text file
// named "chunk_<x>_<y>.yaml" (or ".json" when Format is ChunkFormatJSON).
type DirChunkStore struct {
	Dir    string
	Format ChunkFormat

	mu sync.RWMutex
}

// NewDirChunkStore constructs a DirChunkStore rooted at dir with the given plain-text format.
func NewDirChunkStore(dir string, format ChunkFormat) *DirChunkStore {
	return &DirChunkStore{Dir: dir, Format: format}
}

// Path returns the file path for coord inside s.Dir.
func (s *DirChunkStore) Path(coord ChunkCoord) string {
	ext := ".yaml"
	if s.Format == ChunkFormatJSON {
		ext = ".json"
	}
	return filepath.Join(s.Dir, fmt.Sprintf("chunk_%d_%d%s", coord.X, coord.Y, ext))
}

// LoadChunk reads coord's file from s.Dir, returning found=false if the file does not exist.
func (s *DirChunkStore) LoadChunk(coord ChunkCoord) ([]byte, bool, error) {
	if s == nil || s.Dir == "" {
		return nil, false, fmt.Errorf("tilemap: dir chunk store: empty directory")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.Path(coord))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return raw, true, nil
}

// SaveChunk atomically writes data to coord's file inside s.Dir, creating s.Dir if needed.
func (s *DirChunkStore) SaveChunk(coord ChunkCoord, data []byte) error {
	if s == nil || s.Dir == "" {
		return fmt.Errorf("tilemap: dir chunk store: empty directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	dst := s.Path(coord)
	tmp, err := os.CreateTemp(s.Dir, ".chunk-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// ChunkStreamConfig configures a synchronous ChunkStream for one chunked TileLayer.
// For multi-threaded background chunk loading on top of world3d.ChunkStreamer, see world3d.NewTilemapStreamer.
type ChunkStreamConfig struct {
	// Map is the Tilemap owning LayerIndex.
	Map *Tilemap
	// LayerIndex selects the chunked TileLayer inside Map.Layers.
	LayerIndex int
	// LoadRadius is the Chebyshev chunk radius around the center chunk to keep loaded.
	LoadRadius int
	// UnloadRadius is the Chebyshev chunk radius beyond which loaded chunks are unloaded.
	// Defaults to LoadRadius + 1 when smaller than LoadRadius.
	UnloadRadius int
	// MaxLoadsPerUpdate caps how many missing chunks are loaded or generated per Update call (<= 0 means unlimited).
	MaxLoadsPerUpdate int
	// Format selects ChunkFormatYAML (default) or ChunkFormatJSON when saving and restoring modified chunks.
	Format ChunkFormat
	// Store persists modified chunks on unload and restores them on reload before invoking Generator.
	Store ChunkStore
	// Generator populates a freshly allocated chunk when Store has no saved entry for coord.
	Generator func(coord ChunkCoord, chunk *Chunk) error
	// OnLoad is called after a chunk is loaded or generated and installed into the layer.
	OnLoad func(coord ChunkCoord, chunk *Chunk)
	// OnUnload is called after a chunk is saved (if modified) and removed from the layer.
	OnUnload func(coord ChunkCoord, chunk *Chunk)
	// OnError is called when loading, generating, or saving a chunk fails.
	OnError func(coord ChunkCoord, err error)
}

// ChunkStream streams sparse chunks around a moving 2D world position with hysteresis unloading,
// per-update load budgets, procedural generation, and deterministic YAML/JSON persistence of modified chunks.
type ChunkStream struct {
	cfg ChunkStreamConfig

	unloadBuf []ChunkCoord
	loadBuf   []ChunkCoord
}

// NewChunkStream constructs a ChunkStream from cfg.
func NewChunkStream(cfg ChunkStreamConfig) *ChunkStream {
	if cfg.LoadRadius < 0 {
		cfg.LoadRadius = 0
	}
	if cfg.UnloadRadius < cfg.LoadRadius {
		cfg.UnloadRadius = cfg.LoadRadius + 1
	}
	return &ChunkStream{cfg: cfg}
}

func (s *ChunkStream) layer() (*TileLayer, *chunkGrid, bool) {
	if s == nil || s.cfg.Map == nil || s.cfg.LayerIndex < 0 || s.cfg.LayerIndex >= len(s.cfg.Map.Layers) {
		return nil, nil, false
	}
	l := &s.cfg.Map.Layers[s.cfg.LayerIndex]
	if !l.IsChunked() {
		return nil, nil, false
	}
	return l, l.ensureChunks(), true
}

// CenterChunk returns the ChunkCoord containing worldPos in s's layer.
func (s *ChunkStream) CenterChunk(worldPos core.Point) ChunkCoord {
	_, g, ok := s.layer()
	if !ok {
		return ChunkCoord{}
	}
	m := s.cfg.Map
	if m.TileWidth <= 0 || m.TileHeight <= 0 {
		return ChunkCoord{}
	}
	chunkW := float64(g.size) * float64(m.TileWidth)
	chunkH := float64(g.size) * float64(m.TileHeight)
	return ChunkCoord{
		X: int(math.Floor(float64(worldPos.X) / chunkW)),
		Y: int(math.Floor(float64(worldPos.Y) / chunkH)),
	}
}

func chunkChebyshevDist(a, b ChunkCoord) int {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

func chunkDistSq(a, b ChunkCoord) int64 {
	dx := int64(a.X) - int64(b.X)
	dy := int64(a.Y) - int64(b.Y)
	return dx*dx + dy*dy
}

// Update advances streaming for worldPos:
//  1. Unloads loaded chunks beyond UnloadRadius (saving modified chunks to Store first).
//  2. Loads from Store or generates missing chunks within LoadRadius (closest first), up to MaxLoadsPerUpdate.
func (s *ChunkStream) Update(worldPos core.Point) {
	_, g, ok := s.layer()
	if !ok {
		return
	}
	center := s.CenterChunk(worldPos)

	// 1. Collect and unload chunks beyond UnloadRadius.
	s.unloadBuf = s.unloadBuf[:0]
	for coord := range g.chunks {
		if chunkChebyshevDist(coord, center) > s.cfg.UnloadRadius {
			s.unloadBuf = append(s.unloadBuf, coord)
		}
	}
	if len(s.unloadBuf) > 1 {
		slices.SortFunc(s.unloadBuf, compareChunkCoord)
	}
	for _, coord := range s.unloadBuf {
		ch := s.cfg.Map.RemoveChunk(s.cfg.LayerIndex, coord)
		if ch == nil {
			continue
		}
		if ch.Modified && s.cfg.Store != nil {
			if err := s.SaveChunk(ch); err != nil && s.cfg.OnError != nil {
				s.cfg.OnError(coord, err)
			}
		}
		if s.cfg.OnUnload != nil {
			s.cfg.OnUnload(coord, ch)
		}
	}

	// 2. Collect missing chunks within LoadRadius.
	s.loadBuf = s.loadBuf[:0]
	r := s.cfg.LoadRadius
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			coord := ChunkCoord{X: center.X + dx, Y: center.Y + dy}
			if _, loaded := g.chunks[coord]; !loaded {
				s.loadBuf = append(s.loadBuf, coord)
			}
		}
	}
	if len(s.loadBuf) == 0 {
		return
	}
	if len(s.loadBuf) > 1 {
		slices.SortFunc(s.loadBuf, func(a, b ChunkCoord) int {
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
	}
	limit := len(s.loadBuf)
	if s.cfg.MaxLoadsPerUpdate > 0 && limit > s.cfg.MaxLoadsPerUpdate {
		limit = s.cfg.MaxLoadsPerUpdate
	}
	for i := range limit {
		coord := s.loadBuf[i]
		ch, err := s.LoadOrGenerateChunk(coord)
		if err != nil {
			if s.cfg.OnError != nil {
				s.cfg.OnError(coord, err)
			}
			continue
		}
		if err := s.cfg.Map.PutChunk(s.cfg.LayerIndex, ch); err != nil {
			if s.cfg.OnError != nil {
				s.cfg.OnError(coord, err)
			}
			continue
		}
		if s.cfg.OnLoad != nil {
			s.cfg.OnLoad(coord, ch)
		}
	}
}

// LoadOrGenerateChunk restores coord from Store if present, or otherwise allocates a new Chunk and runs Generator.
// The returned Chunk always has Dirty=true and Modified=false.
func (s *ChunkStream) LoadOrGenerateChunk(coord ChunkCoord) (*Chunk, error) {
	_, g, ok := s.layer()
	if !ok {
		return nil, fmt.Errorf("tilemap: chunk stream: invalid map or non-chunked layer %d", s.cfg.LayerIndex)
	}
	if s.cfg.Store != nil {
		raw, found, err := s.cfg.Store.LoadChunk(coord)
		if err != nil {
			return nil, err
		}
		if found {
			ch, err := DecodeChunk(raw, s.cfg.Format)
			if err != nil {
				return nil, err
			}
			if ch.Coord != coord {
				return nil, fmt.Errorf("tilemap: chunk stream: stored chunk coord %+v does not match requested %+v", ch.Coord, coord)
			}
			if ch.Size != g.size {
				return nil, fmt.Errorf("tilemap: chunk stream: stored chunk size %d does not match layer size %d", ch.Size, g.size)
			}
			ch.Modified = false
			ch.Dirty = true
			return ch, nil
		}
	}
	ch := NewChunk(coord, g.size)
	if s.cfg.Generator != nil {
		if err := s.cfg.Generator(coord, ch); err != nil {
			return nil, err
		}
	}
	ch.recountNonEmpty()
	ch.Modified = false
	ch.Dirty = true
	return ch, nil
}

// SaveChunk encodes ch in s's configured format and writes it to Store, clearing ch.Modified on success.
func (s *ChunkStream) SaveChunk(ch *Chunk) error {
	if s == nil || s.cfg.Store == nil || ch == nil {
		return nil
	}
	raw, err := EncodeChunk(ch, s.cfg.Format)
	if err != nil {
		return err
	}
	if err := s.cfg.Store.SaveChunk(ch.Coord, raw); err != nil {
		return err
	}
	ch.Modified = false
	return nil
}

// SaveModified persists every currently loaded modified chunk in deterministic (Y, X) order.
func (s *ChunkStream) SaveModified() error {
	l, _, ok := s.layer()
	if !ok || s.cfg.Store == nil {
		return nil
	}
	for _, coord := range l.ModifiedChunks() {
		ch := l.Chunk(coord)
		if ch == nil || !ch.Modified {
			continue
		}
		if err := s.SaveChunk(ch); err != nil {
			return err
		}
	}
	return nil
}

// Close saves all loaded modified chunks and unloads every chunk from the layer.
func (s *ChunkStream) Close() error {
	l, _, ok := s.layer()
	if !ok {
		return nil
	}
	var firstErr error
	for _, coord := range l.ChunkCoords() {
		ch := s.cfg.Map.RemoveChunk(s.cfg.LayerIndex, coord)
		if ch == nil {
			continue
		}
		if ch.Modified && s.cfg.Store != nil {
			if err := s.SaveChunk(ch); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if s.cfg.OnUnload != nil {
			s.cfg.OnUnload(coord, ch)
		}
	}
	return firstErr
}

// LoadedCount returns the number of currently loaded chunks in the streamed layer.
func (s *ChunkStream) LoadedCount() int {
	l, _, ok := s.layer()
	if !ok {
		return 0
	}
	return l.ChunkCount()
}
