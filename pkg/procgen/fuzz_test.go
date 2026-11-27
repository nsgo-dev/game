package procgen_test

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/nsgo-dev/game/pkg/procgen"
	"github.com/nsgo-dev/game/pkg/random"
)

func FuzzParseRules(f *testing.F) {
	if data, err := os.ReadFile("testdata/ruins.yaml"); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\ntiles:\n  - {name: a, weight: 1, walkable: true}\nneighbors:\n  - {left: a, right: a}\n"))
	f.Add([]byte("version: 1\ntiles: []\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := procgen.ParseRules(data)
		if err != nil {
			return
		}
		out, err := procgen.EncodeRules(r)
		if err != nil {
			t.Fatalf("EncodeRules of parsed rules: %v", err)
		}
		r2, err := procgen.ParseRules(out)
		if err != nil {
			t.Fatalf("ParseRules of encoded rules: %v\n%s", err, out)
		}
		out2, err := procgen.EncodeRules(r2)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("round trip changed the file: %v\n%s\n---\n%s", err, out, out2)
		}
		if len(r.Tiles) <= 8 {
			g, err := procgen.WFC(random.New(1), procgen.WFCConfig{Width: 6, Height: 5, Rules: r, Attempts: 2})
			if err == nil && !g.Connected(r.Walkable) {
				t.Fatalf("WFC returned a disconnected level:\n%s", g)
			}
			if err != nil && !errors.Is(err, procgen.ErrNoSolution) {
				t.Fatalf("WFC: %v", err)
			}
		}
	})
}

func FuzzBSP(f *testing.F) {
	f.Add(uint64(1), 40, 30, 10, 4, 0, 1, 2)
	f.Add(uint64(2), 12, 12, 0, 0, 0, 0, 0)
	f.Add(uint64(3), 70, 20, 7, 3, 5, 2, 9)
	f.Fuzz(func(t *testing.T, seed uint64, w, h, minLeaf, minRoom, maxRoom, padding, loops int) {
		if w > 120 || h > 120 || loops > 50 {
			t.Skip()
		}
		cfg := procgen.BSPConfig{Width: w, Height: h, MinLeaf: minLeaf, MinRoom: minRoom, MaxRoom: maxRoom, Padding: padding, Loops: loops}
		d, err := procgen.BSP(random.New(seed), cfg)
		if err != nil {
			if !errors.Is(err, procgen.ErrInvalidConfig) {
				t.Fatalf("BSP(%+v): %v", cfg, err)
			}
			return
		}
		if len(d.Rooms) == 0 || !d.Grid.Connected(procgen.IsFloor) {
			t.Fatalf("BSP(%+v) gave %d rooms, connected %v:\n%s", cfg, len(d.Rooms), d.Grid.Connected(procgen.IsFloor), d.Grid)
		}
	})
}

func FuzzCave(f *testing.F) {
	f.Add(uint64(1), 40, 30, 0.45, 5, 5, 3, 0, true)
	f.Add(uint64(2), 3, 3, 0.0, 0, 0, 0, 0, false)
	f.Add(uint64(3), 50, 10, 0.6, 2, 6, 2, 10, true)
	f.Fuzz(func(t *testing.T, seed uint64, w, h int, chance float64, steps, wallAt, floorAt, minRegion int, join bool) {
		if w > 120 || h > 120 || steps > 20 {
			t.Skip()
		}
		cfg := procgen.CaveConfig{Width: w, Height: h, WallChance: chance, Steps: steps, WallAt: wallAt, FloorAt: floorAt, MinRegion: minRegion, Join: join}
		g, err := procgen.Cave(random.New(seed), cfg)
		if err != nil {
			if !errors.Is(err, procgen.ErrInvalidConfig) && !errors.Is(err, procgen.ErrNoFloor) {
				t.Fatalf("Cave(%+v): %v", cfg, err)
			}
			return
		}
		if g.Count(procgen.IsFloor) == 0 || !g.Connected(procgen.IsFloor) {
			t.Fatalf("Cave(%+v) is empty or disconnected:\n%s", cfg, g)
		}
	})
}

func FuzzParseRoomTemplates(f *testing.F) {
	if data, err := os.ReadFile("testdata/dungeon_rooms.yaml"); err == nil {
		f.Add(data)
	}
	if data, err := os.ReadFile("testdata/canonical/rooms.yaml"); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\nrooms: []\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		rt, err := procgen.ParseRoomTemplates(data)
		if err != nil {
			return
		}
		out, err := procgen.EncodeRoomTemplates(rt)
		if err != nil {
			t.Fatalf("EncodeRoomTemplates: %v", err)
		}
		rt2, err := procgen.ParseRoomTemplates(out)
		if err != nil {
			t.Fatalf("ParseRoomTemplates of encoded: %v\n%s", err, out)
		}
		out2, err := procgen.EncodeRoomTemplates(rt2)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("round trip changed room templates: %v\n%s\n---\n%s", err, out, out2)
		}
	})
}
