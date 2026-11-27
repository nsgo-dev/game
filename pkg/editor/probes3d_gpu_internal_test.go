package editor

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/scenemodel"
)

func setupProbeParityProject(t *testing.T) (dir string, sceneFile string) {
	t.Helper()
	dir = t.TempDir()
	for _, sub := range []string{"materials", "scenes"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: Probe Parity\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mats := map[string]string{
		"red.material.yaml":    "version: 1\ntype: material\nbaseColor: '#ff2424'\nemissive: '#ff1818'\nroughness: 0.9\n",
		"blue.material.yaml":   "version: 1\ntype: material\nbaseColor: '#2458ff'\nemissive: '#1840ff'\nroughness: 0.9\n",
		"floor.material.yaml":  "version: 1\ntype: material\nbaseColor: '#4a4f5a'\nroughness: 0.85\n",
		"chrome.material.yaml": "version: 1\ntype: material\nbaseColor: '#ffffff'\nmetallic: 0.92\nroughness: 0.1\n",
		"matte.material.yaml":  "version: 1\ntype: material\nbaseColor: '#e8e8e8'\nmetallic: 0\nroughness: 0.95\n",
	}
	for name, body := range mats {
		if err := os.WriteFile(filepath.Join(dir, "materials", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	probeSz := [3]float32{8, 4, 8}
	gridCnt := [3]int{2, 2, 2}
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Probe Room",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#0a0c12",
		Environment: &sceneformat.EnvironmentDoc{
			Sky:              &sceneformat.SkyDoc{Kind: sceneformat.SkyColor, Color: "#0c1018"},
			AmbientColor:     "#000000",
			AmbientIntensity: sceneformat.Float32(0),
			Tonemapping:      sceneformat.TonemapACES,
			Exposure:         sceneformat.Float32(1.1),
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:   "cam_main",
					Name: "Main Camera",
					X:    0,
					Y:    1.4,
					PosZ: 4.4,
					RotX: -0.14,
					Camera: &sceneformat.Camera3DDoc{
						Projection: sceneformat.ProjectionPerspective,
						FOV:        sceneformat.Float32(55),
						Near:       sceneformat.Float32(0.1),
						Far:        sceneformat.Float32(40),
						Current:    true,
					},
				},
				{
					ID:       "floor",
					ScaleX:   sceneformat.Float32(10),
					ScaleZ:   sceneformat.Float32(10),
					Mesh:     sceneformat.MeshPlane,
					Material: "materials/floor.material.yaml",
				},
				{
					ID:       "red_wall",
					X:        3.0,
					Y:        1.5,
					PosZ:     0,
					ScaleX:   sceneformat.Float32(0.4),
					ScaleY:   sceneformat.Float32(3.5),
					ScaleZ:   sceneformat.Float32(6.0),
					Mesh:     sceneformat.MeshBox,
					Material: "materials/red.material.yaml",
				},
				{
					ID:       "blue_wall",
					X:        -3.0,
					Y:        1.5,
					PosZ:     0,
					ScaleX:   sceneformat.Float32(0.4),
					ScaleY:   sceneformat.Float32(3.5),
					ScaleZ:   sceneformat.Float32(6.0),
					Mesh:     sceneformat.MeshBox,
					Material: "materials/blue.material.yaml",
				},
				{
					ID:       "chrome_orb",
					X:        -0.8,
					Y:        0.9,
					PosZ:     0,
					Mesh:     sceneformat.MeshSphere,
					Material: "materials/chrome.material.yaml",
				},
				{
					ID:       "matte_orb",
					X:        0.8,
					Y:        0.9,
					PosZ:     0,
					Mesh:     sceneformat.MeshSphere,
					Material: "materials/matte.material.yaml",
				},
				{
					ID:   "refl_probe",
					X:    0,
					Y:    1.1,
					PosZ: 0,
					ReflectionProbe: &sceneformat.ReflectionProbeDoc{
						Size:          &probeSz,
						BlendDistance: 0.5,
						Intensity:     sceneformat.Float32(1.2),
						BoxProjection: sceneformat.Bool(true),
						Resolution:    16,
					},
				},
				{
					ID:   "light_grid",
					X:    0,
					Y:    1.1,
					PosZ: 0,
					LightProbeGrid: &sceneformat.LightProbeGridDoc{
						Size:      &probeSz,
						Counts:    &gridCnt,
						Intensity: sceneformat.Float32(1.2),
					},
				},
			},
		}},
	}
	yml, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	sceneFile = filepath.Join(dir, "scenes", "room.scene.yaml")
	if err := os.WriteFile(sceneFile, yml, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, sceneFile
}

// TestViewport3DProbesBakeParityAndRebakeAfterMovingWall verifies both issue #903 acceptance criteria:
//  1. Baking writes the reflection cubemap (.hdr) and light probe grid (.probes.yaml) files, and the
//     game's shading with them matches the editor 3D viewport pixel-for-pixel (checked against a golden).
//  2. Moving a colored wall and rebaking changes both baked files and the rendered pixels while
//     preserving exact editor/game parity.
func TestViewport3DProbesBakeParityAndRebakeAfterMovingWall(t *testing.T) {
	dir, sceneFile := setupProbeParityProject(t)

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.ProjectPath = filepath.Join(dir, "game.yaml")
	ed.ScenePath = "scenes/room.scene.yaml"
	if err := ed.OpenFile(sceneFile); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	ed.SetViewMode(ViewMode3D)
	if err := ed.SetLookThroughCamera3D(true); err != nil {
		t.Fatalf("SetLookThroughCamera3D: %v", err)
	}

	v := newSoftwareViewport(t, ed)
	drew, unbakedImg := v.frame()
	if !drew {
		t.Fatal("unbaked editor viewport recorded no frame")
	}

	// Bake all probes in the scene.
	count, err := ed.BakeAllProbes3D()
	if err != nil {
		t.Fatalf("BakeAllProbes3D: %v", err)
	}
	if count != 2 {
		t.Fatalf("BakeAllProbes3D baked %d probes, want 2", count)
	}

	rpObj, _, _ := ed.FindObject("refl_probe")
	lpObj, _, _ := ed.FindObject("light_grid")
	if rpObj == nil || rpObj.ReflectionProbe == nil || rpObj.ReflectionProbe.Cubemap == "" {
		t.Fatalf("reflection probe cubemap path was not linked after bake")
	}
	if lpObj == nil || lpObj.LightProbeGrid == nil || lpObj.LightProbeGrid.Data == "" {
		t.Fatalf("light probe grid data path was not linked after bake")
	}

	hdrPath := filepath.Join(dir, filepath.FromSlash(rpObj.ReflectionProbe.Cubemap))
	yamlPath := filepath.Join(dir, filepath.FromSlash(lpObj.LightProbeGrid.Data))
	hdrBytes1, err := os.ReadFile(hdrPath)
	if err != nil {
		t.Fatalf("read baked cubemap %s: %v", hdrPath, err)
	}
	if cube, err := render3d.DecodeCubemapHDR(bytes.NewReader(hdrBytes1)); err != nil {
		t.Fatalf("DecodeCubemapHDR: %v", err)
	} else if cube.Size != 16 {
		t.Fatalf("baked cubemap size = %d, want 16", cube.Size)
	}
	yamlBytes1, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read baked light probes %s: %v", yamlPath, err)
	}
	if doc, err := render3d.ParseLightProbeGrid(yamlBytes1); err != nil {
		t.Fatalf("ParseLightProbeGrid: %v", err)
	} else if len(doc.Probes) != 8 {
		t.Fatalf("baked light probe count = %d, want 8", len(doc.Probes))
	}

	// Render the baked scene in the editor viewport and compare against the unbaked frame.
	drew, editorImg := v.frame()
	if !drew {
		t.Fatal("baked editor viewport recorded no frame")
	}
	if samePixels(unbakedImg, editorImg) {
		t.Fatal("baking probes did not change viewport shading")
	}

	// Save the scene YAML, reload it from scratch, and render the game's frame via scenemodel + render3d.
	savedYAML, err := sceneformat.Encode(ed.Doc)
	if err != nil {
		t.Fatalf("Encode saved scene: %v", err)
	}
	parsedDoc, err := sceneformat.Parse(savedYAML)
	if err != nil {
		t.Fatalf("Parse saved scene: %v", err)
	}
	gameImg := renderGameFirstFrame(t, parsedDoc, scenemodel.NewAssets(os.DirFS(dir)), gpuViewWidth, gpuViewHeight)

	// 1. Exact pixel-for-pixel parity between game and editor.
	if !samePixels(gameImg, editorImg) {
		t.Fatal("game frame with baked reflection and light probes does not match editor viewport pixel-for-pixel")
	}

	// 2. Compare against golden reference (UPDATE_GOLDEN=1 writes it).
	assertViewportReference(t, gametest.GoldenPath("", "viewport3d_probes"), editorImg)

	// 3. Move the red wall close behind the spheres and rebake; baked files and rendered image must change.
	if err := ed.MoveObject3D("red_wall", 0.0, 1.5, -2.0); err != nil {
		t.Fatalf("MoveObject3D red_wall: %v", err)
	}
	if _, err := ed.BakeAllProbes3D(); err != nil {
		t.Fatalf("rebake BakeAllProbes3D: %v", err)
	}
	hdrBytes2, err := os.ReadFile(hdrPath)
	if err != nil {
		t.Fatalf("read rebaked cubemap: %v", err)
	}
	yamlBytes2, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read rebaked light probes: %v", err)
	}
	if bytes.Equal(hdrBytes1, hdrBytes2) {
		t.Fatal("rebaking after moving red_wall did not change baked reflection probe .hdr bytes")
	}
	if bytes.Equal(yamlBytes1, yamlBytes2) {
		t.Fatal("rebaking after moving red_wall did not change baked light probe grid .probes.yaml bytes")
	}

	drew, rebakedEditorImg := v.frame()
	if !drew {
		t.Fatal("rebaked editor viewport recorded no frame")
	}
	if samePixels(editorImg, rebakedEditorImg) {
		t.Fatal("rebaking after moving red_wall did not change rendered viewport pixels")
	}

	// Verify game still matches editor pixel-for-pixel after rebake.
	savedYAML2, err := sceneformat.Encode(ed.Doc)
	if err != nil {
		t.Fatalf("Encode rebaked scene: %v", err)
	}
	parsedDoc2, err := sceneformat.Parse(savedYAML2)
	if err != nil {
		t.Fatalf("Parse rebaked scene: %v", err)
	}
	rebakedGameImg := renderGameFirstFrame(t, parsedDoc2, scenemodel.NewAssets(os.DirFS(dir)), gpuViewWidth, gpuViewHeight)
	if !samePixels(rebakedGameImg, rebakedEditorImg) {
		t.Fatal("game frame after rebake does not match editor viewport pixel-for-pixel")
	}
}
