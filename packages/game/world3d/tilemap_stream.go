package world3d

import (
	"context"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/tilemap"
)

// TilemapStreamerConfig configures a TilemapStreamer that drives a sparse chunked tilemap.TileLayer
// with a world3d.ChunkStreamer[*tilemap.Chunk].
type TilemapStreamerConfig struct {
	// Map is the Tilemap owning LayerIndex.
	Map *tilemap.Tilemap
	// LayerIndex selects the chunked TileLayer inside Map.Layers.
	LayerIndex int
	// LoadRadius is the Chebyshev chunk radius around the player chunk to load.
	LoadRadius int
	// UnloadRadius is the Chebyshev chunk radius beyond which loaded and in-flight chunks unload/cancel.
	// Defaults to LoadRadius + 1 when smaller than LoadRadius.
	UnloadRadius int
	// MaxIntegrationsPerFrame caps how many completed chunks integrate into Map per Update call (<= 0 means unlimited).
	MaxIntegrationsPerFrame int
	// Workers sets the number of background ChunkStreamer loader goroutines (0 runs synchronously inside Update).
	Workers int
	// Format selects tilemap.ChunkFormatYAML (default) or tilemap.ChunkFormatJSON for Store persistence.
	Format tilemap.ChunkFormat
	// Store persists modified chunks on unload and restores them on reload before invoking Generator.
	// When Workers > 0, Store must be safe for concurrent use (such as tilemap.MemoryChunkStore or tilemap.DirChunkStore).
	Store tilemap.ChunkStore
	// Generator populates a freshly allocated chunk when Store has no saved entry for coord.
	// It must respect ctx cancellation when Workers > 0.
	Generator func(ctx context.Context, coord tilemap.ChunkCoord, chunk *tilemap.Chunk) error
	// OnLoad runs on the Update goroutine after a chunk is installed into Map.Layers[LayerIndex].
	OnLoad func(coord tilemap.ChunkCoord, chunk *tilemap.Chunk)
	// OnUnload runs on the Update/Close goroutine after a chunk is saved (if modified) and removed from Map.Layers[LayerIndex].
	OnUnload func(coord tilemap.ChunkCoord, chunk *tilemap.Chunk)
	// OnError runs on the Update goroutine when loading, generating, or saving a chunk fails.
	OnError func(coord tilemap.ChunkCoord, err error)
}

// TilemapStreamer bridges world3d.ChunkStreamer[*tilemap.Chunk] onto a sparse chunked tilemap.TileLayer,
// providing bounded streaming, background generation, hysteresis unloading, and deterministic YAML/JSON persistence
// of modified chunks without breaking game/tilemap's stability boundary.
type TilemapStreamer struct {
	cfg       TilemapStreamerConfig
	chunkSize int
	chunkW    float64
	chunkH    float64
	streamer  *ChunkStreamer[*tilemap.Chunk]
}

// NewTilemapStreamer constructs a TilemapStreamer backed by a world3d.ChunkStreamer[*tilemap.Chunk].
// Call Close when finished to save any loaded modified chunks and stop worker goroutines.
func NewTilemapStreamer(cfg TilemapStreamerConfig) (*TilemapStreamer, error) {
	if cfg.Map == nil {
		return nil, fmt.Errorf("world3d: tilemap streamer: nil tilemap")
	}
	if cfg.LayerIndex < 0 || cfg.LayerIndex >= len(cfg.Map.Layers) {
		return nil, fmt.Errorf("world3d: tilemap streamer: layer index %d out of range [0, %d)", cfg.LayerIndex, len(cfg.Map.Layers))
	}
	layer := &cfg.Map.Layers[cfg.LayerIndex]
	if !layer.IsChunked() {
		return nil, fmt.Errorf("world3d: tilemap streamer: layer %d (%q) is not chunked", cfg.LayerIndex, layer.Name)
	}
	if cfg.Map.TileWidth <= 0 || cfg.Map.TileHeight <= 0 {
		return nil, fmt.Errorf("world3d: tilemap streamer: non-positive tile size %gx%g", cfg.Map.TileWidth, cfg.Map.TileHeight)
	}
	cs := layer.EffectiveChunkSize()
	chunkW := float64(cs) * float64(cfg.Map.TileWidth)
	chunkH := float64(cs) * float64(cfg.Map.TileHeight)

	ts := &TilemapStreamer{
		cfg:       cfg,
		chunkSize: cs,
		chunkW:    chunkW,
		chunkH:    chunkH,
	}

	ts.streamer = NewChunkStreamer(ChunkStreamerConfig[*tilemap.Chunk]{
		ChunkSize:               chunkW,
		LoadRadius:              cfg.LoadRadius,
		UnloadRadius:            cfg.UnloadRadius,
		MaxIntegrationsPerFrame: cfg.MaxIntegrationsPerFrame,
		Workers:                 cfg.Workers,
		Loader:                  ts.loadChunk,
		OnLoad:                  ts.onLoad,
		OnUnload:                ts.onUnload,
		OnError:                 ts.onError,
	})
	return ts, nil
}

func (s *TilemapStreamer) loadChunk(ctx context.Context, coord ChunkCoord) (*tilemap.Chunk, error) {
	tc := tilemap.ChunkCoord{X: int(coord.X), Y: int(coord.Z)}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.cfg.Store != nil {
		raw, found, err := s.cfg.Store.LoadChunk(tc)
		if err != nil {
			return nil, err
		}
		if found {
			ch, err := tilemap.DecodeChunk(raw, s.cfg.Format)
			if err != nil {
				return nil, err
			}
			if ch.Coord != tc {
				return nil, fmt.Errorf("world3d: tilemap streamer: stored chunk coord %+v does not match requested %+v", ch.Coord, tc)
			}
			if ch.Size != s.chunkSize {
				return nil, fmt.Errorf("world3d: tilemap streamer: stored chunk size %d does not match layer size %d", ch.Size, s.chunkSize)
			}
			ch.Modified = false
			ch.Dirty = true
			return ch, nil
		}
	}
	ch := tilemap.NewChunk(tc, s.chunkSize)
	if s.cfg.Generator != nil {
		if err := s.cfg.Generator(ctx, tc, ch); err != nil {
			return nil, err
		}
	}
	ch.Modified = false
	ch.Dirty = true
	return ch, nil
}

func (s *TilemapStreamer) onLoad(coord ChunkCoord, ch *tilemap.Chunk) {
	tc := tilemap.ChunkCoord{X: int(coord.X), Y: int(coord.Z)}
	if err := s.cfg.Map.PutChunk(s.cfg.LayerIndex, ch); err != nil {
		if s.cfg.OnError != nil {
			s.cfg.OnError(tc, err)
		}
		return
	}
	if s.cfg.OnLoad != nil {
		s.cfg.OnLoad(tc, ch)
	}
}

func (s *TilemapStreamer) onUnload(coord ChunkCoord, ch *tilemap.Chunk) {
	tc := tilemap.ChunkCoord{X: int(coord.X), Y: int(coord.Z)}
	removed := s.cfg.Map.RemoveChunk(s.cfg.LayerIndex, tc)
	if removed != nil {
		ch = removed
	}
	if ch != nil && ch.Modified && s.cfg.Store != nil {
		if err := s.saveChunk(ch); err != nil && s.cfg.OnError != nil {
			s.cfg.OnError(tc, err)
		}
	}
	if s.cfg.OnUnload != nil {
		s.cfg.OnUnload(tc, ch)
	}
}

func (s *TilemapStreamer) onError(coord ChunkCoord, err error) {
	if s.cfg.OnError != nil {
		s.cfg.OnError(tilemap.ChunkCoord{X: int(coord.X), Y: int(coord.Z)}, err)
	}
}

func (s *TilemapStreamer) saveChunk(ch *tilemap.Chunk) error {
	raw, err := tilemap.EncodeChunk(ch, s.cfg.Format)
	if err != nil {
		return err
	}
	if err := s.cfg.Store.SaveChunk(ch.Coord, raw); err != nil {
		return err
	}
	ch.Modified = false
	return nil
}

// Update advances chunk streaming for a 2D world position (worldX, worldY) in tilemap world points.
func (s *TilemapStreamer) Update(worldX, worldY float64) {
	if s == nil || s.streamer == nil {
		return
	}
	// Scale Y into ChunkStreamer's uniform square-chunk Z axis so non-square tiles (TileWidth != TileHeight) map accurately.
	scaledY := worldY
	if s.chunkH > 0 && s.chunkH != s.chunkW {
		scaledY = worldY * (s.chunkW / s.chunkH)
	}
	s.streamer.Update(WorldPos64{X: worldX, Z: scaledY})
}

// Streamer returns the underlying world3d.ChunkStreamer[*tilemap.Chunk].
func (s *TilemapStreamer) Streamer() *ChunkStreamer[*tilemap.Chunk] {
	if s == nil {
		return nil
	}
	return s.streamer
}

// SaveModified persists every currently loaded modified chunk in deterministic order to Store.
func (s *TilemapStreamer) SaveModified() error {
	if s == nil || s.cfg.Map == nil || s.cfg.Store == nil {
		return nil
	}
	if s.cfg.LayerIndex < 0 || s.cfg.LayerIndex >= len(s.cfg.Map.Layers) {
		return nil
	}
	layer := &s.cfg.Map.Layers[s.cfg.LayerIndex]
	for _, coord := range layer.ModifiedChunks() {
		ch := layer.Chunk(coord)
		if ch == nil || !ch.Modified {
			continue
		}
		if err := s.saveChunk(ch); err != nil {
			return err
		}
	}
	return nil
}

// Close stops background loader workers and unloads all remaining chunks, saving any modified chunks to Store.
func (s *TilemapStreamer) Close() {
	if s == nil || s.streamer == nil {
		return
	}
	s.streamer.Close()
}
