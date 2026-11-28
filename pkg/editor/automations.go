package editor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

const (
	automationDailyTickInterval       = 30 * time.Second
	automationMaxCheckpointFiles      = 256
	automationMaxCheckpointBytes      = 2 << 20  // 2 MiB per file
	automationMaxCheckpointTotalBytes = 16 << 20 // 16 MiB total per checkpoint
	automationMaxRetainedCheckpoints  = 20
	automationMaxConcurrentRuns       = 4
	automationMaxPendingRuns          = 32
	automationRunTimeout              = 10 * time.Minute
	automationDraftTimeout            = 30 * time.Second
	automationMaxStepsPerTurn         = 128
	automationMaxRunHistoryPerRule    = 1024
	automationWeeklyWindow            = 7 * 24 * time.Hour
)

// ErrAutomationYouOnly is returned when an agent actor or automation attempts to apply a You-only draft, keep/dismiss its own review, mutate automations.yaml, or restore a checkpoint.
var ErrAutomationYouOnly = errors.New("editor: automations cannot apply You-only drafts or modify automation settings")

// AgentReviewState tracks the review lifecycle of an automation (or background) turn in the Agent panel.
type AgentReviewState string

const (
	// AgentReviewNone is the default for ordinary interactive chat turns.
	AgentReviewNone AgentReviewState = ""
	// AgentReviewRunning indicates a background automation task is currently executing.
	AgentReviewRunning AgentReviewState = "running"
	// AgentReviewNeedsYou indicates the background task is waiting on a permission prompt or user question.
	AgentReviewNeedsYou AgentReviewState = "needs_you"
	// AgentReviewReady indicates the automation finished and its result is in Ready to review (never unreviewed).
	AgentReviewReady AgentReviewState = "ready_to_review"
	// AgentReviewKept indicates the author reviewed and kept the automation's result.
	AgentReviewKept AgentReviewState = "kept"
	// AgentReviewDismissed indicates the author dismissed the Ready-to-review result (reverting nothing that was never kept).
	AgentReviewDismissed AgentReviewState = "dismissed"
	// AgentReviewFailed indicates the automation failed or was blocked by permissions.
	AgentReviewFailed AgentReviewState = "failed"
)

// AutomationCheckpoint records a pre-run snapshot captured before an automation task starts.
// When a project directory exists, snapshots are also written under .nsgo/checkpoints/<ID>/ (ignored by git).
type AutomationCheckpoint struct {
	ID           string
	AutomationID string
	Trigger      string
	Label        string
	CreatedAt    time.Time
	Dir          string
	Files        map[string][]byte
}

// AutomationDraft holds a candidate AutomationRule drafted from natural language ("+ New automation, in words")
// waiting for the author to keep or discard.
type AutomationDraft struct {
	Words string
	Rule  AutomationRule
	YAML  string
}

type automationLocalStateDoc struct {
	PausedDate   string              `yaml:"pausedDate,omitempty"`
	LastDailyRun map[string]string   `yaml:"lastDailyRun,omitempty"`
	RunHistory   map[string][]string `yaml:"runHistory,omitempty"`
}

type automationCheckpointManifest struct {
	ID           string   `yaml:"id"`
	AutomationID string   `yaml:"automationId"`
	Trigger      string   `yaml:"trigger"`
	Label        string   `yaml:"label"`
	CreatedAt    string   `yaml:"createdAt"`
	Files        []string `yaml:"files,omitempty"`
}

type automationRuntimeState struct {
	mu      sync.Mutex
	file    *AutomationsFile
	loadErr error
	// fileDigest is the SHA-256 (lowercase hex) of the project's automations.yaml as last loaded or saved, or "" when the project has no such file.
	fileDigest string
	// held is true while the project's automations.yaml has enabled rules the person has not allowed on this computer (ApprovedAutomations in editor.yaml); no automation starts while it is set.
	held          bool
	pausedDate    string // "YYYY-MM-DD" in editor clock location; empty when not paused
	lastDailyRun  map[string]string
	runHistory    map[string][]time.Time
	checkpoints   []AutomationCheckpoint
	checkpointSeq int

	activeRuns     map[string]context.CancelFunc
	activeRunCount atomic.Int32
	runGen         uint64
	sem            chan struct{}

	draftOpen bool
	draftText string
	draft     *AutomationDraft
	draftErr  string

	schedulerStop func()
}

func (a *automationRuntimeState) initDefaults() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.file = DefaultAutomations()
	a.lastDailyRun = make(map[string]string)
	a.runHistory = make(map[string][]time.Time)
	a.activeRuns = make(map[string]context.CancelFunc)
	a.sem = make(chan struct{}, automationMaxConcurrentRuns)
}

func (a *automationRuntimeState) ensureInitLocked() {
	if a.file == nil {
		a.file = DefaultAutomations()
	}
	if a.lastDailyRun == nil {
		a.lastDailyRun = make(map[string]string)
	}
	if a.runHistory == nil {
		a.runHistory = make(map[string][]time.Time)
	}
	if a.activeRuns == nil {
		a.activeRuns = make(map[string]context.CancelFunc)
	}
	if a.sem == nil {
		a.sem = make(chan struct{}, automationMaxConcurrentRuns)
	}
}

func (e *Editor) ensureAutomationsInit() {
	if e == nil {
		return
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	e.automations.mu.Unlock()
}

func (e *Editor) stopAutomationRunsAndScheduler() {
	if e == nil {
		return
	}
	e.automations.mu.Lock()
	stopSched := e.automations.schedulerStop
	e.automations.schedulerStop = nil
	e.automations.runGen++
	var cancels []context.CancelFunc
	for id, cancel := range e.automations.activeRuns {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		delete(e.automations.activeRuns, id)
	}
	e.automations.activeRunCount.Store(0)
	e.automations.mu.Unlock()

	if stopSched != nil {
		stopSched()
	}
	for _, cancel := range cancels {
		cancel()
	}
}

func (e *Editor) resetAutomationsForProject() {
	if e == nil {
		return
	}
	e.stopAutomationRunsAndScheduler()
	e.automations.mu.Lock()
	e.automations.file = DefaultAutomations()
	e.automations.loadErr = nil
	e.automations.fileDigest = ""
	e.automations.held = false
	e.automations.pausedDate = ""
	e.automations.lastDailyRun = make(map[string]string)
	e.automations.runHistory = make(map[string][]time.Time)
	e.automations.checkpoints = nil
	e.automations.checkpointSeq = 0
	e.automations.draftOpen = false
	e.automations.draftText = ""
	e.automations.draft = nil
	e.automations.draftErr = ""
	e.automations.mu.Unlock()
}

func (e *Editor) loadAutomationsForOpenProject() {
	if e == nil {
		return
	}
	e.resetAutomationsForProject()
	root := e.automationsProjectRoot()
	if root == "" {
		return
	}
	autoPath := filepath.Join(root, AutomationsFileName)
	var (
		f      *AutomationsFile
		err    error
		digest string
	)
	if !isAutomationPathWithinRoot(root, autoPath) {
		err = fmt.Errorf("editor: %s resolves outside project root", AutomationsFileName)
	} else {
		f, err = LoadAutomationsFile(autoPath)
		if err == nil {
			digest, err = automationsFileDigest(autoPath)
		}
	}
	held := err == nil && automationsHaveEnabledRule(f) && !e.automationsApproved(root, digest)
	e.automations.mu.Lock()
	switch {
	case err == nil:
		e.automations.file = f
		e.automations.fileDigest = digest
		e.automations.held = held
	case errors.Is(err, fs.ErrNotExist):
		e.automations.file = DefaultAutomations()
	default:
		e.automations.file = DefaultAutomations()
		e.automations.loadErr = err
	}
	e.automations.mu.Unlock()
	e.loadAutomationLocalState(root)
	// Restart the daily scheduler if a UI scope is already attached (resetAutomationsForProject stopped it).
	e.startAutomationScheduler()
}

// automationsFileDigest returns the lowercase hex SHA-256 of the automations file at path.
func automationsFileDigest(path string) (string, error) {
	fp, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer fp.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(fp, maxAutomationsFileBytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func automationsHaveEnabledRule(f *AutomationsFile) bool {
	if f == nil {
		return false
	}
	for _, r := range f.Automations {
		if r.Enabled {
			return true
		}
	}
	return false
}

// automationsApproved reports whether the person allowed the automations.yaml with digest in the project at root on this computer.
func (e *Editor) automationsApproved(root, digest string) bool {
	if digest == "" {
		return false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	if e.settings.Agent == nil {
		return false
	}
	for _, ap := range e.settings.Agent.ApprovedAutomations {
		if ap.Project == abs {
			return ap.SHA256 == digest
		}
	}
	return false
}

// recordAutomationsApproval stores in editor.yaml that the person allowed the project's automations.yaml with digest, replacing any earlier entry for the project.
func (e *Editor) recordAutomationsApproval(root, digest string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	e.settingsMu.Lock()
	a := e.ensureAgentSettings()
	replaced := false
	for i := range a.ApprovedAutomations {
		if a.ApprovedAutomations[i].Project == abs {
			a.ApprovedAutomations[i].SHA256 = digest
			replaced = true
			break
		}
	}
	if !replaced {
		a.ApprovedAutomations = append(a.ApprovedAutomations, ApprovedAutomations{Project: abs, SHA256: digest})
		slices.SortFunc(a.ApprovedAutomations, func(x, y ApprovedAutomations) int {
			return strings.Compare(x.Project, y.Project)
		})
	}
	e.settingsMu.Unlock()
	return e.persistEditorSettings()
}

// AutomationsHeld reports whether the open project's automations.yaml has enabled rules that stay off on this computer until the person allows them (AllowProjectAutomations). A project's file is held when it first arrives with a clone, template or pack and whenever it changes outside the editor, for example after a git pull.
func (e *Editor) AutomationsHeld() bool {
	if e == nil {
		return false
	}
	e.automations.mu.Lock()
	defer e.automations.mu.Unlock()
	return e.automations.held
}

// AllowProjectAutomations lets the open project's automations.yaml, as it is now, run on this computer and records that in editor.yaml. Refuses agent actors (ErrAutomationYouOnly).
func (e *Editor) AllowProjectAutomations() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	root := e.automationsProjectRoot()
	e.automations.mu.Lock()
	digest := e.automations.fileDigest
	e.automations.mu.Unlock()
	if root == "" || digest == "" {
		return nil
	}
	if err := e.recordAutomationsApproval(root, digest); err != nil {
		return err
	}
	e.automations.mu.Lock()
	if e.automations.fileDigest == digest {
		e.automations.held = false
	}
	e.automations.mu.Unlock()
	e.notify()
	return nil
}

func (e *Editor) automationsProjectRoot() string {
	if e == nil {
		return ""
	}
	if e.Project != nil && e.ProjectPath != "" {
		return filepath.Dir(e.ProjectPath)
	}
	return ""
}

// AutomationsPath returns the OS path of the open project's automations.yaml, or empty without an open project.
func (e *Editor) AutomationsPath() string {
	root := e.automationsProjectRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, AutomationsFileName)
}

// AutomationsLoadError returns any parse/validation error encountered when loading the open project's automations.yaml.
func (e *Editor) AutomationsLoadError() error {
	if e == nil {
		return nil
	}
	e.automations.mu.Lock()
	err := e.automations.loadErr
	e.automations.mu.Unlock()
	return err
}

// Automations returns a deep copy of the active project's AutomationsFile.
func (e *Editor) Automations() *AutomationsFile {
	if e == nil {
		return DefaultAutomations()
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	cp := &AutomationsFile{
		Version:     e.automations.file.Version,
		Automations: slices.Clone(e.automations.file.Automations),
	}
	e.automations.mu.Unlock()
	return cp
}

// EnabledAutomationsCount returns how many automations are currently enabled.
func (e *Editor) EnabledAutomationsCount() int {
	if e == nil {
		return 0
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	n := 0
	for _, r := range e.automations.file.Automations {
		if r.Enabled {
			n++
		}
	}
	e.automations.mu.Unlock()
	return n
}

// SetAutomations replaces the open project's automation list and writes automations.yaml when a project is open.
// Refuses agent actors (ErrAutomationYouOnly).
func (e *Editor) SetAutomations(f *AutomationsFile) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	if err := ValidateAutomations(f); err != nil {
		return err
	}
	next := &AutomationsFile{
		Version:     f.Version,
		Automations: slices.Clone(f.Automations),
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	prev := e.automations.file
	prevErr := e.automations.loadErr
	e.automations.file = next
	e.automations.loadErr = nil
	e.automations.mu.Unlock()

	if err := e.saveAutomationsIfProjectOpen(next); err != nil {
		e.automations.mu.Lock()
		e.automations.file = prev
		e.automations.loadErr = prevErr
		e.automations.mu.Unlock()
		return err
	}
	e.notify()
	return nil
}

// SetAutomationEnabled toggles the enabled state of the automation with id and writes automations.yaml when a project is open.
// Refuses agent actors (ErrAutomationYouOnly).
func (e *Editor) SetAutomationEnabled(id string, enabled bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	idx := -1
	for i := range e.automations.file.Automations {
		if e.automations.file.Automations[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		e.automations.mu.Unlock()
		return fmt.Errorf("editor: automation %q not found", id)
	}
	if e.automations.file.Automations[idx].Enabled == enabled {
		e.automations.mu.Unlock()
		return nil
	}
	prev := e.automations.file
	next := &AutomationsFile{
		Version:     prev.Version,
		Automations: slices.Clone(prev.Automations),
	}
	next.Automations[idx].Enabled = enabled
	e.automations.file = next
	e.automations.mu.Unlock()

	if err := e.saveAutomationsIfProjectOpen(next); err != nil {
		e.automations.mu.Lock()
		e.automations.file = prev
		e.automations.mu.Unlock()
		return err
	}
	e.notify()
	return nil
}

// AddAutomationRule validates and appends rule to automations.yaml.
// Refuses agent actors (ErrAutomationYouOnly); agents must draft entries via DraftAutomationFromWords for the author to keep.
func (e *Editor) AddAutomationRule(rule AutomationRule) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	rule.ID = uniqueAutomationIDLocked(e.automations.file.Automations, rule.ID)
	prev := e.automations.file
	prevErr := e.automations.loadErr
	candidate := &AutomationsFile{
		Version:     AutomationsVersion,
		Automations: append(slices.Clone(prev.Automations), rule),
	}
	if err := ValidateAutomations(candidate); err != nil {
		e.automations.mu.Unlock()
		return err
	}
	e.automations.file = candidate
	e.automations.loadErr = nil
	e.automations.mu.Unlock()

	if err := e.saveAutomationsIfProjectOpen(candidate); err != nil {
		e.automations.mu.Lock()
		e.automations.file = prev
		e.automations.loadErr = prevErr
		e.automations.mu.Unlock()
		return err
	}
	e.notify()
	return nil
}

// RemoveAutomationRule removes the automation with id and saves automations.yaml.
// Refuses agent actors (ErrAutomationYouOnly).
func (e *Editor) RemoveAutomationRule(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	idx := -1
	for i := range e.automations.file.Automations {
		if e.automations.file.Automations[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		e.automations.mu.Unlock()
		return fmt.Errorf("editor: automation %q not found", id)
	}
	prev := e.automations.file
	nextList := make([]AutomationRule, 0, len(prev.Automations)-1)
	nextList = append(nextList, prev.Automations[:idx]...)
	nextList = append(nextList, prev.Automations[idx+1:]...)
	next := &AutomationsFile{
		Version:     prev.Version,
		Automations: nextList,
	}
	e.automations.file = next
	e.automations.mu.Unlock()

	if err := e.saveAutomationsIfProjectOpen(next); err != nil {
		e.automations.mu.Lock()
		e.automations.file = prev
		e.automations.mu.Unlock()
		return err
	}
	e.notify()
	return nil
}

func (e *Editor) saveAutomationsIfProjectOpen(f *AutomationsFile) error {
	root := e.automationsProjectRoot()
	if root == "" {
		return nil
	}
	p := filepath.Join(root, AutomationsFileName)
	if !isAutomationPathWithinRoot(root, p) {
		return fmt.Errorf("editor: %s resolves outside project root", AutomationsFileName)
	}
	if err := SaveAutomationsFile(p, f); err != nil {
		return err
	}
	e.filesRev++
	e.snapshotProjectTree()
	// Only the person saves automations.yaml through the editor (agents are refused above), so a save keeps an allowed file allowed. A held file stays held, so editing one rule never silently allows the others.
	digest, err := automationsFileDigest(p)
	if err != nil {
		return err
	}
	e.automations.mu.Lock()
	wasHeld := e.automations.held
	e.automations.fileDigest = digest
	e.automations.held = wasHeld && automationsHaveEnabledRule(f)
	e.automations.mu.Unlock()
	if !wasHeld {
		return e.recordAutomationsApproval(root, digest)
	}
	return nil
}

func uniqueAutomationIDLocked(rules []AutomationRule, raw string) string {
	base := slugifyAutomationID(raw)
	used := make(map[string]bool, len(rules))
	for _, r := range rules {
		used[r.ID] = true
	}
	if !used[base] {
		return base
	}
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s-%d", base, n)
		if !used[cand] {
			return cand
		}
	}
}

// PauseAutomationsForToday pauses every automation trigger until the next calendar day (relative to Editor.Now / time.Now).
// Refuses agent actors (ErrAutomationYouOnly).
func (e *Editor) PauseAutomationsForToday() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	today := e.nowTime().Format("2006-01-02")
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	e.automations.pausedDate = today
	e.automations.mu.Unlock()
	e.saveAutomationLocalStateQuietly()
	e.notify()
	return nil
}

// ResumeAutomations clears any active pause-for-today state so triggers fire normally again.
// Refuses agent actors (ErrAutomationYouOnly).
func (e *Editor) ResumeAutomations() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	if e.automations.pausedDate == "" {
		e.automations.mu.Unlock()
		return nil
	}
	e.automations.pausedDate = ""
	e.automations.mu.Unlock()
	e.saveAutomationLocalStateQuietly()
	e.notify()
	return nil
}

// AutomationsPausedToday reports whether automations are currently paused for today.
// When the clock advances to a later calendar day, AutomationsPausedToday returns false automatically.
func (e *Editor) AutomationsPausedToday() bool {
	if e == nil {
		return false
	}
	e.automations.mu.Lock()
	paused := e.automations.pausedDate
	e.automations.mu.Unlock()
	if paused == "" {
		return false
	}
	today := e.nowTime().Format("2006-01-02")
	return today <= paused
}

// AutomationWeeklyRuns returns how many times the automation with id ran during the last 7 days.
func (e *Editor) AutomationWeeklyRuns(id string) int {
	if e == nil {
		return 0
	}
	cutoff := e.nowTime().Add(-automationWeeklyWindow)
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	n := 0
	for _, ts := range e.automations.runHistory[id] {
		if !ts.Before(cutoff) {
			n++
		}
	}
	e.automations.mu.Unlock()
	return n
}

func (e *Editor) recordAutomationRunTimestamp(id string, at time.Time) {
	cutoff := at.Add(-automationWeeklyWindow)
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	prev := e.automations.runHistory[id]
	kept := make([]time.Time, 0, len(prev)+1)
	for _, ts := range prev {
		if !ts.Before(cutoff) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, at)
	if len(kept) > automationMaxRunHistoryPerRule {
		kept = kept[len(kept)-automationMaxRunHistoryPerRule:]
	}
	e.automations.runHistory[id] = kept
	e.automations.mu.Unlock()
	e.saveAutomationLocalStateQuietly()
}

func (e *Editor) loadAutomationLocalState(root string) {
	p := filepath.Join(root, filepath.FromSlash(AutomationsStateFile))
	if !isAutomationPathWithinRoot(root, p) {
		return
	}
	fp, err := os.Open(p)
	if err != nil {
		return
	}
	defer fp.Close()
	data, err := io.ReadAll(io.LimitReader(fp, maxAutomationsFileBytes+1))
	if err != nil || len(data) > maxAutomationsFileBytes {
		return
	}
	var doc automationLocalStateDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return
	}
	cutoff := e.nowTime().Add(-automationWeeklyWindow)
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	e.automations.pausedDate = strings.TrimSpace(doc.PausedDate)
	if doc.LastDailyRun != nil {
		e.automations.lastDailyRun = doc.LastDailyRun
	}
	for id, list := range doc.RunHistory {
		var parsed []time.Time
		for _, raw := range list {
			if ts, err := time.Parse(time.RFC3339, raw); err == nil && !ts.Before(cutoff) {
				parsed = append(parsed, ts)
			}
		}
		if len(parsed) > automationMaxRunHistoryPerRule {
			parsed = parsed[len(parsed)-automationMaxRunHistoryPerRule:]
		}
		if len(parsed) > 0 {
			e.automations.runHistory[id] = parsed
		}
	}
	e.automations.mu.Unlock()
}

func (e *Editor) saveAutomationLocalStateQuietly() {
	root := e.automationsProjectRoot()
	if root == "" {
		return
	}
	p := filepath.Join(root, filepath.FromSlash(AutomationsStateFile))
	if !isAutomationPathWithinRoot(root, p) {
		return
	}
	now := e.nowTime()
	cutoff := now.Add(-automationWeeklyWindow)

	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	doc := automationLocalStateDoc{
		PausedDate: e.automations.pausedDate,
	}
	if len(e.automations.lastDailyRun) > 0 {
		doc.LastDailyRun = make(map[string]string, len(e.automations.lastDailyRun))
		for k, v := range e.automations.lastDailyRun {
			doc.LastDailyRun[k] = v
		}
	}
	if len(e.automations.runHistory) > 0 {
		doc.RunHistory = make(map[string][]string, len(e.automations.runHistory))
		for id, list := range e.automations.runHistory {
			var enc []string
			for _, ts := range list {
				if !ts.Before(cutoff) {
					enc = append(enc, ts.UTC().Format(time.RFC3339))
				}
			}
			if len(enc) > 0 {
				doc.RunHistory[id] = enc
			}
		}
	}
	e.automations.mu.Unlock()

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return
	}
	if err := enc.Close(); err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = writeFileAtomic(p, buf.Bytes())
}

// --- Natural-language drafting ("+ New automation, in words") ---

// AutomationDraftOpen reports whether the "+ New automation, in words" form is open in Settings › Automations.
func (e *Editor) AutomationDraftOpen() bool {
	if e == nil {
		return false
	}
	e.automations.mu.Lock()
	open := e.automations.draftOpen
	e.automations.mu.Unlock()
	return open
}

// AutomationDraftText returns the current natural-language input in "+ New automation, in words".
func (e *Editor) AutomationDraftText() string {
	if e == nil {
		return ""
	}
	e.automations.mu.Lock()
	text := e.automations.draftText
	e.automations.mu.Unlock()
	return text
}

// AutomationDraftError returns any validation/drafting error in "+ New automation, in words".
func (e *Editor) AutomationDraftError() string {
	if e == nil {
		return ""
	}
	e.automations.mu.Lock()
	errMsg := e.automations.draftErr
	e.automations.mu.Unlock()
	return errMsg
}

// PendingAutomationDraft returns a copy of the currently drafted automation waiting to be kept, or nil.
func (e *Editor) PendingAutomationDraft() *AutomationDraft {
	if e == nil {
		return nil
	}
	e.automations.mu.Lock()
	d := e.automations.draft
	if d == nil {
		e.automations.mu.Unlock()
		return nil
	}
	cp := *d
	e.automations.mu.Unlock()
	return &cp
}

// BeginNewAutomationDraft opens the "+ New automation, in words" composer in Settings › Automations.
func (e *Editor) BeginNewAutomationDraft() {
	if e == nil {
		return
	}
	e.automations.mu.Lock()
	e.automations.draftOpen = true
	e.automations.draftErr = ""
	e.automations.mu.Unlock()
	e.notify()
}

// SetAutomationDraftText updates the natural-language description in "+ New automation, in words".
func (e *Editor) SetAutomationDraftText(text string) {
	if e == nil {
		return
	}
	e.automations.mu.Lock()
	if e.automations.draftText == text {
		e.automations.mu.Unlock()
		return
	}
	e.automations.draftText = text
	e.automations.draftErr = ""
	e.automations.mu.Unlock()
	e.notify()
}

// CancelNewAutomationDraft closes and clears the pending "+ New automation, in words" draft.
func (e *Editor) CancelNewAutomationDraft() {
	if e == nil {
		return
	}
	e.automations.mu.Lock()
	e.automations.draftOpen = false
	e.automations.draftText = ""
	e.automations.draft = nil
	e.automations.draftErr = ""
	e.automations.mu.Unlock()
	e.notify()
}

// DraftAutomationFromWords asks the agent/drafter to turn plain words into a reviewable AutomationRule draft.
// The draft is never written to automations.yaml until the author calls KeepAutomationDraft.
func (e *Editor) DraftAutomationFromWords(words string) (*AutomationDraft, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	e.ensureAutomationsInit()
	trimmed := strings.TrimSpace(words)
	if trimmed == "" {
		trimmed = strings.TrimSpace(e.AutomationDraftText())
	}
	if trimmed == "" {
		e.automations.mu.Lock()
		e.automations.draftErr = e.T("settings.automations.draftEmptyError")
		e.automations.mu.Unlock()
		e.notify()
		return nil, errors.New("editor: empty automation description")
	}
	e.automations.mu.Lock()
	e.automations.draftOpen = true
	e.automations.draftText = trimmed
	e.automations.draftErr = ""
	e.automations.mu.Unlock()

	var (
		rule AutomationRule
		err  error
	)
	if e.AutomationDrafter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), automationDraftTimeout)
		rule, err = e.AutomationDrafter(ctx, trimmed)
		cancel()
	} else {
		rule = defaultDraftAutomationFromWords(trimmed)
	}
	if err != nil {
		e.automations.mu.Lock()
		e.automations.draftErr = err.Error()
		e.automations.mu.Unlock()
		e.notify()
		return nil, err
	}
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	rule.ID = uniqueAutomationIDLocked(e.automations.file.Automations, rule.ID)
	e.automations.mu.Unlock()
	if err := validateAutomationRule(rule, "automations[0]"); err != nil {
		e.automations.mu.Lock()
		e.automations.draftErr = err.Error()
		e.automations.mu.Unlock()
		e.notify()
		return nil, err
	}
	previewDoc := &AutomationsFile{
		Version:     AutomationsVersion,
		Automations: []AutomationRule{rule},
	}
	enc, err := EncodeAutomations(previewDoc)
	if err != nil {
		e.automations.mu.Lock()
		e.automations.draftErr = err.Error()
		e.automations.mu.Unlock()
		e.notify()
		return nil, err
	}
	draft := &AutomationDraft{
		Words: trimmed,
		Rule:  rule,
		YAML:  string(enc),
	}
	e.automations.mu.Lock()
	e.automations.draft = draft
	e.automations.mu.Unlock()
	e.notify()
	cp := *draft
	return &cp, nil
}

// KeepAutomationDraft commits the pending natural-language automation draft into automations.yaml.
// Refuses agent actors (ErrAutomationYouOnly) so an automation can never apply a You-only draft.
func (e *Editor) KeepAutomationDraft() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	e.automations.mu.Lock()
	if e.automations.draft == nil {
		e.automations.mu.Unlock()
		return errors.New("editor: no pending automation draft")
	}
	rule := e.automations.draft.Rule
	e.automations.mu.Unlock()
	if err := e.AddAutomationRule(rule); err != nil {
		return err
	}
	e.automations.mu.Lock()
	e.automations.draft = nil
	e.automations.draftOpen = false
	e.automations.draftText = ""
	e.automations.draftErr = ""
	e.automations.mu.Unlock()
	e.notify()
	return nil
}

var (
	reClockTime = regexp.MustCompile(`\b([01]?\d|2[0-3]):([0-5]\d)\b`)
	reGlobToken = regexp.MustCompile(`([a-zA-Z0-9_./*-]+(?:\*|\.yaml|\.yml|\.go)[a-zA-Z0-9_./*-]*)`)
)

// defaultDraftAutomationFromWords turns an English or Chinese plain-language request into a valid AutomationRule.
func defaultDraftAutomationFromWords(words string) AutomationRule {
	clean := strings.TrimSpace(words)
	lower := strings.ToLower(clean)

	// Strip optional leading "add:" / "添加：" prefix (matching the suggestion chip in Settings.dc.html).
	for _, pfx := range []string{"add:", "add ", "添加：", "添加:"} {
		if strings.HasPrefix(lower, pfx) && (pfx != "add " || strings.Contains(lower, "before export") || strings.Contains(lower, "when ")) {
			clean = strings.TrimSpace(clean[len(pfx):])
			lower = strings.ToLower(clean)
			break
		}
	}

	// Split WHEN clause and instruction clause on comma, "->", "→", "then", or "，".
	whenPart, thenPart := splitAutomationWords(clean)
	if thenPart == "" {
		thenPart = clean
	}

	tr := AutomationTrigger{}
	name := ""

	switch {
	case strings.Contains(lower, "before export") || strings.Contains(lower, "before build") ||
		(strings.Contains(lower, "before ") && (strings.Contains(lower, "export") || strings.Contains(lower, "build"))) ||
		strings.Contains(clean, "导出前") || strings.Contains(clean, "构建前"):
		tr.Event = AutomationTriggerBeforeExport
		name = "Before export"
		switch {
		case strings.Contains(lower, "ios"):
			tr.Platform = "ios"
			name = "Before iOS export"
		case strings.Contains(lower, "android"):
			tr.Platform = "android"
			name = "Before Android export"
		case strings.Contains(lower, "web") || strings.Contains(lower, "wasm"):
			tr.Platform = "web"
			name = "Before web export"
		case strings.Contains(lower, "macos"):
			tr.Platform = "macos"
			name = "Before macOS export"
		case strings.Contains(lower, "windows"):
			tr.Platform = "windows"
			name = "Before Windows export"
		case strings.Contains(lower, "linux"):
			tr.Platform = "linux"
			name = "Before Linux export"
		}

	case strings.Contains(lower, "every night") || strings.Contains(lower, "nightly") || strings.Contains(lower, "every day") || strings.Contains(lower, "daily") || strings.Contains(clean, "每晚") || strings.Contains(clean, "每天") || reClockTime.MatchString(lower):
		tr.Event = AutomationTriggerDaily
		clock := "02:00"
		if strings.Contains(lower, "every morning") || strings.Contains(clean, "每天早上") {
			clock = "09:00"
		}
		if m := reClockTime.FindStringSubmatch(lower); len(m) == 3 {
			h, _ := strconv.Atoi(m[1])
			clock = fmt.Sprintf("%02d:%s", h, m[2])
		}
		tr.Time = clock
		if strings.Contains(lower, "night") || strings.Contains(clean, "每晚") {
			name = "Every night"
		} else {
			name = "Every day at " + clock
		}

	case strings.Contains(lower, "keep a review") || strings.Contains(lower, "review is kept") || strings.Contains(lower, "after review") || strings.Contains(clean, "保留审查") || strings.Contains(clean, "审查通过"):
		tr.Event = AutomationTriggerReviewKept
		name = "You keep a review"
		if g := extractGlobFromWords(whenPart); g != "" {
			tr.Glob = g
		}

	case strings.Contains(lower, "is added") || strings.Contains(lower, "new prefab") || strings.Contains(lower, "new scene") || strings.Contains(lower, "new tileset") || strings.Contains(lower, "new cutscene") || strings.Contains(lower, "new material") || strings.Contains(clean, "新增") || strings.Contains(clean, "新建"):
		tr.Event = AutomationTriggerFileAdded
		tr.Kind = detectFileKindFromWords(lower, clean)
		if tr.Kind == AutomationFileKindAny {
			name = "A new file is added"
		} else {
			name = "A new " + string(tr.Kind) + " is added"
		}
		if g := extractGlobFromWords(whenPart); g != "" {
			tr.Glob = g
		}

	default:
		tr.Event = AutomationTriggerFileChanged
		glob := extractGlobFromWords(whenPart)
		if glob == "" {
			glob = extractGlobFromWords(clean)
		}
		if glob == "" {
			switch {
			case strings.Contains(lower, "level") || strings.Contains(clean, "关卡"):
				glob = "scenes/level_*.scene.yaml"
				name = "A level file changes"
			case strings.Contains(lower, "prefab") || strings.Contains(clean, "预制体"):
				glob = "prefabs/**/*.prefab.yaml"
				name = "A prefab changes"
			case strings.Contains(lower, "scene") || strings.Contains(clean, "场景"):
				glob = "scenes/**/*.scene.yaml"
				name = "A scene file changes"
			case strings.Contains(lower, "locale") || strings.Contains(lower, "string") || strings.Contains(clean, "本地化"):
				glob = "locales/**/*.yaml"
				name = "A locale file changes"
			default:
				glob = "scenes/**"
				name = "A scene file changes"
			}
		} else {
			name = "File matches " + glob
		}
		tr.Glob = glob
	}

	id := slugifyAutomationID(name + "-" + thenPart)
	if len(id) > 36 {
		id = strings.TrimRight(id[:36], "-")
	}
	return AutomationRule{
		ID:          id,
		Name:        name,
		Enabled:     true,
		Trigger:     tr,
		Instruction: strings.TrimSpace(thenPart),
	}
}

func splitAutomationWords(s string) (when, then string) {
	for _, sep := range []string{"→", "->", "，", ","} {
		if idx := strings.Index(s, sep); idx > 0 {
			return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+len(sep):])
		}
	}
	// Case-insensitive ASCII search for " then " without slicing s with a strings.ToLower byte index.
	const kw = " then "
	for i := 1; i+len(kw) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(kw)], kw) {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(kw):])
		}
	}
	return s, s
}

func extractGlobFromWords(s string) string {
	for _, m := range reGlobToken.FindAllString(s, -1) {
		cand := strings.Trim(m, `"'.,;:()`)
		if validateAutomationGlob(cand) == nil {
			return cand
		}
	}
	return ""
}

func detectFileKindFromWords(lower, raw string) AutomationFileKind {
	switch {
	case strings.Contains(lower, "prefab") || strings.Contains(raw, "预制体"):
		return AutomationFileKindPrefab
	case strings.Contains(lower, "scene") || strings.Contains(lower, "level") || strings.Contains(raw, "场景") || strings.Contains(raw, "关卡"):
		return AutomationFileKindScene
	case strings.Contains(lower, "tileset") || strings.Contains(raw, "瓦片集"):
		return AutomationFileKindTileset
	case strings.Contains(lower, "cutscene") || strings.Contains(raw, "过场"):
		return AutomationFileKindCutscene
	case strings.Contains(lower, "material") || strings.Contains(raw, "材质"):
		return AutomationFileKindMaterial
	case strings.Contains(lower, "particle") || strings.Contains(raw, "粒子"):
		return AutomationFileKindParticles
	case strings.Contains(lower, "rig") || strings.Contains(raw, "骨骼"):
		return AutomationFileKindRig
	case strings.Contains(lower, "screen") || strings.Contains(lower, "hud") || strings.Contains(raw, "界面"):
		return AutomationFileKindScreen
	case strings.Contains(lower, "table") || strings.Contains(lower, "data") || strings.Contains(raw, "数据表"):
		return AutomationFileKindDataTable
	case strings.Contains(lower, "script") || strings.Contains(lower, ".go") || strings.Contains(raw, "脚本"):
		return AutomationFileKindScript
	case strings.Contains(lower, "asset") || strings.Contains(lower, "sprite") || strings.Contains(lower, "audio") || strings.Contains(raw, "资源"):
		return AutomationFileKindAsset
	default:
		return AutomationFileKindAny
	}
}

// --- Checkpoints (.nsgo/checkpoints) ---

// AutomationCheckpoints returns a copy of checkpoints taken before automation runs.
func (e *Editor) AutomationCheckpoints() []AutomationCheckpoint {
	if e == nil {
		return nil
	}
	e.automations.mu.Lock()
	out := make([]AutomationCheckpoint, len(e.automations.checkpoints))
	for i, cp := range e.automations.checkpoints {
		files := make(map[string][]byte, len(cp.Files))
		for k, v := range cp.Files {
			files[k] = append([]byte(nil), v...)
		}
		cp.Files = files
		out[i] = cp
	}
	e.automations.mu.Unlock()
	return out
}

// AutomationCheckpointByID returns the checkpoint with id, or (zero, false) if not found.
func (e *Editor) AutomationCheckpointByID(id string) (AutomationCheckpoint, bool) {
	if e == nil || id == "" {
		return AutomationCheckpoint{}, false
	}
	e.automations.mu.Lock()
	defer e.automations.mu.Unlock()
	for _, cp := range e.automations.checkpoints {
		if cp.ID == id {
			files := make(map[string][]byte, len(cp.Files))
			for k, v := range cp.Files {
				files[k] = append([]byte(nil), v...)
			}
			cp.Files = files
			return cp, true
		}
	}
	return AutomationCheckpoint{}, false
}

func (e *Editor) takeAutomationCheckpoint(rule AutomationRule, triggerDetail string) (AutomationCheckpoint, error) {
	now := e.nowTime()
	e.automations.mu.Lock()
	e.automations.checkpointSeq++
	seq := e.automations.checkpointSeq
	e.automations.mu.Unlock()
	id := fmt.Sprintf("cp-%s-%03d-%s", now.UTC().Format("20060102-150405"), seq, rule.ID)

	files := make(map[string][]byte)
	totalBytes := 0
	root := e.automationsProjectRoot()
	var cpDir string

	if root != "" {
		// Snapshot tracked YAML/Go text files under root (bounded by count and total bytes, skipping symlinks, internal dirs, and You-only files).
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if p == root {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if name == ".git" || name == ".nsgo" || name == ".game" || name == "dist" || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if len(files) >= automationMaxCheckpointFiles || totalBytes >= automationMaxCheckpointTotalBytes {
				return filepath.SkipAll
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > automationMaxCheckpointBytes {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil
			}
			relSlash := normalizeProjectRelPath(rel)
			if relSlash == "" || isInternalAutomationPath(relSlash) || !isCheckpointTextFile(relSlash) {
				return nil
			}
			if _, isYouOnly := MatchYouOnlyPath(relSlash); isYouOnly {
				return nil
			}
			if totalBytes+int(info.Size()) > automationMaxCheckpointTotalBytes {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil || len(raw) > automationMaxCheckpointBytes || totalBytes+len(raw) > automationMaxCheckpointTotalBytes {
				return nil
			}
			files[relSlash] = raw
			totalBytes += len(raw)
			return nil
		})
		cpDir = filepath.Join(root, filepath.FromSlash(AutomationsCheckpointsDir), id)
		if !isAutomationPathWithinRoot(root, cpDir) {
			return AutomationCheckpoint{}, fmt.Errorf("editor: checkpoint directory %q resolves outside project root", id)
		}
		if err := writeAutomationCheckpointToDisk(cpDir, id, rule, triggerDetail, now, files); err != nil {
			return AutomationCheckpoint{}, err
		}
	} else if e.Doc != nil {
		if raw, err := sceneformat.Encode(e.Doc); err == nil {
			key := e.ScenePath
			if key == "" {
				key = "scene.scene.yaml"
			}
			files[key] = raw
		}
	}

	label := rule.DisplayName() + " · checkpoint before start"
	cp := AutomationCheckpoint{
		ID:           id,
		AutomationID: rule.ID,
		Trigger:      triggerDetail,
		Label:        label,
		CreatedAt:    now,
		Dir:          cpDir,
		Files:        files,
	}
	e.automations.mu.Lock()
	e.automations.checkpoints = append(e.automations.checkpoints, cp)
	if len(e.automations.checkpoints) > automationMaxRetainedCheckpoints {
		drop := len(e.automations.checkpoints) - automationMaxRetainedCheckpoints
		copy(e.automations.checkpoints, e.automations.checkpoints[drop:])
		clear(e.automations.checkpoints[len(e.automations.checkpoints)-drop:])
		e.automations.checkpoints = e.automations.checkpoints[:len(e.automations.checkpoints)-drop]
	}
	e.automations.mu.Unlock()
	return cp, nil
}

func isCheckpointTextFile(relSlash string) bool {
	lower := strings.ToLower(relSlash)
	switch {
	case strings.HasSuffix(lower, ".yaml"),
		strings.HasSuffix(lower, ".yml"),
		strings.HasSuffix(lower, ".json"),
		strings.HasSuffix(lower, ".go"),
		strings.HasSuffix(lower, ".md"),
		strings.HasSuffix(lower, ".yarn"):
		return true
	default:
		return false
	}
}

func writeAutomationCheckpointToDisk(cpDir, id string, rule AutomationRule, triggerDetail string, createdAt time.Time, files map[string][]byte) error {
	if err := os.MkdirAll(cpDir, 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for rel := range files {
		names = append(names, rel)
	}
	slices.Sort(names)

	filesDir := filepath.Join(cpDir, "files")
	for _, rel := range names {
		dst := filepath.Join(filesDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(dst, files[rel]); err != nil {
			return err
		}
	}
	manifest := automationCheckpointManifest{
		ID:           id,
		AutomationID: rule.ID,
		Trigger:      triggerDetail,
		Label:        rule.DisplayName() + " · checkpoint before start",
		CreatedAt:    createdAt.UTC().Format(time.RFC3339),
		Files:        names,
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&manifest); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(cpDir, "manifest.yaml"), buf.Bytes())
}

// RestoreAutomationCheckpoint restores the files captured in checkpoint id.
// Refuses agent actors (ErrAutomationYouOnly), Play mode (ErrPlaying), History preview (ErrHistoryPreview),
// symlink escapes, internal paths, and You-only targets.
func (e *Editor) RestoreAutomationCheckpoint(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	cp, ok := e.AutomationCheckpointByID(id)
	if !ok {
		return fmt.Errorf("editor: checkpoint %q not found", id)
	}
	root := e.automationsProjectRoot()
	if root != "" {
		names := make([]string, 0, len(cp.Files))
		for rel := range cp.Files {
			names = append(names, rel)
		}
		slices.Sort(names)
		// Validate every target before writing any file.
		for _, rel := range names {
			norm := normalizeProjectRelPath(rel)
			if norm == "" || isInternalAutomationPath(norm) {
				return fmt.Errorf("editor: checkpoint contains invalid path %q", rel)
			}
			if _, isYouOnly := e.CheckYouOnlyTarget(norm); isYouOnly {
				return fmt.Errorf("editor: checkpoint refuses You-only path %q", rel)
			}
			dst := filepath.Join(root, filepath.FromSlash(norm))
			if !isAutomationPathWithinRoot(root, dst) {
				return fmt.Errorf("editor: checkpoint path %q resolves outside project root", rel)
			}
			if info, lerr := os.Lstat(dst); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("editor: checkpoint refuses symlink destination %q", rel)
			}
		}
		for _, rel := range names {
			norm := normalizeProjectRelPath(rel)
			dst := filepath.Join(root, filepath.FromSlash(norm))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := writeFileAtomic(dst, cp.Files[rel]); err != nil {
				return err
			}
		}
		if e.FilePath != "" {
			if rel, ok := e.projectRel(e.FilePath); ok {
				if raw, exists := cp.Files[rel]; exists {
					if doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{
						File:          e.FilePath,
						Components:    e.components,
						PhysicsLayers: e.PhysicsLayers(),
					}); err == nil {
						e.Doc = doc
						e.sceneSaved = append([]byte(nil), raw...)
						e.recordSceneDiskSnapshot(e.FilePath, raw)
						e.syncActiveTab()
						e.Rebuild()
					}
				}
			}
		}
		e.filesRev++
		e.snapshotProjectTree()
	} else {
		for _, raw := range cp.Files {
			if doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{Components: e.components}); err == nil {
				e.Doc = doc
				e.Rebuild()
				break
			}
		}
	}
	e.Status = "Restored checkpoint " + id
	e.notify()
	return nil
}

// --- Permissions & Review enforcement ---

// CheckAutomationActionPermission evaluates action (e.g. "Edit", "Create", "Run", "Code", "Delete") and targetPath
// against the project's You-only trust boundary and active MCP permission rules (permissions.yaml, first match wins).
func (e *Editor) CheckAutomationActionPermission(action, targetPath string) AgentPermissionDecision {
	if e == nil {
		return AgentPermissionDeny
	}
	actLower := strings.ToLower(strings.TrimSpace(action))
	if actLower != "read" && actLower != "run" {
		if _, isYouOnly := e.CheckYouOnlyTarget(targetPath); isYouOnly {
			return AgentPermissionDeny
		}
	}
	var permAct PermissionAction
	switch actLower {
	case "edit":
		permAct = PermissionActionEdit
	case "create":
		permAct = PermissionActionCreate
	case "delete":
		permAct = PermissionActionDelete
	case "run":
		permAct = PermissionActionRun
	case "code":
		permAct = PermissionActionCode
	default:
		return AgentPermissionAsk
	}
	ev := e.EvaluatePermission(PermissionRequest{
		Action: permAct,
		Target: targetPath,
	})
	switch ev.Decision {
	case PermissionAllow:
		return AgentPermissionAllow
	case PermissionDeny:
		return AgentPermissionDeny
	default:
		return AgentPermissionAsk
	}
}

// ReadyToReviewTurns returns all automation/background AgentTurns currently waiting in Ready to review.
func (e *Editor) ReadyToReviewTurns() []AgentTurn {
	if e == nil {
		return nil
	}
	var out []AgentTurn
	for _, t := range e.agent.turns {
		if t.ReviewState == AgentReviewReady {
			cp := t
			cp.Steps = slices.Clone(t.Steps)
			out = append(out, cp)
		}
	}
	return out
}

// KeepAgentTurnReview marks an automation turn's Ready-to-review result as kept by the author.
// Refuses agent actors (ErrAutomationYouOnly) so automations can never auto-keep or apply You-only drafts.
func (e *Editor) KeepAgentTurnReview(turnID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	turn := e.findAgentTurn(turnID)
	if turn == nil {
		return fmt.Errorf("editor: agent turn %q not found", turnID)
	}
	if turn.ReviewState != AgentReviewReady {
		return fmt.Errorf("editor: agent turn %q is not ready to review (state %q)", turnID, turn.ReviewState)
	}
	sourceAutoID := turn.AutomationID
	turn.ReviewState = AgentReviewKept
	e.notify()

	_ = e.KeepTask(turnID)
	// Keeping a review may trigger review_kept automations, skipping the same automation ID to avoid recursion.
	e.triggerReviewKeptAutomations(e.ScenePath, sourceAutoID)
	return nil
}

// DismissAgentTurnReview dismisses a Ready-to-review automation turn without reverting anything that was never kept.
// Refuses agent actors (ErrAutomationYouOnly).
func (e *Editor) DismissAgentTurnReview(turnID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.AgentActorActive() {
		return ErrAutomationYouOnly
	}
	turn := e.findAgentTurn(turnID)
	if turn == nil {
		return fmt.Errorf("editor: agent turn %q not found", turnID)
	}
	if turn.ReviewState != AgentReviewReady && turn.ReviewState != AgentReviewFailed {
		return fmt.Errorf("editor: agent turn %q cannot be dismissed in state %q", turnID, turn.ReviewState)
	}
	turn.ReviewState = AgentReviewDismissed
	e.notify()

	_ = e.DismissTasks([]string{turnID})
	return nil
}

// --- Trigger Dispatch & Background Task Execution ---

// NotifyAutomationFileChanged fires any enabled file_changed automations whose glob matches relPath.
// Returns the number of background automation tasks started.
func (e *Editor) NotifyAutomationFileChanged(relPath string) int {
	if e == nil || e.AutomationsPausedToday() || e.agent.mode == AgentModeNone || e.playing || e.HistoryPreviewActive() || e.FirstRunFullScreenOpen() {
		return 0
	}
	rel := e.normalizeAutomationRelPath(relPath)
	if isInternalAutomationPath(rel) {
		return 0
	}
	rules := e.Automations().Automations
	started := 0
	for _, rule := range rules {
		if !rule.Enabled || rule.Trigger.Event != AutomationTriggerFileChanged {
			continue
		}
		if !MatchAutomationGlob(rule.Trigger.Glob, rel) {
			continue
		}
		if e.startAutomationTask(rule, "File changed · "+rel) {
			started++
		}
	}
	return started
}

// NotifyAutomationReviewKept fires any enabled review_kept automations (filtered by Trigger.Glob when set).
// Returns the number of background automation tasks started.
func (e *Editor) NotifyAutomationReviewKept(relPath string) int {
	return e.triggerReviewKeptAutomations(relPath, "")
}

func (e *Editor) triggerReviewKeptAutomations(relPath, skipAutomationID string) int {
	if e == nil || e.AutomationsPausedToday() || e.agent.mode == AgentModeNone || e.playing || e.HistoryPreviewActive() || e.FirstRunFullScreenOpen() {
		return 0
	}
	rel := e.normalizeAutomationRelPath(relPath)
	rules := e.Automations().Automations
	started := 0
	for _, rule := range rules {
		if !rule.Enabled || rule.Trigger.Event != AutomationTriggerReviewKept {
			continue
		}
		if skipAutomationID != "" && rule.ID == skipAutomationID {
			continue
		}
		if rule.Trigger.Glob != "" && (rel == "" || !MatchAutomationGlob(rule.Trigger.Glob, rel)) {
			continue
		}
		detail := "Review kept"
		if rel != "" {
			detail += " · " + rel
		}
		if e.startAutomationTask(rule, detail) {
			started++
		}
	}
	return started
}

// NotifyAutomationFileAdded fires any enabled file_added automations matching relPath's kind (and optional glob).
// Returns the number of background automation tasks started.
func (e *Editor) NotifyAutomationFileAdded(relPath string) int {
	if e == nil || e.AutomationsPausedToday() || e.agent.mode == AgentModeNone || e.playing || e.HistoryPreviewActive() || e.FirstRunFullScreenOpen() {
		return 0
	}
	rel := e.normalizeAutomationRelPath(relPath)
	if isInternalAutomationPath(rel) {
		return 0
	}
	kind := ClassifyAutomationFileKind(rel)
	rules := e.Automations().Automations
	started := 0
	for _, rule := range rules {
		if !rule.Enabled || rule.Trigger.Event != AutomationTriggerFileAdded {
			continue
		}
		if rule.Trigger.Kind != AutomationFileKindAny && rule.Trigger.Kind != kind {
			continue
		}
		if rule.Trigger.Glob != "" && !MatchAutomationGlob(rule.Trigger.Glob, rel) {
			continue
		}
		if e.startAutomationTask(rule, fmt.Sprintf("Added %s · %s", kind, rel)) {
			started++
		}
	}
	return started
}

// TriggerBeforeExportAutomations fires any enabled before_export automations matching platforms (or all when none specified).
// Returns the number of background automation tasks started.
func (e *Editor) TriggerBeforeExportAutomations(platforms ...string) int {
	if e == nil || e.AutomationsPausedToday() || e.agent.mode == AgentModeNone || e.playing || e.HistoryPreviewActive() || e.FirstRunFullScreenOpen() {
		return 0
	}
	rules := e.Automations().Automations
	lowerPlats := make([]string, 0, len(platforms))
	for _, p := range platforms {
		if s := strings.ToLower(strings.TrimSpace(p)); s != "" {
			lowerPlats = append(lowerPlats, s)
		}
	}
	started := 0
	for _, rule := range rules {
		if !rule.Enabled || rule.Trigger.Event != AutomationTriggerBeforeExport {
			continue
		}
		if rule.Trigger.Platform != "" && len(lowerPlats) > 0 && !slices.Contains(lowerPlats, strings.ToLower(rule.Trigger.Platform)) {
			continue
		}
		detail := "Before export"
		if len(lowerPlats) > 0 {
			detail += " · " + strings.Join(lowerPlats, ", ")
		}
		if e.startAutomationTask(rule, detail) {
			started++
		}
	}
	return started
}

// StepAutomations checks all enabled daily automations against the editor's clock (Editor.Now or time.Now)
// and starts any whose scheduled HH:MM has been reached today and has not yet run today.
// Returns the number of background automation tasks started.
func (e *Editor) StepAutomations() int {
	if e == nil || e.AutomationsPausedToday() || e.agent.mode == AgentModeNone || e.playing || e.HistoryPreviewActive() || e.FirstRunFullScreenOpen() {
		return 0
	}
	rules := e.Automations().Automations
	now := e.nowTime()
	today := now.Format("2006-01-02")
	nowMinutes := now.Hour()*60 + now.Minute()

	started := 0
	for _, rule := range rules {
		if !rule.Enabled || rule.Trigger.Event != AutomationTriggerDaily {
			continue
		}
		e.automations.mu.Lock()
		alreadyRan := e.automations.lastDailyRun[rule.ID] == today
		e.automations.mu.Unlock()
		if alreadyRan {
			continue
		}
		h, m, err := parseDailyClockTime(rule.Trigger.Time)
		if err != nil {
			continue
		}
		if nowMinutes < h*60+m {
			continue
		}
		if e.startAutomationTask(rule, "Daily · "+rule.Trigger.Time) {
			e.automations.mu.Lock()
			e.automations.ensureInitLocked()
			e.automations.lastDailyRun[rule.ID] = today
			e.automations.mu.Unlock()
			e.saveAutomationLocalStateQuietly()
			started++
		}
	}
	return started
}

func (e *Editor) startAutomationScheduler() {
	if e == nil {
		return
	}
	e.automations.mu.Lock()
	prevStop := e.automations.schedulerStop
	e.automations.schedulerStop = nil
	e.automations.mu.Unlock()
	if prevStop != nil {
		prevStop()
	}
	// When no UI scope is attached, tests drive daily checks explicitly with StepAutomations.
	if !e.HasScope() {
		return
	}
	var (
		mu      sync.Mutex
		stopped bool
		timer   *time.Timer
	)
	var tick func()
	tick = func() {
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		mu.Unlock()
		e.StepAutomations()
		mu.Lock()
		if !stopped && e.HasScope() {
			timer = time.AfterFunc(automationDailyTickInterval, func() {
				e.postOrRun(tick)
			})
		}
		mu.Unlock()
	}
	mu.Lock()
	timer = time.AfterFunc(automationDailyTickInterval, func() {
		e.postOrRun(tick)
	})
	mu.Unlock()
	stopFn := func() {
		mu.Lock()
		stopped = true
		t := timer
		timer = nil
		mu.Unlock()
		if t != nil {
			t.Stop()
		}
	}
	e.automations.mu.Lock()
	e.automations.schedulerStop = stopFn
	e.automations.mu.Unlock()
}

func (e *Editor) normalizeAutomationRelPath(p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	if rel, ok := e.projectRel(trimmed); ok {
		return normalizeProjectRelPath(rel)
	}
	if filepath.IsAbs(trimmed) {
		if e.ScenePath != "" && e.FilePath != "" && filepath.Clean(trimmed) == filepath.Clean(e.FilePath) {
			return normalizeProjectRelPath(e.ScenePath)
		}
		return normalizeProjectRelPath(filepath.Base(trimmed))
	}
	return normalizeProjectRelPath(trimmed)
}

func (e *Editor) automationRunner() AgentRunner {
	if e == nil {
		return nil
	}
	if e.AgentRunner != nil {
		return e.AgentRunner
	}
	// Only fall back to the default external CLI runner when an agent is actually connected or configured locally.
	if len(e.agent.clients) > 0 || e.agent.mode == AgentModeLocal {
		return defaultCLIAgentRunner()
	}
	return nil
}

// startAutomationTask takes a checkpoint BEFORE starting the runner, creates an AgentTurn in the Agent panel
// and a matching TaskSourceAutomation entry in the shared background task list, and executes the automation
// under bounded concurrency, WithAgentActor, and the project's permission rules.
func (e *Editor) startAutomationTask(rule AutomationRule, triggerDetail string) bool {
	if e == nil || e.AutomationsHeld() || e.AutomationsPausedToday() || e.agent.mode == AgentModeNone || e.playing || e.HistoryPreviewActive() {
		return false
	}
	runner := e.automationRunner()
	if runner == nil {
		return false
	}
	if e.automations.activeRunCount.Load() >= automationMaxPendingRuns {
		return false
	}
	e.ensureAutomationsInit()

	// Always take a checkpoint BEFORE starting the automation (Checkpoints.dc.html).
	cp, cpErr := e.takeAutomationCheckpoint(rule, triggerDetail)

	now := e.nowTime()
	e.recordAutomationRunTimestamp(rule.ID, now)

	e.agent.turnSeq++
	turnID := fmt.Sprintf("auto-turn-%d", e.agent.turnSeq)
	clientName := ""
	agentKind := ExternalAgentAuto
	if c, ok := e.AgentPrimaryClient(); ok {
		clientName = c.DisplayName
		agentKind = DetectExternalAgentKind(c.Name)
	}

	turn := AgentTurn{
		ID:             turnID,
		Prompt:         rule.Instruction,
		Source:         "automation",
		Status:         "running",
		AgentName:      clientName,
		CreatedAt:      now,
		StartedAt:      now,
		Automation:     true,
		AutomationID:   rule.ID,
		AutomationName: rule.DisplayName(),
		TriggerSummary: triggerDetail,
		CheckpointID:   cp.ID,
		ReviewState:    AgentReviewRunning,
	}
	if cpErr != nil {
		turn.Error = fmt.Sprintf("checkpoint failed before start: %v", cpErr)
		turn.Done = true
		turn.Status = "done"
		turn.FinishedAt = now
		turn.ReviewState = AgentReviewFailed
		e.agent.turns = append(e.agent.turns, turn)
		e.notify()
		return false
	}

	e.agent.turns = append(e.agent.turns, turn)
	req := AgentRequest{
		ThreadID:       e.agent.threadID,
		SessionID:      e.agent.sessionID,
		Prompt:         rule.Instruction,
		ProjectRoot:    e.agentProjectRoot(),
		ScenePath:      e.ScenePath,
		SelectedID:     e.SelectedID,
		AgentKind:      agentKind,
		Permissions:    e.agent.permissions,
		AutomationID:   rule.ID,
		AutomationName: rule.DisplayName(),
		TriggerKind:    rule.Trigger.Event,
		TriggerDetail:  triggerDetail,
		CheckpointID:   cp.ID,
		MCPServers:     e.AgentMCPServers(),
	}
	scenePath := e.ScenePath

	ctx, cancel := context.WithTimeout(context.Background(), automationRunTimeout)
	e.automations.mu.Lock()
	e.automations.ensureInitLocked()
	runGen := e.automations.runGen
	sem := e.automations.sem
	e.automations.activeRuns[turnID] = cancel
	e.automations.activeRunCount.Add(1)
	e.automations.mu.Unlock()
	e.notify()

	// Register matching background task in the shared Tasks list (Tasks.dc.html / #1022).
	_, _ = e.SubmitTask(TaskRequest{
		ID:           turnID,
		Title:        rule.DisplayName(),
		Prompt:       rule.Instruction,
		Source:       TaskSourceAutomation,
		StartMode:    TaskStartParallel,
		InitialState: TaskRunning,
		ScenePath:    scenePath,
		StepSummary:  triggerDetail,
	})

	go func() {
		// Enforce bounded concurrent execution while respecting cancellation.
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			e.postOrRun(func() {
				e.finishAutomationRun(runGen, turnID, ctx.Err(), true)
			})
			return
		}
		err := e.WithAgentActor(func() error {
			return runner.Run(ctx, req, func(ev AgentEvent) {
				e.postOrRun(func() {
					e.applyAutomationEvent(runGen, turnID, ev)
				})
			})
		})
		e.postOrRun(func() {
			e.finishAutomationRun(runGen, turnID, err, errors.Is(ctx.Err(), context.Canceled))
		})
	}()
	return true
}

func (e *Editor) applyAutomationEvent(runGen uint64, turnID string, ev AgentEvent) {
	e.automations.mu.Lock()
	curGen := e.automations.runGen
	e.automations.mu.Unlock()
	if curGen != runGen {
		return
	}
	turn := e.findAgentTurn(turnID)
	if turn == nil || turn.Done {
		return
	}
	switch ev.Kind {
	case AgentEventSession:
		if ev.SessionID != "" {
			e.agent.sessionID = ev.SessionID
		}
	case AgentEventStep:
		stepStatus := ev.StepStatus
		if stepStatus == "" {
			stepStatus = AgentStepRunning
		}
		updated := false
		if ev.StepID != "" {
			for i := range turn.Steps {
				if turn.Steps[i].ID == ev.StepID {
					if ev.StepTitle != "" {
						turn.Steps[i].Title = ev.StepTitle
					}
					if ev.StepDetail != "" {
						turn.Steps[i].Detail = ev.StepDetail
					}
					if ev.ToolName != "" {
						turn.Steps[i].ToolName = ev.ToolName
					}
					turn.Steps[i].Status = stepStatus
					updated = true
					break
				}
			}
		}
		if !updated && len(turn.Steps) < automationMaxStepsPerTurn {
			stepID := ev.StepID
			if stepID == "" {
				stepID = fmt.Sprintf("%s-step-%d", turn.ID, len(turn.Steps)+1)
			}
			turn.Steps = append(turn.Steps, AgentStep{
				ID:       stepID,
				Title:    ev.StepTitle,
				Detail:   ev.StepDetail,
				ToolName: ev.ToolName,
				Status:   stepStatus,
			})
		}
		if stepStatus == AgentStepWaiting {
			turn.ReviewState = AgentReviewNeedsYou
			_ = e.RequestTaskPermission(turnID, ev.StepTitle, ev.StepDetail)
			e.SetAgentWaitingForYou(AgentOpenRequest{
				ID:    ev.StepID,
				Title: ev.StepTitle,
				Body:  ev.StepDetail,
			})
			return
		}
	case AgentEventMessage:
		if ev.Text != "" && len(turn.Summary) < maxAgentTurnReplyBytes {
			chunk := ev.Text
			if turn.Summary != "" && !strings.HasSuffix(turn.Summary, "\n") && !strings.HasPrefix(chunk, "\n") {
				chunk = "\n" + chunk
			}
			rem := maxAgentTurnReplyBytes - len(turn.Summary)
			if len(chunk) > rem {
				chunk = chunk[:rem]
			}
			turn.Summary += chunk
		}
	case AgentEventUsage:
		turn.InputTokens += ev.InputTokens
		turn.OutputTokens += ev.OutputTokens
	case AgentEventError:
		if ev.Err != "" {
			turn.Error = ev.Err
		}
	case AgentEventDone:
		if ev.Text != "" && turn.Summary == "" {
			txt := ev.Text
			if len(txt) > maxAgentTurnReplyBytes {
				txt = txt[:maxAgentTurnReplyBytes]
			}
			turn.Summary = txt
		}
		turn.Done = true
		turn.Status = "done"
		turn.FinishedAt = e.nowTime()
		if turn.Error != "" {
			turn.ReviewState = AgentReviewFailed
		} else {
			turn.ReviewState = AgentReviewReady
		}
	}
	e.notify()
}

func (e *Editor) finishAutomationRun(runGen uint64, turnID string, err error, canceled bool) {
	e.automations.mu.Lock()
	if e.automations.runGen != runGen {
		e.automations.mu.Unlock()
		return
	}
	cancel, ok := e.automations.activeRuns[turnID]
	if ok {
		delete(e.automations.activeRuns, turnID)
	}
	e.automations.mu.Unlock()
	if ok {
		defer func() {
			if e.automations.activeRunCount.Add(-1) < 0 {
				e.automations.activeRunCount.Store(0)
			}
		}()
	}
	if cancel != nil {
		cancel()
	}

	var (
		finalState AgentReviewState
		summary    string
		errText    string
	)
	if turn := e.findAgentTurn(turnID); turn != nil {
		if !turn.Done {
			turn.Done = true
			turn.FinishedAt = e.nowTime()
		}
		if canceled {
			turn.Stopped = true
			turn.Status = "stopped"
			turn.ReviewState = AgentReviewDismissed
		} else if err != nil || turn.Error != "" {
			if err != nil && turn.Error == "" {
				turn.Error = err.Error()
			}
			turn.Status = "done"
			turn.ReviewState = AgentReviewFailed
		} else if turn.ReviewState != AgentReviewKept && turn.ReviewState != AgentReviewDismissed {
			turn.Status = "done"
			turn.ReviewState = AgentReviewReady
		}
		for i := range turn.Steps {
			if turn.Steps[i].Status == AgentStepRunning {
				if canceled || err != nil || turn.Error != "" {
					turn.Steps[i].Status = AgentStepFailed
				} else {
					turn.Steps[i].Status = AgentStepDone
				}
			}
		}
		finalState = turn.ReviewState
		summary = turn.Summary
		errText = turn.Error
	}
	switch finalState {
	case AgentReviewReady:
		_ = e.CompleteTaskTurn(turnID, TaskReview{
			ID:        "review-" + turnID,
			TaskID:    turnID,
			ScenePath: e.ScenePath,
			Summary:   summary,
		})
	case AgentReviewFailed:
		_ = e.FailTask(turnID, errText)
	case AgentReviewDismissed:
		_ = e.StopTask(turnID)
	}
	e.notify()
}

// AutomationRunningCount returns the number of background automation tasks currently running.
func (e *Editor) AutomationRunningCount() int {
	if e == nil {
		return 0
	}
	return int(e.automations.activeRunCount.Load())
}

// WaitAutomations blocks briefly until all active background automation runs finish (for headless tests).
// Returns true if all active runs completed within timeout.
func (e *Editor) WaitAutomations(timeout time.Duration) bool {
	if e == nil {
		return true
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if e.automations.activeRunCount.Load() == 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return e.automations.activeRunCount.Load() == 0
}
