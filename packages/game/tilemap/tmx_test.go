package tilemap_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func encodeTileData(t testing.TB, gids []uint32, compression string) string {
	t.Helper()
	raw := make([]byte, len(gids)*4)
	for i, g := range gids {
		binary.LittleEndian.PutUint32(raw[i*4:i*4+4], g)
	}
	var payload []byte
	switch compression {
	case "":
		payload = raw
	case "zlib":
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		if _, err := w.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		payload = buf.Bytes()
	case "gzip":
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		payload = buf.Bytes()
	default:
		t.Fatalf("unknown test compression %q", compression)
	}
	return base64.StdEncoding.EncodeToString(payload)
}

func xmlTilesMarkup(gids []uint32) string {
	var b strings.Builder
	for _, g := range gids {
		if g == 0 {
			b.WriteString("<tile/>")
		} else {
			fmt.Fprintf(&b, `<tile gid="%d"/>`, g)
		}
	}
	return b.String()
}

func assertEqualTilemaps(t *testing.T, got, want *tilemap.Tilemap) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tilemaps differ:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func sampleLoader(images *[]string, useLoadTileset bool) tilemap.TilesetLoader {
	read := func(source string) ([]byte, error) {
		return os.ReadFile(filepath.Join("testdata", filepath.FromSlash(source)))
	}
	l := tilemap.TilesetLoader{
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			if images != nil {
				*images = append(*images, imagePath)
			}
			return core.ImageSource{URI: "asset:" + imagePath}, nil
		},
	}
	if useLoadTileset {
		l.LoadTileset = read
	} else {
		l.LoadJSON = read
	}
	return l
}

func TestLoadTiledTMXAndTMJEquivalenceAcrossEncodings(t *testing.T) {
	want, wantImages := loadSampleTiled(t)

	groundGIDs := []uint32{
		1, 2147483649, 1073741825, 3221225473,
		2684354561, 1610612737, 536870913, 3758096385,
		2, 3, 9, 10,
	}
	overlayGIDs := []uint32{
		0, 0, 0, 0,
		0, 9, 0, 0,
		0, 0, 0, 0,
	}
	hiddenGIDs := []uint32{
		1, 1, 1, 1,
		0, 0, 0, 0,
		0, 0, 0, 0,
	}

	sampleTMX, err := os.ReadFile(filepath.Join("testdata", "sample.tmx"))
	if err != nil {
		t.Fatal(err)
	}

	// Build TMX variants by replacing the three CSV <data> blocks.
	replaceTMXData := func(tmx string, layerData [3]string) []byte {
		out := tmx
		for i, gids := range [][]uint32{groundGIDs, overlayGIDs, hiddenGIDs} {
			_ = gids
			start := strings.Index(out, `<data encoding="csv">`)
			if start < 0 {
				t.Fatalf("missing csv data block %d", i)
			}
			end := strings.Index(out[start:], `</data>`)
			if end < 0 {
				t.Fatalf("missing </data> %d", i)
			}
			out = out[:start] + layerData[i] + out[start+end+len(`</data>`):]
		}
		return []byte(out)
	}

	tmxBase64 := func(comp string) []byte {
		attr := `encoding="base64"`
		if comp != "" {
			attr += ` compression="` + comp + `"`
		}
		var blocks [3]string
		for i, gids := range [][]uint32{groundGIDs, overlayGIDs, hiddenGIDs} {
			enc := encodeTileData(t, gids, comp)
			// Indent with spaces and newlines as Tiled does.
			blocks[i] = fmt.Sprintf("<data %s>\n   %s\n  </data>", attr, enc)
		}
		return replaceTMXData(string(sampleTMX), blocks)
	}

	tmxXMLTiles := replaceTMXData(string(sampleTMX), [3]string{
		"<data>" + xmlTilesMarkup(groundGIDs) + "</data>",
		"<data>" + xmlTilesMarkup(overlayGIDs) + "</data>",
		"<data>" + xmlTilesMarkup(hiddenGIDs) + "</data>",
	})

	// Build TMJ variants with base64, base64+zlib, and base64+gzip layer data.
	sampleTMJ, err := os.ReadFile(filepath.Join("testdata", "sample.tmj"))
	if err != nil {
		t.Fatal(err)
	}
	tmjBase64 := func(comp string) []byte {
		var doc map[string]any
		if err := json.Unmarshal(sampleTMJ, &doc); err != nil {
			t.Fatal(err)
		}
		layers := doc["layers"].([]any)
		for i, gids := range [][]uint32{groundGIDs, overlayGIDs, hiddenGIDs} {
			l := layers[i].(map[string]any)
			l["encoding"] = "base64"
			if comp != "" {
				l["compression"] = comp
			}
			l["data"] = encodeTileData(t, gids, comp)
		}
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	cases := []struct {
		name           string
		data           []byte
		useLoadTileset bool
	}{
		{"TMX CSV (LoadTileset)", sampleTMX, true},
		{"TMX CSV (LoadJSON fallback)", sampleTMX, false},
		{"TMX XML tiles", tmxXMLTiles, true},
		{"TMX base64 uncompressed", tmxBase64(""), true},
		{"TMX base64+zlib", tmxBase64("zlib"), true},
		{"TMX base64+gzip", tmxBase64("gzip"), true},
		{"TMX with UTF-8 BOM", append([]byte{0xEF, 0xBB, 0xBF}, sampleTMX...), true},
		{"TMJ base64 uncompressed", tmjBase64(""), false},
		{"TMJ base64+zlib", tmjBase64("zlib"), false},
		{"TMJ base64+gzip", tmjBase64("gzip"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var imgs []string
			got, err := tilemap.LoadTiled(tc.data, sampleLoader(&imgs, tc.useLoadTileset))
			if err != nil {
				t.Fatalf("LoadTiled(%s): %v", tc.name, err)
			}
			assertEqualTilemaps(t, got, want)
			if !reflect.DeepEqual(imgs, wantImages) {
				t.Fatalf("images = %v, want %v", imgs, wantImages)
			}
		})
	}
}

func TestLoadTiledCrossFormatExternalTilesets(t *testing.T) {
	want, _ := loadSampleTiled(t)

	// 1. TMJ map referencing external .tsx tileset.
	sampleTMJ, err := os.ReadFile(filepath.Join("testdata", "sample.tmj"))
	if err != nil {
		t.Fatal(err)
	}
	tmjWithTSX := bytes.Replace(sampleTMJ, []byte(`"tilesets/props.tsj"`), []byte(`"tilesets/props.tsx"`), 1)
	gotTMJTSX, err := tilemap.LoadTiled(tmjWithTSX, sampleLoader(nil, false))
	if err != nil {
		t.Fatalf("TMJ -> TSX: %v", err)
	}
	assertEqualTilemaps(t, gotTMJTSX, want)

	// 2. TMX map referencing external .tsj tileset.
	sampleTMX, err := os.ReadFile(filepath.Join("testdata", "sample.tmx"))
	if err != nil {
		t.Fatal(err)
	}
	tmxWithTSJ := bytes.Replace(sampleTMX, []byte(`source="tilesets/props.tsx"`), []byte(`source="tilesets/props.tsj"`), 1)
	gotTMXTSJ, err := tilemap.LoadTiled(tmxWithTSJ, sampleLoader(nil, true))
	if err != nil {
		t.Fatalf("TMX -> TSJ: %v", err)
	}
	assertEqualTilemaps(t, gotTMXTSJ, want)
}

func TestLoadTiledInfiniteTMXAndTMJCompressedChunks(t *testing.T) {
	chunkA := []uint32{1, 0}
	chunkB := []uint32{0, 2}

	tmjRaw := []byte(`{
		"width": 0, "height": 0, "tilewidth": 16, "tileheight": 16,
		"orientation": "orthogonal",
		"infinite": true,
		"tilesets": [
			{"firstgid": 1, "name": "t", "image": "tiles.png", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 4}
		],
		"layers": [
			{
				"name": "sky", "type": "imagelayer", "image": "sky.png",
				"imagewidth": 64, "imageheight": 32,
				"offsetx": 0, "offsety": 0, "opacity": 1, "visible": true,
				"parallaxx": 0.5, "parallaxy": 1, "repeatx": true, "repeaty": false
			},
			{
				"name": "world", "type": "group", "offsetx": 4, "offsety": 6, "opacity": 0.5, "visible": true,
				"layers": [
					{
						"name": "sub", "type": "group", "offsetx": 2, "offsety": -2, "opacity": 0.5, "visible": true,
						"layers": [
							{
								"name": "ground", "type": "tilelayer", "visible": true, "opacity": 1,
								"chunks": [
									{"x": -2, "y": -1, "width": 2, "height": 1, "data": [1, 0]},
									{"x": 0, "y": 0, "width": 2, "height": 1, "data": [0, 2]}
								]
							},
							{
								"name": "fog", "type": "imagelayer", "image": "fog.png",
								"imagewidth": 32, "imageheight": 32,
								"offsetx": 10, "offsety": 5, "opacity": 0.8, "visible": true
							}
						]
					}
				]
			}
		]
	}`)

	loader := tilemap.TilesetLoader{
		LoadImage: func(p string) (core.ImageSource, error) {
			return core.ImageSource{URI: "asset:" + p}, nil
		},
	}
	want, err := tilemap.LoadTiled(tmjRaw, loader)
	if err != nil {
		t.Fatalf("LoadTiled infinite TMJ: %v", err)
	}

	buildInfiniteTMX := func(dataAttr, bodyA, bodyB string) []byte {
		return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<map orientation="orthogonal" width="0" height="0" tilewidth="16" tileheight="16" infinite="1">
 <tileset firstgid="1" name="t" tilewidth="16" tileheight="16" tilecount="4" columns="2">
  <image source="tiles.png" width="32" height="32"/>
 </tileset>
 <imagelayer name="sky" parallaxx="0.5" parallaxy="1" repeatx="1">
  <image source="sky.png" width="64" height="32"/>
 </imagelayer>
 <group name="world" offsetx="4" offsety="6" opacity="0.5">
  <group name="sub" offsetx="2" offsety="-2" opacity="0.5">
   <layer name="ground">
    <data%s>
     <chunk x="-2" y="-1" width="2" height="1">%s</chunk>
     <chunk x="0" y="0" width="2" height="1">%s</chunk>
    </data>
   </layer>
   <imagelayer name="fog" offsetx="10" offsety="5" opacity="0.8">
    <image source="fog.png" width="32" height="32"/>
   </imagelayer>
  </group>
 </group>
</map>`, dataAttr, bodyA, bodyB))
	}

	buildInfiniteTMJCompressed := func(comp string) []byte {
		compField := ""
		if comp != "" {
			compField = fmt.Sprintf(`"compression": %q, `, comp)
		}
		return []byte(fmt.Sprintf(`{
			"width": 0, "height": 0, "tilewidth": 16, "tileheight": 16,
			"orientation": "orthogonal",
			"infinite": true,
			"tilesets": [
				{"firstgid": 1, "name": "t", "image": "tiles.png", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 4}
			],
			"layers": [
				{
					"name": "sky", "type": "imagelayer", "image": "sky.png",
					"imagewidth": 64, "imageheight": 32,
					"parallaxx": 0.5, "parallaxy": 1, "repeatx": true
				},
				{
					"name": "world", "type": "group", "offsetx": 4, "offsety": 6, "opacity": 0.5,
					"layers": [
						{
							"name": "sub", "type": "group", "offsetx": 2, "offsety": -2, "opacity": 0.5,
							"layers": [
								{
									"name": "ground", "type": "tilelayer",
									"encoding": "base64", %s
									"chunks": [
										{"x": -2, "y": -1, "width": 2, "height": 1, "data": %q},
										{"x": 0, "y": 0, "width": 2, "height": 1, "data": %q}
									]
								},
								{
									"name": "fog", "type": "imagelayer", "image": "fog.png",
									"imagewidth": 32, "imageheight": 32,
									"offsetx": 10, "offsety": 5, "opacity": 0.8
								}
							]
						}
					]
				}
			]
		}`, compField, encodeTileData(t, chunkA, comp), encodeTileData(t, chunkB, comp)))
	}

	cases := []struct {
		name string
		data []byte
	}{
		{"TMX infinite base64+zlib", buildInfiniteTMX(` encoding="base64" compression="zlib"`, encodeTileData(t, chunkA, "zlib"), encodeTileData(t, chunkB, "zlib"))},
		{"TMX infinite base64+gzip", buildInfiniteTMX(` encoding="base64" compression="gzip"`, encodeTileData(t, chunkA, "gzip"), encodeTileData(t, chunkB, "gzip"))},
		{"TMX infinite base64 uncompressed", buildInfiniteTMX(` encoding="base64"`, encodeTileData(t, chunkA, ""), encodeTileData(t, chunkB, ""))},
		{"TMX infinite CSV", buildInfiniteTMX(` encoding="csv"`, "\n1,0\n", "\n0,2\n")},
		{"TMX infinite XML tiles", buildInfiniteTMX("", xmlTilesMarkup(chunkA), xmlTilesMarkup(chunkB))},
		{"TMJ infinite base64 uncompressed", buildInfiniteTMJCompressed("")},
		{"TMJ infinite base64+zlib", buildInfiniteTMJCompressed("zlib")},
		{"TMJ infinite base64+gzip", buildInfiniteTMJCompressed("gzip")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tilemap.LoadTiled(tc.data, loader)
			if err != nil {
				t.Fatalf("LoadTiled(%s): %v", tc.name, err)
			}
			assertEqualTilemaps(t, got, want)
		})
	}

	// Empty infinite compressed layer (no chunks) should also load cleanly.
	emptyInf := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<map orientation="hexagonal" staggeraxis="x" staggerindex="even" hexsidelength="18" width="0" height="0" tilewidth="32" tileheight="36" infinite="1">
 <properties>
  <property name="notes">Line 1
Line 2</property>
 </properties>
 <layer name="empty">
  <data encoding="base64" compression="zlib"/>
 </layer>
</map>`)
	hm, err := tilemap.LoadTiled(emptyInf, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("empty infinite hex TMX: %v", err)
	}
	if hm.Orientation != tilemap.OrientationHexagonal || hm.StaggerAxis != tilemap.StaggerX ||
		hm.StaggerIndex != tilemap.StaggerEven || hm.HexSideLength != 18 {
		t.Fatalf("hex metadata = %+v", hm)
	}
	if v, ok := hm.Properties.String("notes"); !ok || v != "Line 1\nLine 2" {
		t.Fatalf("multiline property = (%q, %v), want (%q, true)", v, ok, "Line 1\nLine 2")
	}
}

func TestLoadTiledTMXAndCompressedErrors(t *testing.T) {
	t.Run("malformed XML syntax", func(t *testing.T) {
		badXML := []byte("<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"2\" tilewidth=\"16\" tileheight=\"16\">\n  <layer name=\"a\">\n</map>\n")
		_, err := tilemap.LoadTiled(badXML, tilemap.TilesetLoader{})
		loaderrtest.Expect(t, loaderr.WithFile(err, "broken.tmx"), "broken.tmx", 4)
	})

	t.Run("wrong root element", func(t *testing.T) {
		xmlData := []byte("<?xml version=\"1.0\"?>\n<tileset name=\"t\"/>\n")
		_, err := tilemap.LoadTiled(xmlData, tilemap.TilesetLoader{})
		loaderrtest.Expect(t, loaderr.WithFile(err, "wrong.tmx"), "wrong.tmx", 2)
	})

	t.Run("zstd compression in TMX", func(t *testing.T) {
		xmlData := []byte("<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"2\" height=\"1\">\n  <data encoding=\"base64\" compression=\"zstd\">KLUv/QA=</data>\n </layer>\n</map>\n")
		_, err := tilemap.LoadTiled(xmlData, tilemap.TilesetLoader{})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "zstd.tmx"), "zstd.tmx", 4)
		if e.Field != "layers[0].compression" {
			t.Errorf("Field = %q, want layers[0].compression", e.Field)
		}
		if !strings.Contains(strings.ToLower(e.Problem), "zstd") || !strings.Contains(strings.ToLower(e.Fix), "zlib") {
			t.Errorf("expected zstd message and zlib fix hint, got: %v", e)
		}
	})

	t.Run("zstd compression in TMJ", func(t *testing.T) {
		data := loaderrtest.Indent(`{"width":2,"height":1,"tilewidth":16,"tileheight":16,"layers":[{"type":"tilelayer","name":"g","encoding":"base64","compression":"zstd","data":"KLUv/QA="}]}`)
		_, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
		e := loaderrtest.ExpectField(t, loaderr.WithFile(err, "zstd.tmj"), "zstd.tmj", data, "layers[0].compression")
		if !strings.Contains(strings.ToLower(e.Problem), "zstd") {
			t.Errorf("expected zstd problem, got: %v", e)
		}
	})

	t.Run("corrupted base64 in TMX", func(t *testing.T) {
		xmlData := []byte("<?xml version=\"1.0\"?>\n<map width=\"1\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"1\" height=\"1\">\n  <data encoding=\"base64\">!!!not-base64!!!</data>\n </layer>\n</map>\n")
		_, err := tilemap.LoadTiled(xmlData, tilemap.TilesetLoader{})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "badb64.tmx"), "badb64.tmx", 4)
		if e.Field != "layers[0].data" {
			t.Errorf("Field = %q, want layers[0].data", e.Field)
		}
	})

	t.Run("corrupted zlib in TMX", func(t *testing.T) {
		bad := base64.StdEncoding.EncodeToString([]byte{0x78, 0x9c, 0xff, 0xff, 0xff})
		xmlData := []byte("<?xml version=\"1.0\"?>\n<map width=\"1\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"1\" height=\"1\">\n  <data encoding=\"base64\" compression=\"zlib\">" + bad + "</data>\n </layer>\n</map>\n")
		_, err := tilemap.LoadTiled(xmlData, tilemap.TilesetLoader{})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "badzlib.tmx"), "badzlib.tmx", 4)
		if e.Field != "layers[0].data" {
			t.Errorf("Field = %q, want layers[0].data", e.Field)
		}
	})

	t.Run("corrupted gzip in TMJ", func(t *testing.T) {
		bad := base64.StdEncoding.EncodeToString([]byte{0x1f, 0x8b, 0x08, 0x00, 0xde, 0xad, 0xbe, 0xef})
		data := loaderrtest.Indent(fmt.Sprintf(`{"width":1,"height":1,"tilewidth":16,"tileheight":16,"layers":[{"type":"tilelayer","name":"g","encoding":"base64","compression":"gzip","data":%q}]}`, bad))
		_, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
		loaderrtest.ExpectField(t, loaderr.WithFile(err, "badgzip.tmj"), "badgzip.tmj", data, "layers[0].data")
	})

	t.Run("oversized zlib decompression bomb for small layer", func(t *testing.T) {
		// Compress 1 MiB of zeros and stuff it into a 2x2 layer (expects 16 bytes).
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		zeros := make([]byte, 1<<20)
		if _, err := w.Write(zeros); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		bomb := base64.StdEncoding.EncodeToString(buf.Bytes())
		xmlData := []byte("<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"2\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"2\" height=\"2\">\n  <data encoding=\"base64\" compression=\"zlib\">" + bomb + "</data>\n </layer>\n</map>\n")
		_, err := tilemap.LoadTiled(xmlData, tilemap.TilesetLoader{})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "bomb.tmx"), "bomb.tmx", 4)
		if e.Field != "layers[0].data" {
			t.Errorf("Field = %q, want layers[0].data", e.Field)
		}
	})

	t.Run("oversized gzip decompression bomb declaring huge dimensions", func(t *testing.T) {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(make([]byte, 1<<18)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		bomb := base64.StdEncoding.EncodeToString(buf.Bytes())
		xmlData := []byte("<?xml version=\"1.0\"?>\n<map width=\"50000\" height=\"50000\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"50000\" height=\"50000\">\n  <data encoding=\"base64\" compression=\"gzip\">" + bomb + "</data>\n </layer>\n</map>\n")
		_, err := tilemap.LoadTiled(xmlData, tilemap.TilesetLoader{})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "hugebomb.tmx"), "hugebomb.tmx", 4)
		if e.Field != "layers[0].data" {
			t.Errorf("Field = %q, want layers[0].data", e.Field)
		}
	})

	t.Run("widely separated infinite chunks load sparsely without dense allocation", func(t *testing.T) {
		oneTile := encodeTileData(t, []uint32{1}, "zlib")
		xmlData := []byte("<?xml version=\"1.0\"?>\n<map width=\"0\" height=\"0\" tilewidth=\"16\" tileheight=\"16\" infinite=\"1\">\n <layer name=\"g\">\n  <data encoding=\"base64\" compression=\"zlib\">\n   <chunk x=\"-100000\" y=\"-100000\" width=\"1\" height=\"1\">" + oneTile + "</chunk>\n   <chunk x=\"100000\" y=\"100000\" width=\"1\" height=\"1\">" + oneTile + "</chunk>\n  </data>\n </layer>\n</map>\n")
		m, err := tilemap.LoadTiled(xmlData, tilemap.TilesetLoader{})
		if err != nil {
			t.Fatalf("LoadTiled sparse infinite TMX failed: %v", err)
		}
		if len(m.Layers) != 1 || !m.Layers[0].IsChunked() {
			t.Fatalf("expected 1 chunked layer, got %d layers", len(m.Layers))
		}
		if got := m.Layers[0].ChunkCount(); got != 2 {
			t.Fatalf("ChunkCount() = %d, want 2", got)
		}
		if m.TileAt(0, -100000, -100000) != 1 || m.TileAt(0, 100000, 100000) != 1 {
			t.Fatalf("unexpected sparse tiles: (%d, %d)", m.TileAt(0, -100000, -100000), m.TileAt(0, 100000, 100000))
		}
	})

	t.Run("external TSX error names TSX file and line", func(t *testing.T) {
		tsx := []byte("<?xml version=\"1.0\"?>\n<tileset name=\"t\" tilewidth=\"16\" tileheight=\"16\" columns=\"1\">\n <tile id=\"0\">\n  <properties>\n   <property name=\"solid\" type=\"bool\" value=\"yes\"/>\n  </properties>\n </tile>\n</tileset>\n")
		tmx := []byte("<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <tileset firstgid=\"1\" source=\"tiles/cave.tsx\"/>\n</map>\n")
		_, err := tilemap.LoadTiled(tmx, tilemap.TilesetLoader{
			LoadTileset: func(string) ([]byte, error) { return tsx, nil },
		})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "cave.tmx"), "tiles/cave.tsx", 5)
		if e.Field != "tiles[0].properties[0].value" {
			t.Errorf("Field = %q, want tiles[0].properties[0].value", e.Field)
		}
	})

	t.Run("external TSX animation error from TMJ names TSX file and line", func(t *testing.T) {
		tsx := []byte("<?xml version=\"1.0\"?>\n<tileset name=\"t\" tilewidth=\"16\" tileheight=\"16\" columns=\"1\">\n <tile id=\"0\">\n  <animation>\n   <frame tileid=\"0\" duration=\"-5\"/>\n  </animation>\n </tile>\n</tileset>\n")
		tmj := loaderrtest.Indent(`{"width":2,"height":1,"tilewidth":16,"tileheight":16,"tilesets":[{"firstgid":1,"source":"tiles/anim.tsx"}]}`)
		_, err := tilemap.LoadTiled(tmj, tilemap.TilesetLoader{
			LoadJSON: func(string) ([]byte, error) { return tsx, nil },
		})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "cave.tmj"), "tiles/anim.tsx", 5)
		if e.Field != "tiles[0].animation[0].duration" {
			t.Errorf("Field = %q, want tiles[0].animation[0].duration", e.Field)
		}
	})

	t.Run("external TSJ error from TMX names TSJ file and line", func(t *testing.T) {
		tsj := loaderrtest.Indent(`{"name":"t","tiles":[{"id":0,"properties":[{"name":"n","type":"int","value":"bad"}]}]}`)
		tmx := []byte("<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <tileset firstgid=\"1\" source=\"tiles/cave.tsj\"/>\n</map>\n")
		_, err := tilemap.LoadTiled(tmx, tilemap.TilesetLoader{
			LoadTileset: func(string) ([]byte, error) { return tsj, nil },
		})
		loaderrtest.ExpectField(t, loaderr.WithFile(err, "cave.tmx"), "tiles/cave.tsj", tsj, "tiles[0].properties[0].value")
	})

	t.Run("TMX attribute and data validation errors report exact lines", func(t *testing.T) {
		cases := []struct {
			name  string
			xml   string
			line  int
			field string
		}{
			{
				name:  "orientation",
				xml:   "<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"1\" tilewidth=\"16\" tileheight=\"16\" orientation=\"diagonal\"/>\n",
				line:  2,
				field: "orientation",
			},
			{
				name:  "compression without base64",
				xml:   "<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"2\" height=\"1\">\n  <data encoding=\"csv\" compression=\"zlib\">1,2</data>\n </layer>\n</map>\n",
				line:  4,
				field: "layers[0].compression",
			},
			{
				name:  "bad csv token",
				xml:   "<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"2\" height=\"1\">\n  <data encoding=\"csv\">1,notanumber</data>\n </layer>\n</map>\n",
				line:  4,
				field: "layers[0].data",
			},
			{
				name:  "wrong tile count",
				xml:   "<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"2\" height=\"1\">\n  <data encoding=\"csv\">1</data>\n </layer>\n</map>\n",
				line:  4,
				field: "layers[0].data",
			},
			{
				name:  "malformed polygon points",
				xml:   "<?xml version=\"1.0\"?>\n<map width=\"2\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <objectgroup name=\"objs\">\n  <object id=\"1\" x=\"0\" y=\"0\">\n   <polygon points=\"0,0 broken\"/>\n  </object>\n </objectgroup>\n</map>\n",
				line:  5,
				field: "layers[0].objects[0].polygon",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := tilemap.LoadTiled([]byte(tc.xml), tilemap.TilesetLoader{})
				e := loaderrtest.Expect(t, loaderr.WithFile(err, "map.tmx"), "map.tmx", tc.line)
				if e.Field != tc.field {
					t.Errorf("Field = %q, want %q", e.Field, tc.field)
				}
			})
		}
	})
}
