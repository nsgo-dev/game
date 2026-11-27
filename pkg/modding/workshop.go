package modding

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const (
	// WorkshopFile is the deterministic YAML metadata file stored beside mod.yaml in a mod
	// directory published to Steam Workshop.
	WorkshopFile = "workshop.yaml"
	// WorkshopMetadataVersion is the workshop.yaml schema version this package reads and writes.
	WorkshopMetadataVersion = 1
	// WorkshopLegalAgreementURL is the Steam Workshop Subscriber Agreement page shown when
	// Steamworks reports that the publisher must accept the legal agreement before an item
	// becomes visible to other players.
	WorkshopLegalAgreementURL = "https://steamcommunity.com/sharedfiles/workshoplegalagreement"
)

var (
	// ErrWorkshopUnavailable is returned when Steamworks is not available (library absent,
	// Steam client not running, or unsupported platform such as web or mobile).
	ErrWorkshopUnavailable = errors.New("modding: steam workshop unavailable")
	// ErrWorkshopNotFound is returned when a Workshop PublishedFileID does not exist.
	ErrWorkshopNotFound = errors.New("modding: workshop item not found")
	// ErrWorkshopNotInstalled is returned when a Workshop item is not downloaded locally
	// or needs an update before it can be read from disk.
	ErrWorkshopNotInstalled = errors.New("modding: workshop item not installed")
	// ErrWorkshopLegalAgreementRequired is returned when Steamworks requires the author to
	// accept the Steam Workshop Subscriber Agreement before the item is public.
	ErrWorkshopLegalAgreementRequired = errors.New("modding: steam workshop legal agreement acceptance required")
	// ErrInvalidWorkshopMetadata is returned when workshop.yaml fails parsing or validation.
	ErrInvalidWorkshopMetadata = errors.New("modding: invalid workshop metadata")
	// ErrWorkshopUnsafePath is returned when a Workshop directory or file escapes its
	// allowed root or mod directory via path traversal or symlinks.
	ErrWorkshopUnsafePath = errors.New("modding: unsafe workshop path")
)

// PublishedFileID uniquely identifies an item on Steam Workshop (Steamworks PublishedFileId_t).
type PublishedFileID uint64

// String formats id as a decimal Steam Workshop item ID.
func (id PublishedFileID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// WorkshopVisibility controls who can view an item on Steam Workshop.
type WorkshopVisibility string

const (
	// VisibilityPublic makes the item visible to everyone on Steam Workshop.
	VisibilityPublic WorkshopVisibility = "public"
	// VisibilityFriendsOnly restricts visibility to the author's Steam friends.
	VisibilityFriendsOnly WorkshopVisibility = "friendsOnly"
	// VisibilityPrivate restricts visibility to the author and contributors.
	VisibilityPrivate WorkshopVisibility = "private"
	// VisibilityUnlisted hides the item from browse/search while allowing direct link access.
	VisibilityUnlisted WorkshopVisibility = "unlisted"
)

// Valid reports whether v is one of the defined Workshop visibility values.
func (v WorkshopVisibility) Valid() bool {
	switch v {
	case VisibilityPublic, VisibilityFriendsOnly, VisibilityPrivate, VisibilityUnlisted:
		return true
	default:
		return false
	}
}

// steamValue maps v to Steamworks ERemoteStoragePublishedFileVisibility.
func (v WorkshopVisibility) steamValue() uint32 {
	switch v {
	case VisibilityFriendsOnly:
		return 1
	case VisibilityPrivate:
		return 2
	case VisibilityUnlisted:
		return 3
	default:
		return 0 // k_ERemoteStoragePublishedFileVisibilityPublic
	}
}

func visibilityFromSteam(v int32) WorkshopVisibility {
	switch v {
	case 1:
		return VisibilityFriendsOnly
	case 2:
		return VisibilityPrivate
	case 3:
		return VisibilityUnlisted
	default:
		return VisibilityPublic
	}
}

// WorkshopMetadata is the deterministic YAML sidecar (workshop.yaml) stored in a mod or level
// directory published to Steam Workshop. Storing PublishedFileID lets subsequent publishes
// update the existing Workshop item instead of creating duplicates.
type WorkshopMetadata struct {
	// Version is the schema version of workshop.yaml. Files must state [WorkshopMetadataVersion];
	// a value built in code may leave it 0 and [EncodeWorkshopMetadata] writes [WorkshopMetadataVersion].
	Version int `yaml:"version" json:"version"`
	// AppID is the Steam application ID that owns this Workshop item.
	AppID uint32 `yaml:"appId" json:"appId"`
	// PublishedFileID is the Steam Workshop item ID assigned on first publish, or 0 before upload.
	PublishedFileID PublishedFileID `yaml:"publishedFileId,omitempty" json:"publishedFileId,omitempty"`
	// Title is the Steam Workshop item title (defaults to ModManifest.Name when omitted).
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// Description is the Steam Workshop item description (defaults to ModManifest.Description when omitted).
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Tags lists Steam Workshop discovery tags such as "level", "gameplay", or "ui".
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// Visibility controls who can see the Workshop item (public, friendsOnly, private, unlisted).
	Visibility WorkshopVisibility `yaml:"visibility,omitempty" json:"visibility,omitempty"`
	// Preview is a relative slash-separated path inside the mod directory to a preview image.
	Preview string `yaml:"preview,omitempty" json:"preview,omitempty"`
}

const (
	fixWorkshopMeta    = "correct workshop.yaml as docs/en/modding.md describes it"
	fixWorkshopVersion = "set version: 1"
)

// ParseWorkshopMetadata parses and validates a workshop.yaml file. Unknown fields, duplicate
// keys, trailing YAML documents, invalid visibility/preview/tag values, and a missing or
// unsupported version are refused as [ErrInvalidWorkshopMetadata].
func ParseWorkshopMetadata(data []byte) (WorkshopMetadata, error) {
	meta, err := parseWorkshopMetadata(data)
	return meta, loaderr.Locate(err, "", data, fixWorkshopMeta)
}

func parseWorkshopMetadata(data []byte) (WorkshopMetadata, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return WorkshopMetadata{}, loaderr.Errorf("", fixWorkshopMeta, "%w: empty workshop metadata", ErrInvalidWorkshopMetadata)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var meta WorkshopMetadata
	if err := dec.Decode(&meta); err != nil {
		return WorkshopMetadata{}, fmt.Errorf("%w: %w", ErrInvalidWorkshopMetadata, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return WorkshopMetadata{}, loaderr.Errorf("", "keep one YAML document in workshop.yaml; remove the --- and what follows it", "%w: unexpected trailing YAML document", ErrInvalidWorkshopMetadata)
		}
		return WorkshopMetadata{}, fmt.Errorf("%w: %w", ErrInvalidWorkshopMetadata, err)
	}
	if meta.Version != WorkshopMetadataVersion {
		return WorkshopMetadata{}, loaderr.Errorf("version", fixWorkshopVersion, "%w: unsupported version %d (want %d)", ErrInvalidWorkshopMetadata, meta.Version, WorkshopMetadataVersion)
	}
	if err := checkWorkshopVersionLiteral(data); err != nil {
		return WorkshopMetadata{}, err
	}
	if err := meta.Validate(); err != nil {
		return WorkshopMetadata{}, err
	}
	return meta, nil
}

func checkWorkshopVersionLiteral(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidWorkshopMetadata, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return loaderr.Errorf("", fixWorkshopMeta, "%w: workshop metadata must be a mapping", ErrInvalidWorkshopMetadata)
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "version" {
			continue
		}
		val := root.Content[i+1]
		if val.Kind != yaml.ScalarNode || val.ShortTag() != "!!int" || val.Value != strconv.Itoa(WorkshopMetadataVersion) {
			return loaderr.Errorf("version", fixWorkshopVersion, "%w: line %d: version must be the integer %d, not %q", ErrInvalidWorkshopMetadata, val.Line, WorkshopMetadataVersion, val.Value)
		}
	}
	return nil
}

// Validate checks that m has valid Steam Workshop metadata fields. Version may be 0 for a
// value constructed in Go, or [WorkshopMetadataVersion]; [ParseWorkshopMetadata] requires
// files to state version: 1.
func (m WorkshopMetadata) Validate() error {
	if m.Version != 0 && m.Version != WorkshopMetadataVersion {
		return loaderr.Errorf("version", fixWorkshopVersion, "%w: unsupported version %d (want %d)", ErrInvalidWorkshopMetadata, m.Version, WorkshopMetadataVersion)
	}
	if m.AppID == 0 {
		return loaderr.Errorf("appId", "set appId to your Steam application ID (a positive integer)", "%w: appId must be non-zero", ErrInvalidWorkshopMetadata)
	}
	if m.Visibility != "" && !m.Visibility.Valid() {
		return loaderr.Errorf("visibility", "use one of public, friendsOnly, private, or unlisted", "%w: invalid visibility %q", ErrInvalidWorkshopMetadata, m.Visibility)
	}
	if m.Preview != "" {
		if err := validateRelativeAssetPath(m.Preview); err != nil {
			return loaderr.Errorf("preview", "set preview to a relative slash-separated path inside the mod directory, such as \"preview.png\"", "%w: preview %q: %v", ErrInvalidWorkshopMetadata, m.Preview, err)
		}
	}
	seenTags := make(map[string]struct{}, len(m.Tags))
	for _, tag := range m.Tags {
		if strings.TrimSpace(tag) == "" || tag != strings.TrimSpace(tag) {
			return loaderr.Errorf("tags", "use non-empty tag names without leading or trailing spaces", "%w: invalid tag %q", ErrInvalidWorkshopMetadata, tag)
		}
		if _, dup := seenTags[tag]; dup {
			return loaderr.Errorf("tags", "remove duplicate tag entries", "%w: duplicate tag %q", ErrInvalidWorkshopMetadata, tag)
		}
		seenTags[tag] = struct{}{}
	}
	return nil
}

// EncodeWorkshopMetadata validates m and encodes it as deterministic 2-space YAML with fixed
// key order and tags sorted in byte order. A Version of 0 is written as [WorkshopMetadataVersion].
func EncodeWorkshopMetadata(m WorkshopMetadata) ([]byte, error) {
	if m.Version == 0 {
		m.Version = WorkshopMetadataVersion
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if len(m.Tags) > 0 {
		tags := slices.Clone(m.Tags)
		slices.Sort(tags)
		m.Tags = tags
	}
	var node yaml.Node
	if err := node.Encode(m); err != nil {
		return nil, fmt.Errorf("modding: encode workshop metadata: %w", err)
	}
	formatWorkshopTagsFlow(&node)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("modding: encode workshop metadata: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("modding: encode workshop metadata: %w", err)
	}
	return buf.Bytes(), nil
}

func formatWorkshopTagsFlow(doc *yaml.Node) {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "tags" && root.Content[i+1].Kind == yaml.SequenceNode {
			root.Content[i+1].Style = yaml.FlowStyle
		}
	}
}

func validateRelativeAssetPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("path is empty")
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return errors.New("must be a relative slash-separated path")
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("path escapes mod directory")
	}
	return nil
}

// PublishRequest describes a Steam Workshop item update submitted via [WorkshopBackend.SubmitUpdate].
type PublishRequest struct {
	AppID       uint32
	ID          PublishedFileID
	Title       string
	Description string
	Tags        []string
	Visibility  WorkshopVisibility
	PreviewFile string // absolute path to preview image on disk, or empty
	ContentDir  string // absolute path to mod content directory on disk
	ChangeNote  string
}

// PublishResult reports the outcome of submitting a Steam Workshop item update.
type PublishResult struct {
	ID                  PublishedFileID
	NeedsLegalAgreement bool
	LegalAgreementURL   string
}

// InstallInfo describes a locally downloaded Steam Workshop item on disk.
type InstallInfo struct {
	ID         PublishedFileID
	SizeOnDisk uint64
	Folder     string
	Timestamp  time.Time
}

// WorkshopQuery filters items returned by [WorkshopBackend.QueryItems].
type WorkshopQuery struct {
	AppID      uint32
	IDs        []PublishedFileID
	SearchText string
	Tags       []string
	Page       uint32 // 1-based page number (0 is treated as 1)
}

// WorkshopItem describes a Steam Workshop item returned by [WorkshopBackend.QueryItems].
type WorkshopItem struct {
	ID          PublishedFileID
	AppID       uint32
	Title       string
	Description string
	Tags        []string
	Visibility  WorkshopVisibility
	OwnerID     uint64
	UpdatedAt   time.Time
	VotesUp     uint32
	VotesDown   uint32
}

// WorkshopBackend abstracts the Steamworks ISteamUGC workflow.
// Use [NewSteamWorkshop] to talk to the local Steam client on desktop platforms, or
// [NewMemoryWorkshop] for deterministic tests and offline workflows.
type WorkshopBackend interface {
	CreateItem(ctx context.Context, appID uint32) (PublishedFileID, error)
	SubmitUpdate(ctx context.Context, req PublishRequest) (PublishResult, error)
	Subscribe(ctx context.Context, id PublishedFileID) error
	Unsubscribe(ctx context.Context, id PublishedFileID) error
	SubscribedItems(ctx context.Context) ([]PublishedFileID, error)
	ItemInstallInfo(ctx context.Context, id PublishedFileID) (InstallInfo, error)
	DownloadItem(ctx context.Context, id PublishedFileID, highPriority bool) error
	QueryItems(ctx context.Context, query WorkshopQuery) ([]WorkshopItem, error)
}

// SteamWorkshopOptions configures [NewSteamWorkshop].
type SteamWorkshopOptions struct {
	// LibraryPath optionally specifies the exact path to the Steamworks shared library
	// (libsteam_api.dylib on macOS, libsteam_api.so on Linux, steam_api64.dll on Windows).
	// When empty, standard Steamworks library names are probed.
	LibraryPath string
	// SkipInit skips calling SteamAPI_Init and SteamAPI_Shutdown when the host game
	// already manages Steamworks initialization elsewhere.
	SkipInit bool
	// PollInterval sets how often async SteamAPICall_t handles pump SteamAPI_RunCallbacks
	// while waiting for completion. Defaults to 10ms when <= 0.
	PollInterval time.Duration
}

// WorkshopOptions configures a [Workshop] client.
type WorkshopOptions struct {
	// AppID is the default Steam App ID used when creating or publishing items if
	// workshop.yaml and PublishOptions do not specify one.
	AppID uint32
	// AllowedRoots optionally restricts the filesystem roots from which Workshop
	// item install directories may be loaded. When non-empty, ItemInstallInfo.Folder
	// must resolve inside at least one of these directories after symlink evaluation.
	AllowedRoots []string
	// AutoLoad automatically calls [Manager.Load] on newly installed or updated enabled
	// Workshop mods during [Workshop.SyncSubscribed] and [Workshop.InstallSubscribed].
	AutoLoad bool
}

// PublishOptions customizes a single [Workshop.Publish] call.
// Non-zero fields override values from workshop.yaml and mod.yaml.
type PublishOptions struct {
	AppID       uint32
	Title       string
	Description string
	Tags        []string
	Visibility  WorkshopVisibility
	Preview     string // relative slash-separated path inside the mod directory
	ChangeNote  string
}

// InstalledWorkshopItem links a Steam Workshop PublishedFileID to the mod installed in [Manager].
type InstalledWorkshopItem struct {
	ID        PublishedFileID
	ModID     string
	Manifest  ModManifest
	Folder    string
	Timestamp time.Time
}

// SyncResult summarizes the changes [Workshop.SyncSubscribed] applied to [Manager].
type SyncResult struct {
	Installed []InstalledWorkshopItem
	Updated   []InstalledWorkshopItem
	Removed   []InstalledWorkshopItem
	Unchanged []InstalledWorkshopItem
}

// Workshop connects a [WorkshopBackend] to a [*Manager] so players can publish mods and levels
// to Steam Workshop and automatically download, validate, install, update, and remove
// subscribed items through the mod manager.
type Workshop struct {
	backend   WorkshopBackend
	manager   *Manager
	opts      WorkshopOptions
	installed map[PublishedFileID]InstalledWorkshopItem
	byModID   map[string]PublishedFileID
}

// NewWorkshop creates a Workshop client backed by backend and installing into manager.
// manager may be nil for publish-only workflows (such as a mod uploader CLI).
func NewWorkshop(backend WorkshopBackend, manager *Manager, opts WorkshopOptions) *Workshop {
	return &Workshop{
		backend:   backend,
		manager:   manager,
		opts:      opts,
		installed: map[PublishedFileID]InstalledWorkshopItem{},
		byModID:   map[string]PublishedFileID{},
	}
}

// Publish validates the mod in dir, creates a Steam Workshop item if workshop.yaml does not
// yet record a PublishedFileID, submits the update, and writes deterministic workshop.yaml
// metadata back to dir so subsequent publishes update the same item.
//
// If Steamworks reports that the author must accept the Steam Workshop Subscriber Agreement,
// Publish still saves the assigned PublishedFileID to workshop.yaml and returns the populated
// [PublishResult] alongside an error matching [ErrWorkshopLegalAgreementRequired].
func (w *Workshop) Publish(ctx context.Context, dir string, opts PublishOptions) (PublishResult, error) {
	if w.backend == nil {
		return PublishResult{}, ErrWorkshopUnavailable
	}
	if err := ctx.Err(); err != nil {
		return PublishResult{}, err
	}

	manifest, realDir, err := validateConfinedModDir(dir, nil)
	if err != nil {
		return PublishResult{}, err
	}

	metaPath := filepath.Join(realDir, WorkshopFile)
	var meta WorkshopMetadata
	existingData, err := os.ReadFile(metaPath)
	switch {
	case err == nil:
		meta, err = ParseWorkshopMetadata(existingData)
		if err != nil {
			return PublishResult{}, loaderr.WithFile(err, metaPath)
		}
	case errors.Is(err, os.ErrNotExist):
		meta = WorkshopMetadata{Version: WorkshopMetadataVersion}
	default:
		return PublishResult{}, fmt.Errorf("modding: read %s: %w", metaPath, err)
	}

	// Merge effective publishing metadata: PublishOptions > workshop.yaml > defaults/mod.yaml.
	appID := opts.AppID
	if appID == 0 {
		appID = meta.AppID
	}
	if appID == 0 {
		appID = w.opts.AppID
	}
	meta.AppID = appID

	title := opts.Title
	if title == "" {
		title = meta.Title
	}
	if title == "" {
		title = manifest.Name
	}
	meta.Title = title

	description := opts.Description
	if description == "" {
		description = meta.Description
	}
	if description == "" {
		description = manifest.Description
	}
	meta.Description = description

	if opts.Tags != nil {
		meta.Tags = normalizeTags(opts.Tags)
	}
	if opts.Visibility != "" {
		meta.Visibility = opts.Visibility
	}
	if meta.Visibility == "" {
		meta.Visibility = VisibilityPublic
	}
	if opts.Preview != "" {
		meta.Preview = opts.Preview
	}

	if err := meta.Validate(); err != nil {
		return PublishResult{}, err
	}

	var previewAbs string
	if meta.Preview != "" {
		previewAbs, err = resolveConfinedFile(realDir, meta.Preview)
		if err != nil {
			return PublishResult{}, fmt.Errorf("%w: preview %q: %w", ErrInvalidWorkshopMetadata, meta.Preview, err)
		}
	}

	if meta.PublishedFileID == 0 {
		id, err := w.backend.CreateItem(ctx, meta.AppID)
		if err != nil {
			return PublishResult{}, err
		}
		meta.PublishedFileID = id
		// Persist immediately so a transient upload failure never leaks an orphan item on retry.
		if err := writeWorkshopMetadataFile(metaPath, meta); err != nil {
			return PublishResult{}, err
		}
	}

	req := PublishRequest{
		AppID:       meta.AppID,
		ID:          meta.PublishedFileID,
		Title:       meta.Title,
		Description: meta.Description,
		Tags:        slices.Clone(meta.Tags),
		Visibility:  meta.Visibility,
		PreviewFile: previewAbs,
		ContentDir:  realDir,
		ChangeNote:  opts.ChangeNote,
	}

	res, err := w.backend.SubmitUpdate(ctx, req)
	if err != nil {
		return res, err
	}
	if res.ID == 0 {
		res.ID = meta.PublishedFileID
	}
	meta.PublishedFileID = res.ID
	if res.NeedsLegalAgreement && res.LegalAgreementURL == "" {
		res.LegalAgreementURL = WorkshopLegalAgreementURL
	}

	if err := writeWorkshopMetadataFile(metaPath, meta); err != nil {
		return res, err
	}
	if res.NeedsLegalAgreement {
		return res, fmt.Errorf("%w: accept the Steam Workshop Subscriber Agreement at %s", ErrWorkshopLegalAgreementRequired, res.LegalAgreementURL)
	}
	return res, nil
}

// Subscribe subscribes the active player to Workshop item id.
func (w *Workshop) Subscribe(ctx context.Context, id PublishedFileID) error {
	if w.backend == nil {
		return ErrWorkshopUnavailable
	}
	return w.backend.Subscribe(ctx, id)
}

// Unsubscribe unsubscribes the active player from Workshop item id.
func (w *Workshop) Unsubscribe(ctx context.Context, id PublishedFileID) error {
	if w.backend == nil {
		return ErrWorkshopUnavailable
	}
	return w.backend.Unsubscribe(ctx, id)
}

// QueryItems queries Steam Workshop items matching query.
func (w *Workshop) QueryItems(ctx context.Context, query WorkshopQuery) ([]WorkshopItem, error) {
	if w.backend == nil {
		return nil, ErrWorkshopUnavailable
	}
	return w.backend.QueryItems(ctx, query)
}

// Installed returns a deterministic snapshot of Workshop items currently installed in the manager.
func (w *Workshop) Installed() []InstalledWorkshopItem {
	out := make([]InstalledWorkshopItem, 0, len(w.installed))
	for _, item := range w.installed {
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b InstalledWorkshopItem) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	return out
}

// InstallSubscribed ensures subscribed Workshop item id is downloaded, validates its folder
// and mod.yaml safely, and installs or updates it in the mod [Manager].
func (w *Workshop) InstallSubscribed(ctx context.Context, id PublishedFileID) (InstalledWorkshopItem, error) {
	if w.backend == nil {
		return InstalledWorkshopItem{}, ErrWorkshopUnavailable
	}
	if w.manager == nil {
		return InstalledWorkshopItem{}, errors.New("modding: workshop has no mod manager")
	}
	if err := ctx.Err(); err != nil {
		return InstalledWorkshopItem{}, err
	}

	info, err := w.ensureDownloaded(ctx, id, true)
	if err != nil {
		return InstalledWorkshopItem{}, err
	}
	manifest, realFolder, err := validateConfinedModDir(info.Folder, w.opts.AllowedRoots)
	if err != nil {
		return InstalledWorkshopItem{}, fmt.Errorf("modding: workshop item %s: %w", id, err)
	}
	if owner, exists := w.byModID[manifest.ID]; exists && owner != id {
		return InstalledWorkshopItem{}, fmt.Errorf("%w: mod %q is already installed from workshop item %s", ErrDuplicateMod, manifest.ID, owner)
	}

	if prev, ok := w.installed[id]; ok && prev.ModID != manifest.ID {
		_ = w.manager.Uninstall(prev.ModID)
		delete(w.byModID, prev.ModID)
	}

	installedManifest, err := w.manager.Install(realFolder)
	if err != nil {
		return InstalledWorkshopItem{}, fmt.Errorf("modding: install workshop item %s: %w", id, err)
	}

	rec := InstalledWorkshopItem{
		ID:        id,
		ModID:     installedManifest.ID,
		Manifest:  installedManifest,
		Folder:    realFolder,
		Timestamp: info.Timestamp,
	}
	w.installed[id] = rec
	w.byModID[installedManifest.ID] = id

	if w.opts.AutoLoad {
		if modInfo, ok := w.manager.Info(installedManifest.ID); ok && modInfo.Enabled {
			if err := w.manager.Load(installedManifest.ID); err != nil {
				return rec, err
			}
		}
	}
	return rec, nil
}

// SyncSubscribed reconciles the mod [Manager] with all currently subscribed Steam Workshop
// items: unsubscribed items are unloaded and removed, missing or outdated items are downloaded,
// install folders and manifests are validated safely, and valid mods are installed in
// topological dependency order. Failures in individual items are isolated and returned as a
// joined error so healthy subscribed mods still sync.
func (w *Workshop) SyncSubscribed(ctx context.Context) (SyncResult, error) {
	if w.backend == nil {
		return SyncResult{}, ErrWorkshopUnavailable
	}
	if w.manager == nil {
		return SyncResult{}, errors.New("modding: workshop has no mod manager")
	}
	if err := ctx.Err(); err != nil {
		return SyncResult{}, err
	}

	rawIDs, err := w.backend.SubscribedItems(ctx)
	if err != nil {
		return SyncResult{}, err
	}

	subscribedSet := make(map[PublishedFileID]struct{}, len(rawIDs))
	ids := make([]PublishedFileID, 0, len(rawIDs))
	for _, id := range rawIDs {
		if _, dup := subscribedSet[id]; dup {
			continue
		}
		subscribedSet[id] = struct{}{}
		ids = append(ids, id)
	}
	slices.Sort(ids)

	var (
		res  SyncResult
		errs []error
	)

	// 1. Remove items that are no longer subscribed (in deterministic ID order).
	var toRemove []PublishedFileID
	for id := range w.installed {
		if _, stillSubscribed := subscribedSet[id]; !stillSubscribed {
			toRemove = append(toRemove, id)
		}
	}
	slices.Sort(toRemove)
	for _, id := range toRemove {
		prev := w.installed[id]
		if err := w.manager.Uninstall(prev.ModID); err != nil && !errors.Is(err, ErrNotFound) {
			errs = append(errs, fmt.Errorf("modding: uninstall workshop item %s (%s): %w", id, prev.ModID, err))
			continue
		}
		delete(w.installed, id)
		if w.byModID[prev.ModID] == id {
			delete(w.byModID, prev.ModID)
		}
		res.Removed = append(res.Removed, prev)
	}

	// 2. Download and validate subscribed items.
	var pending []workshopCandidate
	pendingModIDs := map[string]PublishedFileID{}

	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		info, err := w.ensureDownloaded(ctx, id, false)
		if err != nil {
			errs = append(errs, fmt.Errorf("modding: workshop item %s: %w", id, err))
			continue
		}
		manifest, realFolder, err := validateConfinedModDir(info.Folder, w.opts.AllowedRoots)
		if err != nil {
			errs = append(errs, fmt.Errorf("modding: workshop item %s: %w", id, err))
			continue
		}
		if prev, ok := w.installed[id]; ok {
			if prev.Folder == realFolder && prev.Timestamp.Equal(info.Timestamp) && prev.ModID == manifest.ID {
				if _, exists := w.manager.Info(prev.ModID); exists {
					res.Unchanged = append(res.Unchanged, prev)
					continue
				}
			}
		}
		if owner, exists := w.byModID[manifest.ID]; exists && owner != id {
			errs = append(errs, fmt.Errorf("modding: workshop item %s: %w: mod %q is already installed from workshop item %s", id, ErrDuplicateMod, manifest.ID, owner))
			continue
		}
		if otherID, dup := pendingModIDs[manifest.ID]; dup {
			errs = append(errs, fmt.Errorf("modding: workshop item %s: %w: mod %q collides with workshop item %s", id, ErrDuplicateMod, manifest.ID, otherID))
			continue
		}
		pendingModIDs[manifest.ID] = id
		prev, wasKnown := w.installed[id]
		pending = append(pending, workshopCandidate{
			id:         id,
			info:       info,
			realFolder: realFolder,
			manifest:   manifest,
			wasKnown:   wasKnown,
			prev:       prev,
		})
	}

	if len(pending) == 0 {
		return res, errors.Join(errs...)
	}

	// 3. Try installing all pending items together first (handles coordinated dependency updates).
	// If any single item has a bad dependency or gameVersion, fall back to topologically ordered
	// per-item installation so healthy mods still succeed.
	var batch []validatedMod
	for _, c := range pending {
		batch = append(batch, validatedMod{
			manifest: c.manifest,
			fsys:     os.DirFS(c.realFolder),
			dir:      filepath.Base(c.realFolder),
		})
	}

	var applied []workshopCandidate
	if w.validateBatchCompatible(pending) && w.manager.installValidated(batch) == nil {
		for _, c := range pending {
			if c.wasKnown && c.prev.ModID != c.manifest.ID {
				_ = w.manager.Uninstall(c.prev.ModID)
				delete(w.byModID, c.prev.ModID)
			}
			applied = append(applied, c)
		}
	} else {
		// Sort pending candidates so dependencies install before dependents.
		orderedPending := orderCandidatesForInstall(pending)
		for _, c := range orderedPending {
			if c.wasKnown && c.prev.ModID != c.manifest.ID {
				_ = w.manager.Uninstall(c.prev.ModID)
				delete(w.byModID, c.prev.ModID)
			}
			if _, err := w.manager.Install(c.realFolder); err != nil {
				errs = append(errs, fmt.Errorf("modding: install workshop item %s (%s): %w", c.id, c.manifest.ID, err))
				continue
			}
			applied = append(applied, c)
		}
	}

	for _, c := range applied {
		rec := InstalledWorkshopItem{
			ID:        c.id,
			ModID:     c.manifest.ID,
			Manifest:  c.manifest,
			Folder:    c.realFolder,
			Timestamp: c.info.Timestamp,
		}
		w.installed[c.id] = rec
		w.byModID[c.manifest.ID] = c.id
		if c.wasKnown {
			res.Updated = append(res.Updated, rec)
		} else {
			res.Installed = append(res.Installed, rec)
		}
	}

	if w.opts.AutoLoad {
		// Load newly installed/updated items in manager topological order.
		appliedIDs := make(map[string]struct{}, len(applied))
		for _, c := range applied {
			appliedIDs[c.manifest.ID] = struct{}{}
		}
		for _, info := range w.manager.List() {
			if _, ok := appliedIDs[info.Manifest.ID]; !ok {
				continue
			}
			if !info.Enabled {
				continue
			}
			if err := w.manager.Load(info.Manifest.ID); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return res, errors.Join(errs...)
}

type workshopCandidate struct {
	id         PublishedFileID
	info       InstallInfo
	realFolder string
	manifest   ModManifest
	wasKnown   bool
	prev       InstalledWorkshopItem
}

func (w *Workshop) validateBatchCompatible(pending []workshopCandidate) bool {
	if w.manager.opts.GameVersion == "" {
		return true
	}
	for _, c := range pending {
		if c.manifest.GameVersion != "" {
			if err := checkGameVersion(w.manager.opts.GameVersion, c.manifest); err != nil {
				return false
			}
		}
	}
	return true
}

func orderCandidatesForInstall(items []workshopCandidate) []workshopCandidate {
	byID := make(map[string]int, len(items))
	for i, it := range items {
		byID[it.manifest.ID] = i
	}
	inDeg := make([]int, len(items))
	dependents := make([][]int, len(items))
	for i, it := range items {
		for depID := range it.manifest.Dependencies {
			if depIdx, ok := byID[depID]; ok && depIdx != i {
				dependents[depIdx] = append(dependents[depIdx], i)
				inDeg[i]++
			}
		}
	}
	var ready []int
	for i := range items {
		if inDeg[i] == 0 {
			ready = append(ready, i)
		}
	}
	slices.SortFunc(ready, func(a, b int) int { return strings.Compare(items[a].manifest.ID, items[b].manifest.ID) })

	out := make([]workshopCandidate, 0, len(items))
	visited := make([]bool, len(items))
	for len(ready) > 0 {
		cur := ready[0]
		ready = ready[1:]
		visited[cur] = true
		out = append(out, items[cur])
		for _, nxt := range dependents[cur] {
			inDeg[nxt]--
			if inDeg[nxt] == 0 {
				ready = append(ready, nxt)
				slices.SortFunc(ready, func(a, b int) int { return strings.Compare(items[a].manifest.ID, items[b].manifest.ID) })
			}
		}
	}
	// Append any remaining cyclic nodes deterministically so Manager.Install reports their exact error.
	for i, it := range items {
		if !visited[i] {
			out = append(out, it)
		}
	}
	return out
}

func (w *Workshop) ensureDownloaded(ctx context.Context, id PublishedFileID, highPriority bool) (InstallInfo, error) {
	info, err := w.backend.ItemInstallInfo(ctx, id)
	if err == nil && info.Folder != "" {
		return info, nil
	}
	if err != nil && !errors.Is(err, ErrWorkshopNotInstalled) {
		return InstallInfo{}, err
	}
	if err := w.backend.DownloadItem(ctx, id, highPriority); err != nil {
		return InstallInfo{}, err
	}
	info, err = w.backend.ItemInstallInfo(ctx, id)
	if err != nil {
		return InstallInfo{}, err
	}
	if info.Folder == "" {
		return InstallInfo{}, fmt.Errorf("%w: workshop item %s has empty install folder", ErrWorkshopNotInstalled, id)
	}
	return info, nil
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			// Keep invalid empty tag so Validate reports it clearly.
			out = append(out, t)
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	slices.Sort(out)
	return out
}

func writeWorkshopMetadataFile(dst string, meta WorkshopMetadata) error {
	data, err := EncodeWorkshopMetadata(meta)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("modding: write %s: %w", dst, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("modding: write %s: %w", dst, err)
	}
	return nil
}

// validateConfinedModDir verifies that folder exists, is confined inside allowedRoots (if non-empty),
// contains no symlinks escaping folder, and holds a valid mod.yaml whose entry file exists inside folder.
func validateConfinedModDir(folder string, allowedRoots []string) (ModManifest, string, error) {
	if strings.TrimSpace(folder) == "" {
		return ModManifest{}, "", fmt.Errorf("%w: empty mod directory", ErrWorkshopUnsafePath)
	}
	absFolder, err := filepath.Abs(folder)
	if err != nil {
		return ModManifest{}, "", fmt.Errorf("%w: %v", ErrWorkshopUnsafePath, err)
	}
	realFolder, err := filepath.EvalSymlinks(absFolder)
	if err != nil {
		return ModManifest{}, "", fmt.Errorf("modding: open mod dir %q: %w", folder, err)
	}
	fi, err := os.Stat(realFolder)
	if err != nil {
		return ModManifest{}, "", fmt.Errorf("modding: stat mod dir %q: %w", folder, err)
	}
	if !fi.IsDir() {
		return ModManifest{}, "", fmt.Errorf("modding: %q is not a directory", folder)
	}

	if len(allowedRoots) > 0 {
		allowed := false
		for _, root := range allowedRoots {
			absRoot, err := filepath.Abs(root)
			if err != nil {
				continue
			}
			realRoot, err := filepath.EvalSymlinks(absRoot)
			if err != nil {
				realRoot = absRoot
			}
			if isPathWithin(realFolder, realRoot) {
				allowed = true
				break
			}
		}
		if !allowed {
			return ModManifest{}, "", fmt.Errorf("%w: %q is outside allowed workshop roots", ErrWorkshopUnsafePath, realFolder)
		}
	}

	if err := checkNoEscapingSymlinks(realFolder, realFolder); err != nil {
		return ModManifest{}, "", err
	}

	manifestPath := filepath.Join(realFolder, ManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, legacyErr := os.Stat(filepath.Join(realFolder, legacyManifestFile)); legacyErr == nil {
				return ModManifest{}, "", fmt.Errorf("%w: %s: mod.json is no longer read; rewrite it as %s", ErrInvalidManifest, realFolder, ManifestFile)
			}
			return ModManifest{}, "", loaderr.Locate(fmt.Errorf("%w: %w", ErrInvalidManifest, err), manifestPath, nil, fixReadManifest)
		}
		return ModManifest{}, "", loaderr.Locate(fmt.Errorf("read: %w", err), manifestPath, nil, fixReadManifest)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return ModManifest{}, "", loaderr.WithFile(err, manifestPath)
	}
	if _, err := resolveConfinedFile(realFolder, manifest.Entry); err != nil {
		return ModManifest{}, "", loaderr.Locate(loaderr.Errorf("entry", "add the script at this path in the mod's directory, or correct entry", "%w: mod %q entry %q: %v", ErrInvalidManifest, manifest.ID, manifest.Entry, err), manifestPath, data, "")
	}
	return manifest, realFolder, nil
}

func resolveConfinedFile(realDir, relSlashPath string) (string, error) {
	if err := validateRelativeAssetPath(relSlashPath); err != nil {
		return "", fmt.Errorf("%w: %v", ErrWorkshopUnsafePath, err)
	}
	candidate := filepath.Join(realDir, filepath.FromSlash(path.Clean(relSlashPath)))
	realTarget, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !isPathWithin(realTarget, realDir) {
		return "", fmt.Errorf("%w: %q escapes mod directory", ErrWorkshopUnsafePath, relSlashPath)
	}
	fi, err := os.Stat(realTarget)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return "", fmt.Errorf("%q is a directory", relSlashPath)
	}
	return realTarget, nil
}

func checkNoEscapingSymlinks(root, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		linfo, err := os.Lstat(full)
		if err != nil {
			return err
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(full)
			if err != nil {
				return fmt.Errorf("%w: unresolved symlink %q: %v", ErrWorkshopUnsafePath, full, err)
			}
			if !isPathWithin(target, root) {
				return fmt.Errorf("%w: symlink %q points outside mod directory", ErrWorkshopUnsafePath, full)
			}
			continue
		}
		if linfo.IsDir() {
			if err := checkNoEscapingSymlinks(root, full); err != nil {
				return err
			}
		}
	}
	return nil
}

func isPathWithin(target, root string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

// MemoryWorkshop is an in-memory and filesystem-backed [WorkshopBackend] that simulates Steam
// Workshop publishing, querying, subscribing, downloading, and updates without requiring the
// Steam client. It supports multiple users via [MemoryWorkshop.AsUser] so one user can publish
// a mod and another player can subscribe and install it through [Manager].
type MemoryWorkshop struct {
	mu      sync.Mutex
	rootDir string
	nextID  PublishedFileID
	items   map[PublishedFileID]*memoryWorkshopItem
	users   map[uint64]*memoryWorkshopUser
	clock   time.Time
}

type memoryWorkshopItem struct {
	item          WorkshopItem
	contentDir    string
	previewFile   string
	sizeOnDisk    uint64
	revision      uint64
	lastChangeLog string
}

type memoryWorkshopUser struct {
	id                  uint64
	needsLegalAgreement bool
	subscribed          map[PublishedFileID]struct{}
	downloads           map[PublishedFileID]memoryDownload
}

type memoryDownload struct {
	folder    string
	size      uint64
	timestamp time.Time
	revision  uint64
}

// NewMemoryWorkshop creates a [MemoryWorkshop] storing published item snapshots and per-user
// downloaded copies inside rootDir.
func NewMemoryWorkshop(rootDir string) *MemoryWorkshop {
	return &MemoryWorkshop{
		rootDir: rootDir,
		nextID:  1000,
		items:   map[PublishedFileID]*memoryWorkshopItem{},
		users:   map[uint64]*memoryWorkshopUser{},
		clock:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// AsUser returns a [WorkshopBackend] view scoped to steamUserID sharing this Workshop catalog.
func (m *MemoryWorkshop) AsUser(steamUserID uint64) WorkshopBackend {
	return &memoryWorkshopView{hub: m, userID: steamUserID}
}

// SetNeedsLegalAgreement configures whether SubmitUpdate reports NeedsLegalAgreement for userID
// (use 0 for the default user when calling methods directly on *MemoryWorkshop).
func (m *MemoryWorkshop) SetNeedsLegalAgreement(userID uint64, required bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userLocked(userID).needsLegalAgreement = required
}

// ItemContentDir returns the stored Workshop snapshot directory for id, if published.
func (m *MemoryWorkshop) ItemContentDir(id PublishedFileID) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok || it.contentDir == "" {
		return "", false
	}
	return it.contentDir, true
}

// CreateItem implements [WorkshopBackend] for the default user (ID 0).
func (m *MemoryWorkshop) CreateItem(ctx context.Context, appID uint32) (PublishedFileID, error) {
	return m.createItem(ctx, 0, appID)
}

// SubmitUpdate implements [WorkshopBackend] for the default user (ID 0).
func (m *MemoryWorkshop) SubmitUpdate(ctx context.Context, req PublishRequest) (PublishResult, error) {
	return m.submitUpdate(ctx, 0, req)
}

// Subscribe implements [WorkshopBackend] for the default user (ID 0).
func (m *MemoryWorkshop) Subscribe(ctx context.Context, id PublishedFileID) error {
	return m.subscribe(ctx, 0, id)
}

// Unsubscribe implements [WorkshopBackend] for the default user (ID 0).
func (m *MemoryWorkshop) Unsubscribe(ctx context.Context, id PublishedFileID) error {
	return m.unsubscribe(ctx, 0, id)
}

// SubscribedItems implements [WorkshopBackend] for the default user (ID 0).
func (m *MemoryWorkshop) SubscribedItems(ctx context.Context) ([]PublishedFileID, error) {
	return m.subscribedItems(ctx, 0)
}

// ItemInstallInfo implements [WorkshopBackend] for the default user (ID 0).
func (m *MemoryWorkshop) ItemInstallInfo(ctx context.Context, id PublishedFileID) (InstallInfo, error) {
	return m.itemInstallInfo(ctx, 0, id)
}

// DownloadItem implements [WorkshopBackend] for the default user (ID 0).
func (m *MemoryWorkshop) DownloadItem(ctx context.Context, id PublishedFileID, highPriority bool) error {
	return m.downloadItem(ctx, 0, id, highPriority)
}

// QueryItems implements [WorkshopBackend] for the default user (ID 0).
func (m *MemoryWorkshop) QueryItems(ctx context.Context, query WorkshopQuery) ([]WorkshopItem, error) {
	return m.queryItems(ctx, query)
}

type memoryWorkshopView struct {
	hub    *MemoryWorkshop
	userID uint64
}

func (v *memoryWorkshopView) CreateItem(ctx context.Context, appID uint32) (PublishedFileID, error) {
	return v.hub.createItem(ctx, v.userID, appID)
}
func (v *memoryWorkshopView) SubmitUpdate(ctx context.Context, req PublishRequest) (PublishResult, error) {
	return v.hub.submitUpdate(ctx, v.userID, req)
}
func (v *memoryWorkshopView) Subscribe(ctx context.Context, id PublishedFileID) error {
	return v.hub.subscribe(ctx, v.userID, id)
}
func (v *memoryWorkshopView) Unsubscribe(ctx context.Context, id PublishedFileID) error {
	return v.hub.unsubscribe(ctx, v.userID, id)
}
func (v *memoryWorkshopView) SubscribedItems(ctx context.Context) ([]PublishedFileID, error) {
	return v.hub.subscribedItems(ctx, v.userID)
}
func (v *memoryWorkshopView) ItemInstallInfo(ctx context.Context, id PublishedFileID) (InstallInfo, error) {
	return v.hub.itemInstallInfo(ctx, v.userID, id)
}
func (v *memoryWorkshopView) DownloadItem(ctx context.Context, id PublishedFileID, highPriority bool) error {
	return v.hub.downloadItem(ctx, v.userID, id, highPriority)
}
func (v *memoryWorkshopView) QueryItems(ctx context.Context, query WorkshopQuery) ([]WorkshopItem, error) {
	return v.hub.queryItems(ctx, query)
}

func (m *MemoryWorkshop) userLocked(userID uint64) *memoryWorkshopUser {
	u, ok := m.users[userID]
	if !ok {
		u = &memoryWorkshopUser{
			id:         userID,
			subscribed: map[PublishedFileID]struct{}{},
			downloads:  map[PublishedFileID]memoryDownload{},
		}
		m.users[userID] = u
	}
	return u
}

func (m *MemoryWorkshop) nextTimeLocked() time.Time {
	m.clock = m.clock.Add(time.Second)
	return m.clock
}

func (m *MemoryWorkshop) createItem(ctx context.Context, userID uint64, appID uint32) (PublishedFileID, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if appID == 0 {
		return 0, fmt.Errorf("%w: appId must be non-zero", ErrInvalidWorkshopMetadata)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	m.items[id] = &memoryWorkshopItem{
		item: WorkshopItem{
			ID:         id,
			AppID:      appID,
			Visibility: VisibilityPublic,
			OwnerID:    userID,
			UpdatedAt:  m.nextTimeLocked(),
		},
	}
	return id, nil
}

func (m *MemoryWorkshop) submitUpdate(ctx context.Context, userID uint64, req PublishRequest) (PublishResult, error) {
	if err := ctx.Err(); err != nil {
		return PublishResult{}, err
	}
	if req.ID == 0 {
		return PublishResult{}, fmt.Errorf("%w: zero PublishedFileID", ErrWorkshopNotFound)
	}
	if req.ContentDir == "" {
		return PublishResult{}, errors.New("modding: submit update requires ContentDir")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.items[req.ID]
	if !ok {
		return PublishResult{}, fmt.Errorf("%w: %s", ErrWorkshopNotFound, req.ID)
	}
	if req.AppID != 0 {
		rec.item.AppID = req.AppID
	}

	rec.revision++
	snapshotDir := filepath.Join(m.rootDir, "published", req.ID.String(), fmt.Sprintf("rev-%d", rec.revision))
	size, err := copyDirTree(req.ContentDir, snapshotDir)
	if err != nil {
		return PublishResult{}, fmt.Errorf("modding: copy workshop content: %w", err)
	}

	if req.Title != "" {
		rec.item.Title = req.Title
	}
	if req.Description != "" {
		rec.item.Description = req.Description
	}
	if req.Tags != nil {
		rec.item.Tags = slices.Clone(req.Tags)
	}
	if req.Visibility != "" {
		rec.item.Visibility = req.Visibility
	}
	rec.item.UpdatedAt = m.nextTimeLocked()
	rec.contentDir = snapshotDir
	rec.previewFile = req.PreviewFile
	rec.sizeOnDisk = size
	rec.lastChangeLog = req.ChangeNote

	u := m.userLocked(userID)
	res := PublishResult{
		ID:                  req.ID,
		NeedsLegalAgreement: u.needsLegalAgreement,
	}
	if res.NeedsLegalAgreement {
		res.LegalAgreementURL = WorkshopLegalAgreementURL
	}
	return res, nil
}

func (m *MemoryWorkshop) subscribe(ctx context.Context, userID uint64, id PublishedFileID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[id]; !ok {
		return fmt.Errorf("%w: %s", ErrWorkshopNotFound, id)
	}
	m.userLocked(userID).subscribed[id] = struct{}{}
	return nil
}

func (m *MemoryWorkshop) unsubscribe(ctx context.Context, userID uint64, id PublishedFileID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.userLocked(userID)
	delete(u.subscribed, id)
	if dl, ok := u.downloads[id]; ok {
		_ = os.RemoveAll(dl.folder)
		delete(u.downloads, id)
	}
	return nil
}

func (m *MemoryWorkshop) subscribedItems(ctx context.Context, userID uint64) ([]PublishedFileID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.userLocked(userID)
	out := make([]PublishedFileID, 0, len(u.subscribed))
	for id := range u.subscribed {
		out = append(out, id)
	}
	slices.Sort(out)
	return out, nil
}

func (m *MemoryWorkshop) itemInstallInfo(ctx context.Context, userID uint64, id PublishedFileID) (InstallInfo, error) {
	if err := ctx.Err(); err != nil {
		return InstallInfo{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.items[id]
	if !ok {
		return InstallInfo{}, fmt.Errorf("%w: %s", ErrWorkshopNotFound, id)
	}
	u := m.userLocked(userID)
	dl, ok := u.downloads[id]
	if !ok || dl.revision != rec.revision {
		return InstallInfo{}, fmt.Errorf("%w: %s", ErrWorkshopNotInstalled, id)
	}
	return InstallInfo{
		ID:         id,
		SizeOnDisk: dl.size,
		Folder:     dl.folder,
		Timestamp:  dl.timestamp,
	}, nil
}

func (m *MemoryWorkshop) downloadItem(ctx context.Context, userID uint64, id PublishedFileID, _ bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.items[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrWorkshopNotFound, id)
	}
	if rec.contentDir == "" {
		return fmt.Errorf("%w: %s has no uploaded content", ErrWorkshopNotInstalled, id)
	}
	dst := filepath.Join(m.rootDir, "users", strconv.FormatUint(userID, 10), "workshop", "content", strconv.FormatUint(uint64(rec.item.AppID), 10), id.String())
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	size, err := copyDirTree(rec.contentDir, dst)
	if err != nil {
		return fmt.Errorf("modding: download workshop item %s: %w", id, err)
	}
	u := m.userLocked(userID)
	u.downloads[id] = memoryDownload{
		folder:    dst,
		size:      size,
		timestamp: rec.item.UpdatedAt,
		revision:  rec.revision,
	}
	return nil
}

func (m *MemoryWorkshop) queryItems(ctx context.Context, query WorkshopQuery) ([]WorkshopItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var idFilter map[PublishedFileID]struct{}
	if len(query.IDs) > 0 {
		idFilter = make(map[PublishedFileID]struct{}, len(query.IDs))
		for _, id := range query.IDs {
			idFilter[id] = struct{}{}
		}
	}
	needle := strings.ToLower(strings.TrimSpace(query.SearchText))

	var matched []WorkshopItem
	for _, rec := range m.items {
		it := rec.item
		if len(idFilter) > 0 {
			if _, ok := idFilter[it.ID]; !ok {
				continue
			}
		}
		if query.AppID != 0 && it.AppID != query.AppID {
			continue
		}
		if needle != "" {
			hay := strings.ToLower(it.Title + "\n" + it.Description)
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		if len(query.Tags) > 0 && !hasAllTags(it.Tags, query.Tags) {
			continue
		}
		it.Tags = slices.Clone(it.Tags)
		matched = append(matched, it)
	}

	slices.SortFunc(matched, func(a, b WorkshopItem) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})

	const pageSize = 50
	page := query.Page
	if page == 0 {
		page = 1
	}
	start := int(page-1) * pageSize
	if start >= len(matched) {
		return nil, nil
	}
	end := min(start+pageSize, len(matched))
	return matched[start:end], nil
}

func hasAllTags(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func copyDirTree(src, dst string) (uint64, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	var total uint64
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: refusing to copy symlink %q", ErrWorkshopUnsafePath, rel)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		total += uint64(len(data))
		return nil
	})
	return total, err
}
