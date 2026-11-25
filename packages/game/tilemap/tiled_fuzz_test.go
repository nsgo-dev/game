package tilemap_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func exerciseFuzzedTilemap(m *tilemap.Tilemap) {
	m.Update(16 * time.Millisecond)
	m.Update(250 * time.Millisecond)

	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	if math.IsNaN(tw) || math.IsInf(tw, 0) || math.IsNaN(th) || math.IsInf(th, 0) || tw <= 0 || th <= 0 {
		return
	}
	// Keep probe extents bounded to a handful of tiles so extreme fuzzed
	// dimensions cannot turn a single fuzz iteration into a billion-cell walk.
	spanX := float32(math.Min(tw*8, 1e6))
	spanY := float32(math.Min(th*6, 1e6))

	var d core.Drawing
	cam := &tilemap.Camera{X: spanX / 2, Y: spanY / 2, Zoom: 1, Width: spanX, Height: spanY}
	m.Draw(&d, cam)
	if m.Width <= 64 && m.Height <= 64 {
		d.Ops = d.Ops[:0]
		m.Draw(&d, nil)
	}

	_ = m.TilesInRect(core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: core.Points(spanX), Height: core.Points(spanY)},
	}, tilemap.TileSolid)
	_ = m.TilesInRect(core.Bounds{
		Origin: core.Point{X: core.Points(-tw), Y: core.Points(-th)},
		Size:   core.Size{Width: core.Points(spanX), Height: core.Points(spanY)},
	}, 0)

	_, _ = m.Raycast(
		core.Point{X: 0, Y: 0},
		core.Point{X: core.Points(spanX), Y: core.Points(spanY)},
		tilemap.TileSolid,
	)
	_, _ = m.Raycast(
		core.Point{X: core.Points(spanX), Y: core.Points(spanY)},
		core.Point{X: 0, Y: 0},
		0,
	)
}

func fuzzEncodeGIDs(gids []uint32, comp string) string {
	raw := make([]byte, len(gids)*4)
	for i, g := range gids {
		binary.LittleEndian.PutUint32(raw[i*4:i*4+4], g)
	}
	switch comp {
	case "zlib":
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		_, _ = w.Write(raw)
		_ = w.Close()
		raw = buf.Bytes()
	case "gzip":
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(raw)
		_ = w.Close()
		raw = buf.Bytes()
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func FuzzLoadTiled(f *testing.F) {
	sampleMap, err := os.ReadFile(filepath.Join("testdata", "sample.tmj"))
	if err != nil {
		f.Fatalf("read sample.tmj: %v", err)
	}
	sampleTileset, err := os.ReadFile(filepath.Join("testdata", "tilesets", "props.tsj"))
	if err != nil {
		f.Fatalf("read props.tsj: %v", err)
	}
	sampleTMX, err := os.ReadFile(filepath.Join("testdata", "sample.tmx"))
	if err != nil {
		f.Fatalf("read sample.tmx: %v", err)
	}
	sampleTSX, err := os.ReadFile(filepath.Join("testdata", "tilesets", "props.tsx"))
	if err != nil {
		f.Fatalf("read props.tsx: %v", err)
	}

	f.Add(sampleMap, sampleTileset)
	f.Add(sampleMap, sampleTSX)
	f.Add(sampleTMX, sampleTSX)
	f.Add(sampleTMX, sampleTileset)
	f.Add(sampleMap, []byte(`{}`))
	f.Add([]byte(`{"width":2,"height":2,"tilewidth":16,"tileheight":16,"orientation":"orthogonal"}`), []byte{})
	f.Add([]byte(`{}`), []byte{})
	f.Add([]byte(`<map width="2" height="1" tilewidth="16" tileheight="16"><layer name="a"><data encoding="base64" compression="zlib">`+fuzzEncodeGIDs([]uint32{1, 2}, "zlib")+`</data></layer></map>`), sampleTSX)
	f.Add([]byte(`<map width="2" height="1" tilewidth="16" tileheight="16"><layer name="a"><data encoding="base64" compression="gzip">`+fuzzEncodeGIDs([]uint32{1, 2}, "gzip")+`</data></layer></map>`), []byte{})

	f.Fuzz(func(t *testing.T, mapData, extTilesetData []byte) {
		m, err := tilemap.LoadTiled(mapData, tilemap.TilesetLoader{
			LoadTileset: func(string) ([]byte, error) {
				return extTilesetData, nil
			},
			LoadImage: func(imagePath string) (core.ImageSource, error) {
				return core.ImageSource{URI: "asset:" + imagePath}, nil
			},
		})
		if err != nil || m == nil {
			return
		}
		exerciseFuzzedTilemap(m)
	})
}

func FuzzLoadTiledTMX(f *testing.F) {
	sampleTMX, err := os.ReadFile(filepath.Join("testdata", "sample.tmx"))
	if err != nil {
		f.Fatalf("read sample.tmx: %v", err)
	}
	sampleTSX, err := os.ReadFile(filepath.Join("testdata", "tilesets", "props.tsx"))
	if err != nil {
		f.Fatalf("read props.tsx: %v", err)
	}
	zChunk := fuzzEncodeGIDs([]uint32{1, 0}, "zlib")
	gChunk := fuzzEncodeGIDs([]uint32{0, 2}, "gzip")
	infTMX := []byte(fmt.Sprintf(`<?xml version="1.0"?><map width="0" height="0" tilewidth="16" tileheight="16" infinite="1"><tileset firstgid="1" source="ext.tsx"/><layer name="g"><data encoding="base64" compression="zlib"><chunk x="-2" y="-1" width="2" height="1">%s</chunk><chunk x="0" y="0" width="2" height="1">%s</chunk></data></layer></map>`, zChunk, zChunk))
	infGzipTMX := []byte(fmt.Sprintf(`<?xml version="1.0"?><map width="0" height="0" tilewidth="16" tileheight="16" infinite="1"><layer name="g"><data encoding="base64" compression="gzip"><chunk x="0" y="0" width="2" height="1">%s</chunk></data></layer></map>`, gChunk))
	xmlTilesTMX := []byte(`<map width="2" height="1" tilewidth="16" tileheight="16"><layer name="g"><data><tile gid="1"/><tile/></data></layer></map>`)

	f.Add(sampleTMX, sampleTSX)
	f.Add(infTMX, sampleTSX)
	f.Add(infGzipTMX, []byte{})
	f.Add(xmlTilesTMX, []byte{})
	f.Add([]byte(`<map`), []byte(`<tileset`))

	f.Fuzz(func(t *testing.T, mapData, extTilesetData []byte) {
		m, err := tilemap.LoadTiled(mapData, tilemap.TilesetLoader{
			LoadTileset: func(string) ([]byte, error) {
				return extTilesetData, nil
			},
			LoadImage: func(imagePath string) (core.ImageSource, error) {
				return core.ImageSource{URI: "asset:" + imagePath}, nil
			},
		})
		if err != nil || m == nil {
			return
		}
		exerciseFuzzedTilemap(m)
	})
}

func FuzzLoadTiledCompressed(f *testing.F) {
	f.Add(uint8(0), uint16(2), uint16(2), []byte{1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 4, 0, 0, 0})
	f.Add(uint8(1), uint16(2), uint16(1), []byte{0x78, 0x9c, 0x63, 0x64, 0x60, 0x60, 0x60, 0x62, 0x60, 0x60, 0x00, 0x00, 0x00, 0x0c, 0x00, 0x04})
	f.Add(uint8(2), uint16(1), uint16(1), []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x63, 0x64, 0x60, 0x60, 0x00, 0x00, 0xa2, 0x93, 0x20, 0x16, 0x04, 0x00, 0x00, 0x00})
	f.Add(uint8(3), uint16(2), uint16(2), []byte{1, 2, 3})

	f.Fuzz(func(t *testing.T, mode uint8, w, h uint16, payload []byte) {
		lw := int(w % 16)
		lh := int(h % 16)
		b64 := base64.StdEncoding.EncodeToString(payload)
		var comp string
		switch mode % 4 {
		case 0:
			comp = ""
		case 1:
			comp = "zlib"
		case 2:
			comp = "gzip"
		case 3:
			comp = "zstd"
		}
		var doc []byte
		if (mode>>2)&1 == 0 {
			// TMX (finite or chunked)
			compAttr := ""
			if comp != "" {
				compAttr = ` compression="` + comp + `"`
			}
			if (mode>>3)&1 == 0 {
				doc = []byte(fmt.Sprintf(`<map width="%d" height="%d" tilewidth="16" tileheight="16"><layer name="l" width="%d" height="%d"><data encoding="base64"%s>%s</data></layer></map>`, lw, lh, lw, lh, compAttr, b64))
			} else {
				doc = []byte(fmt.Sprintf(`<map width="0" height="0" tilewidth="16" tileheight="16" infinite="1"><layer name="l"><data encoding="base64"%s><chunk x="0" y="0" width="%d" height="%d">%s</chunk></data></layer></map>`, compAttr, lw, lh, b64))
			}
		} else {
			// TMJ (finite or chunked)
			if (mode>>3)&1 == 0 {
				doc = []byte(fmt.Sprintf(`{"width":%d,"height":%d,"tilewidth":16,"tileheight":16,"layers":[{"type":"tilelayer","name":"l","width":%d,"height":%d,"encoding":"base64","compression":%q,"data":%q}]}`, lw, lh, lw, lh, comp, b64))
			} else {
				doc = []byte(fmt.Sprintf(`{"width":0,"height":0,"tilewidth":16,"tileheight":16,"infinite":true,"layers":[{"type":"tilelayer","name":"l","encoding":"base64","compression":%q,"chunks":[{"x":0,"y":0,"width":%d,"height":%d,"data":%q}]}]}`, comp, lw, lh, b64))
			}
		}
		m, err := tilemap.LoadTiled(doc, tilemap.TilesetLoader{})
		if err != nil || m == nil {
			return
		}
		exerciseFuzzedTilemap(m)
	})
}
