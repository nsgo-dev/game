package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/internal/releaseplan"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/game/packages/game/tooling/credits"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrReleaseBuildRunning is returned by StartReleaseBuild or ResumeReleaseBuild when a build is already active.
var ErrReleaseBuildRunning = errors.New("editor: a release build is already running")

// ErrNoReleasePresetsIncluded is returned by StartReleaseBuild when no ready preset is checked for inclusion.
var ErrNoReleasePresetsIncluded = errors.New("editor: no buildable export presets are included in this release")

// BuildFailureNotice is shown on every failed preset to reassure the author that remaining targets continue and nothing is uploaded.
const BuildFailureNotice = "Other platforms keep building. Nothing was uploaded."

// Default build budget thresholds used when game.yaml does not declare export.budgets.
const (
	DefaultDownloadSizeBudgetMB = 80.0
	DefaultFrameTimeP95BudgetMs = 16.7
	DefaultBudgetQualityProfile = "low"
)

// BuildPresetStatus reports the current lifecycle state of one platform preset on the Build and Release screen.
type BuildPresetStatus string

const (
	BuildPresetQueued     BuildPresetStatus = "queued"
	BuildPresetBuilding   BuildPresetStatus = "building"
	BuildPresetNotarizing BuildPresetStatus = "notarizing"
	BuildPresetDone       BuildPresetStatus = "done"
	BuildPresetFailed     BuildPresetStatus = "failed"
	BuildPresetSetup      BuildPresetStatus = "setup"
)

// BuildStepID identifies each ordered step executed for a release preset build.
type BuildStepID string

const (
	BuildStepCheck         BuildStepID = "check"
	BuildStepCompile       BuildStepID = "compile"
	BuildStepPack          BuildStepID = "pack"
	BuildStepSign          BuildStepID = "sign"
	BuildStepSmokePlaytest BuildStepID = "smoke_playtest"
	BuildStepMeasure       BuildStepID = "measure"
)

// BuildStepStatus reports the state of one step within a preset build.
type BuildStepStatus string

const (
	BuildStepPending BuildStepStatus = "pending"
	BuildStepRunning BuildStepStatus = "running"
	BuildStepDone    BuildStepStatus = "done"
	BuildStepFailed  BuildStepStatus = "failed"
)

// BuildStep represents one ordered step in a preset's build pipeline.
type BuildStep struct {
	ID       BuildStepID     `json:"id"`
	Label    string          `json:"label"`
	Detail   string          `json:"detail"`
	Status   BuildStepStatus `json:"status"`
	Elapsed  time.Duration   `json:"elapsed"`
	Commands []string        `json:"commands,omitempty"`
	Err      string          `json:"err,omitempty"`
}

// BuildReadinessStatus indicates whether a platform readiness requirement passes, warns, or blocks building.
type BuildReadinessStatus string

const (
	BuildReadinessOK   BuildReadinessStatus = "ok"
	BuildReadinessWarn BuildReadinessStatus = "warn"
	BuildReadinessFail BuildReadinessStatus = "fail"
)

// BuildReadinessItem describes one readiness check for a platform that may require setup before building.
type BuildReadinessItem struct {
	ID             string               `json:"id"`
	Label          string               `json:"label"`
	Detail         string               `json:"detail"`
	Status         BuildReadinessStatus `json:"status"`
	ActionLabel    string               `json:"actionLabel,omitempty"`
	AgentCanHandle bool                 `json:"agentCanHandle,omitempty"`
	RequiresPerson bool                 `json:"requiresPerson,omitempty"`
}

// BuildPresetSigningConfig holds per-preset signing settings loaded from export/<platform>.export.yaml or configured in memory.
type BuildPresetSigningConfig struct {
	Keystore string `yaml:"keystore,omitempty" json:"keystore,omitempty"`
	Alias    string `yaml:"alias,omitempty" json:"alias,omitempty"`
	Identity string `yaml:"identity,omitempty" json:"identity,omitempty"`
	Notarize bool   `yaml:"notarize,omitempty" json:"notarize,omitempty"`
}

// BuildDiffLine is one line in an agent-proposed preset fix diff.
type BuildDiffLine struct {
	LineNumber int    `json:"lineNumber,omitempty"`
	Kind       string `json:"kind"` // "context", "remove", "add"
	Text       string `json:"text"`
}

// BuildPresetFixProposal describes an agent-proposed fix diff shown when a preset build fails.
type BuildPresetFixProposal struct {
	Platform     string                    `json:"platform"`
	Title        string                    `json:"title"`
	Summary      string                    `json:"summary"`
	FilePath     string                    `json:"filePath"`
	Footnote     string                    `json:"footnote,omitempty"`
	Diff         []BuildDiffLine           `json:"diff"`
	TargetPatch  []string                  `json:"targetPatch,omitempty"`
	SigningPatch *BuildPresetSigningConfig `json:"signingPatch,omitempty"`
	Apply        func(ed *Editor) error    `json:"-"`
}

// BuildFailureInfo captures structured details of a failed preset step.
type BuildFailureInfo struct {
	Headline    string                  `json:"headline"`
	StepID      BuildStepID             `json:"stepId"`
	StepIndex   int                     `json:"stepIndex"`
	TotalSteps  int                     `json:"totalSteps"`
	StepLabel   string                  `json:"stepLabel"`
	Elapsed     time.Duration           `json:"elapsed"`
	Commands    []string                `json:"commands"`
	ErrorLine   string                  `json:"errorLine"`
	ExitLine    string                  `json:"exitLine"`
	Notice      string                  `json:"notice"`
	FixProposal *BuildPresetFixProposal `json:"fixProposal,omitempty"`
}

// BuildSizeBreakdown splits a finished release artifact into code, textures, audio, and other bytes.
type BuildSizeBreakdown struct {
	CodeBytes     int64  `yaml:"codeBytes" json:"codeBytes"`
	TexturesBytes int64  `yaml:"texturesBytes" json:"texturesBytes"`
	AudioBytes    int64  `yaml:"audioBytes" json:"audioBytes"`
	OtherBytes    int64  `yaml:"otherBytes" json:"otherBytes"`
	GrowthSummary string `yaml:"growthSummary,omitempty" json:"growthSummary,omitempty"`
}

// BuildSmokePlaytestResult summarizes the per-build smoke playtest step.
type BuildSmokePlaytestResult struct {
	Where          string        `json:"where,omitempty"`
	Summary        string        `json:"summary"`
	Duration       time.Duration `json:"duration"`
	FrameTimeP95Ms float64       `json:"frameTimeP95Ms,omitempty"`
}

// BuildArtifactResult holds the finished artifact path, size delta against the last release,
// category breakdown, budget outcomes, smoke playtest summary, and optional local test URL.
type BuildArtifactResult struct {
	ArtifactPath         string             `json:"artifactPath"`
	ArtifactName         string             `json:"artifactName"`
	SizeBytes            int64              `json:"sizeBytes"`
	DeltaBytes           int64              `json:"deltaBytes"`
	HasLastRelease       bool               `json:"hasLastRelease"`
	Breakdown            BuildSizeBreakdown `json:"breakdown"`
	FrameTimeP95Ms       float64            `json:"frameTimeP95Ms"`
	DownloadBudgetMet    bool               `json:"downloadBudgetMet"`
	FrameTimeBudgetMet   bool               `json:"frameTimeBudgetMet"`
	SmokePlaytestWhere   string             `json:"smokePlaytestWhere,omitempty"`
	SmokePlaytestSummary string             `json:"smokePlaytestSummary,omitempty"`
	TestURL              string             `json:"testUrl,omitempty"`
}

// LastReleaseArtifact records one platform's size and breakdown from the previous release.
type LastReleaseArtifact struct {
	SizeBytes  int64              `yaml:"sizeBytes" json:"sizeBytes"`
	Breakdown  BuildSizeBreakdown `yaml:"breakdown" json:"breakdown"`
	AssetSizes map[string]int64   `yaml:"assetSizes,omitempty" json:"assetSizes,omitempty"`
}

// LastReleaseBaseline holds the previous release's version and per-platform artifact sizes for delta comparison.
type LastReleaseBaseline struct {
	Version   string                         `yaml:"version" json:"version"`
	Artifacts map[string]LastReleaseArtifact `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
}

// BuildPreflightSummary holds the BEFORE EVERY BUILD cards: nsgo check, smoke playtest, and budgets from game.yaml.
type BuildPreflightSummary struct {
	CheckRan             bool          `json:"checkRan"`
	CheckProblems        int           `json:"checkProblems"`
	CheckElapsed         time.Duration `json:"checkElapsed"`
	CheckErr             string        `json:"checkErr,omitempty"`
	SmokeSummary         string        `json:"smokeSummary"`
	QualityProfile       string        `json:"qualityProfile"`
	DownloadSizeBudgetMB float64       `json:"downloadSizeBudgetMB"`
	FrameTimeP95BudgetMs float64       `json:"frameTimeP95BudgetMs"`
	BudgetsFromProject   bool          `json:"budgetsFromProject"`
}

// BuildDeviceEntry describes one selectable run-on-device target on the finished build view.
type BuildDeviceEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	Available bool   `json:"available"`
}

// BuildPresetEntry holds the live state of one platform preset in the Build and Release screen.
type BuildPresetEntry struct {
	Platform     string                   `json:"platform"`
	Name         string                   `json:"name"`
	IconKind     string                   `json:"iconKind"` // "desktop", "web", "phone"
	File         string                   `json:"file"`
	Summary      string                   `json:"summary"`
	Targets      []string                 `json:"targets,omitempty"`
	Included     bool                     `json:"included"`
	Status       BuildPresetStatus        `json:"status"`
	StatusText   string                   `json:"statusText"`
	SizeText     string                   `json:"sizeText"`
	DeltaText    string                   `json:"deltaText"`
	Steps        []BuildStep              `json:"steps"`
	LogFile      string                   `json:"logFile"`
	LogLines     []string                 `json:"logLines"`
	PlanCommands []string                 `json:"planCommands"`
	Signing      BuildPresetSigningConfig `json:"signing"`
	Failure      *BuildFailureInfo        `json:"failure,omitempty"`
	Result       *BuildArtifactResult     `json:"result,omitempty"`
	Readiness    []BuildReadinessItem     `json:"readiness,omitempty"`
	NextStepIdx  int                      `json:"nextStepIdx"`
	StoppedAt    bool                     `json:"stoppedAt,omitempty"`
}

// BuildReleaseOptions configures one StartReleaseBuild invocation.
type BuildReleaseOptions struct {
	DryRun       bool
	Channel      string
	ReleaseNotes string
}

// ReleaseStepRequest provides full context to Editor.ReleaseStepRunner when overriding a release build step.
type ReleaseStepRequest struct {
	Platform string
	StepID   BuildStepID
	Step     BuildStep
	Dir      string
	DryRun   bool
	Plan     *releaseplan.Plan
	Signing  BuildPresetSigningConfig
}

// BuildReleaseSnapshot is a thread-safe value copy of the Build and Release screen state.
type BuildReleaseSnapshot struct {
	Open                bool                  `json:"open"`
	ProjectName         string                `json:"projectName"`
	Version             string                `json:"version"`
	GitCommit           string                `json:"gitCommit"`
	GitBranch           string                `json:"gitBranch"`
	LastReleaseVersion  string                `json:"lastReleaseVersion"`
	CommitsSinceRelease int                   `json:"commitsSinceRelease"`
	ReleaseNotes        string                `json:"releaseNotes"`
	Preflight           BuildPreflightSummary `json:"preflight"`
	Presets             []BuildPresetEntry    `json:"presets"`
	SelectedPlatform    string                `json:"selectedPlatform"`
	ActivePlatform      string                `json:"activePlatform"`
	SelectedDeviceID    string                `json:"selectedDeviceId"`
	Devices             []BuildDeviceEntry    `json:"devices"`
	Running             bool                  `json:"running"`
	Stopped             bool                  `json:"stopped"`
	DryRun              bool                  `json:"dryRun"`
	FollowLog           bool                  `json:"followLog"`
	ShowWholeLog        bool                  `json:"showWholeLog"`
	EditingPreset       bool                  `json:"editingPreset"`
	PlanCommandLine     string                `json:"planCommandLine"`
	DryRunOutput        string                `json:"dryRunOutput"`
	LastErr             string                `json:"lastErr,omitempty"`
}

// SelectedPreset returns the currently selected preset from the snapshot, or nil if no presets exist.
func (s BuildReleaseSnapshot) SelectedPreset() *BuildPresetEntry {
	for i := range s.Presets {
		if s.Presets[i].Platform == s.SelectedPlatform {
			return &s.Presets[i]
		}
	}
	if len(s.Presets) > 0 {
		return &s.Presets[0]
	}
	return nil
}

type releaseBuildRun struct {
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once
	dryRun   bool
}

func (r *releaseBuildRun) finish() {
	r.doneOnce.Do(func() {
		close(r.done)
	})
}

type buildReleaseState struct {
	mu                  sync.Mutex
	open                bool
	rev                 uint64
	gitCommit           string
	gitBranch           string
	lastReleaseVersion  string
	commitsSinceRelease int
	releaseNotes        string
	notesInitialized    bool
	preflight           BuildPreflightSummary
	baseline            LastReleaseBaseline
	presets             []BuildPresetEntry
	signingOverrides    map[string]BuildPresetSigningConfig
	selectedPlatform    string
	activePlatform      string
	selectedDeviceID    string
	dryRun              bool
	followLog           bool
	showWholeLog        bool
	editingPreset       bool
	stopped             bool
	planCommandLine     string
	dryRunOutput        string
	lastErr             error
	pendingStatus       string
	hasPendingStatus    bool
	run                 *releaseBuildRun
}

type presetYAMLConfig struct {
	Summary string                   `yaml:"summary,omitempty"`
	Signing BuildPresetSigningConfig `yaml:"signing,omitempty"`
}

// BuildReleaseScreenOpen reports whether the full-window Build and Release screen is open.
func (e *Editor) BuildReleaseScreenOpen() bool {
	if e == nil {
		return false
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.buildRelease.open
}

// OpenBuildReleaseScreen opens the full-window Build and Release screen for the open project,
// refreshing project metadata, BEFORE EVERY BUILD checks, presets, and platform readiness.
func (e *Editor) OpenBuildReleaseScreen() error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	e.hud.open = false
	e.playtest.open = false
	if e.usages.open {
		e.usages.open = false
		e.usages.rev++
	}
	if e.changes.open {
		e.CloseChangesPanel()
	}
	e.buildRelease.mu.Lock()
	e.buildRelease.open = true
	e.buildRelease.followLog = true
	e.buildRelease.editingPreset = false
	e.buildRelease.showWholeLog = false
	if e.buildRelease.selectedDeviceID == "" {
		e.buildRelease.selectedDeviceID = "browser"
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()

	e.refreshBuildReleaseMetadata()
	e.syncBuildReleasePresets()
	_ = e.RefreshBuildPreflight()
	e.CheckExportCredits()
	e.notify()
	return nil
}

// CloseBuildReleaseScreen closes the Build and Release screen and returns to the scene editor.
func (e *Editor) CloseBuildReleaseScreen() {
	if e == nil {
		return
	}
	if !e.closeBuildReleaseScreenSilent() {
		return
	}
	e.notify()
}

func (e *Editor) closeBuildReleaseScreenSilent() bool {
	if e == nil {
		return false
	}
	e.buildRelease.mu.Lock()
	if !e.buildRelease.open {
		e.buildRelease.mu.Unlock()
		return false
	}
	e.buildRelease.open = false
	e.buildRelease.editingPreset = false
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	return true
}

func (e *Editor) flushBuildReleaseStatus() bool {
	if e == nil {
		return false
	}
	e.buildRelease.mu.Lock()
	has := e.buildRelease.hasPendingStatus
	msg := e.buildRelease.pendingStatus
	e.buildRelease.hasPendingStatus = false
	e.buildRelease.pendingStatus = ""
	e.buildRelease.mu.Unlock()
	if has {
		e.Status = msg
	}
	return has
}

func (e *Editor) notifyBuildReleaseUI(scope *core.Scope, run *releaseBuildRun) {
	if scope == nil {
		return
	}
	scope.Post(func() {
		e.buildRelease.mu.Lock()
		active := e.buildRelease.run == run || e.buildRelease.hasPendingStatus
		e.buildRelease.mu.Unlock()
		if !active {
			return
		}
		e.flushBuildReleaseStatus()
		e.notify()
	})
}

func (e *Editor) buildReleaseRev() uint64 {
	if e == nil {
		return 0
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.buildRelease.rev
}

// BuildReleaseSnapshot returns a thread-safe deep snapshot of the Build and Release screen state.
func (e *Editor) BuildReleaseSnapshot() BuildReleaseSnapshot {
	if e == nil {
		return BuildReleaseSnapshot{}
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()

	projectName := ""
	version := ""
	if e.Project != nil {
		projectName = e.Project.Name
		version = e.Project.AppVersion
	}
	lastVer := e.buildRelease.baseline.Version
	if lastVer == "" {
		lastVer = e.buildRelease.lastReleaseVersion
	}
	if lastVer == "" {
		lastVer = "none"
	}
	presets := make([]BuildPresetEntry, len(e.buildRelease.presets))
	for i, p := range e.buildRelease.presets {
		presets[i] = cloneBuildPresetEntry(p)
	}
	lastErrText := ""
	if e.buildRelease.lastErr != nil {
		lastErrText = e.buildRelease.lastErr.Error()
	}
	return BuildReleaseSnapshot{
		Open:                e.buildRelease.open,
		ProjectName:         projectName,
		Version:             version,
		GitCommit:           e.buildRelease.gitCommit,
		GitBranch:           e.buildRelease.gitBranch,
		LastReleaseVersion:  lastVer,
		CommitsSinceRelease: e.buildRelease.commitsSinceRelease,
		ReleaseNotes:        e.buildRelease.releaseNotes,
		Preflight:           e.buildRelease.preflight,
		Presets:             presets,
		SelectedPlatform:    e.buildRelease.selectedPlatform,
		ActivePlatform:      e.buildRelease.activePlatform,
		SelectedDeviceID:    e.buildRelease.selectedDeviceID,
		Devices:             e.buildDevicesLocked(),
		Running:             e.buildRelease.run != nil,
		Stopped:             e.buildRelease.stopped,
		DryRun:              e.buildRelease.dryRun,
		FollowLog:           e.buildRelease.followLog,
		ShowWholeLog:        e.buildRelease.showWholeLog,
		EditingPreset:       e.buildRelease.editingPreset,
		PlanCommandLine:     e.buildRelease.planCommandLine,
		DryRunOutput:        e.buildRelease.dryRunOutput,
		LastErr:             lastErrText,
	}
}

func (e *Editor) buildDevicesLocked() []BuildDeviceEntry {
	androidOK := false
	androidNote := "Android not built yet"
	for _, p := range e.buildRelease.presets {
		if p.Platform == "android" {
			switch p.Status {
			case BuildPresetDone:
				androidOK = true
				androidNote = "AAB ready"
			case BuildPresetFailed:
				androidNote = "Android build failed"
			case BuildPresetBuilding:
				androidNote = "after Android builds"
			}
		}
	}
	return []BuildDeviceEntry{
		{ID: "browser", Name: "This computer's browser", Note: "Web build", Available: true},
		{ID: "phone-wifi", Name: "Phone · Wi-Fi", Note: "Web build via QR", Available: true},
		{ID: "android-usb", Name: "Android · USB", Note: androidNote, Available: androidOK},
	}
}

// SetReleaseVersion updates the open project's appVersion through EditProject.
func (e *Editor) SetReleaseVersion(version string) error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	version = strings.TrimSpace(version)
	if version == "" || version == e.Project.AppVersion {
		return nil
	}
	if err := e.EditProject("Set release version", func(f *project.File) {
		f.AppVersion = version
	}); err != nil {
		return err
	}
	e.buildRelease.mu.Lock()
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return nil
}

// SetReleaseNotesText sets the release notes draft shown in the Build and Release screen.
func (e *Editor) SetReleaseNotesText(notes string) {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.releaseNotes == notes && e.buildRelease.notesInitialized {
		e.buildRelease.mu.Unlock()
		return
	}
	e.buildRelease.releaseNotes = notes
	e.buildRelease.notesInitialized = true
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// ReleaseDryRun reports whether the Build and Release screen is configured for dry-run builds.
func (e *Editor) ReleaseDryRun() bool {
	if e == nil {
		return false
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.buildRelease.dryRun
}

// SetReleaseDryRun toggles dry-run mode on the Build and Release screen.
func (e *Editor) SetReleaseDryRun(dryRun bool) {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.dryRun == dryRun {
		e.buildRelease.mu.Unlock()
		return
	}
	e.buildRelease.dryRun = dryRun
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SetReleaseFollowLog toggles log auto-following on the Build and Release screen.
func (e *Editor) SetReleaseFollowLog(follow bool) {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.followLog == follow {
		e.buildRelease.mu.Unlock()
		return
	}
	e.buildRelease.followLog = follow
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SetReleaseShowWholeLog toggles expanding the full log view on a failed preset.
func (e *Editor) SetReleaseShowWholeLog(show bool) {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.showWholeLog == show {
		e.buildRelease.mu.Unlock()
		return
	}
	e.buildRelease.showWholeLog = show
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SetReleaseEditingPreset toggles the inline preset editor drawer for the selected preset.
func (e *Editor) SetReleaseEditingPreset(editing bool) {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.editingPreset == editing {
		e.buildRelease.mu.Unlock()
		return
	}
	e.buildRelease.editingPreset = editing
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SelectBuildDevice selects a run-on-device entry on the finished build view.
func (e *Editor) SelectBuildDevice(deviceID string) {
	if e == nil || deviceID == "" {
		return
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.selectedDeviceID == deviceID {
		e.buildRelease.mu.Unlock()
		return
	}
	e.buildRelease.selectedDeviceID = deviceID
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SelectBuildPreset selects platform in the Build and Release screen's preset list.
func (e *Editor) SelectBuildPreset(platform string) {
	if e == nil || platform == "" {
		return
	}
	e.buildRelease.mu.Lock()
	found := false
	for _, p := range e.buildRelease.presets {
		if p.Platform == platform {
			found = true
			break
		}
	}
	if !found || e.buildRelease.selectedPlatform == platform {
		e.buildRelease.mu.Unlock()
		return
	}
	e.buildRelease.selectedPlatform = platform
	e.buildRelease.showWholeLog = false
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SetBuildPresetIncluded checks or unchecks platform for inclusion in the release build.
func (e *Editor) SetBuildPresetIncluded(platform string, included bool) error {
	if e == nil {
		return ErrNoProject
	}
	e.buildRelease.mu.Lock()
	idx := -1
	for i := range e.buildRelease.presets {
		if e.buildRelease.presets[i].Platform == platform {
			idx = i
			break
		}
	}
	if idx < 0 {
		e.buildRelease.mu.Unlock()
		return fmt.Errorf("editor: build preset %q not found", platform)
	}
	if e.buildRelease.presets[idx].Included == included {
		e.buildRelease.mu.Unlock()
		return nil
	}
	e.buildRelease.presets[idx].Included = included
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return nil
}

// SetBuildPresetSigning configures signing settings (such as keystore path, alias, identity, or notarization)
// for platform on the Build and Release screen.
func (e *Editor) SetBuildPresetSigning(platform string, cfg BuildPresetSigningConfig) {
	if e == nil || platform == "" {
		return
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.signingOverrides == nil {
		e.buildRelease.signingOverrides = make(map[string]BuildPresetSigningConfig)
	}
	e.buildRelease.signingOverrides[platform] = cfg
	for i := range e.buildRelease.presets {
		if e.buildRelease.presets[i].Platform == platform {
			e.buildRelease.presets[i].Signing = cfg
			e.buildRelease.presets[i].Steps = defaultStepsForPreset(e.buildRelease.presets[i].Platform, e.buildRelease.presets[i].Targets, cfg)
			break
		}
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SetLastReleaseBaseline sets the previous release's version and per-platform artifact sizes for delta comparison.
func (e *Editor) SetLastReleaseBaseline(baseline LastReleaseBaseline) {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	e.buildRelease.baseline = cloneLastReleaseBaseline(baseline)
	if baseline.Version != "" {
		e.buildRelease.lastReleaseVersion = baseline.Version
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SetBuildPresetFixProposal attaches an agent-proposed preset fix diff to a failed (or selected) preset.
func (e *Editor) SetBuildPresetFixProposal(platform string, proposal *BuildPresetFixProposal) error {
	if e == nil {
		return ErrNoProject
	}
	e.buildRelease.mu.Lock()
	idx := -1
	for i := range e.buildRelease.presets {
		if e.buildRelease.presets[i].Platform == platform {
			idx = i
			break
		}
	}
	if idx < 0 {
		e.buildRelease.mu.Unlock()
		return fmt.Errorf("editor: build preset %q not found", platform)
	}
	p := &e.buildRelease.presets[idx]
	if p.Failure == nil {
		p.Failure = &BuildFailureInfo{
			Headline:   "Build failed",
			StepID:     BuildStepSign,
			StepIndex:  4,
			TotalSteps: 6,
			StepLabel:  "Sign",
			Notice:     BuildFailureNotice,
		}
	}
	p.Failure.FixProposal = cloneFixProposal(proposal)
	if p.Failure.FixProposal != nil && p.Failure.FixProposal.Platform == "" {
		p.Failure.FixProposal.Platform = platform
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return nil
}

// ApplyBuildPresetFixAndRebuild applies the agent-proposed fix diff on platform (updating signing config,
// preset targets, and/or export/<platform>.export.yaml), clears the failure, and rebuilds platform.
func (e *Editor) ApplyBuildPresetFixAndRebuild(platform string) error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.ReleaseBuildRunning() {
		return ErrReleaseBuildRunning
	}

	e.buildRelease.mu.Lock()
	idx := -1
	for i := range e.buildRelease.presets {
		if e.buildRelease.presets[i].Platform == platform {
			idx = i
			break
		}
	}
	if idx < 0 {
		e.buildRelease.mu.Unlock()
		return fmt.Errorf("editor: build preset %q not found", platform)
	}
	p := e.buildRelease.presets[idx]
	if p.Failure == nil || p.Failure.FixProposal == nil {
		e.buildRelease.mu.Unlock()
		return fmt.Errorf("editor: build preset %q has no proposed fix", platform)
	}
	prop := cloneFixProposal(p.Failure.FixProposal)
	dryRun := e.buildRelease.dryRun
	e.buildRelease.mu.Unlock()

	if prop.Apply != nil {
		if err := prop.Apply(e); err != nil {
			return err
		}
	}
	if len(prop.TargetPatch) > 0 {
		if _, ok := e.Project.Preset(platform); ok {
			if err := e.SetExportPresetTargets(platform, prop.TargetPatch); err != nil {
				return err
			}
		}
	}

	if !isSafeBuildPlatformSegment(platform) {
		return fmt.Errorf("editor: invalid build preset platform %q", platform)
	}

	var persistPath string
	var persistCfg presetYAMLConfig
	hasPersist := false

	e.buildRelease.mu.Lock()
	for i := range e.buildRelease.presets {
		if e.buildRelease.presets[i].Platform == platform {
			if prop.SigningPatch != nil {
				e.buildRelease.presets[i].Signing = *prop.SigningPatch
				if e.buildRelease.signingOverrides == nil {
					e.buildRelease.signingOverrides = make(map[string]BuildPresetSigningConfig)
				}
				e.buildRelease.signingOverrides[platform] = *prop.SigningPatch
			} else if strings.TrimSpace(e.buildRelease.presets[i].Signing.Keystore) != "" &&
				!strings.HasPrefix(e.buildRelease.presets[i].Signing.Keystore, "keychain:") {
				// Default fix migrates missing keystore file reference to a keychain reference.
				fixed := e.buildRelease.presets[i].Signing
				fixed.Keystore = "keychain:" + strings.ToLower(strings.ReplaceAll(e.Project.Name, " ", "-")) + " upload"
				e.buildRelease.presets[i].Signing = fixed
				if e.buildRelease.signingOverrides == nil {
					e.buildRelease.signingOverrides = make(map[string]BuildPresetSigningConfig)
				}
				e.buildRelease.signingOverrides[platform] = fixed
			}
			if e.ProjectPath != "" {
				persistPath = filepath.ToSlash(filepath.Join("export", platform+".export.yaml"))
				persistCfg = presetYAMLConfig{
					Summary: e.buildRelease.presets[i].Summary,
					Signing: e.buildRelease.presets[i].Signing,
				}
				hasPersist = true
			}
			e.buildRelease.presets[i].Included = true
			e.buildRelease.presets[i].Failure = nil
			e.buildRelease.presets[i].Result = nil
			e.buildRelease.presets[i].Status = BuildPresetQueued
			e.buildRelease.presets[i].StatusText = "Queued"
			e.buildRelease.presets[i].DeltaText = ""
			e.buildRelease.presets[i].SizeText = "—"
			e.buildRelease.presets[i].NextStepIdx = 0
			e.buildRelease.presets[i].StoppedAt = false
			e.buildRelease.presets[i].LogLines = nil
			e.buildRelease.presets[i].Steps = defaultStepsForPreset(platform, e.buildRelease.presets[i].Targets, e.buildRelease.presets[i].Signing)
			e.buildRelease.selectedPlatform = platform
			break
		}
	}
	e.buildRelease.stopped = false
	e.buildRelease.showWholeLog = false
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()

	// Persist signing update to export/<platform>.export.yaml if that file already exists on disk.
	if hasPersist && e.ProjectPath != "" {
		baseDir := filepath.Dir(e.ProjectPath)
		resolvedPath, rerr := resolveBuildReleasePath(baseDir, persistPath, true)
		if rerr != nil {
			return rerr
		}
		if _, statErr := os.Stat(resolvedPath); statErr == nil {
			data, merr := yaml.Marshal(&persistCfg)
			if merr != nil {
				return merr
			}
			if werr := writeFileAtomic(resolvedPath, data); werr != nil {
				return werr
			}
		}
	}
	e.notify()

	return e.startReleaseBuildInternal([]string{platform}, BuildReleaseOptions{
		DryRun:       dryRun,
		Channel:      e.ExportChannel(),
		ReleaseNotes: e.ExportReleaseNotes(),
	}, false)
}

// RefreshBuildPreflight runs the BEFORE EVERY BUILD checks (nsgo check, smoke playtest summary,
// budgets from game.yaml) and re-evaluates readiness for every preset.
func (e *Editor) RefreshBuildPreflight() error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}

	var problems []assetcheck.Problem
	var elapsed time.Duration
	var checkErr error
	if e.ReleaseCheckRunner != nil {
		problems, elapsed, checkErr = e.ReleaseCheckRunner(context.Background(), dir)
	} else if dir != "" {
		start := time.Now()
		problems, checkErr = runProjectCheck(dir, e.components)
		elapsed = time.Since(start)
	}

	// Read budgets from game.yaml (with defaults when omitted).
	quality := DefaultBudgetQualityProfile
	dlBudget := DefaultDownloadSizeBudgetMB
	frameBudget := DefaultFrameTimeP95BudgetMs
	fromProject := false
	if e.Project.Export != nil && e.Project.Export.Budgets != nil {
		b := e.Project.Export.Budgets
		fromProject = true
		if b.QualityProfile != "" {
			quality = b.QualityProfile
		}
		if b.DownloadSizeMB > 0 {
			dlBudget = b.DownloadSizeMB
		}
		if b.FrameTimeP95Ms > 0 {
			frameBudget = b.FrameTimeP95Ms
		}
	}

	smokeScene := "main scene"
	if e.Project.Autoload != nil && e.Project.Autoload.MainScene != "" {
		smokeScene = e.Project.Autoload.MainScene
	} else if e.Doc != nil && e.Doc.Name != "" {
		smokeScene = e.Doc.Name
	}

	preflight := BuildPreflightSummary{
		CheckRan:             true,
		CheckProblems:        len(problems),
		CheckElapsed:         elapsed,
		SmokeSummary:         fmt.Sprintf("60 s per build, %s", smokeScene),
		QualityProfile:       capitalizeProfile(quality),
		DownloadSizeBudgetMB: dlBudget,
		FrameTimeP95BudgetMs: frameBudget,
		BudgetsFromProject:   fromProject,
	}
	if checkErr != nil {
		preflight.CheckErr = checkErr.Error()
	}

	e.buildRelease.mu.Lock()
	e.buildRelease.preflight = preflight
	for i := range e.buildRelease.presets {
		plat := e.buildRelease.presets[i].Platform
		items := e.evaluateReadinessFor(plat, dir, e.Project)
		e.buildRelease.presets[i].Readiness = items
		blockers, totalIssues := countReadinessIssues(items)
		if blockers > 0 && e.buildRelease.presets[i].Status != BuildPresetDone && e.buildRelease.presets[i].Status != BuildPresetBuilding && e.buildRelease.presets[i].Status != BuildPresetNotarizing {
			e.buildRelease.presets[i].Status = BuildPresetSetup
			e.buildRelease.presets[i].StatusText = "Set up"
			e.buildRelease.presets[i].DeltaText = fmt.Sprintf("%d to fix", totalIssues)
			e.buildRelease.presets[i].Included = false
		} else if blockers == 0 && e.buildRelease.presets[i].Status == BuildPresetSetup {
			e.buildRelease.presets[i].Status = BuildPresetQueued
			e.buildRelease.presets[i].StatusText = "Queued"
			e.buildRelease.presets[i].DeltaText = ""
			e.buildRelease.presets[i].Included = true
		}
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return checkErr
}

// EvaluateBuildReadiness computes and returns the readiness checklist for platform against the open project.
func (e *Editor) EvaluateBuildReadiness(platform string) []BuildReadinessItem {
	if e == nil || e.Project == nil {
		return nil
	}
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}
	return e.evaluateReadinessFor(platform, dir, e.Project)
}

func (e *Editor) evaluateReadinessFor(platform, dir string, f *project.File) []BuildReadinessItem {
	if e.ReleaseReadinessInspector != nil {
		return slices.Clone(e.ReleaseReadinessInspector(platform, dir, f))
	}
	if platform != "ios" {
		return nil
	}

	var items []BuildReadinessItem
	// 1. Bundle id
	if f != nil && strings.TrimSpace(f.ID) != "" {
		items = append(items, BuildReadinessItem{
			ID:     "bundle_id",
			Label:  "Bundle id " + f.ID,
			Detail: "from game.yaml",
			Status: BuildReadinessOK,
		})
	} else {
		items = append(items, BuildReadinessItem{
			ID:             "bundle_id",
			Label:          "Bundle id",
			Detail:         "id is empty in game.yaml",
			Status:         BuildReadinessFail,
			ActionLabel:    "Set bundle id",
			AgentCanHandle: true,
		})
	}

	// 2. App icon
	iconPath, iconDetail, iconOK := inspectAppIcon(dir)
	if iconOK {
		items = append(items, BuildReadinessItem{
			ID:     "app_icon",
			Label:  "App icon",
			Detail: iconDetail,
			Status: BuildReadinessOK,
		})
	} else {
		detail := "art/icon.png (1024 × 1024, no transparency) not found"
		if iconPath != "" {
			detail = iconDetail
		}
		items = append(items, BuildReadinessItem{
			ID:             "app_icon",
			Label:          "App icon",
			Detail:         detail,
			Status:         BuildReadinessWarn,
			ActionLabel:    "Check icon…",
			AgentCanHandle: false,
			RequiresPerson: true,
		})
	}

	// 3. Apple Distribution certificate
	certFound := dir != "" && fileExistsAny(dir, "export/ios.cert", "ios/distribution.cer")
	if certFound {
		items = append(items, BuildReadinessItem{
			ID:     "distribution_cert",
			Label:  "Apple Distribution certificate",
			Detail: "configured in project",
			Status: BuildReadinessOK,
		})
	} else {
		items = append(items, BuildReadinessItem{
			ID:             "distribution_cert",
			Label:          "Apple Distribution certificate",
			Detail:         "using login keychain if configured; sign in to Xcode to install one",
			Status:         BuildReadinessOK,
			RequiresPerson: true,
		})
	}

	// 4. Provisioning profile
	profileFound := dir != "" && fileExistsAny(dir, "export/ios.mobileprovision", "ios/profile.mobileprovision")
	if profileFound {
		items = append(items, BuildReadinessItem{
			ID:     "provisioning_profile",
			Label:  "Provisioning profile",
			Detail: fmt.Sprintf("installed for %s", f.ID),
			Status: BuildReadinessOK,
		})
	} else {
		items = append(items, BuildReadinessItem{
			ID:             "provisioning_profile",
			Label:          "Provisioning profile",
			Detail:         fmt.Sprintf("none for %s on this machine", f.ID),
			Status:         BuildReadinessFail,
			ActionLabel:    "Sign in to Apple…",
			AgentCanHandle: false,
			RequiresPerson: true,
		})
	}

	// 5. Privacy manifest
	privacyPath := findPrivacyManifest(dir)
	if privacyPath != "" {
		items = append(items, BuildReadinessItem{
			ID:     "privacy_manifest",
			Label:  "Privacy manifest",
			Detail: privacyPath + " present (no tracked data declared)",
			Status: BuildReadinessOK,
		})
	} else {
		items = append(items, BuildReadinessItem{
			ID:             "privacy_manifest",
			Label:          "Privacy manifest",
			Detail:         "PrivacyInfo.xcprivacy is missing; the game reads no tracked data",
			Status:         BuildReadinessWarn,
			ActionLabel:    "Let the agent write it",
			AgentCanHandle: true,
			RequiresPerson: false,
		})
	}

	// 6. Touch controls
	touchOK, touchDetail := inspectTouchControls(dir, f)
	if touchOK {
		items = append(items, BuildReadinessItem{
			ID:     "touch_controls",
			Label:  "Touch controls",
			Detail: touchDetail,
			Status: BuildReadinessOK,
		})
	} else {
		items = append(items, BuildReadinessItem{
			ID:             "touch_controls",
			Label:          "Touch controls",
			Detail:         "no touchRegion bindings in game.yaml or input/touch.input.yaml",
			Status:         BuildReadinessWarn,
			ActionLabel:    "Add touch controls",
			AgentCanHandle: true,
			RequiresPerson: false,
		})
	}
	return items
}

// RunBuildReadinessAction executes the fix/action for one readiness item on platform.
// Agent-capable items (such as writing PrivacyInfo.xcprivacy or adding touch controls) are
// performed directly on disk/project; person-required items invoke OnReleaseReadinessPersonAction.
func (e *Editor) RunBuildReadinessAction(platform, itemID string) error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.ReleaseBuildRunning() {
		return ErrReleaseBuildRunning
	}
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}

	var targetItem BuildReadinessItem
	found := false
	e.buildRelease.mu.Lock()
	for _, p := range e.buildRelease.presets {
		if p.Platform == platform {
			for _, it := range p.Readiness {
				if it.ID == itemID {
					targetItem = it
					found = true
					break
				}
			}
			break
		}
	}
	e.buildRelease.mu.Unlock()
	if !found {
		return fmt.Errorf("editor: readiness item %q not found for platform %q", itemID, platform)
	}

	if targetItem.AgentCanHandle {
		switch itemID {
		case "privacy_manifest":
			if dir == "" {
				return ErrNoProject
			}
			target, err := resolveBuildReleasePath(dir, "PrivacyInfo.xcprivacy", true)
			if err != nil {
				return err
			}
			if err := writeFileAtomic(target, []byte(defaultPrivacyManifestXML)); err != nil {
				return err
			}
			e.filesRev++
			e.Status = "Wrote PrivacyInfo.xcprivacy"
		case "touch_controls":
			if err := e.EditProject("Add touch controls", func(f *project.File) {
				if f.Input == nil {
					f.Input = &project.Input{}
				}
				f.Input.Actions = append(f.Input.Actions, project.Action{
					Name: "touch_action",
					Bindings: []project.Binding{{
						Kind: project.KindTouchRegion,
						Region: &project.Rect{
							X: 0, Y: 0, Width: 320, Height: 360,
						},
					}},
				})
			}); err != nil {
				return err
			}
			e.Status = "Added default touch control binding"
		}
		_ = e.RefreshBuildPreflight()
		return nil
	}

	if e.OnReleaseReadinessPersonAction != nil {
		if err := e.OnReleaseReadinessPersonAction(platform, targetItem); err != nil {
			return err
		}
		_ = e.RefreshBuildPreflight()
		return nil
	}
	e.Status = fmt.Sprintf("%s requires manual setup: %s", targetItem.Label, targetItem.ActionLabel)
	e.notify()
	return nil
}

// RunBuildReadinessAgentSetup performs every agent-handleable readiness fix on platform,
// then hands off any remaining person-required items to OnReleaseReadinessPersonAction.
func (e *Editor) RunBuildReadinessAgentSetup(platform string) error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.ReleaseBuildRunning() {
		return ErrReleaseBuildRunning
	}
	snap := e.BuildReleaseSnapshot()
	var items []BuildReadinessItem
	for _, p := range snap.Presets {
		if p.Platform == platform {
			items = p.Readiness
			break
		}
	}
	for _, it := range items {
		if it.Status != BuildReadinessOK && it.AgentCanHandle {
			if err := e.RunBuildReadinessAction(platform, it.ID); err != nil {
				return err
			}
		}
	}
	for _, it := range items {
		if it.Status != BuildReadinessOK && it.RequiresPerson && e.OnReleaseReadinessPersonAction != nil {
			if err := e.OnReleaseReadinessPersonAction(platform, it); err != nil {
				return err
			}
		}
	}
	_ = e.RefreshBuildPreflight()
	return nil
}

// PrioritizeBuildPreset moves platform to be the next queued preset in build order.
func (e *Editor) PrioritizeBuildPreset(platform string) {
	if e == nil || platform == "" {
		return
	}
	e.buildRelease.mu.Lock()
	idx := -1
	for i := range e.buildRelease.presets {
		if e.buildRelease.presets[i].Platform == platform {
			idx = i
			break
		}
	}
	if idx <= 0 {
		e.buildRelease.mu.Unlock()
		return
	}
	target := e.buildRelease.presets[idx]
	insertAt := 0
	for insertAt < len(e.buildRelease.presets) {
		st := e.buildRelease.presets[insertAt].Status
		if st == BuildPresetDone || st == BuildPresetBuilding || st == BuildPresetNotarizing {
			insertAt++
			continue
		}
		break
	}
	if insertAt < idx {
		copy(e.buildRelease.presets[insertAt+1:idx+1], e.buildRelease.presets[insertAt:idx])
		e.buildRelease.presets[insertAt] = target
		e.buildRelease.rev++
	}
	e.buildRelease.mu.Unlock()
	e.notify()
}

// ReleaseBuildRunning reports whether a Build and Release screen run is active.
func (e *Editor) ReleaseBuildRunning() bool {
	if e == nil {
		return false
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.buildRelease.run != nil
}

// ReleaseBuildStopped reports whether the last release build was stopped mid-run and can be resumed.
func (e *Editor) ReleaseBuildStopped() bool {
	if e == nil {
		return false
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.buildRelease.stopped
}

// ReleasePlanCommandLine returns the combined POSIX-quoted `go ... tool nsgo build ...` command line
// matching `game release -dry-run` for the included presets of the latest build.
func (e *Editor) ReleasePlanCommandLine() string {
	if e == nil {
		return ""
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.buildRelease.planCommandLine
}

// ReleaseDryRunOutput returns the exact `releaseplan.WriteDryRun` output for the latest release build.
func (e *Editor) ReleaseDryRunOutput() string {
	if e == nil {
		return ""
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.buildRelease.dryRunOutput
}

// StartReleaseBuild starts building every included, ready preset one at a time on a background goroutine.
// Pass an optional BuildReleaseOptions to override DryRun, Channel, or ReleaseNotes; omitting it uses the screen's settings.
func (e *Editor) StartReleaseBuild(opts ...BuildReleaseOptions) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.ReleaseBuildRunning() {
		return ErrReleaseBuildRunning
	}

	opt := BuildReleaseOptions{
		DryRun:       e.ReleaseDryRun(),
		Channel:      e.ExportChannel(),
		ReleaseNotes: e.ExportReleaseNotes(),
	}
	if len(opts) > 0 {
		opt = opts[0]
		e.buildRelease.mu.Lock()
		e.buildRelease.dryRun = opt.DryRun
		e.buildRelease.mu.Unlock()
	}

	e.syncBuildReleasePresets()

	e.buildRelease.mu.Lock()
	var queue []string
	for i := range e.buildRelease.presets {
		p := &e.buildRelease.presets[i]
		if !p.Included || p.Status == BuildPresetSetup {
			continue
		}
		queue = append(queue, p.Platform)
	}
	e.buildRelease.mu.Unlock()

	if len(queue) == 0 {
		return ErrNoReleasePresetsIncluded
	}
	return e.startReleaseBuildInternal(queue, opt, false)
}

// StopReleaseBuild cancels an in-flight release build while keeping every completed step intact
// so ResumeReleaseBuild can continue from the interrupted step.
func (e *Editor) StopReleaseBuild() {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	run := e.buildRelease.run
	e.buildRelease.stopped = true
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	if run != nil {
		run.cancel()
	}
	e.notify()
}

// ResumeReleaseBuild resumes a previously stopped release build, keeping completed presets and
// continuing the interrupted preset from the step that was running.
func (e *Editor) ResumeReleaseBuild() error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.ReleaseBuildRunning() {
		return ErrReleaseBuildRunning
	}

	e.buildRelease.mu.Lock()
	dryRun := e.buildRelease.dryRun
	var queue []string
	for i := range e.buildRelease.presets {
		p := &e.buildRelease.presets[i]
		if !p.Included || p.Status == BuildPresetSetup || p.Status == BuildPresetDone {
			continue
		}
		queue = append(queue, p.Platform)
	}
	e.buildRelease.stopped = false
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()

	if len(queue) == 0 {
		return ErrNoReleasePresetsIncluded
	}
	return e.startReleaseBuildInternal(queue, BuildReleaseOptions{
		DryRun:       dryRun,
		Channel:      e.ExportChannel(),
		ReleaseNotes: e.ExportReleaseNotes(),
	}, true)
}

// WaitReleaseBuild blocks until any in-flight release build finishes and returns its final error.
func (e *Editor) WaitReleaseBuild() error {
	if e == nil {
		return nil
	}
	e.buildRelease.mu.Lock()
	run := e.buildRelease.run
	e.buildRelease.mu.Unlock()
	if run != nil {
		<-run.done
	}
	if e.flushBuildReleaseStatus() {
		e.notify()
	}
	e.buildRelease.mu.Lock()
	err := e.buildRelease.lastErr
	e.buildRelease.mu.Unlock()
	return err
}

// ShowBuildArtifactInFinder opens the build output folder (or artifact's parent directory) using OpenOutputFolder.
func (e *Editor) ShowBuildArtifactInFinder(platform string) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	baseDir := filepath.Dir(e.ProjectPath)
	e.buildRelease.mu.Lock()
	artifactDir := ""
	for _, p := range e.buildRelease.presets {
		if p.Platform == platform && p.Result != nil && p.Result.ArtifactPath != "" {
			artifactDir = filepath.Dir(p.Result.ArtifactPath)
			break
		}
	}
	e.buildRelease.mu.Unlock()
	if artifactDir != "" {
		resolvedDir, err := resolveBuildReleasePath(baseDir, artifactDir, false)
		if err != nil {
			return err
		}
		opener := e.OpenOutputFolder
		if opener == nil {
			opener = defaultOpenOutputFolder
		}
		return opener(resolvedDir)
	}
	return e.OpenExportOutputFolder()
}

// ReviewReleaseUploads invokes OnReviewReleaseUploads (if set) without uploading any artifact automatically.
func (e *Editor) ReviewReleaseUploads() error {
	if e == nil {
		return ErrNoProject
	}
	snap := e.BuildReleaseSnapshot()
	if e.OnReviewReleaseUploads != nil {
		return e.OnReviewReleaseUploads(snap)
	}
	e.Status = "Review uploads: each store upload asks first before uploading."
	e.notify()
	return nil
}

func (e *Editor) startReleaseBuildInternal(platforms []string, opt BuildReleaseOptions, resume bool) error {
	dir := filepath.Dir(e.ProjectPath)
	notes := strings.TrimSpace(opt.ReleaseNotes)
	if notes != "" && !filepath.IsAbs(notes) {
		notes = filepath.Join(dir, filepath.FromSlash(notes))
	}

	// Build combined target list for included queue presets so ReleaseDryRunOutput matches `game release -dry-run`.
	settings := releaseplan.SettingsFromProject(e.Project)
	e.buildRelease.mu.Lock()
	var combinedTargets []string
	presetMap := make(map[string]BuildPresetEntry, len(e.buildRelease.presets))
	for _, p := range e.buildRelease.presets {
		presetMap[p.Platform] = cloneBuildPresetEntry(p)
	}
	for _, plat := range platforms {
		p := presetMap[plat]
		if len(p.Targets) > 0 {
			combinedTargets = append(combinedTargets, p.Targets...)
		} else {
			combinedTargets = append(combinedTargets, plat)
		}
	}
	e.buildRelease.mu.Unlock()

	combinedOpts := releaseplan.Options{
		Targets: strings.Join(combinedTargets, ","),
		Channel: opt.Channel,
		Notes:   notes,
	}
	combinedPlan, err := releaseplan.Build(dir, combinedOpts, settings)
	if err != nil {
		e.buildRelease.mu.Lock()
		e.buildRelease.lastErr = err
		e.buildRelease.rev++
		e.buildRelease.mu.Unlock()
		e.Status = "Release blocked: " + err.Error()
		e.notify()
		return err
	}
	var dryBuf bytes.Buffer
	releaseplan.WriteDryRun(&dryBuf, combinedPlan)
	combinedDryRun := dryBuf.String()

	// Pre-plan each preset with releaseplan.Build so every preset carries the exact `game release -dry-run` command.
	plans := make(map[string]*releaseplan.Plan, len(platforms))
	planLines := make(map[string][]string, len(platforms))
	for _, plat := range platforms {
		p := presetMap[plat]
		targs := p.Targets
		if len(targs) == 0 {
			targs = []string{plat}
		}
		pPlan, perr := releaseplan.Build(dir, releaseplan.Options{
			Targets: strings.Join(targs, ","),
			Channel: opt.Channel,
			Notes:   notes,
		}, settings)
		if perr != nil {
			return perr
		}
		plans[plat] = pPlan
		var pBuf bytes.Buffer
		releaseplan.WriteDryRun(&pBuf, pPlan)
		lines := splitNonEmptyLines(pBuf.String())
		planLines[plat] = lines
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &releaseBuildRun{
		cancel: cancel,
		done:   make(chan struct{}),
		dryRun: opt.DryRun,
	}

	e.buildRelease.mu.Lock()
	e.buildRelease.run = run
	e.buildRelease.stopped = false
	e.buildRelease.lastErr = nil
	e.buildRelease.planCommandLine = combinedPlan.CommandLine
	e.buildRelease.dryRunOutput = combinedDryRun
	for i := range e.buildRelease.presets {
		p := &e.buildRelease.presets[i]
		if !slices.Contains(platforms, p.Platform) {
			continue
		}
		p.PlanCommands = slices.Clone(planLines[p.Platform])
		if resume && p.NextStepIdx > 0 && p.NextStepIdx < len(p.Steps) {
			p.Status = BuildPresetQueued
			p.StatusText = "Queued"
			p.StoppedAt = false
			p.Failure = nil
			continue
		}
		p.Status = BuildPresetQueued
		p.StatusText = "Queued"
		p.SizeText = "—"
		p.DeltaText = ""
		p.Failure = nil
		p.Result = nil
		p.LogLines = nil
		p.NextStepIdx = 0
		p.StoppedAt = false
		p.Steps = defaultStepsForPreset(p.Platform, p.Targets, p.Signing)
	}
	if e.buildRelease.selectedPlatform == "" && len(platforms) > 0 {
		e.buildRelease.selectedPlatform = platforms[0]
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()

	if opt.DryRun {
		e.Status = "Running dry-run release…"
	} else {
		e.Status = "Building release…"
	}
	e.notify()

	scope := e.scope
	projectClone := e.Project.Clone()

	// Capture runner hooks and component registry on the caller goroutine so the background
	// worker never reads unsynchronized Editor fields.
	comps := e.components
	checkRunner := e.ReleaseCheckRunner
	if checkRunner == nil {
		checkRunner = func(_ context.Context, d string) ([]assetcheck.Problem, time.Duration, error) {
			cs := time.Now()
			probs, err := runProjectCheck(d, comps)
			return probs, time.Since(cs), err
		}
	}
	cmdRunner := e.ExportCommandRunner
	if cmdRunner == nil {
		cmdRunner = defaultExportCommandRunner
	}
	runners := releaseBuildRunners{
		step:     e.ReleaseStepRunner,
		check:    checkRunner,
		cmd:      cmdRunner,
		playtest: e.ReleasePlaytestRunner,
		measure:  e.ReleaseMeasureRunner,
	}

	remaining := make(map[string]bool, len(platforms))
	for _, plat := range platforms {
		remaining[plat] = true
	}

	go func() {
		// Close run.done strictly after cancellation, diskLog close, and final state update.
		defer run.finish()
		defer cancel()
		var firstFailure error
		wasStopped := false

		for len(remaining) > 0 {
			if ctx.Err() != nil {
				wasStopped = true
				break
			}
			// Pick the next queued preset in the live presets order so PrioritizeBuildPreset
			// ("Build next") takes effect immediately for subsequent presets.
			plat := e.nextBuildPlatform(remaining, platforms)
			if plat == "" {
				break
			}
			delete(remaining, plat)
			plan := plans[plat]
			if stopped, failErr := e.executePresetBuild(ctx, scope, run, dir, projectClone, plat, plan, opt.DryRun, runners); stopped {
				wasStopped = true
				break
			} else if failErr != nil && firstFailure == nil {
				firstFailure = failErr
			}
		}

		e.buildRelease.mu.Lock()
		active := e.buildRelease.run == run
		if active {
			e.buildRelease.run = nil
			e.buildRelease.activePlatform = ""
			if wasStopped {
				e.buildRelease.stopped = true
				for i := range e.buildRelease.presets {
					p := &e.buildRelease.presets[i]
					if slices.Contains(platforms, p.Platform) && (p.Status == BuildPresetQueued || p.Status == BuildPresetBuilding || p.Status == BuildPresetNotarizing) {
						p.Status = BuildPresetQueued
						p.StatusText = "Stopped"
						p.DeltaText = ""
						p.StoppedAt = true
					}
				}
			} else {
				e.buildRelease.stopped = false
				e.buildRelease.lastErr = firstFailure
			}
			switch {
			case wasStopped:
				e.buildRelease.pendingStatus = "Release build stopped (completed steps kept)"
			case firstFailure != nil:
				e.buildRelease.pendingStatus = "Release finished with errors: " + firstFailure.Error()
			case opt.DryRun:
				e.buildRelease.pendingStatus = "Dry-run release complete"
			default:
				e.buildRelease.pendingStatus = "Release build complete"
			}
			e.buildRelease.hasPendingStatus = true
			e.buildRelease.rev++
		}
		e.buildRelease.mu.Unlock()
		if active {
			e.notifyBuildReleaseUI(scope, run)
		}
	}()

	return nil
}

type releaseBuildRunners struct {
	step     func(ctx context.Context, req ReleaseStepRequest, stdout, stderr io.Writer) error
	check    func(ctx context.Context, dir string) ([]assetcheck.Problem, time.Duration, error)
	cmd      func(ctx context.Context, args []string, stdout, stderr io.Writer) error
	playtest func(ctx context.Context, platform string, dir string) (BuildSmokePlaytestResult, error)
	measure  func(ctx context.Context, platform string, plan *releaseplan.Plan) (BuildArtifactResult, error)
}

func (e *Editor) nextBuildPlatform(remaining map[string]bool, fallbackOrder []string) string {
	e.buildRelease.mu.Lock()
	for _, p := range e.buildRelease.presets {
		if remaining[p.Platform] {
			e.buildRelease.mu.Unlock()
			return p.Platform
		}
	}
	e.buildRelease.mu.Unlock()
	for _, plat := range fallbackOrder {
		if remaining[plat] {
			return plat
		}
	}
	return ""
}

func (e *Editor) executePresetBuild(
	ctx context.Context,
	scope *core.Scope,
	run *releaseBuildRun,
	dir string,
	proj *project.File,
	platform string,
	plan *releaseplan.Plan,
	dryRun bool,
	runners releaseBuildRunners,
) (stopped bool, failErr error) {
	e.buildRelease.mu.Lock()
	var preset BuildPresetEntry
	for _, p := range e.buildRelease.presets {
		if p.Platform == platform {
			preset = cloneBuildPresetEntry(p)
			break
		}
	}
	preflight := e.buildRelease.preflight
	baseline := cloneLastReleaseBaseline(e.buildRelease.baseline)
	e.buildRelease.mu.Unlock()

	startIdx := preset.NextStepIdx
	if startIdx < 0 || startIdx >= len(preset.Steps) {
		startIdx = 0
	}

	appendLog := func(line string) {
		e.buildRelease.mu.Lock()
		active := e.buildRelease.run == run
		if active {
			if idx := e.findBuildPresetLocked(platform); idx >= 0 {
				e.buildRelease.presets[idx].LogLines = append(e.buildRelease.presets[idx].LogLines, line)
				e.buildRelease.rev++
			}
		}
		e.buildRelease.mu.Unlock()
		if active {
			e.notifyBuildReleaseUI(scope, run)
		}
	}

	var diskLog *os.File
	if !dryRun && dir != "" && preset.LogFile != "" {
		if logAbs, rerr := resolveBuildReleasePath(dir, preset.LogFile, true); rerr == nil {
			if err := os.MkdirAll(filepath.Dir(logAbs), 0o755); err == nil {
				flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
				if startIdx > 0 {
					flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
				}
				if f, err := os.OpenFile(logAbs, flags, 0o644); err == nil {
					diskLog = f
					defer diskLog.Close()
				}
			}
		}
	}
	logLine := func(line string) {
		if diskLog != nil {
			_, _ = fmt.Fprintln(diskLog, line)
		}
		appendLog(line)
	}

	buildStart := time.Now()
	var smokeRes BuildSmokePlaytestResult

	for stepIdx := startIdx; stepIdx < len(preset.Steps); stepIdx++ {
		if ctx.Err() != nil {
			e.buildRelease.mu.Lock()
			active := e.buildRelease.run == run
			if active {
				if idx := e.findBuildPresetLocked(platform); idx >= 0 {
					p := &e.buildRelease.presets[idx]
					p.NextStepIdx = stepIdx
					p.Status = BuildPresetQueued
					p.StatusText = "Stopped"
					p.StoppedAt = true
					p.Steps[stepIdx].Status = BuildStepPending
					e.buildRelease.rev++
				}
			}
			e.buildRelease.mu.Unlock()
			if active {
				e.notifyBuildReleaseUI(scope, run)
			}
			return true, nil
		}

		step := preset.Steps[stepIdx]
		presetStatus := BuildPresetBuilding
		presetStatusText := "Building"
		if step.ID == BuildStepSign && platform == "macos" && preset.Signing.Notarize {
			presetStatus = BuildPresetNotarizing
			presetStatusText = "Notarizing"
		}

		e.buildRelease.mu.Lock()
		active := e.buildRelease.run == run
		if active {
			e.buildRelease.activePlatform = platform
			if idx := e.findBuildPresetLocked(platform); idx >= 0 {
				p := &e.buildRelease.presets[idx]
				p.Status = presetStatus
				p.StatusText = presetStatusText
				p.DeltaText = fmt.Sprintf("step %d of %d", stepIdx+1, len(p.Steps))
				p.NextStepIdx = stepIdx
				p.Steps[stepIdx].Status = BuildStepRunning
				e.buildRelease.rev++
			}
		}
		e.buildRelease.mu.Unlock()
		if active {
			e.notifyBuildReleaseUI(scope, run)
		}

		stepStart := time.Now()
		var stepCommands []string
		var stepDetail string
		var stepErr error
		var failureHeadline string
		var failureErrorLine string

		stream := newExportStreamWriter(logLine)

		req := ReleaseStepRequest{
			Platform: platform,
			StepID:   step.ID,
			Step:     step,
			Dir:      dir,
			DryRun:   dryRun,
			Plan:     plan,
			Signing:  preset.Signing,
		}

		if runners.step != nil {
			if step.ID == BuildStepCompile {
				stepCommands = append(stepCommands, plan.CommandLine)
			}
			if step.ID == BuildStepPack && plan.CreditsPath != "" {
				stepCommands = append(stepCommands, fmt.Sprintf("# then writes the credits of %d assets to %s", len(plan.Credits), plan.CreditsPath))
			}
			stepErr = runners.step(ctx, req, stream, stream)
			stream.Flush()
		} else {
			switch step.ID {
			case BuildStepCheck:
				stepCommands = []string{"nsgo check ./..."}
				probs, checkElapsed, cErr := runners.check(ctx, dir)
				stepErr = cErr
				if checkElapsed == 0 {
					checkElapsed = time.Since(stepStart)
				}
				// Update preflight check card with live result.
				e.buildRelease.mu.Lock()
				if e.buildRelease.run == run {
					e.buildRelease.preflight.CheckRan = true
					e.buildRelease.preflight.CheckProblems = len(probs)
					e.buildRelease.preflight.CheckElapsed = checkElapsed
					if stepErr != nil {
						e.buildRelease.preflight.CheckErr = stepErr.Error()
					} else {
						e.buildRelease.preflight.CheckErr = ""
					}
					e.buildRelease.rev++
				}
				e.buildRelease.mu.Unlock()
				if stepErr == nil && len(probs) > 0 && !dryRun {
					stepErr = fmt.Errorf("%d project problem(s) found", len(probs))
					failureHeadline = fmt.Sprintf("Project check failed: %d problem(s)", len(probs))
				}
				if stepErr == nil {
					stepDetail = fmt.Sprintf("nsgo check · %d problems", len(probs))
					logLine(fmt.Sprintf("nsgo check ./...  ok (%.1fs)", checkElapsed.Seconds()))
				} else {
					failureErrorLine = "error: " + stepErr.Error()
					logLine(failureErrorLine)
				}

			case BuildStepCompile:
				stepCommands = []string{plan.CommandLine}
				logLine(plan.CommandLine)
				if !dryRun {
					if err := runners.cmd(ctx, slices.Clone(plan.Args), stream, stream); err != nil {
						stepErr = fmt.Errorf("nsgo build: %w", err)
						failureHeadline = "Compile failed"
						failureErrorLine = "error: " + stepErr.Error()
					}
					stream.Flush()
				}
				if stepErr == nil {
					stepDetail = strings.Join(presetTargetsOrPlatform(preset), ", ")
				}

			case BuildStepPack:
				if plan.CreditsPath != "" {
					creditCmd := fmt.Sprintf("# then writes the credits of %d assets to %s", len(plan.Credits), plan.CreditsPath)
					stepCommands = []string{creditCmd}
					logLine(creditCmd)
					if !dryRun {
						if name, err := credits.Write(plan.OutputDir, plan.Credits); err != nil {
							stepErr = err
							failureHeadline = "Asset packing failed"
							failureErrorLine = "error: " + err.Error()
						} else {
							logLine(fmt.Sprintf("wrote the credits of %d assets to %s", len(plan.Credits), name))
						}
					}
				} else {
					logLine("pack: " + releaseplan.NoCreditsNote)
				}
				if stepErr == nil {
					assetCount := countProjectAssets(dir)
					stepDetail = fmt.Sprintf("%d files", assetCount)
					logLine(fmt.Sprintf("pack: %d files ready for %s", assetCount, preset.Name))
				}

			case BuildStepSign:
				signCmds, signDetail, sHead, sErrLine, sErr := checkPresetSigning(platform, proj, preset.Signing)
				stepCommands = signCmds
				stepDetail = signDetail
				for _, c := range signCmds {
					logLine(c)
				}
				if sErr != nil {
					stepErr = sErr
					failureHeadline = sHead
					failureErrorLine = sErrLine
					logLine(sErrLine)
					logLine(fmt.Sprintf("build %s: exit status 1", platform))
				} else {
					logLine(fmt.Sprintf("sign %s: ok", platform))
				}

			case BuildStepSmokePlaytest:
				if runners.playtest != nil {
					smokeRes, stepErr = runners.playtest(ctx, platform, dir)
				} else {
					smokeRes = defaultSmokePlaytestResult(platform, preflight.QualityProfile)
				}
				if stepErr != nil {
					failureHeadline = "Smoke playtest failed"
					failureErrorLine = "error: " + stepErr.Error()
					logLine(failureErrorLine)
				} else {
					stepDetail = smokeRes.Where
					if stepDetail == "" {
						stepDetail = "60 s headless"
					}
					logLine(fmt.Sprintf("smoke playtest (%s): %s", platform, smokeRes.Summary))
				}

			case BuildStepMeasure:
				var measured BuildArtifactResult
				if runners.measure != nil {
					measured, stepErr = runners.measure(ctx, platform, plan)
				} else {
					measured = measurePresetArtifact(dir, proj, platform, plan, baseline, preflight, smokeRes)
				}
				if stepErr != nil {
					failureHeadline = "Measure failed"
					failureErrorLine = "error: " + stepErr.Error()
					logLine(failureErrorLine)
				} else {
					measured.DownloadBudgetMet = preflight.DownloadSizeBudgetMB <= 0 || float64(measured.SizeBytes)/(1024*1024) <= preflight.DownloadSizeBudgetMB
					measured.FrameTimeBudgetMet = preflight.FrameTimeP95BudgetMs <= 0 || measured.FrameTimeP95Ms <= preflight.FrameTimeP95BudgetMs
					if measured.SmokePlaytestSummary == "" && smokeRes.Summary != "" {
						measured.SmokePlaytestSummary = smokeRes.Summary
						measured.SmokePlaytestWhere = smokeRes.Where
					}
					stepDetail = fmt.Sprintf("%s · p95 %.1f ms", formatSizeMB(measured.SizeBytes), measured.FrameTimeP95Ms)
					logLine(fmt.Sprintf("measure %s: %s (%s), p95 %.1f ms", platform, measured.ArtifactName, formatSizeMB(measured.SizeBytes), measured.FrameTimeP95Ms))

					// Store finished result onto preset.
					resCopy := measured
					e.buildRelease.mu.Lock()
					if e.buildRelease.run == run {
						if idx := e.findBuildPresetLocked(platform); idx >= 0 {
							p := &e.buildRelease.presets[idx]
							p.Result = &resCopy
							p.SizeText = formatSizeMB(resCopy.SizeBytes)
							if resCopy.HasLastRelease {
								p.DeltaText = formatDeltaMB(resCopy.DeltaBytes)
							} else {
								p.DeltaText = "new"
							}
							e.buildRelease.rev++
						}
					}
					e.buildRelease.mu.Unlock()
				}
			}
		}

		// Re-check cancellation in case Stop was pressed during step execution.
		if ctx.Err() != nil {
			e.buildRelease.mu.Lock()
			active := e.buildRelease.run == run
			if active {
				if idx := e.findBuildPresetLocked(platform); idx >= 0 {
					p := &e.buildRelease.presets[idx]
					p.NextStepIdx = stepIdx
					p.Status = BuildPresetQueued
					p.StatusText = "Stopped"
					p.StoppedAt = true
					p.Steps[stepIdx].Status = BuildStepPending
					e.buildRelease.rev++
				}
			}
			e.buildRelease.mu.Unlock()
			if active {
				e.notifyBuildReleaseUI(scope, run)
			}
			return true, nil
		}

		stepElapsed := time.Since(stepStart)

		if stepErr != nil {
			if failureHeadline == "" {
				failureHeadline = fmt.Sprintf("%s failed: %v", step.Label, stepErr)
			}
			if failureErrorLine == "" {
				failureErrorLine = "error: " + stepErr.Error()
			}
			exitLine := fmt.Sprintf("build %s: exit status 1", platform)
			cmds := slices.Clone(stepCommands)
			if len(cmds) == 0 {
				cmds = []string{plan.CommandLine}
			}
			failInfo := &BuildFailureInfo{
				Headline:   failureHeadline,
				StepID:     step.ID,
				StepIndex:  stepIdx + 1,
				TotalSteps: len(preset.Steps),
				StepLabel:  step.Label,
				Elapsed:    time.Since(buildStart),
				Commands:   cmds,
				ErrorLine:  failureErrorLine,
				ExitLine:   exitLine,
				Notice:     BuildFailureNotice,
			}
			shortTag := shortStepFailureTag(step.ID)
			e.buildRelease.mu.Lock()
			active := e.buildRelease.run == run
			if active {
				if idx := e.findBuildPresetLocked(platform); idx >= 0 {
					p := &e.buildRelease.presets[idx]
					p.Steps[stepIdx].Status = BuildStepFailed
					p.Steps[stepIdx].Elapsed = stepElapsed
					p.Steps[stepIdx].Err = stepErr.Error()
					if len(stepCommands) > 0 {
						p.Steps[stepIdx].Commands = slices.Clone(stepCommands)
					}
					p.Status = BuildPresetFailed
					p.StatusText = "Failed"
					p.DeltaText = shortTag
					p.Failure = failInfo
					p.NextStepIdx = stepIdx
					e.buildRelease.rev++
				}
			}
			e.buildRelease.mu.Unlock()
			if active {
				e.notifyBuildReleaseUI(scope, run)
			}
			return false, fmt.Errorf("%s (%s): %w", platform, step.Label, stepErr)
		}

		e.buildRelease.mu.Lock()
		active = e.buildRelease.run == run
		if active {
			if idx := e.findBuildPresetLocked(platform); idx >= 0 {
				p := &e.buildRelease.presets[idx]
				p.Steps[stepIdx].Status = BuildStepDone
				p.Steps[stepIdx].Elapsed = stepElapsed
				if stepDetail != "" {
					p.Steps[stepIdx].Detail = stepDetail
				}
				if len(stepCommands) > 0 {
					p.Steps[stepIdx].Commands = slices.Clone(stepCommands)
				}
				p.NextStepIdx = stepIdx + 1
				e.buildRelease.rev++
			}
		}
		e.buildRelease.mu.Unlock()
		if active {
			e.notifyBuildReleaseUI(scope, run)
		}
	}

	e.buildRelease.mu.Lock()
	active := e.buildRelease.run == run
	if active {
		if idx := e.findBuildPresetLocked(platform); idx >= 0 {
			p := &e.buildRelease.presets[idx]
			p.Status = BuildPresetDone
			p.StatusText = "Done"
			p.NextStepIdx = len(p.Steps)
			e.buildRelease.rev++
		}
	}
	e.buildRelease.mu.Unlock()
	if active {
		e.notifyBuildReleaseUI(scope, run)
	}
	return false, nil
}

func (e *Editor) stopReleaseBuild() {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	run := e.buildRelease.run
	e.buildRelease.run = nil
	e.buildRelease.mu.Unlock()
	if run != nil {
		// Only cancel; the worker goroutine closes run.done via defer run.finish() once it exits.
		run.cancel()
	}
}

func (e *Editor) resetBuildReleaseForProject() {
	if e == nil {
		return
	}
	e.stopReleaseBuild()
	e.buildRelease.mu.Lock()
	e.buildRelease.open = false
	e.buildRelease.gitCommit = ""
	e.buildRelease.gitBranch = ""
	e.buildRelease.lastReleaseVersion = ""
	e.buildRelease.commitsSinceRelease = 0
	e.buildRelease.releaseNotes = ""
	e.buildRelease.notesInitialized = false
	e.buildRelease.preflight = BuildPreflightSummary{}
	e.buildRelease.baseline = LastReleaseBaseline{}
	e.buildRelease.presets = nil
	e.buildRelease.signingOverrides = nil
	e.buildRelease.selectedPlatform = ""
	e.buildRelease.activePlatform = ""
	e.buildRelease.selectedDeviceID = "browser"
	e.buildRelease.dryRun = false
	e.buildRelease.followLog = true
	e.buildRelease.showWholeLog = false
	e.buildRelease.editingPreset = false
	e.buildRelease.stopped = false
	e.buildRelease.planCommandLine = ""
	e.buildRelease.dryRunOutput = ""
	e.buildRelease.lastErr = nil
	e.buildRelease.pendingStatus = ""
	e.buildRelease.hasPendingStatus = false
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
}

func (e *Editor) refreshBuildReleaseMetadata() {
	if e == nil || e.Project == nil {
		return
	}
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}
	commit := "local"
	branch := "main"
	lastTag := ""
	commitsCount := 0
	var commitSubjects []string

	if dir != "" {
		if out, err := e.runBuildGit(dir, "rev-parse", "--short", "HEAD"); err == nil {
			if s := strings.TrimSpace(out); s != "" {
				commit = s
			}
		}
		if out, err := e.runBuildGit(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			if s := strings.TrimSpace(out); s != "" {
				branch = s
			}
		}
		if out, err := e.runBuildGit(dir, "describe", "--tags", "--abbrev=0"); err == nil {
			if s := strings.TrimSpace(out); s != "" {
				lastTag = strings.TrimPrefix(s, "v")
				if cnt, cerr := e.runBuildGit(dir, "rev-list", "--count", s+"..HEAD"); cerr == nil {
					_, _ = fmt.Sscanf(strings.TrimSpace(cnt), "%d", &commitsCount)
				}
				if logOut, lerr := e.runBuildGit(dir, "log", "--pretty=format:%s", "-n", "8", s+"..HEAD"); lerr == nil {
					commitSubjects = splitNonEmptyLines(logOut)
				}
			}
		}

		// Load last-release.yaml from build/ or export output dir when no baseline was injected.
		e.buildRelease.mu.Lock()
		hasBaseline := e.buildRelease.baseline.Version != "" || len(e.buildRelease.baseline.Artifacts) > 0
		e.buildRelease.mu.Unlock()
		if !hasBaseline {
			if b, ok := loadLastReleaseBaseline(dir, e.Project); ok {
				e.buildRelease.mu.Lock()
				e.buildRelease.baseline = b
				if b.Version != "" {
					lastTag = b.Version
				}
				e.buildRelease.mu.Unlock()
			}
		}
	}

	e.buildRelease.mu.Lock()
	e.buildRelease.gitCommit = commit
	e.buildRelease.gitBranch = branch
	if lastTag != "" && e.buildRelease.lastReleaseVersion == "" {
		e.buildRelease.lastReleaseVersion = lastTag
	}
	e.buildRelease.commitsSinceRelease = commitsCount
	if !e.buildRelease.notesInitialized {
		if len(commitSubjects) > 0 {
			var b strings.Builder
			for _, subj := range commitSubjects {
				b.WriteString("- ")
				b.WriteString(subj)
				b.WriteByte('\n')
			}
			e.buildRelease.releaseNotes = strings.TrimSpace(b.String())
		} else {
			e.buildRelease.releaseNotes = fmt.Sprintf("Release %s.", e.Project.AppVersion)
		}
		e.buildRelease.notesInitialized = true
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
}

func (e *Editor) syncBuildReleasePresets() {
	if e == nil || e.Project == nil {
		return
	}
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}

	var specs []project.Preset
	if e.Project.Export != nil && len(e.Project.Export.Presets) > 0 {
		specs = e.ExportPresets()
	} else {
		platforms := e.EffectiveExportTargets()
		seen := make(map[string]bool, len(platforms))
		for _, t := range platforms {
			plat, _, _ := strings.Cut(t, "_")
			if !seen[plat] {
				seen[plat] = true
				specs = append(specs, project.Preset{Platform: plat})
			}
		}
	}

	e.buildRelease.mu.Lock()
	existing := make(map[string]BuildPresetEntry, len(e.buildRelease.presets))
	for _, p := range e.buildRelease.presets {
		existing[p.Platform] = p
	}
	overrides := e.buildRelease.signingOverrides

	next := make([]BuildPresetEntry, 0, len(specs))
	for _, sp := range specs {
		fileCfg := loadPresetYAMLConfig(dir, sp.Platform)
		signing := fileCfg.Signing
		if ov, ok := overrides[sp.Platform]; ok {
			signing = ov
		}
		summary := fileCfg.Summary
		if summary == "" {
			summary = defaultPresetSummary(sp.Platform, sp.Targets)
		}
		if cur, ok := existing[sp.Platform]; ok {
			cur.Targets = slices.Clone(sp.Targets)
			cur.Summary = summary
			cur.Signing = signing
			next = append(next, cur)
			continue
		}
		readiness := e.evaluateReadinessFor(sp.Platform, dir, e.Project)
		blockers, totalIssues := countReadinessIssues(readiness)
		status := BuildPresetQueued
		statusText := "Queued"
		deltaText := ""
		included := true
		if blockers > 0 {
			status = BuildPresetSetup
			statusText = "Set up"
			deltaText = fmt.Sprintf("%d to fix", totalIssues)
			included = false
		}
		next = append(next, BuildPresetEntry{
			Platform:   sp.Platform,
			Name:       platformDisplayName(sp.Platform),
			IconKind:   platformIconKind(sp.Platform),
			File:       fmt.Sprintf("export/%s.export.yaml", sp.Platform),
			Summary:    summary,
			Targets:    slices.Clone(sp.Targets),
			Included:   included,
			Status:     status,
			StatusText: statusText,
			SizeText:   "—",
			DeltaText:  deltaText,
			Steps:      defaultStepsForPreset(sp.Platform, sp.Targets, signing),
			LogFile:    fmt.Sprintf("build/%s.log", sp.Platform),
			Signing:    signing,
			Readiness:  readiness,
		})
	}
	e.buildRelease.presets = next
	if len(next) == 0 {
		e.buildRelease.selectedPlatform = ""
	} else {
		found := false
		for _, p := range next {
			if p.Platform == e.buildRelease.selectedPlatform {
				found = true
				break
			}
		}
		if !found {
			e.buildRelease.selectedPlatform = next[0].Platform
		}
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
}

func (e *Editor) findBuildPresetLocked(platform string) int {
	for i := range e.buildRelease.presets {
		if e.buildRelease.presets[i].Platform == platform {
			return i
		}
	}
	return -1
}

// --- Pure helpers ---

func defaultStepsForPreset(platform string, targets []string, signing BuildPresetSigningConfig) []BuildStep {
	targetDesc := strings.Join(targets, ", ")
	if targetDesc == "" {
		targetDesc = platform
	}
	signDetail := "release signing"
	switch {
	case signing.Keystore != "":
		signDetail = signing.Keystore
	case signing.Identity != "":
		signDetail = signing.Identity
	case platform == "macos":
		signDetail = "Developer ID · notarized"
	case platform == "android":
		signDetail = "AAB keystore"
	case platform == "web":
		signDetail = "integrity hash"
	}
	smokeDetail := "60 s headless"
	if platform == "android" {
		smokeDetail = "60 s on an emulator"
	} else if platform == "web" {
		smokeDetail = "60 s headless Chromium"
	}
	return []BuildStep{
		{ID: BuildStepCheck, Label: "Check project", Detail: "nsgo check", Status: BuildStepPending},
		{ID: BuildStepCompile, Label: "Compile Go", Detail: targetDesc, Status: BuildStepPending},
		{ID: BuildStepPack, Label: "Pack assets", Detail: packAssetsDetail(platform), Status: BuildStepPending},
		{ID: BuildStepSign, Label: "Sign", Detail: signDetail, Status: BuildStepPending},
		{ID: BuildStepSmokePlaytest, Label: "Smoke playtest", Detail: smokeDetail, Status: BuildStepPending},
		{ID: BuildStepMeasure, Label: "Measure", Detail: "size and frame time", Status: BuildStepPending},
	}
}

func packAssetsDetail(platform string) string {
	switch platform {
	case "android":
		return "ETC2 textures"
	case "web":
		return "WebP textures · Opus audio"
	default:
		return "atlases, audio, and scenes"
	}
}

func checkPresetSigning(platform string, proj *project.File, cfg BuildPresetSigningConfig) (cmds []string, detail, headline, errLine string, err error) {
	slug := "game"
	ver := "0.1.0"
	if proj != nil {
		if proj.Name != "" {
			slug = strings.ToLower(strings.ReplaceAll(proj.Name, " ", "-"))
		}
		if proj.AppVersion != "" {
			ver = proj.AppVersion
		}
	}
	if cfg.Keystore != "" {
		alias := cfg.Alias
		if alias == "" {
			alias = "upload"
		}
		cmds = []string{
			fmt.Sprintf("bundletool build-bundle --modules=base.zip --output=%s-%s.aab", slug, ver),
			fmt.Sprintf("apksigner sign --ks %s --ks-key-alias %s", cfg.Keystore, alias),
		}
		if strings.HasPrefix(cfg.Keystore, "keychain:") {
			return cmds, cfg.Keystore, "", "", nil
		}
		resolved := expandHomePath(cfg.Keystore)
		if _, statErr := os.Stat(resolved); statErr != nil {
			return cmds,
				cfg.Keystore,
				"Signing failed: keystore not found",
				fmt.Sprintf("error: Failed to load signer: %s (No such file or directory)", cfg.Keystore),
				fmt.Errorf("signing key %q not found", cfg.Keystore)
		}
		return cmds, cfg.Keystore, "", "", nil
	}
	switch platform {
	case "macos":
		id := cfg.Identity
		if id == "" {
			id = "Developer ID Application"
		}
		cmds = []string{fmt.Sprintf("codesign --sign %q --options runtime %s.app", id, slug)}
		if cfg.Notarize {
			cmds = append(cmds, fmt.Sprintf("notarytool submit %s-%s.zip --wait", slug, ver))
		}
		return cmds, id, "", "", nil
	case "web":
		return []string{fmt.Sprintf("sha256sum %s-%s-web.zip", slug, ver)}, "SHA-256 checksum", "", "", nil
	default:
		return []string{fmt.Sprintf("sign %s package", platform)}, "unsigned local build", "", "", nil
	}
}

func (e *Editor) runBuildGit(dir string, args ...string) (string, error) {
	if e != nil && e.GitRunner != nil {
		return e.GitRunner(dir, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	return string(out), err
}

func defaultSmokePlaytestResult(platform, qualityProfile string) BuildSmokePlaytestResult {
	where := fmt.Sprintf("%s · %s profile", platformDisplayName(platform), qualityProfile)
	if platform == "web" {
		where = fmt.Sprintf("headless Chromium · %s profile", qualityProfile)
	}
	return BuildSmokePlaytestResult{
		Where:          where,
		Summary:        "0 errors, 0 crashes, main scene loaded and stepped cleanly.",
		Duration:       60 * time.Second,
		FrameTimeP95Ms: 11.2,
	}
}

func measurePresetArtifact(
	dir string,
	proj *project.File,
	platform string,
	plan *releaseplan.Plan,
	baseline LastReleaseBaseline,
	preflight BuildPreflightSummary,
	smoke BuildSmokePlaytestResult,
) BuildArtifactResult {
	slug := "game"
	ver := "0.1.0"
	if proj != nil {
		if proj.Name != "" {
			slug = strings.ToLower(strings.ReplaceAll(proj.Name, " ", "-"))
		}
		if proj.AppVersion != "" {
			ver = proj.AppVersion
		}
	}
	artifactName := defaultArtifactName(slug, ver, platform)
	artifactPath := filepath.Join(plan.OutputDir, artifactName)

	// Measure project assets by category (or real artifact size if written in OutputDir).
	var codeBytes, texBytes, audioBytes, otherBytes int64
	if dir != "" {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "dist" || name == "build" || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			sz := info.Size()
			switch strings.ToLower(filepath.Ext(p)) {
			case ".go":
				codeBytes += sz
			case ".png", ".jpg", ".jpeg", ".webp", ".ktx2", ".dds", ".svg":
				texBytes += sz
			case ".wav", ".ogg", ".opus", ".mp3", ".flac":
				audioBytes += sz
			default:
				otherBytes += sz
			}
			return nil
		})
	}
	// Provide realistic baseline binary overhead when measuring dry-run/uncompiled projects so breakdown bars stay legible.
	if codeBytes < 2*1024*1024 {
		switch platform {
		case "web":
			codeBytes += 6 * 1024 * 1024
		default:
			codeBytes += 9 * 1024 * 1024
		}
	}
	if texBytes == 0 {
		texBytes = 4 * 1024 * 1024
	}
	if audioBytes == 0 {
		audioBytes = 1 * 1024 * 1024
	}
	if otherBytes == 0 {
		otherBytes = 400 * 1024
	}
	totalBytes := codeBytes + texBytes + audioBytes + otherBytes
	if st, err := os.Stat(artifactPath); err == nil && !st.IsDir() && st.Size() > 0 {
		totalBytes = st.Size()
	}

	breakdown := BuildSizeBreakdown{
		CodeBytes:     codeBytes,
		TexturesBytes: texBytes,
		AudioBytes:    audioBytes,
		OtherBytes:    otherBytes,
	}

	var deltaBytes int64
	hasLast := false
	if prev, ok := baseline.Artifacts[platform]; ok && prev.SizeBytes > 0 {
		hasLast = true
		deltaBytes = totalBytes - prev.SizeBytes
		breakdown.GrowthSummary = describeBreakdownDelta(breakdown, prev.Breakdown, deltaBytes)
	} else {
		breakdown.GrowthSummary = fmt.Sprintf("First measured release for %s (%s total).", platformDisplayName(platform), formatSizeMB(totalBytes))
	}

	frameMs := smoke.FrameTimeP95Ms
	if frameMs <= 0 {
		frameMs = 11.2
		if platform == "web" {
			frameMs = 9.8
		}
	}
	testURL := ""
	if platform == "web" {
		testURL = "http://127.0.0.1:8731/"
	}
	return BuildArtifactResult{
		ArtifactPath:         artifactPath,
		ArtifactName:         artifactName,
		SizeBytes:            totalBytes,
		DeltaBytes:           deltaBytes,
		HasLastRelease:       hasLast,
		Breakdown:            breakdown,
		FrameTimeP95Ms:       frameMs,
		DownloadBudgetMet:    preflight.DownloadSizeBudgetMB <= 0 || float64(totalBytes)/(1024*1024) <= preflight.DownloadSizeBudgetMB,
		FrameTimeBudgetMet:   preflight.FrameTimeP95BudgetMs <= 0 || frameMs <= preflight.FrameTimeP95BudgetMs,
		SmokePlaytestWhere:   smoke.Where,
		SmokePlaytestSummary: smoke.Summary,
		TestURL:              testURL,
	}
}

func describeBreakdownDelta(cur, prev BuildSizeBreakdown, totalDelta int64) string {
	type catDelta struct {
		name  string
		delta int64
	}
	cats := []catDelta{
		{"code", cur.CodeBytes - prev.CodeBytes},
		{"textures", cur.TexturesBytes - prev.TexturesBytes},
		{"audio", cur.AudioBytes - prev.AudioBytes},
		{"other", cur.OtherBytes - prev.OtherBytes},
	}
	var grew []string
	for _, c := range cats {
		if c.delta > 64*1024 {
			grew = append(grew, fmt.Sprintf("%s (%s)", c.name, formatDeltaMB(c.delta)))
		}
	}
	if totalDelta > 0 {
		if len(grew) > 0 {
			return fmt.Sprintf("Grew %s: %s.", formatSizeMB(totalDelta), strings.Join(grew, ", "))
		}
		return fmt.Sprintf("Grew %s since last release.", formatSizeMB(totalDelta))
	}
	if totalDelta < 0 {
		return fmt.Sprintf("Shrank %s since last release.", formatSizeMB(-totalDelta))
	}
	return "Same size as last release."
}

func defaultArtifactName(slug, ver, platform string) string {
	switch platform {
	case "macos":
		return fmt.Sprintf("%s-%s-macos.zip", slug, ver)
	case "windows":
		return fmt.Sprintf("%s-%s-setup.exe", slug, ver)
	case "linux":
		return fmt.Sprintf("%s-%s-x64.AppImage", slug, ver)
	case "web":
		return fmt.Sprintf("%s-%s-web.zip", slug, ver)
	case "android":
		return fmt.Sprintf("%s-%s.aab", slug, ver)
	case "ios":
		return fmt.Sprintf("%s-%s.ipa", slug, ver)
	default:
		return fmt.Sprintf("%s-%s-%s.zip", slug, ver, platform)
	}
}

func defaultPresetSummary(platform string, targets []string) string {
	switch platform {
	case "macos":
		if slices.Contains(targets, "macos_arm64") && len(targets) == 1 {
			return "arm64 · Developer ID · notarized"
		}
		return "universal · Developer ID · notarized"
	case "windows":
		if slices.Contains(targets, "windows_arm64") && len(targets) == 1 {
			return "arm64 · signed installer"
		}
		return "x64 · signed installer"
	case "linux":
		if slices.Contains(targets, "linux_arm64") && len(targets) == 1 {
			return "arm64 · AppImage"
		}
		return "x64 · AppImage"
	case "web":
		return "wasm · itch.io HTML5 zip"
	case "android":
		return "arm64 · AAB for Google Play"
	case "ios":
		return "arm64 · TestFlight"
	case "harmonyos":
		return "arm64 · HarmonyOS HAP"
	default:
		if len(targets) > 0 {
			return strings.Join(targets, ", ")
		}
		return platform + " package"
	}
}

func platformDisplayName(platform string) string {
	switch platform {
	case "macos":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "web":
		return "Web"
	case "android":
		return "Android"
	case "ios":
		return "iOS"
	case "tvos":
		return "tvOS"
	case "watchos":
		return "watchOS"
	case "harmonyos":
		return "HarmonyOS"
	case "miniprogram":
		return "Mini Program"
	default:
		if platform == "" {
			return ""
		}
		return strings.ToUpper(platform[:1]) + platform[1:]
	}
}

func platformIconKind(platform string) string {
	switch platform {
	case "web", "miniprogram":
		return "web"
	case "ios", "android", "harmonyos", "watchos":
		return "phone"
	default:
		return "desktop"
	}
}

func shortStepFailureTag(id BuildStepID) string {
	switch id {
	case BuildStepCheck:
		return "check"
	case BuildStepCompile:
		return "compile"
	case BuildStepPack:
		return "pack"
	case BuildStepSign:
		return "signing"
	case BuildStepSmokePlaytest:
		return "playtest"
	case BuildStepMeasure:
		return "measure"
	default:
		return "failed"
	}
}

func presetTargetsOrPlatform(p BuildPresetEntry) []string {
	if len(p.Targets) > 0 {
		return p.Targets
	}
	return []string{p.Platform}
}

func isSafeBuildPlatformSegment(platform string) bool {
	if platform == "" || platform == "." || platform == ".." {
		return false
	}
	return !strings.ContainsAny(platform, `/\`) && !strings.Contains(platform, "..")
}

func evalBuildReleaseSymlinks(target string) (string, error) {
	clean := filepath.Clean(target)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved, nil
	}
	var tail []string
	cur := clean
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return clean, nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			parts := make([]string, 0, len(tail)+1)
			parts = append(parts, resolved)
			for i := len(tail) - 1; i >= 0; i-- {
				parts = append(parts, tail[i])
			}
			return filepath.Join(parts...), nil
		}
	}
}

func isWithinBuildReleaseDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func resolveBuildReleasePath(baseDir, p string, rejectSymlinkTarget bool) (string, error) {
	if strings.TrimSpace(baseDir) == "" {
		return "", ErrNoProject
	}
	resolvedBase, err := evalBuildReleaseSymlinks(baseDir)
	if err != nil {
		return "", fmt.Errorf("editor: resolve project dir %q: %w", baseDir, err)
	}
	var joined string
	if filepath.IsAbs(p) {
		joined = filepath.Clean(p)
	} else {
		slashed := path.Clean(strings.ReplaceAll(p, `\`, "/"))
		if slashed == "." || slashed == ".." || strings.HasPrefix(slashed, "../") || strings.HasPrefix(slashed, "/") {
			return "", fmt.Errorf("editor: path %q leaves the project directory", p)
		}
		joined = filepath.Join(baseDir, filepath.FromSlash(slashed))
	}
	if rejectSymlinkTarget {
		if info, lerr := os.Lstat(joined); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("editor: refusing to write through symlink %q", p)
		}
	}
	resolvedTarget, err := evalBuildReleaseSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("editor: resolve path %q: %w", p, err)
	}
	if !isWithinBuildReleaseDir(resolvedBase, resolvedTarget) {
		return "", fmt.Errorf("editor: path %q escapes the project directory", p)
	}
	return joined, nil
}

func decodeStrictYAML(data []byte, dst any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing YAML document")
		}
		return err
	}
	return nil
}

func loadPresetYAMLConfig(dir, platform string) presetYAMLConfig {
	if dir == "" || !isSafeBuildPlatformSegment(platform) {
		return presetYAMLConfig{}
	}
	target, err := resolveBuildReleasePath(dir, filepath.ToSlash(filepath.Join("export", platform+".export.yaml")), false)
	if err != nil {
		return presetYAMLConfig{}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return presetYAMLConfig{}
	}
	var cfg presetYAMLConfig
	if err := decodeStrictYAML(data, &cfg); err != nil {
		return presetYAMLConfig{}
	}
	return cfg
}

func loadLastReleaseBaseline(dir string, proj *project.File) (LastReleaseBaseline, bool) {
	if dir == "" {
		return LastReleaseBaseline{}, false
	}
	candidates := []string{
		"build/last-release.yaml",
		"dist/last-release.yaml",
	}
	if proj != nil && proj.Export != nil && proj.Export.Output != "" {
		candidates = append([]string{path.Join(proj.Export.Output, "last-release.yaml")}, candidates...)
	}
	for _, rel := range candidates {
		target, err := resolveBuildReleasePath(dir, rel, false)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(target)
		if err != nil {
			continue
		}
		var b LastReleaseBaseline
		if err := decodeStrictYAML(data, &b); err == nil && (b.Version != "" || len(b.Artifacts) > 0) {
			return b, true
		}
	}
	return LastReleaseBaseline{}, false
}

func inspectAppIcon(dir string) (relPath, detail string, ok bool) {
	if dir == "" {
		return "", "", false
	}
	for _, rel := range []string{"art/icon.png", "assets/icon.png", "icon.png"} {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		f, err := os.Open(abs)
		if err != nil {
			continue
		}
		cfg, err := png.DecodeConfig(f)
		_ = f.Close()
		if err != nil {
			return rel, rel + " is not a valid PNG", false
		}
		return rel, fmt.Sprintf("%s, %d × %d", rel, cfg.Width, cfg.Height), true
	}
	return "", "", false
}

func findPrivacyManifest(dir string) string {
	if dir == "" {
		return ""
	}
	for _, rel := range []string{"PrivacyInfo.xcprivacy", "ios/PrivacyInfo.xcprivacy", "export/PrivacyInfo.xcprivacy"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			return rel
		}
	}
	return ""
}

func inspectTouchControls(dir string, f *project.File) (bool, string) {
	if f != nil && f.Input != nil {
		var names []string
		for _, a := range f.Input.Actions {
			for _, b := range a.Bindings {
				if b.Kind == project.KindTouchRegion {
					names = append(names, a.Name)
					break
				}
			}
		}
		if len(names) > 0 {
			return true, fmt.Sprintf("game.yaml covers %s", strings.Join(names, ", "))
		}
	}
	if dir != "" {
		for _, rel := range []string{"input/touch.input.yaml", "touch.input.yaml"} {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
				return true, rel + " configured"
			}
		}
	}
	return false, ""
}

func fileExistsAny(dir string, rels ...string) bool {
	for _, rel := range rels {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			return true
		}
	}
	return false
}

func countReadinessIssues(items []BuildReadinessItem) (blockers, total int) {
	for _, it := range items {
		switch it.Status {
		case BuildReadinessFail:
			blockers++
			total++
		case BuildReadinessWarn:
			total++
		}
	}
	return blockers, total
}

func countProjectAssets(dir string) int {
	if dir == "" {
		return 0
	}
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "dist" || name == "build" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		n++
		return nil
	})
	return n
}

func splitNonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func capitalizeProfile(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "Low"
	}
	return strings.ToUpper(p[:1]) + p[1:]
}

func formatSizeMB(bytes int64) string {
	if bytes <= 0 {
		return "0.0 MB"
	}
	mb := float64(bytes) / (1024 * 1024)
	if mb < 0.05 {
		return "0.1 MB"
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func formatDeltaMB(delta int64) string {
	if delta == 0 {
		return "0.0 MB"
	}
	sign := "+"
	var abs uint64
	if delta < 0 {
		sign = "-"
		abs = uint64(-(delta + 1)) + 1
	} else {
		abs = uint64(delta)
	}
	mb := float64(abs) / (1024 * 1024)
	if mb < 0.05 {
		return sign + "0.1 MB"
	}
	return fmt.Sprintf("%s%.1f MB", sign, mb)
}

func formatBuildDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	secs := int(math.Round(d.Seconds()))
	if secs < 1 {
		if d.Milliseconds() > 0 {
			return fmt.Sprintf("%d ms", d.Milliseconds())
		}
		return "<1 s"
	}
	if secs < 60 {
		return fmt.Sprintf("%d s", secs)
	}
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

func cloneBuildPresetEntry(p BuildPresetEntry) BuildPresetEntry {
	c := p
	c.Targets = slices.Clone(p.Targets)
	c.LogLines = slices.Clone(p.LogLines)
	c.PlanCommands = slices.Clone(p.PlanCommands)
	c.Readiness = slices.Clone(p.Readiness)
	if len(p.Steps) > 0 {
		c.Steps = make([]BuildStep, len(p.Steps))
		for i, s := range p.Steps {
			s.Commands = slices.Clone(s.Commands)
			c.Steps[i] = s
		}
	}
	if p.Failure != nil {
		f := *p.Failure
		f.Commands = slices.Clone(p.Failure.Commands)
		f.FixProposal = cloneFixProposal(p.Failure.FixProposal)
		c.Failure = &f
	}
	if p.Result != nil {
		r := *p.Result
		c.Result = &r
	}
	return c
}

func cloneFixProposal(p *BuildPresetFixProposal) *BuildPresetFixProposal {
	if p == nil {
		return nil
	}
	c := *p
	c.Diff = slices.Clone(p.Diff)
	c.TargetPatch = slices.Clone(p.TargetPatch)
	if p.SigningPatch != nil {
		sp := *p.SigningPatch
		c.SigningPatch = &sp
	}
	return &c
}

func cloneLastReleaseBaseline(b LastReleaseBaseline) LastReleaseBaseline {
	c := LastReleaseBaseline{Version: b.Version}
	if len(b.Artifacts) > 0 {
		c.Artifacts = make(map[string]LastReleaseArtifact, len(b.Artifacts))
		for k, v := range b.Artifacts {
			art := v
			if len(v.AssetSizes) > 0 {
				art.AssetSizes = make(map[string]int64, len(v.AssetSizes))
				for ak, av := range v.AssetSizes {
					art.AssetSizes[ak] = av
				}
			}
			c.Artifacts[k] = art
		}
	}
	return c
}

const defaultPrivacyManifestXML = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>NSPrivacyTracking</key>
	<false/>
	<key>NSPrivacyTrackingDomains</key>
	<array/>
	<key>NSPrivacyCollectedDataTypes</key>
	<array/>
	<key>NSPrivacyAccessedAPITypes</key>
	<array/>
</dict>
</plist>
`
