package fog_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/fog"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestFogOriginFromTilemapDrawAndSnapshot(t *testing.T) {
	m := tilemap.New(4, 3, 16, 16)
	m.Origin = core.Point{X: 64, Y: -32}
	m.SetCellFlags(1, 1, tilemap.TileSolid)

	f := fog.NewFromTilemap(m, tilemap.TileSolid)
	if f.Origin != m.Origin {
		t.Fatalf("NewFromTilemap Origin = %+v, want %+v", f.Origin, m.Origin)
	}
	if !f.Opaque(1, 1) {
		t.Fatal("expected (1,1) to be opaque")
	}

	// CellAt honors Origin: (64+24, -32+24) = (88, -8) is cell (1, 1).
	col, row, ok := f.CellAt(core.Point{X: 88, Y: -8})
	if !ok || col != 1 || row != 1 {
		t.Fatalf("CellAt(88,-8) = (%d,%d,%v), want (1,1,true)", col, row, ok)
	}
	// Point at (8, 8) is outside the offset grid.
	if _, _, ok := f.CellAt(core.Point{X: 8, Y: 8}); ok {
		t.Fatal("expected (8,8) to be out of bounds for offset fog")
	}

	// MoveTo and VisibleAt honor Origin.
	id := f.AddSource(fog.Source{Team: 1, Col: 0, Row: 0, Radius: 1})
	f.MoveTo(id, m.TileToWorld(2, 1))
	f.Update()
	if !f.VisibleAt(1, m.TileToWorld(2, 1)) {
		t.Fatal("expected source cell to be visible at its world coordinates")
	}

	// Orthogonal Draw emits rects shifted by Origin and culls with world-space Camera.
	cam := &tilemap.Camera{X: 96, Y: -8, Width: 64, Height: 48}
	var d core.Drawing
	f.Draw(&d, 1, cam, fog.DefaultStyle())
	rectCount := 0
	for _, op := range d.Ops {
		if op.Kind == core.DrawRect {
			rectCount++
			if op.X < 64 || op.Y < -32 {
				t.Fatalf("DrawRect emitted outside Origin (64,-32): (%v, %v)", op.X, op.Y)
			}
		}
	}
	if rectCount == 0 {
		t.Fatal("expected fog Draw to emit rects inside offset camera view")
	}

	// Isometric Fog also honors Origin for CellAt and Draw.
	iso := tilemap.New(3, 3, 32, 16)
	iso.Orientation = tilemap.OrientationIsometric
	iso.Origin = core.Point{X: 120, Y: 40}
	fIso := fog.NewFromTilemap(iso, 0)
	pt := iso.TileToWorld(1, 2)
	if ic, ir, ok := fIso.CellAt(pt); !ok || ic != 1 || ir != 2 {
		t.Fatalf("isometric CellAt(%+v) = (%d,%d,%v), want (1,2,true)", pt, ic, ir, ok)
	}
	var dIso core.Drawing
	isoBounds := iso.WorldBounds()
	isoCam := &tilemap.Camera{
		X:      float32(isoBounds.Origin.X + isoBounds.Size.Width/2),
		Y:      float32(isoBounds.Origin.Y + isoBounds.Size.Height/2),
		Width:  float32(isoBounds.Size.Width),
		Height: float32(isoBounds.Size.Height),
	}
	fIso.Draw(&dIso, 1, isoCam, fog.DefaultStyle())
	if len(dIso.Ops) == 0 {
		t.Fatal("expected isometric offset fog to emit drawing ops")
	}

	// Snapshot round-trips non-zero Origin, and omits originX/originY when Origin is zero.
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var restored fog.Fog
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if restored.Origin != f.Origin {
		t.Fatalf("restored Origin = %+v, want %+v", restored.Origin, f.Origin)
	}
	if !restored.VisibleAt(1, m.TileToWorld(2, 1)) {
		t.Fatal("restored fog lost world-space visibility")
	}

	zeroFog := fog.New(2, 2, 16, 16)
	zeroRaw, err := json.Marshal(zeroFog)
	if err != nil {
		t.Fatalf("MarshalJSON(zero): %v", err)
	}
	if strings.Contains(string(zeroRaw), "originX") || strings.Contains(string(zeroRaw), "originY") {
		t.Fatalf("zero-origin fog JSON should omit origin fields: %s", zeroRaw)
	}
}
