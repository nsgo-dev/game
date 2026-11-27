package editor_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// minimalValidWebP is a valid 1x1 lossless VP8L WebP image.
var minimalValidWebP = []byte{
	0x52, 0x49, 0x46, 0x46, 0x1a, 0x00, 0x00, 0x00,
	0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x4c,
	0x0d, 0x00, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00,
	0x10, 0x07, 0x10, 0x11, 0x11, 0x88, 0x88, 0xfe,
	0x07, 0x00,
}

func makeSpikesPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{0x2A, 0x25, 0x20, 0xFF}
	spike := color.RGBA{0xA2, 0x6B, 0x45, 0xFF}
	hi := color.RGBA{0xD6, 0x9C, 0x6A, 0xFF}
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, bg)
		}
	}
	// Draw three triangular spikes along the bottom edge.
	for s := range 3 {
		cx := (s*2 + 1) * w / 6
		for y := h / 3; y < h; y++ {
			half := (y - h/3) * (w / 8) / max(1, h-h/3)
			for x := cx - half; x <= cx+half; x++ {
				if x >= 0 && x < w {
					if x == cx-half {
						img.SetRGBA(x, y, hi)
					} else {
						img.SetRGBA(x, y, spike)
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

func makeSampleJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{0x4F, 0x8C, 0xFF, 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeSampleGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{0x10, 0x12, 0x16, 0xFF}, color.RGBA{0xFF, 0x6B, 0x6B, 0xFF}}
	img := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func refFileURL(p string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(p)}).String()
}

func newRefProjectEditor(t *testing.T, withGit bool, initialBrief string) (*editor.Editor, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: dungeon-run\nid: dev.example.dungeon\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gameYAML := "version: 1\nname: dungeon-run\nid: dev.example.dungeon\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if initialBrief != "" {
		if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte(initialBrief), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withGit {
		gitRun := func(args ...string) {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=Test",
				"GIT_AUTHOR_EMAIL=test@example.com",
				"GIT_COMMITTER_NAME=Test",
				"GIT_COMMITTER_EMAIL=test@example.com",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		gitRun("init", "-b", "main")
		gitRun("add", ".")
		gitRun("commit", "-m", "initial")
	}

	ed := newTestEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetRightRailTab(editor.RightTabAgent)
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:        "c1",
		Name:      "claude-code",
		Transport: "stdio",
	})
	return ed, dir
}

// TestAgentDropPNGSentOnceNextRequestDoesNotCarryIt verifies Acceptance Criterion #1:
// dropping a PNG onto the composer attaches it as a chip whose hover shows it, the scripted
// agent receives it once on send, and the next request does not carry it.
func TestAgentDropPNGSentOnceNextRequestDoesNotCarryIt(t *testing.T) {
	ed, _ := newRefProjectEditor(t, false, "")
	pngBytes := makeSpikesPNG(t, 48, 36)
	dropFile := filepath.Join(t.TempDir(), "spikes_ref.png")
	if err := os.WriteFile(dropFile, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	reqs := make(chan editor.AgentRequest, 4)
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		reqs <- req
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Got it."})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Drag-over shows "Drop to attach".
	ed.SetAgentComposerDragOver(true)
	app.Settle()
	if !app.Exists("id:agent-composer-drop-zone") {
		t.Fatal("Drop to attach overlay did not appear")
	}
	if got := agentViewText(t, app, "id:agent-composer-drop-title"); got != "Drop to attach" {
		t.Fatalf("drop title = %q, want %q", got, "Drop to attach")
	}

	// Drop the PNG onto the composer -> becomes a chip whose hover shows the image.
	app.DropFiles("id:agent-composer", refFileURL(dropFile))
	app.Settle()
	if ed.AgentComposerDragOver() {
		t.Fatal("drag-over overlay stayed active after drop")
	}
	if !app.Exists("id:agent-image-chip-0") {
		t.Fatal("attached image chip did not appear after dropping PNG")
	}
	if got := agentViewText(t, app, "id:agent-image-name-0"); got != "spikes_ref.png" {
		t.Fatalf("chip filename = %q, want spikes_ref.png", got)
	}

	// Hovering the chip opens the preview showing the image and its dimensions.
	app.Hover("id:agent-image-chip-0")
	app.Settle()
	if !app.Exists("id:agent-image-preview-popover") || !app.Exists("id:agent-image-preview-img") {
		t.Fatal("hovering attached image chip did not show image preview popover")
	}
	if dims := agentViewText(t, app, "id:agent-image-preview-dims"); dims != "PNG · 48 × 36" {
		t.Fatalf("preview dims = %q, want %q", dims, "PNG · 48 × 36")
	}
	app.HoverOut()
	app.Settle()

	// First request carries the dropped PNG once.
	ed.SetAgentComposerText("Make the spikes look like this.")
	app.Settle()
	app.Press("id:agent-composer-send")
	app.Settle()

	req1 := <-reqs
	if len(req1.AttachedImages) != 1 || len(req1.Images) != 1 {
		t.Fatalf("req1 images = (%d attached, %d total), want (1, 1)", len(req1.AttachedImages), len(req1.Images))
	}
	img1 := req1.Images[0]
	if img1.Name != "spikes_ref.png" || img1.Format != editor.AgentImagePNG || img1.Width != 48 || img1.Height != 36 || img1.FromBrief || img1.Pinned {
		t.Fatalf("unexpected req1 image metadata: %+v", img1)
	}
	if !bytes.Equal(img1.Data, pngBytes) {
		t.Fatal("req1 image bytes did not match dropped PNG")
	}

	app.WaitUntil(5*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected && app.Exists("id:agent-turn-images-0")
	})

	// Chip was cleared after sending so the next request does NOT carry it.
	if len(ed.AgentAttachedImages()) != 0 {
		t.Fatalf("attached images after send = %d, want 0", len(ed.AgentAttachedImages()))
	}
	if app.Exists("id:agent-image-chip-0") {
		t.Fatal("one-shot image chip remained after sending request")
	}

	// Second request receives NO image.
	if err := ed.SubmitAgentPrompt("Now add a torch on the wall."); err != nil {
		t.Fatalf("second SubmitAgentPrompt: %v", err)
	}
	req2 := <-reqs
	if len(req2.AttachedImages) != 0 || len(req2.BriefImages) != 0 || len(req2.Images) != 0 {
		t.Fatalf("second request unexpectedly carried images: attached=%d brief=%d total=%d", len(req2.AttachedImages), len(req2.BriefImages), len(req2.Images))
	}
	app.WaitUntil(5*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected && len(ed.AgentTurns()) == 2
	})
}

// TestAgentPasteAndDropFormatsAndUnsupportedRefusal verifies PNG, JPEG, and WebP via paste and drop,
// and verifies that unsupported formats (GIF, SVG, TXT, corrupt PNG) are refused with the reason.
func TestAgentPasteAndDropFormatsAndUnsupportedRefusal(t *testing.T) {
	ed, _ := newRefProjectEditor(t, false, "")
	tmp := t.TempDir()

	pngBytes := makeSpikesPNG(t, 24, 24)
	jpegBytes := makeSampleJPEG(t, 32, 20)
	webpBytes := minimalValidWebP

	jpegFile := filepath.Join(tmp, "wall_ref.jpg")
	webpFile := filepath.Join(tmp, "door_ref.webp")
	gifFile := filepath.Join(tmp, "anim.gif")
	txtFile := filepath.Join(tmp, "notes.txt")
	svgFile := filepath.Join(tmp, "icon.svg")
	badPNGFile := filepath.Join(tmp, "corrupt.png")

	for p, b := range map[string][]byte{
		jpegFile:   jpegBytes,
		webpFile:   webpBytes,
		gifFile:    makeSampleGIF(t),
		txtFile:    []byte("hello world"),
		svgFile:    []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="8" height="8"/></svg>`),
		badPNGFile: []byte("this is not actually a png"),
	} {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// 1. Paste a PNG via core.PasteEvent into the composer input.
	app.Paste("id:agent-composer-input", core.PasteEvent{
		Files: []core.PastedFile{{Name: "pasted_ref.png", MIME: "image/png", Data: pngBytes}},
	})
	app.Settle()

	// 2. Drop JPEG and WebP files onto the composer.
	app.DropFiles("id:agent-composer", refFileURL(jpegFile), refFileURL(webpFile))
	app.Settle()

	imgs := ed.AgentAttachedImages()
	if len(imgs) != 3 {
		t.Fatalf("attached images = %d, want 3 (PNG, JPEG, WebP)", len(imgs))
	}
	if imgs[0].Format != editor.AgentImagePNG || imgs[1].Format != editor.AgentImageJPEG || imgs[2].Format != editor.AgentImageWebP {
		t.Fatalf("formats = [%s, %s, %s], want [png, jpeg, webp]", imgs[0].Format, imgs[1].Format, imgs[2].Format)
	}

	// Remove the middle chip via its × button.
	app.Press("id:agent-image-remove-1")
	app.Settle()
	if len(ed.AgentAttachedImages()) != 2 {
		t.Fatalf("after remove attached images = %d, want 2", len(ed.AgentAttachedImages()))
	}

	// 3. Unsupported formats must be refused with clear reasons and leave attached chips untouched.
	for _, tc := range []struct {
		name     string
		file     string
		wantText string
	}{
		{"GIF", gifFile, "only PNG, JPEG, or WebP"},
		{"TXT", txtFile, ".txt"},
		{"SVG", svgFile, ".svg"},
		{"corrupt PNG", badPNGFile, "only PNG, JPEG, or WebP"},
	} {
		app.DropFiles("id:agent-composer", refFileURL(tc.file))
		app.Settle()
		if !app.Exists("id:agent-attach-error") {
			t.Fatalf("%s: refusal banner did not appear", tc.name)
		}
		gotErr := agentViewText(t, app, "id:agent-attach-error-text")
		if !strings.Contains(gotErr, tc.wantText) {
			t.Fatalf("%s: error = %q, want substring %q", tc.name, gotErr, tc.wantText)
		}
		if len(ed.AgentAttachedImages()) != 2 {
			t.Fatalf("%s: attached count changed to %d", tc.name, len(ed.AgentAttachedImages()))
		}
	}

	// Dismissing error banner clears it.
	app.Press("id:agent-attach-error-dismiss")
	app.Settle()
	if app.Exists("id:agent-attach-error") {
		t.Fatal("error banner stayed visible after dismiss")
	}

	// 4. Pasting an SVG via PasteEvent is also refused with "SVG".
	app.Paste("id:agent-composer-input", core.PasteEvent{
		Files: []core.PastedFile{{Name: "vector.svg", MIME: "image/svg+xml", Data: []byte(`<svg><circle r="4"/></svg>`)}},
	})
	app.Settle()
	if gotErr := ed.AgentAttachError(); !strings.Contains(gotErr, "SVG") || !strings.Contains(gotErr, "only PNG, JPEG, or WebP") {
		t.Fatalf("pasted SVG error = %q", gotErr)
	}

	// 5. Clipboard hook paste attaches a PNG cleanly.
	ed.ReadClipboardImage = func(done func(core.ClipboardImage, bool, error)) {
		done(core.ClipboardImage{PNG: pngBytes}, true, nil)
	}
	pasted, ok, err := ed.PasteAgentReferenceImage()
	if err != nil || !ok || len(pasted) != 1 || pasted[0].Format != editor.AgentImagePNG {
		t.Fatalf("PasteAgentReferenceImage = (%v, %v, %v)", pasted, ok, err)
	}
}

// TestAgentPinToBriefWritesFileAndYAMLCarriesNextRequestAndUndoRemovesBoth verifies Acceptance Criterion #2:
// Pin to brief writes art/reference/<name> and one YAML line in brief.yaml, shows in Review/Changes,
// carries the image in every subsequent request through the Always chip, and Undo removes both.
func TestAgentPinToBriefWritesFileAndYAMLCarriesNextRequestAndUndoRemovesBoth(t *testing.T) {
	initialBrief := "art:\n  direction: chunky pixel art, 16 px tiles\n  references:\n    - path: art/reference/torch_ref.png\n"
	ed, projDir := newRefProjectEditor(t, true, initialBrief)

	// Write existing torch_ref.png so brief.yaml has 1 valid prior reference.
	if err := os.MkdirAll(filepath.Join(projDir, "art", "reference"), 0o755); err != nil {
		t.Fatal(err)
	}
	torchBytes := makeSpikesPNG(t, 16, 16)
	if err := os.WriteFile(filepath.Join(projDir, "art", "reference", "torch_ref.png"), torchBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	spikesBytes := makeSpikesPNG(t, 48, 32)
	dropFile := filepath.Join(t.TempDir(), "spikes_ref.png")
	if err := os.WriteFile(dropFile, spikesBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	reqs := make(chan editor.AgentRequest, 4)
	ed.AgentRunner = editor.AgentRunnerFunc(func(_ context.Context, req editor.AgentRequest, emit func(editor.AgentEvent)) error {
		reqs <- req
		emit(editor.AgentEvent{Kind: editor.AgentEventMessage, Text: "Applied style from brief."})
		emit(editor.AgentEvent{Kind: editor.AgentEventDone})
		return nil
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Always chip initially shows brief.yaml · 1 ref.
	if got := agentViewText(t, app, "id:agent-always-brief-text"); got != "brief.yaml · 1 ref" {
		t.Fatalf("initial Always chip = %q, want %q", got, "brief.yaml · 1 ref")
	}

	// Drop spikes_ref.png onto composer, then press "Pin to brief".
	app.DropFiles("id:agent-composer", refFileURL(dropFile))
	app.Settle()
	if got := agentViewText(t, app, "id:agent-image-pin-text-0"); got != "Pin to brief" {
		t.Fatalf("pin button before click = %q, want Pin to brief", got)
	}

	app.Press("id:agent-image-pin-0")
	app.Settle()

	// 1. Verify art/reference/spikes_ref.png was written with exact bytes.
	pinnedAbs := filepath.Join(projDir, "art", "reference", "spikes_ref.png")
	gotImgBytes, err := os.ReadFile(pinnedAbs)
	if err != nil {
		t.Fatalf("pinned file missing: %v", err)
	}
	if !bytes.Equal(gotImgBytes, spikesBytes) {
		t.Fatal("art/reference/spikes_ref.png bytes do not match source PNG")
	}

	// 2. Verify brief.yaml has exactly one added line for spikes_ref.png and preserved existing keys.
	gotBriefBytes, err := os.ReadFile(filepath.Join(projDir, "brief.yaml"))
	if err != nil {
		t.Fatalf("read brief.yaml: %v", err)
	}
	gotBrief := string(gotBriefBytes)
	if !strings.Contains(gotBrief, "direction: chunky pixel art, 16 px tiles") {
		t.Fatalf("brief.yaml lost existing direction field:\n%s", gotBrief)
	}
	if !strings.Contains(gotBrief, "- path: art/reference/spikes_ref.png") {
		t.Fatalf("brief.yaml missing pinned reference line:\n%s", gotBrief)
	}
	oldLines := strings.Split(strings.TrimRight(initialBrief, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(gotBrief, "\n"), "\n")
	if len(newLines)-len(oldLines) != 1 {
		t.Fatalf("expected exactly 1 YAML line added to brief.yaml, got +%d lines:\n%s", len(newLines)-len(oldLines), gotBrief)
	}

	// 3. UI reflects pinned state, confirmation card with Undo, and Always chip shows 2 refs.
	if got := agentViewText(t, app, "id:agent-image-pin-text-0"); got != "Pinned" {
		t.Fatalf("pin button after click = %q, want Pinned", got)
	}
	if !app.Exists("id:agent-pin-notice-card") || !app.Exists("id:agent-pin-notice-undo") {
		t.Fatal("Pin to brief confirmation card or Undo button did not appear")
	}
	if got := agentViewText(t, app, "id:agent-always-brief-text"); got != "brief.yaml · 2 refs" {
		t.Fatalf("Always chip after pin = %q, want %q", got, "brief.yaml · 2 refs")
	}

	// Hovering the Always chip previews the pinned brief references.
	app.Hover("id:agent-always-brief")
	app.Settle()
	if !app.Exists("id:agent-brief-ref-row-1") {
		t.Fatal("hovering brief.yaml Always chip did not list both pinned references")
	}
	app.HoverOut()
	app.Settle()

	// 4. Both art/reference/spikes_ref.png and brief.yaml show up in Review / Changes like any change.
	if err := ed.OpenChangesPanel(); err != nil {
		t.Fatalf("OpenChangesPanel: %v", err)
	}
	sess := ed.ChangesSession()
	if sess == nil {
		t.Fatal("ChangesSession returned nil")
	}
	var sawBrief, sawSpikes bool
	for _, f := range sess.Files {
		if f.ProjectPath == "brief.yaml" {
			sawBrief = true
		}
		if f.ProjectPath == "art/reference/spikes_ref.png" {
			sawSpikes = true
		}
	}
	ed.CloseChangesPanel()
	app.Settle()
	if !sawBrief || !sawSpikes {
		t.Fatalf("Changes/Review missing pinned files (brief=%v, spikes=%v): %+v", sawBrief, sawSpikes, sess.Files)
	}

	// 5. Clear one-shot chips and send a brand-new request -> agent receives spikes_ref.png via brief.yaml!
	ed.ClearAgentAttachedImages()
	if err := ed.SubmitAgentPrompt("Build floor 2 traps matching our brief."); err != nil {
		t.Fatalf("SubmitAgentPrompt: %v", err)
	}
	reqPinned := <-reqs
	if len(reqPinned.AttachedImages) != 0 {
		t.Fatalf("expected 0 one-shot attached images, got %d", len(reqPinned.AttachedImages))
	}
	if len(reqPinned.BriefImages) != 2 || len(reqPinned.Images) != 2 {
		t.Fatalf("expected 2 brief images on next request, got brief=%d total=%d", len(reqPinned.BriefImages), len(reqPinned.Images))
	}
	if reqPinned.BriefImages[1].Path != "art/reference/spikes_ref.png" || !reqPinned.BriefImages[1].Pinned || !reqPinned.BriefImages[1].FromBrief {
		t.Fatalf("unexpected brief image entry: %+v", reqPinned.BriefImages[1])
	}
	if !bytes.Equal(reqPinned.BriefImages[1].Data, spikesBytes) {
		t.Fatal("brief image bytes did not match pinned spikes_ref.png")
	}
	app.WaitUntil(5*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected
	})

	// 6. Undo removes BOTH art/reference/spikes_ref.png and the YAML line from brief.yaml.
	app.Press("id:agent-pin-notice-undo")
	app.Settle()

	if _, err := os.Stat(pinnedAbs); !os.IsNotExist(err) {
		t.Fatalf("after Undo art/reference/spikes_ref.png still exists (err=%v)", err)
	}
	restoredBrief, err := os.ReadFile(filepath.Join(projDir, "brief.yaml"))
	if err != nil {
		t.Fatalf("read brief.yaml after Undo: %v", err)
	}
	if string(restoredBrief) != initialBrief {
		t.Fatalf("brief.yaml after Undo =\n%q\nwant:\n%q", string(restoredBrief), initialBrief)
	}
	if app.Exists("id:agent-pin-notice-card") {
		t.Fatal("pin confirmation card stayed visible after Undo")
	}
	if got := agentViewText(t, app, "id:agent-always-brief-text"); got != "brief.yaml · 1 ref" {
		t.Fatalf("Always chip after Undo = %q, want %q", got, "brief.yaml · 1 ref")
	}

	// Subsequent request no longer carries spikes_ref.png.
	if err := ed.SubmitAgentPrompt("Check the floor."); err != nil {
		t.Fatalf("SubmitAgentPrompt after undo: %v", err)
	}
	reqAfterUndo := <-reqs
	if len(reqAfterUndo.BriefImages) != 1 || reqAfterUndo.BriefImages[0].Path != "art/reference/torch_ref.png" {
		t.Fatalf("after Undo brief images = %+v, want only torch_ref.png", reqAfterUndo.BriefImages)
	}
	app.WaitUntil(5*time.Second, func() bool {
		return ed.AgentStatus() == editor.AgentStatusConnected
	})

	// 7. Redo cleanly restores both art/reference/spikes_ref.png and brief.yaml.
	if !ed.Redo() {
		t.Fatal("Redo returned false")
	}
	if _, err := os.Stat(pinnedAbs); err != nil {
		t.Fatalf("after Redo art/reference/spikes_ref.png missing: %v", err)
	}
	if refs := ed.BriefReferences(); len(refs) != 2 || refs[1].Path != "art/reference/spikes_ref.png" {
		t.Fatalf("after Redo BriefReferences = %+v", refs)
	}
}

// TestAgentPinCreatesAndUndoDeletesFreshBriefYAML verifies that in a project without brief.yaml,
// pinning creates art/reference/<name> and brief.yaml, and Undo removes both files cleanly.
func TestAgentPinCreatesAndUndoDeletesFreshBriefYAML(t *testing.T) {
	ed, projDir := newRefProjectEditor(t, false, "")
	pngBytes := makeSpikesPNG(t, 24, 24)
	img, err := ed.AttachAgentReferenceBytes("boss_ref.png", pngBytes)
	if err != nil {
		t.Fatalf("AttachAgentReferenceBytes: %v", err)
	}
	notice, err := ed.PinAgentReferenceImage(img.ID, "")
	if err != nil {
		t.Fatalf("PinAgentReferenceImage: %v", err)
	}
	if notice.RelPath != "art/reference/boss_ref.png" {
		t.Fatalf("notice.RelPath = %q", notice.RelPath)
	}
	briefPath := filepath.Join(projDir, "brief.yaml")
	imgPath := filepath.Join(projDir, "art", "reference", "boss_ref.png")
	if _, err := os.Stat(briefPath); err != nil {
		t.Fatalf("brief.yaml not created: %v", err)
	}
	if _, err := os.Stat(imgPath); err != nil {
		t.Fatalf("art/reference/boss_ref.png not created: %v", err)
	}

	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if _, err := os.Stat(briefPath); !os.IsNotExist(err) {
		t.Fatalf("brief.yaml still exists after Undo (err=%v)", err)
	}
	if _, err := os.Stat(imgPath); !os.IsNotExist(err) {
		t.Fatalf("art/reference/boss_ref.png still exists after Undo (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(projDir, "art", "reference")); !os.IsNotExist(err) {
		t.Fatalf("empty art/reference dir was not cleaned up on Undo (err=%v)", err)
	}
}

func TestAgentReferenceImagesSpecLightDarkTokens(t *testing.T) {
	ed, _ := newRefProjectEditor(t, false, "art:\n  direction: pixel art\n")
	pngBytes := makeSpikesPNG(t, 32, 24)
	img, err := ed.AttachAgentReferenceBytes("spikes_ref.png", pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	ed.SetAgentImagePreviewID(img.ID)
	ed.SetAgentComposerDragOver(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	light := editor.LightSpecPalette()
	dark := editor.DarkSpecPalette()

	if got := app.Find("id:agent-image-preview-popover").Style.Background; got != light.Raised {
		t.Fatalf("light preview popover bg = %#x, want %#x", uint32(got), uint32(light.Raised))
	}
	if got := app.Find("id:agent-composer-drop-zone").Style.Background; got != light.Accent.Fill {
		t.Fatalf("light drop zone bg = %#x, want %#x", uint32(got), uint32(light.Accent.Fill))
	}
	if got := app.Find("id:agent-image-chip-0").Style.Background; got != light.LineRaised {
		t.Fatalf("light chip bg = %#x, want %#x", uint32(got), uint32(light.LineRaised))
	}

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	if got := app.Find("id:agent-image-preview-popover").Style.Background; got != dark.Raised {
		t.Fatalf("dark preview popover bg = %#x, want %#x", uint32(got), uint32(dark.Raised))
	}
	if got := app.Find("id:agent-composer-drop-zone").Style.Background; got != dark.Accent.Fill {
		t.Fatalf("dark drop zone bg = %#x, want %#x", uint32(got), uint32(dark.Accent.Fill))
	}
	if got := app.Find("id:agent-image-chip-0").Style.Background; got != dark.LineRaised {
		t.Fatalf("dark chip bg = %#x, want %#x", uint32(got), uint32(dark.LineRaised))
	}
}

func TestAgentPinBriefInteropWithSettingsBrief(t *testing.T) {
	ed, projDir := newRefProjectEditor(t, false, "")
	pngBytes := makeSpikesPNG(t, 24, 24)
	img, err := ed.AttachAgentReferenceBytes("spikes_ref.png", pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PinAgentReferenceImage(img.ID, "rusty iron spikes\ninjection"); err != nil {
		t.Fatalf("PinAgentReferenceImage: %v", err)
	}

	// Fresh brief.yaml must be valid versioned YAML parseable by project.ParseBrief.
	briefPath := filepath.Join(projDir, project.BriefFileName)
	raw, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := project.ParseBrief(raw)
	if err != nil {
		t.Fatalf("project.ParseBrief failed on pinned brief.yaml:\n%s\nerr: %v", raw, err)
	}
	if parsed.Art == nil || len(parsed.Art.References) != 1 || parsed.Art.References[0].Path != "art/reference/spikes_ref.png" {
		t.Fatalf("unexpected parsed brief art: %+v", parsed.Art)
	}
	if strings.Contains(parsed.Art.References[0].Note, "\n") {
		t.Fatalf("control char/newline leaked into brief note: %q", parsed.Art.References[0].Note)
	}

	// Editor's Settings > Game brief state must have reloaded automatically.
	if !ed.BriefExists() || ed.BriefLoadErr() != nil {
		t.Fatalf("BriefExists=%v BriefLoadErr=%v after pin", ed.BriefExists(), ed.BriefLoadErr())
	}
	// Editing pitch in Settings > Game brief must preserve pinned art.references.
	if err := ed.EditBrief("Set pitch", func(b *project.Brief) {
		b.Pitch = "A tight dungeon platformer."
	}); err != nil {
		t.Fatalf("EditBrief after pin: %v", err)
	}
	refs := ed.BriefReferences()
	if len(refs) != 1 || refs[0].Path != "art/reference/spikes_ref.png" {
		t.Fatalf("pinned reference lost after Settings EditBrief: %+v", refs)
	}
}

func TestAgentReferenceSymlinkAndTOCTOUGuards(t *testing.T) {
	outside := t.TempDir()
	outsidePNG := filepath.Join(outside, "secret.png")
	outsideBrief := filepath.Join(outside, "outside_brief.yaml")
	if err := os.WriteFile(outsidePNG, makeSpikesPNG(t, 16, 16), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideBrief, []byte("version: 1\nart:\n  references:\n    - path: art/reference/escape.png\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Symlinked brief.yaml pointing outside project must be ignored & refused for writes.
	ed1, projDir1 := newRefProjectEditor(t, false, "")
	if err := os.Symlink(outsideBrief, filepath.Join(projDir1, "brief.yaml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if ed1.BriefFileExists() {
		t.Fatal("BriefFileExists returned true for symlinked brief.yaml")
	}
	if refs := ed1.BriefReferences(); len(refs) != 0 {
		t.Fatalf("BriefReferences read symlinked brief.yaml: %+v", refs)
	}
	img1, err := ed1.AttachAgentReferenceBytes("test.png", makeSpikesPNG(t, 16, 16))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ed1.PinAgentReferenceImage(img1.ID, ""); err == nil {
		t.Fatal("PinAgentReferenceImage succeeded against symlinked brief.yaml")
	}

	// 2. Symlinked art/reference directory or image inside brief.yaml must be refused.
	ed2, projDir2 := newRefProjectEditor(t, false, "version: 1\nart:\n  references:\n    - path: art/reference/escape.png\n")
	if err := os.MkdirAll(filepath.Join(projDir2, "art", "reference"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePNG, filepath.Join(projDir2, "art", "reference", "escape.png")); err != nil {
		t.Fatal(err)
	}
	if imgs := ed2.BriefReferenceImages(); len(imgs) != 0 {
		t.Fatalf("BriefReferenceImages loaded symlinked image: %+v", imgs)
	}

	// 3. TOCTOU symlink swap before Undo must refuse to delete outside file.
	ed3, projDir3 := newRefProjectEditor(t, false, "")
	img3, err := ed3.AttachAgentReferenceBytes("safe.png", makeSpikesPNG(t, 16, 16))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ed3.PinAgentReferenceImage(img3.ID, ""); err != nil {
		t.Fatal(err)
	}
	pinnedPath := filepath.Join(projDir3, "art", "reference", "safe.png")
	if err := os.Remove(pinnedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePNG, pinnedPath); err != nil {
		t.Fatal(err)
	}
	if ed3.Undo() {
		t.Fatal("Undo succeeded despite symlink swap on pinned image")
	}
	if _, err := os.Stat(outsidePNG); err != nil {
		t.Fatalf("outside target was touched by Undo: %v", err)
	}
}

func TestAgentReferenceCapsAndInputSafety(t *testing.T) {
	ed, _ := newRefProjectEditor(t, false, "")
	pngBytes := makeSpikesPNG(t, 16, 16)

	// Remote URI schemes and non-localhost file:// URIs must be refused.
	for _, bad := range []string{
		"https://example.com/ref.png",
		"file://evil.example.com/share/ref.png",
		t.TempDir(), // directory, not regular file
	} {
		if _, err := ed.AttachAgentReferenceFile(bad); err == nil {
			t.Fatalf("AttachAgentReferenceFile(%q) unexpectedly succeeded", bad)
		}
	}

	// Enforce per-request attachment cap (8).
	for i := range 8 {
		if _, err := ed.AttachAgentReferenceBytes("ref.png", pngBytes); err != nil {
			t.Fatalf("attach #%d failed: %v", i+1, err)
		}
	}
	if _, err := ed.AttachAgentReferenceBytes("ninth.png", pngBytes); err == nil {
		t.Fatal("expected 9th attachment to be refused by per-request cap")
	}
}

func TestAgentUndoLastBriefPinOnlyUndoesPinAtTopOfHistory(t *testing.T) {
	ed, projDir := newRefProjectEditor(t, false, "version: 1\npitch: Initial\n")
	img, err := ed.AttachAgentReferenceBytes("spikes_ref.png", makeSpikesPNG(t, 24, 24))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PinAgentReferenceImage(img.ID, ""); err != nil {
		t.Fatal(err)
	}
	if !ed.CanUndoLastBriefPin() {
		t.Fatal("CanUndoLastBriefPin should be true right after pinning")
	}

	// Perform an unrelated edit on top of the pin.
	if err := ed.EditBrief("Update pitch", func(b *project.Brief) {
		b.Pitch = "Updated pitch"
	}); err != nil {
		t.Fatal(err)
	}
	if ed.CanUndoLastBriefPin() {
		t.Fatal("CanUndoLastBriefPin should be false after another command pushed onto History")
	}
	if ed.UndoLastBriefPin() {
		t.Fatal("UndoLastBriefPin must not undo an unrelated later command")
	}
	if got := ed.Brief().Pitch; got != "Updated pitch" {
		t.Fatalf("unrelated edit was reverted: pitch = %q", got)
	}
	if _, err := os.Stat(filepath.Join(projDir, "art", "reference", "spikes_ref.png")); err != nil {
		t.Fatalf("pinned file unexpectedly removed: %v", err)
	}
}

func TestAgentComposerFocusPreservedAcrossImageAttachAndPin(t *testing.T) {
	ed, _ := newRefProjectEditor(t, false, "version: 1\npitch: Dungeon\n")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Focus("id:agent-composer-input")
	app.Settle()

	var img editor.AgentReferenceImage
	var attachErr error
	app.Post(func() {
		img, attachErr = ed.AttachAgentReferenceBytes("spikes_ref.png", makeSpikesPNG(t, 24, 24))
	})
	app.Settle()
	if attachErr != nil {
		t.Fatal(attachErr)
	}
	app.Key("a")
	app.Settle()
	if !strings.Contains(ed.AgentComposerText(), "a") {
		t.Fatalf("attaching image stole focus from agent-composer-input (text=%q)", ed.AgentComposerText())
	}

	app.Post(func() {
		ed.SetAgentImagePreviewID(img.ID)
	})
	app.Settle()
	app.Key("b")
	app.Settle()
	if !strings.Contains(ed.AgentComposerText(), "b") {
		t.Fatalf("opening image preview stole focus from agent-composer-input (text=%q)", ed.AgentComposerText())
	}

	var pinErr error
	app.Post(func() {
		_, pinErr = ed.PinAgentReferenceImage(img.ID, "spikes")
	})
	app.Settle()
	if pinErr != nil {
		t.Fatal(pinErr)
	}
	app.Key("c")
	app.Settle()
	if !strings.Contains(ed.AgentComposerText(), "c") {
		t.Fatalf("pinning image stole focus from agent-composer-input (text=%q)", ed.AgentComposerText())
	}
}
