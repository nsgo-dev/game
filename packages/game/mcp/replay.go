package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/nsgo/automation"
)

// Replay tool names registered by Bridge with github.com/nsgo-dev/nsgo/automation.
const (
	// ToolReplayStatus reports current recording/playback state, frame progress and saved recording names.
	ToolReplayStatus = "game_replay_status"
	// ToolReplayRecordStart starts recording fixed-step inputs into a named recording.
	ToolReplayRecordStart = "game_replay_record_start"
	// ToolReplayRecordStop stops the active recording, stores it and returns its deterministic YAML text and summary.
	ToolReplayRecordStop = "game_replay_record_stop"
	// ToolReplayPlay starts replaying a stored recording by name or an inline YAML recording.
	ToolReplayPlay = "game_replay_play"
	// ToolReplayStop stops an active replay playback.
	ToolReplayStop = "game_replay_stop"
	// ToolReplayList lists stored recordings with their frame counts, seeds, step rates and metadata.
	ToolReplayList = "game_replay_list"
)

var (
	// ErrReplayNotConfigured reports a replay tool call on a Bridge without a configured ReplayController.
	ErrReplayNotConfigured = errors.New("mcp: no replay controller configured")
	// ErrAlreadyRecording reports starting a recording while another recording is already active.
	ErrAlreadyRecording = errors.New("mcp: recording is already in progress")
	// ErrNotRecording reports stopping a recording when none is active.
	ErrNotRecording = errors.New("mcp: no recording in progress")
	// ErrAlreadyReplaying reports starting a recording or replay while playback is already in progress.
	ErrAlreadyReplaying = errors.New("mcp: replay is already in progress")
	// ErrNotReplaying reports stopping a replay when none is active.
	ErrNotReplaying = errors.New("mcp: no replay in progress")
)

const defaultReplayStepRate = 60

var _ replay.ActionSource = (*ReplayController)(nil)

// ReplayConfig configures a ReplayController for MCP input recording and deterministic playback.
//
// Pass the resulting *ReplayController as your game's replay.ActionSource so live play, recording and playback share the exact same update loop.
type ReplayConfig struct {
	// Source is the live replay.ActionSource (such as *input.Actions) forwarded during live play and wrapped by a fresh replay.Recorder on each game_replay_record_start. It is required for recording, and it keeps being stepped during playback so its previous-step snapshot is current when live input resumes.
	Source replay.ActionSource

	// Actions lists the default watched action names recorded by game_replay_record_start when the caller omits actions.
	Actions []string

	// Axes lists the default watched axis names recorded by game_replay_record_start when the caller omits axes.
	Axes []string

	// StepRate is the game's fixed step rate in steps per second when the Bridge has no game loop (Config.Loop or Config.LoopFunc); with a loop, the loop's StepRate is used. It defaults to 60. Recordings are made at this rate, and game_replay_play refuses a recording made at another rate.
	StepRate int

	// StepFunc, when set, advances the simulation by one fixed step of duration dt and is expected to call the controller's Step method once. game_replay_play uses it for run_to_end and steps; without it, those advance the Bridge's game loop with Loop.StepNow.
	StepFunc func(dt time.Duration)

	// SeedFunc, when set, supplies the default seed for game_replay_record_start when the caller omits seed.
	SeedFunc func() uint64

	// ResetFunc, when set, resets the simulation deterministically to seed. With reset (the default when ResetFunc or a seed target is configured), game_replay_record_start and game_replay_play first re-seed the Bridge's seed targets (Config.Random, Config.RandomStreams, Config.SetSeed) and then call ResetFunc, so a recording and its replay start from the same state.
	ResetFunc func(seed uint64) error

	// OnRecordStart, when set, is called after a recording starts.
	OnRecordStart func(name string, seed uint64)

	// OnRecordStop, when set, is called after a recording stops with the stored name and a copy of the completed recording.
	OnRecordStop func(name string, rec *replay.Recording)

	// OnReplayStart, when set, is called after ResetFunc succeeds and playback starts.
	OnReplayStart func(rec *replay.Recording)

	// OnReplayStop, when set, is called when playback finishes or is stopped early.
	OnReplayStop func()

	// SaveDir is an optional root directory on disk where game_replay_record_stop may write YAML recordings when save_path is supplied. Writes go through os.Root, so relative paths and symbolic links cannot leave SaveDir; when SaveDir is empty, save_path is refused.
	SaveDir string
}

type storedRecording struct {
	rec      *replay.Recording
	metadata map[string]string
}

// ReplayController coordinates live input forwarding, recording via replay.Recorder, and deterministic playback via replay.Player for MCP tools.
//
// ReplayController implements replay.ActionSource so a game can read inputs from it unconditionally across live play, recording and playback.
type ReplayController struct {
	cfg ReplayConfig

	activeRec     *replay.Recorder
	recordingName string
	recordingMeta map[string]string

	player     *replay.Player
	replayName string
	// trailing is set once the player has run its trailing step after the final recorded step, so the next Step returns to live input.
	trailing bool

	saved   map[string]storedRecording
	nextSeq int
}

// NewReplayController creates a ReplayController from cfg.
func NewReplayController(cfg ReplayConfig) *ReplayController {
	cfg.Actions = slices.Clone(cfg.Actions)
	cfg.Axes = slices.Clone(cfg.Axes)
	return &ReplayController{
		cfg:   cfg,
		saved: make(map[string]storedRecording),
	}
}

// Replay returns the ReplayController configured on b, or nil if none is set.
func (b *Bridge) Replay() *ReplayController {
	if b == nil {
		return nil
	}
	return b.cfg.Replay
}

// SetReplay replaces the ReplayController configured on b.
func (b *Bridge) SetReplay(ctrl *ReplayController) {
	if b == nil {
		return
	}
	b.cfg.Replay = ctrl
}

// Step advances the active replay.Player, active replay.Recorder or live Source by one fixed step.
//
// After the final recorded step, one more Step is a trailing playback step that releases actions still held at the end of the recording (firing JustReleased once); the Step after it finishes playback and resumes live input. The live Source is stepped during playback too, so its previous-step snapshot is current when live input resumes.
func (c *ReplayController) Step() {
	if c == nil {
		return
	}
	if c.player != nil && c.trailing {
		c.finishReplay()
	}
	if c.player != nil {
		if c.player.Done() {
			c.trailing = true
		}
		c.player.Step()
		if c.cfg.Source != nil {
			c.cfg.Source.Step()
		}
		return
	}
	if c.activeRec != nil {
		c.activeRec.Step()
		return
	}
	if c.cfg.Source != nil {
		c.cfg.Source.Step()
	}
}

// Pressed reports whether name is pressed on the active input source for the current step.
func (c *ReplayController) Pressed(name string) bool {
	if c == nil {
		return false
	}
	if c.player != nil {
		return c.player.Pressed(name)
	}
	if c.activeRec != nil {
		return c.activeRec.Pressed(name)
	}
	if c.cfg.Source != nil {
		return c.cfg.Source.Pressed(name)
	}
	return false
}

// JustPressed reports whether name became pressed on the active input source on the current step.
func (c *ReplayController) JustPressed(name string) bool {
	if c == nil {
		return false
	}
	if c.player != nil {
		return c.player.JustPressed(name)
	}
	if c.activeRec != nil {
		return c.activeRec.JustPressed(name)
	}
	if c.cfg.Source != nil {
		return c.cfg.Source.JustPressed(name)
	}
	return false
}

// JustReleased reports whether name was released on the active input source on the current step.
func (c *ReplayController) JustReleased(name string) bool {
	if c == nil {
		return false
	}
	if c.player != nil {
		return c.player.JustReleased(name)
	}
	if c.activeRec != nil {
		return c.activeRec.JustReleased(name)
	}
	if c.cfg.Source != nil {
		return c.cfg.Source.JustReleased(name)
	}
	return false
}

// Axis returns the value of axis name on the active input source for the current step.
func (c *ReplayController) Axis(name string) float64 {
	if c == nil {
		return 0
	}
	if c.player != nil {
		return c.player.Axis(name)
	}
	if c.activeRec != nil {
		return c.activeRec.Axis(name)
	}
	if c.cfg.Source != nil {
		return c.cfg.Source.Axis(name)
	}
	return 0
}

// Recording reports whether an input recording is currently in progress.
func (c *ReplayController) Recording() bool {
	return c != nil && c.activeRec != nil
}

// Replaying reports whether replay playback is currently in progress (not yet finished).
func (c *ReplayController) Replaying() bool {
	return c != nil && c.player != nil && !c.player.Done()
}

// SaveRecording validates and stores a deep copy of rec under name in c's in-memory store.
func (c *ReplayController) SaveRecording(name string, rec *replay.Recording) error {
	if c == nil {
		return ErrReplayNotConfigured
	}
	if name == "" {
		return errors.New("mcp: recording name is required")
	}
	if err := rec.Validate(); err != nil {
		return fmt.Errorf("mcp: invalid recording %q: %w", name, err)
	}
	if c.saved == nil {
		c.saved = make(map[string]storedRecording)
	}
	c.saved[name] = storedRecording{rec: rec.Clone()}
	return nil
}

// GetRecording returns a deep copy of the stored recording named name.
func (c *ReplayController) GetRecording(name string) (*replay.Recording, bool) {
	if c == nil {
		return nil, false
	}
	entry, ok := c.saved[name]
	if !ok || entry.rec == nil {
		return nil, false
	}
	return entry.rec.Clone(), true
}

// DeleteRecording removes the stored recording named name and reports whether it existed.
func (c *ReplayController) DeleteRecording(name string) bool {
	if c == nil {
		return false
	}
	if _, ok := c.saved[name]; !ok {
		return false
	}
	delete(c.saved, name)
	return true
}

// RecordingNames returns the sorted names of all recordings stored in c.
func (c *ReplayController) RecordingNames() []string {
	if c == nil || len(c.saved) == 0 {
		return []string{}
	}
	names := make([]string, 0, len(c.saved))
	for name := range c.saved {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// replayStepRate returns the game's fixed step rate: the active game loop's StepRate, else ReplayConfig.StepRate, else 60.
func (b *Bridge) replayStepRate(c *ReplayController) int {
	if l := b.activeLoop(); l != nil {
		return l.StepRate()
	}
	if c.cfg.StepRate > 0 {
		return c.cfg.StepRate
	}
	return defaultReplayStepRate
}

// canResetReplay reports whether a reset before recording or playback has anything to do.
func (b *Bridge) canResetReplay(c *ReplayController) bool {
	return c.cfg.ResetFunc != nil || b.seedConfigured()
}

// resetReplay re-seeds the Bridge's seed targets and then calls ReplayConfig.ResetFunc with seed.
func (b *Bridge) resetReplay(c *ReplayController, seed uint64) error {
	b.applySeed(seed)
	if c.cfg.ResetFunc != nil {
		return c.cfg.ResetFunc(seed)
	}
	return nil
}

// resolveReplayReset returns whether to reset, given the caller's optional reset argument: it defaults to true whenever a reset has something to do.
func (b *Bridge) resolveReplayReset(c *ReplayController, arg *bool) (bool, error) {
	can := b.canResetReplay(c)
	if arg == nil {
		return can, nil
	}
	if *arg && !can {
		return false, errors.New("mcp: reset requested but neither ReplayConfig.ResetFunc nor a seed target (Config.Random, Config.RandomStreams, Config.SetSeed) is configured")
	}
	return *arg, nil
}

func (c *ReplayController) finishReplay() {
	if c.player == nil {
		return
	}
	c.player = nil
	c.replayName = ""
	c.trailing = false
	if c.cfg.OnReplayStop != nil {
		c.cfg.OnReplayStop()
	}
}

func (c *ReplayController) allocateName() string {
	for {
		c.nextSeq++
		candidate := fmt.Sprintf("recording_%d", c.nextSeq)
		if _, exists := c.saved[candidate]; !exists {
			return candidate
		}
	}
}

// ReplayStatusResult is the JSON result of game_replay_status.
type ReplayStatusResult struct {
	Recording         bool              `json:"recording"`
	RecordingName     string            `json:"recording_name,omitempty"`
	RecordingSeed     uint64            `json:"recording_seed,omitempty"`
	RecordingStepRate int               `json:"recording_step_rate,omitempty"`
	RecordingMetadata map[string]string `json:"recording_metadata,omitempty"`
	FramesRecorded    int               `json:"frames_recorded"`
	Replaying         bool              `json:"replaying"`
	ReplayDone        bool              `json:"replay_done,omitempty"`
	ReplayName        string            `json:"replay_name,omitempty"`
	ReplayFrame       int               `json:"replay_frame"`
	ReplayTotalFrames int               `json:"replay_total_frames"`
	SavedRecordings   []string          `json:"saved_recordings"`
}

// ReplayRecordStartResult is the JSON result of game_replay_record_start.
type ReplayRecordStartResult struct {
	Recording bool              `json:"recording"`
	Name      string            `json:"name"`
	Seed      uint64            `json:"seed"`
	StepRate  int               `json:"step_rate"`
	Actions   []string          `json:"actions,omitempty"`
	Axes      []string          `json:"axes,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ReplayRecordStopResult is the JSON result of game_replay_record_stop.
type ReplayRecordStopResult struct {
	Name      string            `json:"name"`
	Frames    int               `json:"frames"`
	Seed      uint64            `json:"seed"`
	StepRate  int               `json:"step_rate"`
	Events    int               `json:"events"`
	Actions   []string          `json:"actions,omitempty"`
	Axes      []string          `json:"axes,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	SavedPath string            `json:"saved_path,omitempty"`
	YAML      string            `json:"yaml"`
}

// ReplayPlayResult is the JSON result of game_replay_play.
type ReplayPlayResult struct {
	Name              string `json:"name,omitempty"`
	Seed              uint64 `json:"seed"`
	StepRate          int    `json:"step_rate"`
	Replaying         bool   `json:"replaying"`
	Completed         bool   `json:"completed"`
	FramesPlayed      int    `json:"frames_played"`
	ReplayTotalFrames int    `json:"replay_total_frames"`
}

// ReplayStopResult is the JSON result of game_replay_stop.
type ReplayStopResult struct {
	Stopped           bool   `json:"stopped"`
	Name              string `json:"name,omitempty"`
	FramesPlayed      int    `json:"frames_played"`
	ReplayTotalFrames int    `json:"replay_total_frames"`
}

// ReplaySummary describes one stored recording returned by game_replay_list.
type ReplaySummary struct {
	Name     string            `json:"name"`
	Frames   int               `json:"frames"`
	Seed     uint64            `json:"seed"`
	StepRate int               `json:"step_rate"`
	Events   int               `json:"events"`
	Actions  []string          `json:"actions,omitempty"`
	Axes     []string          `json:"axes,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ReplayListResult is the JSON result of game_replay_list.
type ReplayListResult struct {
	Count      int             `json:"count"`
	Recordings []ReplaySummary `json:"recordings"`
}

var (
	schemaReplayStatus = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)

	schemaReplayRecordStart = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"name": {
				"type": "string",
				"description": "Name under which the recording will be stored when stopped (auto-generated when omitted)."
			},
			"seed": {
				"type": "integer",
				"minimum": 0,
				"description": "Deterministic RNG seed recorded in the replay header (defaults to ReplayConfig.SeedFunc or 0)."
			},
			"actions": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Override the watched action names for this recording (defaults to ReplayConfig.Actions)."
			},
			"axes": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Override the watched axis names for this recording (defaults to ReplayConfig.Axes)."
			},
			"metadata": {
				"type": "object",
				"additionalProperties": {"type": "string"},
				"description": "Optional string key-value metadata kept with the recording in this game's in-memory store and returned by game_replay_status and game_replay_list. It is not part of the recording's YAML, so save_path, the returned yaml and replays from inline yaml do not carry it."
			},
			"reset": {
				"type": "boolean",
				"description": "Re-seed Config.Random, Config.RandomStreams and Config.SetSeed with seed and call ReplayConfig.ResetFunc(seed) before recording starts, so the recording starts from the state game_replay_play resets to (default true when ResetFunc or a seed target is configured)."
			}
		}
	}`)

	schemaReplayRecordStop = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"name": {
				"type": "string",
				"description": "Optional override name under which to store the finished recording."
			},
			"save_path": {
				"type": "string",
				"description": "Optional relative file path under ReplayConfig.SaveDir to also write the deterministic YAML recording."
			},
			"overwrite": {
				"type": "boolean",
				"description": "Replace an existing stored recording of the same name and an existing file at save_path (default false, which refuses both and keeps recording)."
			}
		}
	}`)

	schemaReplayPlay = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of a stored recording from game_replay_list. Mutually exclusive with yaml."
			},
			"yaml": {
				"type": "string",
				"description": "Inline deterministic replay YAML text (format version 2). Mutually exclusive with name."
			},
			"run_to_end": {
				"type": "boolean",
				"description": "Advance every recorded frame synchronously via ReplayConfig.StepFunc or the game loop's StepNow before returning (default false). Refused for a recording longer than 3600 steps; play it with steps or frame by frame instead."
			},
			"steps": {
				"type": "integer",
				"minimum": 1,
				"maximum": 3600,
				"description": "Advance up to this many recorded frames synchronously via ReplayConfig.StepFunc or the game loop's StepNow (1..3600). Mutually exclusive with run_to_end."
			},
			"reset": {
				"type": "boolean",
				"description": "Re-seed Config.Random, Config.RandomStreams and Config.SetSeed with the recording's seed and call ReplayConfig.ResetFunc(seed) before playback starts (default true when ResetFunc or a seed target is configured)."
			}
		}
	}`)

	schemaReplayStop = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)

	schemaReplayList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)
)

func (b *Bridge) replayTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolReplayStatus,
			Description: "Return the current input recording and replay playback status, frame progress and saved recording names.",
			InputSchema: schemaReplayStatus,
			Handler:     b.handleReplayStatus,
		},
		{
			Name:        ToolReplayRecordStart,
			Description: "Start recording per-step game inputs into a named recording.",
			InputSchema: schemaReplayRecordStart,
			Handler:     b.handleReplayRecordStart,
		},
		{
			Name:        ToolReplayRecordStop,
			Description: "Stop the active input recording, store it in memory (and optionally under SaveDir) and return its deterministic YAML text and summary.",
			InputSchema: schemaReplayRecordStop,
			Handler:     b.handleReplayRecordStop,
		},
		{
			Name:        ToolReplayPlay,
			Description: "Replay a stored recording by name or an inline YAML recording, optionally resetting to its seed and advancing frames to completion.",
			InputSchema: schemaReplayPlay,
			Handler:     b.handleReplayPlay,
		},
		{
			Name:        ToolReplayStop,
			Description: "Stop an active replay playback cleanly.",
			InputSchema: schemaReplayStop,
			Handler:     b.handleReplayStop,
		},
		{
			Name:        ToolReplayList,
			Description: "List stored input recordings with their frame counts, seeds, step rates and metadata.",
			InputSchema: schemaReplayList,
			Handler:     b.handleReplayList,
		},
	}
}

func (b *Bridge) requireReplay() (*ReplayController, error) {
	if b == nil || b.cfg.Replay == nil {
		return nil, ErrReplayNotConfigured
	}
	return b.cfg.Replay, nil
}

func (b *Bridge) handleReplayStatus(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	c, err := b.requireReplay()
	if err != nil {
		return nil, err
	}
	res := ReplayStatusResult{
		SavedRecordings: c.RecordingNames(),
	}
	if c.activeRec != nil {
		res.Recording = true
		res.RecordingName = c.recordingName
		res.RecordingSeed = c.activeRec.Seed()
		res.RecordingStepRate = c.activeRec.Rate()
		res.RecordingMetadata = cloneStringMap(c.recordingMeta)
		res.FramesRecorded = c.activeRec.TotalSteps()
	}
	if c.player != nil {
		res.ReplayName = c.replayName
		res.ReplayFrame = c.player.StepIndex()
		res.ReplayTotalFrames = c.player.TotalSteps()
		if c.player.Done() {
			res.ReplayDone = true
		} else {
			res.Replaying = true
		}
	}
	return res, nil
}

type replayRecordStartArgs struct {
	Name     string            `json:"name"`
	Seed     *uint64           `json:"seed"`
	Actions  []string          `json:"actions"`
	Axes     []string          `json:"axes"`
	Metadata map[string]string `json:"metadata"`
	Reset    *bool             `json:"reset"`
}

func (b *Bridge) handleReplayRecordStart(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[replayRecordStartArgs](raw)
	if err != nil {
		return nil, err
	}
	c, err := b.requireReplay()
	if err != nil {
		return nil, err
	}
	if c.activeRec != nil {
		return nil, ErrAlreadyRecording
	}
	if c.player != nil {
		if !c.player.Done() {
			return nil, fmt.Errorf("mcp: cannot start recording: %w", ErrAlreadyReplaying)
		}
		c.finishReplay()
	}
	if c.cfg.Source == nil {
		return nil, errors.New("mcp: replay controller has no input source configured (set ReplayConfig.Source)")
	}
	doReset, err := b.resolveReplayReset(c, args.Reset)
	if err != nil {
		return nil, err
	}

	var seed uint64
	if args.Seed != nil {
		seed = *args.Seed
	} else if c.cfg.SeedFunc != nil {
		seed = c.cfg.SeedFunc()
	}

	actions := c.cfg.Actions
	if len(args.Actions) > 0 {
		actions = args.Actions
	}
	axes := c.cfg.Axes
	if len(args.Axes) > 0 {
		axes = args.Axes
	}
	rec := replay.NewRecorder(c.cfg.Source, seed, b.replayStepRate(c), actions, axes)

	if doReset {
		if err := b.resetReplay(c, seed); err != nil {
			return nil, fmt.Errorf("mcp: reset before record failed: %w", err)
		}
	}

	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = c.allocateName()
	}
	meta := cloneStringMap(args.Metadata)

	c.activeRec = rec
	c.recordingName = name
	c.recordingMeta = meta
	if c.cfg.OnRecordStart != nil {
		c.cfg.OnRecordStart(name, seed)
	}

	snap := rec.Recording()
	return ReplayRecordStartResult{
		Recording: true,
		Name:      name,
		Seed:      snap.Seed,
		StepRate:  snap.Rate,
		Actions:   snap.Actions,
		Axes:      snap.Axes,
		Metadata:  cloneStringMap(meta),
	}, nil
}

type replayRecordStopArgs struct {
	Name      string `json:"name"`
	SavePath  string `json:"save_path"`
	Overwrite bool   `json:"overwrite"`
}

func (b *Bridge) handleReplayRecordStop(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[replayRecordStopArgs](raw)
	if err != nil {
		return nil, err
	}
	c, err := b.requireReplay()
	if err != nil {
		return nil, err
	}
	if c.activeRec == nil {
		return nil, ErrNotRecording
	}

	var cleanRel string
	if args.SavePath != "" {
		cleanRel, err = resolveSafeSavePath(c.cfg.SaveDir, args.SavePath)
		if err != nil {
			return nil, err
		}
	}

	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = c.recordingName
	}
	if _, exists := c.saved[name]; exists && !args.Overwrite {
		return nil, fmt.Errorf("mcp: a recording named %q is already stored; pass a different name or overwrite: true (still recording)", name)
	}

	rec := c.activeRec.Recording()
	yamlBytes, err := rec.Encode()
	if err != nil {
		return nil, fmt.Errorf("mcp: encode recording: %w", err)
	}

	if cleanRel != "" {
		if err := writeSaveFile(c.cfg.SaveDir, cleanRel, yamlBytes, args.Overwrite); err != nil {
			return nil, err
		}
	}

	meta := cloneStringMap(c.recordingMeta)

	if c.saved == nil {
		c.saved = make(map[string]storedRecording)
	}
	c.saved[name] = storedRecording{
		rec:      rec.Clone(),
		metadata: cloneStringMap(meta),
	}

	c.activeRec = nil
	c.recordingName = ""
	c.recordingMeta = nil

	if c.cfg.OnRecordStop != nil {
		c.cfg.OnRecordStop(name, rec.Clone())
	}

	return ReplayRecordStopResult{
		Name:      name,
		Frames:    rec.TotalSteps,
		Seed:      rec.Seed,
		StepRate:  rec.Rate,
		Events:    len(rec.Events),
		Actions:   slices.Clone(rec.Actions),
		Axes:      slices.Clone(rec.Axes),
		Metadata:  meta,
		SavedPath: filepath.ToSlash(cleanRel),
		YAML:      string(yamlBytes),
	}, nil
}

type replayPlayArgs struct {
	Name     string `json:"name"`
	YAML     string `json:"yaml"`
	RunToEnd bool   `json:"run_to_end"`
	Steps    int    `json:"steps"`
	Reset    *bool  `json:"reset"`
}

func (b *Bridge) handleReplayPlay(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[replayPlayArgs](raw)
	if err != nil {
		return nil, err
	}
	c, err := b.requireReplay()
	if err != nil {
		return nil, err
	}
	if args.Name == "" && args.YAML == "" {
		return nil, errors.New("mcp: either name or yaml is required")
	}
	if args.Name != "" && args.YAML != "" {
		return nil, errors.New("mcp: specify either name or yaml, not both")
	}
	if args.Steps < 0 || args.Steps > MaxLoopSteps {
		return nil, fmt.Errorf("mcp: steps must be between 1 and %d (got %d)", MaxLoopSteps, args.Steps)
	}
	if args.RunToEnd && args.Steps > 0 {
		return nil, errors.New("mcp: cannot specify both run_to_end and steps")
	}
	if c.activeRec != nil {
		return nil, fmt.Errorf("mcp: cannot start replay: %w", ErrAlreadyRecording)
	}
	if c.player != nil {
		if !c.player.Done() {
			return nil, ErrAlreadyReplaying
		}
		c.finishReplay()
	}

	var (
		rec        *replay.Recording
		replayName string
	)
	if args.Name != "" {
		entry, ok := c.saved[args.Name]
		if !ok || entry.rec == nil {
			avail := c.RecordingNames()
			if len(avail) == 0 {
				return nil, fmt.Errorf("mcp: recording %q not found (no recordings saved)", args.Name)
			}
			return nil, fmt.Errorf("mcp: recording %q not found (available: %s)", args.Name, strings.Join(avail, ", "))
		}
		rec = entry.rec.Clone()
		replayName = args.Name
	} else {
		decoded, err := replay.Decode([]byte(args.YAML))
		if err != nil {
			return nil, fmt.Errorf("mcp: invalid replay yaml: %w", err)
		}
		rec = decoded
	}

	if err := rec.Validate(); err != nil {
		return nil, fmt.Errorf("mcp: invalid recording: %w", err)
	}
	// A recording only reproduces its run at the fixed rate it was made at: a different dt per step changes the simulation.
	if rate := b.replayStepRate(c); rec.Rate != rate {
		return nil, fmt.Errorf("mcp: invalid recording: %w", loaderr.Errorf("rate", fmt.Sprintf("record again at the game's fixed rate of %d steps per second, or run the game at %d", rate, rec.Rate), "recording rate %d differs from the game's fixed step rate %d", rec.Rate, rate))
	}
	if args.RunToEnd && rec.TotalSteps > MaxLoopSteps {
		return nil, fmt.Errorf("mcp: run_to_end is limited to %d steps but the recording has %d; omit run_to_end to play it frame by frame and advance it with game_loop_step or the running game", MaxLoopSteps, rec.TotalSteps)
	}

	var advance func(n int)
	if args.RunToEnd || args.Steps > 0 {
		advance = b.replayAdvancer(c, rec.StepDuration())
		if advance == nil {
			return nil, errors.New("mcp: advancing replay frames requires ReplayConfig.StepFunc or a game loop (Config.Loop or Config.LoopFunc)")
		}
	}

	doReset, err := b.resolveReplayReset(c, args.Reset)
	if err != nil {
		return nil, err
	}

	player := replay.NewPlayer(rec)
	c.player = player
	c.replayName = replayName
	c.trailing = false

	if doReset {
		if err := b.resetReplay(c, rec.Seed); err != nil {
			c.player = nil
			c.replayName = ""
			return nil, fmt.Errorf("mcp: reset before replay failed: %w", err)
		}
	}
	if c.cfg.OnReplayStart != nil {
		c.cfg.OnReplayStart(rec.Clone())
	}

	switch {
	case args.RunToEnd:
		advance(rec.TotalSteps)
	case args.Steps > 0:
		advance(min(args.Steps, rec.TotalSteps))
	}
	// A completed player stays active until the game's next Step runs its trailing step, which releases actions held at the end of the recording.
	completed := player.Done()
	return ReplayPlayResult{
		Name:              replayName,
		Seed:              rec.Seed,
		StepRate:          rec.Rate,
		Replaying:         c.player == player && !completed,
		Completed:         completed,
		FramesPlayed:      player.StepIndex(),
		ReplayTotalFrames: rec.TotalSteps,
	}, nil
}

// replayAdvancer returns a function that advances the simulation by n recorded frames: through ReplayConfig.StepFunc when set, otherwise through the Bridge's game loop with Loop.StepNow so the loop's step count, game time and Draw stay in sync. It returns nil when neither is configured.
func (b *Bridge) replayAdvancer(c *ReplayController, dt time.Duration) func(n int) {
	if c.cfg.StepFunc != nil {
		step := c.cfg.StepFunc
		return func(n int) {
			player := c.player
			for range n {
				if c.player != player || player.Done() {
					return
				}
				step(dt)
			}
		}
	}
	if l := b.activeLoop(); l != nil {
		return func(n int) {
			if n > 0 {
				l.StepNow(n)
			}
		}
	}
	return nil
}

func (b *Bridge) handleReplayStop(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	c, err := b.requireReplay()
	if err != nil {
		return nil, err
	}
	if c.player == nil {
		return nil, ErrNotReplaying
	}
	if c.player.Done() {
		c.finishReplay()
		return nil, ErrNotReplaying
	}
	res := ReplayStopResult{
		Stopped:           true,
		Name:              c.replayName,
		FramesPlayed:      c.player.StepIndex(),
		ReplayTotalFrames: c.player.TotalSteps(),
	}
	c.finishReplay()
	return res, nil
}

func (b *Bridge) handleReplayList(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	c, err := b.requireReplay()
	if err != nil {
		return nil, err
	}
	names := c.RecordingNames()
	out := ReplayListResult{
		Count:      len(names),
		Recordings: make([]ReplaySummary, 0, len(names)),
	}
	for _, name := range names {
		entry := c.saved[name]
		if entry.rec == nil {
			continue
		}
		out.Recordings = append(out.Recordings, ReplaySummary{
			Name:     name,
			Frames:   entry.rec.TotalSteps,
			Seed:     entry.rec.Seed,
			StepRate: entry.rec.Rate,
			Events:   len(entry.rec.Events),
			Actions:  slices.Clone(entry.rec.Actions),
			Axes:     slices.Clone(entry.rec.Axes),
			Metadata: cloneStringMap(entry.metadata),
		})
	}
	return out, nil
}

func cloneStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return maps.Clone(m)
}

// resolveSafeSavePath checks that rel is a relative path inside saveDir and returns it cleaned. The write itself goes through os.Root (writeSaveFile), which also refuses symbolic links that leave saveDir.
func resolveSafeSavePath(saveDir, rel string) (cleanRel string, err error) {
	if strings.TrimSpace(saveDir) == "" {
		return "", errors.New("mcp: save_path requires ReplayConfig.SaveDir to be configured")
	}
	if strings.TrimSpace(rel) == "" {
		return "", errors.New("mcp: save_path must not be empty")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("mcp: save_path %q must be relative to SaveDir", rel)
	}
	cleaned := filepath.Clean(rel)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("mcp: save_path %q escapes SaveDir", rel)
	}
	return cleaned, nil
}

// writeSaveFile writes data to rel inside saveDir through os.Root, creating saveDir and any parent directories. Without overwrite, an existing file is refused.
func writeSaveFile(saveDir, rel string, data []byte, overwrite bool) error {
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		return fmt.Errorf("mcp: create SaveDir: %w", err)
	}
	root, err := os.OpenRoot(saveDir)
	if err != nil {
		return fmt.Errorf("mcp: open SaveDir: %w", err)
	}
	defer root.Close()
	if dir := filepath.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mcp: create recording directory for %q: %w", filepath.ToSlash(rel), err)
		}
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !overwrite {
		flags |= os.O_EXCL
	}
	f, err := root.OpenFile(rel, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("mcp: save_path %q already exists; pass overwrite: true to replace it (still recording)", filepath.ToSlash(rel))
		}
		return fmt.Errorf("mcp: write recording %q: %w", filepath.ToSlash(rel), err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("mcp: write recording %q: %w", filepath.ToSlash(rel), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("mcp: write recording %q: %w", filepath.ToSlash(rel), err)
	}
	return nil
}
