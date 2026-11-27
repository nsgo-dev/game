package editorcmd_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type viewport3DScreenshotResolver struct {
	sceneformat.AtlasMap
	assets *scenemodel.Assets
}

func (r viewport3DScreenshotResolver) Material(path string) (*resource.Material, error) {
	return r.assets.Material(path)
}

type glbPartBox struct {
	cx, cy, cz float32
	w, h, d    float32
	matIndex   int
}

type glbMat struct {
	name      string
	color     [4]float32
	roughness float32
	metallic  float32
}

// buildBoxesGLB constructs a minimal valid glTF 2.0 binary (.glb) containing one mesh
// with one primitive per material group of axis-aligned boxes.
func buildBoxesGLB(t *testing.T, boxes []glbPartBox, mats []glbMat) []byte {
	t.Helper()
	type primData struct {
		positions [][3]float32
		normals   [][3]float32
		indices   []uint16
	}
	byMat := make([]primData, len(mats))
	addBox := func(p *primData, b glbPartBox) {
		x0, x1 := b.cx-b.w/2, b.cx+b.w/2
		y0, y1 := b.cy-b.h/2, b.cy+b.h/2
		z0, z1 := b.cz-b.d/2, b.cz+b.d/2
		faces := []struct {
			n [3]float32
			v [4][3]float32
		}{
			{[3]float32{0, 1, 0}, [4][3]float32{{x0, y1, z1}, {x1, y1, z1}, {x1, y1, z0}, {x0, y1, z0}}},
			{[3]float32{0, -1, 0}, [4][3]float32{{x0, y0, z0}, {x1, y0, z0}, {x1, y0, z1}, {x0, y0, z1}}},
			{[3]float32{1, 0, 0}, [4][3]float32{{x1, y0, z1}, {x1, y0, z0}, {x1, y1, z0}, {x1, y1, z1}}},
			{[3]float32{-1, 0, 0}, [4][3]float32{{x0, y0, z0}, {x0, y0, z1}, {x0, y1, z1}, {x0, y1, z0}}},
			{[3]float32{0, 0, 1}, [4][3]float32{{x0, y0, z1}, {x1, y0, z1}, {x1, y1, z1}, {x0, y1, z1}}},
			{[3]float32{0, 0, -1}, [4][3]float32{{x1, y0, z0}, {x0, y0, z0}, {x0, y1, z0}, {x1, y1, z0}}},
		}
		for _, f := range faces {
			base := uint16(len(p.positions))
			for _, v := range f.v {
				p.positions = append(p.positions, v)
				p.normals = append(p.normals, f.n)
			}
			p.indices = append(p.indices, base, base+1, base+2, base, base+2, base+3)
		}
	}
	for _, b := range boxes {
		addBox(&byMat[b.matIndex], b)
	}

	var bin bytes.Buffer
	align4 := func() {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
	}
	var bufferViews []map[string]any
	var accessors []map[string]any
	var primitives []map[string]any

	for mi, p := range byMat {
		if len(p.indices) == 0 {
			continue
		}
		// Positions
		align4()
		posOff := bin.Len()
		minV := [3]float32{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))}
		maxV := [3]float32{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))}
		for _, v := range p.positions {
			for k := range 3 {
				if v[k] < minV[k] {
					minV[k] = v[k]
				}
				if v[k] > maxV[k] {
					maxV[k] = v[k]
				}
			}
			_ = binary.Write(&bin, binary.LittleEndian, v)
		}
		posBV := len(bufferViews)
		bufferViews = append(bufferViews, map[string]any{
			"buffer":     0,
			"byteOffset": posOff,
			"byteLength": bin.Len() - posOff,
			"target":     34962,
		})
		posAcc := len(accessors)
		accessors = append(accessors, map[string]any{
			"bufferView":    posBV,
			"componentType": 5126,
			"count":         len(p.positions),
			"type":          "VEC3",
			"min":           []float32{minV[0], minV[1], minV[2]},
			"max":           []float32{maxV[0], maxV[1], maxV[2]},
		})

		// Normals
		align4()
		nrmOff := bin.Len()
		for _, n := range p.normals {
			_ = binary.Write(&bin, binary.LittleEndian, n)
		}
		nrmBV := len(bufferViews)
		bufferViews = append(bufferViews, map[string]any{
			"buffer":     0,
			"byteOffset": nrmOff,
			"byteLength": bin.Len() - nrmOff,
			"target":     34962,
		})
		nrmAcc := len(accessors)
		accessors = append(accessors, map[string]any{
			"bufferView":    nrmBV,
			"componentType": 5126,
			"count":         len(p.normals),
			"type":          "VEC3",
		})

		// Indices
		align4()
		idxOff := bin.Len()
		for _, ix := range p.indices {
			_ = binary.Write(&bin, binary.LittleEndian, ix)
		}
		idxBV := len(bufferViews)
		bufferViews = append(bufferViews, map[string]any{
			"buffer":     0,
			"byteOffset": idxOff,
			"byteLength": bin.Len() - idxOff,
			"target":     34963,
		})
		idxAcc := len(accessors)
		accessors = append(accessors, map[string]any{
			"bufferView":    idxBV,
			"componentType": 5123,
			"count":         len(p.indices),
			"type":          "SCALAR",
		})

		primitives = append(primitives, map[string]any{
			"attributes": map[string]any{"POSITION": posAcc, "NORMAL": nrmAcc},
			"indices":    idxAcc,
			"material":   mi,
		})
	}
	align4()

	gltfMats := make([]map[string]any, len(mats))
	for i, m := range mats {
		gltfMats[i] = map[string]any{
			"name": m.name,
			"pbrMetallicRoughness": map[string]any{
				"baseColorFactor": []float32{m.color[0], m.color[1], m.color[2], m.color[3]},
				"metallicFactor":  m.metallic,
				"roughnessFactor": m.roughness,
			},
		}
	}
	doc := map[string]any{
		"asset":       map[string]any{"version": "2.0"},
		"scene":       0,
		"scenes":      []map[string]any{{"nodes": []int{0}}},
		"nodes":       []map[string]any{{"mesh": 0}},
		"meshes":      []map[string]any{{"primitives": primitives}},
		"materials":   gltfMats,
		"accessors":   accessors,
		"bufferViews": bufferViews,
		"buffers":     []map[string]any{{"byteLength": bin.Len()}},
	}
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, ' ')
	}
	binBytes := bin.Bytes()
	totalLen := 12 + 8 + len(jsonBytes) + 8 + len(binBytes)
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x46546C67)) // "glTF"
	_ = binary.Write(&out, binary.LittleEndian, uint32(2))
	_ = binary.Write(&out, binary.LittleEndian, uint32(totalLen))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(jsonBytes)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x4E4F534A)) // "JSON"
	out.Write(jsonBytes)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(binBytes)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x004E4942)) // "BIN\0"
	out.Write(binBytes)
	return out.Bytes()
}

// TestViewport3DModelsMacOSScreenshots renders the 3D viewport with the bottom MODELS strip
// and placed-model / group inspector cards on the real native macOS host beside Viewport3D.dc.html.
func TestViewport3DModelsMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	stone := glbMat{name: "Stone", color: [4]float32{0.58, 0.61, 0.66, 1}, roughness: 0.8, metallic: 0}
	water := glbMat{name: "Water", color: [4]float32{0.25, 0.50, 0.62, 1}, roughness: 0.15, metallic: 0.1}
	wood := glbMat{name: "Wood", color: [4]float32{0.56, 0.45, 0.34, 1}, roughness: 0.75, metallic: 0}
	iron := glbMat{name: "Iron", color: [4]float32{0.28, 0.31, 0.36, 1}, roughness: 0.5, metallic: 0.7}
	glow := glbMat{name: "LanternGlow", color: [4]float32{0.92, 0.78, 0.42, 1}, roughness: 0.3, metallic: 0}
	foliage := glbMat{name: "Hedge", color: [4]float32{0.36, 0.58, 0.40, 1}, roughness: 0.85, metallic: 0}

	models := map[string][]byte{
		"fountain.glb": buildBoxesGLB(t, []glbPartBox{
			{0, 0.3, 0, 2.2, 0.6, 2.2, 0},
			{0, 0.62, 0, 1.8, 0.06, 1.8, 1},
			{0, 1.2, 0, 0.45, 1.1, 0.45, 0},
			{0, 1.82, 0, 1.0, 0.18, 1.0, 0},
			{0, 2.02, 0, 0.22, 0.25, 0.22, 0},
		}, []glbMat{stone, water}),
		"wall_low.glb": buildBoxesGLB(t, []glbPartBox{
			{0, 0.5, 0, 2.4, 1.0, 0.5, 0},
		}, []glbMat{stone}),
		"bench.glb": buildBoxesGLB(t, []glbPartBox{
			{0, 0.42, 0, 1.8, 0.12, 0.55, 0},
			{-0.7, 0.2, 0, 0.14, 0.4, 0.48, 0},
			{0.7, 0.2, 0, 0.14, 0.4, 0.48, 0},
		}, []glbMat{wood}),
		"lantern.glb": buildBoxesGLB(t, []glbPartBox{
			{0, 0.9, 0, 0.18, 1.8, 0.18, 0},
			{0, 2.0, 0, 0.42, 0.42, 0.42, 1},
		}, []glbMat{iron, glow}),
		"hedge.glb": buildBoxesGLB(t, []glbPartBox{
			{0, 0.45, 0, 1.8, 0.9, 0.8, 0},
		}, []glbMat{foliage}),
		"gate_arch.glb": buildBoxesGLB(t, []glbPartBox{
			{-1.1, 1.0, 0, 0.55, 2.0, 0.55, 0},
			{1.1, 1.0, 0, 0.55, 2.0, 0.55, 0},
			{0, 2.15, 0, 2.9, 0.35, 0.6, 0},
		}, []glbMat{stone}),
	}
	for name, data := range models {
		if err := os.WriteFile(filepath.Join(dir, "models", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	stoneMat := "type: material\nversion: 1\nname: Stone\nbaseColor: '#8a919c'\nroughness: 0.8\nmetallic: 0\n"
	if err := os.WriteFile(filepath.Join(dir, "materials", "stone.material.yaml"), []byte(stoneMat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	sceneYAML := `version: 2
name: courtyard
layers:
  - name: level
    objects:
      - id: ground
        name: Ground
        scaleX: 14
        scaleZ: 14
        mesh: plane
        material: materials/stone.material.yaml
      - id: fountain
        name: Fountain
        x: 0
        "y": 0
        posZ: 0
        model: models/fountain.glb
        material: materials/stone.material.yaml
        components:
          body3d:
            version: 1
            kind: static
          collider3d:
            version: 1
            shape: convex
      - id: gate
        name: Gate
        x: 0
        "y": 0
        posZ: 4.5
        children:
          - id: pillar_l
            x: -1.4
            "y": 1.1
            scaleX: 0.6
            scaleY: 2.2
            scaleZ: 0.6
            mesh: box
            material: materials/stone.material.yaml
          - id: pillar_r
            x: 1.4
            "y": 1.1
            scaleX: 0.6
            scaleY: 2.2
            scaleZ: 0.6
            mesh: box
            material: materials/stone.material.yaml
          - id: lintel
            "y": 2.35
            scaleX: 3.6
            scaleY: 0.35
            scaleZ: 0.6
            mesh: box
            material: materials/stone.material.yaml
      - id: benches
        name: Benches
        children:
          - id: bench_01
            x: -2.6
            "y": 0
            posZ: -2.6
            model: models/bench.glb
          - id: bench_02
            x: 2.6
            "y": 0
            posZ: -2.6
            model: models/bench.glb
      - id: lanterns
        name: Lanterns
        children:
          - id: lantern_01
            x: -4.2
            "y": 0
            posZ: -4.2
            model: models/lantern.glb
          - id: lantern_02
            x: 4.2
            "y": 0
            posZ: -4.2
            model: models/lantern.glb
`
	if err := os.WriteFile(filepath.Join(dir, "scenes", "courtyard.scene.yaml"), []byte(sceneYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	gameYAML := "version: 1\nname: dungeon-run\nid: dev.dungeonrun.game\nappVersion: 0.4.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: courtyard\n  scenes:\n    - name: courtyard\n      path: scenes/courtyard.scene.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, viewport3DScreenshotResolver{assets: scenemodel.NewAssets(os.DirFS(dir))})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(sceneformat.PhysicsComponentRegistry()); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetAppearance(editor.AppearanceDark)
	ed.SetLanguage(editor.LanguageEnglish)
	ed.SetViewMode(editor.ViewMode3D)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0.8, 0},
		Yaw:      35 * math.Pi / 180,
		Pitch:    28 * math.Pi / 180,
		Distance: 10.5,
		FOV:      float32(math.Pi / 4),
		Near:     0.1,
		Far:      80,
	}
	ed.SelectObject("fountain")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	ed.WaitModelThumbnails()
	app.Settle()

	// 1. Placed model selected: bottom MODELS strip + MODEL, MATERIAL, COLLIDER, and Ask to add a component…
	app.Screenshot("viewport3d-model-inspector")

	// 2. Group selected: CHILDREN card in the 3D inspector.
	app.Post(func() { ed.SelectObject("gate") })
	app.Settle()
	app.Screenshot("viewport3d-group-inspector")

	// 3. "Or ask for one" inline agent prompt open over the MODELS strip.
	app.Press("id:viewport3d-ask-for-model")
	app.Settle()
	app.Screenshot("viewport3d-ask-model")
}
