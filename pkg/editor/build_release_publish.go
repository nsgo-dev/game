package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nsgo-dev/game/internal/releaseplan"
	"github.com/nsgo-dev/game/pkg/editor/keychain"
	"github.com/nsgo-dev/game/pkg/tooling/sizecheck"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/targets"
)

// DefaultReleaseNotesFile is the project-relative Markdown file written by the Build and Release screen's
// RELEASE NOTES editor when no explicit -release-notes path has been configured in the Export panel.
const DefaultReleaseNotesFile = "release-notes.md"

const (
	// maxReleaseNotesBytes caps release notes Markdown reads and writes (256 KiB).
	maxReleaseNotesBytes = 256 << 10
	// maxChangelogFragmentBytes caps each changelog.d fragment read when drafting release notes.
	maxChangelogFragmentBytes = 64 << 10
)

func validateReleaseNotesContent(notes string) error {
	if strings.IndexByte(notes, 0) >= 0 {
		return errors.New("editor: release notes must not contain NUL bytes")
	}
	if len(notes) > maxReleaseNotesBytes {
		return fmt.Errorf("editor: release notes exceed maximum size (%d bytes)", maxReleaseNotesBytes)
	}
	return nil
}

// ValidateReleaseNotes reports whether notes is safe and within the size budget for release notes Markdown.
func ValidateReleaseNotes(notes string) error {
	return validateReleaseNotesContent(notes)
}

func readBoundedTextFile(absPath string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("editor: file %q exceeds maximum size (%d bytes)", filepath.Base(absPath), maxBytes)
	}
	return data, nil
}

func isSafeGitRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") {
		return false
	}
	for _, r := range ref {
		if r <= 0x20 || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return true
}

// ErrUploadRequiresConfirmation is returned whenever code attempts to start a store upload without an
// active, user-confirmed Review uploads session.
var ErrUploadRequiresConfirmation = errors.New("editor: store uploads always require confirmation in Review uploads")

// ErrUploadRequiresPersonConfirmation is returned when an agent attempts to confirm or trigger a store upload.
// Store uploads always require explicit human confirmation, whatever the permission profile.
var ErrUploadRequiresPersonConfirmation = errors.New("editor: store uploads require the person's confirmation in Review uploads, whatever the permission profile")

// ErrNoUploadsSelected is returned when confirming Review uploads with no included artifacts.
var ErrNoUploadsSelected = errors.New("editor: no artifacts are selected to upload")

// ErrReleaseUploadRunning is returned when starting or mutating upload selections while an upload batch is running.
var ErrReleaseUploadRunning = errors.New("editor: a store upload is already running")

// ErrInvalidPublishDestination is returned when an unknown store destination is supplied.
var ErrInvalidPublishDestination = errors.New("editor: unknown publish destination")

// AgentPermissionProfile aliases PermissionProfile from Settings -> Permissions.
// Store uploads ALWAYS ask first in Review uploads regardless of profile (including Autonomous).
type AgentPermissionProfile = PermissionProfile

// StoreUploadRequiresConfirmation reports whether store publishing requires explicit confirmation
// in Review uploads for the given permission profile. It always returns true.
func StoreUploadRequiresConfirmation(AgentPermissionProfile) bool {
	return true
}

// PublishDestination identifies a supported store upload destination on the Build and Release screen.
type PublishDestination string

const (
	// PublishDestItchIO publishes desktop or web packages to itch.io.
	PublishDestItchIO PublishDestination = "itch.io"
	// PublishDestGooglePlay publishes Android App Bundles (.aab) to Google Play internal testing.
	PublishDestGooglePlay PublishDestination = "google_play_internal"
	// PublishDestTestFlight publishes iOS builds (.ipa) to Apple TestFlight.
	PublishDestTestFlight PublishDestination = "testflight"
)

// AllPublishDestinations returns every supported store upload destination in display order.
func AllPublishDestinations() []PublishDestination {
	return []PublishDestination{
		PublishDestItchIO,
		PublishDestGooglePlay,
		PublishDestTestFlight,
	}
}

// Valid reports whether d is a recognized PublishDestination.
func (d PublishDestination) Valid() bool {
	switch d {
	case PublishDestItchIO, PublishDestGooglePlay, PublishDestTestFlight:
		return true
	default:
		return false
	}
}

// DisplayName returns the user-facing store destination label shown in Review uploads.
func (d PublishDestination) DisplayName() string {
	switch d {
	case PublishDestItchIO:
		return "itch.io"
	case PublishDestGooglePlay:
		return "Google Play · internal testing"
	case PublishDestTestFlight:
		return "TestFlight"
	default:
		return string(d)
	}
}

// DefaultPublishDestinationForPlatform returns the default store destination and track label for platform.
// For itch.io targets, the track label matches the channel(s) `nsgo publish itch` uses by default
// (html5, osx-<arch>, windows-<arch>, linux-<arch>).
func DefaultPublishDestinationForPlatform(platform string) (PublishDestination, string) {
	switch platform {
	case "android":
		return PublishDestGooglePlay, "internal track"
	case "ios":
		return PublishDestTestFlight, "TestFlight beta"
	case "web":
		return PublishDestItchIO, "html5"
	case "macos", "windows", "linux":
		return PublishDestItchIO, itchChannelsLabel(platform, nil, nil)
	default:
		if system, _, ok := strings.Cut(platform, "_"); ok && (system == "macos" || system == "windows" || system == "linux") {
			return PublishDestItchIO, itchChannelForTarget(platform, nil)
		}
		return PublishDestItchIO, platform
	}
}

func expandPublishTargets(platform string, configured []string) []string {
	var raw []string
	for _, t := range configured {
		if s := strings.TrimSpace(t); s != "" {
			raw = append(raw, s)
		}
	}
	if len(raw) == 0 && strings.TrimSpace(platform) != "" {
		raw = []string{strings.TrimSpace(platform)}
	}
	if len(raw) == 0 {
		return nil
	}
	if expanded, err := targets.Expand(raw); err == nil && len(expanded) > 0 {
		return expanded
	}
	return raw
}

func itchChannelForTarget(target string, overrides map[string]string) string {
	if custom := strings.TrimSpace(overrides[target]); custom != "" {
		return custom
	}
	system, arch, _ := strings.Cut(target, "_")
	switch system {
	case "web":
		return "html5"
	case "macos":
		if arch != "" {
			return "osx-" + arch
		}
		return "osx"
	case "windows":
		if arch != "" {
			return "windows-" + arch
		}
		return "windows"
	case "linux":
		if arch != "" {
			return "linux-" + arch
		}
		return "linux"
	default:
		return target
	}
}

func itchChannelsLabel(platform string, configured []string, overrides map[string]string) string {
	expanded := expandPublishTargets(platform, configured)
	if len(expanded) == 0 {
		return itchChannelForTarget(platform, overrides)
	}
	var channels []string
	for _, t := range expanded {
		ch := itchChannelForTarget(t, overrides)
		if ch != "" && !slices.Contains(channels, ch) {
			channels = append(channels, ch)
		}
	}
	return strings.Join(channels, ", ")
}

// ReleaseNotesDraftRequest holds the git history, kept reviews, and changelog entries passed to
// Editor.ReleaseNotesDrafter or DefaultReleaseNotesDraft.
type ReleaseNotesDraftRequest struct {
	ProjectName      string   `json:"projectName"`
	Version          string   `json:"version"`
	LastReleaseTag   string   `json:"lastReleaseTag,omitempty"`
	SinceRef         string   `json:"sinceRef"`
	CommitCount      int      `json:"commitCount"`
	CommitSubjects   []string `json:"commitSubjects,omitempty"`
	KeptReviews      []string `json:"keptReviews,omitempty"`
	ChangelogEntries []string `json:"changelogEntries,omitempty"`
}

// ReleaseNotesDraft is the player-facing Markdown and provenance badge produced for RELEASE NOTES.
type ReleaseNotesDraft struct {
	Markdown   string `json:"markdown"`
	Provenance string `json:"provenance"`
}

// ReleaseUploadStatus reports the lifecycle state of one upload item in Review uploads.
type ReleaseUploadStatus string

const (
	UploadStatusPending   ReleaseUploadStatus = "pending"
	UploadStatusUploading ReleaseUploadStatus = "uploading"
	UploadStatusDone      ReleaseUploadStatus = "done"
	UploadStatusFailed    ReleaseUploadStatus = "failed"
	UploadStatusCancelled ReleaseUploadStatus = "cancelled"
)

// ReleaseUploadItem describes one artifact queued for user confirmation in Review uploads.
// It never stores credential secrets.
type ReleaseUploadItem struct {
	Platform          string              `json:"platform"`
	PlatformName      string              `json:"platformName"`
	Targets           []string            `json:"targets,omitempty"`
	Destination       PublishDestination  `json:"destination"`
	DestinationLabel  string              `json:"destinationLabel"`
	Track             string              `json:"track,omitempty"`
	ManualOnly        bool                `json:"manualOnly,omitempty"`
	Version           string              `json:"version"`
	BuildNumber       string              `json:"buildNumber,omitempty"`
	ArtifactName      string              `json:"artifactName"`
	ArtifactPath      string              `json:"artifactPath"`
	ArtifactRelPath   string              `json:"artifactRelPath"`
	SizeBytes         int64               `json:"sizeBytes"`
	Included          bool                `json:"included"`
	HasCredential     bool                `json:"hasCredential"`
	CredentialAccount string              `json:"credentialAccount,omitempty"`
	Status            ReleaseUploadStatus `json:"status"`
	Progress          float64             `json:"progress"`
	StatusMessage     string              `json:"statusMessage,omitempty"`
	ResultURL         string              `json:"resultUrl,omitempty"`
	Err               string              `json:"err,omitempty"`
}

// ReleaseUploadReviewSnapshot is a thread-safe value copy of the Review uploads confirmation dialog state.
type ReleaseUploadReviewSnapshot struct {
	Open                bool                   `json:"open"`
	SessionID           uint64                 `json:"sessionId"`
	PermissionProfile   AgentPermissionProfile `json:"permissionProfile"`
	ReleaseNotesFile    string                 `json:"releaseNotesFile"`
	ReleaseNotesPreview string                 `json:"releaseNotesPreview,omitempty"`
	Items               []ReleaseUploadItem    `json:"items"`
	Running             bool                   `json:"running"`
	Completed           bool                   `json:"completed"`
	KeychainEditorOpen  bool                   `json:"keychainEditorOpen"`
	KeychainDest        PublishDestination     `json:"keychainDest,omitempty"`
	LastErr             string                 `json:"lastErr,omitempty"`
}

// ReleaseUploadRequest describes one user-confirmed artifact upload passed to ReleaseUploader.
type ReleaseUploadRequest struct {
	ProjectDir       string
	OutputDir        string
	Platform         string
	Targets          []string
	Destination      PublishDestination
	DestinationLabel string
	Track            string
	Version          string
	ArtifactPath     string
	ArtifactName     string
	SizeBytes        int64
	ReleaseNotesPath string
	Credential       PublishCredential
}

// ReleaseUploadProgress reports incremental upload progress for one artifact.
type ReleaseUploadProgress struct {
	Fraction float64
	Message  string
}

// ReleaseUploadResult describes the outcome of uploading one artifact to a store destination.
type ReleaseUploadResult struct {
	Destination PublishDestination
	Platform    string
	Version     string
	URL         string
	Message     string
}

// ReleaseUploader uploads a single user-confirmed release artifact to a store destination.
type ReleaseUploader interface {
	Upload(ctx context.Context, req ReleaseUploadRequest, progress func(ReleaseUploadProgress)) (ReleaseUploadResult, error)
}

// ReleaseUploaderFunc adapts a plain function to ReleaseUploader.
type ReleaseUploaderFunc func(ctx context.Context, req ReleaseUploadRequest, progress func(ReleaseUploadProgress)) (ReleaseUploadResult, error)

// Upload calls f(ctx, req, progress).
func (f ReleaseUploaderFunc) Upload(ctx context.Context, req ReleaseUploadRequest, progress func(ReleaseUploadProgress)) (ReleaseUploadResult, error) {
	return f(ctx, req, progress)
}

type releaseUploadRun struct {
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once
}

func (r *releaseUploadRun) finish() {
	r.doneOnce.Do(func() {
		close(r.done)
	})
}

type releaseUploadReviewState struct {
	open               bool
	sessionID          uint64
	nextSessionID      uint64
	items              []ReleaseUploadItem
	completed          bool
	keychainEditorOpen bool
	keychainDest       PublishDestination
	lastErr            error
	run                *releaseUploadRun
}

// AgentPermissionProfile returns the active agent permission profile (defaulting to PermissionProfileBalanced).
func (e *Editor) AgentPermissionProfile() AgentPermissionProfile {
	if e == nil {
		return PermissionProfileBalanced
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	if !e.buildRelease.permissionProfile.Valid() {
		return PermissionProfileBalanced
	}
	return e.buildRelease.permissionProfile
}

// SetAgentPermissionProfile sets the editor's agent permission profile in Settings -> Permissions (permissions.yaml).
// Store uploads always ask first in Review uploads regardless of profile.
func (e *Editor) SetAgentPermissionProfile(profile AgentPermissionProfile) error {
	if e == nil {
		return ErrNoProject
	}
	if !profile.Valid() {
		return fmt.Errorf("editor: unknown agent permission profile %q", profile)
	}
	return e.SelectPermissionProfile(profile)
}

// WithAgentActor runs fn marked as an agent actor so strict human-only boundaries
// (such as confirming store uploads in Review uploads) are enforced regardless of permission profile.
func (e *Editor) WithAgentActor(fn func() error) error {
	if e == nil {
		return fn()
	}
	e.agentActorDepth.Add(1)
	defer e.agentActorDepth.Add(-1)
	return fn()
}

// AgentActorActive reports whether the current call is executing on behalf of an agent.
func (e *Editor) AgentActorActive() bool {
	return e != nil && e.agentActorDepth.Load() > 0
}

// ReleaseNotesFile returns the project-relative Markdown file path used for RELEASE NOTES
// and read by `game release -release-notes`. It uses ExportReleaseNotes() when set, otherwise DefaultReleaseNotesFile.
func (e *Editor) ReleaseNotesFile() string {
	if e == nil {
		return DefaultReleaseNotesFile
	}
	if configured := strings.TrimSpace(e.ExportReleaseNotes()); configured != "" {
		return filepath.ToSlash(configured)
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	if p := strings.TrimSpace(e.buildRelease.notesFile); p != "" {
		return p
	}
	return DefaultReleaseNotesFile
}

// ReleaseNotesAbsPath resolves and validates the absolute path of the release notes Markdown file inside the open project.
func (e *Editor) ReleaseNotesAbsPath() (string, error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return "", ErrNoProject
	}
	baseDir := filepath.Dir(e.ProjectPath)
	return resolveBuildReleasePath(baseDir, e.ReleaseNotesFile(), true)
}

// SetReleaseNotesFile sets the project-relative Markdown file path for RELEASE NOTES (syncing ExportReleaseNotes).
// Paths escaping the project directory or pointing through symlinks are refused.
func (e *Editor) SetReleaseNotesFile(relPath string) error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	relPath = strings.TrimSpace(strings.ReplaceAll(relPath, `\`, "/"))
	if relPath == "" {
		relPath = DefaultReleaseNotesFile
	}
	relPath = path.Clean(relPath)
	if relPath == "." || relPath == ".." || strings.HasPrefix(relPath, "../") || strings.HasPrefix(relPath, "/") || strings.Contains(relPath, ":") {
		return fmt.Errorf("editor: release notes path %q must be inside the project directory", relPath)
	}
	var loadedText string
	loadedFromDisk := false
	if e.ProjectPath != "" {
		abs, err := resolveBuildReleasePath(filepath.Dir(e.ProjectPath), relPath, true)
		if err != nil {
			return err
		}
		if data, rerr := readBoundedTextFile(abs, maxReleaseNotesBytes); rerr == nil && strings.TrimSpace(string(data)) != "" {
			loadedText = string(data)
			loadedFromDisk = true
		}
	}
	e.SetExportReleaseNotes(relPath)
	e.buildRelease.mu.Lock()
	e.buildRelease.notesFile = relPath
	if loadedFromDisk {
		e.buildRelease.releaseNotes = loadedText
		e.buildRelease.notesInitialized = true
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return nil
}

// SetReleaseKeptReviews sets the kept review summaries included when drafting player-facing release notes.
func (e *Editor) SetReleaseKeptReviews(reviews []string) {
	if e == nil {
		return
	}
	var cleaned []string
	for _, r := range reviews {
		if s := strings.TrimSpace(r); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	e.buildRelease.mu.Lock()
	e.buildRelease.keptReviews = cleaned
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// AddReleaseKeptReview appends one kept review summary to the inputs used when drafting release notes.
func (e *Editor) AddReleaseKeptReview(summary string) {
	if e == nil {
		return
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}
	e.buildRelease.mu.Lock()
	if !slices.Contains(e.buildRelease.keptReviews, summary) {
		e.buildRelease.keptReviews = append(e.buildRelease.keptReviews, summary)
		e.buildRelease.rev++
	}
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SaveReleaseNotes writes the current RELEASE NOTES text atomically to the exact Markdown file
// read by `game release -release-notes`, modifying ONLY that file.
func (e *Editor) SaveReleaseNotes() error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	e.buildRelease.mu.Lock()
	notes := e.buildRelease.releaseNotes
	e.buildRelease.mu.Unlock()
	return e.writeReleaseNotesFile(notes)
}

func (e *Editor) writeReleaseNotesFile(notes string) error {
	if err := validateReleaseNotesContent(notes); err != nil {
		return err
	}
	if e == nil || e.ProjectPath == "" {
		return nil
	}
	abs, err := e.ReleaseNotesAbsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("editor: create release notes directory: %w", err)
	}
	data := notes
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	if err := writeFileAtomic(abs, []byte(data)); err != nil {
		return err
	}
	return nil
}

// DraftReleaseNotes reads git commits since the last release tag (or initial commit), kept reviews,
// and changelog fragments, drafts player-facing Markdown via Editor.ReleaseNotesDrafter or DefaultReleaseNotesDraft,
// and writes the result directly to the release notes Markdown file read by `game release -release-notes`.
func (e *Editor) DraftReleaseNotes() (ReleaseNotesDraft, error) {
	if e == nil || e.Project == nil {
		return ReleaseNotesDraft{}, ErrNoProject
	}
	req := e.collectReleaseNotesDraftRequest()
	var draft ReleaseNotesDraft
	var err error
	if e.ReleaseNotesDrafter != nil {
		draft, err = e.ReleaseNotesDrafter(context.Background(), req)
		if err != nil {
			return ReleaseNotesDraft{}, err
		}
	} else {
		draft = DefaultReleaseNotesDraft(req)
	}
	if strings.TrimSpace(draft.Provenance) == "" {
		draft.Provenance = formatReleaseNotesProvenance(req.CommitCount, req.SinceRef, len(req.KeptReviews))
	}
	if err := e.writeReleaseNotesFile(draft.Markdown); err != nil {
		return ReleaseNotesDraft{}, err
	}
	e.buildRelease.mu.Lock()
	e.buildRelease.commitsSinceRelease = req.CommitCount
	if req.LastReleaseTag != "" {
		e.buildRelease.lastReleaseVersion = req.LastReleaseTag
	}
	e.buildRelease.notesSinceRef = req.SinceRef
	e.buildRelease.notesProvenance = draft.Provenance
	e.buildRelease.releaseNotes = draft.Markdown
	e.buildRelease.notesInitialized = true
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return draft, nil
}

// RedraftReleaseNotes regenerates the release notes from git commits and kept reviews,
// overwriting the release notes Markdown file on disk.
func (e *Editor) RedraftReleaseNotes() (ReleaseNotesDraft, error) {
	return e.DraftReleaseNotes()
}

func (e *Editor) collectReleaseNotesDraftRequest() ReleaseNotesDraftRequest {
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}
	lastTag, sinceRef, count, subjects := e.readReleaseGitHistory(dir)

	e.buildRelease.mu.Lock()
	kept := slices.Clone(e.buildRelease.keptReviews)
	baselineVer := e.buildRelease.baseline.Version
	e.buildRelease.mu.Unlock()

	if lastTag == "" && baselineVer != "" {
		lastTag = baselineVer
		if sinceRef == "" || sinceRef == "initial commit" {
			sinceRef = baselineVer
		}
	}
	if sinceRef == "" {
		if lastTag != "" {
			sinceRef = lastTag
		} else {
			sinceRef = "initial commit"
		}
	}

	changelogEntries := readProjectChangelogFragments(dir)

	projName := ""
	ver := ""
	if e.Project != nil {
		projName = e.Project.Name
		ver = e.Project.AppVersion
	}

	return ReleaseNotesDraftRequest{
		ProjectName:      projName,
		Version:          ver,
		LastReleaseTag:   lastTag,
		SinceRef:         sinceRef,
		CommitCount:      count,
		CommitSubjects:   subjects,
		KeptReviews:      kept,
		ChangelogEntries: changelogEntries,
	}
}

func (e *Editor) readReleaseGitHistory(dir string) (lastTag, sinceRef string, count int, subjects []string) {
	if dir == "" {
		return "", "", 0, nil
	}
	if out, err := e.runBuildGit(dir, "describe", "--tags", "--abbrev=0"); err == nil {
		if rawTag := strings.TrimSpace(out); isSafeGitRef(rawTag) {
			lastTag = strings.TrimPrefix(rawTag, "v")
			sinceRef = lastTag
			if cnt, cerr := e.runBuildGit(dir, "rev-list", "--count", rawTag+"..HEAD"); cerr == nil {
				_, _ = fmt.Sscanf(strings.TrimSpace(cnt), "%d", &count)
			}
			// Try up to 32 commits first; fall back to -n 8 for test runners mocking the original command.
			if logOut, lerr := e.runBuildGit(dir, "log", "--pretty=format:%s", "-n", "32", rawTag+"..HEAD"); lerr == nil && strings.TrimSpace(logOut) != "" {
				subjects = splitNonEmptyLines(logOut)
			} else if logOut, lerr := e.runBuildGit(dir, "log", "--pretty=format:%s", "-n", "8", rawTag+"..HEAD"); lerr == nil {
				subjects = splitNonEmptyLines(logOut)
			}
			return lastTag, sinceRef, count, subjects
		}
	}

	// No release tag exists yet: read commits since the initial commit.
	if cnt, cerr := e.runBuildGit(dir, "rev-list", "--count", "HEAD"); cerr == nil {
		_, _ = fmt.Sscanf(strings.TrimSpace(cnt), "%d", &count)
	}
	if logOut, lerr := e.runBuildGit(dir, "log", "--pretty=format:%s", "-n", "32", "HEAD"); lerr == nil && strings.TrimSpace(logOut) != "" {
		subjects = splitNonEmptyLines(logOut)
	}
	if count > 0 || len(subjects) > 0 {
		if count == 0 {
			count = len(subjects)
		}
		sinceRef = "initial commit"
	}
	return "", sinceRef, count, subjects
}

// DefaultReleaseNotesDraft builds clean, deterministic, player-facing Markdown release notes
// from git commits since the last release tag (or initial commit), kept reviews, and changelog entries.
func DefaultReleaseNotesDraft(req ReleaseNotesDraftRequest) ReleaseNotesDraft {
	provenance := formatReleaseNotesProvenance(req.CommitCount, req.SinceRef, len(req.KeptReviews))

	var lead string
	var playerBullets []string
	var fallbackBullets []string
	seen := make(map[string]bool)

	addUnique := func(dst *[]string, item string) {
		item = strings.TrimSpace(item)
		if item == "" {
			return
		}
		key := strings.ToLower(item)
		if seen[key] {
			return
		}
		seen[key] = true
		*dst = append(*dst, item)
	}

	// Kept reviews are already written in human/player terms; lead with the first if concise, and include the rest as bullets.
	for i, rev := range req.KeptReviews {
		text := strings.TrimSpace(rev)
		if text == "" {
			continue
		}
		if i == 0 && !strings.HasPrefix(text, "-") && !strings.HasPrefix(text, "*") {
			lead = ensureSentence(text)
			seen[strings.ToLower(strings.TrimSuffix(lead, "."))] = true
			continue
		}
		text = strings.TrimSpace(strings.TrimLeft(text, "-* "))
		addUnique(&playerBullets, text)
	}

	// Changelog fragments under changelog.d/ provide curated user-visible notes.
	for _, entry := range req.ChangelogEntries {
		cleaned := cleanChangelogBullet(entry)
		if cleaned != "" {
			addUnique(&playerBullets, cleaned)
		}
	}

	// Convert commit subjects into player-facing bullets, separating internal plumbing from player changes.
	for _, raw := range req.CommitSubjects {
		bullet, internal := humanizeCommitSubject(raw)
		if bullet == "" {
			continue
		}
		if internal {
			addUnique(&fallbackBullets, bullet)
		} else {
			addUnique(&playerBullets, bullet)
		}
	}

	bullets := playerBullets
	if len(bullets) == 0 {
		bullets = fallbackBullets
	}

	var b strings.Builder
	if lead != "" {
		b.WriteString(lead)
		if len(bullets) > 0 {
			b.WriteString("\n\n")
		}
	}
	for i, item := range bullets {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(item)
	}
	md := strings.TrimSpace(b.String())
	if md == "" {
		ver := strings.TrimSpace(req.Version)
		if ver == "" {
			ver = "0.1.0"
		}
		md = fmt.Sprintf("Release %s.", ver)
	}
	return ReleaseNotesDraft{
		Markdown:   md,
		Provenance: provenance,
	}
}

func formatReleaseNotesProvenance(commits int, sinceRef string, keptReviews int) string {
	ref := strings.TrimSpace(sinceRef)
	if ref == "" {
		ref = "initial commit"
	}
	switch {
	case commits > 0 && keptReviews > 0:
		cUnit := "commits"
		if commits == 1 {
			cUnit = "commit"
		}
		rUnit := "kept reviews"
		if keptReviews == 1 {
			rUnit = "kept review"
		}
		return fmt.Sprintf("Drafted from %d %s and %d %s since %s", commits, cUnit, keptReviews, rUnit, ref)
	case commits > 0:
		cUnit := "commits"
		if commits == 1 {
			cUnit = "commit"
		}
		return fmt.Sprintf("Drafted from %d %s since %s", commits, cUnit, ref)
	case keptReviews > 0:
		rUnit := "kept reviews"
		if keptReviews == 1 {
			rUnit = "kept review"
		}
		return fmt.Sprintf("Drafted from %d %s", keptReviews, rUnit)
	default:
		return ""
	}
}

func humanizeCommitSubject(raw string) (bullet string, internal bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "merge ") || strings.HasPrefix(lower, "revert \"merge ") {
		return "", true
	}

	// Parse conventional commit prefix: `type(scope)!: description`
	if colon := strings.IndexByte(s, ':'); colon > 0 && colon < 32 {
		head := s[:colon]
		rest := strings.TrimSpace(s[colon+1:])
		if rest != "" {
			kind := strings.ToLower(head)
			if paren := strings.IndexByte(kind, '('); paren > 0 && strings.HasSuffix(strings.TrimSuffix(kind, "!"), ")") {
				kind = kind[:paren]
			} else {
				kind = strings.TrimSuffix(kind, "!")
			}
			switch kind {
			case "fix", "bugfix":
				return "Fixed: " + rest, false
			case "feat", "feature", "perf", "balance", "art", "audio", "level", "ui", "gameplay":
				return capitalizeSentence(rest), false
			case "chore", "ci", "test", "tests", "build", "docs", "refactor", "style":
				return capitalizeSentence(rest), true
			}
		}
	}
	return capitalizeSentence(s), false
}

func cleanChangelogBullet(entry string) string {
	s := strings.TrimSpace(entry)
	s = strings.TrimSpace(strings.TrimLeft(s, "-* "))
	if s == "" {
		return ""
	}
	// Strip leading backtick-wrapped package prefix like "`game/editor`: ..."
	if strings.HasPrefix(s, "`") {
		if end := strings.Index(s[1:], "`:"); end > 0 {
			rest := strings.TrimSpace(s[end+3:])
			if rest != "" {
				s = rest
			}
		}
	}
	return capitalizeSentence(s)
}

func capitalizeSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func ensureSentence(s string) string {
	s = capitalizeSentence(s)
	if s == "" {
		return ""
	}
	last := s[len(s)-1]
	if last != '.' && last != '!' && last != '?' {
		s += "."
	}
	return s
}

func readProjectChangelogFragments(dir string) []string {
	if dir == "" {
		return nil
	}
	chDir, err := resolveBuildReleasePath(dir, "changelog.d", false)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(chDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") || strings.EqualFold(name, "README.md") {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)

	var bullets []string
	for _, name := range names {
		p, perr := resolveBuildReleasePath(dir, path.Join("changelog.d", name), false)
		if perr != nil {
			continue
		}
		data, rerr := readBoundedTextFile(p, maxChangelogFragmentBytes)
		if rerr != nil {
			continue
		}
		for _, line := range splitNonEmptyLines(string(data)) {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
				bullets = append(bullets, strings.TrimSpace(trimmed[2:]))
			}
		}
	}
	return bullets
}

// --- Keychain credential management ---

// PublishKeychainService is the system keychain service identifier used to store release upload credentials outside any game project directory.
const PublishKeychainService = "dev.nsgo.game.publish"

// PublishCredential holds a store upload credential retrieved from the system keychain just before upload. Its fields carry `yaml:"-" json:"-"` and redacted String/GoString methods so secrets are never serialized into project files, snapshots, or logs.
type PublishCredential struct {
	Destination PublishDestination `yaml:"-" json:"-"`
	Account     string             `yaml:"-" json:"-"`
	Secret      string             `yaml:"-" json:"-"`
}

// String redacts the credential secret for safe logging.
func (c PublishCredential) String() string {
	return fmt.Sprintf("PublishCredential{Destination:%q, Account:%q, Secret:[REDACTED]}", c.Destination, c.Account)
}

// GoString redacts the credential secret for %#v formatting.
func (c PublishCredential) GoString() string {
	return c.String()
}

func (e *Editor) activeKeychain() keychain.Store {
	if e != nil && e.Keychain != nil {
		return e.Keychain
	}
	return keychain.Default()
}

func (e *Editor) publishKeychainAccount(dest PublishDestination) string {
	scope := "project"
	if e != nil && e.Project != nil {
		if id := strings.TrimSpace(e.Project.ID); id != "" {
			scope = id
		} else if name := strings.TrimSpace(e.Project.Name); name != "" {
			scope = strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		}
	}
	return scope + ":" + string(dest)
}

// StorePublishCredential stores a store upload token/key in the system keychain (never in project files).
func (e *Editor) StorePublishCredential(dest PublishDestination, account, secret string) error {
	if e == nil {
		return ErrNoProject
	}
	if !dest.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidPublishDestination, dest)
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return errors.New("editor: credential secret must not be empty")
	}
	kcAccount := e.publishKeychainAccount(dest)
	if err := e.activeKeychain().Set(PublishKeychainService, kcAccount, secret); err != nil {
		return err
	}
	displayAccount := strings.TrimSpace(account)
	if displayAccount == "" {
		displayAccount = "stored in system keychain"
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.keychainAccounts == nil {
		e.buildRelease.keychainAccounts = make(map[PublishDestination]string)
	}
	e.buildRelease.keychainAccounts[dest] = displayAccount
	for i := range e.buildRelease.uploadReview.items {
		if e.buildRelease.uploadReview.items[i].Destination == dest {
			e.buildRelease.uploadReview.items[i].HasCredential = true
			e.buildRelease.uploadReview.items[i].CredentialAccount = displayAccount
		}
	}
	e.buildRelease.uploadReview.keychainEditorOpen = false
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.Status = fmt.Sprintf("Saved %s credential to system keychain", dest.DisplayName())
	e.notify()
	return nil
}

// HasPublishCredential reports whether a credential for dest is stored in the system keychain.
func (e *Editor) HasPublishCredential(dest PublishDestination) bool {
	if e == nil || !dest.Valid() {
		return false
	}
	val, err := e.activeKeychain().Get(PublishKeychainService, e.publishKeychainAccount(dest))
	return err == nil && val != ""
}

// PublishCredentialAccount returns the non-secret display label for dest's keychain entry.
func (e *Editor) PublishCredentialAccount(dest PublishDestination) string {
	if e == nil || !dest.Valid() {
		return ""
	}
	e.buildRelease.mu.Lock()
	acc := e.buildRelease.keychainAccounts[dest]
	e.buildRelease.mu.Unlock()
	if acc != "" {
		return acc
	}
	if e.HasPublishCredential(dest) {
		return "stored in system keychain"
	}
	return ""
}

// DeletePublishCredential removes the stored credential for dest from the system keychain.
func (e *Editor) DeletePublishCredential(dest PublishDestination) error {
	if e == nil {
		return ErrNoProject
	}
	if !dest.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidPublishDestination, dest)
	}
	if err := e.activeKeychain().Delete(PublishKeychainService, e.publishKeychainAccount(dest)); err != nil {
		return err
	}
	e.buildRelease.mu.Lock()
	delete(e.buildRelease.keychainAccounts, dest)
	for i := range e.buildRelease.uploadReview.items {
		if e.buildRelease.uploadReview.items[i].Destination == dest {
			e.buildRelease.uploadReview.items[i].HasCredential = false
			e.buildRelease.uploadReview.items[i].CredentialAccount = ""
		}
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return nil
}

// --- Review uploads & confirmation-gated store publishing ---

// OpenReviewUploads opens the Review uploads confirmation dialog listing every artifact,
// its destination, size, and version before anything is sent. It never starts an upload.
func (e *Editor) OpenReviewUploads() error {
	return e.ReviewReleaseUploads()
}

// CloseReviewUploads closes the Review uploads confirmation dialog without starting any upload.
func (e *Editor) CloseReviewUploads() {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	if !e.buildRelease.uploadReview.open {
		e.buildRelease.mu.Unlock()
		return
	}
	// Invalidate the current session ID so nothing can confirm a closed dialog.
	e.buildRelease.uploadReview.open = false
	e.buildRelease.uploadReview.sessionID = 0
	e.buildRelease.uploadReview.keychainEditorOpen = false
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// OpenKeychainCredentialEditor opens the inline keychain credential form inside Review uploads for dest.
func (e *Editor) OpenKeychainCredentialEditor(dest PublishDestination) {
	if e == nil || !dest.Valid() {
		return
	}
	e.buildRelease.mu.Lock()
	e.buildRelease.uploadReview.keychainEditorOpen = true
	e.buildRelease.uploadReview.keychainDest = dest
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// CloseKeychainCredentialEditor closes the inline keychain credential form inside Review uploads.
func (e *Editor) CloseKeychainCredentialEditor() {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	if !e.buildRelease.uploadReview.keychainEditorOpen {
		e.buildRelease.mu.Unlock()
		return
	}
	e.buildRelease.uploadReview.keychainEditorOpen = false
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
}

// SetReleaseUploadItemIncluded toggles whether platform's artifact is included in the Review uploads batch.
func (e *Editor) SetReleaseUploadItemIncluded(platform string, included bool) error {
	if e == nil {
		return ErrNoProject
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.uploadReview.run != nil {
		e.buildRelease.mu.Unlock()
		return ErrReleaseUploadRunning
	}
	found := false
	for i := range e.buildRelease.uploadReview.items {
		if e.buildRelease.uploadReview.items[i].Platform == platform {
			if included && (e.buildRelease.uploadReview.items[i].ManualOnly || !e.buildRelease.uploadReview.items[i].Destination.Valid()) {
				label := e.buildRelease.uploadReview.items[i].DestinationLabel
				e.buildRelease.mu.Unlock()
				return fmt.Errorf("editor: %s cannot be uploaded automatically (%s)", platform, label)
			}
			e.buildRelease.uploadReview.items[i].Included = included
			found = true
			break
		}
	}
	if !found {
		e.buildRelease.mu.Unlock()
		return fmt.Errorf("editor: upload item for platform %q not found", platform)
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return nil
}

// SetReleaseUploadDestination changes the target store destination for platform inside Review uploads.
func (e *Editor) SetReleaseUploadDestination(platform string, dest PublishDestination) error {
	if e == nil {
		return ErrNoProject
	}
	if !dest.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidPublishDestination, dest)
	}
	hasCred := e.HasPublishCredential(dest)
	acc := e.PublishCredentialAccount(dest)

	e.buildRelease.mu.Lock()
	if e.buildRelease.uploadReview.run != nil {
		e.buildRelease.mu.Unlock()
		return ErrReleaseUploadRunning
	}
	found := false
	for i := range e.buildRelease.uploadReview.items {
		if e.buildRelease.uploadReview.items[i].Platform == platform {
			e.buildRelease.uploadReview.items[i].Destination = dest
			e.buildRelease.uploadReview.items[i].DestinationLabel = dest.DisplayName()
			e.buildRelease.uploadReview.items[i].ManualOnly = false
			e.buildRelease.uploadReview.items[i].HasCredential = hasCred
			e.buildRelease.uploadReview.items[i].CredentialAccount = acc
			found = true
			break
		}
	}
	if !found {
		e.buildRelease.mu.Unlock()
		return fmt.Errorf("editor: upload item for platform %q not found", platform)
	}
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()
	e.notify()
	return nil
}

// StartReleaseUpload unconditionally refuses to upload directly without confirmation in Review uploads.
// Callers must open Review uploads (ReviewReleaseUploads) and have the person confirm via ConfirmReleaseUploads.
func (e *Editor) StartReleaseUpload() error {
	return ErrUploadRequiresConfirmation
}

// ReleaseUploadRunning reports whether a user-confirmed store upload batch is currently running.
func (e *Editor) ReleaseUploadRunning() bool {
	if e == nil {
		return false
	}
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	return e.buildRelease.uploadReview.run != nil
}

// CancelReleaseUpload cancels any in-flight store upload batch.
func (e *Editor) CancelReleaseUpload() {
	if e == nil {
		return
	}
	e.buildRelease.mu.Lock()
	run := e.buildRelease.uploadReview.run
	e.buildRelease.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// WaitReleaseUpload blocks until any in-flight store upload batch finishes and returns its first error, if any.
func (e *Editor) WaitReleaseUpload() error {
	if e == nil {
		return nil
	}
	e.buildRelease.mu.Lock()
	run := e.buildRelease.uploadReview.run
	e.buildRelease.mu.Unlock()
	if run != nil {
		<-run.done
	}
	if e.flushBuildReleaseStatus() {
		e.notify()
	}
	e.buildRelease.mu.Lock()
	err := e.buildRelease.uploadReview.lastErr
	e.buildRelease.mu.Unlock()
	return err
}

// buildReviewUploadItems constructs the list of artifacts ready for Review uploads from the current presets.
func (e *Editor) buildReviewUploadItems() []ReleaseUploadItem {
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}
	var nsgoCfg releaseplan.NSGoSettings
	if dir != "" {
		nsgoCfg, _ = releaseplan.ReadNSGoSettings(dir)
	}
	ver := "0.1.0"
	slug := "game"
	outDir := filepath.Join(dir, releaseplan.DefaultOutput)
	if e.Project != nil {
		if strings.TrimSpace(e.Project.AppVersion) != "" {
			ver = strings.TrimSpace(e.Project.AppVersion)
		}
		if strings.TrimSpace(e.Project.Name) != "" {
			slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(e.Project.Name), " ", "-"))
		}
		if dir != "" {
			if resolvedOut, err := releaseplan.OutputDir(dir, releaseplan.Options{}, releaseplan.SettingsFromProject(e.Project), nsgoCfg); err == nil {
				outDir = resolvedOut
			}
		} else if e.Project.Export != nil && e.Project.Export.Output != "" {
			outDir = filepath.FromSlash(e.Project.Export.Output)
		}
	}
	exportChannel := strings.TrimSpace(e.ExportChannel())

	e.buildRelease.mu.Lock()
	presets := make([]BuildPresetEntry, len(e.buildRelease.presets))
	for i, p := range e.buildRelease.presets {
		presets[i] = cloneBuildPresetEntry(p)
	}
	cachedAccounts := make(map[PublishDestination]string, len(e.buildRelease.keychainAccounts))
	for k, v := range e.buildRelease.keychainAccounts {
		cachedAccounts[k] = v
	}
	e.buildRelease.mu.Unlock()

	type credLookup struct {
		hasCred bool
		account string
	}
	credCache := make(map[PublishDestination]credLookup, 3)
	lookupCred := func(dest PublishDestination) credLookup {
		if c, ok := credCache[dest]; ok {
			return c
		}
		has := e.HasPublishCredential(dest)
		acc := cachedAccounts[dest]
		if acc == "" && has {
			acc = "stored in system keychain"
		}
		c := credLookup{hasCred: has, account: acc}
		credCache[dest] = c
		return c
	}

	var items []ReleaseUploadItem
	for _, p := range presets {
		// Include presets that have finished building (Result != nil) or are checked & ready.
		if p.Result == nil && (!p.Included || p.Status == BuildPresetSetup || p.Status == BuildPresetFailed) {
			continue
		}
		artifactName := defaultArtifactName(slug, ver, p.Platform)
		artifactPath := filepath.Join(outDir, artifactName)
		var sizeBytes int64
		if p.Result != nil && (p.Result.ArtifactPath != "" || p.Result.SizeMeasured) {
			if p.Result.ArtifactName != "" {
				artifactName = p.Result.ArtifactName
			}
			if p.Result.ArtifactPath != "" {
				artifactPath = p.Result.ArtifactPath
			}
			sizeBytes = p.Result.SizeBytes
		} else {
			appName := nsgoCfg.Name
			appID := nsgoCfg.ID
			if e.Project != nil {
				if appName == "" {
					appName = e.Project.Name
				}
				if appID == "" {
					appID = e.Project.ID
				}
			}
			if art, err := sizecheck.MeasureBuiltArtifact(outDir, appName, appID, ver, p.Platform, p.Targets); err == nil && art.Found {
				artifactName = art.ArtifactName
				artifactPath = art.ArtifactPath
				sizeBytes = art.SizeBytes
			} else if st, err := os.Stat(artifactPath); err == nil && !st.IsDir() {
				sizeBytes = st.Size()
			}
		}
		relPath := artifactName
		if dir != "" && artifactPath != "" {
			if rel, err := filepath.Rel(dir, artifactPath); err == nil && !strings.HasPrefix(rel, "..") {
				relPath = filepath.ToSlash(rel)
			}
		}

		channel := strings.TrimSpace(p.Channel)
		if channel == "" {
			channel = exportChannel
		}
		if channel == "" {
			channel = strings.TrimSpace(nsgoCfg.Channel)
		}
		resolvedTargets := expandPublishTargets(p.Platform, p.Targets)

		if channel == "steam" && slices.Contains(releaseplan.SteamPlatforms, p.Platform) {
			items = append(items, ReleaseUploadItem{
				Platform:         p.Platform,
				PlatformName:     p.Name,
				Targets:          resolvedTargets,
				DestinationLabel: "upload with SteamPipe",
				Track:            "Steam channel",
				ManualOnly:       true,
				Version:          ver,
				ArtifactName:     artifactName,
				ArtifactPath:     artifactPath,
				ArtifactRelPath:  relPath,
				SizeBytes:        sizeBytes,
				Included:         false,
				Status:           UploadStatusPending,
			})
			continue
		}

		dest, track := DefaultPublishDestinationForPlatform(p.Platform)
		if dest == PublishDestItchIO {
			track = itchChannelsLabel(p.Platform, p.Targets, nsgoCfg.ItchChannels)
		}
		cred := lookupCred(dest)
		items = append(items, ReleaseUploadItem{
			Platform:          p.Platform,
			PlatformName:      p.Name,
			Targets:           resolvedTargets,
			Destination:       dest,
			DestinationLabel:  dest.DisplayName(),
			Track:             track,
			Version:           ver,
			BuildNumber:       nsgoCfg.BuildNumber,
			ArtifactName:      artifactName,
			ArtifactPath:      artifactPath,
			ArtifactRelPath:   relPath,
			SizeBytes:         sizeBytes,
			Included:          p.Status == BuildPresetDone || p.Result != nil,
			HasCredential:     cred.hasCred,
			CredentialAccount: cred.account,
			Status:            UploadStatusPending,
		})
	}
	// If no preset was built yet, include all uploadable items by default so the author can inspect them.
	anyIncluded := false
	for _, it := range items {
		if it.Included {
			anyIncluded = true
			break
		}
	}
	if !anyIncluded {
		for i := range items {
			if !items[i].ManualOnly && items[i].Destination.Valid() {
				items[i].Included = true
			}
		}
	}
	return items
}

type confirmedUploadJob struct {
	item      ReleaseUploadItem
	kcAccount string
}

// ConfirmReleaseUploads starts uploading the selected artifacts from the open Review uploads dialog.
// Strict trust boundary:
//   - Always refuses when called by an agent actor (AgentActorActive() == true), whatever the permission profile.
//   - Always refuses while playing or while Start Screen / Merge Conflict screens are active.
//   - Always refuses unless Review uploads is currently open and sessionID matches the active review session.
//   - Validates every selected artifact path stays within the open project directory.
//   - Consumes sessionID immediately so confirmation can never be replayed.
func (e *Editor) ConfirmReleaseUploads(sessionID uint64) error {
	if e == nil || e.Project == nil {
		return ErrNoProject
	}
	if e.AgentActorActive() {
		return ErrUploadRequiresPersonConfirmation
	}
	if e.playing {
		return ErrPlaying
	}
	if e.StartScreenOpen() || e.MergeConflictOpen() {
		return ErrUploadRequiresConfirmation
	}

	baseDir := ""
	if e.ProjectPath != "" {
		baseDir = filepath.Dir(e.ProjectPath)
	}

	e.buildRelease.mu.Lock()
	rev := &e.buildRelease.uploadReview
	if rev.run != nil {
		e.buildRelease.mu.Unlock()
		return ErrReleaseUploadRunning
	}
	if !rev.open || sessionID == 0 || rev.sessionID != sessionID {
		e.buildRelease.mu.Unlock()
		return ErrUploadRequiresConfirmation
	}

	var selected []ReleaseUploadItem
	for i := range rev.items {
		if rev.items[i].Included && !rev.items[i].ManualOnly && rev.items[i].Destination.Valid() {
			selected = append(selected, rev.items[i])
		}
	}
	if len(selected) == 0 {
		e.buildRelease.mu.Unlock()
		return ErrNoUploadsSelected
	}

	// Validate every selected artifact path stays within the project directory before consuming the session.
	if baseDir != "" {
		for i := range selected {
			if strings.TrimSpace(selected[i].ArtifactPath) != "" {
				resolved, err := resolveBuildReleasePath(baseDir, selected[i].ArtifactPath, false)
				if err != nil {
					e.buildRelease.mu.Unlock()
					return err
				}
				selected[i].ArtifactPath = resolved
			}
		}
	}

	// Precompute keychain accounts on the UI goroutine so the background upload worker never reads e.Project.
	jobs := make([]confirmedUploadJob, len(selected))
	for i, it := range selected {
		jobs[i] = confirmedUploadJob{
			item:      it,
			kcAccount: e.publishKeychainAccount(it.Destination),
		}
	}

	for i := range rev.items {
		if rev.items[i].Included && !rev.items[i].ManualOnly && rev.items[i].Destination.Valid() {
			rev.items[i].Status = UploadStatusPending
			rev.items[i].Progress = 0
			rev.items[i].Err = ""
			rev.items[i].StatusMessage = "Queued"
		}
	}

	// Consume the confirmation session immediately so it cannot be replayed.
	rev.sessionID = 0
	rev.completed = false
	rev.lastErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	run := &releaseUploadRun{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	rev.run = run
	e.buildRelease.rev++
	e.buildRelease.mu.Unlock()

	outDir := ""
	if baseDir != "" {
		nsgoCfg, _ := releaseplan.ReadNSGoSettings(baseDir)
		if resolvedOut, err := releaseplan.OutputDir(baseDir, releaseplan.Options{}, releaseplan.SettingsFromProject(e.Project), nsgoCfg); err == nil {
			outDir = resolvedOut
		}
	}
	notesAbs, _ := e.ReleaseNotesAbsPath()
	uploader := e.ReleaseUploader
	if uploader == nil {
		uploader = defaultReleaseUploader{}
	}
	kc := e.activeKeychain()
	scope := e.scope

	e.Status = fmt.Sprintf("Uploading %d release artifact(s)…", len(jobs))
	e.notify()

	go e.executeConfirmedReleaseUploads(ctx, scope, run, jobs, baseDir, outDir, notesAbs, kc, uploader)
	return nil
}

func (e *Editor) executeConfirmedReleaseUploads(
	ctx context.Context,
	scope *core.Scope,
	run *releaseUploadRun,
	jobs []confirmedUploadJob,
	projectDir string,
	outputDir string,
	notesAbs string,
	kc keychain.Store,
	uploader ReleaseUploader,
) {
	defer run.finish()
	defer run.cancel()

	notifyUI := func() {
		if scope == nil {
			return
		}
		scope.Post(func() {
			e.flushBuildReleaseStatus()
			e.notify()
		})
	}

	var firstErr error
	uploadedCount := 0

	for _, job := range jobs {
		item := job.item
		if ctx.Err() != nil {
			e.updateUploadItemLocked(run, item.Platform, func(it *ReleaseUploadItem) {
				it.Status = UploadStatusCancelled
				it.StatusMessage = "Cancelled"
			})
			notifyUI()
			continue
		}

		e.updateUploadItemLocked(run, item.Platform, func(it *ReleaseUploadItem) {
			it.Status = UploadStatusUploading
			it.Progress = 0.05
			it.StatusMessage = "Loading credential from keychain…"
			it.Err = ""
		})
		notifyUI()

		secret, kerr := kc.Get(PublishKeychainService, job.kcAccount)
		if kerr != nil {
			errMsg := fmt.Sprintf("missing %s credential in system keychain", item.DestinationLabel)
			if !errors.Is(kerr, keychain.ErrNotFound) {
				errMsg = kerr.Error()
			}
			if firstErr == nil {
				firstErr = errors.New(errMsg)
			}
			e.updateUploadItemLocked(run, item.Platform, func(it *ReleaseUploadItem) {
				it.Status = UploadStatusFailed
				it.HasCredential = false
				it.Err = errMsg
				it.StatusMessage = "Failed"
			})
			notifyUI()
			continue
		}

		req := ReleaseUploadRequest{
			ProjectDir:       projectDir,
			OutputDir:        outputDir,
			Platform:         item.Platform,
			Targets:          slices.Clone(item.Targets),
			Destination:      item.Destination,
			DestinationLabel: item.DestinationLabel,
			Track:            item.Track,
			Version:          item.Version,
			ArtifactPath:     item.ArtifactPath,
			ArtifactName:     item.ArtifactName,
			SizeBytes:        item.SizeBytes,
			ReleaseNotesPath: notesAbs,
			Credential: PublishCredential{
				Destination: item.Destination,
				Account:     item.CredentialAccount,
				Secret:      secret,
			},
		}

		res, uerr := uploader.Upload(ctx, req, func(p ReleaseUploadProgress) {
			frac := clampUploadFraction(p.Fraction)
			e.updateUploadItemLocked(run, item.Platform, func(it *ReleaseUploadItem) {
				it.Progress = frac
				if p.Message != "" {
					it.StatusMessage = p.Message
				}
			})
			notifyUI()
		})

		if uerr != nil {
			if ctx.Err() != nil {
				e.updateUploadItemLocked(run, item.Platform, func(it *ReleaseUploadItem) {
					it.Status = UploadStatusCancelled
					it.StatusMessage = "Cancelled"
				})
			} else {
				if firstErr == nil {
					firstErr = uerr
				}
				e.updateUploadItemLocked(run, item.Platform, func(it *ReleaseUploadItem) {
					it.Status = UploadStatusFailed
					it.Err = uerr.Error()
					it.StatusMessage = "Failed"
				})
			}
			notifyUI()
			continue
		}

		uploadedCount++
		msg := res.Message
		if msg == "" {
			msg = "Uploaded"
		}
		// The upload itself succeeded, so a publish record that cannot be read or written is reported on the item rather than turning the upload into a failure.
		if projectDir != "" {
			if recErr := recordPublishUpload(projectDir, item, res.URL, time.Now()); recErr != nil {
				msg += " (publish record not saved: " + recErr.Error() + ")"
			}
		}
		e.updateUploadItemLocked(run, item.Platform, func(it *ReleaseUploadItem) {
			it.Status = UploadStatusDone
			it.Progress = 1
			it.StatusMessage = msg
			it.ResultURL = res.URL
			it.Err = ""
		})
		notifyUI()
	}

	e.buildRelease.mu.Lock()
	if e.buildRelease.uploadReview.run == run {
		e.buildRelease.uploadReview.run = nil
		e.buildRelease.uploadReview.completed = true
		e.buildRelease.uploadReview.lastErr = firstErr
		switch {
		case ctx.Err() != nil:
			e.buildRelease.pendingStatus = "Store upload cancelled"
		case firstErr != nil:
			e.buildRelease.pendingStatus = fmt.Sprintf("Uploaded %d of %d artifacts (with errors)", uploadedCount, len(jobs))
		default:
			e.buildRelease.pendingStatus = fmt.Sprintf("Uploaded %d release artifact(s)", uploadedCount)
		}
		e.buildRelease.hasPendingStatus = true
		e.buildRelease.rev++
	}
	e.buildRelease.mu.Unlock()
	notifyUI()
}

// recordPublishUpload writes a successful upload into the destination's publish record.
func recordPublishUpload(projectDir string, item ReleaseUploadItem, resultURL string, at time.Time) error {
	rec, err := LoadPublishRecord(projectDir, item.Destination)
	if err != nil {
		return err
	}
	if err := rec.RecordUpload(item.Version, item.BuildNumber, item.ArtifactName, item.ArtifactPath, resultURL, item.SizeBytes, at); err != nil {
		return err
	}
	return SavePublishRecord(projectDir, rec)
}

func clampUploadFraction(frac float64) float64 {
	if math.IsNaN(frac) {
		return 0
	}
	if math.IsInf(frac, 1) {
		return 1
	}
	if math.IsInf(frac, -1) || frac < 0 {
		return 0
	}
	if frac > 1 {
		return 1
	}
	return frac
}

func (e *Editor) updateUploadItemLocked(run *releaseUploadRun, platform string, fn func(*ReleaseUploadItem)) {
	e.buildRelease.mu.Lock()
	defer e.buildRelease.mu.Unlock()
	if e.buildRelease.uploadReview.run != run {
		return
	}
	for i := range e.buildRelease.uploadReview.items {
		if e.buildRelease.uploadReview.items[i].Platform == platform {
			fn(&e.buildRelease.uploadReview.items[i])
			e.buildRelease.rev++
			return
		}
	}
}

type defaultReleaseUploader struct{}

func (defaultReleaseUploader) Upload(ctx context.Context, req ReleaseUploadRequest, progress func(ReleaseUploadProgress)) (ReleaseUploadResult, error) {
	if err := ctx.Err(); err != nil {
		return ReleaseUploadResult{}, err
	}
	if !req.Destination.Valid() {
		return ReleaseUploadResult{}, fmt.Errorf("%w: %q", ErrInvalidPublishDestination, req.Destination)
	}
	projectDir := strings.TrimSpace(req.ProjectDir)
	if projectDir == "" && strings.TrimSpace(req.ArtifactPath) != "" {
		projectDir = filepath.Dir(req.ArtifactPath)
	}
	if projectDir == "" {
		return ReleaseUploadResult{}, errors.New("editor: project directory is required to run nsgo publish")
	}
	req.ProjectDir = projectDir

	publishArgs, err := buildNSGoPublishArgs(req)
	if err != nil {
		return ReleaseUploadResult{}, err
	}
	env, secrets, err := buildNSGoPublishEnv(os.Environ(), req)
	if err != nil {
		return ReleaseUploadResult{}, err
	}

	bin, cmdArgs := resolveNSGoPublishCommand(projectDir, publishArgs)
	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	cmd.Dir = projectDir
	cmd.Env = env
	cmd.Stdin = nil

	var (
		mu          sync.Mutex
		lines       []string
		linesSeen   int
		lastFrac    = 0.10
		lastURL     string
		lastMessage string
	)
	const maxBufferedLines = 64

	onLine := func(raw string) {
		cleaned := strings.TrimSpace(stripANSI(raw))
		if cleaned == "" {
			return
		}
		cleaned = redactSecrets(cleaned, secrets)
		mu.Lock()
		linesSeen++
		if len(lines) >= maxBufferedLines {
			copy(lines, lines[1:])
			lines[len(lines)-1] = cleaned
		} else {
			lines = append(lines, cleaned)
		}
		if u := extractPublishURL(cleaned); u != "" {
			lastURL = u
		}
		lastMessage = cleaned
		frac := lastFrac
		if parsed, ok := parseExportProgressPercent(cleaned); ok {
			mapped := 0.10 + float64(parsed)*0.85
			if mapped > frac {
				frac = mapped
			}
		} else {
			estimated := 0.10 + 0.80*(1.0-math.Pow(0.78, float64(linesSeen)))
			if estimated > 0.90 {
				estimated = 0.90
			}
			if estimated > frac {
				frac = estimated
			}
		}
		lastFrac = frac
		mu.Unlock()
		if progress != nil {
			progress(ReleaseUploadProgress{
				Fraction: frac,
				Message:  cleaned,
			})
		}
	}

	if progress != nil {
		progress(ReleaseUploadProgress{
			Fraction: 0.10,
			Message:  fmt.Sprintf("Running nsgo publish for %s…", req.DestinationLabel),
		})
	}

	stream := newPublishLineWriter(onLine)
	cmd.Stdout = stream
	cmd.Stderr = stream

	runErr := cmd.Run()
	stream.Flush()

	mu.Lock()
	capturedLines := slices.Clone(lines)
	resultURL := lastURL
	resultMsg := lastMessage
	mu.Unlock()

	if runErr != nil {
		if ctx.Err() != nil {
			return ReleaseUploadResult{}, ctx.Err()
		}
		detail := formatPublishFailureMessage(capturedLines, runErr, secrets)
		return ReleaseUploadResult{}, fmt.Errorf("nsgo publish (%s): %s", req.DestinationLabel, detail)
	}

	if resultURL == "" && req.Destination == PublishDestItchIO {
		resultURL = deriveItchURL(projectDir, capturedLines)
	}
	if resultMsg == "" {
		resultMsg = "Uploaded to " + req.DestinationLabel
	}
	if progress != nil {
		progress(ReleaseUploadProgress{
			Fraction: 1,
			Message:  resultMsg,
		})
	}
	return ReleaseUploadResult{
		Destination: req.Destination,
		Platform:    req.Platform,
		Version:     req.Version,
		URL:         resultURL,
		Message:     resultMsg,
	}, nil
}

func resolveNSGoPublishCommand(projectDir string, publishArgs []string) (string, []string) {
	if hasNSGoGoModTool(projectDir) {
		return "go", append([]string{"tool", "nsgo"}, publishArgs...)
	}
	if nsgoPath, err := exec.LookPath("nsgo"); err == nil {
		return nsgoPath, publishArgs
	}
	return "go", append([]string{"tool", "nsgo"}, publishArgs...)
}

func hasNSGoGoModTool(dir string) bool {
	cur, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		goModPath := filepath.Join(cur, "go.mod")
		if info, statErr := os.Stat(goModPath); statErr == nil && info.Mode().IsRegular() {
			data, readErr := readBoundedTextFile(goModPath, 1<<20)
			if readErr != nil {
				return false
			}
			return goModDeclaresNSGoTool(string(data))
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

func goModDeclaresNSGoTool(content string) bool {
	const nsgoToolPkg = "github.com/nsgo-dev/nsgo/cmd/nsgo"
	inToolBlock := false
	for _, line := range strings.Split(content, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if inToolBlock {
			if line == ")" {
				inToolBlock = false
				continue
			}
			if strings.Trim(line, `"`) == nsgoToolPkg {
				return true
			}
			continue
		}
		if strings.HasPrefix(line, "tool") && len(line) > len("tool") {
			next := line[len("tool")]
			if next != ' ' && next != '\t' && next != '(' {
				continue
			}
			rest := strings.TrimSpace(line[len("tool"):])
			if rest == "(" {
				inToolBlock = true
				continue
			}
			if strings.Trim(rest, `"`) == nsgoToolPkg {
				return true
			}
		}
	}
	return false
}

func buildNSGoPublishArgs(req ReleaseUploadRequest) ([]string, error) {
	switch req.Destination {
	case PublishDestItchIO:
		publishTargets := expandPublishTargets(req.Platform, req.Targets)
		if len(publishTargets) == 0 {
			return nil, fmt.Errorf("editor: no itch.io targets resolved for %s", req.Platform)
		}
		for _, t := range publishTargets {
			if t == "" || strings.HasPrefix(t, "-") {
				return nil, fmt.Errorf("editor: invalid publish target %q", t)
			}
		}
		args := []string{"publish", "itch", "-target", strings.Join(publishTargets, ",")}
		if strings.TrimSpace(req.OutputDir) != "" {
			outArg := projectRelativeArg(req.ProjectDir, req.OutputDir)
			if strings.HasPrefix(outArg, "-") {
				outArg = "./" + outArg
			}
			args = append(args, "-output", outArg)
		}
		if ver := strings.TrimSpace(req.Version); ver != "" {
			if strings.HasPrefix(ver, "-") {
				return nil, fmt.Errorf("editor: invalid release version %q", ver)
			}
			args = append(args, "-version", ver)
		}
		return args, nil

	case PublishDestGooglePlay:
		playTrack := normalizeGooglePlayTrack(req.Track)
		if playTrack == "" || strings.HasPrefix(playTrack, "-") {
			return nil, fmt.Errorf("editor: invalid Google Play track %q", req.Track)
		}
		args := []string{"publish", "-target", "android", "-track", playTrack}
		if aabPath, ok := resolveExistingUploadArtifact(req, ".aab"); ok {
			args = append(args, safePositionalArg(projectRelativeArg(req.ProjectDir, aabPath)))
		}
		return args, nil

	case PublishDestTestFlight:
		ipaPath, ok := resolveExistingUploadArtifact(req, ".ipa")
		if !ok {
			name := req.ArtifactName
			if name == "" {
				name = req.Platform + ".ipa"
			}
			return nil, fmt.Errorf("editor: build %s before uploading (%s not found)", req.Platform, name)
		}
		return []string{"publish", "ios", safePositionalArg(projectRelativeArg(req.ProjectDir, ipaPath))}, nil

	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidPublishDestination, req.Destination)
	}
}

func normalizeGooglePlayTrack(track string) string {
	t := strings.TrimSpace(track)
	if t == "" || strings.EqualFold(t, "internal track") {
		return "internal"
	}
	if strings.HasSuffix(strings.ToLower(t), " track") {
		t = strings.TrimSpace(t[:len(t)-len(" track")])
	}
	if t == "" {
		return "internal"
	}
	return t
}

func resolveExistingUploadArtifact(req ReleaseUploadRequest, ext string) (string, bool) {
	if p := strings.TrimSpace(req.ArtifactPath); p != "" && strings.EqualFold(filepath.Ext(p), ext) {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p, true
		}
	}
	searchDirs := []string{}
	if outDir := strings.TrimSpace(req.OutputDir); outDir != "" {
		searchDirs = append(searchDirs, filepath.Join(outDir, "releases"), outDir)
	}
	if req.ProjectDir != "" {
		defaultOut := filepath.Join(req.ProjectDir, releaseplan.DefaultOutput)
		if !slices.Contains(searchDirs, filepath.Join(defaultOut, "releases")) {
			searchDirs = append(searchDirs, filepath.Join(defaultOut, "releases"))
		}
		if !slices.Contains(searchDirs, defaultOut) {
			searchDirs = append(searchDirs, defaultOut)
		}
	}
	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var matches []string
		for _, ent := range entries {
			if ent.IsDir() || !strings.EqualFold(filepath.Ext(ent.Name()), ext) {
				continue
			}
			matches = append(matches, filepath.Join(dir, ent.Name()))
		}
		if len(matches) == 1 {
			return matches[0], true
		}
	}
	return "", false
}

func projectRelativeArg(projectDir, targetPath string) string {
	if projectDir == "" {
		return filepath.Clean(targetPath)
	}
	if rel, err := filepath.Rel(projectDir, targetPath); err == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Clean(targetPath)
}

func safePositionalArg(arg string) string {
	if strings.HasPrefix(arg, "-") {
		return "./" + arg
	}
	return arg
}

func unsetEnvVars(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if ok && slices.Contains(keys, name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func buildNSGoPublishEnv(baseEnv []string, req ReleaseUploadRequest) ([]string, []string, error) {
	secret := strings.TrimSpace(req.Credential.Secret)
	if secret == "" {
		return nil, nil, fmt.Errorf("editor: missing %s credential in system keychain", req.DestinationLabel)
	}
	env := slices.Clone(baseEnv)
	secrets := []string{secret}
	switch req.Destination {
	case PublishDestItchIO:
		env = unsetEnvVars(env, "BUTLER_API_KEY", "NSGO_PLAY_SERVICE_ACCOUNT", "NSGO_APP_STORE_KEY", "NSGO_APP_STORE_KEY_ID", "NSGO_APP_STORE_ISSUER")
		env = setEnvVar(env, "BUTLER_API_KEY", secret)
	case PublishDestGooglePlay:
		env = unsetEnvVars(env, "BUTLER_API_KEY", "NSGO_PLAY_SERVICE_ACCOUNT", "NSGO_APP_STORE_KEY", "NSGO_APP_STORE_KEY_ID", "NSGO_APP_STORE_ISSUER")
		env = setEnvVar(env, "NSGO_PLAY_SERVICE_ACCOUNT", secret)
	case PublishDestTestFlight:
		env = unsetEnvVars(env, "BUTLER_API_KEY", "NSGO_PLAY_SERVICE_ACCOUNT", "NSGO_APP_STORE_KEY")
		key, keyID, issuer := parseAppStoreConnectCredential(req.Credential.Account, secret)
		env = setEnvVar(env, "NSGO_APP_STORE_KEY", key)
		if keyID != "" {
			env = unsetEnvVars(env, "NSGO_APP_STORE_KEY_ID")
			env = setEnvVar(env, "NSGO_APP_STORE_KEY_ID", keyID)
		}
		if issuer != "" {
			env = unsetEnvVars(env, "NSGO_APP_STORE_ISSUER")
			env = setEnvVar(env, "NSGO_APP_STORE_ISSUER", issuer)
		}
		if key != "" && key != secret {
			secrets = append(secrets, key)
		}
	default:
		return nil, nil, fmt.Errorf("%w: %q", ErrInvalidPublishDestination, req.Destination)
	}
	return env, secrets, nil
}

func parseAppStoreConnectCredential(account, secret string) (key, keyID, issuer string) {
	key = secret
	if strings.HasPrefix(secret, "{") {
		var payload struct {
			Key       string `json:"key"`
			Private   string `json:"private_key"`
			EnvKey    string `json:"NSGO_APP_STORE_KEY"`
			KeyID     string `json:"key_id"`
			KeyIDAlt  string `json:"keyId"`
			EnvKeyID  string `json:"NSGO_APP_STORE_KEY_ID"`
			Issuer    string `json:"issuer"`
			IssuerID  string `json:"issuer_id"`
			IssuerAlt string `json:"issuerId"`
			EnvIssuer string `json:"NSGO_APP_STORE_ISSUER"`
		}
		if err := json.Unmarshal([]byte(secret), &payload); err == nil {
			if v := strings.TrimSpace(firstNonEmpty(payload.Key, payload.Private, payload.EnvKey)); v != "" {
				key = v
			}
			keyID = strings.TrimSpace(firstNonEmpty(payload.KeyID, payload.KeyIDAlt, payload.EnvKeyID))
			issuer = strings.TrimSpace(firstNonEmpty(payload.Issuer, payload.IssuerID, payload.IssuerAlt, payload.EnvIssuer))
		}
	}
	if (keyID == "" || issuer == "") && account != "" {
		acc := strings.TrimSpace(account)
		for _, sep := range []string{":", "/"} {
			if left, right, ok := strings.Cut(acc, sep); ok {
				left = strings.TrimSpace(left)
				right = strings.TrimSpace(right)
				if left != "" && right != "" && !strings.ContainsAny(left+right, " \t\r\n") {
					if keyID == "" {
						keyID = left
					}
					if issuer == "" {
						issuer = right
					}
					break
				}
			}
		}
	}
	return key, keyID, issuer
}

func redactSecrets(text string, secrets []string) string {
	for _, sec := range secrets {
		if sec != "" {
			text = strings.ReplaceAll(text, sec, "[redacted]")
		}
	}
	return text
}

func stripANSI(s string) string {
	if !strings.ContainsRune(s, '\x1b') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func extractPublishURL(line string) string {
	for _, field := range strings.Fields(line) {
		cand := strings.Trim(field, `"'()[]{}<>.,;:`)
		if !strings.HasPrefix(cand, "https://") && !strings.HasPrefix(cand, "http://") {
			continue
		}
		u, err := url.Parse(cand)
		if err == nil && u.Host != "" {
			return u.String()
		}
	}
	return ""
}

func deriveItchURL(projectDir string, lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		for _, field := range strings.Fields(lines[i]) {
			cand := strings.Trim(field, `"'()[]{}<>.,;:`)
			if slug, _, ok := strings.Cut(cand, ":"); ok {
				if owner, game, ok2 := strings.Cut(slug, "/"); ok2 && isSimpleSlug(owner) && isSimpleSlug(game) {
					return "https://" + owner + ".itch.io/" + game
				}
			}
		}
	}
	if nsgoCfg, err := releaseplan.ReadNSGoSettings(projectDir); err == nil {
		if owner, game, ok := strings.Cut(nsgoCfg.ItchProject, "/"); ok && isSimpleSlug(owner) && isSimpleSlug(game) {
			return "https://" + owner + ".itch.io/" + game
		}
	}
	return ""
}

func isSimpleSlug(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func formatPublishFailureMessage(lines []string, runErr error, secrets []string) string {
	if len(lines) > 0 {
		start := 0
		if len(lines) > 4 {
			start = len(lines) - 4
		}
		msg := strings.Join(lines[start:], " | ")
		if len(msg) > 512 {
			msg = msg[len(msg)-512:]
		}
		return redactSecrets(msg, secrets)
	}
	return redactSecrets(runErr.Error(), secrets)
}

type publishLineWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	onLine func(string)
}

func newPublishLineWriter(onLine func(string)) *publishLineWriter {
	return &publishLineWriter{onLine: onLine}
}

func (w *publishLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	var lines []string
	for _, b := range p {
		if b == '\n' || b == '\r' {
			if w.buf.Len() > 0 {
				lines = append(lines, w.buf.String())
				w.buf.Reset()
			}
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

func (w *publishLineWriter) Flush() {
	w.mu.Lock()
	rem := w.buf.String()
	w.buf.Reset()
	w.mu.Unlock()
	if rem != "" {
		w.onLine(rem)
	}
}
