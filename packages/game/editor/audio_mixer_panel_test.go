package editor_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// makeTestAudioWAV builds a 16-bit mono PCM WAV with an initial impulse + 6 kHz tone burst followed by trailing silence so reverb tails and low-pass attenuation produce unmistakable PCM differences.
func makeTestAudioWAV(t *testing.T) ([]byte, []float64, int) {
	t.Helper()
	const sr = 44100
	frames := sr // 1 second
	samples := make([]float64, frames)
	samples[0] = 0.9
	for i := 1; i < sr/20; i++ { // 50ms high-frequency burst
		samples[i] = 0.6 * math.Sin(2*math.Pi*6000*float64(i)/float64(sr))
	}
	wav, err := sound.EncodeWAV(samples, sr, 1)
	if err != nil {
		t.Fatalf("EncodeWAV: %v", err)
	}
	return wav, samples, sr
}

func rmsSlice(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		sum += s * s
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func nearFloat32(a, b float32) bool {
	return math.Abs(float64(a-b)) < 1e-3
}

// #909 Acceptance 1: adding a bus with a reverb in the panel, saving, reading it back from game.yaml, and undoing to remove it.
func TestAudioMixerPanelAddBusReverbSaveAndUndo(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	if app.Exists("id:audio-mixer-panel") {
		t.Fatal("audio mixer panel should start closed")
	}

	// Open the Audio Mixer panel from the toolbar.
	app.Press("id:toolbar-audio-mixer")
	app.Settle()
	if !app.Exists("id:audio-mixer-panel") {
		t.Fatal("pressing toolbar-audio-mixer did not open the panel")
	}

	// Add a bus named "cave" through the panel controls.
	app.Type("id:mixer-new-bus-name", "cave")
	app.Press("id:mixer-add-bus")
	app.Settle()
	if buses := ed.AudioBuses(); len(buses) != 1 || buses[0].Name != "cave" {
		t.Fatalf("AudioBuses after Add Bus = %+v, want [cave]", buses)
	}

	// Add a Reverb effect to "cave" and edit its parameters through the panel's NumberInputs.
	app.Press("id:mixer-add-effect-kind")
	app.Press("role:option Reverb")
	app.Press("id:mixer-add-effect")
	app.Settle()
	if got := ed.AudioBuses()[0].Effects; len(got) != 1 || got[0].Reverb == nil {
		t.Fatalf("effects after Add Effect = %+v, want one Reverb", got)
	}

	app.Clear("label:Effect 0 Room Size")
	app.Type("label:Effect 0 Room Size", "0.8")
	app.Submit("label:Effect 0 Room Size")
	app.Clear("label:Effect 0 Damping")
	app.Type("label:Effect 0 Damping", "0.3")
	app.Submit("label:Effect 0 Damping")
	app.Clear("label:Effect 0 Wet")
	app.Type("label:Effect 0 Wet", "0.6")
	app.Submit("label:Effect 0 Wet")
	app.Settle()

	// Save via toolbar Save and read back from game.yaml on disk.
	app.Press("id:toolbar-save")
	app.Settle()
	saved, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check after Save: %v", err)
	}
	if saved.Audio == nil || len(saved.Audio.Buses) != 1 {
		t.Fatalf("saved game.yaml audio = %+v, want 1 bus", saved.Audio)
	}
	bus := saved.Audio.Buses[0]
	if bus.Name != "cave" || len(bus.Effects) != 1 || bus.Effects[0].Reverb == nil {
		t.Fatalf("saved bus = %+v, want cave with Reverb", bus)
	}
	rv := bus.Effects[0].Reverb
	if math.Abs(rv.RoomSize-0.8) > 1e-6 || math.Abs(rv.Damping-0.3) > 1e-6 || math.Abs(rv.Wet-0.6) > 1e-6 {
		t.Fatalf("saved reverb = %+v, want roomSize=0.8 damping=0.3 wet=0.6", rv)
	}

	// Undo edits: 3 param edits + 1 add reverb + 1 add bus = 5 undos restore game.yaml to no audio section.
	for range 5 {
		app.Press("id:toolbar-undo")
	}
	app.Settle()
	if ed.Project.Audio != nil {
		t.Fatalf("Project.Audio after Undo = %+v, want nil", ed.Project.Audio)
	}
	if !app.Exists("id:mixer-empty-buses") {
		t.Fatal("expected empty buses message after undoing bus addition")
	}

	// Saving after Undo writes game.yaml back without the bus or reverb.
	app.Press("id:toolbar-save")
	app.Settle()
	reverted, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check after Undo+Save: %v", err)
	}
	if reverted.Audio != nil {
		t.Fatalf("saved game.yaml after Undo = %+v, want nil Audio", reverted.Audio)
	}

	// Redo brings the bus and reverb back.
	for range 5 {
		app.Press("id:toolbar-redo")
	}
	app.Settle()
	if got := ed.AudioBuses(); len(got) != 1 || got[0].Name != "cave" || len(got[0].Effects) != 1 || got[0].Effects[0].Reverb == nil || math.Abs(got[0].Effects[0].Reverb.Wet-0.6) > 1e-6 {
		t.Fatalf("AudioBuses after Redo = %+v", got)
	}
}

func TestAudioMixerBusAndEffectChainOperationsAndValidation(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}

	// Without a project, edits fail with ErrNoProject.
	if err := ed.AddAudioBus("sfx"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("AddAudioBus without project = %v, want ErrNoProject", err)
	}
	if err := ed.SetAudioMasterVolume(0.5); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("SetAudioMasterVolume without project = %v, want ErrNoProject", err)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Master volume valid & invalid values.
	if ed.AudioMasterVolume() != 1 {
		t.Fatalf("initial master volume = %v, want 1", ed.AudioMasterVolume())
	}
	for _, bad := range []float32{-0.01, 1.01, float32(math.NaN()), float32(math.Inf(1))} {
		if err := ed.SetAudioMasterVolume(bad); err == nil {
			t.Fatalf("SetAudioMasterVolume(%v) succeeded, want validation error", bad)
		}
	}
	if ed.History.CanUndo() {
		t.Fatal("refused master volume edits pushed to history")
	}
	if err := ed.SetAudioMasterVolume(0.75); err != nil {
		t.Fatalf("SetAudioMasterVolume(0.75): %v", err)
	}
	if !nearFloat32(ed.AudioMasterVolume(), 0.75) {
		t.Fatalf("AudioMasterVolume = %v, want 0.75", ed.AudioMasterVolume())
	}

	// Add, rename, reorder, and delete buses.
	if err := ed.AddAudioBus(""); err == nil {
		t.Fatal("AddAudioBus empty name succeeded")
	}
	if err := ed.AddAudioBus("bad name"); err == nil {
		t.Fatal("AddAudioBus spaced name succeeded")
	}
	if err := ed.AddAudioBus("music"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddAudioBus("music"); err == nil {
		t.Fatal("AddAudioBus duplicate name succeeded")
	}
	if err := ed.AddAudioBus("sfx"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddAudioBus("ui"); err != nil {
		t.Fatal(err)
	}

	// Reorder buses: move "ui" (index 2) to index 0 -> [ui, music, sfx].
	if err := ed.MoveAudioBus(-1, 0); err == nil {
		t.Fatal("MoveAudioBus(-1, 0) succeeded")
	}
	if err := ed.MoveAudioBus(0, 3); err == nil {
		t.Fatal("MoveAudioBus(0, 3) succeeded")
	}
	if err := ed.MoveAudioBus(2, 0); err != nil {
		t.Fatalf("MoveAudioBus(2, 0): %v", err)
	}
	buses := ed.AudioBuses()
	if len(buses) != 3 || buses[0].Name != "ui" || buses[1].Name != "music" || buses[2].Name != "sfx" {
		t.Fatalf("buses after reorder = %+v, want [ui, music, sfx]", buses)
	}

	// Rename bus: valid and invalid cases.
	if err := ed.RenameAudioBus("missing", "x"); err == nil {
		t.Fatal("RenameAudioBus missing succeeded")
	}
	if err := ed.RenameAudioBus("ui", "music"); err == nil {
		t.Fatal("RenameAudioBus duplicate succeeded")
	}
	if err := ed.RenameAudioBus("ui", "bad name"); err == nil {
		t.Fatal("RenameAudioBus invalid name succeeded")
	}
	if err := ed.RenameAudioBus("ui", "voice"); err != nil {
		t.Fatalf("RenameAudioBus: %v", err)
	}
	if ed.AudioBuses()[0].Name != "voice" {
		t.Fatalf("bus 0 after rename = %q, want voice", ed.AudioBuses()[0].Name)
	}

	// Bus volume valid & invalid values.
	for _, bad := range []float32{-0.1, 1.5, float32(math.NaN())} {
		if err := ed.SetAudioBusVolume("sfx", bad); err == nil {
			t.Fatalf("SetAudioBusVolume(%v) succeeded", bad)
		}
	}
	if err := ed.SetAudioBusVolume("sfx", 0.4); err != nil {
		t.Fatal(err)
	}
	if v, ok := ed.AudioBusVolume("sfx"); !ok || !nearFloat32(v, 0.4) {
		t.Fatalf("AudioBusVolume(sfx) = %v, %v, want 0.4, true", v, ok)
	}

	// Add every supported effect kind to "sfx", reorder them, edit them, remove one.
	for i, kind := range editor.AudioEffectKinds {
		gotIdx, err := ed.AddAudioBusEffectKind("sfx", kind)
		if err != nil {
			t.Fatalf("AddAudioBusEffectKind(%q): %v", kind, err)
		}
		if gotIdx != i {
			t.Fatalf("AddAudioBusEffectKind(%q) index = %d, want %d", kind, gotIdx, i)
		}
	}
	if _, err := ed.AddAudioBusEffectKind("sfx", editor.AudioEffectKind("bogus")); err == nil {
		t.Fatal("AddAudioBusEffectKind(bogus) succeeded")
	}

	// Reorder effects: move Bitcrush (5) to index 0.
	if err := ed.MoveAudioBusEffect("sfx", 5, 0); err != nil {
		t.Fatalf("MoveAudioBusEffect: %v", err)
	}
	if kind := editor.AudioEffectKindOf(ed.AudioBuses()[2].Effects[0]); kind != editor.AudioEffectBitcrush {
		t.Fatalf("effect 0 after reorder = %q, want bitcrush", kind)
	}
	if err := ed.MoveAudioBusEffect("sfx", 0, 99); err == nil {
		t.Fatal("MoveAudioBusEffect out of range succeeded")
	}

	// Invalid out-of-range effect parameters are rejected matching project.Validate.
	badEffects := []struct {
		name string
		fx   project.Effect
	}{
		{"empty", project.Effect{}},
		{"two-fields", project.Effect{Gain: &project.Gain{Factor: 1}, Reverb: &project.Reverb{Wet: 0.5}}},
		{"lowPass-cutoff-zero", project.Effect{LowPass: &project.Filter{CutoffHz: 0, Q: 0.7}}},
		{"lowPass-q-neg", project.Effect{LowPass: &project.Filter{CutoffHz: 1000, Q: -0.1}}},
		{"highPass-cutoff-nan", project.Effect{HighPass: &project.Filter{CutoffHz: math.NaN(), Q: 0}}},
		{"delay-time-zero", project.Effect{Delay: &project.Delay{Time: 0, Feedback: 0.5, Wet: 0.5}}},
		{"delay-feedback-over", project.Effect{Delay: &project.Delay{Time: 100 * time.Millisecond, Feedback: 1.0, Wet: 0.5}}},
		{"delay-wet-neg", project.Effect{Delay: &project.Delay{Time: 100 * time.Millisecond, Feedback: 0.5, Wet: -0.1}}},
		{"reverb-room-over", project.Effect{Reverb: &project.Reverb{RoomSize: 1.2, Damping: 0.5, Wet: 0.5}}},
		{"reverb-damping-neg", project.Effect{Reverb: &project.Reverb{RoomSize: 0.5, Damping: -0.2, Wet: 0.5}}},
		{"reverb-wet-over", project.Effect{Reverb: &project.Reverb{RoomSize: 0.5, Damping: 0.5, Wet: 1.1}}},
		{"gain-neg", project.Effect{Gain: &project.Gain{Factor: -0.5}}},
		{"gain-inf", project.Effect{Gain: &project.Gain{Factor: math.Inf(1)}}},
		{"bitcrush-bits-zero", project.Effect{Bitcrush: &project.Bitcrush{Bits: 0, Downsample: 1}}},
		{"bitcrush-bits-16", project.Effect{Bitcrush: &project.Bitcrush{Bits: 16, Downsample: 1}}},
		{"bitcrush-downsample-neg", project.Effect{Bitcrush: &project.Bitcrush{Bits: 8, Downsample: -1}}},
	}
	histLen := ed.History.Len()
	for _, tc := range badEffects {
		if _, err := ed.AddAudioBusEffect("sfx", tc.fx); err == nil {
			t.Fatalf("AddAudioBusEffect(%s) succeeded, want validation error", tc.name)
		}
		if err := ed.SetAudioBusEffect("sfx", 0, tc.fx); err == nil {
			t.Fatalf("SetAudioBusEffect(%s) succeeded, want validation error", tc.name)
		}
		// Single-field out-of-range effects clamp cleanly into project.Validate's valid range.
		if editor.AudioEffectKindOf(tc.fx) != "" {
			clamped := editor.ClampAudioEffect(tc.fx)
			if err := ed.SetAudioBusEffect("sfx", 0, clamped); err != nil {
				t.Fatalf("SetAudioBusEffect(ClampAudioEffect(%s)) failed: %v (clamped=%+v)", tc.name, err, clamped)
			}
			ed.Undo()
		}
	}
	if ed.History.Index() != histLen {
		t.Fatalf("history index = %d after rejected/undone edits, want %d", ed.History.Index(), histLen)
	}

	// Remove an effect and undo it.
	if err := ed.RemoveAudioBusEffect("sfx", 0); err != nil {
		t.Fatal(err)
	}
	if got := len(ed.AudioBuses()[2].Effects); got != 5 {
		t.Fatalf("effects after remove = %d, want 5", got)
	}
	ed.Undo()
	if got := len(ed.AudioBuses()[2].Effects); got != 6 {
		t.Fatalf("effects after undo remove = %d, want 6", got)
	}

	// Delete buses and verify UI reorder/rename/delete buttons too.
	if err := ed.DeleteAudioBus("missing"); err == nil {
		t.Fatal("DeleteAudioBus(missing) succeeded")
	}
	if err := ed.DeleteAudioBus("voice"); err != nil {
		t.Fatal(err)
	}
	if err := ed.DeleteAudioBus("music"); err != nil {
		t.Fatal(err)
	}
	if err := ed.DeleteAudioBus("sfx"); err != nil {
		t.Fatal(err)
	}
	// Master was explicitly set to 0.75, so Audio stays non-nil with empty Buses.
	if ed.Project.Audio == nil || len(ed.Project.Audio.Buses) != 0 || !nearFloat32(*ed.Project.Audio.Master, 0.75) {
		t.Fatalf("Audio after deleting all buses with master set = %+v", ed.Project.Audio)
	}

	// UI buttons for reorder, rename, and delete.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))
	app.Press("id:toolbar-audio-mixer")
	app.Type("id:mixer-new-bus-name", "alpha")
	app.Press("id:mixer-add-bus")
	app.Type("id:mixer-new-bus-name", "beta")
	app.Press("id:mixer-add-bus")
	app.Settle()

	// Move alpha (0) down -> [beta, alpha].
	app.Press("id:mixer-bus-0-down")
	app.Settle()
	if got := ed.AudioBuses(); len(got) != 2 || got[0].Name != "beta" || got[1].Name != "alpha" {
		t.Fatalf("buses after UI down = %+v, want [beta, alpha]", got)
	}

	// Rename selected bus ("beta") to "bgm".
	app.Press("id:mixer-bus-0-select")
	app.Clear("id:mixer-bus-rename-input")
	app.Type("id:mixer-bus-rename-input", "bgm")
	app.Press("id:mixer-bus-rename")
	app.Settle()
	if got := ed.AudioBuses(); got[0].Name != "bgm" {
		t.Fatalf("bus 0 after UI rename = %q, want bgm", got[0].Name)
	}

	// Delete "alpha" via UI delete button.
	app.Press("id:mixer-bus-1-delete")
	app.Settle()
	if got := ed.AudioBuses(); len(got) != 1 || got[0].Name != "bgm" {
		t.Fatalf("buses after UI delete = %+v, want [bgm]", got)
	}

	// Play mode refuses audio mixer edits.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		if err := ed.SetAudioBusVolume("bgm", 0.2); !errors.Is(err, editor.ErrPlaying) {
			t.Errorf("SetAudioBusVolume while playing = %v, want ErrPlaying", err)
		}
		if err := ed.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	app.Settle()
}

// #909 Acceptance 2: auditioning a sound through a bus applies the bus's volume and effects identically to ConfigureMixer + sound.Mixer, and updates live as volumes and effects change.
func TestAudioMixerAuditionAppliesBusVolumeAndEffectsLikeRuntime(t *testing.T) {
	dir := writeEditorProject(t)
	rawWAV, drySamples, sr := makeTestAudioWAV(t)
	audioRel := "audio/impulse.wav"
	audioFull := filepath.Join(dir, filepath.FromSlash(audioRel))
	if err := os.MkdirAll(filepath.Dir(audioFull), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioFull, rawWAV, 0o644); err != nil {
		t.Fatal(err)
	}
	// Also write a non-WAV file to test dry vs effect-bus handling.
	oggRel := "audio/loop.ogg"
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(oggRel)), []byte("OggS-fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// ProjectAudioFiles lists project audio files in sorted order.
	audioFiles, err := ed.ProjectAudioFiles()
	if err != nil {
		t.Fatalf("ProjectAudioFiles: %v", err)
	}
	if len(audioFiles) != 2 || audioFiles[0] != audioRel || audioFiles[1] != oggRel {
		t.Fatalf("ProjectAudioFiles = %v, want [%s %s]", audioFiles, audioRel, oggRel)
	}

	// Configure master = 0.8, bus "cave" volume = 0.5 with Reverb(0.75, 0.4, 0.6).
	if err := ed.SetAudioMasterVolume(0.8); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddAudioBus("cave"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetAudioBusVolume("cave", 0.5); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.AddAudioBusEffect("cave", project.Effect{
		Reverb: &project.Reverb{RoomSize: 0.75, Damping: 0.4, Wet: 0.6},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveProject(); err != nil {
		t.Fatal(err)
	}

	// Build an independent reference runtime mixer using project.Check + ConfigureMixer + ProcessSource.
	savedProj, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check: %v", err)
	}
	var refMixer *sound.Mixer
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		refMixer = sound.NewMixer(cx, "game")
		savedProj.ConfigureMixer(refMixer)
		return core.View{}
	}, nsgotest.Environment(core.Environment{Audio: true}))

	refProcessed, err := refMixer.ProcessSource("cave", audio.Source{Data: rawWAV})
	if err != nil {
		t.Fatalf("refMixer.ProcessSource: %v", err)
	}
	refGain := refMixer.Master() * refMixer.Volume("cave")

	// Start EditorView in nsgotest with Audio enabled and register the expected processed WAV duration.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900), nsgotest.Environment(core.Environment{Audio: true}), func(a *nsgotest.App) {
		a.AudioDurations = map[string]time.Duration{
			string(rawWAV):            time.Second,
			string(refProcessed.Data): time.Second,
		}
	})

	// Open the Audio Mixer panel, pick audio/impulse.wav, and press Audition.
	app.Press("id:toolbar-audio-mixer")
	app.Press("id:mixer-audition-file")
	app.Press("role:option " + audioRel)
	app.Press("id:mixer-audition-play")
	app.Settle()

	if !ed.Auditioning() || ed.AuditionBus() != "cave" || ed.AuditionFile() != audioRel {
		t.Fatalf("audition state = (%v, %q, %q), want (true, cave, %s)", ed.Auditioning(), ed.AuditionBus(), ed.AuditionFile(), audioRel)
	}
	if !nearFloat32(ed.AuditionGain(), refGain) {
		t.Fatalf("AuditionGain = %v, want %v (master*bus)", ed.AuditionGain(), refGain)
	}
	if !bytes.Equal(ed.AuditionSource().Data, refProcessed.Data) {
		t.Fatal("audition processed WAV differs from reference ConfigureMixer + sound.Mixer.ProcessSource output")
	}
	if len(app.Players) != 1 {
		t.Fatalf("len(app.Players) = %d, want 1", len(app.Players))
	}
	p0 := app.Players[0]
	if !p0.Playing || !nearFloat32(p0.Volume, refGain) {
		t.Fatalf("audition player = playing:%v volume:%v, want playing:true volume:%v", p0.Playing, p0.Volume, refGain)
	}
	if !bytes.Equal(p0.Source.Data, refProcessed.Data) {
		t.Fatal("audio.Player received different WAV bytes than ConfigureMixer + sound.Mixer.ProcessSource")
	}

	// Verify non-zero processed PCM difference between dry and reverb-processed audio:
	// the trailing window (200ms..600ms) is silent in drySamples, but carries a measurable reverb tail after bus processing.
	wetSamples, gotSR, gotCh, err := sound.DecodeWAV(p0.Source.Data)
	if err != nil {
		t.Fatalf("DecodeWAV(audition): %v", err)
	}
	if gotSR != sr || gotCh != 1 || len(wetSamples) != len(drySamples) {
		t.Fatalf("decoded audition WAV format = sr:%d ch:%d len:%d", gotSR, gotCh, len(wetSamples))
	}
	dryTailRMS := rmsSlice(drySamples[sr/5 : 3*sr/5])
	wetTailRMS := rmsSlice(wetSamples[sr/5 : 3*sr/5])
	if dryTailRMS != 0 {
		t.Fatalf("expected silent dry tail, got RMS %v", dryTailRMS)
	}
	if wetTailRMS <= 1e-4 {
		t.Fatalf("reverb bus produced negligible tail RMS %v, want > 1e-4", wetTailRMS)
	}

	// Live volume edits update the playing audio.Player immediately without restarting it.
	app.Post(func() {
		if err := ed.SetAudioBusVolume("cave", 0.25); err != nil {
			t.Errorf("SetAudioBusVolume: %v", err)
		}
	})
	app.Settle()
	if len(app.Players) != 1 {
		t.Fatalf("volume edit created extra player (count %d), want live update on existing player", len(app.Players))
	}
	if !nearFloat32(p0.Volume, 0.8*0.25) {
		t.Fatalf("player volume after bus volume edit = %v, want 0.20", p0.Volume)
	}
	app.Post(func() {
		if err := ed.SetAudioMasterVolume(0.5); err != nil {
			t.Errorf("SetAudioMasterVolume: %v", err)
		}
	})
	app.Settle()
	if !nearFloat32(p0.Volume, 0.5*0.25) {
		t.Fatalf("player volume after master volume edit = %v, want 0.125", p0.Volume)
	}

	// Live effect chain edit: adding a LowPass(500, 0) immediately re-processes and swaps playback.
	app.Post(func() {
		if _, err := ed.AddAudioBusEffect("cave", project.Effect{
			LowPass: &project.Filter{CutoffHz: 500, Q: 0},
		}); err != nil {
			t.Errorf("AddAudioBusEffect(lowPass): %v", err)
		}
	})
	app.Settle()
	if len(app.Players) != 2 {
		t.Fatalf("len(app.Players) after live effect addition = %d, want 2", len(app.Players))
	}
	if !p0.Closed {
		t.Fatal("previous audition player was not closed when effect chain changed")
	}
	p1 := app.Players[1]
	// Compute expected reference WAV for Reverb + LowPass chain.
	refChain := sound.EffectChain{sound.Reverb(0.75, 0.4, 0.6), sound.LowPass(500, 0)}
	wantLPReverb, err := refChain.ProcessWAV(rawWAV)
	if err != nil {
		t.Fatalf("refChain.ProcessWAV: %v", err)
	}
	if !bytes.Equal(p1.Source.Data, wantLPReverb) {
		t.Fatal("live-updated audition WAV does not match Reverb + LowPass chain output")
	}
	if bytes.Equal(p1.Source.Data, p0.Source.Data) {
		t.Fatal("adding LowPass did not change audition PCM output")
	}
	// Verify 6 kHz burst energy is strongly attenuated by the LowPass(500, 0).
	lpSamples, _, _, err := sound.DecodeWAV(p1.Source.Data)
	if err != nil {
		t.Fatalf("DecodeWAV(lp): %v", err)
	}
	burstBefore := rmsSlice(wetSamples[sr/100 : sr/25])
	burstAfter := rmsSlice(lpSamples[sr/100 : sr/25])
	if burstAfter >= burstBefore*0.2 {
		t.Fatalf("LowPass did not attenuate 6 kHz burst: before=%v after=%v", burstBefore, burstAfter)
	}

	// Undo while auditioning reverts the LowPass and immediately restores the Reverb-only playback.
	app.Press("id:toolbar-undo")
	app.Settle()
	if len(app.Players) != 3 {
		t.Fatalf("len(app.Players) after Undo = %d, want 3", len(app.Players))
	}
	if !bytes.Equal(app.Players[2].Source.Data, refProcessed.Data) {
		t.Fatal("Undo during audition did not restore Reverb-only processed WAV")
	}

	// Removing the last effect on the bus switches live playback back to dry PCM.
	app.Post(func() {
		if err := ed.RemoveAudioBusEffect("cave", 0); err != nil {
			t.Errorf("RemoveAudioBusEffect: %v", err)
		}
	})
	app.Settle()
	if len(app.Players) != 4 {
		t.Fatalf("len(app.Players) after removing all effects = %d, want 4", len(app.Players))
	}
	if !bytes.Equal(app.Players[3].Source.Data, rawWAV) {
		t.Fatal("removing all bus effects did not revert audition source to dry WAV")
	}

	// Pressing Stop stops auditioning and closes the active player.
	app.Press("id:mixer-audition-play")
	app.Settle()
	if ed.Auditioning() {
		t.Fatal("Stop did not end auditioning")
	}
	if !app.Players[3].Closed {
		t.Fatal("Stop did not close active audition player")
	}

	// Auditioning a non-WAV file succeeds on a dry bus and reports an error when bus effects require PCM WAV.
	app.Post(func() {
		if err := ed.AuditionAudioBus("cave", oggRel); err != nil {
			t.Errorf("AuditionAudioBus(dry, ogg): %v", err)
		}
		ed.StopAudition()
		if _, err := ed.AddAudioBusEffectKind("cave", editor.AudioEffectReverb); err != nil {
			t.Errorf("AddAudioBusEffectKind: %v", err)
		}
		if err := ed.AuditionAudioBus("cave", oggRel); err == nil || !strings.Contains(err.Error(), "WAV") {
			t.Errorf("AuditionAudioBus(reverb, ogg) = %v, want WAV decode error", err)
		}
		// Path traversal outside project root is refused.
		if err := ed.AuditionAudioBus("cave", "../outside.wav"); err == nil {
			t.Errorf("AuditionAudioBus path traversal succeeded")
		}
	})
	app.Settle()
}
