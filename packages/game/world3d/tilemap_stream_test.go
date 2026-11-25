package world3d_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/core"
)

func TestTilemapStreamerSyncPersistenceAndBoundedWalker(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format tilemap.ChunkFormat
	}{
		{"yaml", tilemap.ChunkFormatYAML},
		{"json", tilemap.ChunkFormatJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:tiles.png"}, 16, 16, 4, 16)
			m := tilemap.NewInfinite(16, 16, ts)
			m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("terrain", 16)}
			m.SetFlags(1, tilemap.TileSolid)

			store := tilemap.NewMemoryChunkStore()
			const loadR, unloadR = 2, 3
			maxAllowed := (2*unloadR + 1) * (2*unloadR + 1)

			genCount := 0
			streamer, err := world3d.NewTilemapStreamer(world3d.TilemapStreamerConfig{
				Map:          m,
				LayerIndex:   0,
				LoadRadius:   loadR,
				UnloadRadius: unloadR,
				Workers:      0, // deterministic synchronous streaming
				Format:       tc.format,
				Store:        store,
				Generator: func(_ context.Context, coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
					genCount++
					for ly := range ch.Size {
						for lx := range ch.Size {
							if (coord.X+coord.Y+lx+ly)&3 == 0 {
								ch.Tiles[ly*ch.Size+lx] = 1
							}
						}
					}
					return nil
				},
			})
			if err != nil {
				t.Fatalf("NewTilemapStreamer: %v", err)
			}
			defer streamer.Close()

			// Load around negative chunk (-4, 5).
			wx, wy := float64(-4*16*16+16), float64(5*16*16+16)
			streamer.Update(wx, wy)
			target := tilemap.ChunkCoord{X: -4, Y: 5}
			ch := m.ChunkAt(0, target)
			if ch == nil {
				t.Fatalf("chunk %+v not loaded", target)
			}

			// Modify tiles inside target chunk (including flipped GID).
			baseCol, baseRow := target.X*16, target.Y*16
			m.SetTile(0, baseCol+1, baseRow+2, tilemap.FlipHorizontal|3)
			m.SetTile(0, baseCol+10, baseRow+11, 2)
			beforeBytes, err := tilemap.EncodeChunk(ch, tc.format)
			if err != nil {
				t.Fatalf("EncodeChunk: %v", err)
			}

			// Walk 10,000 tiles in +X, -X, +Y, -Y and verify loaded chunk count stays strictly bounded.
			cam := &tilemap.Camera{Zoom: 1, Width: 320, Height: 180}
			d := core.Drawing{Ops: make([]core.DrawOp, 0, 512)}
			for _, dir := range [][2]int{{4, 0}, {-4, 0}, {0, 4}, {0, -4}} {
				for s := range 2501 {
					px := float64(dir[0] * s * 16)
					py := float64(dir[1] * s * 16)
					streamer.Update(px, py)
					cam.X, cam.Y = float32(px), float32(py)
					d.Ops = d.Ops[:0]
					m.Draw(&d, cam)
					if len(d.Ops) == 0 {
						t.Fatalf("expected draw ops at (%g,%g)", px, py)
					}
					if n := m.Layers[0].ChunkCount(); n > maxAllowed {
						t.Fatalf("loaded chunks %d exceeded bound %d", n, maxAllowed)
					}
				}
			}

			// Verify target chunk was unloaded and persisted to Store.
			if m.ChunkAt(0, target) != nil {
				t.Fatalf("expected %+v to be unloaded far away", target)
			}
			if !store.Has(target) {
				t.Fatalf("expected %+v to be saved in Store", target)
			}
			if store.Count() != 1 {
				t.Fatalf("Store.Count = %d, want 1 (only modified chunk saved)", store.Count())
			}

			// Return to (-4, 5) and verify byte-for-byte restoration.
			streamer.Update(wx, wy)
			reloaded := m.ChunkAt(0, target)
			if reloaded == nil {
				t.Fatalf("chunk %+v did not reload", target)
			}
			afterBytes, err := tilemap.EncodeChunk(reloaded, tc.format)
			if err != nil {
				t.Fatalf("EncodeChunk reloaded: %v", err)
			}
			if !bytes.Equal(beforeBytes, afterBytes) {
				t.Fatalf("reloaded chunk bytes differ:\nbefore:\n%s\nafter:\n%s", beforeBytes, afterBytes)
			}
			if got := m.TileAt(0, baseCol+1, baseRow+2); got != tilemap.FlipHorizontal|3 {
				t.Fatalf("restored TileAt = %#x, want %#x", got, tilemap.FlipHorizontal|3)
			}
		})
	}
}

func TestTilemapStreamerAsyncWorkersAndNonSquareTiles(t *testing.T) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:iso.png"}, 32, 16, 4, 16)
	m := tilemap.NewInfinite(32, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", 8)}
	store := tilemap.NewMemoryChunkStore()

	streamer, err := world3d.NewTilemapStreamer(world3d.TilemapStreamerConfig{
		Map:                     m,
		LayerIndex:              0,
		LoadRadius:              1,
		UnloadRadius:            2,
		MaxIntegrationsPerFrame: 4,
		Workers:                 2,
		Format:                  tilemap.ChunkFormatYAML,
		Store:                   store,
		Generator: func(ctx context.Context, coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			ch.Tiles[0] = uint32((coord.X & 3) + 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewTilemapStreamer async: %v", err)
	}

	// Non-square tiles (32x16), chunkSize 8 -> chunkW=256, chunkH=128.
	// World pos (300, -200) -> chunkX=floor(300/256)=1, chunkY=floor(-200/128)=-2.
	deadline := time.Now().Add(2 * time.Second)
	for m.Layers[0].ChunkCount() < 9 && time.Now().Before(deadline) {
		streamer.Update(300, -200)
		time.Sleep(2 * time.Millisecond)
	}
	if got := m.Layers[0].ChunkCount(); got != 9 {
		t.Fatalf("async loaded chunks = %d, want 9", got)
	}
	center := tilemap.ChunkCoord{X: 1, Y: -2}
	if m.ChunkAt(0, center) == nil {
		t.Fatalf("expected center chunk %+v for non-square tiles", center)
	}

	// Edit a tile and verify Close saves modified chunks automatically.
	m.SetTile(0, center.X*8+3, center.Y*8+4, 9)
	streamer.Close()
	if !store.Has(center) {
		t.Fatalf("expected Close to save modified chunk %+v", center)
	}
	if m.Layers[0].ChunkCount() != 0 {
		t.Fatalf("expected all chunks unloaded on Close, got %d", m.Layers[0].ChunkCount())
	}
}

func BenchmarkWorld3DTilemapStreamerWalker10000(b *testing.B) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "asset:tiles.png"}, 16, 16, 4, 16)
	m := tilemap.NewInfinite(16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", 16)}

	const loadR, unloadR = 2, 3
	maxAllowed := (2*unloadR + 1) * (2*unloadR + 1)
	streamer, err := world3d.NewTilemapStreamer(world3d.TilemapStreamerConfig{
		Map:          m,
		LayerIndex:   0,
		LoadRadius:   loadR,
		UnloadRadius: unloadR,
		Workers:      0,
		Generator: func(_ context.Context, coord tilemap.ChunkCoord, ch *tilemap.Chunk) error {
			base := coord.X*31 + coord.Y*17
			for ly := range ch.Size {
				rowOff := ly * ch.Size
				for lx := range ch.Size {
					ch.Tiles[rowOff+lx] = uint32(((base + lx + ly) & 3) + 1)
				}
			}
			return nil
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	defer streamer.Close()

	cam := &tilemap.Camera{Zoom: 1, Width: 320, Height: 180}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 512)}

	const stepsPerLeg = 10000
	const totalSteps = stepsPerLeg * 4
	posAt := func(step int) (float64, float64) {
		idx := step % totalSteps
		leg := idx / stepsPerLeg
		off := float64((idx % stepsPerLeg) * 16)
		switch leg {
		case 0:
			return off, 0
		case 1:
			return -off, 0
		case 2:
			return 0, off
		default:
			return 0, -off
		}
	}

	streamer.Update(0, 0)
	m.Draw(&d, cam)

	maxChunks := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		wx, wy := posAt(i)
		streamer.Update(wx, wy)
		cam.X, cam.Y = float32(wx), float32(wy)
		d.Ops = d.Ops[:0]
		m.Draw(&d, cam)
		if n := m.Layers[0].ChunkCount(); n > maxChunks {
			maxChunks = n
		}
	}
	b.StopTimer()
	if maxChunks > maxAllowed {
		b.Fatalf("loaded chunks %d exceeded bound %d", maxChunks, maxAllowed)
	}
	b.ReportMetric(float64(maxChunks), "max_chunks")
}
