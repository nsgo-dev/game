package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/credits"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// ErrExportRunning is returned by StartExportBuild when a release build or dry run is already running.
var ErrExportRunning = errors.New("editor: an export build is already running")

// ExportBuildOptions configures one release build or dry run launched from the Export panel.
type ExportBuildOptions struct {
	// Targets restricts the build to the chosen targets; empty builds all presets in game.yaml (or game release's defaults when no presets are defined).
	Targets []string
	// Channel selects the distribution channel ("", "direct", "store", "steam", "none"). Empty uses nsgo.yaml's updates.channel.
	Channel string
	// ReleaseNotes is an optional path (project-relative or absolute) to a Markdown file passed to -release-notes.
	ReleaseNotes string
	// DryRun prints the nsgo build command without running nsgo build or writing CREDITS.md.
	DryRun bool
}

// ExportCreditsReport summarizes the pre-flight credits check for the open project's assets section.
type ExportCreditsReport struct {
	// Configured reports whether game.yaml declares an assets section.
	Configured bool
	// Entries are the credited assets found.
	Entries []credits.Entry
	// Missing are the slash-separated paths of assets without a credit, sorted.
	Missing []string
	// Err is any non-missing error encountered while scanning assets or parsing sidecars.
	Err error
}

// OK reports whether the credits pre-flight check permits a release build.
func (r ExportCreditsReport) OK() bool {
	return r.Err == nil && len(r.Missing) == 0
}

type exportBuildRun struct {
	project   string
	cancel    context.CancelFunc
	done      chan struct{}
	doneOnce  sync.Once
	dryRun    bool
	outputDir string
	canceled  bool
	toast     *ToastHandle
	progress  float32
	linesSeen int
}

func (r *exportBuildRun) finish() {
	r.doneOnce.Do(func() {
		close(r.done)
	})
}

type exportPanelState struct {
	mu                 sync.Mutex
	selectedPlatform   string
	selectedTargets    []string
	channel            string
	releaseNotes       string
	dryRun             bool
	credits            ExportCreditsReport
	appleTeamIDPath    string
	appleTeamIDChecked bool
	appleTeamIDReady   bool
	output             []string
	lastErr            error
	lastOutputDir      string
	lastCommandLine    string
	run                *exportBuildRun
}

// ExportPanelOpen reports whether the Settings view is open on the Export page.
func (e *Editor) ExportPanelOpen() bool {
	return e != nil && e.settingsOpen && e.settingsPage == SettingsPageExport
}

// SetExportPanelOpen opens the Settings view on the Export page (refreshing the pre-flight credits check), or closes Settings when open is false and the Export page is active.
func (e *Editor) SetExportPanelOpen(open bool) {
	if e == nil {
		return
	}
	if open {
		e.OpenSettingsPage(SettingsPageExport)
		return
	}
	if e.ExportPanelOpen() {
		e.CloseSettings()
	}
}

// ExportExperimentalInfo describes an experimental export preset's missing capabilities and documentation links.
type ExportExperimentalInfo struct {
	Platform string
	Missing  string
	DocsURL  string
	IssueURL string
}

// ExportPresetExperimental returns experimental platform metadata for platform, or false when platform is a primary target.
func (e *Editor) ExportPresetExperimental(platform string) (ExportExperimentalInfo, bool) {
	info, ok := releaseplan.ExperimentalInfoFor(platform)
	if !ok {
		return ExportExperimentalInfo{}, false
	}
	lang := "en"
	if e != nil {
		lang = e.ResolvedLanguage()
	}
	return ExportExperimentalInfo{
		Platform: info.Platform,
		Missing:  info.Missing,
		DocsURL:  releaseplan.ExperimentalDocsURL(lang),
		IssueURL: info.IssueURL,
	}, true
}

// OpenExperimentalPlatformDocs opens the platform support documentation's experimental targets section in the host browser (via OpenBrowserURL when set).
func (e *Editor) OpenExperimentalPlatformDocs() error {
	lang := "en"
	if e != nil {
		lang = e.ResolvedLanguage()
	}
	targetURL := releaseplan.ExperimentalDocsURL(lang)
	opener := defaultOpenBrowserURL
	if e != nil && e.OpenBrowserURL != nil {
		opener = e.OpenBrowserURL
	}
	return opener(targetURL)
}

// ExportPresetReadiness reports whether a platform export preset is ready to build ("Ready") or needs setup ("Set up"), along with a concise detail string matching design/editor/Settings.dc.html.
func (e *Editor) ExportPresetReadiness(platform string) (ready bool, detail string) {
	switch platform {
	case "ios", "tvos", "watchos":
		if !e.nsgoHasAppleTeamID() {
			return false, "Needs a team ID"
		}
	}
	if e != nil {
		if c := e.ExportCredits(); c.Configured && !c.OK() {
			return false, "Needs asset credits"
		}
	}
	if e != nil && e.Project != nil {
		if cur, ok := e.Project.Preset(platform); ok && len(cur.Targets) > 0 {
			return true, strings.Join(cur.Targets, ", ")
		}
	}
	switch platform {
	case "macos":
		return true, "Universal, macOS targets"
	case "windows":
		return true, "Windows x64 / arm64"
	case "linux":
		return true, "Linux x64 / arm64"
	case "web":
		return true, "WebAssembly, compressed"
	case "android":
		return true, "Android arm64 / x64"
	default:
		return true, "default (" + platform + ")"
	}
}

// ExportNavWarningMeta returns the warning badge label for the Export page in the Settings navigation sidebar (for example "iOS" when an iOS preset needs a team ID, or "Credits" when asset credits are missing), or empty when every configured preset is ready.
func (e *Editor) ExportNavWarningMeta() string {
	if e == nil || e.Project == nil {
		return ""
	}
	for _, p := range e.ExportPresets() {
		if ready, _ := e.ExportPresetReadiness(p.Platform); !ready {
			return exportPlatformDisplayName(p.Platform)
		}
	}
	if c := e.ExportCredits(); c.Configured && !c.OK() {
		return "Credits"
	}
	return ""
}

func exportPlatformDisplayName(platform string) string {
	switch platform {
	case "macos":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "ios":
		return "iOS"
	case "tvos":
		return "tvOS"
	case "watchos":
		return "watchOS"
	case "android":
		return "Android"
	case "harmonyos":
		return "HarmonyOS"
	case "web":
		return "Web"
	case "miniprogram":
		return "Mini Program"
	default:
		return platform
	}
}

const maxNSGoReadBytes = 1 << 20 // 1 MiB safety cap for reading nsgo.yaml metadata

func (e *Editor) resetExportPanelCache() {
	if e == nil {
		return
	}
	e.exportPanel.mu.Lock()
	e.exportPanel.credits = ExportCreditsReport{}
	e.exportPanel.appleTeamIDPath = ""
	e.exportPanel.appleTeamIDChecked = false
	e.exportPanel.appleTeamIDReady = false
	e.exportPanel.mu.Unlock()
}

func (e *Editor) refreshAppleTeamIDCache() bool {
	if e == nil {
		return false
	}
	projPath := e.ProjectPath
	ready := false
	if projPath != "" {
		ready = readNSGoHasAppleTeamID(filepath.Dir(projPath))
	}
	e.exportPanel.mu.Lock()
	e.exportPanel.appleTeamIDPath = projPath
	e.exportPanel.appleTeamIDChecked = true
	e.exportPanel.appleTeamIDReady = ready
	e.exportPanel.mu.Unlock()
	return ready
}

func readNSGoHasAppleTeamID(projectDir string) bool {
	if projectDir == "" {
		return false
	}
	nsgoPath := filepath.Join(projectDir, project.NSGoFileName)
	info, err := os.Lstat(nsgoPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxNSGoReadBytes {
		return false
	}
	f, err := os.Open(nsgoPath)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxNSGoReadBytes+1))
	if err != nil || len(data) > maxNSGoReadBytes {
		return false
	}
	var cfg struct {
		TeamID string `yaml:"team_id"`
		Apple  struct {
			TeamID string `yaml:"team_id"`
		} `yaml:"apple"`
		IOS struct {
			TeamID string `yaml:"team_id"`
		} `yaml:"ios"`
		Signing struct {
			TeamID string `yaml:"team_id"`
			Apple  struct {
				TeamID string `yaml:"team_id"`
			} `yaml:"apple"`
		} `yaml:"signing"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false
	}
	return strings.TrimSpace(cfg.TeamID) != "" ||
		strings.TrimSpace(cfg.Apple.TeamID) != "" ||
		strings.TrimSpace(cfg.IOS.TeamID) != "" ||
		strings.TrimSpace(cfg.Signing.TeamID) != "" ||
		strings.TrimSpace(cfg.Signing.Apple.TeamID) != ""
}

func (e *Editor) nsgoHasAppleTeamID() bool {
	if e == nil || e.ProjectPath == "" {
		return false
	}
	e.exportPanel.mu.Lock()
	if e.exportPanel.appleTeamIDChecked && e.exportPanel.appleTeamIDPath == e.ProjectPath {
		ready := e.exportPanel.appleTeamIDReady
		e.exportPanel.mu.Unlock()
		return ready
	}
	e.exportPanel.mu.Unlock()
	return e.refreshAppleTeamIDCache()
}

// ExportOutput returns the open project's export.output directory, or empty when unset.
func (e *Editor) ExportOutput() string {
	if e == nil || e.Project == nil || e.Project.Export == nil {
		return ""
	}
	return e.Project.Export.Output
}

// SetExportOutput sets game.yaml's export.output directory through EditProject as one undoable step. Clearing it when no presets are configured removes the empty export section.
func (e *Editor) SetExportOutput(output string) error {
	if e == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if strings.Contains(output, ":") {
		return fmt.Errorf("project: export output %q must be a slash-separated directory inside the project", output)
	}
	if output == e.ExportOutput() {
		return nil
	}
	return e.EditProject("Set export output", func(f *project.File) {
		if output == "" {
			if f.Export != nil {
				f.Export.Output = ""
				cleanupEmptyExport(f)
			}
			return
		}
		ensureExport(f).Output = output
	})
}

// ExportPresets returns a cloned slice of the open project's export presets in build order.
func (e *Editor) ExportPresets() []project.Preset {
	if e == nil || e.Project == nil || e.Project.Export == nil || len(e.Project.Export.Presets) == 0 {
		return nil
	}
	out := make([]project.Preset, len(e.Project.Export.Presets))
	for i, p := range e.Project.Export.Presets {
		out[i] = project.Preset{
			Platform: p.Platform,
			Targets:  slices.Clone(p.Targets),
		}
	}
	return out
}

// AvailableExportPlatforms returns the platforms from project.Platforms that do not yet have an export preset in the open project.
func (e *Editor) AvailableExportPlatforms() []string {
	existing := make(map[string]bool)
	if e != nil && e.Project != nil && e.Project.Export != nil {
		for _, p := range e.Project.Export.Presets {
			existing[p.Platform] = true
		}
	}
	var out []string
	for _, plat := range project.Platforms {
		if !existing[plat] {
			out = append(out, plat)
		}
	}
	return out
}

// SelectedExportPlatform returns the currently selected preset platform in the Export panel, falling back to the first declared preset.
func (e *Editor) SelectedExportPlatform() string {
	if e == nil || e.Project == nil || e.Project.Export == nil || len(e.Project.Export.Presets) == 0 {
		return ""
	}
	if e.exportPanel.selectedPlatform != "" {
		for _, p := range e.Project.Export.Presets {
			if p.Platform == e.exportPanel.selectedPlatform {
				return p.Platform
			}
		}
	}
	return e.Project.Export.Presets[0].Platform
}

// SelectExportPlatform selects platform in the Export panel when it has a preset in the open project.
func (e *Editor) SelectExportPlatform(platform string) {
	if e == nil || e.findExportPresetIndex(platform) < 0 {
		return
	}
	if e.exportPanel.selectedPlatform == platform {
		return
	}
	e.exportPanel.selectedPlatform = platform
	e.notify()
}

// AddExportPreset appends a new export preset for platform (with optional targets chosen from project.TargetsOf(platform)) through EditProject as one undoable step.
func (e *Editor) AddExportPreset(platform string, targets ...string) error {
	if e == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	var targetCopy []string
	if len(targets) > 0 {
		targetCopy = slices.Clone(targets)
	}
	if err := e.EditProject("Add export preset "+platform, func(f *project.File) {
		ex := ensureExport(f)
		ex.Presets = append(ex.Presets, project.Preset{
			Platform: platform,
			Targets:  targetCopy,
		})
	}); err != nil {
		return err
	}
	e.exportPanel.selectedPlatform = platform
	e.notify()
	return nil
}

// RemoveExportPreset deletes the export preset for platform through EditProject as one undoable step. Removing the last preset when export.output is empty removes the empty export section.
func (e *Editor) RemoveExportPreset(platform string) error {
	if e == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findExportPresetIndex(platform)
	if idx < 0 {
		return fmt.Errorf("editor: export preset for platform %q does not exist", platform)
	}
	return e.EditProject("Remove export preset "+platform, func(f *project.File) {
		f.Export.Presets = slices.Delete(f.Export.Presets, idx, idx+1)
		if len(f.Export.Presets) == 0 {
			f.Export.Presets = nil
		}
		cleanupEmptyExport(f)
	})
}

// MoveExportPreset moves platform's export preset by delta positions (-1 up, +1 down) through EditProject.
func (e *Editor) MoveExportPreset(platform string, delta int) error {
	if e == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findExportPresetIndex(platform)
	if idx < 0 {
		return fmt.Errorf("editor: export preset for platform %q does not exist", platform)
	}
	next := idx + delta
	if next < 0 || next >= len(e.Project.Export.Presets) || next == idx {
		return nil
	}
	return e.EditProject("Reorder export preset "+platform, func(f *project.File) {
		f.Export.Presets[idx], f.Export.Presets[next] = f.Export.Presets[next], f.Export.Presets[idx]
	})
}

// SetExportPresetTargets replaces platform's target list through EditProject. An empty slice builds the platform's default targets. Every target must belong to project.TargetsOf(platform).
func (e *Editor) SetExportPresetTargets(platform string, targets []string) error {
	if e == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findExportPresetIndex(platform)
	if idx < 0 {
		return fmt.Errorf("editor: export preset for platform %q does not exist", platform)
	}
	var cleaned []string
	if len(targets) > 0 {
		cleaned = slices.Clone(targets)
	}
	if slices.Equal(e.Project.Export.Presets[idx].Targets, cleaned) {
		return nil
	}
	return e.EditProject("Set export targets for "+platform, func(f *project.File) {
		f.Export.Presets[idx].Targets = cleaned
	})
}

// ToggleExportPresetTarget adds or removes target on platform's export preset in canonical project.TargetsOf(platform) order through EditProject.
func (e *Editor) ToggleExportPresetTarget(platform, target string) error {
	if e == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	idx := e.findExportPresetIndex(platform)
	if idx < 0 {
		return fmt.Errorf("editor: export preset for platform %q does not exist", platform)
	}
	valid := project.TargetsOf(platform)
	if !slices.Contains(valid, target) {
		return fmt.Errorf("project: export platform %q target %q must be one of %s", platform, target, strings.Join(valid, ", "))
	}
	cur := e.Project.Export.Presets[idx].Targets
	has := slices.Contains(cur, target)
	var next []string
	for _, candidate := range valid {
		if candidate == target {
			if !has {
				next = append(next, candidate)
			}
			continue
		}
		if slices.Contains(cur, candidate) {
			next = append(next, candidate)
		}
	}
	return e.SetExportPresetTargets(platform, next)
}

// EffectiveExportTargets returns the build targets that an "all presets" build targets for the open project: game.yaml's BuildTargets when presets are configured, otherwise SteamPlatforms for the steam channel or DefaultPlatforms.
func (e *Editor) EffectiveExportTargets() []string {
	if e != nil && e.Project != nil && e.Project.Export != nil && len(e.Project.Export.Presets) > 0 {
		return e.Project.BuildTargets()
	}
	channel := e.ExportChannel()
	if channel == "" && e != nil && e.ProjectPath != "" {
		if nsgo, err := releaseplan.ReadNSGoSettings(filepath.Dir(e.ProjectPath)); err == nil {
			channel = nsgo.Channel
		}
	}
	if channel == "steam" {
		return slices.Clone(releaseplan.SteamPlatforms)
	}
	return slices.Clone(releaseplan.DefaultPlatforms)
}

// ExportChannel returns the distribution channel selected in the Export panel ("" means nsgo.yaml's default).
func (e *Editor) ExportChannel() string {
	if e == nil {
		return ""
	}
	return e.exportPanel.channel
}

// SetExportChannel sets the distribution channel in the Export panel ("", "direct", "store", "steam", or "none").
func (e *Editor) SetExportChannel(channel string) error {
	if e == nil {
		return nil
	}
	if channel != "" && !slices.Contains(releaseplan.Channels, channel) {
		return fmt.Errorf("editor: unknown export channel %q (want one of %s or empty)", channel, strings.Join(releaseplan.Channels, ", "))
	}
	if e.exportPanel.channel == channel {
		return nil
	}
	e.exportPanel.channel = channel
	e.notify()
	return nil
}

// ExportReleaseNotes returns the release notes file path configured in the Export panel.
func (e *Editor) ExportReleaseNotes() string {
	if e == nil {
		return ""
	}
	return e.exportPanel.releaseNotes
}

// SetExportReleaseNotes sets the release notes Markdown file path in the Export panel.
func (e *Editor) SetExportReleaseNotes(path string) {
	if e == nil || e.exportPanel.releaseNotes == path {
		return
	}
	e.exportPanel.releaseNotes = path
	e.notify()
}

// ExportDryRun reports whether the Export panel's Dry Run toggle is enabled.
func (e *Editor) ExportDryRun() bool {
	return e != nil && e.exportPanel.dryRun
}

// SetExportDryRun toggles dry-run mode in the Export panel.
func (e *Editor) SetExportDryRun(dryRun bool) {
	if e == nil || e.exportPanel.dryRun == dryRun {
		return
	}
	e.exportPanel.dryRun = dryRun
	e.notify()
}

// ExportSelectedTargets returns the subset of targets chosen for building in the Export panel, or nil when building all presets.
func (e *Editor) ExportSelectedTargets() []string {
	if e == nil || len(e.exportPanel.selectedTargets) == 0 {
		return nil
	}
	return slices.Clone(e.exportPanel.selectedTargets)
}

// SetExportSelectedTargets restricts builds from the Export panel to targets; an empty slice builds all presets.
func (e *Editor) SetExportSelectedTargets(targets []string) {
	if e == nil {
		return
	}
	var cleaned []string
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t != "" && !slices.Contains(cleaned, t) {
			cleaned = append(cleaned, t)
		}
	}
	if slices.Equal(e.exportPanel.selectedTargets, cleaned) {
		return
	}
	e.exportPanel.selectedTargets = cleaned
	e.notify()
}

// ToggleExportBuildTarget toggles target in the Export panel's chosen build target subset.
func (e *Editor) ToggleExportBuildTarget(target string) {
	if e == nil || target == "" {
		return
	}
	cur := slices.Clone(e.exportPanel.selectedTargets)
	if idx := slices.Index(cur, target); idx >= 0 {
		cur = slices.Delete(cur, idx, idx+1)
	} else {
		cur = append(cur, target)
	}
	e.SetExportSelectedTargets(cur)
}

// ClearExportSelectedTargets resets the build scope to all presets.
func (e *Editor) ClearExportSelectedTargets() {
	e.SetExportSelectedTargets(nil)
}

// ExportCredits returns the most recent pre-flight credits check report.
func (e *Editor) ExportCredits() ExportCreditsReport {
	if e == nil {
		return ExportCreditsReport{}
	}
	e.exportPanel.mu.Lock()
	defer e.exportPanel.mu.Unlock()
	return ExportCreditsReport{
		Configured: e.exportPanel.credits.Configured,
		Entries:    slices.Clone(e.exportPanel.credits.Entries),
		Missing:    slices.Clone(e.exportPanel.credits.Missing),
		Err:        e.exportPanel.credits.Err,
	}
}

// CheckExportCredits runs the same credits check as `game release` against the open project's assets section, caches the result for the Export panel, and returns it.
func (e *Editor) CheckExportCredits() ExportCreditsReport {
	if e == nil {
		return ExportCreditsReport{}
	}
	report := ExportCreditsReport{}
	if e.Project != nil && e.ProjectPath != "" {
		root := filepath.Dir(e.ProjectPath)
		if e.Project.Assets != nil {
			report.Configured = true
			entries, err := credits.Collect(root, e.Project.Assets)
			report.Entries = entries
			if err != nil {
				var missing *credits.MissingError
				if errors.As(err, &missing) {
					report.Missing = slices.Clone(missing.Assets)
				} else {
					report.Err = err
				}
			}
		} else if _, err := credits.CheckGeneratedLicenses(root); err != nil {
			report.Err = err
		}
	}
	e.refreshAppleTeamIDCache()
	e.exportPanel.mu.Lock()
	e.exportPanel.credits = report
	e.exportPanel.mu.Unlock()
	e.notify()
	return e.ExportCredits()
}

// ExportRunning reports whether a release build or dry run is currently in flight.
func (e *Editor) ExportRunning() bool {
	if e == nil {
		return false
	}
	e.exportPanel.mu.Lock()
	defer e.exportPanel.mu.Unlock()
	return e.exportPanel.run != nil
}

// ExportOutputLines returns a copy of every line currently in the Export panel's Output log.
func (e *Editor) ExportOutputLines() []string {
	if e == nil {
		return nil
	}
	e.exportPanel.mu.Lock()
	defer e.exportPanel.mu.Unlock()
	return slices.Clone(e.exportPanel.output)
}

// ExportOutputText returns the Export panel's Output log joined with newlines.
func (e *Editor) ExportOutputText() string {
	lines := e.ExportOutputLines()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// ClearExportOutput clears the Export panel's Output log.
func (e *Editor) ClearExportOutput() {
	if e == nil {
		return
	}
	e.exportPanel.mu.Lock()
	e.exportPanel.output = nil
	e.exportPanel.lastErr = nil
	e.exportPanel.mu.Unlock()
	e.notify()
}

// ExportLastError returns the error from the last completed or rejected release build, or nil.
func (e *Editor) ExportLastError() error {
	if e == nil {
		return nil
	}
	e.exportPanel.mu.Lock()
	defer e.exportPanel.mu.Unlock()
	return e.exportPanel.lastErr
}

// ExportLastOutputDir returns the resolved output directory of the last planned or completed build.
func (e *Editor) ExportLastOutputDir() string {
	if e == nil {
		return ""
	}
	e.exportPanel.mu.Lock()
	defer e.exportPanel.mu.Unlock()
	return e.exportPanel.lastOutputDir
}

// ExportLastCommandLine returns the exact POSIX-quoted `go ... tool nsgo build ...` command line from the most recent build or dry run.
func (e *Editor) ExportLastCommandLine() string {
	if e == nil {
		return ""
	}
	e.exportPanel.mu.Lock()
	defer e.exportPanel.mu.Unlock()
	return e.exportPanel.lastCommandLine
}

// ExportBuildOptionsFromPanel returns the ExportBuildOptions currently configured in the Export panel controls.
func (e *Editor) ExportBuildOptionsFromPanel() ExportBuildOptions {
	return ExportBuildOptions{
		Targets:      e.ExportSelectedTargets(),
		Channel:      e.ExportChannel(),
		ReleaseNotes: e.ExportReleaseNotes(),
		DryRun:       e.ExportDryRun(),
	}
}

// RunExportBuild launches a release build (or dry run) using the Export panel's current options.
func (e *Editor) RunExportBuild() error {
	return e.StartExportBuild(e.ExportBuildOptionsFromPanel())
}

// StartExportBuild validates the open project's release settings and asset credits, then executes `nsgo build` (or prints the exact dry-run command) off the UI goroutine, streaming output lines into the Output log and opening the output folder when a non-dry-run build succeeds.
func (e *Editor) StartExportBuild(opts ExportBuildOptions) error {
	if e == nil {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.ExportRunning() {
		return ErrExportRunning
	}
	if opts.Channel != "" && !slices.Contains(releaseplan.Channels, opts.Channel) {
		return fmt.Errorf("editor: unknown export channel %q (want one of %s or empty)", opts.Channel, strings.Join(releaseplan.Channels, ", "))
	}

	// Refresh credits pre-flight before planning so the panel displays any missing credits immediately.
	e.CheckExportCredits()
	e.TriggerBeforeExportAutomations(opts.Targets...)

	dir := filepath.Dir(e.ProjectPath)
	notes := strings.TrimSpace(opts.ReleaseNotes)
	if notes != "" && !filepath.IsAbs(notes) {
		notes = filepath.Join(dir, filepath.FromSlash(notes))
	}
	planOpts := releaseplan.Options{
		Targets: strings.Join(opts.Targets, ","),
		Channel: opts.Channel,
		Notes:   notes,
	}
	plan, err := releaseplan.Build(dir, planOpts, releaseplan.SettingsFromProject(e.Project))
	projectName := exportProjectDisplayName(e.Project, dir)
	if err != nil {
		e.exportPanel.mu.Lock()
		e.exportPanel.output = []string{"game release: " + err.Error()}
		e.exportPanel.lastErr = err
		e.exportPanel.lastCommandLine = ""
		e.exportPanel.mu.Unlock()
		e.Status = "Export blocked: " + err.Error()
		if !opts.DryRun {
			retryOpts := opts
			e.EmitToastEvent(ToastEvent{
				Kind:        ToastEventExportFailed,
				ID:          "toast:export",
				Message:     fmt.Sprintf("Couldn't export %s: %s", projectName, err.Error()),
				ActionLabel: "Retry",
				OnAction: func() {
					_ = e.StartExportBuild(retryOpts)
				},
			})
		}
		e.notify()
		return err
	}

	var initialLines []string
	for _, exp := range plan.Experimental {
		initialLines = append(initialLines, releaseplan.FormatExperimentalWarning(exp))
	}
	if !plan.AssetsConfigured {
		initialLines = append(initialLines, releaseplan.NoCreditsNote)
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &exportBuildRun{
		project:   e.ProjectPath,
		cancel:    cancel,
		done:      make(chan struct{}),
		dryRun:    opts.DryRun,
		outputDir: plan.OutputDir,
	}
	e.exportPanel.mu.Lock()
	e.exportPanel.run = run
	e.exportPanel.output = initialLines
	e.exportPanel.lastErr = nil
	e.exportPanel.lastOutputDir = plan.OutputDir
	e.exportPanel.lastCommandLine = plan.CommandLine
	e.exportPanel.mu.Unlock()

	if opts.DryRun {
		e.Status = "Running dry-run release…"
	} else {
		e.Status = "Building release…"
		targetLabel := exportToastTargetLabel(opts.Targets, e.EffectiveExportTargets())
		msg := fmt.Sprintf("Exporting %s", projectName)
		if targetLabel != "" {
			msg = fmt.Sprintf("Exporting %s for %s", projectName, targetLabel)
		}
		run.toast = e.EmitToastEvent(ToastEvent{
			Kind:        ToastEventExportRunning,
			ID:          "toast:export",
			Message:     msg,
			ActionLabel: "Cancel",
			OnCancel: func() {
				e.CancelExportBuild()
			},
		})
	}
	e.notify()

	runner := e.ExportCommandRunner
	if runner == nil {
		runner = defaultExportCommandRunner
	}
	opener := e.OpenOutputFolder
	if opener == nil {
		opener = defaultOpenOutputFolder
	}
	scope := e.scope
	retryOpts := opts
	displayOut := plan.OutputDir
	if rel, rerr := filepath.Rel(dir, plan.OutputDir); rerr == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, "..") {
		displayOut = filepath.ToSlash(rel)
	}

	go func() {
		defer cancel()
		var runErr error
		stream := newExportStreamWriter(func(line string) {
			e.postExportUI(scope, func() {
				var nextProgress float32
				var updateToast bool
				e.exportPanel.mu.Lock()
				active := e.exportPanel.run == run
				if active {
					e.exportPanel.output = append(e.exportPanel.output, line)
					if run.toast != nil {
						run.linesSeen++
						if parsed, ok := parseExportProgressPercent(line); ok {
							if parsed > run.progress {
								run.progress = parsed
							}
						} else {
							estimated := float32(1.0 - math.Pow(0.82, float64(run.linesSeen)))
							if estimated > 0.90 {
								estimated = 0.90
							}
							if estimated > run.progress {
								run.progress = estimated
							}
						}
						nextProgress = run.progress
						updateToast = true
					}
				}
				e.exportPanel.mu.Unlock()
				if active {
					if updateToast && run.toast != nil {
						run.toast.SetProgress(nextProgress, "")
					}
					e.notify()
				}
			})
		})

		if opts.DryRun {
			releaseplan.WriteDryRun(stream, plan)
			stream.Flush()
		} else {
			if err := runner(ctx, slices.Clone(plan.Args), stream, stream); err != nil {
				runErr = fmt.Errorf("nsgo build: %w", err)
			} else if plan.CreditsPath != "" {
				name, err := credits.Write(plan.OutputDir, plan.Credits)
				if err != nil {
					runErr = err
				} else {
					fmt.Fprintf(stream, "wrote the credits of %d assets to %s\n", len(plan.Credits), name)
				}
			}
			stream.Flush()
			if runErr != nil {
				e.postExportUI(scope, func() {
					e.exportPanel.mu.Lock()
					active := e.exportPanel.run == run
					if active {
						e.exportPanel.output = append(e.exportPanel.output, "game release: "+runErr.Error())
					}
					e.exportPanel.mu.Unlock()
					if active {
						e.notify()
					}
				})
			}
		}

		var openErr error
		if runErr == nil && !opts.DryRun && opener != nil {
			openErr = opener(plan.OutputDir)
		}

		e.postExportUI(scope, func() {
			e.exportPanel.mu.Lock()
			active := e.exportPanel.run == run
			wasCanceled := run.canceled || errors.Is(runErr, context.Canceled)
			if active {
				e.exportPanel.run = nil
				e.exportPanel.lastErr = runErr
				if openErr != nil {
					e.exportPanel.output = append(e.exportPanel.output, "export: open output folder: "+openErr.Error())
				}
			}
			e.exportPanel.mu.Unlock()
			if active {
				switch {
				case runErr != nil:
					e.Status = "Export failed: " + runErr.Error()
					if !opts.DryRun && run.toast != nil {
						if wasCanceled {
							run.toast.Dismiss()
						} else {
							run.toast.Fail(fmt.Sprintf("Couldn't export %s: %s", projectName, runErr.Error()), "Retry", func() {
								_ = e.StartExportBuild(retryOpts)
							})
						}
					}
				case opts.DryRun:
					e.Status = "Dry-run release complete"
				case openErr != nil:
					e.Status = "Release built to " + plan.OutputDir + " (could not open folder: " + openErr.Error() + ")"
					if run.toast != nil {
						outDir := plan.OutputDir
						run.toast.Complete("Exported "+displayOut, e.revealInFileManagerLabel(), "", func() {
							if opener != nil {
								_ = opener(outDir)
							}
						})
					}
				default:
					e.Status = "Release built to " + plan.OutputDir
					if run.toast != nil {
						outDir := plan.OutputDir
						run.toast.Complete("Exported "+displayOut, e.revealInFileManagerLabel(), "", func() {
							if opener != nil {
								_ = opener(outDir)
							}
						})
					}
				}
				e.notify()
			}
			run.finish()
		})
	}()

	return nil
}

// ExportToastHandle returns the active export build's *ToastHandle, or nil when no non-dry-run export is running.
func (e *Editor) ExportToastHandle() *ToastHandle {
	if e == nil {
		return nil
	}
	e.exportPanel.mu.Lock()
	defer e.exportPanel.mu.Unlock()
	if e.exportPanel.run == nil {
		return nil
	}
	return e.exportPanel.run.toast
}

func exportProjectDisplayName(proj *project.File, dir string) string {
	if proj != nil && strings.TrimSpace(proj.Name) != "" {
		return strings.TrimSpace(proj.Name)
	}
	if base := filepath.Base(dir); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return "project"
}

func exportToastTargetLabel(explicit, effective []string) string {
	targets := explicit
	if len(targets) == 0 {
		targets = effective
	}
	if len(targets) == 0 {
		return ""
	}
	var names []string
	for _, t := range targets {
		plat := t
		if idx := strings.IndexByte(t, '_'); idx > 0 {
			plat = t[:idx]
		}
		name := exportPlatformDisplayName(plat)
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func parseExportProgressPercent(line string) (float32, bool) {
	for _, tok := range strings.Fields(line) {
		trimmed := strings.Trim(tok, "()[].,;:")
		if !strings.HasSuffix(trimmed, "%") {
			continue
		}
		numStr := strings.TrimSuffix(trimmed, "%")
		v, err := strconv.ParseFloat(numStr, 32)
		if err != nil || v < 0 || v > 100 {
			continue
		}
		return clampToastProgress(float32(v / 100.0)), true
	}
	return 0, false
}

// CancelExportBuild cancels an in-flight release build, if any.
func (e *Editor) CancelExportBuild() {
	if e == nil {
		return
	}
	e.exportPanel.mu.Lock()
	run := e.exportPanel.run
	if run != nil {
		run.canceled = true
	}
	e.exportPanel.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// WaitExportBuild blocks until any in-flight release build finishes and returns its final error.
func (e *Editor) WaitExportBuild() error {
	if e == nil {
		return nil
	}
	e.exportPanel.mu.Lock()
	run := e.exportPanel.run
	e.exportPanel.mu.Unlock()
	if run != nil {
		<-run.done
	}
	e.exportPanel.mu.Lock()
	err := e.exportPanel.lastErr
	e.exportPanel.mu.Unlock()
	return err
}

// OpenExportOutputFolder opens the open project's resolved release output folder using OpenOutputFolder (or the OS file manager when nil).
func (e *Editor) OpenExportOutputFolder() error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	dir := filepath.Dir(e.ProjectPath)
	nsgo, _ := releaseplan.ReadNSGoSettings(dir)
	outDir, err := releaseplan.OutputDir(dir, releaseplan.Options{}, releaseplan.SettingsFromProject(e.Project), nsgo)
	if err != nil {
		return err
	}
	opener := e.OpenOutputFolder
	if opener == nil {
		opener = defaultOpenOutputFolder
	}
	return opener(outDir)
}

func (e *Editor) stopExportBuild() {
	if e == nil {
		return
	}
	e.exportPanel.mu.Lock()
	run := e.exportPanel.run
	e.exportPanel.run = nil
	if run != nil {
		run.canceled = true
	}
	e.exportPanel.mu.Unlock()
	if run != nil {
		run.cancel()
		if run.toast != nil {
			run.toast.Dismiss()
		}
		run.finish()
	}
}

func (e *Editor) resetExportPanelForProject() {
	if e == nil {
		return
	}
	e.stopExportBuild()
	e.exportPanel.mu.Lock()
	e.exportPanel.selectedPlatform = ""
	e.exportPanel.selectedTargets = nil
	e.exportPanel.channel = ""
	e.exportPanel.releaseNotes = ""
	e.exportPanel.dryRun = false
	e.exportPanel.output = nil
	e.exportPanel.lastErr = nil
	e.exportPanel.lastOutputDir = ""
	e.exportPanel.lastCommandLine = ""
	e.exportPanel.credits = ExportCreditsReport{}
	e.exportPanel.mu.Unlock()
	e.syncExportPanel()
	if e.ExportPanelOpen() {
		e.CheckExportCredits()
	}
}

func (e *Editor) syncExportPanel() {
	if e == nil {
		return
	}
	e.refreshAppleTeamIDCache()
	if e.Project == nil || e.Project.Export == nil || len(e.Project.Export.Presets) == 0 {
		e.exportPanel.selectedPlatform = ""
		return
	}
	if e.findExportPresetIndex(e.exportPanel.selectedPlatform) < 0 {
		e.exportPanel.selectedPlatform = e.Project.Export.Presets[0].Platform
	}
}

func (e *Editor) findExportPresetIndex(platform string) int {
	if e == nil || e.Project == nil || e.Project.Export == nil || platform == "" {
		return -1
	}
	for i, p := range e.Project.Export.Presets {
		if p.Platform == platform {
			return i
		}
	}
	return -1
}

func ensureExport(f *project.File) *project.Export {
	if f.Export == nil {
		f.Export = &project.Export{}
	}
	return f.Export
}

func cleanupEmptyExport(f *project.File) {
	if f.Export != nil && f.Export.Output == "" && f.Export.Budgets == nil && len(f.Export.Presets) == 0 {
		f.Export = nil
	}
}

func (e *Editor) postExportUI(scope *core.Scope, fn func()) {
	if scope != nil {
		scope.Post(fn)
		return
	}
	e.postOrRun(fn)
}

func newExportCommand(ctx context.Context, args, baseEnv []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = releaseplan.WithReleaseEnv(baseEnv)
	return cmd
}

func defaultExportCommandRunner(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cmd := newExportCommand(ctx, args, os.Environ())
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func defaultOpenOutputFolder(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "linux", "freebsd", "openbsd", "netbsd":
		cmd = exec.Command("xdg-open", dir)
	default:
		return fmt.Errorf("editor: opening folders is not supported on %s", runtime.GOOS)
	}
	return cmd.Start()
}

// exportStreamWriter splits concurrent stdout/stderr writes into complete lines and delivers each line to onLine.
type exportStreamWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	onLine func(string)
}

func newExportStreamWriter(onLine func(string)) *exportStreamWriter {
	return &exportStreamWriter{onLine: onLine}
}

func (w *exportStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	var lines []string
	for _, b := range p {
		if b == '\r' {
			continue
		}
		if b == '\n' {
			lines = append(lines, w.buf.String())
			w.buf.Reset()
			continue
		}
		w.buf.WriteByte(b)
	}
	w.mu.Unlock()
	for _, line := range lines {
		w.onLine(line)
	}
	return len(p), nil
}

func (w *exportStreamWriter) Flush() {
	w.mu.Lock()
	rem := w.buf.String()
	w.buf.Reset()
	w.mu.Unlock()
	if rem != "" {
		w.onLine(rem)
	}
}

// ExportPanelView renders the Export & Release panel below the viewport while a project is open and ExportPanelOpen reports true. It edits game.yaml's export section through EditProject, shows any pre-flight asset credits problems, runs release builds or dry runs off the UI goroutine, and streams their output into the Output view.
func ExportPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Project != nil && ed.ExportPanelOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			return exportPanel(cx, ed, rev)
		},
	}
}

func exportPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	pal := ed.SpecPalette(cx)
	outputDraft := core.NewSignal(cx, ed.ExportOutput())
	outputErr := core.NewSignal(cx, "")
	addPlatform := core.NewSignal(cx, firstAvailableExportPlatform(ed))

	// Keep the output directory draft synced with project edits and Undo/Redo.
	projectOutput := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ExportOutput()
	})
	cx.Effect(func() {
		outputDraft.Set(projectOutput.Get())
		outputErr.Set("")
	})

	// Keep the Add Preset dropdown synced to an available platform.
	availableKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return strings.Join(ed.AvailableExportPlatforms(), ",")
	})
	cx.Effect(func() {
		_ = availableKey.Get()
		avail := ed.AvailableExportPlatforms()
		if len(avail) == 0 {
			addPlatform.Set("")
			return
		}
		if !slices.Contains(avail, addPlatform.Get()) {
			addPlatform.Set(avail[0])
		}
	})

	editDisabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil || ed.ExportRunning()
	}

	commitOutput := func() {
		if err := ed.SetExportOutput(outputDraft.Get()); err != nil {
			outputErr.Set(err.Error())
			ed.report(err)
			outputDraft.Set(ed.ExportOutput())
			return
		}
		outputErr.Set("")
	}

	presetsKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project == nil || ed.Project.Export == nil {
			return ""
		}
		var b strings.Builder
		for i, p := range ed.Project.Export.Presets {
			fmt.Fprintf(&b, "%d:%s=%s|", i, p.Platform, strings.Join(p.Targets, ","))
		}
		return b.String()
	})

	scopeKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return strings.Join(ed.EffectiveExportTargets(), ",")
	})

	creditsKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		r := ed.ExportCredits()
		errText := ""
		if r.Err != nil {
			errText = r.Err.Error()
		}
		return fmt.Sprintf("%t|%d|%s|%s", r.Configured, len(r.Entries), strings.Join(r.Missing, ","), errText)
	})

	label := func(text string) core.Node {
		return core.Text{Content: text, Style: core.Style{FontSize: 11, FontWeight: 600, LetterSpacing: 0.66, Color: pal.TextFaint}}
	}

	channelOptions := []ui.Option[string]{
		{Value: "", Label: "(default)"},
		{Value: "direct", Label: "direct"},
		{Value: "store", Label: "store"},
		{Value: "steam", Label: "steam"},
		{Value: "none", Label: "none"},
	}

	// Build & release action bar styled with SpecPalette tokens.
	buildBar := core.View{
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
			label("OUTPUT"),
			core.View{
				Style: core.Style{Width: core.Pt(140)},
				Children: []core.Node{
					ui.Input{
						TestID:      "export-output-input",
						Label:       "Export Output Directory",
						Placeholder: "dist (default)",
						Disabled:    editDisabled,
						Invalid:     func() bool { return outputErr.Get() != "" },
						Value:       outputDraft.Get,
						OnChange: func(v string) {
							outputDraft.Set(v)
							outputErr.Set("")
						},
						OnSubmit: func(string) { commitOutput() },
					},
				},
			},
			settingsSpecButton(pal, "export-output-apply", "Set", "Apply export output directory", false, editDisabled, commitOutput),
			label("CHANNEL"),
			core.View{
				Style: core.Style{Width: core.Pt(118), FlexShrink: 0},
				Children: []core.Node{
					ui.Select[string]{
						TestID:   "export-channel-select",
						Label:    "Release Channel",
						Disabled: editDisabled,
						Options:  channelOptions,
						Style:    core.Style{Width: core.Pt(118)},
						Value: func() string {
							_ = rev.Get()
							return ed.ExportChannel()
						},
						OnChange: func(v string) {
							ed.report(ed.SetExportChannel(v))
						},
					},
				},
			},
			label("NOTES"),
			core.View{
				Style: core.Style{Width: core.Pt(148), FlexShrink: 0},
				Children: []core.Node{
					ui.Input{
						TestID:      "export-notes-input",
						Label:       "Release Notes File",
						Placeholder: "release-notes.md",
						Disabled:    editDisabled,
						Style:       core.Style{Width: core.Pt(148)},
						Value: func() string {
							_ = rev.Get()
							return ed.ExportReleaseNotes()
						},
						OnChange: func(v string) { ed.SetExportReleaseNotes(v) },
					},
				},
			},
			ui.ToggleButton{
				TestID:   "export-dry-run",
				Text:     "Dry Run",
				Disabled: editDisabled,
				On: func() bool {
					_ = rev.Get()
					return ed.ExportDryRun()
				},
				OnChange: func(on bool) { ed.SetExportDryRun(on) },
			},
			ui.Button{
				TestID:  "export-build",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || ed.Project == nil || ed.ExportRunning()
				},
				Busy: func() bool {
					_ = rev.Get()
					return ed.ExportRunning()
				},
				TextFunc: func() string {
					_ = rev.Get()
					if ed.ExportDryRun() {
						return "Dry Run"
					}
					return "Build"
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
					FlexShrink:   0,
					FocusVisible: pal.FocusRing(2),
				},
				OnPress: func() {
					ed.report(ed.RunExportBuild())
				},
			},
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.ExportRunning()
				},
				Then: func(cx *core.Scope) core.Node {
					return settingsSpecButton(pal, "export-cancel", "Cancel", "Cancel export build", false, nil, func() {
						ed.CancelExportBuild()
					})
				},
			},
			settingsSpecButton(pal, "export-open-output", "Open Output", "Open export output folder", false, func() bool {
				_ = rev.Get()
				return ed.Project == nil
			}, func() { ed.report(ed.OpenExportOutputFolder()) }),
			core.View{Style: core.Style{FlexGrow: 1}},
			settingsSpecButton(pal, "export-close", "Close", "Close Export settings", false, nil, func() {
				ed.SetExportPanelOpen(false)
			}),
		},
	}

	presetsColumn := core.View{
		TestID: "export-presets-column",
		Style: core.Style{
			FlexGrow:    1,
			FlexBasis:   core.Pt(0),
			MinWidth:    core.Pt(420),
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
							ui.Select[string]{
								TestID:      "export-add-platform",
								Label:       "Platform to Add",
								Placeholder: "All platforms added",
								Disabled: func() bool {
									return editDisabled() || len(ed.AvailableExportPlatforms()) == 0
								},
								OptionsFunc: func() []ui.Option[string] {
									_ = availableKey.Get()
									avail := ed.AvailableExportPlatforms()
									opts := make([]ui.Option[string], len(avail))
									for i, p := range avail {
										lbl := p
										if releaseplan.IsExperimental(p) {
											lbl = p + " (" + ed.T("settings.metaExperimental") + ")"
										}
										opts[i] = ui.Option[string]{Value: p, Label: lbl}
									}
									return opts
								},
								Value:    addPlatform.Get,
								OnChange: addPlatform.Set,
							},
						},
					},
					settingsSpecButton(pal, "export-add-preset", "Add Preset", "Add platform export preset", false, func() bool {
						return editDisabled() || addPlatform.Get() == ""
					}, func() {
						plat := addPlatform.Get()
						if plat == "" {
							return
						}
						ed.report(ed.AddExportPreset(plat))
					}),
				},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				ContentStyle: core.Style{Gap: 4},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if presetsKey.Get() == "" {
							return core.Text{
								TestID:  "export-empty-presets",
								Content: "No export presets yet. Default platforms will be built until a preset is added.",
								Style:   core.Style{FontSize: 12, Color: pal.TextFaint, Padding: core.Symmetric(12, 0)},
							}
						}
						presets := ed.ExportPresets()
						rows := make([]core.Node, 0, len(presets))
						for i, p := range presets {
							rows = append(rows, exportPresetRow(cx, ed, rev, pal, i, p, len(presets), editDisabled))
						}
						return core.View{Style: core.Style{Gap: 8}, Children: rows}
					}),
				},
			},
			// Build scope bar: build all presets or toggle chosen targets.
			core.View{
				TestID: "export-build-scope",
				Style: core.Style{
					Gap:         8,
					Padding:     core.Edges{Top: 12},
					Borders:     core.Edges{Top: 1},
					BorderColor: pal.Line,
				},
				Children: []core.Node{
					ui.HStack{
						Gap: 8,
						Children: []core.Node{
							label("BUILD TARGETS"),
							ui.ToggleButton{
								TestID:   "export-scope-all",
								Text:     "All Presets",
								Disabled: editDisabled,
								On: func() bool {
									_ = rev.Get()
									return len(ed.ExportSelectedTargets()) == 0
								},
								OnChange: func(on bool) {
									if on {
										ed.ClearExportSelectedTargets()
									}
								},
							},
						},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = scopeKey.Get()
						targets := ed.EffectiveExportTargets()
						btns := make([]core.Node, 0, len(targets))
						for _, t := range targets {
							target := t
							btns = append(btns, ui.ToggleButton{
								TestID:   "export-scope-target-" + target,
								Text:     target,
								Disabled: editDisabled,
								On: func() bool {
									_ = rev.Get()
									return slices.Contains(ed.ExportSelectedTargets(), target)
								},
								OnChange: func(bool) {
									ed.ToggleExportBuildTarget(target)
								},
							})
						}
						return core.View{
							Style: core.Style{
								Direction: core.Row,
								Wrap:      core.Wrap,
								Gap:       4,
							},
							Children: btns,
						}
					}),
				},
			},
		},
	}

	outputColumn := core.View{
		TestID: "export-output-column",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Gap:       8,
			Padding:   core.Edges{Left: 16},
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			// Credits pre-flight status banner.
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = creditsKey.Get()
				report := ed.ExportCredits()
				borderColor := pal.Line
				statusColor := pal.TextFaint
				bg := pal.Panel
				statusText := "No assets section in game.yaml (no CREDITS.md will be written)"
				if report.Configured {
					switch {
					case report.Err != nil:
						borderColor = pal.Problem().Edge
						statusColor = pal.Problem().Fg
						bg = pal.Problem().Fill
						statusText = "Credits error: " + report.Err.Error()
					case len(report.Missing) > 0:
						borderColor = pal.Problem().Edge
						statusColor = pal.Problem().Fg
						bg = pal.Problem().Fill
						noun := "assets have"
						if len(report.Missing) == 1 {
							noun = "asset has"
						}
						statusText = fmt.Sprintf("Credits required: %d %s no credit (release builds are blocked)", len(report.Missing), noun)
					default:
						borderColor = pal.Added.Edge
						statusColor = pal.Added.Fg
						bg = pal.Added.Fill
						statusText = fmt.Sprintf("Credits OK: %d asset(s) credited", len(report.Entries))
					}
				}
				children := []core.Node{
					ui.HStack{
						Gap: 8,
						Children: []core.Node{
							core.Text{
								TestID:  "export-credits-status",
								Content: statusText,
								Style:   core.Style{FontSize: 11.5, FontWeight: 600, Color: statusColor, FlexGrow: 1},
							},
							settingsSpecButton(pal, "export-credits-refresh", "Refresh Credits", "Refresh asset credits check", false, nil, func() {
								ed.CheckExportCredits()
							}),
						},
					},
				}
				if len(report.Missing) > 0 {
					missingLines := make([]string, len(report.Missing))
					for i, a := range report.Missing {
						missingLines[i] = fmt.Sprintf("• %s  (add credit in %s)", a, importsettings.Path(a))
					}
					children = append(children, core.Text{
						TestID:  "export-credits-missing",
						Content: strings.Join(missingLines, "\n"),
						Style:   core.Style{FontSize: 11, FontFamily: core.FontMonospace, Color: pal.Problem().Fg},
					})
				}
				return core.View{
					TestID: "export-credits-section",
					Style: core.Style{
						Gap:          4,
						Padding:      core.Symmetric(8, 12),
						CornerRadius: 8,
						BorderWidth:  1,
						BorderColor:  borderColor,
						Background:   bg,
					},
					Children: children,
				}
			}),
			// Output log header and scrollable stream.
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					label("OUTPUT"),
					core.Text{
						TestID: "export-run-status",
						ContentFunc: func() string {
							_ = rev.Get()
							if ed.ExportRunning() {
								return "Running…"
							}
							if err := ed.ExportLastError(); err != nil {
								return "Failed"
							}
							if len(ed.ExportOutputLines()) > 0 {
								return "Done"
							}
							return "Idle"
						},
						Style: core.Style{FontSize: 11.5, Color: pal.TextFaint, FlexGrow: 1},
					},
					settingsSpecButton(pal, "export-log-clear", "Clear", "Clear export build output", false, func() bool {
						_ = rev.Get()
						return ed.ExportRunning() || len(ed.ExportOutputLines()) == 0
					}, func() { ed.ClearExportOutput() }),
				},
			},
			ui.ScrollView{
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					Background:   pal.Panel,
					CornerRadius: 8,
					BorderWidth:  1,
					BorderColor:  pal.Line,
					Padding:      core.All(12),
				},
				Children: []core.Node{
					core.Text{
						TestID: "export-output-log",
						ContentFunc: func() string {
							_ = rev.Get()
							txt := ed.ExportOutputText()
							if txt == "" {
								return "Build output will stream here."
							}
							return txt
						},
						Style: core.Style{FontSize: 11.5, FontFamily: core.FontMonospace, Color: pal.TextSecondary},
					},
				},
			},
		},
	}

	return core.View{
		TestID: "export-panel",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Gap:       12,
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			buildBar,
			psErrorText(pal, "export-output-error", outputErr),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{presetsColumn, outputColumn},
			},
		},
	}
}

func exportPresetRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], pal SpecPalette, idx int, p project.Preset, total int, disabled func() bool) core.Node {
	plat := p.Platform
	validTargets := project.TargetsOf(plat)
	targetBtns := make([]core.Node, 0, len(validTargets))
	for _, t := range validTargets {
		target := t
		targetBtns = append(targetBtns, ui.ToggleButton{
			TestID:   "export-target-" + plat + "-" + target,
			Text:     target,
			Disabled: disabled,
			On: func() bool {
				_ = rev.Get()
				if cur, ok := ed.Project.Preset(plat); ok {
					return slices.Contains(cur.Targets, target)
				}
				return false
			},
			OnChange: func(bool) {
				ed.report(ed.ToggleExportPresetTarget(plat, target))
			},
		})
	}
	policy := policyYouOnly
	if plat == "web" {
		policy = policyAgentMayChange
	}
	expInfo, isExp := ed.ExportPresetExperimental(plat)

	headerChildren := []core.Node{
		core.View{
			Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
			Children: []core.Node{
				core.Text{
					Content: exportPlatformDisplayName(plat),
					Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: pal.TextPrimary},
				},
				core.Text{
					TestID: "export-preset-summary-" + plat,
					ContentFunc: func() string {
						_ = rev.Get()
						_, detail := ed.ExportPresetReadiness(plat)
						return detail
					},
					StyleFunc: func() core.Style {
						_ = rev.Get()
						ready, _ := ed.ExportPresetReadiness(plat)
						c := pal.TextFaint
						if !ready {
							c = pal.Warning.Fg
						}
						return core.Style{FontSize: 11.5, Color: c}
					},
				},
			},
		},
	}
	if isExp {
		headerChildren = append(headerChildren, core.View{
			TestID: "export-preset-experimental-" + plat,
			Role:   core.RoleButton,
			Label:  ed.T("settings.metaExperimental") + " — " + expInfo.DocsURL,
			Style: core.Style{
				Height:         core.Pt(24),
				Padding:        core.Symmetric(0, 10),
				CornerRadius:   6,
				Background:     pal.Warning.Fill,
				BorderWidth:    1,
				BorderColor:    pal.Warning.Edge,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   pal.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.report(ed.OpenExperimentalPlatformDocs())
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("settings.metaExperimental"),
					Style:   core.Style{FontSize: 11.5, FontWeight: 600, Color: pal.Warning.Fg},
				},
			},
		})
	}
	headerChildren = append(headerChildren,
		core.View{
			TestID: "export-preset-readiness-" + plat,
			StyleFunc: func() core.Style {
				_ = rev.Get()
				ready, _ := ed.ExportPresetReadiness(plat)
				bg, border := pal.Added.Fill, pal.Added.Edge
				if !ready {
					bg, border = pal.Warning.Fill, pal.Warning.Edge
				}
				return core.Style{
					Height:         core.Pt(24),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   6,
					Background:     bg,
					BorderWidth:    1,
					BorderColor:    border,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				}
			},
			Children: []core.Node{
				core.Text{
					ContentFunc: func() string {
						_ = rev.Get()
						if ready, _ := ed.ExportPresetReadiness(plat); ready {
							return ed.T("settings.metaReady")
						}
						return ed.T("settings.metaSetUp")
					},
					StyleFunc: func() core.Style {
						_ = rev.Get()
						c := pal.Added.Fg
						if ready, _ := ed.ExportPresetReadiness(plat); !ready {
							c = pal.Warning.Fg
						}
						return core.Style{FontSize: 11.5, FontWeight: 600, Color: c}
					},
				},
			},
		},
		settingsPolicyBadge(pal, ed, policy),
	)

	rowChildren := []core.Node{
		core.View{
			Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
			Children: headerChildren,
		},
	}
	if isExp {
		rowChildren = append(rowChildren, core.View{
			TestID: "export-preset-experimental-note-" + plat,
			Role:   core.RoleButton,
			Label:  expInfo.DocsURL,
			Style: core.Style{
				Direction:    core.Row,
				Wrap:         core.Wrap,
				AlignItems:   core.AlignCenter,
				Gap:          6,
				Padding:      core.Symmetric(4, 8),
				CornerRadius: 6,
				Background:   pal.Warning.Fill,
				BorderWidth:  1,
				BorderColor:  pal.Warning.Edge,
				FocusVisible: pal.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.report(ed.OpenExperimentalPlatformDocs())
			},
			Children: []core.Node{
				core.Text{
					Content: expInfo.Missing + " · " + expInfo.DocsURL,
					Style:   core.Style{FontSize: 11, Color: pal.Warning.Fg},
				},
			},
		})
	}
	rowChildren = append(rowChildren, core.View{
		Style: core.Style{
			Direction:  core.Row,
			Wrap:       core.Wrap,
			AlignItems: core.AlignCenter,
			Gap:        4,
		},
		Children: append(targetBtns,
			core.View{Style: core.Style{FlexGrow: 1}},
			settingsSpecButton(pal, "export-preset-up-"+plat, "Up", "Move "+plat+" preset up", false, func() bool {
				return disabled() || idx == 0
			}, func() { ed.report(ed.MoveExportPreset(plat, -1)) }),
			settingsSpecButton(pal, "export-preset-down-"+plat, "Down", "Move "+plat+" preset down", false, func() bool {
				return disabled() || idx >= total-1
			}, func() { ed.report(ed.MoveExportPreset(plat, 1)) }),
			settingsSpecButton(pal, "export-preset-remove-"+plat, "Remove", "Remove "+plat+" preset", false, disabled, func() {
				ed.report(ed.RemoveExportPreset(plat))
			}),
		),
	})

	return core.View{
		TestID: "export-preset-" + plat,
		Style: core.Style{
			Gap:          8,
			Padding:      core.Symmetric(12, 12),
			CornerRadius: 8,
			BorderWidth:  1,
			BorderColor:  pal.Line,
			Background:   pal.Panel,
		},
		Children: rowChildren,
	}
}

func firstAvailableExportPlatform(ed *Editor) string {
	avail := ed.AvailableExportPlatforms()
	if len(avail) == 0 {
		return ""
	}
	return avail[0]
}
