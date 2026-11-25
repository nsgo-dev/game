package editor_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/releaseplan"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
	"github.com/nsgo-dev/game/packages/game/tooling/credits"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type recordedGenCall struct {
	Target     string
	Prompt     string
	Count      int
	References []string
	Seed       *int64
	VarySeed   *int64
	Refine     string
}

func makePixelSpritePNG(t *testing.T, w, h int, colors []color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	outline := color.RGBA{R: 0x19, G: 0x1B, B: 0x22, A: 0xFF}
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			if y == 1 || y == h-2 || x == 1 || x == w-2 {
				img.SetRGBA(x, y, outline)
			} else {
				img.SetRGBA(x, y, colors[(x+y)%len(colors)])
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeToneWAV(t *testing.T, durSec, rmsTargetDB float64) []byte {
	t.Helper()
	const sr = 22050
	frames := int(math.Round(durSec * sr))
	samples := make([]float64, frames)
	for i := range frames {
		phase := 2 * math.Pi * 440.0 * float64(i) / float64(sr)
		env := 1.0
		if i < frames/10 {
			env = float64(i) / float64(max(1, frames/10))
		}
		samples[i] = math.Sin(phase) * env * 0.5
	}
	buf := &audioconv.Buffer{
		SampleRate: sr,
		Channels:   1,
		Samples:    samples,
	}
	buf.NormalizeRMS(rmsTargetDB)
	var out bytes.Buffer
	if err := audioconv.EncodeWAV(&out, buf); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func startFixtureImageGenerator(t *testing.T, license string, commercial bool, payloadOverride [][]byte) (sdkmcp.Transport, *[]recordedGenCall, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var calls []recordedGenCall

	frost4 := []color.RGBA{
		{R: 0x7C, G: 0xC4, B: 0xE8, A: 0xFF},
		{R: 0xE0, G: 0xF6, B: 0xFF, A: 0xFF},
		{R: 0x3F, G: 0x7F, B: 0xA8, A: 0xFF},
		{R: 0x0F, G: 0x11, B: 0x16, A: 0xFF},
	}
	frost7 := append(slices.Clone(frost4),
		color.RGBA{R: 0x9F, G: 0xD8, B: 0xF0, A: 0xFF},
		color.RGBA{R: 0xC8, G: 0xB8, B: 0xFF, A: 0xFF},
		color.RGBA{R: 0x2F, G: 0x6A, B: 0x90, A: 0xFF},
	)

	cA := makePixelSpritePNG(t, 16, 12, frost4)
	cB := makePixelSpritePNG(t, 16, 12, []color.RGBA{frost4[1], frost4[0], frost4[2], frost4[3]})
	cC := makePixelSpritePNG(t, 16, 12, frost7)
	cD := makePixelSpritePNG(t, 24, 18, frost4)

	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "pixel-gen", Version: "1.0.0"}, nil)
	srv.AddTool(&sdkmcp.Tool{
		Name:        "generate_image",
		Description: "Generate pixel art candidates",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var raw map[string]any
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			_ = json.Unmarshal(req.Params.Arguments, &raw)
		}
		var rec recordedGenCall
		if v, ok := raw["target"].(string); ok {
			rec.Target = v
		}
		if v, ok := raw["prompt"].(string); ok {
			rec.Prompt = v
		}
		if v, ok := raw["count"].(float64); ok {
			rec.Count = int(v)
		}
		if v, ok := raw["refine"].(string); ok {
			rec.Refine = v
		}
		if arr, ok := raw["references"].([]any); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					rec.References = append(rec.References, s)
				}
			}
		}
		if v, ok := raw["seed"].(float64); ok {
			s := int64(v)
			rec.Seed = &s
		}
		if v, ok := raw["vary_seed"].(float64); ok {
			s := int64(v)
			rec.VarySeed = &s
		}
		mu.Lock()
		calls = append(calls, rec)
		callNum := len(calls)
		mu.Unlock()

		baseSeed := int64(81514) + int64((callNum-1)*4)
		if rec.Seed != nil {
			baseSeed = *rec.Seed
		}
		imgs := [][]byte{cA, cB, cC, cD}
		if len(payloadOverride) > 0 {
			imgs = payloadOverride
		}
		type wireCand struct {
			Seed     int64  `json:"seed"`
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
		}
		cands := make([]wireCand, len(imgs))
		for i, rawBytes := range imgs {
			cands[i] = wireCand{
				Seed:     baseSeed + int64(i),
				MimeType: "image/png",
				Data:     base64.StdEncoding.EncodeToString(rawBytes),
			}
		}
		body, _ := json.Marshal(map[string]any{
			"model":          "pixflux-1",
			"license":        license,
			"commercialUse":  commercial,
			"elapsedSeconds": 21,
			"candidates":     cands,
		})
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}},
		}, nil
	})

	clientT, serverT := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Run(ctx, serverT)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return clientT, &calls, &mu
}

func startFixtureSoundGenerator(t *testing.T, license string, commercial bool) sdkmcp.Transport {
	t.Helper()
	wA := makeToneWAV(t, 0.34, -14.1)
	wB := makeToneWAV(t, 0.29, -13.8)
	wC := makeToneWAV(t, 0.61, -9.6)

	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "sfx-gen", Version: "1.0.0"}, nil)
	srv.AddTool(&sdkmcp.Tool{
		Name:        "generate_sound",
		Description: "Generate SFX candidates",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		body, _ := json.Marshal(map[string]any{
			"model":         "sfx-v1",
			"license":       license,
			"commercialUse": commercial,
			"candidates": []map[string]any{
				{"seed": 4410, "mimeType": "audio/wav", "data": base64.StdEncoding.EncodeToString(wA)},
				{"seed": 4411, "mimeType": "audio/wav", "data": base64.StdEncoding.EncodeToString(wB)},
				{"seed": 4412, "mimeType": "audio/wav", "data": base64.StdEncoding.EncodeToString(wC)},
			},
		})
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}},
		}, nil
	})

	clientT, serverT := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Run(ctx, serverT)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return clientT
}

func newAssetGenTestProject(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: dungeon-run\nid: dev.example.dungeon\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gameYAML := `version: 1
name: dungeon-run
id: dev.example.dungeon
appVersion: 0.1.0
window: {width: 640, height: 360}
`
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	briefYAML := `art:
  summary: "16 px pixel art, 4-colour shading, cold stone with warm torch light. 1 px dark outline on characters."
  pixelGrid: 16
  maxColors: 4
  outlineColors: 1
  minContrast: 3.0
audio:
  targetLoudnessDB: -14.0
  maxPeakDB: -0.5
  toleranceDB: 3.0
`
	if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte(briefYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "art", "characters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "audio", "sfx"), 0o755); err != nil {
		t.Fatal(err)
	}
	refSlime := makePixelSpritePNG(t, 16, 12, []color.RGBA{
		{R: 0x5F, G: 0xBF, B: 0x7A, A: 0xFF},
		{R: 0xB4, G: 0xEF, B: 0xC2, A: 0xFF},
		{R: 0x3B, G: 0x8A, B: 0x55, A: 0xFF},
		{R: 0x0F, G: 0x11, B: 0x16, A: 0xFF},
	})
	if err := os.WriteFile(filepath.Join(dir, "art", "characters", "slime.png"), refSlime, 0o644); err != nil {
		t.Fatal(err)
	}
	refJump := makeToneWAV(t, 0.31, -14.0)
	if err := os.WriteFile(filepath.Join(dir, "audio", "sfx", "jump.wav"), refJump, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatal(err)
	}
	ed.SetAssetApprovalAuthor("kim")
	ed.LoadAssetStyleBrief()
	return ed, dir
}

func listAllFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// Acceptance #1: Picking one of four candidates from a fixture MCP generator writes EXACTLY
// the image file and its <file>.source.yaml file (and nothing was written before Pick);
// ONE Undo removes BOTH files.
func TestAssetGeneratorPickWritesExactTwoFilesAndUndoRemovesBoth(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	beforeFiles := listAllFiles(t, dir)

	transport, calls, mu := startFixtureImageGenerator(t, "pixellab-commercial-1.2", true, nil)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{
		Name: "pixel-gen",
		Kind: editor.AssetGenImages,
	}, transport); err != nil {
		t.Fatalf("ConnectAssetGenerator: %v", err)
	}

	req, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/slime_frost.png",
		Generator:  "pixel-gen",
		Prompt:     "ice slime, 16 px pixel art, 4-colour shading, 1 px dark outline, facing right",
		UserPrompt: "An ice slime for floor 3. Same size and rules as slime.png.",
		References: []string{"art/characters/slime.png", "brief.yaml#art"},
		Count:      4,
	})
	if err != nil {
		t.Fatalf("GenerateAsset: %v", err)
	}
	if len(req.Candidates) != 4 {
		t.Fatalf("got %d candidates, want 4", len(req.Candidates))
	}

	// Untrusted rule: NOTHING written to disk before a pick!
	afterGenFiles := listAllFiles(t, dir)
	if !slices.Equal(beforeFiles, afterGenFiles) {
		t.Fatalf("GenerateAsset wrote files before Pick: before=%v after=%v", beforeFiles, afterGenFiles)
	}

	// Verify style checks against brief.yaml and reference slime.png:
	// A & B pass all 4 checks; C fails palette (7 colors); D fails pixel grid (1.5x) and size (24x18).
	for _, cid := range []string{"A", "B"} {
		idx := int(cid[0] - 'A')
		for _, chk := range req.Candidates[idx].Checks {
			if !chk.Pass {
				t.Errorf("candidate %s failed check %q: %s", cid, chk.Title, chk.Detail)
			}
		}
	}
	if req.Candidates[2].Checks[0].Pass {
		t.Fatalf("candidate C (7 colors) unexpectedly passed Palette check: %+v", req.Candidates[2].Checks[0])
	}
	if req.Candidates[3].Checks[1].Pass || req.Candidates[3].Checks[2].Pass {
		t.Fatalf("candidate D (24x18, 1.5x) unexpectedly passed Pixel grid or Size check: %+v", req.Candidates[3].Checks)
	}

	// Pick candidate B.
	if err := ed.PickGeneratedCandidate(req.ID, "B"); err != nil {
		t.Fatalf("PickGeneratedCandidate: %v", err)
	}
	afterPickFiles := listAllFiles(t, dir)
	wantAdded := []string{"art/characters/slime_frost.png", "art/characters/slime_frost.png.source.yaml"}
	var diff []string
	for _, f := range afterPickFiles {
		if !slices.Contains(beforeFiles, f) {
			diff = append(diff, f)
		}
	}
	if !slices.Equal(diff, wantAdded) {
		t.Fatalf("Pick added files %v, want EXACTLY %v", diff, wantAdded)
	}

	// Verify written image decodes cleanly as 16x12 PNG.
	imgBytes, err := os.ReadFile(filepath.Join(dir, "art/characters/slime_frost.png"))
	if err != nil {
		t.Fatal(err)
	}
	decodedImg, err := png.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		t.Fatalf("picked image is not valid PNG: %v", err)
	}
	if decodedImg.Bounds().Dx() != 16 || decodedImg.Bounds().Dy() != 12 {
		t.Fatalf("picked image size = %dx%d, want 16x12", decodedImg.Bounds().Dx(), decodedImg.Bounds().Dy())
	}

	// Verify written .source.yaml parses strictly and round-trips deterministically.
	srcBytes, err := os.ReadFile(filepath.Join(dir, "art/characters/slime_frost.png.source.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := importsettings.ParseAssetSource(srcBytes)
	if err != nil {
		t.Fatalf("ParseAssetSource: %v", err)
	}
	if src.Generator != "pixel-gen" || src.Model != "pixflux-1" || src.Seed == nil || *src.Seed != 81515 ||
		src.License != "pixellab-commercial-1.2" || !src.CommercialUse || src.ApprovedBy != "kim" ||
		!slices.Equal(src.References, []string{"art/characters/slime.png", "brief.yaml#art"}) {
		t.Fatalf("unexpected source metadata: %+v", src)
	}
	canon, err := importsettings.EncodeAssetSource(src)
	if err != nil || !bytes.Equal(srcBytes, canon) {
		t.Fatalf("source YAML not canonical:\ngot:\n%s\nwant:\n%s", srcBytes, canon)
	}

	// ONE Undo removes BOTH files cleanly!
	if !ed.Undo() {
		t.Fatal("ed.Undo() returned false")
	}
	afterUndoFiles := listAllFiles(t, dir)
	if !slices.Equal(beforeFiles, afterUndoFiles) {
		t.Fatalf("after ONE Undo, files = %v, want exact original %v", afterUndoFiles, beforeFiles)
	}

	// Redo restores both files.
	if !ed.Redo() {
		t.Fatal("ed.Redo() returned false")
	}
	afterRedoFiles := listAllFiles(t, dir)
	if !slices.Equal(afterPickFiles, afterRedoFiles) {
		t.Fatalf("after Redo, files = %v, want %v", afterRedoFiles, afterPickFiles)
	}

	// Verify Vary this, Refine with words, and Regenerate N re-invoke generator with the same references.
	if _, err := ed.VaryGeneratedCandidate(context.Background(), req.ID, "B"); err != nil {
		t.Fatalf("VaryGeneratedCandidate: %v", err)
	}
	if _, err := ed.RefineGeneratedAsset(context.Background(), req.ID, "frost only on top"); err != nil {
		t.Fatalf("RefineGeneratedAsset: %v", err)
	}
	if _, err := ed.RegenerateAsset(context.Background(), req.ID); err != nil {
		t.Fatalf("RegenerateAsset: %v", err)
	}

	mu.Lock()
	gotCalls := slices.Clone(*calls)
	mu.Unlock()
	if len(gotCalls) != 4 {
		t.Fatalf("generator call count = %d, want 4 (generate, vary, refine, regenerate)", len(gotCalls))
	}
	wantRefs := []string{"art/characters/slime.png", "brief.yaml#art"}
	for i, c := range gotCalls {
		if !slices.Equal(c.References, wantRefs) {
			t.Errorf("call %d references = %v, want %v", i, c.References, wantRefs)
		}
	}
	if gotCalls[1].VarySeed == nil || *gotCalls[1].VarySeed != 81515 {
		t.Errorf("Vary call vary_seed = %v, want 81515", gotCalls[1].VarySeed)
	}
	if gotCalls[2].Refine != "frost only on top" {
		t.Errorf("Refine call refine = %q, want %q", gotCalls[2].Refine, "frost only on top")
	}
}

func TestAssetGeneratorRejectsUntrustedNonImagePayload(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()
	beforeFiles := listAllFiles(t, dir)

	malicious := [][]byte{[]byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`)}
	transport, _, _ := startFixtureImageGenerator(t, "CC0-1.0", true, malicious)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{
		Name: "pixel-gen",
		Kind: editor.AssetGenImages,
	}, transport); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/evil.png",
		Generator:  "pixel-gen",
		Prompt:     "evil",
	}); err == nil || !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("expected untrusted non-image error, got %v", err)
	}
	if after := listAllFiles(t, dir); !slices.Equal(beforeFiles, after) {
		t.Fatalf("rejected payload wrote files: %v", after)
	}
}

// Acceptance #2: A generated file with an unknown license blocks a release build with its path in the error message.
func TestGeneratedAssetWithUnknownLicenseBlocksReleaseBuild(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	sndTransport := startFixtureSoundGenerator(t, "unknown", false)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{
		Name: "sfx-gen",
		Kind: editor.AssetGenSound,
	}, sndTransport); err != nil {
		t.Fatal(err)
	}

	req, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "audio/sfx/torch_flicker.wav",
		Generator:  "sfx-gen",
		Kind:       editor.AssetGenSound,
		Prompt:     "warm dungeon torch crackle",
		References: []string{"audio/sfx/jump.wav"},
		Count:      3,
	})
	if err != nil {
		t.Fatalf("GenerateAsset sound: %v", err)
	}
	if len(req.Candidates) != 3 {
		t.Fatalf("sound candidates = %d, want 3", len(req.Candidates))
	}
	if !req.Candidates[0].AudioCheckPass || !req.Candidates[1].AudioCheckPass {
		t.Fatalf("candidates A/B should pass loudness check: A=%+v B=%+v", req.Candidates[0], req.Candidates[1])
	}
	if req.Candidates[2].AudioCheckPass {
		t.Fatalf("candidate C (-9.6 dBFS vs -14 dBFS jump.wav) should fail loudness check: %+v", req.Candidates[2])
	}

	// Audition sound candidate A in memory.
	if err := ed.PlayGeneratedSoundCandidate(req.ID, "A"); err != nil {
		t.Fatalf("PlayGeneratedSoundCandidate: %v", err)
	}
	if pr, pc := ed.PlayingGeneratedSound(); pr != req.ID || pc != "A" {
		t.Fatalf("PlayingGeneratedSound = (%q, %q), want (%q, A)", pr, pc, req.ID)
	}
	ed.StopGeneratedSoundCandidate()

	// Pick candidate A (has license: unknown).
	if err := ed.PickGeneratedCandidate(req.ID, "A"); err != nil {
		t.Fatalf("PickGeneratedCandidate: %v", err)
	}

	// Release build MUST fail with *credits.UnknownLicenseError naming audio/sfx/torch_flicker.wav!
	_, buildErr := releaseplan.Build(dir, releaseplan.Options{}, releaseplan.SettingsFromProject(ed.Project))
	var unk *credits.UnknownLicenseError
	if !errors.As(buildErr, &unk) {
		t.Fatalf("releaseplan.Build error = %v, want *credits.UnknownLicenseError", buildErr)
	}
	if !strings.Contains(buildErr.Error(), "audio/sfx/torch_flicker.wav") {
		t.Fatalf("releaseplan.Build error %q missing blocked asset path", buildErr.Error())
	}

	// Export panel pre-flight credits check also reports the blocking error.
	rep := ed.CheckExportCredits()
	if rep.OK() || rep.Err == nil || !strings.Contains(rep.Err.Error(), "audio/sfx/torch_flicker.wav") {
		t.Fatalf("ExportCredits = %+v, want blocking error naming audio/sfx/torch_flicker.wav", rep)
	}

	// Setting license via editor unblocks the release build!
	if err := ed.SetGeneratedAssetLicense("audio/sfx/torch_flicker.wav", "CC0-1.0", true); err != nil {
		t.Fatalf("SetGeneratedAssetLicense: %v", err)
	}
	if _, err := releaseplan.Build(dir, releaseplan.Options{}, releaseplan.SettingsFromProject(ed.Project)); err != nil {
		t.Fatalf("releaseplan.Build after SetGeneratedAssetLicense: %v", err)
	}
}

func TestAssetGeneratorScreenUIInteraction(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	imgT, _, _ := startFixtureImageGenerator(t, "pixellab-commercial-1.2", true, nil)
	sndT := startFixtureSoundGenerator(t, "sfx-gen-standard", true)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "pixel-gen", Kind: editor.AssetGenImages}, imgT); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "sfx-gen", Kind: editor.AssetGenSound}, sndT); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/slime_frost.png",
		Generator:  "pixel-gen",
		Prompt:     "ice slime, 16 px pixel art, 4-colour shading, 1 px dark outline",
		UserPrompt: "An ice slime for floor 3. Same size and rules as slime.png.",
		References: []string{"art/characters/slime.png", "brief.yaml#art"},
		Count:      4,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "audio/sfx/slime_freeze.wav",
		Generator:  "sfx-gen",
		Kind:       editor.AssetGenSound,
		Prompt:     "the ice slime's freeze attack, short and crisp",
		References: []string{"audio/sfx/jump.wav"},
		Count:      3,
	}); err != nil {
		t.Fatal(err)
	}
	ed.SetAssetGeneratorOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	if app.Find("id:asset-gen-screen") == nil {
		t.Fatal("asset-gen-screen not rendered")
	}
	if notice := app.Find("id:asset-gen-untrusted-notice"); notice == nil || !strings.Contains(notice.Text, "untrusted") {
		t.Fatalf("missing untrusted notice: %+v", notice)
	}

	// Switch preview background to Checker and back to Floor 3.
	app.Press("id:asset-gen-bg-checker")
	if !ed.AssetGenPreviewBackground().Checker {
		t.Fatal("preview background did not switch to checker")
	}
	app.Press("id:asset-gen-bg-floor3")

	// Pick candidate B via UI button.
	app.Press("id:asset-gen-pick-B")
	if _, err := os.Stat(filepath.Join(dir, "art/characters/slime_frost.png")); err != nil {
		t.Fatalf("UI Pick B did not write image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "art/characters/slime_frost.png.source.yaml")); err != nil {
		t.Fatalf("UI Pick B did not write source sidecar: %v", err)
	}

	// Pressing the same button ("Undo pick") removes both files!
	app.Press("id:asset-gen-pick-B")
	if _, err := os.Stat(filepath.Join(dir, "art/characters/slime_frost.png")); !os.IsNotExist(err) {
		t.Fatalf("UI Undo pick did not remove image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "art/characters/slime_frost.png.source.yaml")); !os.IsNotExist(err) {
		t.Fatalf("UI Undo pick did not remove source sidecar: %v", err)
	}
}
