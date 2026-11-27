package editor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func runAssetBuildInTest(ctx context.Context, spec editor.AssetsBuildSpec, stdout, stderr io.Writer) error {
	data, err := os.ReadFile(filepath.Join(spec.Dir, project.FileName))
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return err
	}
	f, err := project.Parse(data)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return err
	}
	if f.Assets == nil {
		err := fmt.Errorf("%s has no assets section", project.FileName)
		fmt.Fprintf(stderr, "%v\n", err)
		return err
	}
	b := &assetbuild.Builder{
		Root:  spec.Dir,
		Force: spec.Force,
	}
	rep, err := b.Build(ctx, f.Assets)
	if rep != nil {
		for _, target := range rep.Targets {
			if target.Status != assetbuild.UpToDate {
				fmt.Fprintf(stdout, "%s %s\n", target.Status, target.ID)
			}
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return err
	}
	fmt.Fprintf(stdout, "%d built, %d up to date\n", rep.Count(assetbuild.Built), rep.Count(assetbuild.UpToDate))
	return nil
}

// #915 Acceptance 1: selecting an image in the file browser shows its import settings
// and credit section in the inspector inheriting defaults from game.yaml; setting filter
// and credit writes a valid *.import.yaml sidecar that parses back unchanged through
// importsettings.Parse, Undo restores the previous sidecar (or removes a newly created one),
// Redo writes it back, and Reset restores inherited target defaults.
func TestAssetImportInspectorImageFilterCreditUndoAndReset(t *testing.T) {
	dir := t.TempDir()
	gameYAML := `version: 1
name: ImportFixture
id: dev.example.importfixture
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  textures:
    - in: art/hero.png
      out: assets/hero.ktx2
      format: bc3
      mipmaps: true
      srgb: true
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
    objects:
      - id: hero
        x: 32
        y: 32
        sprite:
          atlas: actors
          frame: hero
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"art/actors.json":        fixtureAtlasJSON,
		"scenes/room.scene.yaml": roomScene,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
		"art/hero.png":   encodeTestPNG(t, 16, 16),
	})

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Select art/hero.png in the file browser.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-art/hero.png")
	app.Settle()

	if !app.Exists("id:inspector-asset-import") {
		t.Fatal("expected inspector-asset-import when an image is selected in the file browser")
	}
	if got := allText(app.Find("id:import-asset-path")); !strings.Contains(got, "art/hero.png") {
		t.Fatalf("import-asset-path = %q, want art/hero.png", got)
	}

	// Verify inherited defaults from game.yaml texture target before any sidecar exists.
	info, err := ed.InspectAssetImport("art/hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if info.SidecarExists {
		t.Fatal("expected no sidecar before editing")
	}
	if fmtVal, over := info.EffectiveImageFormat(); fmtVal != "bc3" || over {
		t.Fatalf("EffectiveImageFormat = (%q, %v), want (bc3, false)", fmtVal, over)
	}
	if mips, over := info.EffectiveImageMipmaps(); !mips || over {
		t.Fatalf("EffectiveImageMipmaps = (%v, %v), want (true, false)", mips, over)
	}
	if srgb, over := info.EffectiveImageSRGB(); !srgb || over {
		t.Fatalf("EffectiveImageSRGB = (%v, %v), want (true, false)", srgb, over)
	}
	if filt, over := info.EffectiveImageFilter(); filt != "linear" || over {
		t.Fatalf("EffectiveImageFilter = (%q, %v), want (linear, false)", filt, over)
	}

	// Set image filter to "nearest" in the Inspector.
	app.Press("id:import-image-filter")
	app.Press("role:option nearest")
	app.Settle()

	sidecarPath := filepath.Join(dir, "art", "hero.png.import.yaml")
	data1, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("expected sidecar written at %s: %v", sidecarPath, err)
	}
	parsed1, err := importsettings.Parse(data1)
	if err != nil {
		t.Fatalf("sidecar did not parse through importsettings.Parse: %v\n%s", err, data1)
	}
	if parsed1.Image == nil || parsed1.Image.Filter != "nearest" {
		t.Fatalf("parsed image filter = %+v, want nearest", parsed1.Image)
	}

	// Set credit in the Inspector UI (author, license, source, notice).
	app.Type("id:import-credit-author", "Ada Lovelace")
	app.Submit("id:import-credit-author")
	app.Settle()
	// Missing license yet should show inline validation hint and keep the draft without writing invalid YAML.
	if !app.Exists("id:import-credit-error") {
		t.Fatal("expected inline validation hint when only author is filled")
	}

	app.Type("id:import-credit-license", "CC-BY-4.0")
	app.Submit("id:import-credit-license")
	app.Settle()
	if app.Exists("id:import-credit-error") {
		t.Fatalf("unexpected credit error after setting author and license: %s", allText(app.Find("id:import-credit-error")))
	}

	app.Type("id:import-credit-source", "https://example.com/hero")
	app.Submit("id:import-credit-source")
	app.Type("id:import-credit-notice", "Copyright Ada")
	app.Submit("id:import-credit-notice")
	app.Settle()

	data2, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("read sidecar after credit edit: %v", err)
	}
	parsed2, err := importsettings.Parse(data2)
	if err != nil {
		t.Fatalf("importsettings.Parse failed: %v\n%s", err, data2)
	}
	reencoded, err := importsettings.Encode(parsed2)
	if err != nil {
		t.Fatalf("importsettings.Encode failed: %v", err)
	}
	if !bytes.Equal(data2, reencoded) {
		t.Fatalf("sidecar did not round-trip unchanged:\ngot:\n%s\nwant:\n%s", data2, reencoded)
	}
	if parsed2.Image == nil || parsed2.Image.Filter != "nearest" {
		t.Fatalf("Image = %+v, want filter=nearest", parsed2.Image)
	}
	if parsed2.Credit == nil ||
		parsed2.Credit.Author != "Ada Lovelace" ||
		parsed2.Credit.License != "CC-BY-4.0" ||
		parsed2.Credit.Source != "https://example.com/hero" ||
		parsed2.Credit.Notice != "Copyright Ada" {
		t.Fatalf("Credit = %+v, want full Ada Lovelace CC-BY-4.0 credit", parsed2.Credit)
	}

	// Undo every edit back to before the sidecar existed: Undo removes the newly created sidecar!
	for ed.History.CanUndo() {
		undoInUI(app, ed)
	}
	if _, err := os.Stat(sidecarPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected sidecar removed after undoing back to creation, stat err = %v", err)
	}

	// Redo brings the exact sidecar back.
	for ed.History.CanRedo() {
		redoInUI(app, ed)
	}
	afterRedo, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("expected sidecar restored on redo: %v", err)
	}
	if !bytes.Equal(afterRedo, data2) {
		t.Fatalf("sidecar after redo differs:\ngot:\n%s\nwant:\n%s", afterRedo, data2)
	}

	// Override format (bc3 -> etc2) then Reset format back to game.yaml target default (bc3).
	app.Press("id:import-image-format")
	app.Press("role:option etc2")
	app.Settle()
	info, _ = ed.InspectAssetImport("art/hero.png")
	if fmtVal, over := info.EffectiveImageFormat(); fmtVal != "etc2" || !over {
		t.Fatalf("EffectiveImageFormat after override = (%q, %v), want (etc2, true)", fmtVal, over)
	}
	app.Press("id:import-image-format-reset")
	app.Settle()
	info, _ = ed.InspectAssetImport("art/hero.png")
	if fmtVal, over := info.EffectiveImageFormat(); fmtVal != "bc3" || over {
		t.Fatalf("EffectiveImageFormat after reset = (%q, %v), want (bc3, false)", fmtVal, over)
	}

	// Reset filter and credit: when no overrides remain, sidecar file is removed cleanly.
	app.Press("id:import-image-filter-reset")
	app.Settle()
	app.Press("id:import-credit-reset")
	app.Settle()
	if _, err := os.Stat(sidecarPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected empty sidecar removed after resetting all fields, stat err = %v", err)
	}

	// Undo restores the previous pre-existing sidecar bytes verbatim (including hand-authored comments).
	handWritten := []byte("# custom hand-authored comment\nversion: 1\nimage:\n  filter: linear\n")
	if err := os.WriteFile(sidecarPath, handWritten, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetImageImportFilter("art/hero.png", "nearest"); err != nil {
		t.Fatal(err)
	}
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	restored, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, handWritten) {
		t.Fatalf("Undo did not restore exact previous sidecar bytes:\ngot:\n%s\nwant:\n%s", restored, handWritten)
	}
}

type recordingReloader struct {
	sceneformat.AtlasMap
	calls [][]string
}

func (r *recordingReloader) AtlasNames() []string {
	names := make([]string, 0, len(r.AtlasMap))
	for k := range r.AtlasMap {
		names = append(names, k)
	}
	return names
}

func (r *recordingReloader) ReloadAssets(_ string, changed []string) error {
	cp := make([]string, len(changed))
	copy(cp, changed)
	r.calls = append(r.calls, cp)
	return nil
}

// #915 Acceptance 2: after Reimport (running `game assets` with output in the Output panel),
// a changed atlas frame shows immediately in the Asset Browser and Viewport without
// restarting the editor, and Build Assets reloads changed textures too.
func TestAssetImportReimportReloadsAtlasInBrowserAndViewport(t *testing.T) {
	dir := t.TempDir()
	gameYAML := `version: 1
name: ReimportFixture
id: dev.example.reimportfixture
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  atlases:
    - name: actors
      in: art/sprites
      out: assets/atlases
  textures:
    - in: art/albedo.png
      out: assets/albedo.ktx2
      format: rgba8
`
	// Initially the scene references actors/hero_0.png, which does not exist until art/sprites/hero.png is sliced and reimported!
	roomScene := `version: 2
name: Room
layers:
  - name: actors
    objects:
      - id: hero
        x: 40
        y: 24
        sprite:
          atlas: actors
          frame: hero_0.png
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
	}, map[string][]byte{
		"art/sprites/hero.png": encodeTestPNG(t, 32, 16),
		"art/albedo.png":       encodeTestPNG(t, 16, 16),
	})

	// Run initial asset build so assets/atlases/actors.json initially has single frame "hero.png" (32x16).
	if err := runAssetBuildInTest(context.Background(), editor.AssetsBuildSpec{Dir: dir}, io.Discard, io.Discard); err != nil {
		t.Fatalf("initial asset build: %v", err)
	}
	initJSON, err := os.ReadFile(filepath.Join(dir, "assets", "atlases", "actors.json"))
	if err != nil {
		t.Fatal(err)
	}
	initPNG, err := os.ReadFile(filepath.Join(dir, "assets", "atlases", "actors.png"))
	if err != nil {
		t.Fatal(err)
	}
	initAtlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:assets/atlases/actors.png", Data: initPNG}, initJSON)
	if err != nil {
		t.Fatal(err)
	}

	reloader := &recordingReloader{
		AtlasMap: sceneformat.AtlasMap{"actors": initAtlas},
	}
	ed, err := editor.New(nil, reloader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.AssetsCommandRunner = runAssetBuildInTest

	var reloadEvents []editor.ReloadAssetsResult
	ed.OnReloadAssets = func(res editor.ReloadAssetsResult) {
		reloadEvents = append(reloadEvents, res)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	// Before reimport: hero.png is unsliced so actors only has hero.png; hero_0.png is missing so BuildErr is set.
	if ed.BuildErr == nil {
		t.Fatal("expected BuildErr before slicing hero.png into hero_0.png")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))
	openAssetBrowserInUI(app, ed)
	openFileBrowserInUI(app, ed)

	if !app.Exists("id:asset-actors-hero.png") {
		t.Fatal("expected asset browser to list initial unsliced frame hero.png")
	}
	if app.Exists("id:asset-actors-hero_0.png") {
		t.Fatal("did not expect hero_0.png in asset browser before slicing and reimporting")
	}

	// Select art/sprites/hero.png in the file browser, enable 16x16 Slice in the Inspector, and press Reimport.
	app.Press("id:files-row-art/sprites/hero.png")
	app.Settle()
	app.Press("id:import-image-slice-enabled")
	app.Settle()

	app.Press("id:import-reimport")
	app.WaitUntil(5*time.Second, func() bool { return !ed.AssetsBuilding() })
	if err := ed.AssetsBuildLastError(); err != nil {
		t.Fatalf("Reimport failed: %v", err)
	}
	ed.WaitForProblems()
	app.Settle()

	// Output panel must open and show `game assets` output.
	if !app.Exists("id:output-panel") {
		t.Fatal("expected Output panel open after Reimport")
	}
	outputText := allText(app.Find("id:output-panel"))
	if !strings.Contains(outputText, "built atlas assets/atlases/actors") {
		t.Fatalf("Output panel missing atlas build line:\n%s", outputText)
	}

	// Asset Browser must immediately reflect the newly sliced frames without restarting the editor.
	if !app.Exists("id:asset-actors-hero_0.png") || !app.Exists("id:asset-actors-hero_1.png") {
		t.Fatalf("expected asset browser to show hero_0.png and hero_1.png immediately after Reimport; assets=%+v", ed.Assets())
	}
	if app.Exists("id:asset-actors-hero.png") {
		t.Fatal("expected old unsliced hero.png frame to be replaced in asset browser after Reimport")
	}

	// Viewport / BuiltScene must immediately resolve and draw hero_0.png (16x16) with bumped ImageSource.Version.
	if ed.BuildErr != nil {
		t.Fatalf("expected BuildErr cleared after Reimport, got: %v", ed.BuildErr)
	}
	hero := ed.Built.Object("hero")
	if hero == nil || hero.Sprite == nil {
		t.Fatalf("expected built hero sprite in viewport after Reimport, got %+v", hero)
	}
	if hero.Sprite.Frame.W != 16 || hero.Sprite.Frame.H != 16 {
		t.Fatalf("viewport hero frame size = (%v, %v), want (16, 16)", hero.Sprite.Frame.W, hero.Sprite.Frame.H)
	}
	if hero.Atlas == nil || hero.Atlas.Image.Version == 0 {
		t.Fatalf("expected reloaded atlas image Version > 0, got %+v", hero.Atlas)
	}

	// OnReloadAssets and AssetReloader must have received the changed atlas.
	if len(reloadEvents) == 0 || !slicesContains(reloadEvents[len(reloadEvents)-1].Atlases, "actors") {
		t.Fatalf("reloadEvents = %+v, want actors atlas reloaded", reloadEvents)
	}
	if len(reloader.calls) == 0 || !slicesContains(reloader.calls[len(reloader.calls)-1], "assets/atlases/actors.json") {
		t.Fatalf("AssetReloader calls = %+v, want assets/atlases/actors.json", reloader.calls)
	}

	// Edit texture filter on art/albedo.png and run Build Assets.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-art/albedo.png")
	app.Settle()
	app.Press("id:import-image-filter")
	app.Press("role:option nearest")
	app.Settle()

	runEditorCommand(t, app, ed, editor.CmdBuildAssets)
	app.WaitUntil(5*time.Second, func() bool { return !ed.AssetsBuilding() })
	if err := ed.AssetsBuildLastError(); err != nil {
		t.Fatalf("Build Assets failed: %v", err)
	}
	ed.WaitForProblems()
	app.Settle()

	if len(reloadEvents) < 2 || !slicesContains(reloadEvents[len(reloadEvents)-1].Textures, "assets/albedo.ktx2") {
		t.Fatalf("expected texture assets/albedo.ktx2 in reload events, got %+v", reloadEvents)
	}

	// Verify failing asset build streams stderr into Output panel and sets error status cleanly.
	ed.AssetsCommandRunner = func(_ context.Context, _ editor.AssetsBuildSpec, _, stderr io.Writer) error {
		fmt.Fprintln(stderr, "art/albedo.png.import.yaml:1: bad texture setting")
		return errors.New("exit status 1")
	}
	if err := ed.BuildAssets(); err != nil {
		t.Fatalf("BuildAssets start error: %v", err)
	}
	app.WaitUntil(5*time.Second, func() bool { return !ed.AssetsBuilding() })
	if err := ed.AssetsBuildLastError(); err == nil {
		t.Fatal("expected error from failing asset build")
	}
	app.Settle()
	var sawStderr bool
	for _, entry := range ed.OutputEntries() {
		if entry.Source == editor.OutputStderr && entry.Level == editor.OutputError && strings.Contains(entry.Text, "bad texture setting") {
			sawStderr = true
		}
	}
	if !sawStderr {
		t.Fatalf("expected stderr error line in Output panel, entries=%+v", ed.OutputEntries())
	}
}

func TestAssetImportAudioFontModelKindsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	gameYAML := `version: 1
name: KindsFixture
id: dev.example.kindsfixture
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  audio:
    - in: audio/jump.wav
      out: assets/jump.mp3
      format: mp3
      rate: 44100
      channels: 1
      bitrate: 192
      peakDB: -2
  fonts:
    - in: fonts/hud.ttf
      out: assets/hud.fnt
      size: 48
      chars: "0123456789"
      padding: 2
  models:
    - in: models/hero.glb
      out: assets/hero.glb
      scale: 0.01
      animations: false
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
		"audio/jump.wav":         "RIFF",
		"fonts/hud.ttf":          "OTTO",
		"models/hero.glb":        "glTF",
	}, nil)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// 1. Audio kind.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-audio/jump.wav")
	app.Settle()
	for _, id := range []string{
		"id:import-audio-format",
		"id:import-audio-rate",
		"id:import-audio-channels",
		"id:import-audio-bitrate",
		"id:import-audio-norm-mode",
		"id:import-audio-loop-enabled",
		"id:import-credit-author",
	} {
		if !app.Exists(id) {
			t.Fatalf("audio inspector missing %s", id)
		}
	}
	audioInfo, err := ed.InspectAssetImport("audio/jump.wav")
	if err != nil {
		t.Fatal(err)
	}
	if v, over := audioInfo.EffectiveAudioFormat(); v != "mp3" || over {
		t.Fatalf("audio default format = (%q, %v), want (mp3, false)", v, over)
	}
	if v, over := audioInfo.EffectiveAudioRate(); v != 44100 || over {
		t.Fatalf("audio default rate = (%d, %v), want (44100, false)", v, over)
	}
	if mode, db, over := audioInfo.EffectiveAudioNormalization(); mode != "peakDB" || db != -2 || over {
		t.Fatalf("audio default norm = (%q, %v, %v), want (peakDB, -2, false)", mode, db, over)
	}

	// Edit audio format to wav, channels to 2, and set a loop; verify sidecar validates & parses.
	if err := ed.SetAudioImportFormat("audio/jump.wav", "wav"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAudioImportChannels("audio/jump.wav", 2); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAudioImportLoop("audio/jump.wav", &importsettings.Loop{Start: 100, End: 500}); err != nil {
		t.Fatal(err)
	}
	// Switching format to mp3 while a loop is set must be refused by importsettings validation.
	if err := ed.SetAudioImportFormat("audio/jump.wav", "mp3"); err == nil {
		t.Fatal("expected validation error setting format=mp3 while loop is configured")
	}
	rawAudio, err := os.ReadFile(filepath.Join(dir, "audio", "jump.wav.import.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	parsedAudio, err := importsettings.Parse(rawAudio)
	if err != nil {
		t.Fatalf("parse audio sidecar: %v", err)
	}
	if parsedAudio.Audio == nil || parsedAudio.Audio.Format != "wav" || parsedAudio.Audio.Channels != 2 || parsedAudio.Audio.Loop == nil || parsedAudio.Audio.Loop.End != 500 {
		t.Fatalf("unexpected audio sidecar: %+v", parsedAudio.Audio)
	}
	app.Settle()

	// 2. Font kind.
	ed.SelectFile("fonts/hud.ttf")
	app.Settle()
	for _, id := range []string{
		"id:import-font-size",
		"id:import-font-padding",
		"id:import-font-chars",
		"id:import-credit-author",
	} {
		if !app.Exists(id) {
			t.Fatalf("font inspector missing %s", id)
		}
	}
	fontInfo, err := ed.InspectAssetImport("fonts/hud.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if v, over := fontInfo.EffectiveFontSize(); v != 48 || over {
		t.Fatalf("font default size = (%d, %v), want (48, false)", v, over)
	}
	if v, over := fontInfo.EffectiveFontPadding(); v != 2 || over {
		t.Fatalf("font default padding = (%d, %v), want (2, false)", v, over)
	}
	if v, over := fontInfo.EffectiveFontChars(); v != "0123456789" || over {
		t.Fatalf("font default chars = (%q, %v), want (0123456789, false)", v, over)
	}

	pad := 4
	if err := ed.SetFontImportSize("fonts/hud.ttf", 64); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetFontImportPadding("fonts/hud.ttf", &pad); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetFontImportChars("fonts/hud.ttf", "ABCDEF"); err != nil {
		t.Fatal(err)
	}
	rawFont, err := os.ReadFile(filepath.Join(dir, "fonts", "hud.ttf.import.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	parsedFont, err := importsettings.Parse(rawFont)
	if err != nil {
		t.Fatalf("parse font sidecar: %v", err)
	}
	if parsedFont.Font == nil || parsedFont.Font.Size != 64 || *parsedFont.Font.Padding != 4 || parsedFont.Font.Chars != "ABCDEF" {
		t.Fatalf("unexpected font sidecar: %+v", parsedFont.Font)
	}
	app.Settle()

	// 3. Model kind.
	ed.SelectFile("models/hero.glb")
	app.Settle()
	for _, id := range []string{
		"id:import-model-scale",
		"id:import-model-animations",
		"id:import-credit-author",
	} {
		if !app.Exists(id) {
			t.Fatalf("model inspector missing %s", id)
		}
	}
	modelInfo, err := ed.InspectAssetImport("models/hero.glb")
	if err != nil {
		t.Fatal(err)
	}
	if v, over := modelInfo.EffectiveModelScale(); v != 0.01 || over {
		t.Fatalf("model default scale = (%v, %v), want (0.01, false)", v, over)
	}
	if v, over := modelInfo.EffectiveModelAnimations(); v || over {
		t.Fatalf("model default animations = (%v, %v), want (false, false)", v, over)
	}

	keepAnims := true
	if err := ed.SetModelImportScale("models/hero.glb", 0.5); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetModelImportAnimations("models/hero.glb", &keepAnims); err != nil {
		t.Fatal(err)
	}
	rawModel, err := os.ReadFile(filepath.Join(dir, "models", "hero.glb.import.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	parsedModel, err := importsettings.Parse(rawModel)
	if err != nil {
		t.Fatalf("parse model sidecar: %v", err)
	}
	if parsedModel.Model == nil || parsedModel.Model.Scale != 0.5 || parsedModel.Model.Animations == nil || !*parsedModel.Model.Animations {
		t.Fatalf("unexpected model sidecar: %+v", parsedModel.Model)
	}
	// Reset model fields back to target defaults.
	if err := ed.ResetAssetImportField("models/hero.glb", "scale"); err != nil {
		t.Fatal(err)
	}
	if err := ed.ResetAssetImportField("models/hero.glb", "animations"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "models", "hero.glb.import.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected empty model sidecar removed after resetting all fields, err=%v", err)
	}
}

func TestAssetImportInspectorSwitchesWithObjectSelection(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:     fixtureProjectYAML,
		project.NSGoFileName: fixtureNSGoYAML,
		"art/actors.json":    fixtureAtlasJSON,
		"scenes/room.scene.yaml": `version: 2
name: Room
layers:
  - name: actors
    objects:
      - id: hero
        sprite:
          atlas: actors
          frame: hero
`,
	}, map[string][]byte{
		"art/actors.png": encodeTestPNG(t, 32, 16),
		"art/icon.png":   encodeTestPNG(t, 16, 16),
	})

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	// Without a project open, import methods return ErrNoProject.
	if err := ed.SetImageImportFilter("art/icon.png", "nearest"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("SetImageImportFilter without project = %v, want ErrNoProject", err)
	}
	if err := ed.BuildAssets(); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("BuildAssets without project = %v, want ErrNoProject", err)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	ed.SelectObject("hero")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Initially object inspector is visible for hero.
	if !app.Exists("id:inspector-object-id") {
		t.Fatal("expected object inspector for hero")
	}

	// Selecting an image switches inspector to asset import settings.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-art/icon.png")
	app.Settle()
	if !app.Exists("id:inspector-asset-import") {
		t.Fatal("expected asset import inspector after selecting art/icon.png")
	}

	// Re-selecting the same object switches back to object inspector.
	ed.SelectObject("hero")
	app.Settle()
	if !app.Exists("id:inspector-object-id") {
		t.Fatal("expected object inspector after re-selecting hero")
	}

	// Re-selecting the same image switches back to asset import inspector.
	ed.SelectFile("art/icon.png")
	app.Settle()
	if !app.Exists("id:inspector-asset-import") {
		t.Fatal("expected asset import inspector after re-selecting art/icon.png")
	}

	// Selecting a non-asset file (game.yaml) switches back to the selected object.
	ed.SelectFile(project.FileName)
	app.Settle()
	if app.Exists("id:inspector-asset-import") {
		t.Fatal("did not expect asset import inspector for game.yaml")
	}
	if !app.Exists("id:inspector-object-id") {
		t.Fatal("expected object inspector when non-asset file is selected")
	}

	// Editing while playing returns ErrPlaying.
	if err := ed.Play(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ed.Stop() }()
	if err := ed.SetImageImportFilter("art/icon.png", "nearest"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SetImageImportFilter while playing = %v, want ErrPlaying", err)
	}
	if err := ed.BuildAssets(); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("BuildAssets while playing = %v, want ErrPlaying", err)
	}
}

func slicesContains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// Regression test for BLOCKER #1: unrelated rev bumps must not remount assetImportInspectorView
// or wipe unsaved credit drafts and validation errors.
func TestAssetImportInspectorKeepsDraftAndErrorAcrossRevBumps(t *testing.T) {
	dir := t.TempDir()
	gameYAML := `version: 1
name: DraftFixture
id: dev.example.draftfixture
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
	}, map[string][]byte{
		"art/hero.png": encodeTestPNG(t, 16, 16),
	})

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-art/hero.png")
	app.Settle()
	if !app.Exists("id:inspector-asset-import") {
		t.Fatal("expected asset import inspector")
	}

	// Type an author without license and submit so creditDraft and creditErr are populated.
	app.Focus("id:import-credit-author")
	app.Type("id:import-credit-author", "Jane Doe")
	app.Key("enter")
	app.Settle()
	if !app.Exists("id:import-credit-error") {
		t.Fatal("expected credit validation error when license is missing")
	}

	// Trigger multiple unrelated revision bumps (tool switch + output panel toggle).
	ed.SetTool(editor.ToolMove)
	ed.SetOutputPanelOpen(true)
	app.Settle()

	if got := app.Find("id:import-credit-author").Text; got != "Jane Doe" {
		t.Fatalf("import-credit-author draft lost after rev bumps: got %q, want %q", got, "Jane Doe")
	}
	if !app.Exists("id:import-credit-error") {
		t.Fatal("import-credit-error was wiped by unrelated rev bumps")
	}
}

// Regression test for BLOCKER #2 and MINOR #1:
//   - meta.image traversal paths like "../../etc/passwd" or "../secret.png" never escape outDir/root
//     and fall back cleanly to <out>/<name>.png.
//   - multi-page atlases (name-0.json) report their actual resolved JSON path in changedPaths.
func TestReloadAssetsRejectsPathTraversalAndTracksMultiPageAtlas(t *testing.T) {
	parentDir := t.TempDir()
	projectDir := filepath.Join(parentDir, "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a sentinel file outside the project root that a traversal path would reach if unchecked.
	outsideSecret := filepath.Join(parentDir, "secret.png")
	if err := os.WriteFile(outsideSecret, []byte("OUTSIDE-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	gameYAML := `version: 1
name: PathGuardFixture
id: dev.example.pathguard
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  atlases:
    - name: traversal
      in: art/raw
      out: build/atlases
    - name: multipage
      in: art/multi
      out: build/atlases
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
`
	traversalJSON := `{"frames":{"hero":{"frame":{"x":0,"y":0,"w":8,"h":8},"sourceSize":{"w":8,"h":8}}},"meta":{"image":"../../../secret.png","size":{"w":8,"h":8}}}`
	multipageJSON := `{"frames":{"tile":{"frame":{"x":0,"y":0,"w":8,"h":8},"sourceSize":{"w":8,"h":8}}},"meta":{"image":"multipage-0.png","size":{"w":8,"h":8}}}`
	fallbackPNG := encodeTestPNG(t, 8, 8)
	multiPNG := encodeTestPNG(t, 8, 8)

	writeFixtureFiles(t, projectDir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
	}, nil)

	reloader := &recordingReloader{
		AtlasMap: sceneformat.AtlasMap{
			"traversal": &sprite.Atlas{},
			"multipage": &sprite.Atlas{},
		},
	}
	ed, err := editor.New(nil, reloader)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(projectDir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	// Write built outputs after OpenProject (simulating an asset build).
	writeFixtureFiles(t, projectDir, map[string]string{
		"build/atlases/traversal.json":   traversalJSON,
		"build/atlases/multipage-0.json": multipageJSON,
	}, map[string][]byte{
		"build/atlases/traversal.png":   fallbackPNG,
		"build/atlases/multipage-0.png": multiPNG,
	})

	res, err := ed.ReloadAssets()
	if err != nil {
		t.Fatalf("ReloadAssets: %v", err)
	}
	if !slicesContains(res.Atlases, "traversal") || !slicesContains(res.Atlases, "multipage") {
		t.Fatalf("reloaded atlases = %v, want traversal and multipage", res.Atlases)
	}

	// Traversal atlas must fall back to build/atlases/traversal.png, never reading outsideSecret.
	travAtlas, err := reloader.Atlas("traversal")
	if err != nil || travAtlas == nil {
		t.Fatalf("traversal atlas missing: %v", err)
	}
	if travAtlas.Image.URI != "asset:build/atlases/traversal.png" {
		t.Fatalf("traversal Image.URI = %q, want asset:build/atlases/traversal.png", travAtlas.Image.URI)
	}
	if bytes.Equal(travAtlas.Image.Data, []byte("OUTSIDE-SECRET")) {
		t.Fatal("ReloadAssets read a file outside the project root via meta.image traversal")
	}
	if !bytes.Equal(travAtlas.Image.Data, fallbackPNG) {
		t.Fatal("traversal atlas did not load fallback build/atlases/traversal.png bytes")
	}

	// changedPaths must contain fallback traversal.png and resolved multipage-0.json (not multipage.json).
	if len(reloader.calls) == 0 {
		t.Fatal("expected AssetReloader.ReloadAssets to be called")
	}
	changed := reloader.calls[len(reloader.calls)-1]
	for _, p := range changed {
		if strings.Contains(p, "..") || strings.Contains(p, "secret.png") {
			t.Fatalf("changedPaths leaked traversal path %q: %v", p, changed)
		}
	}
	if !slicesContains(changed, "build/atlases/multipage-0.json") {
		t.Fatalf("changedPaths = %v, want build/atlases/multipage-0.json", changed)
	}
	if slicesContains(changed, "build/atlases/multipage.json") {
		t.Fatalf("changedPaths = %v, should not report nonexistent build/atlases/multipage.json", changed)
	}
}

// #880 Acceptance: selecting an image opens the import panel with an interactive slice-grid preview and draggable per-cell pivot markers;
// slicing a sheet, setting default + per-cell pivots (including via drag with Undo/Redo) and credit writes a deterministic sidecar
// that round-trips byte-for-byte unchanged, and after `game assets` (Reimport) the packed atlas frames carry those exact pivots in `sprite.LoadAtlas`.
func TestSpriteSheetSliceAndPivotPanelRoundTripAndAtlasBuild(t *testing.T) {
	dir := t.TempDir()
	gameYAML := `version: 1
name: PivotFixture
id: dev.example.pivotfixture
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  atlases:
    - name: actors
      in: art/sprites
      out: assets/atlases
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
`
	// 74x20 sheet with margin=2, spacing=2 and 16x16 cells holds 4x1 cells; count=3 keeps frames 0, 1, 2.
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
	}, map[string][]byte{
		"art/sprites/hero.png": encodeTestPNG(t, 74, 20),
	})

	atlases := sceneformat.AtlasMap{
		"actors": &sprite.Atlas{},
	}
	ed, err := editor.New(nil, atlases)
	if err != nil {
		t.Fatal(err)
	}
	ed.AssetsCommandRunner = runAssetBuildInTest
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	// Select the sprite sheet in the file browser.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-art/sprites/hero.png")
	app.Settle()

	if !app.Exists("id:import-image-preview") || !app.Exists("id:import-image-preview-canvas") {
		t.Fatal("expected image import preview canvas when an image is selected")
	}
	if got := allText(app.Find("id:import-image-preview-info")); !strings.Contains(got, "74x20 px") || !strings.Contains(got, "unsliced") {
		t.Fatalf("preview info before slice = %q, want 74x20 px unsliced", got)
	}

	// Enable slicing in the panel and configure width, height, margin, spacing, count.
	app.Press("id:import-image-slice-enabled")
	app.Settle()
	if err := ed.SetImageImportSlice("art/sprites/hero.png", &importsettings.Slice{
		Width:   16,
		Height:  16,
		Margin:  2,
		Spacing: 2,
		Count:   3,
	}); err != nil {
		t.Fatalf("SetImageImportSlice: %v", err)
	}
	app.Settle()

	if got := allText(app.Find("id:import-image-preview-info")); !strings.Contains(got, "4x1 grid (3 cells)") {
		t.Fatalf("preview info after slice = %q, want 4x1 grid (3 cells)", got)
	}

	// Preview canvas should draw the image and pivot marker arcs for all 3 cells.
	previewOps := app.Find("id:import-image-preview-canvas").Ops
	var drawImgCount, drawArcCount int
	for _, op := range previewOps {
		switch op.Kind {
		case core.DrawImage:
			drawImgCount++
		case core.DrawArc:
			drawArcCount++
		}
	}
	if drawImgCount == 0 {
		t.Fatal("expected preview canvas to draw the source image")
	}
	if drawArcCount < 6 { // 2 arcs (fill + stroke) per marker x 3 cells
		t.Fatalf("expected pivot markers for all 3 cells (>=6 DrawArc ops), got %d", drawArcCount)
	}

	// Set default pivot to bottom-center [0.5, 1] using the panel's Bottom preset button.
	app.Press("id:import-image-pivot-preset-bottom")
	app.Settle()
	info, err := ed.InspectAssetImport("art/sprites/hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if pt, over := info.EffectiveDefaultImagePivot(); !over || pt != (importsettings.PivotPoint{0.5, 1}) {
		t.Fatalf("default pivot after Bottom preset = (%v, %v), want ([0.5, 1], true)", pt, over)
	}

	// Drag cell 1's pivot marker interactively from [0.5, 1] to [0.25, 0.75] on the preview canvas.
	const canvasW, canvasH float32 = 240, 180
	startX, startY, ok := ed.ImageImportPreviewCellPoint("art/sprites/hero.png", 1, importsettings.PivotPoint{0.5, 1}, canvasW, canvasH)
	if !ok {
		t.Fatal("ImageImportPreviewCellPoint start failed")
	}
	targetX, targetY, ok := ed.ImageImportPreviewCellPoint("art/sprites/hero.png", 1, importsettings.PivotPoint{0.25, 0.75}, canvasW, canvasH)
	if !ok {
		t.Fatal("ImageImportPreviewCellPoint target failed")
	}
	sidecarPath := filepath.Join(dir, "art", "sprites", "hero.png.import.yaml")
	beforeDragBytes, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}

	if !ed.BeginImagePivotDrag("art/sprites/hero.png", startX, startY, canvasW, canvasH) {
		t.Fatal("BeginImagePivotDrag did not hit cell 1's pivot marker")
	}
	if !ed.ImagePivotDragging() || ed.SelectedImportSliceCell() != 1 {
		t.Fatalf("dragging=%v selectedCell=%d, want true and 1", ed.ImagePivotDragging(), ed.SelectedImportSliceCell())
	}
	ed.UpdateImagePivotDrag(targetX, targetY)
	// Mid-drag must not write to disk yet.
	midDragBytes, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(midDragBytes, beforeDragBytes) {
		t.Fatal("UpdateImagePivotDrag wrote sidecar before EndImagePivotDrag")
	}
	if err := ed.EndImagePivotDrag(); err != nil {
		t.Fatalf("EndImagePivotDrag: %v", err)
	}
	app.Settle()

	info, err = ed.InspectAssetImport("art/sprites/hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if pt, over, _ := info.EffectiveCellImagePivot(1); !over || pt != (importsettings.PivotPoint{0.25, 0.75}) {
		t.Fatalf("cell 1 pivot after drag = (%v, %v), want ([0.25, 0.75], true)", pt, over)
	}

	// Undo restores the exact sidecar before the drag; Redo restores cell 1's dragged pivot.
	if !ed.Undo() {
		t.Fatal("Undo after pivot drag failed")
	}
	afterUndo, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUndo, beforeDragBytes) {
		t.Fatalf("Undo did not restore pre-drag sidecar:\ngot:\n%s\nwant:\n%s", afterUndo, beforeDragBytes)
	}
	if !ed.Redo() {
		t.Fatal("Redo after pivot drag failed")
	}

	// Fill in credit section through the panel UI.
	app.Type("id:import-credit-author", "Kenney")
	app.Type("id:import-credit-license", "CC0-1.0")
	app.Submit("id:import-credit-license")
	app.Settle()

	// Verify the written sidecar parses and round-trips byte-for-byte unchanged.
	writtenBytes, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	parsed, err := importsettings.Parse(writtenBytes)
	if err != nil {
		t.Fatalf("importsettings.Parse failed: %v\n%s", err, writtenBytes)
	}
	reencoded, err := importsettings.Encode(parsed)
	if err != nil {
		t.Fatalf("importsettings.Encode failed: %v", err)
	}
	if !bytes.Equal(writtenBytes, reencoded) {
		t.Fatalf("sidecar did not round-trip unchanged:\ngot:\n%s\nwant:\n%s", writtenBytes, reencoded)
	}

	// Trigger Reimport from the panel to run `game assets` and reload the packed atlas.
	app.Press("id:import-reimport")
	deadline := time.Now().Add(5 * time.Second)
	for ed.AssetsBuilding() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		app.Settle()
	}
	app.Settle()
	if ed.AssetsBuilding() {
		t.Fatal("Reimport did not finish in time")
	}

	// Load the built atlas JSON directly via sprite.LoadAtlas and verify every frame's exact pivot.
	atlasJSON, err := os.ReadFile(filepath.Join(dir, "assets", "atlases", "actors.json"))
	if err != nil {
		t.Fatalf("read built atlas json: %v", err)
	}
	builtAtlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:assets/atlases/actors.png"}, atlasJSON)
	if err != nil {
		t.Fatalf("sprite.LoadAtlas: %v\n%s", err, atlasJSON)
	}
	if len(builtAtlas.Frames) != 3 {
		t.Fatalf("packed %d frames, want 3: %+v", len(builtAtlas.Frames), builtAtlas.Frames)
	}
	wantPivots := map[string][2]float32{
		"hero_0.png": {8, 16}, // default [0.5, 1] on 16x16
		"hero_1.png": {4, 12}, // cell 1 override [0.25, 0.75] on 16x16
		"hero_2.png": {8, 16}, // default [0.5, 1] on 16x16
	}
	for name, want := range wantPivots {
		fr, ok := builtAtlas.Frames[name]
		if !ok {
			t.Fatalf("missing packed frame %q", name)
		}
		if fr.PivotX != want[0] || fr.PivotY != want[1] {
			t.Fatalf("builtAtlas[%q] pivot = (%v, %v), want (%v, %v)", name, fr.PivotX, fr.PivotY, want[0], want[1])
		}
		// Verify live-reloaded atlas in Editor.Resolver matches too.
		reloadedFr, ok := atlases["actors"].Frames[name]
		if !ok || reloadedFr.PivotX != want[0] || reloadedFr.PivotY != want[1] {
			t.Fatalf("reloaded atlas[%q] pivot = (%v, %v), want (%v, %v)", name, reloadedFr.PivotX, reloadedFr.PivotY, want[0], want[1])
		}
	}
}

func TestImagePivotUnslicedDragCancelAndSlicePruning(t *testing.T) {
	dir := t.TempDir()
	gameYAML := `version: 1
name: UnslicedPivotFixture
id: dev.example.unslicedpivot
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  atlases:
    - name: ui
      in: art/ui
      out: assets/atlases
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
`
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
	}, map[string][]byte{
		"art/ui/icon.png": encodeTestPNG(t, 32, 16),
	})

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	const canvasW, canvasH float32 = 240, 180
	// Cancel a drag on an unsliced image: must leave no sidecar on disk.
	cx, cy, ok := ed.ImageImportPreviewCellPoint("art/ui/icon.png", -1, importsettings.PivotPoint{0.5, 0.5}, canvasW, canvasH)
	if !ok {
		t.Fatal("ImageImportPreviewCellPoint failed")
	}
	if !ed.BeginImagePivotDrag("art/ui/icon.png", cx, cy, canvasW, canvasH) {
		t.Fatal("BeginImagePivotDrag on unsliced image failed")
	}
	ed.CancelImagePivotDrag()
	if ed.ImagePivotDragging() {
		t.Fatal("CancelImagePivotDrag left drag active")
	}
	sidecarPath := filepath.Join(dir, "art", "ui", "icon.png.import.yaml")
	if _, err := os.Stat(sidecarPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no sidecar after cancelled drag, stat err = %v", err)
	}

	// Complete drag to center [0.5, 0.5] on the unsliced image.
	if !ed.BeginImagePivotDrag("art/ui/icon.png", cx, cy, canvasW, canvasH) {
		t.Fatal("BeginImagePivotDrag failed")
	}
	if err := ed.EndImagePivotDrag(); err != nil {
		t.Fatalf("EndImagePivotDrag: %v", err)
	}
	info, err := ed.InspectAssetImport("art/ui/icon.png")
	if err != nil {
		t.Fatal(err)
	}
	if pt, over := info.EffectiveDefaultImagePivot(); !over || pt != (importsettings.PivotPoint{0.5, 0.5}) {
		t.Fatalf("unsliced pivot = (%v, %v), want ([0.5, 0.5], true)", pt, over)
	}

	// Enable slicing with 2 cells, add a cell 1 override, shrink count to 1 (prunes cell 1), then disable slice (keeps default pivot).
	if err := ed.SetImageImportSlice("art/ui/icon.png", &importsettings.Slice{Width: 16, Height: 16, Count: 2}); err != nil {
		t.Fatal(err)
	}
	cell1 := importsettings.PivotPoint{0.25, 0.75}
	if err := ed.SetImageImportCellPivot("art/ui/icon.png", 1, &cell1); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetImageImportSlice("art/ui/icon.png", &importsettings.Slice{Width: 16, Height: 16, Count: 1}); err != nil {
		t.Fatalf("shrinking count should prune out-of-range cell pivot: %v", err)
	}
	info, _ = ed.InspectAssetImport("art/ui/icon.png")
	if pv, ok := info.EffectiveImagePivot(); !ok || len(pv.Cells) != 0 {
		t.Fatalf("expected cell 1 pruned when count=1, got %+v", pv)
	}

	cell0 := importsettings.PivotPoint{0.1, 0.2}
	if err := ed.SetImageImportCellPivot("art/ui/icon.png", 0, &cell0); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetImageImportSlice("art/ui/icon.png", nil); err != nil {
		t.Fatalf("clearing slice should clear pivot.cells while keeping pivot.default: %v", err)
	}
	info, _ = ed.InspectAssetImport("art/ui/icon.png")
	if pt, over := info.EffectiveDefaultImagePivot(); !over || pt != (importsettings.PivotPoint{0.5, 0.5}) {
		t.Fatalf("default pivot after clearing slice = (%v, %v), want ([0.5, 0.5], true)", pt, over)
	}
	if err := ed.ResetAssetImportField("art/ui/icon.png", "pivot"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sidecarPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected sidecar removed after resetting pivot, stat err = %v", err)
	}
}

func TestImageImportDenseGridPreviewNonFiniteDragPlayCancelAndSymlinkGuard(t *testing.T) {
	parentDir := t.TempDir()
	projectDir := filepath.Join(parentDir, "proj")
	outsidePNG := filepath.Join(parentDir, "outside.png")
	if err := os.WriteFile(outsidePNG, encodeTestPNG(t, 24, 24), 0o644); err != nil {
		t.Fatal(err)
	}

	gameYAML := `version: 1
name: DenseSliceSafety
id: dev.example.denseslice
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
assets:
  atlases:
    - name: sheet
      in: art/sheet
      out: assets/atlases
`
	roomScene := `version: 2
name: Room
layers:
  - name: actors
`
	writeFixtureFiles(t, projectDir, map[string]string{
		project.FileName:         gameYAML,
		project.NSGoFileName:     fixtureNSGoYAML,
		"scenes/room.scene.yaml": roomScene,
	}, map[string][]byte{
		"art/sheet/big.png": encodeTestPNG(t, 1024, 1024),
	})
	symlinkPath := filepath.Join(projectDir, "art", "sheet", "linked.png")
	if err := os.Symlink(outsidePNG, symlinkPath); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(projectDir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	// 1. Symlink pointing outside project root must not be read by InspectAssetImport / preview.
	linkedInfo, err := ed.InspectAssetImport("art/sheet/linked.png")
	if err != nil {
		t.Fatalf("InspectAssetImport(linked.png): %v", err)
	}
	if linkedInfo.ImageWidth != 0 || linkedInfo.ImageHeight != 0 {
		t.Fatalf("symlinked outside image leaked dimensions %dx%d, want 0x0", linkedInfo.ImageWidth, linkedInfo.ImageHeight)
	}

	// 2. 1024x1024 image with 1x1 slice (1,048,576 cells) must stay instant in DrawImageImportPreview,
	//    mounted InspectorView (numeric cell picker instead of 1M-option Select), and O(1) BeginImagePivotDrag.
	if err := ed.SetImageImportSlice("art/sheet/big.png", &importsettings.Slice{Width: 1, Height: 1}); err != nil {
		t.Fatalf("SetImageImportSlice(1x1): %v", err)
	}
	ed.SelectFile("art/sheet/big.png")

	var drawing core.Drawing
	ed.DrawImageImportPreview(&drawing, core.Size{Width: 240, Height: 180}, "art/sheet/big.png")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	})
	app.Resize(1280, 800)
	if app.Find("id:import-image-pivot-cell-select") == nil {
		t.Fatal("expected bounded cell selector to mount for dense 1x1 grid")
	}

	// 3. Non-finite coordinates are ignored by BeginImagePivotDrag / UpdateImagePivotDrag.
	if ed.BeginImagePivotDrag("art/sheet/big.png", float32(math.NaN()), 50, 240, 180) {
		t.Fatal("BeginImagePivotDrag accepted NaN")
	}
	if ed.BeginImagePivotDrag("art/sheet/big.png", float32(math.Inf(1)), 50, 240, 180) {
		t.Fatal("BeginImagePivotDrag accepted +Inf")
	}
	cx, cy, ok := ed.ImageImportPreviewCellPoint("art/sheet/big.png", 0, importsettings.PivotPoint{0.5, 0.5}, 240, 180)
	if !ok {
		t.Fatal("ImageImportPreviewCellPoint(cell 0) failed on dense grid")
	}
	if !ed.BeginImagePivotDrag("art/sheet/big.png", cx, cy, 240, 180) {
		t.Fatal("BeginImagePivotDrag failed on dense grid")
	}
	ed.UpdateImagePivotDrag(float32(math.NaN()), cy)
	ed.UpdateImagePivotDrag(cx, float32(math.Inf(-1)))

	// 4. Entering Play mode cancels an active image pivot drag cleanly.
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if ed.ImagePivotDragging() {
		t.Fatal("Play did not cancel active image pivot drag")
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
