package editor_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func renderReferenceArtPNG(t *testing.T, w, h int, accent color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		// Warm dungeon stone gradient matching Sketch.dc.html spikes_ref.png thumbnail.
		tVal := float64(y) / float64(max(1, h-1))
		r := uint8(0x3B - int(tVal*0x11))
		g := uint8(0x35 - int(tVal*0x10))
		b := uint8(0x30 - int(tVal*0x10))
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{r, g, b, 0xFF})
		}
	}
	// Three iron spikes along the floor.
	floorY := h - h/5
	for y := floorY; y < h; y++ {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{0x1F, 0x23, 0x2A, 0xFF})
		}
	}
	for s := range 3 {
		cx := (s*2 + 1) * w / 6
		tipY := h / 4
		baseHalf := w / 9
		for y := tipY; y < floorY; y++ {
			half := (y - tipY) * baseHalf / max(1, floorY-tipY)
			for x := cx - half; x <= cx+half; x++ {
				if x >= 0 && x < w {
					if x <= cx-half+1 {
						img.SetRGBA(x, y, color.RGBA{0xE0, 0xA8, 0x7A, 0xFF})
					} else {
						img.SetRGBA(x, y, accent)
					}
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestAgentReferenceImagesMacOSScreenshots captures real macOS screenshots of the Agent composer's
// reference image drop overlay, attached chip with hover preview, Pin to brief YAML diff card,
// and unsupported-type refusal banner beside design/editor/Sketch.dc.html and Spec.dc.html.
func TestAgentReferenceImagesMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: dungeon-run\nid: dev.example.dungeon\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: dungeon-run\nid: dev.example.dungeon\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initialBrief := "art:\n  direction: chunky pixel art, 16 px tiles\n  references:\n    - path: art/reference/torch_ref.png\n"
	if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte(initialBrief), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "art", "reference"), 0o755); err != nil {
		t.Fatal(err)
	}
	torchPNG := renderReferenceArtPNG(t, 64, 48, color.RGBA{0xE9, 0xA8, 0x4A, 0xFF})
	if err := os.WriteFile(filepath.Join(dir, "art", "reference", "torch_ref.png"), torchPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ed.Close() })
	ed.SetAppearance(editor.AppearanceDark)
	ed.SetLanguage(editor.LanguageEnglish)
	if err := ed.OpenDemo(); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, _ editor.AgentRequest, emit func(editor.AgentEvent)) error {
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "1", StepTitle: "Read level_01.scene.yaml and brief.yaml", StepStatus: editor.AgentStepDone, StepIndex: 1, StepTotal: 2})
		emit(editor.AgentEvent{Kind: editor.AgentEventStep, StepID: "2", StepTitle: "Placed 3 rusty iron spikes along the lower floor", StepStatus: editor.AgentStepDone, StepIndex: 2, StepTotal: 2})
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Done. Spikes match spikes_ref.png."})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	onUI := func(fn func()) {
		var done atomic.Bool
		ed.Post(func() {
			defer done.Store(true)
			fn()
		})
		app.WaitUntil(5*time.Second, done.Load)
	}

	// Seed one completed turn with a reference thumbnail so the thread looks like a real session.
	spikesPNG := renderReferenceArtPNG(t, 160, 96, color.RGBA{0xA2, 0x6B, 0x45, 0xFF})
	var stepErr error
	onUI(func() {
		if _, err := ed.AttachAgentReferenceBytes("torch_wall.png", torchPNG); err != nil {
			stepErr = err
			return
		}
		stepErr = ed.SubmitAgentPrompt("Add a torch beside the door.")
	})
	if stepErr != nil {
		t.Fatal(stepErr)
	}
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("id:agent-turn-summary-0")
	})

	// 1. Composer with attached spikes_ref.png chip + hover preview open + Always brief.yaml chip.
	var img editor.AgentReferenceImage
	onUI(func() {
		var err error
		img, err = ed.AttachAgentReferenceBytes("spikes_ref.png", spikesPNG)
		if err != nil {
			stepErr = err
			return
		}
		ed.SetAgentComposerText("Build what I sketched. Spikes should look like the reference image.")
		ed.SetAgentImagePreviewID(img.ID)
	})
	if stepErr != nil {
		t.Fatal(stepErr)
	}
	app.Settle()
	app.Screenshot("agent-ref/composer-attached-preview")

	// 2. Drag-over "Drop to attach" overlay state matching Sketch.dc.html.
	onUI(func() {
		ed.SetAgentImagePreviewID("")
		ed.SetAgentComposerDragOver(true)
	})
	app.Settle()
	app.Screenshot("agent-ref/composer-drop-zone")

	// 3. Pin to brief: green Pinned chip + "Pinned spikes_ref.png" YAML diff card with Undo + Always brief.yaml · 2 refs.
	onUI(func() {
		ed.SetAgentComposerDragOver(false)
		_, stepErr = ed.PinAgentReferenceImage(img.ID, "rusty iron spikes, three per tile")
	})
	if stepErr != nil {
		t.Fatal(stepErr)
	}
	app.Settle()
	app.Screenshot("agent-ref/composer-pinned-diff")

	// 4. Unsupported file refusal banner.
	onUI(func() {
		ed.DismissAgentPinNotice()
		_, _ = ed.AttachAgentReferenceBytes("notes.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="4" height="4"/></svg>`))
	})
	app.Settle()
	app.Screenshot("agent-ref/composer-refused-type")
}
