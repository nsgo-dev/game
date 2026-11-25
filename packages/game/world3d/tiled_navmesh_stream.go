package world3d

import (
	"context"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// NavTileStreamerConfig configures a NavTileStreamer that drives a 3D TiledNavMesh
// from a world3d.ChunkStreamer[*NavTile], loading and unloading navmesh tiles in lockstep
// with streamed world chunks.
type NavTileStreamerConfig struct {
	// Mesh is the target TiledNavMesh whose tiles are added on chunk load and removed on chunk unload.
	Mesh *TiledNavMesh
	// LoadRadius is the Chebyshev chunk radius around the anchor position to load.
	LoadRadius int
	// UnloadRadius is the Chebyshev chunk radius beyond which loaded and in-flight tiles unload/cancel.
	// Defaults to LoadRadius + 1 when smaller than LoadRadius.
	UnloadRadius int
	// MaxIntegrationsPerFrame caps how many completed tiles are installed into Mesh per Update call
	// (<= 0 allows unlimited integrations per frame).
	MaxIntegrationsPerFrame int
	// Workers sets the number of background ChunkStreamer loader goroutines.
	// When 0, Bake runs synchronously inside Update (useful for deterministic tests).
	Workers int
	// Bake builds the standalone *NavTile for chunk coord and world-space XZ bounds.
	// When Workers > 0, Bake runs on worker goroutines and must respect ctx cancellation.
	// Return (nil, nil) for a chunk with no walkable surface.
	Bake func(ctx context.Context, coord ChunkCoord, bounds math3d.AABB) (*NavTile, error)
	// OnLoad runs on the Update goroutine after the tile is installed into Mesh.
	OnLoad func(coord ChunkCoord, tile *NavTile)
	// OnUnload runs on the Update/Close goroutine after the tile is removed from Mesh.
	OnUnload func(coord ChunkCoord)
	// OnError runs on the Update goroutine when Bake returns a non-nil error.
	OnError func(coord ChunkCoord, err error)
}

// NavTileStreamer bridges world3d.ChunkStreamer[*NavTile] onto a 3D TiledNavMesh:
// chunks entering LoadRadius are baked (optionally on background workers) and installed into Mesh
// with active dynamic obstacles applied and cross-border portals linked; chunks leaving UnloadRadius
// are removed from Mesh, unlinking neighbor portals and invalidating paths that crossed them.
type NavTileStreamer struct {
	cfg      NavTileStreamerConfig
	streamer *ChunkStreamer[*NavTile]
}

// NewNavTileStreamer constructs a NavTileStreamer backed by a ChunkStreamer[*NavTile]
// whose ChunkSize matches cfg.Mesh.TileSize(). Call Close when finished to stop worker
// goroutines and unload remaining tiles.
func NewNavTileStreamer(cfg NavTileStreamerConfig) (*NavTileStreamer, error) {
	if cfg.Mesh == nil {
		return nil, fmt.Errorf("world3d: nav tile streamer: nil tiled navmesh")
	}
	if cfg.Mesh.TileSize() <= 0 {
		return nil, fmt.Errorf("world3d: nav tile streamer: non-positive tile size %g", cfg.Mesh.TileSize())
	}
	ns := &NavTileStreamer{cfg: cfg}
	ns.streamer = NewChunkStreamer(ChunkStreamerConfig[*NavTile]{
		ChunkSize:               float64(cfg.Mesh.TileSize()),
		LoadRadius:              cfg.LoadRadius,
		UnloadRadius:            cfg.UnloadRadius,
		MaxIntegrationsPerFrame: cfg.MaxIntegrationsPerFrame,
		Workers:                 cfg.Workers,
		Loader:                  ns.loadTile,
		OnLoad:                  ns.onLoad,
		OnUnload:                ns.onUnload,
		OnError:                 cfg.OnError,
	})
	return ns, nil
}

func (s *NavTileStreamer) loadTile(ctx context.Context, coord ChunkCoord) (*NavTile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.cfg.Bake == nil {
		return nil, nil
	}
	minX, minZ, maxX, maxZ := s.cfg.Mesh.tileBounds64(coord)
	bounds := math3d.AABB{
		Min: math3d.Vec3{X: float32(minX), Y: 0, Z: float32(minZ)},
		Max: math3d.Vec3{X: float32(maxX), Y: 0, Z: float32(maxZ)},
	}
	return s.cfg.Bake(ctx, coord, bounds)
}

func (s *NavTileStreamer) onLoad(coord ChunkCoord, tile *NavTile) {
	if tile != nil {
		s.cfg.Mesh.SetTile(coord, tile)
	}
	if s.cfg.OnLoad != nil {
		s.cfg.OnLoad(coord, tile)
	}
}

func (s *NavTileStreamer) onUnload(coord ChunkCoord, _ *NavTile) {
	s.cfg.Mesh.RemoveTile(coord)
	if s.cfg.OnUnload != nil {
		s.cfg.OnUnload(coord)
	}
}

// Update advances chunk streaming around worldPos (offset by Mesh.Origin() when non-zero),
// installing completed tiles up to MaxIntegrationsPerFrame and unloading distant tiles.
func (s *NavTileStreamer) Update(worldPos WorldPos64) {
	if s == nil || s.streamer == nil {
		return
	}
	shifted := WorldPos64{
		X: worldPos.X - s.cfg.Mesh.originX,
		Y: worldPos.Y,
		Z: worldPos.Z - s.cfg.Mesh.originZ,
	}
	s.streamer.Update(shifted)
}

// Streamer returns the underlying ChunkStreamer[*NavTile].
func (s *NavTileStreamer) Streamer() *ChunkStreamer[*NavTile] {
	if s == nil {
		return nil
	}
	return s.streamer
}

// LoadedTiles returns a sorted slice of chunk coordinates currently tracked as loaded.
func (s *NavTileStreamer) LoadedTiles() []ChunkCoord {
	if s == nil || s.streamer == nil {
		return nil
	}
	return s.streamer.LoadedChunks()
}

// Close cancels in-flight bakes, waits for worker goroutines to exit, and unloads all remaining tiles.
func (s *NavTileStreamer) Close() {
	if s == nil || s.streamer == nil {
		return
	}
	s.streamer.Close()
}
