package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// AudioEffectKind identifies one DSP effect type supported by project.Effect in game.yaml.
type AudioEffectKind string

const (
	AudioEffectLowPass  AudioEffectKind = "lowPass"
	AudioEffectHighPass AudioEffectKind = "highPass"
	AudioEffectDelay    AudioEffectKind = "delay"
	AudioEffectReverb   AudioEffectKind = "reverb"
	AudioEffectGain     AudioEffectKind = "gain"
	AudioEffectBitcrush AudioEffectKind = "bitcrush"
)

// AudioEffectKinds lists every effect kind supported by the audio bus mixer in display order.
var AudioEffectKinds = []AudioEffectKind{
	AudioEffectLowPass,
	AudioEffectHighPass,
	AudioEffectDelay,
	AudioEffectReverb,
	AudioEffectGain,
	AudioEffectBitcrush,
}

// Label returns a human-readable display name for kind.
func (k AudioEffectKind) Label() string {
	switch k {
	case AudioEffectLowPass:
		return "LowPass"
	case AudioEffectHighPass:
		return "HighPass"
	case AudioEffectDelay:
		return "Delay"
	case AudioEffectReverb:
		return "Reverb"
	case AudioEffectGain:
		return "Gain"
	case AudioEffectBitcrush:
		return "Bitcrush"
	default:
		return string(k)
	}
}

func (e *Editor) localizedAudioEffectKind(k AudioEffectKind) string {
	switch k {
	case AudioEffectLowPass:
		return e.T("audioMixer.effect.lowPass")
	case AudioEffectHighPass:
		return e.T("audioMixer.effect.highPass")
	case AudioEffectDelay:
		return e.T("audioMixer.effect.delay")
	case AudioEffectReverb:
		return e.T("audioMixer.effect.reverb")
	case AudioEffectGain:
		return e.T("audioMixer.effect.gain")
	case AudioEffectBitcrush:
		return e.T("audioMixer.effect.bitcrush")
	default:
		return k.Label()
	}
}

// DefaultAudioEffect returns a valid project.Effect with practical defaults within project.Validate's ranges.
func DefaultAudioEffect(kind AudioEffectKind) (project.Effect, error) {
	switch kind {
	case AudioEffectLowPass:
		return project.Effect{LowPass: &project.Filter{CutoffHz: 1200, Q: 0}}, nil
	case AudioEffectHighPass:
		return project.Effect{HighPass: &project.Filter{CutoffHz: 400, Q: 0}}, nil
	case AudioEffectDelay:
		return project.Effect{Delay: &project.Delay{Time: 250 * time.Millisecond, Feedback: 0.35, Wet: 0.35}}, nil
	case AudioEffectReverb:
		return project.Effect{Reverb: &project.Reverb{RoomSize: 0.5, Damping: 0.5, Wet: 0.35}}, nil
	case AudioEffectGain:
		return project.Effect{Gain: &project.Gain{Factor: 1}}, nil
	case AudioEffectBitcrush:
		return project.Effect{Bitcrush: &project.Bitcrush{Bits: 8, Downsample: 2}}, nil
	default:
		return project.Effect{}, fmt.Errorf("editor: unknown audio effect kind %q", kind)
	}
}

// AudioEffectKindOf returns the AudioEffectKind configured on fx, or empty when none or more than one is set.
func AudioEffectKindOf(fx project.Effect) AudioEffectKind {
	var kind AudioEffectKind
	count := 0
	if fx.LowPass != nil {
		kind = AudioEffectLowPass
		count++
	}
	if fx.HighPass != nil {
		kind = AudioEffectHighPass
		count++
	}
	if fx.Delay != nil {
		kind = AudioEffectDelay
		count++
	}
	if fx.Reverb != nil {
		kind = AudioEffectReverb
		count++
	}
	if fx.Gain != nil {
		kind = AudioEffectGain
		count++
	}
	if fx.Bitcrush != nil {
		kind = AudioEffectBitcrush
		count++
	}
	if count != 1 {
		return ""
	}
	return kind
}

// ClampAudioEffect returns a copy of fx with every parameter clamped into project.Validate's valid range.
func ClampAudioEffect(fx project.Effect) project.Effect {
	out := cloneAudioEffect(fx)
	switch {
	case out.LowPass != nil:
		clampFilter(out.LowPass)
	case out.HighPass != nil:
		clampFilter(out.HighPass)
	case out.Delay != nil:
		if out.Delay.Time < time.Millisecond {
			out.Delay.Time = time.Millisecond
		}
		out.Delay.Feedback = clampFiniteRange(out.Delay.Feedback, 0, 0.99)
		out.Delay.Wet = clampFiniteRange(out.Delay.Wet, 0, 1)
	case out.Reverb != nil:
		out.Reverb.RoomSize = clampFiniteRange(out.Reverb.RoomSize, 0, 1)
		out.Reverb.Damping = clampFiniteRange(out.Reverb.Damping, 0, 1)
		out.Reverb.Wet = clampFiniteRange(out.Reverb.Wet, 0, 1)
	case out.Gain != nil:
		if !isFinite64(out.Gain.Factor) || out.Gain.Factor < 0 {
			out.Gain.Factor = 0
		}
	case out.Bitcrush != nil:
		if out.Bitcrush.Bits < 1 {
			out.Bitcrush.Bits = 1
		} else if out.Bitcrush.Bits > 15 {
			out.Bitcrush.Bits = 15
		}
		if out.Bitcrush.Downsample < 0 {
			out.Bitcrush.Downsample = 0
		}
	}
	return out
}

func clampFilter(f *project.Filter) {
	if !isFinite64(f.CutoffHz) || f.CutoffHz <= 0 {
		f.CutoffHz = 1
	}
	if !isFinite64(f.Q) || f.Q < 0 {
		f.Q = 0
	}
}

func clampFiniteRange(v, lo, hi float64) float64 {
	if !isFinite64(v) {
		return lo
	}
	return min(max(v, lo), hi)
}

func isFinite64(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func cloneAudioEffect(e project.Effect) project.Effect {
	var out project.Effect
	if e.LowPass != nil {
		v := *e.LowPass
		out.LowPass = &v
	}
	if e.HighPass != nil {
		v := *e.HighPass
		out.HighPass = &v
	}
	if e.Delay != nil {
		v := *e.Delay
		out.Delay = &v
	}
	if e.Reverb != nil {
		v := *e.Reverb
		out.Reverb = &v
	}
	if e.Gain != nil {
		v := *e.Gain
		out.Gain = &v
	}
	if e.Bitcrush != nil {
		v := *e.Bitcrush
		out.Bitcrush = &v
	}
	return out
}

// audioMixerState holds the audio bus mixer's transient UI state and live audition sound.Mixer.
type audioMixerState struct {
	selectedBus    string
	selectedFile   string
	loop           bool
	mixer          *sound.Mixer
	mixerScope     *core.Scope
	knownBuses     map[string]struct{}
	music          *sound.Music
	auditioning    bool
	auditionBus    string
	auditionFile   string
	auditionRaw    []byte
	auditionChain  string
	auditionSource audio.Source
}

// AudioMixerOpen reports whether the Settings view is open on the Audio buses page.
func (e *Editor) AudioMixerOpen() bool {
	return e != nil && e.settingsOpen && e.settingsPage == SettingsPageAudio
}

// SetAudioMixerOpen opens the Settings view on the Audio buses page, or closes Settings when open is false and the Audio buses page is active. Closing stops any active audition.
func (e *Editor) SetAudioMixerOpen(open bool) {
	if e == nil {
		return
	}
	if open {
		e.OpenSettingsPage(SettingsPageAudio)
		return
	}
	if e.AudioMixerOpen() {
		e.CloseSettings()
		return
	}
	if e.audioMixer.auditioning {
		e.stopAudioAudition()
		e.notify()
	}
}

// SelectedAudioBus returns the currently selected audio bus name in the mixer panel, falling back to the first declared bus when the selection is unset or no longer exists.
func (e *Editor) SelectedAudioBus() string {
	if e == nil || e.Project == nil || e.Project.Audio == nil || len(e.Project.Audio.Buses) == 0 {
		return ""
	}
	if e.audioMixer.selectedBus != "" {
		for _, b := range e.Project.Audio.Buses {
			if b.Name == e.audioMixer.selectedBus {
				return b.Name
			}
		}
	}
	return e.Project.Audio.Buses[0].Name
}

// SelectAudioBus selects bus in the mixer panel when it exists in the open project.
func (e *Editor) SelectAudioBus(name string) bool {
	if e == nil || e.findAudioBusIndex(name) < 0 {
		return false
	}
	if e.audioMixer.selectedBus == name {
		return true
	}
	e.audioMixer.selectedBus = name
	e.notify()
	return true
}

// SelectedAudioFile returns the project-relative audio file path selected for auditioning.
func (e *Editor) SelectedAudioFile() string {
	if e == nil {
		return ""
	}
	return e.audioMixer.selectedFile
}

// SelectAudioFile sets the project-relative audio file path selected for auditioning.
func (e *Editor) SelectAudioFile(relPath string) {
	if e == nil || e.audioMixer.selectedFile == relPath {
		return
	}
	e.audioMixer.selectedFile = relPath
	e.notify()
}

// AuditionLoop reports whether audition playback loops.
func (e *Editor) AuditionLoop() bool {
	return e != nil && e.audioMixer.loop
}

// SetAuditionLoop controls whether audition playback loops.
func (e *Editor) SetAuditionLoop(loop bool) {
	if e == nil || e.audioMixer.loop == loop {
		return
	}
	e.audioMixer.loop = loop
	if e.audioMixer.music != nil {
		e.audioMixer.music.Loop = loop
	}
	e.notify()
}

// Auditioning reports whether the mixer panel is actively auditioning a sound through a bus.
func (e *Editor) Auditioning() bool {
	return e != nil && e.audioMixer.auditioning
}

// AuditionBus returns the bus name currently being auditioned, or empty when idle.
func (e *Editor) AuditionBus() string {
	if e == nil || !e.audioMixer.auditioning {
		return ""
	}
	return e.audioMixer.auditionBus
}

// AuditionFile returns the project-relative path currently being auditioned, or empty when idle.
func (e *Editor) AuditionFile() string {
	if e == nil || !e.audioMixer.auditioning {
		return ""
	}
	return e.audioMixer.auditionFile
}

// AuditionSource returns the processed audio.Source currently playing through the audition mixer.
func (e *Editor) AuditionSource() audio.Source {
	if e == nil || !e.audioMixer.auditioning {
		return audio.Source{}
	}
	return e.audioMixer.auditionSource
}

// AuditionMixer returns the editor's sound.Mixer configured via project.File.ConfigureMixer, initializing it on the attached core.Scope when available.
func (e *Editor) AuditionMixer() *sound.Mixer {
	if e == nil {
		return nil
	}
	if e.scope != nil {
		e.ensureAudioMixer()
	}
	return e.audioMixer.mixer
}

// AuditionGain returns the effective playback gain (master * bus volume) for the active audition bus, or 0 when not auditioning.
func (e *Editor) AuditionGain() float32 {
	if e == nil || !e.audioMixer.auditioning || e.audioMixer.mixer == nil {
		return 0
	}
	return e.audioMixer.mixer.Master() * e.audioMixer.mixer.Volume(e.audioMixer.auditionBus)
}

// AudioMasterVolume returns the project's configured master volume in [0, 1], defaulting to 1 when unset.
func (e *Editor) AudioMasterVolume() float32 {
	if e == nil || e.Project == nil || e.Project.Audio == nil || e.Project.Audio.Master == nil {
		return 1
	}
	return *e.Project.Audio.Master
}

// SetAudioMasterVolume updates game.yaml's audio.master volume through EditProject (validated by project.Validate and undoable).
func (e *Editor) SetAudioMasterVolume(volume float32) error {
	return e.EditProject("Set Master Volume", func(f *project.File) {
		if f.Audio == nil {
			f.Audio = &project.Audio{}
		}
		f.Audio.Master = project.Float32(volume)
	})
}

// AudioBuses returns a cloned copy of the open project's audio buses in declaration order.
func (e *Editor) AudioBuses() []project.Bus {
	if e == nil || e.Project == nil || e.Project.Audio == nil || len(e.Project.Audio.Buses) == 0 {
		return nil
	}
	cloned := e.Project.Clone()
	return cloned.Audio.Buses
}

// AudioBusVolume returns bus's configured volume (defaulting to 1 when nil) and whether the bus exists.
func (e *Editor) AudioBusVolume(name string) (float32, bool) {
	idx := e.findAudioBusIndex(name)
	if idx < 0 {
		return 0, false
	}
	b := e.Project.Audio.Buses[idx]
	if b.Volume == nil {
		return 1, true
	}
	return *b.Volume, true
}

// AddAudioBus appends a new audio bus to game.yaml's audio.buses through EditProject and selects it.
func (e *Editor) AddAudioBus(name string) error {
	if err := e.EditProject("Add Audio Bus "+name, func(f *project.File) {
		if f.Audio == nil {
			f.Audio = &project.Audio{}
		}
		f.Audio.Buses = append(f.Audio.Buses, project.Bus{Name: name})
	}); err != nil {
		return err
	}
	e.audioMixer.selectedBus = name
	e.notify()
	return nil
}

// RenameAudioBus renames an existing audio bus through EditProject.
func (e *Editor) RenameAudioBus(oldName, newName string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findAudioBusIndex(oldName)
	if idx < 0 {
		return fmt.Errorf("editor: audio bus %q not found", oldName)
	}
	if oldName == newName {
		return nil
	}
	wasSelected := e.audioMixer.selectedBus == oldName
	wasAuditioning := e.audioMixer.auditioning && e.audioMixer.auditionBus == oldName
	if err := e.EditProject("Rename Audio Bus "+oldName+" to "+newName, func(f *project.File) {
		f.Audio.Buses[idx].Name = newName
	}); err != nil {
		return err
	}
	if wasSelected {
		e.audioMixer.selectedBus = newName
	}
	if wasAuditioning {
		e.audioMixer.auditionBus = newName
		if e.audioMixer.music != nil {
			e.audioMixer.music.Group = newName
		}
		e.syncAudioMixer()
	}
	e.notify()
	return nil
}

// DeleteAudioBus removes the named bus from game.yaml's audio.buses through EditProject.
func (e *Editor) DeleteAudioBus(name string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findAudioBusIndex(name)
	if idx < 0 {
		return fmt.Errorf("editor: audio bus %q not found", name)
	}
	return e.EditProject("Delete Audio Bus "+name, func(f *project.File) {
		f.Audio.Buses = slices.Delete(f.Audio.Buses, idx, idx+1)
		if len(f.Audio.Buses) == 0 {
			f.Audio.Buses = nil
			if f.Audio.Master == nil {
				f.Audio = nil
			}
		}
	})
}

// MoveAudioBus moves the bus at index from to index to through EditProject.
func (e *Editor) MoveAudioBus(from, to int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Audio == nil || from < 0 || from >= len(e.Project.Audio.Buses) || to < 0 || to >= len(e.Project.Audio.Buses) {
		return fmt.Errorf("editor: audio bus index out of range (%d -> %d)", from, to)
	}
	if from == to {
		return nil
	}
	return e.EditProject("Reorder Audio Buses", func(f *project.File) {
		b := f.Audio.Buses[from]
		f.Audio.Buses = slices.Delete(f.Audio.Buses, from, from+1)
		f.Audio.Buses = slices.Insert(f.Audio.Buses, to, b)
	})
}

// SetAudioBusVolume updates the named bus's volume in [0, 1] through EditProject.
func (e *Editor) SetAudioBusVolume(name string, volume float32) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findAudioBusIndex(name)
	if idx < 0 {
		return fmt.Errorf("editor: audio bus %q not found", name)
	}
	return e.EditProject("Set Bus "+name+" Volume", func(f *project.File) {
		f.Audio.Buses[idx].Volume = project.Float32(volume)
	})
}

// AddAudioBusEffect appends fx to the named bus's DSP effect chain through EditProject and returns the new effect index.
func (e *Editor) AddAudioBusEffect(bus string, fx project.Effect) (int, error) {
	if e.playing {
		return -1, ErrPlaying
	}
	if e.Project == nil {
		return -1, ErrNoProject
	}
	idx := e.findAudioBusIndex(bus)
	if idx < 0 {
		return -1, fmt.Errorf("editor: audio bus %q not found", bus)
	}
	cloned := cloneAudioEffect(fx)
	newIdx := len(e.Project.Audio.Buses[idx].Effects)
	if err := e.EditProject("Add Effect to "+bus, func(f *project.File) {
		f.Audio.Buses[idx].Effects = append(f.Audio.Buses[idx].Effects, cloneAudioEffect(cloned))
	}); err != nil {
		return -1, err
	}
	return newIdx, nil
}

// AddAudioBusEffectKind appends a default effect of kind to the named bus's effect chain through EditProject.
func (e *Editor) AddAudioBusEffectKind(bus string, kind AudioEffectKind) (int, error) {
	fx, err := DefaultAudioEffect(kind)
	if err != nil {
		return -1, err
	}
	return e.AddAudioBusEffect(bus, fx)
}

// SetAudioBusEffect replaces the effect at index on bus through EditProject.
func (e *Editor) SetAudioBusEffect(bus string, index int, fx project.Effect) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findAudioBusIndex(bus)
	if idx < 0 {
		return fmt.Errorf("editor: audio bus %q not found", bus)
	}
	if index < 0 || index >= len(e.Project.Audio.Buses[idx].Effects) {
		return fmt.Errorf("editor: audio bus %q effect index %d out of range", bus, index)
	}
	cloned := cloneAudioEffect(fx)
	return e.EditProject("Edit Effect on "+bus, func(f *project.File) {
		f.Audio.Buses[idx].Effects[index] = cloneAudioEffect(cloned)
	})
}

// RemoveAudioBusEffect deletes the effect at index from bus's effect chain through EditProject.
func (e *Editor) RemoveAudioBusEffect(bus string, index int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findAudioBusIndex(bus)
	if idx < 0 {
		return fmt.Errorf("editor: audio bus %q not found", bus)
	}
	if index < 0 || index >= len(e.Project.Audio.Buses[idx].Effects) {
		return fmt.Errorf("editor: audio bus %q effect index %d out of range", bus, index)
	}
	return e.EditProject("Remove Effect from "+bus, func(f *project.File) {
		f.Audio.Buses[idx].Effects = slices.Delete(f.Audio.Buses[idx].Effects, index, index+1)
		if len(f.Audio.Buses[idx].Effects) == 0 {
			f.Audio.Buses[idx].Effects = nil
		}
	})
}

// MoveAudioBusEffect moves an effect on bus from index from to index to through EditProject.
func (e *Editor) MoveAudioBusEffect(bus string, from, to int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findAudioBusIndex(bus)
	if idx < 0 {
		return fmt.Errorf("editor: audio bus %q not found", bus)
	}
	n := len(e.Project.Audio.Buses[idx].Effects)
	if from < 0 || from >= n || to < 0 || to >= n {
		return fmt.Errorf("editor: audio bus %q effect index out of range (%d -> %d)", bus, from, to)
	}
	if from == to {
		return nil
	}
	return e.EditProject("Reorder Effects on "+bus, func(f *project.File) {
		fx := f.Audio.Buses[idx].Effects[from]
		f.Audio.Buses[idx].Effects = slices.Delete(f.Audio.Buses[idx].Effects, from, from+1)
		f.Audio.Buses[idx].Effects = slices.Insert(f.Audio.Buses[idx].Effects, to, fx)
	})
}

// ProjectAudioFiles lists slash-separated relative paths of audio files (.wav, .ogg, .mp3, .m4a, .flac, .aac, .opus) inside the open project.
func (e *Editor) ProjectAudioFiles() ([]string, error) {
	files, err := e.ProjectFiles()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if f.Dir {
			continue
		}
		if isAudioFileExt(path.Ext(f.Path)) {
			out = append(out, f.Path)
		}
	}
	return out, nil
}

func isAudioFileExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".wav", ".ogg", ".mp3", ".m4a", ".flac", ".aac", ".opus":
		return true
	default:
		return false
	}
}

// AuditionAudioBus loads relPath from the open project and plays it through bus using the editor's sound.Mixer configured by project.File.ConfigureMixer. Changes to master volume, bus volume, or the bus's effect chain update live while auditioning.
func (e *Editor) AuditionAudioBus(bus, relPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.scope == nil {
		return errors.New("editor: audio audition requires a core.Scope (mount EditorView or call AttachScope)")
	}
	idx := e.findAudioBusIndex(bus)
	if idx < 0 {
		return fmt.Errorf("editor: audio bus %q not found", bus)
	}
	if relPath == "" || relPath == "." || !fs.ValidPath(relPath) || strings.Contains(relPath, `\`) {
		return fmt.Errorf("editor: invalid audio path %q", relPath)
	}
	fullPath := filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(relPath))
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("editor: read audio %q: %w", relPath, err)
	}
	e.ensureAudioMixer()
	processed, err := e.audioMixer.mixer.ProcessSource(bus, audio.Source{Data: raw})
	if err != nil {
		return fmt.Errorf("editor: process audio %q on bus %q: %w", relPath, bus, err)
	}
	if e.audioMixer.music == nil {
		e.audioMixer.music = e.audioMixer.mixer.Music(bus)
	}
	e.audioMixer.music.Group = bus
	e.audioMixer.music.Loop = e.audioMixer.loop
	// Play applies the bus's chain itself; passing raw keeps it from processing twice.
	e.audioMixer.music.Play(audio.Source{Data: raw}, 0)

	e.audioMixer.auditioning = true
	e.audioMixer.auditionBus = bus
	e.audioMixer.auditionFile = relPath
	e.audioMixer.selectedFile = relPath
	e.audioMixer.auditionRaw = raw
	e.audioMixer.auditionChain = audioBusEffectsKey(e.Project.Audio.Buses[idx].Effects)
	e.audioMixer.auditionSource = processed
	e.Status = e.T("audioMixer.statusAuditioning", l10n.Param("file", relPath), l10n.Param("bus", bus))
	e.notify()
	return nil
}

// StopAudition stops any active audio bus audition playback.
func (e *Editor) StopAudition() {
	if e == nil {
		return
	}
	was := e.audioMixer.auditioning
	e.stopAudioAudition()
	if was {
		e.Status = e.T("audioMixer.statusStopped")
		e.notify()
	}
}

func (e *Editor) stopAudioAudition() {
	if e == nil {
		return
	}
	if e.audioMixer.music != nil {
		e.audioMixer.music.Stop(0)
	}
	e.audioMixer.auditioning = false
	e.audioMixer.auditionBus = ""
	e.audioMixer.auditionFile = ""
	e.audioMixer.auditionRaw = nil
	e.audioMixer.auditionChain = ""
	e.audioMixer.auditionSource = audio.Source{}
}

func (e *Editor) cleanupAudioMixerScope(cx *core.Scope) {
	if e == nil {
		return
	}
	e.stopAudioAudition()
	if e.audioMixer.mixerScope == cx {
		e.audioMixer.mixer = nil
		e.audioMixer.mixerScope = nil
		e.audioMixer.music = nil
		e.audioMixer.knownBuses = nil
	}
}

func (e *Editor) ensureAudioMixer() {
	if e == nil || e.scope == nil {
		return
	}
	if e.audioMixer.mixer == nil || e.audioMixer.mixerScope != e.scope {
		var m *sound.Mixer
		e.scope.Run(func() {
			m = sound.NewMixer(e.scope, "editor.audioMixer")
		})
		e.audioMixer.mixer = m
		e.audioMixer.mixerScope = e.scope
		e.audioMixer.music = nil
		e.audioMixer.knownBuses = map[string]struct{}{}
	}
	e.configureAuditionMixer()
}

// configureAuditionMixer synchronizes e.audioMixer.mixer with e.Project via project.File.ConfigureMixer, resetting any buses or defaults that were removed or cleared by an edit or Undo.
func (e *Editor) configureAuditionMixer() {
	m := e.audioMixer.mixer
	if m == nil {
		return
	}
	if e.Project == nil || e.Project.Audio == nil || e.Project.Audio.Master == nil {
		m.SetDefaultMaster(1)
	}
	current := map[string]project.Bus{}
	if e.Project != nil && e.Project.Audio != nil {
		for _, b := range e.Project.Audio.Buses {
			current[b.Name] = b
		}
	}
	for old := range e.audioMixer.knownBuses {
		b, ok := current[old]
		if !ok {
			m.SetDefaultVolume(old, 1)
			m.SetGroupEffects(old)
			continue
		}
		if b.Volume == nil {
			m.SetDefaultVolume(old, 1)
		}
		if len(b.Effects) == 0 {
			m.SetGroupEffects(old)
		}
	}
	if e.Project != nil {
		e.Project.ConfigureMixer(m)
	}
	e.audioMixer.knownBuses = make(map[string]struct{}, len(current))
	for name := range current {
		e.audioMixer.knownBuses[name] = struct{}{}
	}
}

// syncAudioMixer keeps the selected bus, the audition sound.Mixer, and any live audition playback in step with e.Project after edits, undo/redo, or project open.
func (e *Editor) syncAudioMixer() {
	if e == nil {
		return
	}
	if e.Project == nil || e.Project.Audio == nil || len(e.Project.Audio.Buses) == 0 {
		e.audioMixer.selectedBus = ""
	} else if e.findAudioBusIndex(e.audioMixer.selectedBus) < 0 {
		e.audioMixer.selectedBus = e.Project.Audio.Buses[0].Name
	}
	e.configureAuditionMixer()
	if !e.audioMixer.auditioning {
		return
	}
	idx := e.findAudioBusIndex(e.audioMixer.auditionBus)
	if idx < 0 || e.audioMixer.mixer == nil || e.audioMixer.music == nil {
		e.stopAudioAudition()
		return
	}
	b := e.Project.Audio.Buses[idx]
	fp := audioBusEffectsKey(b.Effects)
	if fp == e.audioMixer.auditionChain {
		// Volume changes are already applied live by configureAuditionMixer through sound.Mixer.apply().
		return
	}
	processed, err := e.audioMixer.mixer.ProcessSource(b.Name, audio.Source{Data: e.audioMixer.auditionRaw})
	if err != nil {
		e.stopAudioAudition()
		e.Status = e.T("audioMixer.statusError", l10n.Param("error", err.Error()))
		return
	}
	e.audioMixer.auditionChain = fp
	e.audioMixer.auditionSource = processed
	e.audioMixer.music.Group = b.Name
	e.audioMixer.music.Loop = e.audioMixer.loop
	// Play applies the bus's chain itself; passing the raw bytes keeps it from processing twice.
	e.audioMixer.music.Play(audio.Source{Data: e.audioMixer.auditionRaw}, 0)
}

func (e *Editor) findAudioBusIndex(name string) int {
	if e == nil || e.Project == nil || e.Project.Audio == nil || name == "" {
		return -1
	}
	for i, b := range e.Project.Audio.Buses {
		if b.Name == name {
			return i
		}
	}
	return -1
}

func (e *Editor) nextDefaultBusName() string {
	for i := 1; ; i++ {
		name := fmt.Sprintf("bus_%d", i)
		if e.findAudioBusIndex(name) < 0 {
			return name
		}
	}
}

func audioBusEffectsKey(effects []project.Effect) string {
	if len(effects) == 0 {
		return ""
	}
	var b strings.Builder
	for _, fx := range effects {
		switch {
		case fx.LowPass != nil:
			fmt.Fprintf(&b, "lp:%g:%g|", fx.LowPass.CutoffHz, fx.LowPass.Q)
		case fx.HighPass != nil:
			fmt.Fprintf(&b, "hp:%g:%g|", fx.HighPass.CutoffHz, fx.HighPass.Q)
		case fx.Delay != nil:
			fmt.Fprintf(&b, "dl:%d:%g:%g|", fx.Delay.Time, fx.Delay.Feedback, fx.Delay.Wet)
		case fx.Reverb != nil:
			fmt.Fprintf(&b, "rv:%g:%g:%g|", fx.Reverb.RoomSize, fx.Reverb.Damping, fx.Reverb.Wet)
		case fx.Gain != nil:
			fmt.Fprintf(&b, "gn:%g|", fx.Gain.Factor)
		case fx.Bitcrush != nil:
			fmt.Fprintf(&b, "bc:%d:%d|", fx.Bitcrush.Bits, fx.Bitcrush.Downsample)
		}
	}
	return b.String()
}

// AudioMixerPanelView renders the Audio Bus Mixer panel below the viewport while a project is open and AudioMixerOpen reports true. It edits game.yaml's audio section through EditProject and auditions project audio files through sound.Mixer configured by project.File.ConfigureMixer.
func AudioMixerPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Project != nil && ed.AudioMixerOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			return audioMixerPanel(cx, ed, rev)
		},
	}
}

func formatGainDB(gain float32) string {
	g := float64(gain)
	if math.IsNaN(g) || math.IsInf(g, -1) || g <= 0.0001 {
		return "−∞ dB"
	}
	if math.IsInf(g, 1) {
		return "+∞ dB"
	}
	db := 20 * math.Log10(g)
	if math.Abs(db) < 0.05 {
		return "0.0 dB"
	}
	if db < 0 {
		return fmt.Sprintf("−%.1f dB", -db)
	}
	return fmt.Sprintf("+%.1f dB", db)
}

func audioMixerPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	pal := ed.Palette(cx)
	newBusName := core.NewSignal(cx, "")
	addEffectKind := core.NewSignal(cx, AudioEffectReverb)
	renameText := core.NewSignal(cx, ed.SelectedAudioBus())

	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil
	}

	selectedBus := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.SelectedAudioBus()
	})
	cx.Effect(func() {
		renameText.Set(selectedBus.Get())
	})

	// busListKey changes only when buses are added, removed, renamed, or reordered, so volume sliders remain mounted while dragged.
	busListKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project == nil || ed.Project.Audio == nil {
			return ""
		}
		var b strings.Builder
		for i, bus := range ed.Project.Audio.Buses {
			fmt.Fprintf(&b, "%d:%s|", i, bus.Name)
		}
		return b.String()
	})

	// effectListKey changes only when the selected bus or its effect sequence structure changes, so parameter NumberInputs stay mounted during edits.
	effectListKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		name := ed.SelectedAudioBus()
		idx := ed.findAudioBusIndex(name)
		if idx < 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString(name)
		b.WriteByte('|')
		for i, fx := range ed.Project.Audio.Buses[idx].Effects {
			fmt.Fprintf(&b, "%d:%s|", i, AudioEffectKindOf(fx))
		}
		return b.String()
	})

	// Cache project audio file listing keyed by project path and filesRev.
	audioFilesKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%s\x00%d", ed.ProjectPath, ed.filesRev)
	})
	audioFiles := core.NewMemoFunc(cx, func() []string {
		_ = audioFilesKey.Get()
		files, err := ed.ProjectAudioFiles()
		if err != nil {
			return nil
		}
		return files
	}, slices.Equal)

	label := func(text string) core.Node {
		return core.Text{Content: text, Style: core.Style{FontSize: 11, FontWeight: 600, LetterSpacing: 0.66, Color: pal.TextFaint}}
	}

	// Master output row in the Settings.dc.html row layout (label, help, value control, policy).
	masterRow := settingsRow(
		pal,
		ed,
		"mixer-master-row",
		ed.T("audioMixer.master"),
		ed.T("audioMixer.output"),
		ui.HStack{
			Gap: 8,
			Children: []core.Node{
				core.View{
					Style: core.Style{Width: core.Pt(160)},
					Children: []core.Node{
						ui.Slider{
							TestID:   "mixer-master-slider",
							Label:    ed.T("audioMixer.masterVolumeLabel"),
							Min:      0,
							Max:      1,
							Step:     0.01,
							Disabled: disabled,
							Value: func() float64 {
								_ = rev.Get()
								return float64(ed.AudioMasterVolume())
							},
							ValueText: func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) },
							OnChange: func(v float64) {
								ed.report(ed.SetAudioMasterVolume(float32(clampFiniteRange(v, 0, 1))))
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						Height:         core.Pt(28),
						MinWidth:       core.Pt(72),
						Padding:        core.Symmetric(0, 8),
						CornerRadius:   6,
						Background:     pal.Raised,
						BorderWidth:    1,
						BorderColor:    pal.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.Text{
							TestID: "mixer-master-value",
							ContentFunc: func() string {
								_ = rev.Get()
								return formatGainDB(ed.AudioMasterVolume())
							},
							Style: core.Style{FontSize: 11.5, FontFamily: core.FontMonospace, Color: pal.TextSecondary},
						},
					},
				},
			},
		},
		policyAgentMayChange,
		nil,
	)

	// Audition toolbar row styled with theme.Palette tokens.
	auditionBar := core.View{
		Style: core.Style{
			Direction:    core.Row,
			Wrap:         core.Wrap,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			Padding:      core.Symmetric(8, 12),
			CornerRadius: 12,
			Background:   pal.Panel,
			BorderWidth:  1,
			BorderColor:  pal.Line,
		},
		Children: []core.Node{
			label(ed.T("audioMixer.auditionHeading")),
			core.View{
				Style: core.Style{Width: core.Pt(220)},
				Children: []core.Node{
					ui.Select[string]{
						TestID:      "mixer-audition-file",
						Label:       ed.T("audioMixer.auditionFileLabel"),
						Placeholder: ed.T("audioMixer.auditionFilePlaceholder"),
						Searchable:  true,
						Disabled:    disabled,
						OptionsFunc: func() []ui.Option[string] {
							files := audioFiles.Get()
							opts := make([]ui.Option[string], len(files))
							for i, f := range files {
								opts[i] = ui.Option[string]{Value: f, Label: f}
							}
							return opts
						},
						Value: func() string {
							_ = rev.Get()
							return ed.SelectedAudioFile()
						},
						OnChange: func(v string) {
							ed.SelectAudioFile(v)
						},
					},
				},
			},
			ui.ToggleButton{
				TestID:   "mixer-audition-loop",
				Text:     ed.T("audioMixer.loop"),
				Disabled: disabled,
				On: func() bool {
					_ = rev.Get()
					return ed.AuditionLoop()
				},
				OnChange: func(on bool) { ed.SetAuditionLoop(on) },
			},
			ui.Button{
				TestID:  "mixer-audition-play",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Disabled: func() bool {
					_ = rev.Get()
					if ed.Playing() || ed.Project == nil {
						return true
					}
					if ed.Auditioning() {
						return false
					}
					return ed.SelectedAudioBus() == "" || ed.SelectedAudioFile() == ""
				},
				TextFunc: func() string {
					_ = rev.Get()
					if ed.Auditioning() {
						return ed.T("audioMixer.stop")
					}
					return ed.T("audioMixer.audition")
				},
				Color: func() core.Color { return pal.Accent.Fg },
				TextStyle: core.Style{
					FontSize:   12,
					FontWeight: 600,
					Color:      pal.Accent.OnFill,
				},
				Style: core.Style{
					Height:       core.Pt(28),
					CornerRadius: 8,
					Padding:      core.Symmetric(0, 12),
					FocusVisible: pal.FocusRing(2),
				},
				OnPress: func() {
					if ed.Auditioning() {
						ed.StopAudition()
						return
					}
					ed.report(ed.AuditionAudioBus(ed.SelectedAudioBus(), ed.SelectedAudioFile()))
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			settingsSpecButton(pal, "mixer-close", ed.T("audioMixer.close"), ed.T("audioMixer.closeLabel"), false, nil, func() {
				ed.SetAudioMixerOpen(false)
			}),
		},
	}

	busColumn := core.View{
		TestID: "mixer-bus-column",
		Style: core.Style{
			FlexGrow:    1,
			FlexBasis:   core.Pt(0),
			MinWidth:    core.Pt(340),
			Gap:         8,
			Padding:     core.Edges{Right: 16},
			Borders:     core.Edges{Right: 1},
			BorderColor: pal.Line,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{
							ui.Input{
								TestID:      "mixer-new-bus-name",
								Label:       ed.T("audioMixer.newBusNameLabel"),
								Placeholder: ed.T("audioMixer.newBusNamePlaceholder"),
								Disabled:    disabled,
								Value:       newBusName.Get,
								OnChange:    newBusName.Set,
							},
						},
					},
					settingsSpecButton(pal, "mixer-add-bus", ed.T("audioMixer.addBus"), ed.T("audioMixer.addBusLabel"), false, disabled, func() {
						name := strings.TrimSpace(newBusName.Get())
						if name == "" {
							name = ed.nextDefaultBusName()
						}
						if err := ed.AddAudioBus(name); err != nil {
							ed.report(err)
							return
						}
						newBusName.Set("")
					}),
				},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1},
				ContentStyle: core.Style{Gap: 4},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if busListKey.Get() == "" {
							return core.Text{
								TestID:  "mixer-empty-buses",
								Content: ed.T("audioMixer.emptyBuses"),
								Style:   core.Style{FontSize: 12, Color: pal.TextFaint, Padding: core.Symmetric(12, 0)},
							}
						}
						buses := ed.Project.Audio.Buses
						rows := make([]core.Node, 0, len(buses))
						for i, b := range buses {
							rows = append(rows, mixerBusRow(cx, ed, rev, pal, i, b.Name, len(buses), selectedBus, disabled))
						}
						return core.View{Style: core.Style{Gap: 4}, Children: rows}
					}),
				},
			},
		},
	}

	effectOpts := make([]ui.Option[AudioEffectKind], len(AudioEffectKinds))
	for i, k := range AudioEffectKinds {
		effectOpts[i] = ui.Option[AudioEffectKind]{Value: k, Label: ed.localizedAudioEffectKind(k)}
	}

	detailColumn := core.View{
		TestID: "mixer-detail-column",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Gap:       8,
			Padding:   core.Edges{Left: 16},
		},
		Children: []core.Node{
			core.Show{
				When: func() bool { return selectedBus.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{FlexGrow: 1, Gap: 8},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Direction:  core.Row,
									Wrap:       core.Wrap,
									AlignItems: core.AlignCenter,
									Gap:        8,
								},
								Children: []core.Node{
									label(ed.T("audioMixer.busHeading")),
									core.View{
										Style: core.Style{Width: core.Pt(140)},
										Children: []core.Node{
											ui.Input{
												TestID:   "mixer-bus-rename-input",
												Label:    ed.T("audioMixer.renameBusLabel"),
												Disabled: disabled,
												Value:    renameText.Get,
												OnChange: renameText.Set,
											},
										},
									},
									settingsSpecButton(pal, "mixer-bus-rename", ed.T("audioMixer.rename"), ed.T("audioMixer.renameSelectedBusLabel"), false, func() bool {
										if disabled() {
											return true
										}
										cur := selectedBus.Get()
										next := strings.TrimSpace(renameText.Get())
										return cur == "" || next == "" || next == cur
									}, func() {
										ed.report(ed.RenameAudioBus(selectedBus.Get(), strings.TrimSpace(renameText.Get())))
									}),
									core.View{Style: core.Style{FlexGrow: 1}},
									core.View{
										Style: core.Style{Width: core.Pt(136)},
										Children: []core.Node{
											ui.Select[AudioEffectKind]{
												TestID:   "mixer-add-effect-kind",
												Label:    ed.T("audioMixer.effectTypeLabel"),
												Disabled: disabled,
												Options:  effectOpts,
												Value:    addEffectKind.Get,
												OnChange: addEffectKind.Set,
												Style:    core.Style{Width: core.Pt(136)},
											},
										},
									},
									settingsSpecButton(pal, "mixer-add-effect", ed.T("audioMixer.addEffect"), ed.T("audioMixer.addEffectLabel"), false, disabled, func() {
										_, err := ed.AddAudioBusEffectKind(selectedBus.Get(), addEffectKind.Get())
										ed.report(err)
									}),
								},
							},
							ui.ScrollView{
								Style:        core.Style{FlexGrow: 1},
								ContentStyle: core.Style{Gap: 8},
								Children: []core.Node{
									core.Dynamic(func(cx *core.Scope) core.Node {
										if effectListKey.Get() == "" {
											return nil
										}
										busName := selectedBus.Get()
										idx := ed.findAudioBusIndex(busName)
										if idx < 0 {
											return nil
										}
										effects := ed.Project.Audio.Buses[idx].Effects
										if len(effects) == 0 {
											return core.Text{
												TestID:  "mixer-empty-effects",
												Content: ed.T("audioMixer.emptyEffects"),
												Style:   core.Style{FontSize: 11.5, Color: pal.TextFaint, Padding: core.Symmetric(8, 0)},
											}
										}
										rows := make([]core.Node, 0, len(effects))
										for ei, fx := range effects {
											rows = append(rows, mixerEffectRow(cx, ed, rev, pal, busName, ei, len(effects), AudioEffectKindOf(fx), disabled))
										}
										return core.View{Style: core.Style{Gap: 8}, Children: rows}
									}),
								},
							},
						},
					}
				},
				Else: func(cx *core.Scope) core.Node {
					return core.Text{
						Content: ed.T("audioMixer.selectOrAddBus"),
						Style:   core.Style{FontSize: 12, Color: pal.TextFaint},
					}
				},
			},
		},
	}

	return core.View{
		TestID: "audio-mixer-panel",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Gap:       12,
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			masterRow,
			auditionBar,
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{busColumn, detailColumn},
			},
		},
	}
}

func mixerBusRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], pal theme.Palette, idx int, busName string, total int, selectedBus *core.Memo[string], disabled func() bool) core.Node {
	accentEdge := ui.Alpha(pal.Accent.Fg, 0.45)
	activeNav := ui.Alpha(pal.Accent.Fg, 0.16)
	return core.View{
		TestID: fmt.Sprintf("mixer-bus-%d", idx),
		StyleFunc: func() core.Style {
			bg := pal.Panel
			border := pal.Line
			if selectedBus.Get() == busName {
				bg = pal.Raised
				border = accentEdge
			}
			return core.Style{
				Padding:      core.Symmetric(8, 12),
				Gap:          8,
				CornerRadius: 8,
				Background:   bg,
				BorderWidth:  1,
				BorderColor:  border,
			}
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					ui.Button{
						TestID:  fmt.Sprintf("mixer-bus-%d-select", idx),
						Text:    busName,
						Label:   ed.T("audioMixer.selectBusLabel", l10n.Param("bus", busName)),
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						Flex:    1,
						Color: func() core.Color {
							if selectedBus.Get() == busName {
								return activeNav
							}
							return pal.Panel
						},
						TextStyle: core.Style{
							FontSize:   12.5,
							FontWeight: 600,
							Color:      pal.TextPrimary,
							FlexGrow:   1,
							TextAlign:  core.TextAlignLeft,
						},
						Style: core.Style{
							Height:       core.Pt(28),
							CornerRadius: 6,
							FocusVisible: pal.FocusRing(2),
						},
						OnPress: func() {
							ed.SelectAudioBus(busName)
						},
					},
					core.Text{
						ContentFunc: func() string {
							_ = rev.Get()
							bi := ed.findAudioBusIndex(busName)
							if bi >= 0 && len(ed.Project.Audio.Buses[bi].Effects) > 0 {
								return ed.T("audioMixer.intoMasterFx", l10n.Param("count", len(ed.Project.Audio.Buses[bi].Effects)))
							}
							return ed.T("audioMixer.intoMaster")
						},
						Style: core.Style{FontSize: 11, Color: pal.TextFaint},
					},
					settingsSpecButton(pal, fmt.Sprintf("mixer-bus-%d-up", idx), ed.T("audioMixer.up"), ed.T("audioMixer.moveBusUpLabel", l10n.Param("bus", busName)), false, func() bool {
						return disabled() || idx == 0
					}, func() { ed.report(ed.MoveAudioBus(idx, idx-1)) }),
					settingsSpecButton(pal, fmt.Sprintf("mixer-bus-%d-down", idx), ed.T("audioMixer.down"), ed.T("audioMixer.moveBusDownLabel", l10n.Param("bus", busName)), false, func() bool {
						return disabled() || idx >= total-1
					}, func() { ed.report(ed.MoveAudioBus(idx, idx+1)) }),
					settingsSpecButton(pal, fmt.Sprintf("mixer-bus-%d-delete", idx), ed.T("audioMixer.del"), ed.T("audioMixer.deleteBusLabel", l10n.Param("bus", busName)), false, disabled, func() {
						ed.report(ed.DeleteAudioBus(busName))
					}),
				},
			},
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{
							ui.Slider{
								TestID:   fmt.Sprintf("mixer-bus-%d-volume", idx),
								Label:    ed.T("audioMixer.busVolumeLabel", l10n.Param("bus", busName)),
								Min:      0,
								Max:      1,
								Step:     0.01,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									v, _ := ed.AudioBusVolume(busName)
									return float64(v)
								},
								ValueText: func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) },
								OnChange: func(v float64) {
									ed.report(ed.SetAudioBusVolume(busName, float32(clampFiniteRange(v, 0, 1))))
								},
							},
						},
					},
					core.View{
						Style: core.Style{
							Height:         core.Pt(24),
							MinWidth:       core.Pt(64),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							Background:     pal.Bg,
							BorderWidth:    1,
							BorderColor:    pal.LineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{
								TestID: fmt.Sprintf("mixer-bus-%d-volume-value", idx),
								ContentFunc: func() string {
									_ = rev.Get()
									v, _ := ed.AudioBusVolume(busName)
									return formatGainDB(v)
								},
								Style: core.Style{FontSize: 11, FontFamily: core.FontMonospace, Color: pal.TextSecondary},
							},
						},
					},
					settingsPolicyBadge(pal, ed, policyAgentMayChange),
				},
			},
		},
	}
}

func mixerEffectRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], pal theme.Palette, busName string, ei, total int, kind AudioEffectKind, disabled func() bool) core.Node {
	readEffect := func() (project.Effect, bool) {
		_ = rev.Get()
		bi := ed.findAudioBusIndex(busName)
		if bi < 0 || ei >= len(ed.Project.Audio.Buses[bi].Effects) {
			return project.Effect{}, false
		}
		return ed.Project.Audio.Buses[bi].Effects[ei], true
	}
	updateEffect := func(mutate func(fx *project.Effect)) {
		cur, ok := readEffect()
		if !ok {
			return
		}
		next := cloneAudioEffect(cur)
		mutate(&next)
		ed.report(ed.SetAudioBusEffect(busName, ei, ClampAudioEffect(next)))
	}

	paramField := func(testID, lbl, caption string, minV, maxV, step float64, read func(project.Effect) float64, write func(*project.Effect, float64)) core.Node {
		return ui.HStack{
			Gap: 4,
			Children: []core.Node{
				core.Text{Content: caption, Style: core.Style{FontSize: 11, Color: pal.TextFaint}},
				core.View{
					Style: core.Style{Width: core.Pt(86)},
					Children: []core.Node{
						ui.NumberInput{
							TestID:   testID,
							Label:    lbl,
							Min:      minV,
							Max:      maxV,
							Step:     step,
							Disabled: disabled,
							Value: func() float64 {
								fx, ok := readEffect()
								if !ok {
									return minV
								}
								return read(fx)
							},
							OnChange: func(v float64) {
								updateEffect(func(fx *project.Effect) {
									write(fx, v)
								})
							},
						},
					},
				},
			},
		}
	}

	var params []core.Node
	switch kind {
	case AudioEffectLowPass:
		params = []core.Node{
			paramField(fmt.Sprintf("mixer-effect-%d-cutoff", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.cutoffHz"))), ed.T("audioMixer.param.cutoffHz"), 1, 48000, 10,
				func(fx project.Effect) float64 {
					if fx.LowPass == nil {
						return 1200
					}
					return fx.LowPass.CutoffHz
				},
				func(fx *project.Effect, v float64) {
					if fx.LowPass != nil {
						fx.LowPass.CutoffHz = v
					}
				},
			),
			paramField(fmt.Sprintf("mixer-effect-%d-q", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.q"))), ed.T("audioMixer.param.q"), 0, 100, 0.05,
				func(fx project.Effect) float64 {
					if fx.LowPass == nil {
						return 0
					}
					return fx.LowPass.Q
				},
				func(fx *project.Effect, v float64) {
					if fx.LowPass != nil {
						fx.LowPass.Q = v
					}
				},
			),
		}
	case AudioEffectHighPass:
		params = []core.Node{
			paramField(fmt.Sprintf("mixer-effect-%d-cutoff", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.cutoffHz"))), ed.T("audioMixer.param.cutoffHz"), 1, 48000, 10,
				func(fx project.Effect) float64 {
					if fx.HighPass == nil {
						return 400
					}
					return fx.HighPass.CutoffHz
				},
				func(fx *project.Effect, v float64) {
					if fx.HighPass != nil {
						fx.HighPass.CutoffHz = v
					}
				},
			),
			paramField(fmt.Sprintf("mixer-effect-%d-q", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.q"))), ed.T("audioMixer.param.q"), 0, 100, 0.05,
				func(fx project.Effect) float64 {
					if fx.HighPass == nil {
						return 0
					}
					return fx.HighPass.Q
				},
				func(fx *project.Effect, v float64) {
					if fx.HighPass != nil {
						fx.HighPass.Q = v
					}
				},
			),
		}
	case AudioEffectDelay:
		params = []core.Node{
			paramField(fmt.Sprintf("mixer-effect-%d-time", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.timeMs"))), ed.T("audioMixer.param.timeMs"), 1, 10000, 10,
				func(fx project.Effect) float64 {
					if fx.Delay == nil {
						return 250
					}
					return float64(fx.Delay.Time) / float64(time.Millisecond)
				},
				func(fx *project.Effect, v float64) {
					if fx.Delay != nil {
						fx.Delay.Time = time.Duration(math.Round(v)) * time.Millisecond
					}
				},
			),
			paramField(fmt.Sprintf("mixer-effect-%d-feedback", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.feedback"))), ed.T("audioMixer.param.feedback"), 0, 0.99, 0.01,
				func(fx project.Effect) float64 {
					if fx.Delay == nil {
						return 0
					}
					return fx.Delay.Feedback
				},
				func(fx *project.Effect, v float64) {
					if fx.Delay != nil {
						fx.Delay.Feedback = v
					}
				},
			),
			paramField(fmt.Sprintf("mixer-effect-%d-wet", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.wet"))), ed.T("audioMixer.param.wet"), 0, 1, 0.01,
				func(fx project.Effect) float64 {
					if fx.Delay == nil {
						return 0
					}
					return fx.Delay.Wet
				},
				func(fx *project.Effect, v float64) {
					if fx.Delay != nil {
						fx.Delay.Wet = v
					}
				},
			),
		}
	case AudioEffectReverb:
		params = []core.Node{
			paramField(fmt.Sprintf("mixer-effect-%d-room", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.roomSize"))), ed.T("audioMixer.param.room"), 0, 1, 0.01,
				func(fx project.Effect) float64 {
					if fx.Reverb == nil {
						return 0
					}
					return fx.Reverb.RoomSize
				},
				func(fx *project.Effect, v float64) {
					if fx.Reverb != nil {
						fx.Reverb.RoomSize = v
					}
				},
			),
			paramField(fmt.Sprintf("mixer-effect-%d-damping", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.damping"))), ed.T("audioMixer.param.damping"), 0, 1, 0.01,
				func(fx project.Effect) float64 {
					if fx.Reverb == nil {
						return 0
					}
					return fx.Reverb.Damping
				},
				func(fx *project.Effect, v float64) {
					if fx.Reverb != nil {
						fx.Reverb.Damping = v
					}
				},
			),
			paramField(fmt.Sprintf("mixer-effect-%d-wet", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.wet"))), ed.T("audioMixer.param.wet"), 0, 1, 0.01,
				func(fx project.Effect) float64 {
					if fx.Reverb == nil {
						return 0
					}
					return fx.Reverb.Wet
				},
				func(fx *project.Effect, v float64) {
					if fx.Reverb != nil {
						fx.Reverb.Wet = v
					}
				},
			),
		}
	case AudioEffectGain:
		params = []core.Node{
			paramField(fmt.Sprintf("mixer-effect-%d-factor", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.factor"))), ed.T("audioMixer.param.factor"), 0, 100, 0.05,
				func(fx project.Effect) float64 {
					if fx.Gain == nil {
						return 1
					}
					return fx.Gain.Factor
				},
				func(fx *project.Effect, v float64) {
					if fx.Gain != nil {
						fx.Gain.Factor = v
					}
				},
			),
		}
	case AudioEffectBitcrush:
		params = []core.Node{
			paramField(fmt.Sprintf("mixer-effect-%d-bits", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.bits"))), ed.T("audioMixer.param.bits"), 1, 15, 1,
				func(fx project.Effect) float64 {
					if fx.Bitcrush == nil {
						return 8
					}
					return float64(fx.Bitcrush.Bits)
				},
				func(fx *project.Effect, v float64) {
					if fx.Bitcrush != nil {
						fx.Bitcrush.Bits = int(math.Round(v))
					}
				},
			),
			paramField(fmt.Sprintf("mixer-effect-%d-downsample", ei), ed.T("audioMixer.effectParamLabel", l10n.Param("index", ei), l10n.Param("param", ed.T("audioMixer.param.downsample"))), ed.T("audioMixer.param.downsample"), 0, 1024, 1,
				func(fx project.Effect) float64 {
					if fx.Bitcrush == nil {
						return 0
					}
					return float64(fx.Bitcrush.Downsample)
				},
				func(fx *project.Effect, v float64) {
					if fx.Bitcrush != nil {
						fx.Bitcrush.Downsample = int(math.Round(v))
					}
				},
			),
		}
	}

	return core.View{
		TestID: fmt.Sprintf("mixer-effect-%d", ei),
		Style: core.Style{
			Padding:      core.Symmetric(8, 12),
			Gap:          8,
			CornerRadius: 8,
			Background:   pal.Panel,
			BorderWidth:  1,
			BorderColor:  pal.Line,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.Text{
						TestID:  fmt.Sprintf("mixer-effect-%d-title", ei),
						Content: fmt.Sprintf("%d. %s", ei+1, ed.localizedAudioEffectKind(kind)),
						Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: pal.TextPrimary, FlexGrow: 1},
					},
					settingsSpecButton(pal, fmt.Sprintf("mixer-effect-%d-up", ei), ed.T("audioMixer.up"), ed.T("audioMixer.moveEffectUpLabel", l10n.Param("index", ei)), false, func() bool {
						return disabled() || ei == 0
					}, func() { ed.report(ed.MoveAudioBusEffect(busName, ei, ei-1)) }),
					settingsSpecButton(pal, fmt.Sprintf("mixer-effect-%d-down", ei), ed.T("audioMixer.down"), ed.T("audioMixer.moveEffectDownLabel", l10n.Param("index", ei)), false, func() bool {
						return disabled() || ei >= total-1
					}, func() { ed.report(ed.MoveAudioBusEffect(busName, ei, ei+1)) }),
					settingsSpecButton(pal, fmt.Sprintf("mixer-effect-%d-remove", ei), ed.T("audioMixer.remove"), ed.T("audioMixer.removeEffectLabel", l10n.Param("index", ei)), false, disabled, func() {
						ed.report(ed.RemoveAudioBusEffect(busName, ei))
					}),
				},
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					Wrap:       core.Wrap,
					AlignItems: core.AlignCenter,
					Gap:        12,
				},
				Children: params,
			},
		},
	}
}
