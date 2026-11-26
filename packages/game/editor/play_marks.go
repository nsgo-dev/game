package editor

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	gamemcp "github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

const rollingReplayWindowSeconds = 60

// PlayInspectorTab selects between the Inspector and Agent tabs in the right panel during Play mode.
type PlayInspectorTab int

const (
	// PlayTabInspector shows the normal or live remote entity inspector.
	PlayTabInspector PlayInspectorTab = iota
	// PlayTabAgent shows the Play mode Agent tab with MARKS · N and agent replay stepping progress.
	PlayTabAgent
)

// PlayMark records one moment marked during Play mode (⌥F / AddPlayMark) with its saved input recording.
type PlayMark struct {
	ID         int       `json:"id"`
	Frame      int       `json:"frame"`
	Seconds    float64   `json:"seconds"`
	TimeLabel  string    `json:"timeLabel"`
	ReplayPath string    `json:"replayPath"`
	AbsPath    string    `json:"absPath,omitempty"`
	Scene      string    `json:"scene,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	Sent       bool      `json:"sent,omitempty"`
}

// PlayMarksThread bundles all unsent marks from a Play session into one thread sent to the agent.
type PlayMarksThread struct {
	Scene     string     `json:"scene,omitempty"`
	Marks     []PlayMark `json:"marks"`
	Summary   string     `json:"summary"`
	CreatedAt time.Time  `json:"createdAt"`
}

// PlayLiveEntityState captures a live scene entity's runtime transform and components at a frozen frame.
type PlayLiveEntityState struct {
	ID         string         `json:"id"`
	Name       string         `json:"name,omitempty"`
	Layer      string         `json:"layer,omitempty"`
	X          float32        `json:"x"`
	Y          float32        `json:"y"`
	Components map[string]any `json:"components,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// AskFrozenFrameRequest carries everything attached when the author asks the agent about a frozen Play frame (⌘L).
type AskFrozenFrameRequest struct {
	Frame        int                   `json:"frame"`
	Seconds      float64               `json:"seconds"`
	Scene        string                `json:"scene,omitempty"`
	Prompt       string                `json:"prompt"`
	FramePNG     []byte                `json:"-"`
	FrameWidth   int                   `json:"frameWidth"`
	FrameHeight  int                   `json:"frameHeight"`
	ReplayPath   string                `json:"replayPath,omitempty"`
	ReplayAbs    string                `json:"replayAbs,omitempty"`
	ReplayFrames int                   `json:"replayFrames"`
	SelectedID   string                `json:"selectedId,omitempty"`
	LiveEntities []PlayLiveEntityState `json:"liveEntities,omitempty"`
	LiveChanges  []RemoteLiveChange    `json:"liveChanges,omitempty"`
	CreatedAt    time.Time             `json:"createdAt"`
}

// PlayAgentStep is one progress row displayed in the Play mode Agent tab while the agent inspects a replay.
type PlayAgentStep struct {
	Text   string `json:"text"`
	Status string `json:"status"` // "done", "active", or "pending"
}

type playMarksState struct {
	marks          []PlayMark
	nextMarkID     int
	lastThread     *PlayMarksThread
	lastReplayPath string
	lastReplayAbs  string
	lastPanicRec   *replay.Recording

	tab          PlayInspectorTab
	askOpen      bool
	askFrame     int
	askSeconds   float64
	askDraft     string
	askFramePNG  []byte
	askFrameW    int
	askFrameH    int
	activeAsk    *AskFrozenFrameRequest
	activeReplay *replay.Recording
	sentQuestion string
	sentMeta     string
	agentPill    string
	agentSteps   []PlayAgentStep
	agentActive  bool
}

// playRollingRecorder maintains a sliding 60-second window (maxFrames = 60 * rate) of input changes
// that encodes cleanly as replay.Recording version 2 YAML.
type playRollingRecorder struct {
	src        replay.ActionSource
	seed       uint64
	rate       int
	maxFrames  int
	actions    []string
	axes       []string
	totalSteps int
	// windowStart is the first step index retained in the sliding window (0 while totalSteps <= maxFrames).
	windowStart int
	// curAction and curAxis hold the latest sampled input state (after the most recent Step).
	curAction []bool
	curAxis   []float64
	// baseAction and baseAxis hold the input state entering windowStart.
	baseAction []bool
	baseAxis   []float64
	// events[eventHead:] holds delta events in absolute step coordinates [windowStart, totalSteps).
	events    []replay.StepEvent
	eventHead int
}

func newPlayRollingRecorder(src replay.ActionSource, seed uint64, rate int, actions, axes []string) *playRollingRecorder {
	if rate <= 0 {
		rate = 60
	}
	act := uniquePlayNames(actions)
	ax := uniquePlayNames(axes)
	return &playRollingRecorder{
		src:        src,
		seed:       seed,
		rate:       rate,
		maxFrames:  rollingReplayWindowSeconds * rate,
		actions:    act,
		axes:       ax,
		curAction:  make([]bool, len(act)),
		curAxis:    make([]float64, len(ax)),
		baseAction: make([]bool, len(act)),
		baseAxis:   make([]float64, len(ax)),
	}
}

func uniquePlayNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *playRollingRecorder) Step() {
	if r == nil {
		return
	}
	if r.src != nil {
		r.src.Step()
	}
	stepIdx := r.totalSteps
	r.totalSteps++

	var press, release []int
	for i, name := range r.actions {
		now := r.src != nil && r.src.Pressed(name)
		if now != r.curAction[i] {
			r.curAction[i] = now
			if now {
				press = append(press, i)
			} else {
				release = append(release, i)
			}
		}
	}

	var axisChanges []replay.AxisChange
	for i, name := range r.axes {
		var now float64
		if r.src != nil {
			now = r.src.Axis(name)
		}
		if now == 0 || math.IsNaN(now) || math.IsInf(now, 0) {
			now = 0
		}
		if now != r.curAxis[i] {
			r.curAxis[i] = now
			axisChanges = append(axisChanges, replay.AxisChange{Index: i, Value: now})
		}
	}

	if len(press) > 0 || len(release) > 0 || len(axisChanges) > 0 {
		r.events = append(r.events, replay.StepEvent{
			Step:    stepIdx,
			Press:   press,
			Release: release,
			Axes:    axisChanges,
		})
	}

	if r.maxFrames > 0 && r.totalSteps-r.windowStart > r.maxFrames {
		newStart := r.totalSteps - r.maxFrames
		r.trimBefore(newStart)
	}
}

func (r *playRollingRecorder) trimBefore(newStart int) {
	if newStart <= r.windowStart {
		return
	}
	for r.eventHead < len(r.events) && r.events[r.eventHead].Step < newStart {
		ev := r.events[r.eventHead]
		for _, idx := range ev.Press {
			if idx >= 0 && idx < len(r.baseAction) {
				r.baseAction[idx] = true
			}
		}
		for _, idx := range ev.Release {
			if idx >= 0 && idx < len(r.baseAction) {
				r.baseAction[idx] = false
			}
		}
		for _, ch := range ev.Axes {
			if ch.Index >= 0 && ch.Index < len(r.baseAxis) {
				r.baseAxis[ch.Index] = ch.Value
			}
		}
		r.events[r.eventHead] = replay.StepEvent{}
		r.eventHead++
	}
	if r.eventHead == len(r.events) {
		r.events = r.events[:0]
		r.eventHead = 0
	} else if r.eventHead >= 256 && r.eventHead*2 >= len(r.events) {
		rem := len(r.events) - r.eventHead
		copy(r.events[:rem], r.events[r.eventHead:])
		clear(r.events[rem:])
		r.events = r.events[:rem]
		r.eventHead = 0
	}
	r.windowStart = newStart
}

func (r *playRollingRecorder) WindowStart() int {
	if r == nil {
		return 0
	}
	return r.windowStart
}

func (r *playRollingRecorder) Pressed(name string) bool {
	if r == nil || r.src == nil {
		return false
	}
	return r.src.Pressed(name)
}

func (r *playRollingRecorder) JustPressed(name string) bool {
	if r == nil || r.src == nil {
		return false
	}
	return r.src.JustPressed(name)
}

func (r *playRollingRecorder) JustReleased(name string) bool {
	if r == nil || r.src == nil {
		return false
	}
	return r.src.JustReleased(name)
}

func (r *playRollingRecorder) Axis(name string) float64 {
	if r == nil || r.src == nil {
		return 0
	}
	return r.src.Axis(name)
}

func (r *playRollingRecorder) FrameCount() int {
	if r == nil {
		return 0
	}
	return r.totalSteps - r.windowStart
}

func (r *playRollingRecorder) MaxFrames() int {
	if r == nil {
		return rollingReplayWindowSeconds * 60
	}
	return r.maxFrames
}

// Recording builds a valid replay.Recording (version 2) for the current rolling window.
// When totalSteps <= maxFrames (within 60s), windowStart is 0 and every event keeps its exact Play frame index.
// When totalSteps > maxFrames, steps are rebased to [0, maxFrames) and any held actions or non-zero axes
// entering the window are emitted on step 0 so playback starts with the exact window-entry input state.
func (r *playRollingRecorder) Recording() *replay.Recording {
	if r == nil {
		return &replay.Recording{
			Version: replay.FormatVersion,
			Rate:    60,
		}
	}
	frames := r.totalSteps - r.windowStart
	rec := &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       r.seed,
		Rate:       r.rate,
		Actions:    slices.Clone(r.actions),
		Axes:       slices.Clone(r.axes),
		TotalSteps: frames,
	}
	if frames == 0 {
		return rec
	}

	// Build initial entering-state event at step 0 when the window has slid past frame 0.
	var events []replay.StepEvent
	if r.windowStart > 0 {
		var press0 []int
		for i, held := range r.baseAction {
			if held {
				press0 = append(press0, i)
			}
		}
		var axes0 []replay.AxisChange
		for i, v := range r.baseAxis {
			if v != 0 {
				axes0 = append(axes0, replay.AxisChange{Index: i, Value: v})
			}
		}
		if len(press0) > 0 || len(axes0) > 0 {
			events = append(events, replay.StepEvent{
				Step:  0,
				Press: press0,
				Axes:  axes0,
			})
		}
	}

	for _, ev := range r.events[r.eventHead:] {
		relStep := ev.Step - r.windowStart
		if relStep < 0 || relStep >= frames {
			continue
		}
		if relStep == 0 && len(events) == 1 && events[0].Step == 0 {
			events[0] = mergeStep0Events(events[0], ev, len(r.actions), len(r.axes))
			if len(events[0].Press) == 0 && len(events[0].Release) == 0 && len(events[0].Axes) == 0 {
				events = events[:0]
			}
			continue
		}
		events = append(events, replay.StepEvent{
			Step:    relStep,
			Press:   slices.Clone(ev.Press),
			Release: slices.Clone(ev.Release),
			Axes:    slices.Clone(ev.Axes),
		})
	}
	if len(events) > 0 {
		rec.Events = events
	}
	return rec
}

func mergeStep0Events(base, delta replay.StepEvent, nActions, nAxes int) replay.StepEvent {
	state := make([]bool, nActions)
	for _, idx := range base.Press {
		if idx >= 0 && idx < nActions {
			state[idx] = true
		}
	}
	for _, idx := range delta.Press {
		if idx >= 0 && idx < nActions {
			state[idx] = true
		}
	}
	for _, idx := range delta.Release {
		if idx >= 0 && idx < nActions {
			state[idx] = false
		}
	}
	var press []int
	for i, on := range state {
		if on {
			press = append(press, i)
		}
	}

	axisVals := make([]float64, nAxes)
	axisSet := make([]bool, nAxes)
	for _, ch := range base.Axes {
		if ch.Index >= 0 && ch.Index < nAxes {
			axisVals[ch.Index] = ch.Value
			axisSet[ch.Index] = true
		}
	}
	for _, ch := range delta.Axes {
		if ch.Index >= 0 && ch.Index < nAxes {
			axisVals[ch.Index] = ch.Value
			axisSet[ch.Index] = true
		}
	}
	var axes []replay.AxisChange
	for i, ok := range axisSet {
		if ok && axisVals[i] != 0 {
			axes = append(axes, replay.AxisChange{Index: i, Value: axisVals[i]})
		}
	}
	return replay.StepEvent{
		Step:  0,
		Press: press,
		Axes:  axes,
	}
}

// PlayRollingWindowStart returns the first absolute Play frame retained in the rolling 60-second window.
func (e *Editor) PlayRollingWindowStart() int {
	if e == nil || e.playRolling == nil {
		return 0
	}
	return e.playRolling.WindowStart()
}

// PlayRollingFrameCount returns how many input frames are currently kept in Play mode's rolling 60-second buffer.
func (e *Editor) PlayRollingFrameCount() int {
	if e == nil || e.playRolling == nil {
		return 0
	}
	return e.playRolling.FrameCount()
}

// PlayRollingMaxFrames returns the maximum rolling replay buffer capacity in frames (60 * PlayRateHz, 3,600 at 60 Hz).
func (e *Editor) PlayRollingMaxFrames() int {
	if e == nil {
		return rollingReplayWindowSeconds * 60
	}
	if e.playRolling != nil {
		return e.playRolling.MaxFrames()
	}
	return rollingReplayWindowSeconds * e.PlayRateHz()
}

// PlayRollingSummary returns the Play-mode replay sidebar summary text, e.g. "Last 60 s kept · 3 600 frames".
func (e *Editor) PlayRollingSummary() string {
	n := e.PlayRollingFrameCount()
	unit := "frames"
	if n == 1 {
		unit = "frame"
	}
	return fmt.Sprintf("Last 60 s kept · %s %s", formatPlayFrameNumber(n), unit)
}

// PlayLastReplayPath returns the project-relative path of the most recently saved Play replay file, or empty.
func (e *Editor) PlayLastReplayPath() string {
	if e == nil {
		return ""
	}
	return e.playMarks.lastReplayPath
}

// PlayRollingRecording returns a snapshot copy of Play mode's current rolling replay.Recording (version 2).
func (e *Editor) PlayRollingRecording() *replay.Recording {
	if e == nil {
		return nil
	}
	if e.playRolling != nil {
		return e.playRolling.Recording()
	}
	return &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       e.PlaySeed,
		Rate:       e.PlayRateHz(),
		Actions:    e.playWatchedActions(),
		Axes:       uniquePlayNames(e.PlayInputAxes),
		TotalSteps: e.playSteps,
	}
}

func (e *Editor) playWatchedActions() []string {
	if e == nil {
		return nil
	}
	if len(e.PlayInputActions) > 0 {
		return uniquePlayNames(e.PlayInputActions)
	}
	if e.Project != nil && e.Project.Input != nil && len(e.Project.Input.Actions) > 0 {
		names := make([]string, 0, len(e.Project.Input.Actions))
		for _, act := range e.Project.Input.Actions {
			if act.Name != "" {
				names = append(names, act.Name)
			}
		}
		return uniquePlayNames(names)
	}
	return nil
}

func (e *Editor) replayRootDir() (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.ProjectPath != "" {
		return filepath.Dir(e.ProjectPath), nil
	}
	if e.ReplayDir != "" {
		return e.ReplayDir, nil
	}
	if e.FilePath != "" {
		return filepath.Dir(e.FilePath), nil
	}
	return "", errors.New("editor: open a project or set Editor.ReplayDir to save replays under replays/")
}

func (e *Editor) playSceneSlug() string {
	if e == nil {
		return "scene"
	}
	raw := ""
	switch {
	case e.ScenePath != "":
		raw = filepath.Base(e.ScenePath)
	case e.FilePath != "":
		raw = filepath.Base(e.FilePath)
	case e.Doc != nil && e.Doc.Name != "":
		raw = e.Doc.Name
	default:
		raw = "scene"
	}
	for _, suffix := range []string{".scene.yaml", ".scene.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(strings.ToLower(raw), suffix) {
			raw = raw[:len(raw)-len(suffix)]
			break
		}
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_' || unicode.IsSpace(r) || r == '.' || r == '/':
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_-")
	if slug == "" {
		return "scene"
	}
	return slug
}

func (e *Editor) currentPlayFrameAndRate() (frame int, rate int) {
	rate = e.PlayRateHz()
	if e.Playing() {
		return e.playSteps, rate
	}
	if e.RemoteConnected() {
		snap := e.remoteSnapshotShared()
		if snap.Loop != nil {
			if snap.Loop.StepHz > 0 {
				rate = snap.Loop.StepHz
			}
			return int(snap.Loop.Steps), rate
		}
	}
	return e.playSteps, rate
}

// SavePlayRollingReplay atomically encodes and writes Play mode's rolling recording under replays/<scene>-<frame>.replay.yaml
// using replay version 2 YAML.
func (e *Editor) SavePlayRollingReplay() (relPath, absPath string, rec *replay.Recording, err error) {
	if e == nil {
		return "", "", nil, errors.New("editor: nil editor")
	}
	root, err := e.replayRootDir()
	if err != nil {
		return "", "", nil, err
	}
	rec = e.PlayRollingRecording()
	if rec == nil {
		return "", "", nil, errors.New("editor: no rolling replay available")
	}
	frame, _ := e.currentPlayFrameAndRate()
	if rec.TotalSteps == 0 && frame > 0 && !e.Playing() {
		rec.TotalSteps = frame
	}
	data, err := rec.Encode()
	if err != nil {
		return "", "", nil, fmt.Errorf("editor: encode replay: %w", err)
	}
	replaysDir := filepath.Join(root, "replays")
	if err := os.MkdirAll(replaysDir, 0o755); err != nil {
		return "", "", nil, fmt.Errorf("editor: create replays directory: %w", err)
	}

	slug := e.playSceneSlug()
	baseName := fmt.Sprintf("%s-%d.replay.yaml", slug, frame)
	absPath = filepath.Join(replaysDir, baseName)
	for n := 2; ; n++ {
		if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
			break
		}
		baseName = fmt.Sprintf("%s-%d-%d.replay.yaml", slug, frame, n)
		absPath = filepath.Join(replaysDir, baseName)
	}
	if err := writeFileAtomic(absPath, data); err != nil {
		return "", "", nil, fmt.Errorf("editor: write replay %s: %w", baseName, err)
	}
	relPath = "replays/" + baseName
	e.playMarks.lastReplayPath = relPath
	e.playMarks.lastReplayAbs = absPath
	e.output.mu.Lock()
	e.output.lastReplayPath = relPath
	e.output.lastReplayAbs = absPath
	e.output.mu.Unlock()
	if e.ProjectPath != "" {
		e.filesRev++
	}
	return relPath, absPath, rec, nil
}

// resetPlayMarksForProject clears all Play marks, frozen-frame ask state, rolling replay buffer,
// and panic output state when switching projects or opening the Demo Room.
func (e *Editor) resetPlayMarksForProject() {
	if e == nil {
		return
	}
	e.playMarks = playMarksState{}
	e.playRolling = nil
	e.output.mu.Lock()
	e.output.panicReport = nil
	e.output.panicRuntimeOpen = false
	e.output.runLines = nil
	e.output.lastReplayPath = ""
	e.output.lastReplayAbs = ""
	e.output.mu.Unlock()
}

// AddPlayMark adds a mark at the current Play frame without stopping or pausing Play,
// saving the rolling input recording under replays/<scene>-<frame>.replay.yaml.
func (e *Editor) AddPlayMark() (PlayMark, error) {
	if e == nil {
		return PlayMark{}, errors.New("editor: nil editor")
	}
	if !e.Playing() && !e.RemoteConnected() {
		return PlayMark{}, errors.New("editor: marks can only be added while playing")
	}
	frame, rate := e.currentPlayFrameAndRate()
	relPath, absPath, _, err := e.SavePlayRollingReplay()
	if err != nil {
		return PlayMark{}, err
	}
	sec := float64(frame) / float64(max(1, rate))
	e.playMarks.nextMarkID++
	mark := PlayMark{
		ID:         e.playMarks.nextMarkID,
		Frame:      frame,
		Seconds:    sec,
		TimeLabel:  formatPlayClock(frame, rate),
		ReplayPath: relPath,
		AbsPath:    absPath,
		Scene:      e.playSceneDisplayName(),
		CreatedAt:  e.now(),
	}
	e.playMarks.marks = append(e.playMarks.marks, mark)
	e.Status = fmt.Sprintf("Marked frame %s (%s)", formatPlayFrameNumber(frame), relPath)
	e.notify()
	return mark, nil
}

// PlayMarks returns a copy of all marks recorded in the current or most recent Play session.
func (e *Editor) PlayMarks() []PlayMark {
	if e == nil || len(e.playMarks.marks) == 0 {
		return nil
	}
	return slices.Clone(e.playMarks.marks)
}

// UnsentPlayMarks returns a copy of marks that have not yet been sent to the agent.
func (e *Editor) UnsentPlayMarks() []PlayMark {
	if e == nil || len(e.playMarks.marks) == 0 {
		return nil
	}
	var out []PlayMark
	for _, m := range e.playMarks.marks {
		if !m.Sent {
			out = append(out, m)
		}
	}
	return out
}

// SendPlayMarks bundles all unsent marks into one PlayMarksThread, stores it in LastPlayMarksThread,
// invokes Editor.OnPlayMarksThread if set, and updates the Agent tab. Returns (zero, false) when no unsent marks exist.
func (e *Editor) SendPlayMarks() (PlayMarksThread, bool) {
	if e == nil {
		return PlayMarksThread{}, false
	}
	var unsent []PlayMark
	for i := range e.playMarks.marks {
		if !e.playMarks.marks[i].Sent {
			e.playMarks.marks[i].Sent = true
			unsent = append(unsent, e.playMarks.marks[i])
		}
	}
	if len(unsent) == 0 {
		return PlayMarksThread{}, false
	}
	summary := fmt.Sprintf("%d marks from this session: look at what happens around each one.", len(unsent))
	if len(unsent) == 1 {
		summary = "1 mark from this session: look at what happens around it."
	}
	thread := PlayMarksThread{
		Scene:     e.playSceneDisplayName(),
		Marks:     unsent,
		Summary:   summary,
		CreatedAt: e.now(),
	}
	cp := thread
	cp.Marks = slices.Clone(thread.Marks)
	e.playMarks.lastThread = &cp

	e.playMarks.tab = PlayTabAgent
	e.playMarks.sentQuestion = summary
	e.playMarks.sentMeta = fmt.Sprintf("%d marks · replays attached", len(unsent))
	e.playMarks.agentActive = true
	steps := make([]PlayAgentStep, 0, len(unsent)+1)
	for _, m := range unsent {
		steps = append(steps, PlayAgentStep{
			Text:   fmt.Sprintf("Attached mark at %s (frame %s) · %s", m.TimeLabel, formatPlayFrameNumber(m.Frame), m.ReplayPath),
			Status: "done",
		})
	}
	steps = append(steps, PlayAgentStep{
		Text:   "Reviewing marked replays via MCP",
		Status: "active",
	})
	e.playMarks.agentSteps = steps

	if e.OnPlayMarksThread != nil {
		e.OnPlayMarksThread(thread)
	}
	e.Status = fmt.Sprintf("Sent %d mark(s) to agent as one thread", len(unsent))
	e.notify()
	return thread, true
}

// LastPlayMarksThread returns a copy of the most recently dispatched PlayMarksThread, or nil if none has been sent.
func (e *Editor) LastPlayMarksThread() *PlayMarksThread {
	if e == nil || e.playMarks.lastThread == nil {
		return nil
	}
	cp := *e.playMarks.lastThread
	cp.Marks = slices.Clone(e.playMarks.lastThread.Marks)
	return &cp
}

// PlayInspectorTab returns the active right-panel tab during Play mode (PlayTabInspector or PlayTabAgent).
func (e *Editor) PlayInspectorTab() PlayInspectorTab {
	if e == nil {
		return PlayTabInspector
	}
	return e.playMarks.tab
}

// SetPlayInspectorTab switches between the Inspector and Agent tabs during Play mode.
func (e *Editor) SetPlayInspectorTab(tab PlayInspectorTab) {
	if e == nil || e.playMarks.tab == tab {
		return
	}
	e.playMarks.tab = tab
	e.notify()
}

// PlayAgentActive reports whether the Play mode Agent tab has an active question, sent marks thread, or stepping progress.
func (e *Editor) PlayAgentActive() bool {
	if e == nil {
		return false
	}
	return e.playMarks.agentActive || e.playMarks.agentPill != "" || len(e.playMarks.agentSteps) > 0
}

// AskFrozenFrameOpen reports whether Play mode's "Ask about frame N" dialog is currently open.
func (e *Editor) AskFrozenFrameOpen() bool {
	return e != nil && e.playMarks.askOpen
}

// AskFrozenFrameDraft returns the current prompt text typed into the "Ask about frame N" dialog.
func (e *Editor) AskFrozenFrameDraft() string {
	if e == nil {
		return ""
	}
	return e.playMarks.askDraft
}

// SetAskFrozenFrameDraft updates the prompt draft in the "Ask about frame N" dialog.
func (e *Editor) SetAskFrozenFrameDraft(text string) {
	if e == nil || e.playMarks.askDraft == text {
		return
	}
	e.playMarks.askDraft = text
	e.notify()
}

// AskFrozenFrameNumber returns the frozen frame number shown in the "Ask about frame N" dialog.
func (e *Editor) AskFrozenFrameNumber() int {
	if e == nil {
		return 0
	}
	return e.playMarks.askFrame
}

// AskFrozenFramePNG returns a copy of the PNG captured when "Ask about frame N" was opened.
func (e *Editor) AskFrozenFramePNG() []byte {
	if e == nil || len(e.playMarks.askFramePNG) == 0 {
		return nil
	}
	return slices.Clone(e.playMarks.askFramePNG)
}

// OpenAskFrozenFrame pauses Play mode, captures the current viewport frame PNG, and opens the "Ask about frame N" dialog.
func (e *Editor) OpenAskFrozenFrame() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if !e.Playing() && !e.RemoteConnected() {
		return errors.New("editor: start Play mode before asking about a frozen frame")
	}
	if e.Playing() && !e.playPaused {
		e.SetPlayPaused(true)
	} else if e.RemoteConnected() {
		snap := e.remoteSnapshotShared()
		if snap.Loop != nil && !snap.Loop.Paused {
			_ = e.RemotePause()
		}
	}
	frame, rate := e.currentPlayFrameAndRate()
	pngData, pw, ph := e.capturePlayFramePNG()
	e.playMarks.askOpen = true
	e.playMarks.askFrame = frame
	e.playMarks.askSeconds = float64(frame) / float64(max(1, rate))
	e.playMarks.askFramePNG = pngData
	e.playMarks.askFrameW = pw
	e.playMarks.askFrameH = ph
	e.Status = fmt.Sprintf("Frozen for your question · frame %s", formatPlayFrameNumber(frame))
	e.notify()
	return nil
}

// CloseAskFrozenFrame closes the "Ask about frame N" dialog while keeping the game paused.
func (e *Editor) CloseAskFrozenFrame() {
	if e == nil || !e.playMarks.askOpen {
		return
	}
	e.playMarks.askOpen = false
	e.notify()
}

// SubmitAskFrozenFrame saves the rolling replay under replays/, attaches the captured frame image and live state snapshot,
// switches to the Agent tab, invokes Editor.OnAskFrozenFrame if set, and keeps the game paused.
func (e *Editor) SubmitAskFrozenFrame(prompt string) (AskFrozenFrameRequest, error) {
	if e == nil {
		return AskFrozenFrameRequest{}, errors.New("editor: nil editor")
	}
	if !e.Playing() && !e.RemoteConnected() {
		return AskFrozenFrameRequest{}, errors.New("editor: start Play mode before asking about a frozen frame")
	}
	text := strings.TrimSpace(prompt)
	if text == "" {
		text = strings.TrimSpace(e.playMarks.askDraft)
	}
	if text == "" {
		return AskFrozenFrameRequest{}, errors.New("editor: describe what looks wrong before asking")
	}
	if e.Playing() && !e.playPaused {
		e.SetPlayPaused(true)
	}

	frame, rate := e.currentPlayFrameAndRate()
	if e.playMarks.askOpen && e.playMarks.askFrame > 0 {
		frame = e.playMarks.askFrame
	}
	pngData, pw, ph := e.playMarks.askFramePNG, e.playMarks.askFrameW, e.playMarks.askFrameH
	if len(pngData) == 0 {
		pngData, pw, ph = e.capturePlayFramePNG()
	}

	relPath, absPath, rec, err := e.SavePlayRollingReplay()
	if err != nil {
		return AskFrozenFrameRequest{}, err
	}
	entities := e.snapshotPlayLiveEntities()
	liveChanges := e.RemoteLiveChanges()
	selID := e.SelectedID
	if e.RemoteConnected() {
		if r := e.SelectedRemoteEntity(); r != "" {
			selID = r
		}
	}

	req := AskFrozenFrameRequest{
		Frame:        frame,
		Seconds:      float64(frame) / float64(max(1, rate)),
		Scene:        e.playSceneDisplayName(),
		Prompt:       text,
		FramePNG:     slices.Clone(pngData),
		FrameWidth:   pw,
		FrameHeight:  ph,
		ReplayPath:   relPath,
		ReplayAbs:    absPath,
		ReplayFrames: rec.TotalSteps,
		SelectedID:   selID,
		LiveEntities: entities,
		LiveChanges:  liveChanges,
		CreatedAt:    e.now(),
	}

	reqCopy := cloneAskFrozenFrameRequest(req)
	e.playMarks.askOpen = false
	e.playMarks.askDraft = ""
	e.playMarks.activeAsk = &reqCopy
	e.playMarks.activeReplay = rec.Clone()
	e.playMarks.tab = PlayTabAgent
	e.playMarks.agentActive = true
	e.playMarks.sentQuestion = text

	secLen := float64(rec.TotalSteps) / float64(max(1, rate))
	metaParts := []string{
		"Frame " + formatPlayFrameNumber(frame),
		fmt.Sprintf("replay %.0f s", math.Max(1, math.Round(secLen))),
	}
	if selID != "" {
		metaParts = append(metaParts, selID)
	}
	if len(liveChanges) > 0 {
		unit := "live changes"
		if len(liveChanges) == 1 {
			unit = "live change"
		}
		metaParts = append(metaParts, fmt.Sprintf("%d %s", len(liveChanges), unit))
	}
	e.playMarks.sentMeta = strings.Join(metaParts, " · ")

	startFrame := max(0, frame-rec.TotalSteps)
	e.playMarks.agentSteps = []PlayAgentStep{
		{
			Text:   fmt.Sprintf("Loaded replay frames %s–%s and the frame image", formatPlayFrameNumber(startFrame), formatPlayFrameNumber(frame)),
			Status: "done",
		},
		{
			Text:   "Waiting for agent to step the replay via MCP",
			Status: "active",
		},
		{
			Text:   "Propose a fix as a review",
			Status: "pending",
		},
	}

	if e.OnAskFrozenFrame != nil {
		e.OnAskFrozenFrame(req)
	}
	e.Status = fmt.Sprintf("Asked agent about frame %s (%s)", formatPlayFrameNumber(frame), relPath)
	e.notify()
	return req, nil
}

// ActiveFrozenFrameContext returns a snapshot of the active frozen-frame Ask request (or the current live Play frame context when no Ask dialog has been submitted yet).
func (e *Editor) ActiveFrozenFrameContext() *AskFrozenFrameRequest {
	if e == nil {
		return nil
	}
	if e.playMarks.activeAsk != nil {
		cp := cloneAskFrozenFrameRequest(*e.playMarks.activeAsk)
		cp.LiveEntities = e.snapshotPlayLiveEntities()
		cp.LiveChanges = e.RemoteLiveChanges()
		return &cp
	}
	if !e.Playing() && !e.RemoteConnected() {
		return nil
	}
	frame, rate := e.currentPlayFrameAndRate()
	pngData, pw, ph := e.capturePlayFramePNG()
	rec := e.PlayRollingRecording()
	frames := 0
	if rec != nil {
		frames = rec.TotalSteps
	}
	return &AskFrozenFrameRequest{
		Frame:        frame,
		Seconds:      float64(frame) / float64(max(1, rate)),
		Scene:        e.playSceneDisplayName(),
		FramePNG:     pngData,
		FrameWidth:   pw,
		FrameHeight:  ph,
		ReplayPath:   e.playMarks.lastReplayPath,
		ReplayAbs:    e.playMarks.lastReplayAbs,
		ReplayFrames: frames,
		SelectedID:   e.SelectedID,
		LiveEntities: e.snapshotPlayLiveEntities(),
		LiveChanges:  e.RemoteLiveChanges(),
		CreatedAt:    e.now(),
	}
}

const maxAgentPlayReplaySteps = 3600

// AgentStepPlayReplay steps Play mode across frames [fromFrame, toFrame] on behalf of the agent (via MCP),
// updates the viewport agent pill ("agent stepping frames A–B") and Agent tab step log ("Stepped frames A–B with game_loop_step…"),
// and keeps the game paused until the person explicitly resumes.
func (e *Editor) AgentStepPlayReplay(fromFrame, toFrame int, note string) (int, error) {
	if e == nil {
		return 0, errors.New("editor: nil editor")
	}
	if fromFrame < 0 || toFrame < 0 {
		return 0, errors.New("editor: frame numbers must be non-negative")
	}
	if toFrame < fromFrame {
		return 0, fmt.Errorf("editor: to_frame (%d) must be >= from_frame (%d)", toFrame, fromFrame)
	}
	if toFrame-fromFrame > maxAgentPlayReplaySteps {
		return 0, fmt.Errorf("editor: frame span %d exceeds maximum %d steps", toFrame-fromFrame, maxAgentPlayReplaySteps)
	}
	if !e.Playing() {
		return 0, errors.New("editor: Play mode is not active")
	}
	if !e.playPaused {
		e.SetPlayPaused(true)
	}

	// If a replay recording is loaded and the requested target frame is earlier than the current frame,
	// replay deterministically from step 0 up to toFrame (rebased against the rolling 60 s window when needed) while staying paused.
	if toFrame < e.playSteps {
		rec := e.playMarks.activeReplay
		fromRolling := false
		if rec == nil && e.playMarks.activeAsk != nil && e.playMarks.activeAsk.ReplayAbs != "" {
			if loaded, err := replay.LoadFile(e.playMarks.activeAsk.ReplayAbs); err == nil {
				rec = loaded
				fromRolling = true
			}
		}
		if rec == nil {
			rec = e.PlayRollingRecording()
			fromRolling = true
		}
		target := toFrame
		if fromRolling {
			if windowStart := e.PlayRollingWindowStart(); windowStart > 0 {
				if toFrame < windowStart {
					return e.playSteps, fmt.Errorf("editor: frame %d is outside the 60 s rolling replay window (starts at frame %d)", toFrame, windowStart)
				}
				target = toFrame - windowStart
			}
		}
		if err := e.ReplayUpToFrame(rec, target); err != nil {
			return e.playSteps, err
		}
	} else {
		steps := toFrame - e.playSteps
		if steps > maxAgentPlayReplaySteps {
			return e.playSteps, fmt.Errorf("editor: advancing %d steps to frame %d exceeds maximum %d steps", steps, toFrame, maxAgentPlayReplaySteps)
		}
		if steps > 0 {
			e.StepPlay(steps)
			if !e.playing {
				return e.playSteps, fmt.Errorf("editor: replay stopped before reaching frame %d (stopped at frame %d)", toFrame, e.playSteps)
			}
		}
	}

	// Guarantee game stays paused after agent stepping.
	e.playPaused = true
	if e.playLoop != nil {
		e.playLoop.SetPaused(true)
	}

	pill := formatAgentSteppingPill(fromFrame, toFrame)
	stepText := fmt.Sprintf("Stepped frames %s–%s with game_loop_step", formatPlayFrameNumber(fromFrame), formatPlayFrameNumber(toFrame))
	if trimmed := strings.TrimSpace(note); trimmed != "" {
		stepText += "; " + trimmed
	} else {
		stepText += "…"
	}

	e.playMarks.agentPill = pill
	e.playMarks.agentActive = true
	e.playMarks.tab = PlayTabAgent
	e.upsertAgentStep(stepText, "done")
	e.Status = pill
	e.notify()
	return e.playSteps, nil
}

// AgentStepPlayReplayFile loads a replay file from absPath (optional) and runs AgentStepPlayReplay on it.
func (e *Editor) AgentStepPlayReplayFile(absPath string, fromFrame, toFrame int, note string) (int, error) {
	if e == nil {
		return 0, errors.New("editor: nil editor")
	}
	if fromFrame < 0 || toFrame < 0 {
		return 0, errors.New("editor: frame numbers must be non-negative")
	}
	if toFrame < fromFrame {
		return 0, fmt.Errorf("editor: to_frame (%d) must be >= from_frame (%d)", toFrame, fromFrame)
	}
	if toFrame-fromFrame > maxAgentPlayReplaySteps {
		return 0, fmt.Errorf("editor: frame span %d exceeds maximum %d steps", toFrame-fromFrame, maxAgentPlayReplaySteps)
	}
	if absPath != "" {
		rec, err := replay.LoadFile(absPath)
		if err != nil {
			return 0, err
		}
		e.playMarks.activeReplay = rec
		if !e.Playing() {
			if err := e.ReplayUpToFrame(rec, fromFrame); err != nil {
				return 0, err
			}
		}
	}
	return e.AgentStepPlayReplay(fromFrame, toFrame, note)
}

func (e *Editor) upsertAgentStep(text, status string) {
	// Replace the placeholder "Waiting for agent..." active row if present.
	for i := range e.playMarks.agentSteps {
		if strings.HasPrefix(e.playMarks.agentSteps[i].Text, "Waiting for agent") {
			e.playMarks.agentSteps[i] = PlayAgentStep{Text: text, Status: status}
			return
		}
	}
	// Insert before the final pending "Propose a fix as a review" row when present.
	n := len(e.playMarks.agentSteps)
	if n > 0 && e.playMarks.agentSteps[n-1].Status == "pending" {
		tail := e.playMarks.agentSteps[n-1]
		e.playMarks.agentSteps[n-1] = PlayAgentStep{Text: text, Status: status}
		e.playMarks.agentSteps = append(e.playMarks.agentSteps, tail)
		return
	}
	e.playMarks.agentSteps = append(e.playMarks.agentSteps, PlayAgentStep{Text: text, Status: status})
}

// PlayAgentPill returns the viewport agent status pill text (e.g. "agent stepping frames 4 800–4 812"), or "".
func (e *Editor) PlayAgentPill() string {
	if e == nil {
		return ""
	}
	return e.playMarks.agentPill
}

// PlayAgentSteps returns a copy of the step rows shown in the Play mode Agent tab.
func (e *Editor) PlayAgentSteps() []PlayAgentStep {
	if e == nil || len(e.playMarks.agentSteps) == 0 {
		return nil
	}
	return slices.Clone(e.playMarks.agentSteps)
}

// RecordPlayAgentStep appends a custom progress step to the Play mode Agent tab.
func (e *Editor) RecordPlayAgentStep(text, status string) {
	if e == nil || strings.TrimSpace(text) == "" {
		return
	}
	if status == "" {
		status = "active"
	}
	e.playMarks.agentActive = true
	e.upsertAgentStep(text, status)
	e.notify()
}

// ClearPlayAgentActivity clears the viewport agent stepping pill.
func (e *Editor) ClearPlayAgentActivity() {
	if e == nil {
		return
	}
	if e.playMarks.agentPill == "" {
		return
	}
	e.playMarks.agentPill = ""
	e.notify()
}

func (e *Editor) snapshotPlayLiveEntities() []PlayLiveEntityState {
	if e == nil {
		return nil
	}
	if e.RemoteConnected() {
		snap := e.remoteSnapshotShared()
		if len(snap.Entities) > 0 {
			out := make([]PlayLiveEntityState, 0, len(snap.Entities))
			for _, ent := range snap.Entities {
				out = append(out, PlayLiveEntityState{
					ID:    ent.Ref,
					Name:  ent.Name,
					Layer: ent.Layer,
				})
			}
			return out
		}
	}
	scene := e.playScene
	if scene == nil {
		scene = e.Built
	}
	if scene == nil {
		return nil
	}
	var out []PlayLiveEntityState
	var walk func(layer string, objs []*sceneformat.BuiltObject)
	walk = func(layer string, objs []*sceneformat.BuiltObject) {
		for _, bo := range objs {
			if bo == nil || bo.Doc == nil {
				continue
			}
			ent := PlayLiveEntityState{
				ID:    bo.ID,
				Name:  bo.Doc.Name,
				Layer: layer,
				X:     bo.Doc.X,
				Y:     bo.Doc.Y,
			}
			if len(bo.Doc.Components) > 0 {
				ent.Components = make(map[string]any, len(bo.Doc.Components))
				for k, v := range bo.Doc.Components {
					ent.Components[k] = cloneAny(v)
				}
			}
			if len(bo.Doc.Properties) > 0 {
				ent.Properties = make(map[string]any, len(bo.Doc.Properties))
				for k, v := range bo.Doc.Properties {
					ent.Properties[k] = cloneAny(v)
				}
			}
			out = append(out, ent)
			if len(bo.Children) > 0 {
				walk(layer, bo.Children)
			}
		}
	}
	for _, layer := range scene.Layers {
		walk(layer.Name, layer.Objects)
	}
	return out
}

func (e *Editor) capturePlayFramePNG() ([]byte, int, int) {
	if e == nil {
		return nil, 0, 0
	}
	w, h := e.CanvasSize()
	if w <= 0 || h <= 0 {
		w, h = e.ViewportSize()
	}
	var d core.Drawing
	e.DrawViewport(&d, core.Size{Width: core.Points(w), Height: core.Points(h)})
	pw, ph := int(math.Round(float64(w))), int(math.Round(float64(h)))
	if pw <= 0 {
		pw = 640
	}
	if ph <= 0 {
		ph = 360
	}
	res, err := gamemcp.RenderPNG(d.Ops, pw, ph, 0)
	if err != nil {
		return nil, 0, 0
	}
	return res.PNG, res.Width, res.Height
}

func (e *Editor) playSceneDisplayName() string {
	if e == nil {
		return "scene"
	}
	if e.ScenePath != "" {
		return e.ScenePath
	}
	if e.Doc != nil && e.Doc.Name != "" {
		return e.Doc.Name
	}
	if e.FilePath != "" {
		return filepath.Base(e.FilePath)
	}
	return "scene"
}

func cloneAskFrozenFrameRequest(r AskFrozenFrameRequest) AskFrozenFrameRequest {
	out := r
	out.FramePNG = slices.Clone(r.FramePNG)
	if len(r.LiveEntities) > 0 {
		out.LiveEntities = make([]PlayLiveEntityState, len(r.LiveEntities))
		for i, ent := range r.LiveEntities {
			cp := ent
			if len(ent.Components) > 0 {
				cp.Components = make(map[string]any, len(ent.Components))
				for k, v := range ent.Components {
					cp.Components[k] = cloneAny(v)
				}
			}
			if len(ent.Properties) > 0 {
				cp.Properties = make(map[string]any, len(ent.Properties))
				for k, v := range ent.Properties {
					cp.Properties[k] = cloneAny(v)
				}
			}
			out.LiveEntities[i] = cp
		}
	}
	if len(r.LiveChanges) > 0 {
		out.LiveChanges = slices.Clone(r.LiveChanges)
	}
	return out
}

func formatPlayFrameNumber(n int) string {
	if n < 0 {
		return "-" + formatPlayFrameNumber(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
	}
	for i := rem; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func formatPlayClock(frame, rate int) string {
	if rate <= 0 {
		rate = 60
	}
	sec := float64(frame) / float64(rate)
	m := int(sec) / 60
	r := sec - float64(m*60)
	if r < 10 {
		return fmt.Sprintf("%d:0%.1f", m, r)
	}
	return fmt.Sprintf("%d:%.1f", m, r)
}

func formatAgentSteppingPill(fromFrame, toFrame int) string {
	return fmt.Sprintf("agent stepping frames %s–%s", formatPlayFrameNumber(fromFrame), formatPlayFrameNumber(toFrame))
}
