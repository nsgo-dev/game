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
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
	"github.com/nsgo-dev/game/pkg/tooling/credits"
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

func makeMinimalValidOGG() []byte {
	// Minimal OggS page carrying a valid Vorbis identification packet (1 ch, 48000 Hz).
	pkt := make([]byte, 30)
	pkt[0] = 1
	copy(pkt[1:7], "vorbis")
	pkt[11] = 1 // channels
	// sampleRate = 48000 (0x0000BB80) little-endian at bytes 12..15
	pkt[12] = 0x80
	pkt[13] = 0xBB
	pkt[14] = 0x00
	pkt[15] = 0x00

	page := make([]byte, 27+1+len(pkt))
	copy(page[0:4], "OggS")
	page[4] = 0 // version
	// granule = 24000 (0.5s at 48000 Hz) at bytes 6..13
	page[6] = 0xC0
	page[7] = 0x5D
	page[26] = 1 // 1 segment
	page[27] = byte(len(pkt))
	copy(page[28:], pkt)
	return page
}

func TestAssetGeneratorPathTraversalAndSymlinkContainment(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.png")
	if err := os.WriteFile(outsideFile, []byte("DO_NOT_OVERWRITE"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideRefPNG := filepath.Join(outsideDir, "outside_ref.png")
	if err := os.WriteFile(outsideRefPNG, makePixelSpritePNG(t, 16, 12, []color.RGBA{{R: 255, G: 0, B: 0, A: 255}}), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideSource := filepath.Join(outsideDir, "outside.source.yaml")
	if err := os.WriteFile(outsideSource, []byte("version: 1\ngenerator: x\nprompt: p\nlicense: unknown\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideBrief := filepath.Join(outsideDir, "outside_brief.yaml")
	if err := os.WriteFile(outsideBrief, []byte("art:\n  summary: ESCAPED_BRIEF\n  pixelGrid: 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Symlinked brief.yaml pointing outside project root must be ignored.
	if err := os.Remove(filepath.Join(dir, "brief.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideBrief, filepath.Join(dir, "brief.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	brief := ed.LoadAssetStyleBrief()
	if strings.Contains(brief.Summary, "ESCAPED_BRIEF") {
		t.Fatalf("LoadAssetStyleBrief followed symlink outside project root: %+v", brief)
	}

	// 2. Reference path traversal and symlinked reference must be refused cleanly.
	if err := os.Symlink(outsideRefPNG, filepath.Join(dir, "art", "characters", "escaped_ref.png")); err != nil {
		t.Fatal(err)
	}
	imgT, _, _ := startFixtureImageGenerator(t, "CC0-1.0", true, nil)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "pixel-gen", Kind: editor.AssetGenImages}, imgT); err != nil {
		t.Fatal(err)
	}
	reqRef, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/slime_refcheck.png",
		Generator:  "pixel-gen",
		Prompt:     "slime",
		References: []string{"../../" + filepath.Base(outsideDir) + "/outside_ref.png", "art/characters/escaped_ref.png"},
		Count:      2,
	})
	if err != nil {
		t.Fatalf("GenerateAsset: %v", err)
	}
	if reqRef.ReferenceAsset != "" || reqRef.RefWidth != 0 {
		t.Fatalf("loadRequestReference read outside reference: asset=%q width=%d", reqRef.ReferenceAsset, reqRef.RefWidth)
	}

	// 3. PickGeneratedCandidate must refuse symlinked target file and symlinked parent dir.
	if err := os.Symlink(outsideFile, filepath.Join(dir, "art", "characters", "symlink_target.png")); err != nil {
		t.Fatal(err)
	}
	reqFileLink, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/symlink_target.png",
		Generator:  "pixel-gen",
		Prompt:     "slime",
		Count:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.PickGeneratedCandidate(reqFileLink.ID, "A"); err == nil {
		t.Fatal("PickGeneratedCandidate succeeded through symlink target file, want error")
	}
	gotOutside, _ := os.ReadFile(outsideFile)
	if string(gotOutside) != "DO_NOT_OVERWRITE" {
		t.Fatalf("outside file was modified through target symlink: %q", string(gotOutside))
	}

	if err := os.Symlink(outsideDir, filepath.Join(dir, "art", "escaped_dir")); err != nil {
		t.Fatal(err)
	}
	reqDirLink, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/escaped_dir/new_sprite.png",
		Generator:  "pixel-gen",
		Prompt:     "slime",
		Count:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.PickGeneratedCandidate(reqDirLink.ID, "A"); err == nil {
		t.Fatal("PickGeneratedCandidate succeeded through symlink parent dir, want error")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new_sprite.png")); !os.IsNotExist(err) {
		t.Fatalf("PickGeneratedCandidate wrote outside project through symlink dir: %v", err)
	}

	// 4. SetGeneratedAssetLicense must refuse symlinked .source.yaml.
	if err := os.Symlink(outsideSource, filepath.Join(dir, "art", "characters", "lic_escape.png.source.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetGeneratedAssetLicense("art/characters/lic_escape.png", "CC0-1.0", true); err == nil {
		t.Fatal("SetGeneratedAssetLicense succeeded through symlink sidecar, want error")
	}
	gotSrc, _ := os.ReadFile(outsideSource)
	if strings.Contains(string(gotSrc), "CC0-1.0") {
		t.Fatalf("outside source file was modified through symlink: %q", string(gotSrc))
	}
}

func TestAssetGeneratorUntrustedPayloadCapsAndOGG(t *testing.T) {
	ed, _ := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	validPNG := makePixelSpritePNG(t, 16, 12, []color.RGBA{{R: 0x7C, G: 0xC4, B: 0xE8, A: 0xFF}})
	validB64 := base64.StdEncoding.EncodeToString(validPNG)

	// 1. > 16 candidates rejected before decoding.
	srvMany := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "many-gen", Version: "1.0.0"}, nil)
	srvMany.AddTool(&sdkmcp.Tool{
		Name:        "generate_image",
		Description: "Too many candidates",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		cands := make([]map[string]any, 17)
		for i := range cands {
			cands[i] = map[string]any{"seed": i + 1, "mimeType": "image/png", "data": validB64}
		}
		raw, _ := json.Marshal(map[string]any{"model": "m", "candidates": cands})
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(raw)}}}, nil
	})
	st1, ct1 := sdkmcp.NewInMemoryTransports()
	if _, err := srvMany.Connect(context.Background(), st1, nil); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "many-gen", Kind: editor.AssetGenImages}, ct1); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/too_many.png",
		Generator:  "many-gen",
		Prompt:     "slime",
	}); err == nil || !strings.Contains(err.Error(), "exceeding limit") {
		t.Fatalf("GenerateAsset with 17 candidates err = %v, want limit error", err)
	}

	// 2. Oversized base64 string rejected BEFORE decoding.
	srvHuge := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "huge-gen", Version: "1.0.0"}, nil)
	srvHuge.AddTool(&sdkmcp.Tool{
		Name:        "generate_image",
		Description: "Oversized base64",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		hugeB64 := strings.Repeat("A", base64.StdEncoding.EncodedLen(16<<20)+16)
		raw, _ := json.Marshal(map[string]any{
			"model":      "m",
			"candidates": []map[string]any{{"seed": 1, "mimeType": "image/png", "data": hugeB64}},
		})
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(raw)}}}, nil
	})
	st2, ct2 := sdkmcp.NewInMemoryTransports()
	if _, err := srvHuge.Connect(context.Background(), st2, nil); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "huge-gen", Kind: editor.AssetGenImages}, ct2); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/too_big.png",
		Generator:  "huge-gen",
		Prompt:     "slime",
	}); err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("GenerateAsset with oversized base64 err = %v, want size limit error", err)
	}

	// 3. Server-attempted prompt rewrite is ignored, huge ElapsedSeconds is clamped, and OGG loudness is marked unmeasured (not fabricated -14.0 dBFS).
	srvOGG := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "ogg-gen", Version: "1.0.0"}, nil)
	srvOGG.AddTool(&sdkmcp.Tool{
		Name:        "generate_sound",
		Description: "Generate OGG with spoofed prompt and extreme elapsed",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		raw, _ := json.Marshal(map[string]any{
			"model":          "ogg-v1\ninjected",
			"prompt":         "SPOOFED PROMPT BY SERVER",
			"license":        "CC0-1.0\r\nforged: true",
			"commercialUse":  true,
			"elapsedSeconds": 1e15,
			"candidates": []map[string]any{
				{"seed": 42, "mimeType": "audio/ogg", "data": base64.StdEncoding.EncodeToString(makeMinimalValidOGG())},
			},
		})
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(raw)}}}, nil
	})
	st3, ct3 := sdkmcp.NewInMemoryTransports()
	if _, err := srvOGG.Connect(context.Background(), st3, nil); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "ogg-gen", Kind: editor.AssetGenSound}, ct3); err != nil {
		t.Fatal(err)
	}
	oggReq, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "audio/sfx/chime.ogg",
		Generator:  "ogg-gen",
		Kind:       editor.AssetGenSound,
		Prompt:     "crisp crystal chime",
	})
	if err != nil {
		t.Fatalf("GenerateAsset OGG: %v", err)
	}
	if oggReq.Prompt != "crisp crystal chime" {
		t.Fatalf("server overwrote prompt: got %q, want %q", oggReq.Prompt, "crisp crystal chime")
	}
	if oggReq.Elapsed > 24*time.Hour || oggReq.Elapsed <= 0 {
		t.Fatalf("Elapsed not clamped cleanly: %v", oggReq.Elapsed)
	}
	if strings.ContainsAny(oggReq.Model+oggReq.License, "\r\n") {
		t.Fatalf("control chars not stripped from metadata: model=%q license=%q", oggReq.Model, oggReq.License)
	}
	if len(oggReq.Candidates) != 1 {
		t.Fatalf("got %d OGG candidates, want 1", len(oggReq.Candidates))
	}
	cand := oggReq.Candidates[0]
	if cand.AudioLoudnessMeasured {
		t.Fatal("OGG candidate reported AudioLoudnessMeasured=true, want false")
	}
	if !math.IsNaN(cand.LoudnessDBFS) {
		t.Fatalf("OGG candidate fabricated LoudnessDBFS=%v, want NaN", cand.LoudnessDBFS)
	}
	if !strings.Contains(strings.ToLower(cand.AudioCheckText), "unmeasured") {
		t.Fatalf("OGG AudioCheckText = %q, want unmeasured notice", cand.AudioCheckText)
	}
}

func TestAssetGeneratorProjectSwitchResetAndAsyncCancel(t *testing.T) {
	ed, _ := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	// Create a second valid project directory to switch to.
	ed2, dir2 := newAssetGenTestProject(t)
	ed2.DisconnectAllAssetGenerators()

	// Slow MCP server that blocks until cancelled.
	started := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	srvSlow := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "slow-gen", Version: "1.0.0"}, nil)
	srvSlow.AddTool(&sdkmcp.Tool{
		Name:        "generate_image",
		Description: "Slow generator",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		select {
		case canceled <- struct{}{}:
		default:
		}
		return nil, ctx.Err()
	})
	st, ct := sdkmcp.NewInMemoryTransports()
	if _, err := srvSlow.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	imgT, _, _ := startFixtureImageGenerator(t, "CC0-1.0", true, nil)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "pixel-gen", Kind: editor.AssetGenImages}, imgT); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "slow-gen", Kind: editor.AssetGenImages}, ct); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/slime_frost.png",
		Generator:  "pixel-gen",
		Prompt:     "frost slime",
		Count:      2,
	}); err != nil {
		t.Fatal(err)
	}
	ed.SetAssetGeneratorOpen(true)

	// Launch a concurrent blocking call on slow-gen to verify OpenProject cancels/invalidates cleanly under -race.
	errCh := make(chan error, 1)
	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()
	go func() {
		_, err := ed.GenerateAsset(callCtx, editor.GenerateAssetParams{
			TargetPath: "art/characters/stale.png",
			Generator:  "slow-gen",
			Prompt:     "stale",
		})
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("slow generator never started")
	}

	// Switch project: must close Generated Assets screen, clear requests, disconnect servers, and invalidate stale calls!
	if err := ed.OpenProject(dir2); err != nil {
		t.Fatalf("OpenProject(dir2): %v", err)
	}
	if ed.AssetGeneratorOpen() {
		t.Fatal("AssetGeneratorOpen still true after OpenProject")
	}
	if reqs := ed.GeneratedAssetRequests(); len(reqs) != 0 {
		t.Fatalf("GeneratedAssetRequests not cleared after OpenProject: got %d", len(reqs))
	}
	if srvs := ed.AssetGenerators(); len(srvs) != 0 {
		t.Fatalf("AssetGenerators not disconnected after OpenProject: got %d", len(srvs))
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("stale GenerateAsset across OpenProject succeeded, want error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stale GenerateAsset did not unblock after OpenProject")
	}

	// Also verify OpenDemo resets cleanly.
	ed.SetAssetGeneratorOpen(true)
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.AssetGeneratorOpen() {
		t.Fatal("AssetGeneratorOpen still true after OpenDemo")
	}
}

func TestAssetGeneratorInlineLicenseEditorNoHardcodedCC0(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	sndT := startFixtureSoundGenerator(t, "unknown", false)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "sfx-gen", Kind: editor.AssetGenSound}, sndT); err != nil {
		t.Fatal(err)
	}
	req, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "audio/sfx/torch_flicker.wav",
		Generator:  "sfx-gen",
		Kind:       editor.AssetGenSound,
		Prompt:     "torch crackle",
		Count:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.PickGeneratedCandidate(req.ID, "A"); err != nil {
		t.Fatal(err)
	}
	ed.SetAssetGeneratorOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	// Clicking "Set license" MUST open the inline license editor and NOT silently write "CC0-1.0"!
	app.Press("id:asset-gen-set-license-0")
	if app.Find("id:asset-gen-license-editor") == nil {
		t.Fatal("clicking Set license did not open inline license editor")
	}
	srcPath := filepath.Join(dir, "audio/sfx/torch_flicker.wav.source.yaml")
	rawBefore, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawBefore), "CC0-1.0") {
		t.Fatalf("clicking Set license fabricated CC0-1.0 without user input:\n%s", string(rawBefore))
	}

	// User enters explicit license + enables commercial use + presses Save.
	app.Type("id:asset-gen-license-input", "Apache-2.0")
	app.Press("id:asset-gen-license-commercial")
	app.Press("id:asset-gen-license-save")

	rawAfter, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	src, err := importsettings.ParseAssetSource(rawAfter)
	if err != nil {
		t.Fatalf("ParseAssetSource after Save: %v", err)
	}
	if src.License != "Apache-2.0" || !src.CommercialUse {
		t.Fatalf("saved license = (%q, commercial=%v), want (Apache-2.0, true)", src.License, src.CommercialUse)
	}
}

func TestAssetGeneratorSafeUndoPickAfterUnrelatedEdit(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	imgT, _, _ := startFixtureImageGenerator(t, "CC0-1.0", true, nil)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "pixel-gen", Kind: editor.AssetGenImages}, imgT); err != nil {
		t.Fatal(err)
	}
	req, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/slime_frost.png",
		Generator:  "pixel-gen",
		Prompt:     "frost slime",
		Count:      2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.PickGeneratedCandidate(req.ID, "B"); err != nil {
		t.Fatal(err)
	}

	// Perform an unrelated edit on Editor.History AFTER picking candidate B!
	placedID, err := ed.PlaceObject("main", sceneformat.ObjectDoc{
		ID:   "hero_after_pick",
		Name: "hero_renamed_after_pick",
	})
	if err != nil {
		t.Fatalf("PlaceObject: %v", err)
	}

	// Now undo ONLY the picked asset via UndoPickedCandidate (the exact handler wired to the "Undo pick" button).
	if err := ed.UndoPickedCandidate(req.ID, "B"); err != nil {
		t.Fatalf("UndoPickedCandidate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "art/characters/slime_frost.png")); !os.IsNotExist(err) {
		t.Fatalf("UndoPickedCandidate did not remove picked image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "art/characters/slime_frost.png.source.yaml")); !os.IsNotExist(err) {
		t.Fatalf("UndoPickedCandidate did not remove picked source sidecar: %v", err)
	}

	// The unrelated scene edit MUST still be intact!
	obj, _, _ := ed.FindObject(placedID)
	if obj == nil || obj.Name != "hero_renamed_after_pick" {
		t.Fatalf("UndoPickedCandidate clobbered unrelated History edit: obj=%+v", obj)
	}

	// And Cmd+Z on the unpick restores the picked files cleanly!
	ed.Undo()
	if _, err := os.Stat(filepath.Join(dir, "art/characters/slime_frost.png")); err != nil {
		t.Fatalf("Undo after UndoPickedCandidate did not restore image: %v", err)
	}
}

func TestAssetGeneratorGuardsAndLicenseParseError(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()

	imgT, _, _ := startFixtureImageGenerator(t, "CC0-1.0", true, nil)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "pixel-gen", Kind: editor.AssetGenImages}, imgT); err != nil {
		t.Fatal(err)
	}
	req, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/slime_frost.png",
		Generator:  "pixel-gen",
		Prompt:     "frost slime",
		Count:      2,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Malformed .source.yaml surfaces scan error in GeneratedAssetLicenseError() and UI.
	badSidecar := filepath.Join(dir, "art/characters/broken.png.source.yaml")
	if err := os.WriteFile(badSidecar, []byte("version: 99\nunknownField: bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed.RefreshGeneratedAssetLicenses()
	if errMsg := ed.GeneratedAssetLicenseError(); errMsg == "" {
		t.Fatal("GeneratedAssetLicenseError() empty for malformed .source.yaml")
	}
	ed.SetAssetGeneratorOpen(true)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	if app.Find("id:asset-gen-license-error") == nil {
		t.Fatal("asset-gen-license-error not rendered for malformed .source.yaml")
	}
	_ = os.Remove(badSidecar)
	ed.RefreshGeneratedAssetLicenses()
	if errMsg := ed.GeneratedAssetLicenseError(); errMsg != "" {
		t.Fatalf("GeneratedAssetLicenseError() not cleared after fixing file: %q", errMsg)
	}
	ed.CloseAssetGenerator()

	// 2. Play mode blocks generation, picking, unpicking, and license editing.
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	defer ed.Stop()
	if _, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/characters/while_playing.png",
		Generator:  "pixel-gen",
		Prompt:     "slime",
	}); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("GenerateAsset while playing = %v, want ErrPlaying", err)
	}
	if err := ed.PickGeneratedCandidate(req.ID, "A"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("PickGeneratedCandidate while playing = %v, want ErrPlaying", err)
	}
	if err := ed.UndoPickedCandidate(req.ID, "A"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("UndoPickedCandidate while playing = %v, want ErrPlaying", err)
	}
	if err := ed.SetGeneratedAssetLicense("art/characters/slime_frost.png", "CC0-1.0", true); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SetGeneratedAssetLicense while playing = %v, want ErrPlaying", err)
	}
}
