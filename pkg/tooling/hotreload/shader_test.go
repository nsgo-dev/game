//go:build !release

package hotreload_test

import (
	"bytes"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func solidColorWGSL(r, g, b string) string {
	return `
struct VertexOut {
	@builtin(position) pos: vec4<f32>,
}

@vertex
fn vs_main(@builtin(vertex_index) idx: u32) -> VertexOut {
	var positions = array<vec2<f32>, 3>(
		vec2<f32>(-1.0, -1.0),
		vec2<f32>(3.0, -1.0),
		vec2<f32>(-1.0, 3.0),
	);
	var out: VertexOut;
	out.pos = vec4<f32>(positions[idx], 0.0, 1.0);
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return vec4<f32>(` + r + `, ` + g + `, ` + b + `, 1.0);
}
`
}

func renderFullscreen(t *testing.T, sr *gpu.SoftwareRenderer, pipe *gpu.Pipeline) color.RGBA {
	t.Helper()
	if pipe == nil {
		t.Fatal("renderFullscreen: pipeline is nil")
	}
	dev := sr.Device()
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x000000),
		}},
	})
	pass.SetPipeline(pipe)
	pass.Draw(3, 0)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}
	img := sr.Image()
	return img.RGBAAt(img.Bounds().Dx()/2, img.Bounds().Dy()/2)
}

func writeFileAtomic(t *testing.T, path, data string) {
	t.Helper()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.WriteString(data); err != nil {
		_ = tmp.Close()
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		t.Fatal(err)
	}
	cleanup = false
}

func wantRGBA(t *testing.T, got, want color.RGBA) {
	t.Helper()
	if got != want {
		t.Fatalf("pixel = %+v, want %+v", got, want)
	}
}

func TestRecognizedShaderAndMaterialExtensions(t *testing.T) {
	for _, p := range []string{"lit.wgsl", "shaders/LIT.WGSL", "fx/blur.kage", "fx/BLUR.KAGE"} {
		if !hotreload.IsShaderFile(p) {
			t.Errorf("IsShaderFile(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"lit.glsl", "lit.wgsl.bak", "mat.material.yaml", ""} {
		if hotreload.IsShaderFile(p) {
			t.Errorf("IsShaderFile(%q) = true, want false", p)
		}
	}

	for _, p := range []string{"stone.material.yaml", "mats/STONE.MATERIAL.YAML", "wood.mat.yaml", "glass.material.yml", "ice.mat.yml"} {
		if !hotreload.IsMaterialFile(p) {
			t.Errorf("IsMaterialFile(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"scene.scene.yaml", "plain.yaml", "shader.wgsl", ""} {
		if hotreload.IsMaterialFile(p) {
			t.Errorf("IsMaterialFile(%q) = true, want false", p)
		}
	}
}

func TestWatchPatternMatchesCompoundExtensions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "stone.material.yaml"), "version: 1\nname: v1\n")
	writeFile(t, filepath.Join(dir, "other.yaml"), "version: 1\n")

	var (
		mu   sync.Mutex
		seen []string
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(15*time.Millisecond),
			hotreload.WithDebounce(15*time.Millisecond),
		)
		w.WatchPattern(".material.yaml", func(rel string, _ []byte) {
			mu.Lock()
			seen = append(seen, rel)
			mu.Unlock()
		})
		return core.View{}
	})

	writeFile(t, filepath.Join(dir, "other.yaml"), "version: 2\n")
	writeFile(t, filepath.Join(dir, "stone.material.yaml"), "version: 1\nname: v2\n")

	app.WaitUntil(time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) >= 1
	})
	app.Settle()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "stone.material.yaml" {
		t.Fatalf("seen = %q, want [stone.material.yaml]", seen)
	}
}

// TestShaderHotReloadUpdatesPixelsWithinOneSecondAndRecoversFromErrors verifies issue #366's acceptance:
// an edited WGSL shader updates rendered pixels within 1 second under game dev (default watcher timings),
// compile/pipeline errors keep the previous working pipeline active without crashing, and fixing the error
// restores live reloading and clears the error.
func TestShaderHotReloadUpdatesPixelsWithinOneSecondAndRecoversFromErrors(t *testing.T) {
	dir := t.TempDir()
	shaderDir := filepath.Join(dir, "shaders")
	if err := os.MkdirAll(shaderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shaderFile := filepath.Join(shaderDir, "quad.wgsl")
	writeFileAtomic(t, shaderFile, solidColorWGSL("1.0", "0.0", "0.0"))
	t.Setenv(hotreload.AssetsDirEnv, dir)

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	var (
		reloader    *hotreload.ShaderReloader
		mu          sync.Mutex
		reloadedHit []string
		errHistory  []hotreload.ReloadError
		reloadedCnt atomic.Int32
		errCnt      atomic.Int32
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewDevWatcher(cx) // default timings: 150ms poll + 40ms debounce
		reloader = hotreload.NewShaderReloader(cx, dev,
			hotreload.WithShaderDir(hotreload.DevAssetsDir()),
			hotreload.WithOnShaderReload(func(name, path string) {
				mu.Lock()
				reloadedHit = append(reloadedHit, name+":"+path)
				mu.Unlock()
				reloadedCnt.Add(1)
			}),
			hotreload.WithOnShaderError(func(re hotreload.ReloadError) {
				mu.Lock()
				errHistory = append(errHistory, re)
				mu.Unlock()
				errCnt.Add(1)
			}),
		)
		w.WatchShaders(reloader)

		_, err := reloader.RegisterPipeline("quad", hotreload.PipelineSpec{
			Shader: "shaders/quad.wgsl",
			Build: func(v, f *gpu.ShaderModule) (gpu.PipelineDesc, error) {
				return gpu.PipelineDesc{
					Label:     "quad",
					Vertex:    gpu.ShaderStage{Module: v, EntryPoint: "vs_main"},
					Fragment:  gpu.ShaderStage{Module: f, EntryPoint: "fs_main"},
					Primitive: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList},
					Targets:   []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
				}, nil
			},
		})
		if err != nil {
			t.Errorf("RegisterPipeline: %v", err)
		}
		return core.View{}
	})

	initialPipe := reloader.Pipeline("quad")
	wantRGBA(t, renderFullscreen(t, sr, initialPipe), color.RGBA{R: 255, G: 0, B: 0, A: 255})

	// 1. Edit shader to green and verify live reload updates pixels within 1 second.
	start := time.Now()
	writeFileAtomic(t, shaderFile, solidColorWGSL("0.0", "1.0", "0.0"))
	app.WaitUntil(time.Second, func() bool { return reloadedCnt.Load() >= 1 })
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("shader reload took %v, want < 1s", elapsed)
	}
	if gotErrs := errCnt.Load(); gotErrs != 0 {
		t.Fatalf("unexpected %d reload error(s) during valid shader save: %+v", gotErrs, reloader.Errors())
	}
	greenPipe := reloader.Pipeline("quad")
	if greenPipe == nil || greenPipe.ID() == initialPipe.ID() {
		t.Fatalf("expected new pipeline after reload (old=%v new=%v)", initialPipe, greenPipe)
	}
	if initialPipe.Ready() {
		t.Fatal("replaced old pipeline was not closed")
	}
	wantRGBA(t, renderFullscreen(t, sr, greenPipe), color.RGBA{R: 0, G: 255, B: 0, A: 255})

	// 2. Introduce a WGSL compile error: must report structured error without crashing AND keep drawing green.
	badWGSL := strings.Replace(solidColorWGSL("0.0", "1.0", "0.0"), "vec4<f32>(0.0, 1.0, 0.0, 1.0)", "undefined_symbol", 1)
	prevErrs := errCnt.Load()
	writeFileAtomic(t, shaderFile, badWGSL)
	app.WaitUntil(time.Second, func() bool { return errCnt.Load() > prevErrs })

	re, hasErr := reloader.LastError("quad")
	if !hasErr {
		t.Fatal("expected LastError(\"quad\") after broken shader edit")
	}
	if re.Kind != hotreload.ReloadKindPipeline || re.Path != "shaders/quad.wgsl" || re.Line <= 0 || re.At.IsZero() {
		t.Fatalf("unexpected ReloadError: %+v", re)
	}
	le, ok := loaderr.As(re)
	if !ok || le.File != "shaders/quad.wgsl" || le.Line <= 0 {
		t.Fatalf("expected *loaderr.Error with file and line, got %v", re.Err)
	}
	// Active pipeline must still be the working green pipeline!
	if cur := reloader.Pipeline("quad"); cur != greenPipe {
		t.Fatalf("broken edit replaced active pipeline: got %v, want %v", cur, greenPipe)
	}
	wantRGBA(t, renderFullscreen(t, sr, reloader.Pipeline("quad")), color.RGBA{R: 0, G: 255, B: 0, A: 255})

	// 3. Valid WGSL but missing required entry point -> CreatePipeline fails, still keeps green pipeline!
	missingEntryWGSL := strings.Replace(solidColorWGSL("1.0", "1.0", "0.0"), "fn fs_main", "fn fs_other", 1)
	prevErrs = errCnt.Load()
	writeFileAtomic(t, shaderFile, missingEntryWGSL)
	app.WaitUntil(time.Second, func() bool { return errCnt.Load() > prevErrs })
	if cur := reloader.Pipeline("quad"); cur != greenPipe {
		t.Fatalf("entry-point error replaced active pipeline")
	}
	wantRGBA(t, renderFullscreen(t, sr, reloader.Pipeline("quad")), color.RGBA{R: 0, G: 255, B: 0, A: 255})

	// 4. Fix shader to blue -> recovers seamlessly, clears error, swaps pipeline, draws blue.
	prevReloads := reloadedCnt.Load()
	writeFileAtomic(t, shaderFile, solidColorWGSL("0.0", "0.0", "1.0"))
	app.WaitUntil(time.Second, func() bool { return reloadedCnt.Load() > prevReloads })

	if _, stillErr := reloader.LastError("quad"); stillErr {
		t.Fatal("expected LastError(\"quad\") to clear after valid shader edit")
	}
	if errs := reloader.Errors(); len(errs) != 0 {
		t.Fatalf("expected no remaining errors, got %+v", errs)
	}
	bluePipe := reloader.Pipeline("quad")
	if bluePipe == nil || bluePipe.ID() == greenPipe.ID() {
		t.Fatalf("expected fresh pipeline after recovery")
	}
	wantRGBA(t, renderFullscreen(t, sr, bluePipe), color.RGBA{R: 0, G: 0, B: 255, A: 255})

	mu.Lock()
	if len(reloadedHit) < 2 || len(errHistory) < 2 {
		t.Fatalf("reloadedHit=%v errHistory=%d", reloadedHit, len(errHistory))
	}
	mu.Unlock()
}

// TestShaderHotReloadToleratesTruncateThenWrite verifies issue #962:
// an editor save that truncates a shader file before writing new contents never
// emits a spurious empty-source ReloadError, while a file that genuinely stays
// empty still reports the empty-source error after settling and recovers cleanly.
func TestShaderHotReloadToleratesTruncateThenWrite(t *testing.T) {
	dir := t.TempDir()
	shaderDir := filepath.Join(dir, "shaders")
	if err := os.MkdirAll(shaderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shaderFile := filepath.Join(shaderDir, "quad.wgsl")
	writeFileAtomic(t, shaderFile, solidColorWGSL("1.0", "0.0", "0.0"))

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	var (
		reloader    *hotreload.ShaderReloader
		mu          sync.Mutex
		errHistory  []hotreload.ReloadError
		reloadedCnt atomic.Int32
		errCnt      atomic.Int32
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(15*time.Millisecond),
			hotreload.WithDebounce(20*time.Millisecond),
		)
		reloader = hotreload.NewShaderReloader(cx, dev,
			hotreload.WithShaderDir(dir),
			hotreload.WithOnShaderReload(func(string, string) {
				reloadedCnt.Add(1)
			}),
			hotreload.WithOnShaderError(func(re hotreload.ReloadError) {
				mu.Lock()
				errHistory = append(errHistory, re)
				mu.Unlock()
				errCnt.Add(1)
			}),
		)
		w.WatchShaders(reloader)

		_, err := reloader.RegisterPipeline("quad", hotreload.PipelineSpec{
			Shader: "shaders/quad.wgsl",
			Build: func(v, f *gpu.ShaderModule) (gpu.PipelineDesc, error) {
				return gpu.PipelineDesc{
					Label:     "quad",
					Vertex:    gpu.ShaderStage{Module: v, EntryPoint: "vs_main"},
					Fragment:  gpu.ShaderStage{Module: f, EntryPoint: "fs_main"},
					Primitive: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList},
					Targets:   []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
				}, nil
			},
		})
		if err != nil {
			t.Errorf("RegisterPipeline: %v", err)
		}
		return core.View{}
	})

	wantRGBA(t, renderFullscreen(t, sr, reloader.Pipeline("quad")), color.RGBA{R: 255, G: 0, B: 0, A: 255})

	truncateThenWrite := func(content string, pause time.Duration) {
		t.Helper()
		f, err := os.OpenFile(shaderFile, os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(pause)
		if _, err := f.WriteString(content); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Truncate-then-write a valid shader with a pause longer than the normal
	// 20ms debounce so the watcher observes the 0-byte state multiple times.
	truncateThenWrite(solidColorWGSL("0.0", "1.0", "0.0"), 75*time.Millisecond)
	app.WaitUntil(time.Second, func() bool { return reloadedCnt.Load() >= 1 })
	app.Settle()

	mu.Lock()
	if len(errHistory) != 0 {
		t.Fatalf("truncate-then-write emitted spurious ReloadError(s): %+v", errHistory)
	}
	mu.Unlock()
	greenPipe := reloader.Pipeline("quad")
	wantRGBA(t, renderFullscreen(t, sr, greenPipe), color.RGBA{R: 0, G: 255, B: 0, A: 255})

	// 2. Truncate-then-write a broken shader: must report the real syntax error,
	// never a transient empty-source error.
	badWGSL := strings.Replace(solidColorWGSL("0.0", "1.0", "0.0"), "vec4<f32>(0.0, 1.0, 0.0, 1.0)", "undefined_symbol", 1)
	truncateThenWrite(badWGSL, 75*time.Millisecond)
	app.WaitUntil(time.Second, func() bool { return errCnt.Load() >= 1 })
	app.Settle()

	re, ok := reloader.LastError("quad")
	if !ok || re.Line <= 0 || strings.Contains(re.Message, "empty shader source") {
		t.Fatalf("expected real WGSL line error after truncate-then-write, got ok=%v re=%+v", ok, re)
	}
	mu.Lock()
	for _, h := range errHistory {
		if strings.Contains(h.Message, "empty shader source") {
			t.Fatalf("unexpected empty-source error during truncate-then-write: %+v", errHistory)
		}
	}
	mu.Unlock()
	if reloader.Pipeline("quad") != greenPipe {
		t.Fatal("broken edit replaced active pipeline")
	}

	// 3. Genuinely empty file: truncating and leaving the file empty must still
	// report an empty-source ReloadError once settled, keeping the green pipeline.
	prevErrs := errCnt.Load()
	if err := os.Truncate(shaderFile, 0); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, func() bool { return errCnt.Load() > prevErrs })

	re, ok = reloader.LastError("quad")
	if !ok || !strings.Contains(re.Message, "empty shader source") {
		t.Fatalf("expected empty-source ReloadError for genuinely empty file, got ok=%v re=%+v", ok, re)
	}
	if reloader.Pipeline("quad") != greenPipe {
		t.Fatal("empty file replaced active pipeline")
	}

	// 4. Writing a valid shader recovers and clears the empty-source error.
	prevReloads := reloadedCnt.Load()
	writeFileAtomic(t, shaderFile, solidColorWGSL("0.0", "0.0", "1.0"))
	app.WaitUntil(time.Second, func() bool { return reloadedCnt.Load() > prevReloads })
	if _, stillErr := reloader.LastError("quad"); stillErr {
		t.Fatal("expected error to clear after valid shader save")
	}
	wantRGBA(t, renderFullscreen(t, sr, reloader.Pipeline("quad")), color.RGBA{R: 0, G: 0, B: 255, A: 255})
}

func TestWatchShaderSupportsKageAndReportsLineErrors(t *testing.T) {
	dir := t.TempDir()
	kagePath := filepath.Join(dir, "tint.kage")
	validKage := `package main

func Fragment(dst vec4, pos vec2) vec4 {
	return vec4(1, 0, 0, 1)
}
`
	writeFile(t, kagePath, validKage)

	var (
		gotMod atomic.Pointer[gpu.ShaderModule]
		gotErr atomic.Value
		calls  atomic.Int32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(15*time.Millisecond),
			hotreload.WithDebounce(15*time.Millisecond),
		)
		w.WatchShader("tint.kage", func(mod *gpu.ShaderModule, err error) {
			if err != nil {
				gotErr.Store(err)
			} else {
				gotMod.Store(mod)
			}
			calls.Add(1)
		})
		return core.View{}
	})

	// Break Kage syntax.
	writeFile(t, kagePath, "package main\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\treturn broken_ident\n}\n")
	app.WaitUntil(time.Second, func() bool { return calls.Load() >= 1 })
	errVal, _ := gotErr.Load().(error)
	le, ok := loaderr.As(errVal)
	if !ok || le.File != "tint.kage" || le.Line <= 0 {
		t.Fatalf("expected *loaderr.Error with file and line for Kage, got %v", errVal)
	}

	// Fix Kage shader.
	fixedKage := strings.Replace(validKage, "vec4(1, 0, 0, 1)", "vec4(0, 1, 0, 1)", 1)
	writeFile(t, kagePath, fixedKage)
	app.WaitUntil(time.Second, func() bool { return calls.Load() >= 2 })
	mod := gotMod.Load()
	if mod == nil || !mod.HasFragmentEntry("Fragment") {
		t.Fatalf("expected compiled Kage ShaderModule with Fragment entry, got %v", mod)
	}
}

func TestMaterialParseEncodeRoundTripAndValidation(t *testing.T) {
	dw := false
	orig := &hotreload.MaterialDef{
		Version:       hotreload.MaterialVersion,
		Name:          "Neon Crystal",
		Shader:        "shaders/crystal.wgsl",
		VertexEntry:   "vs_main",
		FragmentEntry: "fs_main",
		Blend:         "alpha",
		Cull:          "none",
		FrontFace:     "cw",
		DepthWrite:    &dw,
		DepthCompare:  "lessEqual",
		Scalars:       map[string]float32{"metallic": 0.1, "roughness": 0.25},
		Vec2:          map[string][2]float32{"uvScale": {2, 2}},
		Vec3:          map[string][3]float32{"rim": {0.1, 0.5, 1}},
		Vec4:          map[string][4]float32{"tint": {1, 0.5, 0.25, 1}},
		Colors:        map[string]string{"albedo": "#33ccff"},
		Textures:      map[string]string{"albedoMap": "textures/crystal.png"},
	}

	enc1, err := hotreload.EncodeMaterial(orig)
	if err != nil {
		t.Fatalf("EncodeMaterial: %v", err)
	}
	parsed, err := hotreload.ParseMaterialFile("materials/crystal.material.yaml", enc1)
	if err != nil {
		t.Fatalf("ParseMaterialFile: %v", err)
	}
	enc2, err := hotreload.EncodeMaterial(parsed)
	if err != nil {
		t.Fatalf("EncodeMaterial 2: %v", err)
	}
	if !bytes.Equal(enc1, enc2) {
		t.Fatalf("round-trip mismatch:\n--- enc1 ---\n%s\n--- enc2 ---\n%s", enc1, enc2)
	}

	if _, ok := parsed.Color("albedo"); !ok {
		t.Fatal("expected parsed Color(\"albedo\")")
	}
	desc := gpu.PipelineDesc{Targets: []gpu.ColorTargetState{{Format: gpu.FormatBGRA8Unorm}}}
	parsed.ApplyToPipelineDesc(&desc, gpu.FormatDepth24Plus)
	if desc.Primitive.CullMode != gpu.CullNone || desc.Primitive.FrontFace != gpu.FrontCW {
		t.Fatalf("unexpected primitive state: %+v", desc.Primitive)
	}
	if desc.DepthStencil == nil || desc.DepthStencil.DepthWrite != false || desc.DepthStencil.DepthCompare != gpu.CompareLessEqual {
		t.Fatalf("unexpected depth stencil: %+v", desc.DepthStencil)
	}
	if desc.Targets[0].Blend == nil {
		t.Fatal("expected blend state applied")
	}

	// Refusal cases: all must fail with *loaderr.Error.
	badCases := map[string]string{
		"empty":             "   \n",
		"wrong version":     "version: 2\n",
		"unknown field":     "version: 1\nshininess: 3\n",
		"trailing doc":      "version: 1\n---\nversion: 1\n",
		"bad shader ext":    "version: 1\nshader: shaders/lit.glsl\n",
		"bad shader path":   "version: 1\nshader: ../escape.wgsl\n",
		"backslash shader":  "version: 1\nshader: shaders\\lit.wgsl\n",
		"bad blend":         "version: 1\nblend: screen\n",
		"bad cull":          "version: 1\ncull: sideways\n",
		"bad frontFace":     "version: 1\nfrontFace: upsideDown\n",
		"bad depthCmp":      "version: 1\ndepthCompare: maybe\n",
		"bad color":         "version: 1\ncolors:\n  albedo: not-a-color\n",
		"bad texture":       "version: 1\ntextures:\n  albedo: /abs/path.png\n",
		"backslash texture": "version: 1\ntextures:\n  albedo: textures\\a.png\n",
	}
	for name, src := range badCases {
		t.Run(name, func(t *testing.T) {
			_, err := hotreload.ParseMaterialFile("materials/bad.material.yaml", []byte(src))
			if err == nil {
				t.Fatalf("ParseMaterialFile(%s) unexpectedly succeeded", name)
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("expected *loaderr.Error, got %T: %v", err, err)
			}
			if le.File != "materials/bad.material.yaml" {
				t.Fatalf("le.File = %q, want materials/bad.material.yaml", le.File)
			}
		})
	}

	// NaN scalar must fail Validate.
	nanMat := &hotreload.MaterialDef{Version: 1, Scalars: map[string]float32{"x": float32(math.NaN())}}
	if err := nanMat.Validate(); err == nil {
		t.Fatal("expected NaN scalar to fail Validate")
	}
}

func TestMaterialHotReloadAndShaderDependencyTracking(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "shaders"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}

	shaderA := filepath.Join(dir, "shaders", "crystal.wgsl")
	shaderB := filepath.Join(dir, "shaders", "alt.wgsl")
	matFile := filepath.Join(dir, "materials", "crystal.material.yaml")

	writeFile(t, shaderA, solidColorWGSL("1.0", "0.0", "0.0")) // red
	writeFile(t, shaderB, solidColorWGSL("1.0", "0.0", "1.0")) // magenta
	writeFile(t, matFile, `version: 1
name: Crystal
shader: shaders/crystal.wgsl
blend: opaque
cull: back
scalars:
  roughness: 0.2
colors:
  tint: "#ff0000"
`)

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	var (
		shaders   *hotreload.ShaderReloader
		materials *hotreload.MaterialReloader
		matReload atomic.Int32
		matErrors atomic.Int32
	)

	buildMat := func(def *hotreload.MaterialDef, v, f *gpu.ShaderModule) (gpu.PipelineDesc, error) {
		desc := gpu.PipelineDesc{
			Label:    def.Name,
			Vertex:   gpu.ShaderStage{Module: v},
			Fragment: gpu.ShaderStage{Module: f},
			Targets:  []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
		}
		def.ApplyToPipelineDesc(&desc, gpu.FormatUndefined)
		return desc, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(15*time.Millisecond),
			hotreload.WithDebounce(15*time.Millisecond),
		)
		shaders = hotreload.NewShaderReloader(cx, dev, hotreload.WithShaderDir(dir))
		materials = hotreload.NewMaterialReloader(cx, shaders,
			hotreload.WithMaterialDir(dir),
			hotreload.WithOnMaterialReload(func(name, path string) {
				matReload.Add(1)
			}),
			hotreload.WithOnMaterialError(func(re hotreload.ReloadError) {
				matErrors.Add(1)
			}),
		)
		w.WatchMaterials(materials)

		_, _, err := materials.Register("crystal", "materials/crystal.material.yaml", buildMat)
		if err != nil {
			t.Errorf("Register material: %v", err)
		}
		return core.View{}
	})

	if got := materials.Material("crystal").Scalars["roughness"]; got != 0.2 {
		t.Fatalf("initial roughness = %v, want 0.2", got)
	}
	wantRGBA(t, renderFullscreen(t, sr, materials.Pipeline("crystal")), color.RGBA{R: 255, G: 0, B: 0, A: 255})

	// 1. Edit material YAML parameters live.
	writeFile(t, matFile, `version: 1
name: Crystal Polished
shader: shaders/crystal.wgsl
blend: alpha
cull: none
scalars:
  roughness: 0.85
colors:
  tint: "#00ff88"
`)
	app.WaitUntil(time.Second, func() bool { return matReload.Load() >= 1 })
	m := materials.Material("crystal")
	if m.Scalars["roughness"] != 0.85 || m.Blend != "alpha" || m.Cull != "none" || m.Colors["tint"] != "#00ff88" {
		t.Fatalf("unexpected reloaded material: %+v", m)
	}
	goodPipe := materials.Pipeline("crystal")

	// 2. Write invalid material YAML (unknown field): must keep old material & pipeline and report *loaderr.Error.
	writeFile(t, matFile, "version: 1\nshader: shaders/crystal.wgsl\nunknownKey: 123\n")
	app.WaitUntil(time.Second, func() bool { return matErrors.Load() >= 1 })
	re, ok := materials.LastError("crystal")
	if !ok || re.Kind != hotreload.ReloadKindMaterial {
		t.Fatalf("expected material ReloadError, got ok=%v re=%+v", ok, re)
	}
	le, ok := loaderr.As(re)
	if !ok || le.File != "materials/crystal.material.yaml" || le.Line <= 0 {
		t.Fatalf("expected *loaderr.Error with file and line, got %v", re.Err)
	}
	if cur := materials.Material("crystal"); cur.Scalars["roughness"] != 0.85 {
		t.Fatalf("broken material edit clobbered roughness: got %v, want 0.85", cur.Scalars["roughness"])
	}
	if materials.Pipeline("crystal") != goodPipe {
		t.Fatal("broken material edit replaced active pipeline")
	}

	// 3. Edit the referenced shader file (shaders/crystal.wgsl) -> dependent material reloads automatically!
	prevReloads := matReload.Load()
	writeFile(t, shaderA, solidColorWGSL("0.0", "1.0", "1.0")) // cyan
	app.WaitUntil(time.Second, func() bool { return matReload.Load() > prevReloads })
	cyanPipe := materials.Pipeline("crystal")
	if cyanPipe == goodPipe {
		t.Fatal("expected material pipeline to rebuild when its shader changed")
	}
	if _, stillErr := materials.LastError("crystal"); stillErr {
		t.Fatal("expected material error to clear after successful shader rebuild")
	}
	wantRGBA(t, renderFullscreen(t, sr, cyanPipe), color.RGBA{R: 0, G: 255, B: 255, A: 255})

	// 4. Break the referenced shader -> material records error without crashing AND keeps drawing cyan!
	prevErrs := matErrors.Load()
	writeFile(t, shaderA, "@vertex fn vs_main() { broken")
	app.WaitUntil(time.Second, func() bool { return matErrors.Load() > prevErrs })
	if re, ok := materials.LastError("crystal"); !ok || re.Path != "shaders/crystal.wgsl" {
		t.Fatalf("expected shader error on material, got ok=%v re=%+v", ok, re)
	}
	if materials.Pipeline("crystal") != cyanPipe {
		t.Fatal("broken shader replaced active material pipeline")
	}
	wantRGBA(t, renderFullscreen(t, sr, materials.Pipeline("crystal")), color.RGBA{R: 0, G: 255, B: 255, A: 255})

	// 5. Fix the referenced shader to yellow -> material recovers and draws yellow.
	prevReloads = matReload.Load()
	writeFile(t, shaderA, solidColorWGSL("1.0", "1.0", "0.0")) // yellow
	app.WaitUntil(time.Second, func() bool { return matReload.Load() > prevReloads })
	if _, stillErr := materials.LastError("crystal"); stillErr {
		t.Fatal("expected material error cleared after shader fix")
	}
	wantRGBA(t, renderFullscreen(t, sr, materials.Pipeline("crystal")), color.RGBA{R: 255, G: 255, B: 0, A: 255})

	// 6. Switch material's shader to shaders/alt.wgsl -> edits to alt.wgsl now reload crystal!
	prevReloads = matReload.Load()
	writeFile(t, matFile, "version: 1\nname: Crystal Alt\nshader: shaders/alt.wgsl\n")
	app.WaitUntil(time.Second, func() bool { return matReload.Load() > prevReloads })
	wantRGBA(t, renderFullscreen(t, sr, materials.Pipeline("crystal")), color.RGBA{R: 255, G: 0, B: 255, A: 255})

	prevReloads = matReload.Load()
	writeFile(t, shaderB, solidColorWGSL("0.0", "0.0", "1.0")) // blue
	app.WaitUntil(time.Second, func() bool { return matReload.Load() > prevReloads })
	wantRGBA(t, renderFullscreen(t, sr, materials.Pipeline("crystal")), color.RGBA{R: 0, G: 0, B: 255, A: 255})
}

func TestSplitVertexAndFragmentShaderReloadAndSharedMaterials(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "shaders"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}

	vertSrc := `
struct VertexOut {
	@builtin(position) pos: vec4<f32>,
}

@vertex
fn vs_main(@builtin(vertex_index) idx: u32) -> VertexOut {
	var positions = array<vec2<f32>, 3>(
		vec2<f32>(-1.0, -1.0),
		vec2<f32>(3.0, -1.0),
		vec2<f32>(-1.0, 3.0),
	);
	var out: VertexOut;
	out.pos = vec4<f32>(positions[idx], 0.0, 1.0);
	return out;
}
`
	fragSrc := func(r, g, b string) string {
		return `
struct VertexOut {
	@builtin(position) pos: vec4<f32>,
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return vec4<f32>(` + r + `, ` + g + `, ` + b + `, 1.0);
}
`
	}

	vertFile := filepath.Join(dir, "shaders", "tri.vert.wgsl")
	fragFile := filepath.Join(dir, "shaders", "tri.frag.wgsl")
	matAFile := filepath.Join(dir, "materials", "a.material.yaml")
	matBFile := filepath.Join(dir, "materials", "b.mat.yaml")
	writeFile(t, vertFile, vertSrc)
	writeFile(t, fragFile, fragSrc("1.0", "0.0", "0.0"))
	writeFile(t, matAFile, "version: 1\nname: MatA\nvertexShader: shaders/tri.vert.wgsl\nfragmentShader: shaders/tri.frag.wgsl\n")
	writeFile(t, matBFile, "version: 1\nname: MatB\nvertexShader: shaders/tri.vert.wgsl\nfragmentShader: shaders/tri.frag.wgsl\n")

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	var (
		shaders   *hotreload.ShaderReloader
		materials *hotreload.MaterialReloader
		matReload atomic.Int32
		matErrors atomic.Int32
	)
	buildMat := func(def *hotreload.MaterialDef, v, f *gpu.ShaderModule) (gpu.PipelineDesc, error) {
		desc := gpu.PipelineDesc{
			Label:    def.Name,
			Vertex:   gpu.ShaderStage{Module: v},
			Fragment: gpu.ShaderStage{Module: f},
			Targets:  []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
		}
		def.ApplyToPipelineDesc(&desc, gpu.FormatUndefined)
		return desc, nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(15*time.Millisecond),
			hotreload.WithDebounce(15*time.Millisecond),
		)
		shaders = hotreload.NewShaderReloader(cx, dev, hotreload.WithShaderDir(dir))
		materials = hotreload.NewMaterialReloader(cx, shaders,
			hotreload.WithMaterialDir(dir),
			hotreload.WithOnMaterialReload(func(string, string) { matReload.Add(1) }),
			hotreload.WithOnMaterialError(func(hotreload.ReloadError) { matErrors.Add(1) }),
		)
		w.WatchMaterials(materials)
		if _, _, err := materials.Register("a", "materials/a.material.yaml", buildMat); err != nil {
			t.Errorf("Register a: %v", err)
		}
		if _, _, err := materials.Register("b", "materials/b.mat.yaml", buildMat); err != nil {
			t.Errorf("Register b: %v", err)
		}
		return core.View{}
	})

	if names := materials.MaterialNames(); len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("MaterialNames = %q, want [a b]", names)
	}
	wantRGBA(t, renderFullscreen(t, sr, materials.Pipeline("a")), color.RGBA{R: 255, G: 0, B: 0, A: 255})
	wantRGBA(t, renderFullscreen(t, sr, materials.Pipeline("b")), color.RGBA{R: 255, G: 0, B: 0, A: 255})

	// Editing the shared fragment shader reloads both materials.
	writeFile(t, fragFile, fragSrc("0.0", "1.0", "0.0"))
	app.WaitUntil(time.Second, func() bool { return matReload.Load() >= 2 })
	pipeA := materials.Pipeline("a")
	pipeB := materials.Pipeline("b")
	wantRGBA(t, renderFullscreen(t, sr, pipeA), color.RGBA{R: 0, G: 255, B: 0, A: 255})
	wantRGBA(t, renderFullscreen(t, sr, pipeB), color.RGBA{R: 0, G: 255, B: 0, A: 255})
	if materials.Revision() == 0 {
		t.Fatal("expected MaterialReloader.Revision() > 0")
	}

	// Breaking the shared vertex shader keeps both working pipelines active and records errors for both.
	writeFile(t, vertFile, "@vertex fn vs_main() { broken")
	app.WaitUntil(time.Second, func() bool { return matErrors.Load() >= 2 })
	if materials.Pipeline("a") != pipeA || materials.Pipeline("b") != pipeB {
		t.Fatal("broken vertex shader replaced active material pipelines")
	}
	if errs := materials.Errors(); len(errs) != 2 {
		t.Fatalf("Errors() = %d, want 2", len(errs))
	}
	wantRGBA(t, renderFullscreen(t, sr, materials.Pipeline("a")), color.RGBA{R: 0, G: 255, B: 0, A: 255})

	// Fixing the vertex shader clears errors for both materials.
	prevReloads := matReload.Load()
	writeFile(t, vertFile, vertSrc+"\n// fixed\n")
	app.WaitUntil(time.Second, func() bool { return matReload.Load() >= prevReloads+2 })
	if errs := materials.Errors(); len(errs) != 0 {
		t.Fatalf("Errors() after fix = %+v, want none", errs)
	}

	// Closing reloaders is idempotent and prevents post-close registrations/reloads.
	materials.Close()
	materials.Close()
	shaders.Close()
	shaders.Close()
	if _, _, err := materials.RegisterBytes("c", "materials/c.material.yaml", []byte("version: 1\n"), nil); err == nil {
		t.Fatal("RegisterBytes after Close unexpectedly succeeded")
	}
	if _, err := shaders.RegisterShaderBytes("s", "shaders/s.wgsl", []byte(solidColorWGSL("1.0", "1.0", "1.0"))); err == nil {
		t.Fatal("RegisterShaderBytes after Close unexpectedly succeeded")
	}
	if tracked, err := shaders.ReloadShader("shaders/tri.frag.wgsl", []byte(fragSrc("0.0", "0.0", "1.0"))); tracked || err != nil {
		t.Fatalf("ReloadShader after Close = (%v, %v), want (false, nil)", tracked, err)
	}
	if tracked, err := materials.ReloadMaterial("materials/a.material.yaml", []byte("version: 1\n")); tracked || err != nil {
		t.Fatalf("ReloadMaterial after Close = (%v, %v), want (false, nil)", tracked, err)
	}
}

func TestNilDevWatcherIgnoresShaderAndMaterialWatch(t *testing.T) {
	t.Setenv(hotreload.AssetsDirEnv, "")
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewDevWatcher(cx)
		if w != nil {
			t.Errorf("NewDevWatcher = %v, want nil outside game dev", w)
		}
		sr := hotreload.NewShaderReloader(cx, nil)
		mr := hotreload.NewMaterialReloader(cx, sr)
		w.WatchShader("quad.wgsl", func(*gpu.ShaderModule, error) { t.Error("nil watcher fired") })
		w.WatchShaders(sr)
		w.WatchMaterials(mr)
		return core.View{}
	})
}
