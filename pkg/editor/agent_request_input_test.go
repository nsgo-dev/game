package editor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

func makeTinyTestPNG(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := range 6 {
		for x := range 8 {
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func setupRequestContractProject(t *testing.T) (string, *Editor) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"scenes", "prefabs", "art", "rules"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: slime-quest
id: com.example.slimequest
appVersion: 0.1.0
window:
  title: Slime Quest
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte("title: Slime Quest\ngoals:\n  - BRIEF-MARKER-CRISP-COMBAT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Rules\n- RULES-MARKER-DETERMINISTIC-EDITS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prefabs/slime.prefab.yaml"), []byte("version: 1\nname: slime\nPREFAB-MARKER-SPEED: 40\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "art/slime.png"), makeTinyTestPNG(t, 0x33, 0xcc, 0x55), 0o644); err != nil {
		t.Fatal(err)
	}
	scenePath := filepath.Join(dir, "scenes/level_01.scene.yaml")
	if err := os.WriteFile(scenePath, []byte(`version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    objects:
      - id: slime_01
        name: Slime
        x: 120
        y: 200
        properties:
          speed: 42
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.WaitForProblems()
	ed.problems.problems = []assetcheck.Problem{
		{
			File:    "scenes/level_01.scene.yaml",
			Line:    12,
			Object:  "slime_01",
			Message: "PROBLEM-MARKER-UNBOUNDED-PATROL",
		},
	}
	ed.AddComposerPlaytestRun(ComposerPlaytestRun{
		ID:      "run-42",
		Name:    "Floor 1 smoke",
		Summary: "2 deaths",
		Detail:  "PLAYTEST-MARKER-DEATH-AT-SPIKES",
	})
	return dir, ed
}

func TestRenderAgentModelInputContract(t *testing.T) {
	t.Run("empty context preserves prompt verbatim", func(t *testing.T) {
		in := RenderAgentModelInput(AgentRequest{Prompt: "--dangerously-skip-permissions"})
		if in.AlwaysContext != "" || in.TurnContext != "" || in.Context != "" {
			t.Fatalf("expected empty context, got always=%q turn=%q full=%q", in.AlwaysContext, in.TurnContext, in.Context)
		}
		if in.TurnPrompt != "--dangerously-skip-permissions" || in.FullPrompt != "--dangerously-skip-permissions" {
			t.Fatalf("prompt altered: turn=%q full=%q", in.TurnPrompt, in.FullPrompt)
		}
		if len(in.Images) != 0 || in.HasCompactedSummary {
			t.Fatalf("unexpected images=%d summary=%v", len(in.Images), in.HasCompactedSummary)
		}
	})

	t.Run("manual AssembledContext and ViewportImage repro", func(t *testing.T) {
		vp := makeTinyTestPNG(t, 0xaa, 0x22, 0x44)
		req := AgentRequest{
			Prompt:           "make the slime faster",
			AssembledContext: "# level_01.scene.yaml · lines 8–14\nMARKER-CHIP-CONTEXT",
			ViewportImage:    vp,
			ViewportWidth:    8,
			ViewportHeight:   6,
			HasViewportImg:   true,
		}
		in := RenderAgentModelInput(req)
		if !strings.Contains(in.TurnPrompt, "MARKER-CHIP-CONTEXT") || !strings.Contains(in.TurnPrompt, "make the slime faster") {
			t.Fatalf("TurnPrompt missing context or prompt: %q", in.TurnPrompt)
		}
		if !strings.Contains(in.FullPrompt, "MARKER-CHIP-CONTEXT") || !strings.Contains(in.FullPrompt, "make the slime faster") {
			t.Fatalf("FullPrompt missing context or prompt: %q", in.FullPrompt)
		}
		if len(in.Images) != 1 || in.Images[0].ID != viewportImageChipID || !bytes.Equal(in.Images[0].Data, vp) {
			t.Fatalf("viewport image missing from RenderAgentModelInput: %+v", in.Images)
		}
	})

	t.Run("assembled composer request splits Always from TurnContext and includes all chips and images", func(t *testing.T) {
		dir, ed := setupRequestContractProject(t)
		ed.AddAgentThreadTurn(AgentThreadTurn{Prompt: "Place floor", Reply: "COMPACT-MARKER-PLACED-FLOOR"})
		ed.AddAgentThreadTurn(AgentThreadTurn{Prompt: "Add lever", Reply: "Added lever_01"})
		if _, err := ed.CompactAgentThread(); err != nil {
			t.Fatalf("CompactAgentThread: %v", err)
		}
		ed.SelectObject("slime_01")
		if _, ok := ed.AcceptSuggestedChip(); !ok {
			t.Fatal("AcceptSuggestedChip failed")
		}
		for _, m := range ed.SearchMentions("slime", MentionGroupPrefabs) {
			ed.AcceptMentionItem(m.ID)
		}
		for _, m := range ed.SearchMentions("", MentionGroupProblems) {
			ed.AcceptMentionItem(m.ID)
		}
		for _, m := range ed.SearchMentions("", MentionGroupPlaytests) {
			ed.AcceptMentionItem(m.ID)
		}
		for _, m := range ed.SearchMentions("slime", MentionGroupAssets) {
			ed.AcceptMentionItem(m.ID)
		}
		ed.SetViewportImageIncluded(true)

		req := ed.AssembleAgentRequest("make the slime faster")
		req.ProjectRoot = dir
		in := RenderAgentModelInput(req)

		if !strings.Contains(in.AlwaysContext, "BRIEF-MARKER-CRISP-COMBAT") || !strings.Contains(in.AlwaysContext, "RULES-MARKER-DETERMINISTIC-EDITS") {
			t.Fatalf("AlwaysContext missing brief/rules: %q", in.AlwaysContext)
		}
		if strings.Contains(in.TurnContext, "BRIEF-MARKER-CRISP-COMBAT") || strings.Contains(in.TurnContext, "RULES-MARKER-DETERMINISTIC-EDITS") {
			t.Fatalf("TurnContext duplicated Always chips: %q", in.TurnContext)
		}
		for _, want := range []string{
			"# Compacted earlier requests",
			"COMPACT-MARKER-PLACED-FLOOR",
			"id: slime_01",
			"speed: 42",
			"PREFAB-MARKER-SPEED: 40",
			"PROBLEM-MARKER-UNBOUNDED-PATROL",
			"PLAYTEST-MARKER-DEATH-AT-SPIKES",
			"# Viewport image",
		} {
			if !strings.Contains(in.TurnContext, want) {
				t.Errorf("TurnContext missing %q:\n%s", want, in.TurnContext)
			}
			if !strings.Contains(in.FullPrompt, want) {
				t.Errorf("FullPrompt missing %q:\n%s", want, in.FullPrompt)
			}
		}
		for _, wantAlways := range []string{"BRIEF-MARKER-CRISP-COMBAT", "RULES-MARKER-DETERMINISTIC-EDITS"} {
			if !strings.Contains(in.FullPrompt, wantAlways) {
				t.Errorf("FullPrompt missing Always chip %q", wantAlways)
			}
		}
		if !in.HasCompactedSummary {
			t.Fatal("expected HasCompactedSummary = true")
		}

		// Viewport PNG + attached image-asset chip PNG both present.
		var sawViewport, sawAsset bool
		for _, img := range in.Images {
			if img.ID == viewportImageChipID && len(img.Data) > 0 {
				sawViewport = true
			}
			if img.Path == "art/slime.png" && len(img.Data) > 0 {
				sawAsset = true
			}
		}
		if !sawViewport || !sawAsset {
			t.Fatalf("expected viewport and asset images in input.Images (sawViewport=%v sawAsset=%v): %+v", sawViewport, sawAsset, in.Images)
		}
	})
}
