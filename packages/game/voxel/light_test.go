package voxel_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/voxel"
)

func lightingTestRegistry(t *testing.T) (reg *voxel.Registry, stone, glass, water, leaves, tinted, torch, candle, glowstone voxel.BlockID) {
	t.Helper()
	reg = voxel.NewRegistry()
	stone = reg.MustRegister(voxel.BlockDef{Name: "stone", MaterialID: 0})
	glass = reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true, MaterialID: 1})
	water = reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, LightOpacity: 2, MaterialID: 2})
	leaves = reg.MustRegister(voxel.BlockDef{Name: "leaves", Transparent: true, LightOpacity: 1, MaterialID: 3})
	tinted = reg.MustRegister(voxel.BlockDef{Name: "tinted_glass", Transparent: true, LightOpacity: 15, MaterialID: 4})
	torch = reg.MustRegister(voxel.BlockDef{Name: "torch", Transparent: true, NonSolid: true, Emissive: 14, MaterialID: 5})
	candle = reg.MustRegister(voxel.BlockDef{Name: "candle", Transparent: true, NonSolid: true, Emissive: 8, MaterialID: 6})
	glowstone = reg.MustRegister(voxel.BlockDef{Name: "glowstone", Emissive: 15, MaterialID: 7})
	return
}

// cloneAndRecompute creates a fresh world with identical blocks and runs a full
// RecomputeLight() from scratch as ground truth for incremental light checks.
func cloneAndRecompute(t *testing.T, src *voxel.World) *voxel.World {
	t.Helper()
	doc := src.ToDoc("ref")
	ref, err := voxel.FromDoc(doc)
	if err != nil {
		t.Fatalf("FromDoc reference clone: %v", err)
	}
	ref.RecomputeLight()
	return ref
}

func assertLightsEqualBox(t *testing.T, context string, got, want *voxel.World, minP, maxP voxel.BlockCoord) {
	t.Helper()
	for y := minP.Y; y <= maxP.Y; y++ {
		for z := minP.Z; z <= maxP.Z; z++ {
			for x := minP.X; x <= maxP.X; x++ {
				p := voxel.BlockCoord{X: x, Y: y, Z: z}
				gs, gb := got.Light(p)
				ws, wb := want.Light(p)
				if gs != ws || gb != wb {
					t.Fatalf("%s: light mismatch at %+v: got (sky=%d, blk=%d), want (sky=%d, blk=%d)",
						context, p, gs, gb, ws, wb)
				}
			}
		}
	}
}

func TestLightBlockDefValidationAndYAML(t *testing.T) {
	reg := voxel.NewRegistry()
	if _, err := reg.Register(voxel.BlockDef{Name: "too_bright", Emissive: 16}); !errors.Is(err, voxel.ErrInvalidBlock) {
		t.Fatalf("emissive=16 err = %v, want ErrInvalidBlock", err)
	}
	if _, err := reg.Register(voxel.BlockDef{Name: "too_opaque", Transparent: true, LightOpacity: 16}); !errors.Is(err, voxel.ErrInvalidBlock) {
		t.Fatalf("lightOpacity=16 err = %v, want ErrInvalidBlock", err)
	}

	stone := reg.MustRegister(voxel.BlockDef{Name: "stone"})
	glass := reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true})
	water := reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, LightOpacity: 2})
	torch := reg.MustRegister(voxel.BlockDef{Name: "torch", Transparent: true, NonSolid: true, Emissive: 14})

	if reg.Emissive(voxel.Air) != 0 || reg.LightOpacity(voxel.Air) != 0 {
		t.Fatal("Air light defaults mismatch")
	}
	if reg.Emissive(stone) != 0 || reg.LightOpacity(stone) != voxel.MaxLightLevel {
		t.Fatal("opaque stone light properties mismatch")
	}
	if reg.Emissive(glass) != 0 || reg.LightOpacity(glass) != 0 {
		t.Fatal("transparent glass light properties mismatch")
	}
	if reg.Emissive(water) != 0 || reg.LightOpacity(water) != 2 {
		t.Fatal("water light properties mismatch")
	}
	if reg.Emissive(torch) != 14 || reg.LightOpacity(torch) != 0 {
		t.Fatal("torch light properties mismatch")
	}
	if reg.Emissive(999) != 0 || reg.LightOpacity(999) != voxel.MaxLightLevel {
		t.Fatal("unregistered block light fallback mismatch")
	}

	// YAML round-trip preserves Emissive and LightOpacity and recomputes light on load.
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 7, Y: 0, Z: 7}, stone)
	w.SetBlock(voxel.BlockCoord{X: 3, Y: 1, Z: 3}, torch)
	w.SetBlock(voxel.BlockCoord{X: 4, Y: 1, Z: 3}, water)

	data, err := voxel.SaveYAML(w, "LitRoom")
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	loaded, _, err := voxel.LoadYAML(data)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	data2, err := voxel.SaveYAML(loaded, "LitRoom")
	if err != nil {
		t.Fatalf("SaveYAML after LoadYAML: %v", err)
	}
	if !bytes.Equal(data, data2) {
		t.Fatalf("YAML round-trip with emissive/lightOpacity changed bytes:\n--- before ---\n%s\n--- after ---\n%s", data, data2)
	}
	assertLightsEqualBox(t, "loaded YAML light", loaded, w,
		voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 7, Y: 7, Z: 7})

	// Validation rejects emissive > 15 and lightOpacity > 15 with field paths.
	for _, tc := range []struct {
		name      string
		yaml      string
		wantField string
	}{
		{
			name:      "emissive over 15",
			yaml:      "version: 1\nchunkSize: 4\npalette:\n  - id: 1\n    name: sun\n    emissive: 16\n",
			wantField: "palette[0].emissive",
		},
		{
			name:      "lightOpacity over 15",
			yaml:      "version: 1\nchunkSize: 4\npalette:\n  - id: 1\n    name: murky\n    transparent: true\n    lightOpacity: 20\n",
			wantField: "palette[0].lightOpacity",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := voxel.ParseWith([]byte(tc.yaml), "bad.voxel.yaml")
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			le, ok := loaderr.As(err)
			if !ok || le.Field != tc.wantField {
				t.Fatalf("err = %v, want *loaderr.Error with Field=%q", err, tc.wantField)
			}
		})
	}
}

func TestTorchPlaceRemoveMatchesFullRecompute(t *testing.T) {
	reg, stone, _, _, _, _, torch, candle, glowstone := lightingTestRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 16, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Build an enclosed dark stone room from (0,0,0) to (25,10,25) so sky light is 0 inside.
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 25, Y: 10, Z: 25}, stone)
	w.FillBox(voxel.BlockCoord{X: 1, Y: 1, Z: 1}, voxel.BlockCoord{X: 24, Y: 9, Z: 24}, voxel.Air)

	minCheck := voxel.BlockCoord{X: 0, Y: 0, Z: 0}
	maxCheck := voxel.BlockCoord{X: 25, Y: 10, Z: 25}

	// Confirm the sealed room is pitch dark (sky=0, block=0).
	if s, b := w.Light(voxel.BlockCoord{X: 12, Y: 5, Z: 12}); s != 0 || b != 0 {
		t.Fatalf("sealed cave center light = (%d, %d), want (0, 0)", s, b)
	}
	assertLightsEqualBox(t, "empty sealed cave", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// 1. Place a torch at (12, 5, 12) (emissive 14): verify exact Manhattan falloff.
	torchA := voxel.BlockCoord{X: 12, Y: 5, Z: 12}
	w.SetBlock(torchA, torch)
	for dx := -15; dx <= 15; dx++ {
		x := torchA.X + dx
		if x < 1 || x > 24 {
			continue
		}
		dist := dx
		if dist < 0 {
			dist = -dist
		}
		want := uint8(0)
		if 14-dist > 0 {
			want = uint8(14 - dist)
		}
		if got := w.BlockLight(voxel.BlockCoord{X: x, Y: 5, Z: 12}); got != want {
			t.Fatalf("torch falloff at dx=%d: got %d, want %d", dx, got, want)
		}
	}
	assertLightsEqualBox(t, "after placing torchA", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// 2. Place a second overlapping torchB at (18, 5, 12) and a weaker candle at (14, 5, 12) inside torchA's radius.
	torchB := voxel.BlockCoord{X: 18, Y: 5, Z: 12}
	candleC := voxel.BlockCoord{X: 14, Y: 5, Z: 12}
	w.SetBlock(torchB, torch)
	w.SetBlock(candleC, candle)
	assertLightsEqualBox(t, "overlapping torchA+torchB+candleC", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// 3. Remove torchA: torchB and candleC must seamlessly maintain exact light!
	w.SetBlock(torchA, voxel.Air)
	if got := w.BlockLight(candleC); got != 10 { // torchB (14) at dist 4 => 10 > candle(8)
		t.Fatalf("candleC light after removing torchA = %d, want 10", got)
	}
	assertLightsEqualBox(t, "after removing torchA", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// 4. Remove torchB: candleC must drop to its own 8 and illuminate around itself!
	w.SetBlock(torchB, voxel.Air)
	if got := w.BlockLight(candleC); got != 8 {
		t.Fatalf("candleC light after removing torchB = %d, want 8", got)
	}
	assertLightsEqualBox(t, "after removing torchB", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// 5. Place a stone wall blocking candleC, then remove it, then place opaque glowstone (15).
	w.FillBox(voxel.BlockCoord{X: 15, Y: 1, Z: 1}, voxel.BlockCoord{X: 15, Y: 9, Z: 24}, stone)
	assertLightsEqualBox(t, "wall blocking candleC", w, cloneAndRecompute(t, w), minCheck, maxCheck)
	if got := w.BlockLight(voxel.BlockCoord{X: 16, Y: 5, Z: 12}); got != 0 {
		t.Fatalf("light behind sealed wall = %d, want 0", got)
	}

	// Open a 1-block doorway in the wall: light floods through the gap!
	w.SetBlock(voxel.BlockCoord{X: 15, Y: 5, Z: 12}, voxel.Air)
	if got := w.BlockLight(voxel.BlockCoord{X: 16, Y: 5, Z: 12}); got != 6 {
		t.Fatalf("light through doorway = %d, want 6", got)
	}
	assertLightsEqualBox(t, "doorway opened", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// Replace doorway with opaque glowstone (emissive 15): emits 15 outward while blocking candle behind it.
	w.SetBlock(voxel.BlockCoord{X: 15, Y: 5, Z: 12}, glowstone)
	if got := w.BlockLight(voxel.BlockCoord{X: 15, Y: 5, Z: 12}); got != 15 {
		t.Fatalf("glowstone self light = %d, want 15", got)
	}
	if got := w.BlockLight(voxel.BlockCoord{X: 16, Y: 5, Z: 12}); got != 14 {
		t.Fatalf("adjacent to glowstone = %d, want 14", got)
	}
	assertLightsEqualBox(t, "glowstone placed", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// Remove glowstone and candleC -> room must return to completely dark (0).
	w.SetBlock(voxel.BlockCoord{X: 15, Y: 5, Z: 12}, voxel.Air)
	w.SetBlock(candleC, voxel.Air)
	assertLightsEqualBox(t, "all lights removed", w, cloneAndRecompute(t, w), minCheck, maxCheck)
	if got := w.BlockLight(torchA); got != 0 {
		t.Fatalf("residual block light after removing all lights = %d, want 0", got)
	}
}

func TestSkyLightColumnsRoofsAndWaterAttenuation(t *testing.T) {
	reg, stone, glass, water, _, _, _, _, _ := lightingTestRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Floor at y=0 in chunk (0,0,0), roof at y=20 in chunk (0,2,0) — note chunk (0,1,0) is empty!
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 15, Y: 0, Z: 15}, stone)
	w.FillBox(voxel.BlockCoord{X: 0, Y: 20, Z: 0}, voxel.BlockCoord{X: 15, Y: 20, Z: 15}, stone)
	// Seal the walls all the way up from y=1 to y=19 on the outer perimeter so inside is a deep shaft.
	w.FillBox(voxel.BlockCoord{X: 0, Y: 1, Z: 0}, voxel.BlockCoord{X: 15, Y: 19, Z: 0}, stone)
	w.FillBox(voxel.BlockCoord{X: 0, Y: 1, Z: 15}, voxel.BlockCoord{X: 15, Y: 19, Z: 15}, stone)
	w.FillBox(voxel.BlockCoord{X: 0, Y: 1, Z: 0}, voxel.BlockCoord{X: 0, Y: 19, Z: 15}, stone)
	w.FillBox(voxel.BlockCoord{X: 15, Y: 1, Z: 0}, voxel.BlockCoord{X: 15, Y: 19, Z: 15}, stone)

	minCheck := voxel.BlockCoord{X: -1, Y: 0, Z: -1}
	maxCheck := voxel.BlockCoord{X: 16, Y: 22, Z: 16}

	// Above roof is sky=15; inside sealed multi-chunk shaft is sky=0.
	if got := w.SkyLight(voxel.BlockCoord{X: 7, Y: 21, Z: 7}); got != 15 {
		t.Fatalf("above roof sky = %d, want 15", got)
	}
	if got := w.SkyLight(voxel.BlockCoord{X: 7, Y: 1, Z: 7}); got != 0 {
		t.Fatalf("inside sealed multi-chunk shaft sky = %d, want 0", got)
	}
	assertLightsEqualBox(t, "sealed 3-chunk-tall chamber", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// Open a 1-block skylight at (7, 20, 7): direct sky=15 must drop straight down
	// through chunks (0,2,0) -> (0,1,0) -> (0,0,0) all the way to y=1!
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 20, Z: 7}, voxel.Air)
	for y := 1; y <= 21; y++ {
		if got := w.SkyLight(voxel.BlockCoord{X: 7, Y: y, Z: 7}); got != 15 {
			t.Fatalf("skylight shaft at y=%d: sky=%d, want 15", y, got)
		}
	}
	if got := w.SkyLight(voxel.BlockCoord{X: 8, Y: 1, Z: 7}); got != 14 {
		t.Fatalf("adjacent to skylight at floor: sky=%d, want 14", got)
	}
	assertLightsEqualBox(t, "skylight opened", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// Put transparent glass (LightOpacity=0) in the skylight: sky=15 still passes straight down!
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 20, Z: 7}, glass)
	if got := w.SkyLight(voxel.BlockCoord{X: 7, Y: 1, Z: 7}); got != 15 {
		t.Fatalf("under glass skylight sky = %d, want 15", got)
	}
	assertLightsEqualBox(t, "glass skylight", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// Replace skylight column with water (LightOpacity=2): attenuates by 2 per block!
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 20, Z: 7}, water)
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 19, Z: 7}, water)
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 18, Z: 7}, water)
	if got := w.SkyLight(voxel.BlockCoord{X: 7, Y: 20, Z: 7}); got != 13 {
		t.Fatalf("water top sky = %d, want 13", got)
	}
	if got := w.SkyLight(voxel.BlockCoord{X: 7, Y: 19, Z: 7}); got != 11 {
		t.Fatalf("water depth 2 sky = %d, want 11", got)
	}
	if got := w.SkyLight(voxel.BlockCoord{X: 7, Y: 18, Z: 7}); got != 9 {
		t.Fatalf("water depth 3 sky = %d, want 9", got)
	}
	assertLightsEqualBox(t, "water attenuation", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// Seal the skylight back with stone: shaft must return to sky=0!
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 20, Z: 7}, stone)
	if got := w.SkyLight(voxel.BlockCoord{X: 7, Y: 1, Z: 7}); got != 0 {
		t.Fatalf("re-sealed skylight sky = %d, want 0", got)
	}
	assertLightsEqualBox(t, "re-sealed skylight", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// Expand world downward into cy=-1 and then remove it: incremental must match full recompute.
	w.SetBlock(voxel.BlockCoord{X: 7, Y: -4, Z: 7}, stone)
	assertLightsEqualBox(t, "expanded downward cy=-1", w, cloneAndRecompute(t, w),
		voxel.BlockCoord{X: -1, Y: -8, Z: -1}, maxCheck)
	w.SetBlock(voxel.BlockCoord{X: 7, Y: -4, Z: 7}, voxel.Air)
	assertLightsEqualBox(t, "removed cy=-1 block", w, cloneAndRecompute(t, w),
		voxel.BlockCoord{X: -1, Y: -8, Z: -1}, maxCheck)
}

func TestIncrementalMatchesFullRecomputeRandomized(t *testing.T) {
	reg, stone, glass, water, leaves, tinted, torch, candle, glowstone := lightingTestRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 4, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	palette := []voxel.BlockID{
		voxel.Air, voxel.Air, voxel.Air,
		stone, stone, glass, water, leaves, tinted, torch, candle, glowstone,
	}

	rng := rand.New(rand.NewSource(860))
	minCheck := voxel.BlockCoord{X: -10, Y: -8, Z: -10}
	maxCheck := voxel.BlockCoord{X: 10, Y: 10, Z: 10}

	// Seed an initial multi-chunk cave & terrain structure across negative and positive coords.
	for range 80 {
		p := voxel.BlockCoord{
			X: rng.Intn(13) - 6,
			Y: rng.Intn(11) - 4,
			Z: rng.Intn(13) - 6,
		}
		w.SetBlock(p, palette[rng.Intn(len(palette))])
	}
	assertLightsEqualBox(t, "initial random structure", w, cloneAndRecompute(t, w), minCheck, maxCheck)

	// Perform 150 incremental edits across chunk boundaries and verify exact equality with full recompute.
	for step := range 150 {
		p := voxel.BlockCoord{
			X: rng.Intn(13) - 6,
			Y: rng.Intn(11) - 4,
			Z: rng.Intn(13) - 6,
		}
		id := palette[rng.Intn(len(palette))]
		if !w.SetBlock(p, id) {
			continue
		}
		// Check every 5th edit and every edit involving emissive or opacity changes near borders.
		if step%5 == 0 || reg.Emissive(id) > 0 || id == voxel.Air {
			assertLightsEqualBox(t, "random incremental edit", w, cloneAndRecompute(t, w), minCheck, maxCheck)
		}
	}
	assertLightsEqualBox(t, "final random state", w, cloneAndRecompute(t, w), minCheck, maxCheck)
}

func TestCrossChunkLightingDirtyTrackingAndNoSeams(t *testing.T) {
	reg, stone, _, _, _, _, torch, _, _ := lightingTestRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 16, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Build a dark tunnel floor & ceiling across 3 chunks along X: (0,0,0), (1,0,0), (2,0,0).
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 47, Y: 0, Z: 15}, stone)
	w.FillBox(voxel.BlockCoord{X: 0, Y: 8, Z: 0}, voxel.BlockCoord{X: 47, Y: 8, Z: 15}, stone)
	// Seal north/south/east/west ends so sky light is 0 inside.
	w.FillBox(voxel.BlockCoord{X: 0, Y: 1, Z: 0}, voxel.BlockCoord{X: 47, Y: 7, Z: 0}, stone)
	w.FillBox(voxel.BlockCoord{X: 0, Y: 1, Z: 15}, voxel.BlockCoord{X: 47, Y: 7, Z: 15}, stone)
	w.FillBox(voxel.BlockCoord{X: 0, Y: 1, Z: 0}, voxel.BlockCoord{X: 0, Y: 7, Z: 15}, stone)
	w.FillBox(voxel.BlockCoord{X: 47, Y: 1, Z: 0}, voxel.BlockCoord{X: 47, Y: 7, Z: 15}, stone)

	mesher := voxel.NewMesher()
	opts := voxel.MeshOptions{
		Lighting:         true,
		SmoothLighting:   true,
		AmbientOcclusion: true,
		WorldSpace:       true,
	}
	mesher.RemeshDirty(w, opts)
	if w.DirtyCount() != 0 {
		t.Fatalf("DirtyCount after initial remesh = %d, want 0", w.DirtyCount())
	}

	// 1. Place a torch at (1, 1, 8) in chunk (0,0,0):
	//    emissive 14 reaches x = 1 + 13 = 14 (x=15 stays 0), so ONLY chunk (0,0,0) must be dirtied!
	w.SetBlock(voxel.BlockCoord{X: 1, Y: 1, Z: 8}, torch)
	if dirty := w.DirtyChunks(); len(dirty) != 1 || dirty[0] != (voxel.ChunkCoord{X: 0}) {
		t.Fatalf("far-from-border torch dirtied %v, want only [(0,0,0)]", dirty)
	}
	mesher.RemeshDirty(w, opts)

	// 2. Place a torch at (14, 1, 8) in chunk (0,0,0):
	//    light crosses x=15|16 into chunk (1,0,0) up to x=27, never reaching chunk (2,0,0) (x>=32).
	//    BOTH (0,0,0) and (1,0,0) must be dirtied, and (2,0,0) must NOT be dirtied!
	w.SetBlock(voxel.BlockCoord{X: 14, Y: 1, Z: 8}, torch)
	dirty := w.DirtyChunks()
	if len(dirty) != 2 || dirty[0] != (voxel.ChunkCoord{X: 0}) || dirty[1] != (voxel.ChunkCoord{X: 1}) {
		t.Fatalf("border-crossing torch dirtied %v, want [(0,0,0) (1,0,0)]", dirty)
	}

	// Verify exact block light continuity across the x=15 | x=16 chunk boundary.
	if got := w.BlockLight(voxel.BlockCoord{X: 15, Y: 1, Z: 8}); got != 13 {
		t.Fatalf("x=15 block light = %d, want 13", got)
	}
	if got := w.BlockLight(voxel.BlockCoord{X: 16, Y: 1, Z: 8}); got != 12 {
		t.Fatalf("x=16 block light = %d, want 12", got)
	}

	// 3. Remesh both chunks with SmoothLighting+AO+WorldSpace and verify ZERO seams
	//    at every shared boundary vertex on the floor top plane (x == 16, y == 1, normal == +Y)!
	rebuilt := mesher.RemeshDirty(w, opts)
	if len(rebuilt) != 2 {
		t.Fatalf("remeshed %d chunks, want 2", len(rebuilt))
	}
	cm0, cm1 := rebuilt[0], rebuilt[1]
	assertValidCCWMesh(t, cm0)
	assertValidCCWMesh(t, cm1)

	type vtxKey struct {
		x, y, z int
	}
	type vtxShade struct {
		sky, blk uint8
		ao       uint8
		color    math3d.Vec4
	}
	collectBoundary := func(cm voxel.ChunkMesh) map[vtxKey]vtxShade {
		m := make(map[vtxKey]vtxShade)
		for i, p := range cm.Mesh.Positions {
			if cm.Mesh.Normals[i] != (math3d.Vec3{Y: 1}) {
				continue
			}
			if math.Abs(float64(p.X-16)) < 1e-4 && math.Abs(float64(p.Y-1)) < 1e-4 {
				k := vtxKey{x: int(math.Round(float64(p.X))), y: int(math.Round(float64(p.Y))), z: int(math.Round(float64(p.Z)))}
				m[k] = vtxShade{
					sky:   cm.SkyLight[i],
					blk:   cm.BlockLight[i],
					ao:    cm.AO[i],
					color: cm.Colors[i],
				}
			}
		}
		return m
	}
	b0 := collectBoundary(cm0)
	b1 := collectBoundary(cm1)
	if len(b0) == 0 {
		t.Fatal("expected boundary vertices on floor at x=16, y=1")
	}
	checked := 0
	for k, s0 := range b0 {
		s1, ok := b1[k]
		if !ok {
			continue
		}
		checked++
		if s0.sky != s1.sky || s0.blk != s1.blk || s0.ao != s1.ao || s0.color != s1.color {
			t.Fatalf("cross-chunk lighting seam at %+v: chunk0=%+v chunk1=%+v", k, s0, s1)
		}
	}
	if checked == 0 {
		t.Fatal("no shared boundary vertices matched between chunk 0 and chunk 1")
	}

	// 4. Removing the border-crossing torch must dirty only (0,0,0) and (1,0,0), not (2,0,0).
	w.SetBlock(voxel.BlockCoord{X: 14, Y: 1, Z: 8}, voxel.Air)
	dirty = w.DirtyChunks()
	if len(dirty) != 2 || dirty[0] != (voxel.ChunkCoord{X: 0}) || dirty[1] != (voxel.ChunkCoord{X: 1}) {
		t.Fatalf("removing border-crossing torch dirtied %v, want [(0,0,0) (1,0,0)]", dirty)
	}
	assertLightsEqualBox(t, "after removing border torch", w, cloneAndRecompute(t, w),
		voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 47, Y: 8, Z: 15})
}

func TestTorchAgainstChunkBoundaryWallAndUnallocatedNeighbor(t *testing.T) {
	reg, stone, _, _, _, _, torch, _, _ := lightingTestRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Place stone at (7, 3, 4) and torch at (7, 3, 3) with chunk (1,0,0) NOT allocated.
	// The +X face of stone at (7,3,4) samples (8,3,4) in unallocated chunk (1,0,0),
	// which must have blockLight=12 (14 -> 13 at (8,3,3) -> 12 at (8,3,4)).
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 3, Z: 4}, stone)
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 3, Z: 3}, torch)
	if w.ChunkCount() != 1 {
		t.Fatalf("ChunkCount = %d, want 1 (airLight must not inflate ChunkCount)", w.ChunkCount())
	}
	if got := w.BlockLight(voxel.BlockCoord{X: 8, Y: 3, Z: 4}); got != 12 {
		t.Fatalf("unallocated neighbor (8,3,4) BlockLight = %d, want 12", got)
	}
	cm := w.MeshChunk(voxel.ChunkCoord{X: 0}, voxel.MeshOptions{Lighting: true})
	foundPosX := false
	for i := 0; i < len(cm.Mesh.Positions); i += 4 {
		if cm.Mesh.Normals[i] != (math3d.Vec3{X: 1}) {
			continue
		}
		// Compute quad center Z to select the stone block at (7,3,4) (Z in [4,5]),
		// not the torch at (7,3,3) (Z in [3,4]).
		centerZ := (cm.Mesh.Positions[i].Z + cm.Mesh.Positions[i+1].Z + cm.Mesh.Positions[i+2].Z + cm.Mesh.Positions[i+3].Z) * 0.25
		if centerZ > 4.0 && centerZ < 5.0 {
			foundPosX = true
			for k := range 4 {
				if cm.BlockLight[i+k] != 12 {
					t.Fatalf("+X face of stone at (7,3,4) vertex %d got BlockLight=%d, want 12", k, cm.BlockLight[i+k])
				}
			}
		}
	}
	if !foundPosX {
		t.Fatal("did not find +X face of stone at (7,3,4)")
	}

	// Now place a wall block at (8, 3, 3) in chunk (1,0,0), clear dirty, remove torch in (0,0,0):
	// chunk (1,0,0) must be dirtied because its -X face sampled (7,3,3)!
	w.SetBlock(voxel.BlockCoord{X: 8, Y: 3, Z: 3}, stone)
	w.RemeshDirty(voxel.MeshOptions{Lighting: true})
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 3, Z: 3}, voxel.Air)
	if !w.IsDirty(voxel.ChunkCoord{X: 1}) {
		t.Fatalf("removing torch pressed against wall in chunk (1,0,0) did not dirty (1,0,0); dirty=%v", w.DirtyChunks())
	}

	// PruneEmptyChunks preserves light passing through an emptied chunk.
	w.Clear()
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 3, Z: 3}, torch)
	w.SetBlock(voxel.BlockCoord{X: 10, Y: 3, Z: 3}, stone)
	w.RemeshDirty(voxel.MeshOptions{Lighting: true})
	w.SetBlock(voxel.BlockCoord{X: 10, Y: 3, Z: 3}, voxel.Air)
	w.RemeshDirty(voxel.MeshOptions{Lighting: true})
	if pruned := w.PruneEmptyChunks(); pruned != 1 {
		t.Fatalf("PruneEmptyChunks = %d, want 1", pruned)
	}
	if got := w.BlockLight(voxel.BlockCoord{X: 10, Y: 3, Z: 3}); got != 11 {
		t.Fatalf("BlockLight after pruning empty chunk = %d, want 11", got)
	}
}

func TestMeshVertexLightAndRender3DColoredMesh(t *testing.T) {
	reg, stone, _, _, _, _, torch, _, glowstone := lightingTestRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Verify LightFactor is strictly monotonic from 0 to 15.
	if voxel.LightFactor(0) != 0 || voxel.LightFactor(voxel.MaxLightLevel) != 1 {
		t.Fatalf("LightFactor endpoints = (%v, %v), want (0, 1)", voxel.LightFactor(0), voxel.LightFactor(voxel.MaxLightLevel))
	}
	for l := uint8(0); l < voxel.MaxLightLevel; l++ {
		if voxel.LightFactor(l) >= voxel.LightFactor(l+1) {
			t.Fatalf("LightFactor(%d)=%v >= LightFactor(%d)=%v", l, voxel.LightFactor(l), l+1, voxel.LightFactor(l+1))
		}
	}

	// Flat sunlit 8x8 floor at y=0: every top face has uniform sky=15, block=0,
	// so greedy meshing with Lighting=true must STILL merge the entire top face into 1 quad!
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 7, Y: 0, Z: 7}, stone)
	sunlit := w.MeshChunk(voxel.ChunkCoord{}, voxel.MeshOptions{Lighting: true})
	assertValidCCWMesh(t, sunlit)
	topQuads := 0
	for i := 0; i < len(sunlit.Mesh.Positions); i += 4 {
		if sunlit.Mesh.Normals[i] == (math3d.Vec3{Y: 1}) {
			topQuads++
			for k := range 4 {
				if sunlit.SkyLight[i+k] != 15 || sunlit.BlockLight[i+k] != 0 {
					t.Fatalf("sunlit floor top vertex light = (%d, %d), want (15, 0)", sunlit.SkyLight[i+k], sunlit.BlockLight[i+k])
				}
				if sunlit.Colors[i+k] != (math3d.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
					t.Fatalf("sunlit floor color = %+v, want white", sunlit.Colors[i+k])
				}
			}
		}
	}
	if topQuads != 1 {
		t.Fatalf("uniformly sunlit 8x8 floor topQuads = %d, want 1", topQuads)
	}

	// Place a torch at (3, 1, 3) and test night-time LightStyle + AO + render3d.ColoredMesh!
	w.SetBlock(voxel.BlockCoord{X: 3, Y: 1, Z: 3}, torch)
	w.SetBlock(voxel.BlockCoord{X: 5, Y: 1, Z: 3}, glowstone)

	nightStyle := voxel.LightStyle{
		SkyColor:   math3d.Vec3{X: 0.1, Y: 0.15, Z: 0.3},
		BlockColor: math3d.Vec3{X: 1.0, Y: 0.85, Z: 0.6},
		Ambient:    0.02,
	}
	cm := w.MeshChunk(voxel.ChunkCoord{}, voxel.MeshOptions{
		Lighting:         true,
		AmbientOcclusion: true,
		LightStyle:       nightStyle,
	})
	assertValidCCWMesh(t, cm)
	if len(cm.SkyLight) != len(cm.Mesh.Positions) || len(cm.BlockLight) != len(cm.Mesh.Positions) || len(cm.Colors) != len(cm.Mesh.Positions) {
		t.Fatalf("light/color slice lengths (%d, %d, %d) != vertex count %d",
			len(cm.SkyLight), len(cm.BlockLight), len(cm.Colors), len(cm.Mesh.Positions))
	}

	// Greedy meshing must split the floor top face where block light varies.
	nightTopQuads := 0
	sawGlowstoneMax := false
	for i := 0; i < len(cm.Mesh.Positions); i += 4 {
		if cm.Mesh.Normals[i] == (math3d.Vec3{Y: 1}) {
			nightTopQuads++
		}
		// Glowstone at (5,1,3) must emit BlockLight==15 on its own faces.
		p := cm.Mesh.Positions[i]
		if p.X >= 5 && p.X <= 6 && p.Y >= 1 && p.Y <= 2 && p.Z >= 3 && p.Z <= 4 {
			if cm.BlockLight[i] == 15 {
				sawGlowstoneMax = true
			}
		}
		// Verify vertex color matches CombineLightColor(sky, blk, AOFactor(ao), nightStyle).
		for k := range 4 {
			wantCol := voxel.CombineLightColor(cm.SkyLight[i+k], cm.BlockLight[i+k], voxel.AOFactor(cm.AO[i+k]), nightStyle)
			if cm.Colors[i+k] != wantCol {
				t.Fatalf("vertex %d color = %+v, want %+v", i+k, cm.Colors[i+k], wantCol)
			}
		}
	}
	if nightTopQuads <= 1 {
		t.Fatalf("expected floor top face to split across varying torch light, got %d quads", nightTopQuads)
	}
	if !sawGlowstoneMax {
		t.Fatal("expected glowstone block faces to carry BlockLight == 15")
	}

	// Verify render3d.ColoredMesh compatibility and packed GPU vertex buffer layout.
	colored := cm.ColoredMesh()
	if err := colored.Validate(); err != nil {
		t.Fatalf("render3d.ColoredMesh.Validate failed: %v", err)
	}
	if !colored.HasVertexColors() {
		t.Fatal("expected ColoredMesh.HasVertexColors() == true")
	}
	vbuf := colored.AppendVertexData(nil)
	if len(vbuf) != colored.VertexCount()*render3d.ColoredVertexStride {
		t.Fatalf("vertex buffer bytes = %d, want %d", len(vbuf), colored.VertexCount()*render3d.ColoredVertexStride)
	}
	// Spot-check first vertex's packed linear RGBA color at ColoredColorOffset (32).
	r := math.Float32frombits(binary.LittleEndian.Uint32(vbuf[render3d.ColoredColorOffset:]))
	g := math.Float32frombits(binary.LittleEndian.Uint32(vbuf[render3d.ColoredColorOffset+4:]))
	b := math.Float32frombits(binary.LittleEndian.Uint32(vbuf[render3d.ColoredColorOffset+8:]))
	a := math.Float32frombits(binary.LittleEndian.Uint32(vbuf[render3d.ColoredColorOffset+12:]))
	if r != cm.Colors[0].X || g != cm.Colors[0].Y || b != cm.Colors[0].Z || a != cm.Colors[0].W {
		t.Fatalf("packed GPU color = (%v,%v,%v,%v), want %+v", r, g, b, a, cm.Colors[0])
	}

	// Verify Lighting: false leaves SkyLight/BlockLight/Colors nil, while ColoredMesh() still validates with AO.
	aoOnly := w.MeshChunk(voxel.ChunkCoord{}, voxel.MeshOptions{AmbientOcclusion: true})
	if aoOnly.SkyLight != nil || aoOnly.BlockLight != nil || aoOnly.Colors != nil {
		t.Fatal("expected nil SkyLight/BlockLight/Colors when Lighting is false")
	}
	if err := aoOnly.ColoredMesh().Validate(); err != nil {
		t.Fatalf("AO-only ColoredMesh.Validate failed: %v", err)
	}
}

func BenchmarkTorchPlaceRemove(b *testing.B) {
	reg := voxel.NewRegistry()
	stone := reg.MustRegister(voxel.BlockDef{Name: "stone"})
	torch := reg.MustRegister(voxel.BlockDef{Name: "torch", Transparent: true, NonSolid: true, Emissive: 14})

	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 16, Registry: reg})
	if err != nil {
		b.Fatal(err)
	}
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 31, Y: 0, Z: 31}, stone)
	w.FillBox(voxel.BlockCoord{X: 0, Y: 10, Z: 0}, voxel.BlockCoord{X: 31, Y: 10, Z: 31}, stone)
	mesher := voxel.NewMesher()
	opts := voxel.MeshOptions{Lighting: true, SmoothLighting: true, AmbientOcclusion: true}
	mesher.RemeshDirty(w, opts)

	pos := voxel.BlockCoord{X: 15, Y: 1, Z: 15}
	b.ResetTimer()
	for i := range b.N {
		if i%2 == 0 {
			w.SetBlock(pos, torch)
		} else {
			w.SetBlock(pos, voxel.Air)
		}
		_ = mesher.RemeshDirty(w, opts)
	}
}

func BenchmarkMeshChunkWithLighting(b *testing.B) {
	w, _ := buildTerrain256(b)
	mesher := voxel.NewMesher()
	cc := voxel.ChunkCoord{X: 8, Y: 0, Z: 8}
	opts := voxel.MeshOptions{Lighting: true, SmoothLighting: true, AmbientOcclusion: true}
	b.ResetTimer()
	for range b.N {
		_ = mesher.MeshChunk(w, cc, opts)
	}
}
