package mcp_test

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	atlasV1 = `{
	"frames": {
		"idle.png": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}},
		"run.png":  {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}}
	},
	"meta": {"scale": "1"}
}`
	atlasV2 = `{
	"frames": {
		"idle.png": {"frame": {"x": 0, "y": 0, "w": 24, "h": 32}, "sourceSize": {"w": 24, "h": 32}},
		"run.png":  {"frame": {"x": 24, "y": 0, "w": 24, "h": 32}, "sourceSize": {"w": 24, "h": 32}},
		"jump.png": {"frame": {"x": 48, "y": 0, "w": 24, "h": 32}, "sourceSize": {"w": 24, "h": 32}}
	},
	"meta": {"scale": "1"}
}`
	atlasBroken = `{"frames": [`

	validWGSLV1 = `
@vertex fn vs_main(@builtin(vertex_index) idx: u32) -> @builtin(position) vec4f {
	return vec4f(0.0, 0.0, 0.0, 1.0);
}
@fragment fn fs_main() -> @location(0) vec4f {
	return vec4f(1.0, 0.0, 0.0, 1.0);
}
`
	validWGSLV2 = `
@vertex fn vs_main(@builtin(vertex_index) idx: u32) -> @builtin(position) vec4f {
	return vec4f(0.0, 0.0, 0.0, 1.0);
}
@fragment fn fs_main() -> @location(0) vec4f {
	return vec4f(0.0, 1.0, 0.0, 1.0);
}
`
	brokenWGSL = `@vertex fn vs_main( -> broken`

	validMaterialV1 = `version: 1
name: hero_mat
shader: shaders/hero.wgsl
blend: opaque
scalars:
  roughness: 0.4
`
	validMaterialV2 = `version: 1
name: hero_mat
shader: shaders/hero.wgsl
blend: alpha
scalars:
  roughness: 0.85
`
	brokenMaterial = `version: 99
unknownField: true
`
	validResourceV1 = `type: curve
version: 1
keys:
  - time: 0
    value: 0
  - time: 1
    value: 1
`
	validResourceV2 = `type: curve
version: 1
keys:
  - time: 0
    value: 0
  - time: 1
    value: 2.5
`
)

func writeDiskFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

// TestAssetsListAndAtlasHotReload verifies listing asset groups/memory and reloading an on-disk sprite atlas through MCP updates the live running game in place.
func TestAssetsListAndAtlasHotReload(t *testing.T) {
	dir := t.TempDir()
	writeDiskFile(t, dir, "sprites/hero.json", atlasV1)
	writeDiskFile(t, dir, "sprites/hero.png", "png-bytes-v1")
	writeDiskFile(t, dir, "data/config.txt", "difficulty=normal")
	writeDiskFile(t, dir, "data/table.txt", "level-1-data")

	var (
		cx        *core.Scope
		mgr       *assets.Manager
		liveAtlas *sprite.Atlas
		draws     int
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		mgr = assets.NewManager(cx, assets.WithFS(os.DirFS(dir)))

		// Add groups out of alphabetical order to verify deterministic sorting.
		mgr.Group("level1").
			AddAtlas("hero", "asset:sprites/hero.json", core.ImageSource{
				URI:  "asset:sprites/hero.png",
				Size: core.Size{Width: 32, Height: 16},
			}).
			AddImage("bg", core.ImageSource{
				URI:    "asset:images/bg.png",
				Pixels: image.NewRGBA(image.Rect(0, 0, 16, 16)),
			}).
			AddSound("jump", audio.Source{URI: "asset:sounds/jump.wav"}).
			AddBytes("config", "asset:data/config.txt").
			AddCustom("table", "asset:data/table.txt", func(b []byte) (any, error) {
				return strings.ToUpper(string(b)), nil
			}, nil)

		mgr.Group("boss").
			AddBytes("boss_script", "asset:data/table.txt")

		mgr.LoadGroup("level1", func(err error) {
			if err != nil {
				t.Errorf("LoadGroup(level1): %v", err)
			}
		})

		if _, err := mcp.Attach(cx, mcp.Config{Assets: mgr}); err != nil {
			t.Fatal(err)
		}
		return core.Canvas{
			Style: core.Style{Width: core.Pt(64), Height: core.Pt(64)},
			Draw: func(*core.Drawing, core.Size) {
				_ = mgr.Revision()
				draws++
			},
		}
	},
		nsgotest.Environment(core.Environment{Audio: true}),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{
				"asset:sounds/jump.wav": 200 * time.Millisecond,
			}
			a.ImageSizes = map[string]core.Size{
				"asset:sprites/hero.png": {Width: 32, Height: 16},
			}
		},
	)
	app.WaitUntil(time.Second, func() bool { return mgr.Group("level1").Done() })

	// Grab the live *sprite.Atlas pointer held by the running game.
	app.Post(func() {
		var ok bool
		liveAtlas, ok = mgr.Atlas("hero")
		if !ok || liveAtlas == nil {
			t.Fatal("hero atlas not loaded")
		}
	})
	app.Settle()

	if f := liveAtlas.Frames["idle.png"]; f.W != 16 || f.H != 16 {
		t.Fatalf("initial idle frame = %+v, want 16x16", f)
	}

	// 1. List asset groups: sorted deterministically ("boss", "level1") with accurate states & memory.
	listRes := callTool[mcp.AssetsListResult](t, app, cx, mcp.ToolAssetsList, nil)
	if listRes.GroupCount != 2 || len(listRes.Groups) != 2 {
		t.Fatalf("groups = %+v, want 2", listRes)
	}
	if listRes.Groups[0].Name != "boss" || listRes.Groups[0].State != assets.StateUnloaded || listRes.Groups[0].Bytes != 0 {
		t.Fatalf("groups[0] = %+v, want unloaded boss group with 0 bytes", listRes.Groups[0])
	}
	lvl := listRes.Groups[1]
	if lvl.Name != "level1" || lvl.State != assets.StateLoaded || lvl.Loaded != 5 || lvl.Total != 5 {
		t.Fatalf("groups[1] = %+v, want loaded level1 (5/5)", lvl)
	}
	if lvl.Bytes <= 0 || listRes.TotalBytes != lvl.Bytes {
		t.Fatalf("memory bytes = group:%d total:%d, want >0 and equal", lvl.Bytes, listRes.TotalBytes)
	}
	// Verify assets within level1 are sorted by key and have accurate kinds & bytes.
	wantKeys := []string{"bg", "config", "hero", "jump", "table"}
	for i, wantKey := range wantKeys {
		if lvl.Assets[i].Key != wantKey || !lvl.Assets[i].Loaded || lvl.Assets[i].Bytes <= 0 {
			t.Fatalf("level1.Assets[%d] = %+v, want loaded key=%q bytes>0", i, lvl.Assets[i], wantKey)
		}
	}
	// bg has a 16x16 RGBA pixel buffer => exactly 1024 bytes.
	if lvl.Assets[0].Bytes != 16*16*4 {
		t.Fatalf("bg bytes = %d, want %d", lvl.Assets[0].Bytes, 16*16*4)
	}
	// config is raw bytes "difficulty=normal" => 17 bytes.
	if lvl.Assets[1].Bytes != int64(len("difficulty=normal")) {
		t.Fatalf("config bytes = %d, want %d", lvl.Assets[1].Bytes, len("difficulty=normal"))
	}
	// hero atlas has 2 frames initially.
	if lvl.Assets[2].Kind != "atlas" || lvl.Assets[2].Frames != 2 {
		t.Fatalf("hero entry = %+v, want kind=atlas frames=2", lvl.Assets[2])
	}

	// Filter by state and kind.
	loadedOnly := callTool[mcp.AssetsListResult](t, app, cx, mcp.ToolAssetsList, map[string]any{
		"state": "loaded",
		"kind":  "atlas",
	})
	if loadedOnly.GroupCount != 1 || loadedOnly.AssetCount != 1 || loadedOnly.Groups[0].Assets[0].Key != "hero" {
		t.Fatalf("filtered list = %+v", loadedOnly)
	}

	// 2. Edit sprites/hero.json on disk and reload through MCP -> live *sprite.Atlas updates in place!
	writeDiskFile(t, dir, "sprites/hero.json", atlasV2)
	drawsBefore := draws

	reloadRes := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{
		"key": "hero",
	})
	if !reloadRes.Reloaded || reloadRes.Error != "" || reloadRes.Key != "hero" || reloadRes.Group != "level1" || reloadRes.Kind != "atlas" {
		t.Fatalf("reload hero = %+v", reloadRes)
	}
	if reloadRes.Bytes <= 0 || reloadRes.DurationMS < 0 || reloadRes.Revision != 1 {
		t.Fatalf("reload hero metrics = %+v", reloadRes)
	}
	// The exact live *sprite.Atlas pointer held by the game now has the new 24x32 frames + jump.png!
	if len(liveAtlas.Frames) != 3 {
		t.Fatalf("liveAtlas frame count = %d, want 3", len(liveAtlas.Frames))
	}
	if f := liveAtlas.Frames["idle.png"]; f.W != 24 || f.H != 32 {
		t.Fatalf("liveAtlas idle frame = %+v, want 24x32", f)
	}
	if f, ok := liveAtlas.Frames["jump.png"]; !ok || f.X != 48 || f.W != 24 || f.H != 32 {
		t.Fatalf("liveAtlas jump frame = (%+v, %v), want x=48 24x32", f, ok)
	}
	// Canvas reading mgr.Revision() redrew automatically!
	if draws <= drawsBefore {
		t.Fatalf("expected canvas to redraw after MCP atlas reload (before=%d after=%d)", drawsBefore, draws)
	}

	// 3. Reload atlas image specifically by relative path and verify image bytes updated on liveAtlas.
	writeDiskFile(t, dir, "sprites/hero.png", "png-bytes-v2-updated")
	imgReload := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{
		"key": "sprites/hero.png",
	})
	if !imgReload.Reloaded || imgReload.Error != "" {
		t.Fatalf("reload hero.png = %+v", imgReload)
	}
	if string(liveAtlas.Image.Data) != "png-bytes-v2-updated" {
		t.Fatalf("liveAtlas.Image.Data = %q, want %q", liveAtlas.Image.Data, "png-bytes-v2-updated")
	}

	// 4. Write broken JSON to disk and reload through MCP: returns structured error, keeps liveAtlas intact!
	writeDiskFile(t, dir, "sprites/hero.json", atlasBroken)
	brokenRes := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{
		"key": "asset:sprites/hero.json",
	})
	if brokenRes.Reloaded || brokenRes.Error == "" || !strings.Contains(brokenRes.Error, "hero") {
		t.Fatalf("broken reload = %+v, want reloaded=false with error", brokenRes)
	}
	// Live atlas kept its previous working 3 frames!
	if len(liveAtlas.Frames) != 3 || liveAtlas.Frames["idle.png"].W != 24 {
		t.Fatalf("broken reload corrupted liveAtlas: %+v", liveAtlas.Frames)
	}

	// 5. Fix JSON on disk and update data/config.txt & data/table.txt, then call game_assets_reload_group!
	writeDiskFile(t, dir, "sprites/hero.json", atlasV1)
	writeDiskFile(t, dir, "data/config.txt", "difficulty=hardcore")
	writeDiskFile(t, dir, "data/table.txt", "level-1-remastered")

	groupRes := callTool[mcp.AssetsReloadGroupResult](t, app, cx, mcp.ToolAssetsReloadGroup, map[string]any{
		"group": "level1",
	})
	// "bg" names asset:images/bg.png, which is not in dir, and "jump" is a sound, so both are skipped rather than failed while hero, config and table reload.
	if groupRes.Group != "level1" || groupRes.Count != 3 || groupRes.Failed != 0 || groupRes.Skipped != 2 || !groupRes.Reloaded {
		t.Fatalf("group reload (with missing bg file) = %+v", groupRes)
	}
	skipped := map[string]string{}
	for _, it := range groupRes.Items {
		if it.Skipped != "" {
			skipped[it.Key] = it.Skipped
		}
	}
	if skipped["bg"] != assets.SkippedNoFile || skipped["jump"] != assets.SkippedSound {
		t.Fatalf("group reload skipped = %v", skipped)
	}

	// Reloading the missing image by key reports the read error, and reloading the sound by key reports it as skipped, not reloaded.
	bgRes := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "bg"})
	if bgRes.Reloaded || bgRes.Error == "" {
		t.Fatalf("reload missing bg = %+v, want an error", bgRes)
	}
	jumpRes := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "jump"})
	if jumpRes.Reloaded || jumpRes.Error != "" || len(jumpRes.Items) != 1 || jumpRes.Items[0].Skipped != assets.SkippedSound {
		t.Fatalf("reload jump = %+v, want skipped without error", jumpRes)
	}
	if len(liveAtlas.Frames) != 2 || liveAtlas.Frames["idle.png"].W != 16 {
		t.Fatalf("liveAtlas after group reload = %+v, want V1 (2 frames, 16x16)", liveAtlas.Frames)
	}
	var (
		gotConfig []byte
		gotTable  any
	)
	app.Post(func() {
		gotConfig, _ = mgr.Bytes("config")
		gotTable, _ = mgr.Get("table")
	})
	app.Settle()
	if string(gotConfig) != "difficulty=hardcore" {
		t.Fatalf("config after group reload = %q", gotConfig)
	}
	if gotTable != "LEVEL-1-REMASTERED" {
		t.Fatalf("custom table after group reload = %v, want LEVEL-1-REMASTERED", gotTable)
	}

	// Provide images/bg.png on disk and verify the group reload now replaces bg too.
	writeDiskFile(t, dir, "images/bg.png", "bg-png-v2")
	cleanGroupRes := callTool[mcp.AssetsReloadGroupResult](t, app, cx, mcp.ToolAssetsReloadGroup, map[string]any{
		"group": "level1",
	})
	if !cleanGroupRes.Reloaded || cleanGroupRes.Failed != 0 || cleanGroupRes.Count != 4 || cleanGroupRes.Skipped != 1 || cleanGroupRes.Error != "" {
		t.Fatalf("clean group reload = %+v", cleanGroupRes)
	}
}

// TestShadersMaterialsResourcesAndCustomAtlasReload verifies MCP listing and reloading across hotreload.ShaderReloader, hotreload.MaterialReloader, resource.Library, standalone RegisterAtlas, and Config.ReloadAsset fallback hook.
func TestShadersMaterialsResourcesAndCustomAtlasReload(t *testing.T) {
	dir := t.TempDir()
	writeDiskFile(t, dir, "shaders/hero.wgsl", validWGSLV1)
	writeDiskFile(t, dir, "materials/hero.material.yaml", validMaterialV1)
	writeDiskFile(t, dir, "resources/ease.yaml", validResourceV1)
	writeDiskFile(t, dir, "atlases/ui.json", atlasV1)

	fsys := os.DirFS(dir)
	resLib := resource.NewLibrary(fsys)
	loadedCurve, err := resource.LoadAs[*resource.Curve](resLib, "resources/ease.yaml")
	if err != nil {
		t.Fatal(err)
	}

	standaloneAtlas, err := sprite.LoadAtlas(core.Src("asset:atlases/ui.png"), []byte(atlasV1))
	if err != nil {
		t.Fatal(err)
	}

	var (
		cx       *core.Scope
		shaders  *hotreload.ShaderReloader
		mats     *hotreload.MaterialReloader
		hookKeys []string
		hookErr  error
	)

	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		shaders = hotreload.NewShaderReloader(cx, nil, hotreload.WithShaderDir(dir))
		if _, err := shaders.RegisterShader("hero_shader", "shaders/hero.wgsl"); err != nil {
			t.Fatal(err)
		}
		mats = hotreload.NewMaterialReloader(cx, shaders, hotreload.WithMaterialDir(dir))
		if _, _, err := mats.Register("hero_mat", "materials/hero.material.yaml", nil); err != nil {
			t.Fatal(err)
		}

		b := mcp.New(mcp.Config{
			Resources: resLib,
			Shaders:   shaders,
			Materials: mats,
			ReloadAsset: func(_ context.Context, key string) error {
				hookKeys = append(hookKeys, key)
				return hookErr
			},
		})
		if err := b.RegisterAtlas("ui_atlas", "ui", "atlases/ui.json", standaloneAtlas, func(_ context.Context, p string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		}); err != nil {
			t.Fatal(err)
		}
		if err := b.Attach(cx); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// 1. game_assets_list surfaces materials, resources, shaders, and custom ui groups in sorted order.
	listed := callTool[mcp.AssetsListResult](t, app, cx, mcp.ToolAssetsList, nil)
	if listed.GroupCount != 4 {
		t.Fatalf("groups = %+v, want 4 (materials, resources, shaders, ui)", listed)
	}
	wantGroups := []string{"materials", "resources", "shaders", "ui"}
	for i, want := range wantGroups {
		if listed.Groups[i].Name != want {
			t.Fatalf("groups[%d].Name = %q, want %q", i, listed.Groups[i].Name, want)
		}
	}

	// 2. Reload standalone atlas after updating disk file -> updates standaloneAtlas in place.
	writeDiskFile(t, dir, "atlases/ui.json", atlasV2)
	uiReload := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "ui_atlas"})
	if !uiReload.Reloaded || uiReload.Error != "" || len(standaloneAtlas.Frames) != 3 || standaloneAtlas.Frames["idle.png"].W != 24 {
		t.Fatalf("standalone atlas reload = %+v, frames=%+v", uiReload, standaloneAtlas.Frames)
	}

	// 3. Reload valid shader edit, then broken shader edit (must report structured error & keep previous shader alive).
	writeDiskFile(t, dir, "shaders/hero.wgsl", validWGSLV2)
	shReload := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "hero_shader"})
	if !shReload.Reloaded || shReload.Kind != "shader" || shaders.Revision() != 1 {
		t.Fatalf("shader reload = %+v, rev=%d", shReload, shaders.Revision())
	}
	writeDiskFile(t, dir, "shaders/hero.wgsl", brokenWGSL)
	shBroken := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "shaders/hero.wgsl"})
	if shBroken.Reloaded || shBroken.Error == "" {
		t.Fatalf("broken shader reload = %+v, want reloaded=false + error", shBroken)
	}
	if shaders.Shader("hero_shader") == nil {
		t.Fatal("previous working shader was lost after compile error")
	}
	// Restore valid shader for subsequent reloads.
	writeDiskFile(t, dir, "shaders/hero.wgsl", validWGSLV2)

	// 4. Reload valid material edit, then broken material YAML (keeps previous material active).
	writeDiskFile(t, dir, "materials/hero.material.yaml", validMaterialV2)
	matReload := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "hero_mat"})
	if !matReload.Reloaded || matReload.Kind != "material" {
		t.Fatalf("material reload = %+v", matReload)
	}
	if def := mats.Material("hero_mat"); def == nil || def.Blend != "alpha" || def.Scalars["roughness"] != 0.85 {
		t.Fatalf("reloaded material def = %+v", def)
	}
	writeDiskFile(t, dir, "materials/hero.material.yaml", brokenMaterial)
	matBroken := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "hero_mat"})
	if matBroken.Reloaded || matBroken.Error == "" {
		t.Fatalf("broken material reload = %+v", matBroken)
	}
	if def := mats.Material("hero_mat"); def == nil || def.Blend != "alpha" {
		t.Fatalf("broken material overwrote working material: %+v", def)
	}
	writeDiskFile(t, dir, "materials/hero.material.yaml", validMaterialV2)

	// 5. Reload YAML resource in place via resource.Library.
	writeDiskFile(t, dir, "resources/ease.yaml", validResourceV2)
	resReload := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "resources/ease.yaml"})
	if !resReload.Reloaded || resReload.Group != "resources" {
		t.Fatalf("resource reload = %+v", resReload)
	}
	if loadedCurve.Keys[1].Value != 2.5 {
		t.Fatalf("live curve not updated in place: %+v", loadedCurve.Keys)
	}

	// 6. Reload all=true reloads everything across all groups cleanly.
	allRes := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"all": true})
	if !allRes.Reloaded || len(allRes.Items) != 4 || allRes.Error != "" {
		t.Fatalf("reload all = %+v", allRes)
	}

	// 7. Fallback ReloadAsset hook handles unknown keys (success and structured error).
	hookRes := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "scenes/level1.scene.yaml"})
	if !hookRes.Reloaded || len(hookKeys) != 1 || hookKeys[0] != "scenes/level1.scene.yaml" {
		t.Fatalf("fallback hook reload = %+v, keys=%v", hookRes, hookKeys)
	}
	hookErr = errors.New("scene parse failed at line 4")
	hookFail := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "scenes/broken.scene.yaml"})
	if hookFail.Reloaded || !strings.Contains(hookFail.Error, "line 4") {
		t.Fatalf("fallback hook failure = %+v", hookFail)
	}
}

func TestAssetsToolsValidationAndErrorCases(t *testing.T) {
	var (
		cx *core.Scope
		b  *mcp.Bridge
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		var err error
		b, err = mcp.Attach(cx, mcp.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Unconfigured asset manager / reloader returns clear errors.
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetsList, nil); err == nil || !strings.Contains(err.Error(), "no asset manager") {
		t.Fatalf("assets list unconfigured = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "hero"}); err == nil || !strings.Contains(err.Error(), "no asset manager") {
		t.Fatalf("asset reload unconfigured = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetsReloadGroup, map[string]any{"group": "level1"}); err == nil || !strings.Contains(err.Error(), "no asset manager") {
		t.Fatalf("reload group unconfigured = %v", err)
	}

	// Unknown JSON fields rejected on all three tools.
	for _, tool := range []string{mcp.ToolAssetsList, mcp.ToolAssetReload, mcp.ToolAssetsReloadGroup} {
		if _, err := callToolRaw(t, app, cx, tool, `{"bogus":true}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("%s unknown field = %v, want unknown field error", tool, err)
		}
	}

	// Missing required args / invalid combinations.
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetsList, map[string]any{"state": "invalid"}); err == nil || !strings.Contains(err.Error(), "invalid asset state") {
		t.Fatalf("invalid state = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetReload, `{}`); err == nil || !strings.Contains(err.Error(), "key is required") {
		t.Fatalf("missing key = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "hero", "all": true}); err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("key+all = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetsReloadGroup, `{}`); err == nil || !strings.Contains(err.Error(), "group is required") {
		t.Fatalf("missing group = %v", err)
	}

	// Attach a live assets.Manager with an unloaded group, test not-found & not-loaded errors.
	var mgr *assets.Manager
	var childScope *core.Scope
	app.Post(func() {
		childScope = cx.Child()
		mgr = assets.NewManager(childScope)
		mgr.Group("unloaded_lvl").AddBytes("doc", "asset:doc.txt")
		b.SetAssets(mgr)
	})
	app.Settle()

	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing asset = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "doc", "group": "no_such_group"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing group = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetsReloadGroup, map[string]any{"group": "unloaded_lvl"}); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("reloading unloaded group = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetsReloadGroup, map[string]any{"group": "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("reloading unknown group = %v", err)
	}

	// Disposed manager returns clear error.
	app.Post(func() { childScope.Dispose() })
	app.Settle()
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetsList, nil); err == nil || !strings.Contains(err.Error(), "disposed") {
		t.Fatalf("disposed manager list = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "doc"}); err == nil || !strings.Contains(err.Error(), "disposed") {
		t.Fatalf("disposed manager reload = %v", err)
	}

	// Custom asset registration validation.
	if err := (*mcp.Bridge)(nil).RegisterAsset(mcp.CustomAsset{Key: "a"}); !errors.Is(err, mcp.ErrNilBridge) {
		t.Fatalf("nil bridge RegisterAsset = %v", err)
	}
	if err := b.RegisterAsset(mcp.CustomAsset{}); !errors.Is(err, mcp.ErrEmptyAssetKey) {
		t.Fatalf("empty key RegisterAsset = %v", err)
	}
	if err := b.RegisterAsset(mcp.CustomAsset{Key: "dup"}); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterAsset(mcp.CustomAsset{Key: "dup"}); !errors.Is(err, mcp.ErrDuplicateAsset) {
		t.Fatalf("duplicate RegisterAsset = %v", err)
	}
	if err := b.RegisterAtlas("a", "g", "p.json", nil, nil); err == nil {
		t.Fatal("expected error for nil atlas in RegisterAtlas")
	}
}

func TestAssetReloadAllNeedsMoreThanTheReloadAssetHook(t *testing.T) {
	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			ReloadAsset: func(context.Context, string) error { return nil },
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})
	if _, err := callToolRaw(t, app, cx, mcp.ToolAssetReload, map[string]any{"all": true}); err == nil || !strings.Contains(err.Error(), "single key") {
		t.Fatalf("reload all with only the ReloadAsset hook = %v, want an error", err)
	}
	res := callTool[mcp.AssetReloadResult](t, app, cx, mcp.ToolAssetReload, map[string]any{"key": "scenes/level1.scene.yaml"})
	if !res.Reloaded || res.Error != "" {
		t.Fatalf("reload one key through the hook = %+v", res)
	}
}
