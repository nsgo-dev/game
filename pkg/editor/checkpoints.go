package editor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	"time"
	"unicode"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"go.yaml.in/yaml/v3"
)

// CheckpointVersion is the schema version of checkpoint manifest YAML files stored under .nsgo/checkpoints/.
const CheckpointVersion = 1

// CheckpointSchemaURL is the published JSON Schema URL for checkpoint manifests.
const CheckpointSchemaURL = "https://game.nsgo.dev/schemas/checkpoint.v1.json"

// CheckpointsDir is the slash-separated path from a project root where persistent turn checkpoints are stored.
const CheckpointsDir = ".nsgo/checkpoints"

// CheckpointManifestFile is the manifest filename inside each checkpoint directory under CheckpointsDir.
const CheckpointManifestFile = "manifest.yaml"

// DefaultCheckpointMaxBytes is the default total disk budget for .nsgo/checkpoints before the oldest checkpoints are pruned (128 MiB).
const DefaultCheckpointMaxBytes int64 = 128 << 20

const (
	// maxCheckpointManifestBytes caps manifest.yaml size when loading from disk.
	maxCheckpointManifestBytes int64 = 2 << 20
	// maxCheckpointFileBytes caps any single project file captured into or restored from a checkpoint (32 MiB).
	maxCheckpointFileBytes int64 = 32 << 20
	// maxCheckpointFilesPerTurn caps how many distinct files one checkpoint manifest may record.
	maxCheckpointFilesPerTurn = 500
	// maxCheckpointDirs caps how many checkpoint directories LoadCheckpoints scans per project.
	maxCheckpointDirs = 2000
)

// CheckpointKind identifies why a persistent checkpoint was created.
type CheckpointKind string

const (
	// CheckpointKindTurn marks a checkpoint created for an agent turn's edits.
	CheckpointKindTurn CheckpointKind = "turn"
	// CheckpointKindTask marks a checkpoint taken before an autonomous background task starts.
	CheckpointKindTask CheckpointKind = "task"
	// CheckpointKindAutomation marks a checkpoint taken before an automation run starts.
	CheckpointKindAutomation CheckpointKind = "automation"
)

// CheckpointManifest is the strict versioned YAML manifest stored at .nsgo/checkpoints/<id>/manifest.yaml.
type CheckpointManifest struct {
	// Version is the schema version; it must equal CheckpointVersion (1).
	Version int `yaml:"version"`
	// ID is the unique checkpoint directory name (for example "20261126T144100Z-0001").
	ID string `yaml:"id"`
	// CreatedAt is the RFC3339 UTC timestamp when the checkpoint was started.
	CreatedAt string `yaml:"createdAt"`
	// Who identifies who initiated the checkpoint ("agent", "you", or "git").
	Who HistoryAuthor `yaml:"who"`
	// Kind identifies why the checkpoint was taken ("turn", "task", or "automation").
	Kind CheckpointKind `yaml:"kind"`
	// Title is the short human-readable summary shown in the History panel.
	Title string `yaml:"title"`
	// Detail is optional secondary context (such as change counts or trigger details).
	Detail string `yaml:"detail,omitempty"`
	// Files lists every project file captured before its first write in this checkpoint, sorted by path.
	Files []CheckpointFile `yaml:"files,omitempty"`
}

// CheckpointFile records one project file captured before its first write in a checkpoint.
type CheckpointFile struct {
	// Path is the slash-separated path from the project root (for example "scenes/level_01.scene.yaml").
	Path string `yaml:"path"`
	// Existed reports whether the file existed before the turn wrote it; false means restoring deletes the file.
	Existed bool `yaml:"existed"`
	// Size is the saved file size in bytes when Existed is true.
	Size int64 `yaml:"size,omitempty"`
	// SHA256 is the lowercase hex SHA-256 digest of the saved bytes when Existed is true.
	SHA256 string `yaml:"sha256,omitempty"`
	// Mode is the optional 4-digit octal Unix file mode (for example "0755") when non-default.
	Mode string `yaml:"mode,omitempty"`
}

// CheckpointSummary pairs a loaded CheckpointManifest with its total on-disk footprint in bytes.
type CheckpointSummary struct {
	CheckpointManifest
	// TotalBytes is the total size on disk of the checkpoint directory (manifest plus saved files).
	TotalBytes int64
}

// CheckpointOptions configures a new turn, task, or automation checkpoint.
type CheckpointOptions struct {
	// Who defaults to HistoryAuthorAgent when empty.
	Who HistoryAuthor
	// Kind defaults to CheckpointKindTurn when empty.
	Kind CheckpointKind
	// Title is shown in the History panel; defaults to a descriptive label when empty.
	Title string
	// Detail is optional secondary context shown alongside the title.
	Detail string
	// PreCaptureFiles lists project-relative paths to snapshot immediately before work starts.
	PreCaptureFiles []string
}

// activeCheckpointTurn tracks an in-progress agent turn, task, or automation checkpoint.
type activeCheckpointTurn struct {
	manifest     CheckpointManifest
	dir          string
	recorded     map[string]bool
	persisted    bool
	keepEmpty    bool
	countedAgent bool
	projectDir   string
}

type checkpointState struct {
	active *activeCheckpointTurn
	list   []CheckpointSummary
	seq    uint64
}

// ParseCheckpointManifest strictly decodes and validates a versioned checkpoint manifest YAML document,
// refusing empty documents, duplicate keys, unknown fields, trailing documents, and unsupported versions.
func ParseCheckpointManifest(data []byte) (*CheckpointManifest, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("provide a checkpoint manifest starting with version: %d", CheckpointVersion), "editor: checkpoint manifest: empty document (missing version)"), "", data, "")
	}
	if int64(len(data)) > maxCheckpointManifestBytes {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("keep checkpoint manifests under %d bytes", maxCheckpointManifestBytes), "editor: checkpoint manifest: document exceeds maximum size (%d bytes)", maxCheckpointManifestBytes), "", data, "")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("editor: checkpoint manifest: parse: %w", err), "", data, "check the YAML syntax in the checkpoint manifest")
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("write the checkpoint manifest as a YAML mapping starting with version: %d", CheckpointVersion), "editor: checkpoint manifest: document must be a YAML mapping with version"), "", data, "")
	}
	if err := checkDuplicateYAMLKeys(root.Content[0]); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	hasVersion := false
	for i := 0; i+1 < len(root.Content[0].Content); i += 2 {
		if root.Content[0].Content[i].Value == "version" {
			hasVersion = true
			break
		}
	}
	if !hasVersion {
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("add version: %d at the top of the checkpoint manifest", CheckpointVersion), "editor: checkpoint manifest: missing required field version"), "", data, "")
	}

	var m CheckpointManifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("editor: checkpoint manifest: decode: %w", err), "", data, "check the YAML syntax and field names in the checkpoint manifest")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the checkpoint manifest and remove any --- separator", "editor: checkpoint manifest: unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("editor: checkpoint manifest: decode: %w", err), "", data, "remove trailing YAML content after the checkpoint manifest")
	}
	if err := ValidateCheckpointManifest(&m); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &m, nil
}

// ValidateCheckpointManifest checks that m conforms to version 1 of the checkpoint manifest schema.
func ValidateCheckpointManifest(m *CheckpointManifest) error {
	if m == nil {
		return loaderr.Errorf("", "pass a non-nil *editor.CheckpointManifest to ValidateCheckpointManifest", "editor: checkpoint manifest: nil document")
	}
	if m.Version != CheckpointVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the checkpoint manifest", CheckpointVersion), "editor: checkpoint manifest: unsupported version %d (want %d)", m.Version, CheckpointVersion)
	}
	if err := validateCheckpointID(m.ID); err != nil {
		return loaderr.Errorf("id", "set id to 1..128 ASCII letters, digits, '-' or '_' without '..'", "editor: checkpoint manifest: %w", err)
	}
	if m.CreatedAt == "" {
		return loaderr.Errorf("createdAt", "set createdAt to an RFC3339 UTC timestamp such as 2026-01-02T15:04:05Z", "editor: checkpoint manifest: createdAt is required")
	}
	if _, err := time.Parse(time.RFC3339, m.CreatedAt); err != nil {
		return loaderr.Errorf("createdAt", "format createdAt as an RFC3339 timestamp such as 2026-01-02T15:04:05Z", "editor: checkpoint manifest: createdAt %q must be RFC3339: %w", m.CreatedAt, err)
	}
	switch m.Who {
	case HistoryAuthorAgent, HistoryAuthorYou, HistoryAuthorGit:
	default:
		return loaderr.Errorf("who", "set who to agent, you, or git", "editor: checkpoint manifest: invalid who %q (want agent, you, or git)", m.Who)
	}
	switch m.Kind {
	case CheckpointKindTurn, CheckpointKindTask, CheckpointKindAutomation:
	default:
		return loaderr.Errorf("kind", "set kind to turn, task, or automation", "editor: checkpoint manifest: invalid kind %q (want turn, task, or automation)", m.Kind)
	}
	if err := validateCheckpointText("title", m.Title, true); err != nil {
		return loaderr.Errorf("title", "set title to a non-empty single-line summary without surrounding spaces or control characters", "editor: checkpoint manifest: %w", err)
	}
	if err := validateCheckpointText("detail", m.Detail, false); err != nil {
		return loaderr.Errorf("detail", "set detail to a single-line string without surrounding spaces or control characters", "editor: checkpoint manifest: %w", err)
	}
	if len(m.Files) > maxCheckpointFilesPerTurn {
		return loaderr.Errorf("files", fmt.Sprintf("record at most %d files in a single checkpoint manifest", maxCheckpointFilesPerTurn), "editor: checkpoint manifest: too many files (%d > %d)", len(m.Files), maxCheckpointFilesPerTurn)
	}
	seen := make(map[string]bool, len(m.Files))
	for i, f := range m.Files {
		fPath := fmt.Sprintf("files[%d]", i)
		clean, err := cleanCheckpointRelPath(f.Path)
		if err != nil || clean != f.Path {
			if err != nil {
				return loaderr.Errorf(fPath+".path", "set path to a canonical relative slash-separated project file path", "editor: checkpoint manifest: files[%d]: %w", i, err)
			}
			return loaderr.Errorf(fPath+".path", fmt.Sprintf("normalize files[%d].path to %q", i, clean), "editor: checkpoint manifest: files[%d].path %q must be canonical slash-separated relative path %q", i, f.Path, clean)
		}
		if seen[f.Path] {
			return loaderr.Errorf(fPath+".path", "remove the duplicate file entry from files", "editor: checkpoint manifest: duplicate file path %q", f.Path)
		}
		seen[f.Path] = true
		if f.Size < 0 {
			return loaderr.Errorf(fPath+".size", "set size to a non-negative byte count", "editor: checkpoint manifest: files[%d].size %d must not be negative", i, f.Size)
		}
		if f.Existed {
			if f.Size > maxCheckpointFileBytes {
				return loaderr.Errorf(fPath+".size", fmt.Sprintf("keep captured file size at or below %d bytes", maxCheckpointFileBytes), "editor: checkpoint manifest: files[%d].size %d exceeds maximum %d", i, f.Size, maxCheckpointFileBytes)
			}
			if !validSHA256Hex(f.SHA256) {
				return loaderr.Errorf(fPath+".sha256", "set sha256 to 64 lowercase hexadecimal characters", "editor: checkpoint manifest: files[%d].sha256 %q must be 64 lowercase hex characters", i, f.SHA256)
			}
			if f.Mode != "" {
				if _, err := parseOctalFileMode(f.Mode); err != nil {
					return loaderr.Errorf(fPath+".mode", "set mode to a 4-digit octal permission string such as 0755 or omit it", "editor: checkpoint manifest: files[%d].mode: %w", i, err)
				}
			}
		} else {
			if f.Size != 0 || f.SHA256 != "" || f.Mode != "" {
				return loaderr.Errorf(fPath, "omit size, sha256, and mode when existed is false", "editor: checkpoint manifest: files[%d]: non-existent file must not set size, sha256, or mode", i)
			}
		}
	}
	return nil
}

// EncodeCheckpointManifest validates m and encodes it as deterministic 2-space YAML with files sorted by path.
func EncodeCheckpointManifest(m *CheckpointManifest) ([]byte, error) {
	if err := ValidateCheckpointManifest(m); err != nil {
		return nil, err
	}
	cp := *m
	if len(m.Files) > 0 {
		cp.Files = slices.Clone(m.Files)
		slices.SortFunc(cp.Files, func(a, b CheckpointFile) int {
			return strings.Compare(a.Path, b.Path)
		})
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&cp); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("editor: checkpoint manifest: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: checkpoint manifest: encode: %w", err)
	}
	return buf.Bytes(), nil
}

func validateCheckpointID(id string) error {
	if id == "" {
		return errors.New("id is required")
	}
	if len(id) > 128 {
		return fmt.Errorf("id %q is too long", id)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("id %q must not contain '..'", id)
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("id %q contains invalid character %q", id, r)
	}
	return nil
}

func validateCheckpointText(field, s string, required bool) error {
	if s == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if strings.TrimSpace(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s %q must have no surrounding spaces or control characters", field, s)
	}
	return nil
}

func validSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func parseOctalFileMode(s string) (os.FileMode, error) {
	if len(s) != 4 || s[0] != '0' {
		return 0, fmt.Errorf("invalid octal mode %q (want 4 digits like 0644)", s)
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || v > 0o777 {
		return 0, fmt.Errorf("invalid octal mode %q", s)
	}
	return os.FileMode(v), nil
}

func formatOctalFileMode(mode os.FileMode) string {
	perm := mode.Perm()
	if perm == 0 || perm == 0o644 {
		return ""
	}
	return fmt.Sprintf("%04o", perm)
}

// cleanCheckpointRelPath validates that rel names a regular project file inside the project root,
// outside .nsgo/ and .git/, and outside all You-only configuration files (permissions, tools, models, usage).
func cleanCheckpointRelPath(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("path is empty")
	}
	if strings.IndexFunc(rel, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("path %q contains control characters", rel)
	}
	if strings.ContainsRune(rel, '\\') {
		return "", fmt.Errorf("path %q must use forward slashes", rel)
	}
	if strings.ContainsRune(rel, ':') {
		return "", fmt.Errorf("path %q contains invalid ':'", rel)
	}
	if path.IsAbs(rel) || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("path %q must be relative to the project root", rel)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || !fs.ValidPath(clean) {
		return "", fmt.Errorf("path %q leaves the project root", rel)
	}
	lower := strings.ToLower(clean)
	if lower == ".nsgo" || strings.HasPrefix(lower, ".nsgo/") || lower == ".git" || strings.HasPrefix(lower, ".git/") {
		return "", fmt.Errorf("path %q is inside reserved metadata directory", rel)
	}
	if dom, youOnly := MatchYouOnlyPath(clean); youOnly {
		return "", fmt.Errorf("path %q is protected by the You-only rule (%s)", clean, dom.DisplayName())
	}
	return clean, nil
}

func (e *Editor) checkCheckpointYouOnly(cleanRel string) error {
	if dom, ok := MatchYouOnlyPath(cleanRel); ok {
		return fmt.Errorf("editor: checkpoint: %s is protected by the You-only rule (%s)", cleanRel, dom.DisplayName())
	}
	if e != nil {
		if dom, ok := e.CheckYouOnlyTarget(cleanRel); ok {
			return fmt.Errorf("editor: checkpoint: %s is protected by the You-only rule (%s)", cleanRel, dom.DisplayName())
		}
	}
	return nil
}

// resolvePathNoSymlinks resolves cleanRel inside rootDir and refuses ANY symlink along the path
// (including dangling symlinks and internal symlinks pointing to reserved or You-only files).
func resolvePathNoSymlinks(rootDir, rel string) (string, error) {
	clean, err := cleanCheckpointRelPath(rel)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return "", err
	}
	if realRoot, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = realRoot
	}
	parts := strings.Split(clean, "/")
	cur := rootAbs
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, lerr := os.Lstat(cur)
		if lerr != nil {
			if errors.Is(lerr, fs.ErrNotExist) {
				// Remaining path segments do not exist yet.
				break
			}
			return "", fmt.Errorf("editor: checkpoint: stat %s: %w", clean, lerr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("editor: checkpoint: %s traverses or targets a symlink", clean)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("editor: checkpoint: %s parent %q is not a directory", clean, part)
		}
	}
	abs := filepath.Join(rootAbs, filepath.FromSlash(clean))
	if !isSubpath(rootAbs, abs) {
		return "", fmt.Errorf("editor: checkpoint: %s escapes root", clean)
	}
	return abs, nil
}

// readFileBoundedNoSymlink reads a regular non-symlink file capped at maxBytes.
func readFileBoundedNoSymlink(abs string, maxBytes int64) ([]byte, os.FileInfo, error) {
	linfo, err := os.Lstat(abs)
	if err != nil {
		return nil, nil, err
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("editor: checkpoint: refusing symlink %s", filepath.Base(abs))
	}
	if !linfo.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("editor: checkpoint: %s is not a regular file", filepath.Base(abs))
	}
	if maxBytes > 0 && linfo.Size() > maxBytes {
		return nil, nil, fmt.Errorf("editor: checkpoint: file %s exceeds maximum size (%d bytes)", filepath.Base(abs), maxBytes)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("editor: checkpoint: %s is not a regular file", filepath.Base(abs))
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, nil, fmt.Errorf("editor: checkpoint: file %s exceeds maximum size (%d bytes)", filepath.Base(abs), maxBytes)
	}
	var data []byte
	if maxBytes > 0 {
		data, err = io.ReadAll(io.LimitReader(f, maxBytes+1))
		if err != nil {
			return nil, nil, err
		}
		if int64(len(data)) > maxBytes {
			return nil, nil, fmt.Errorf("editor: checkpoint: file %s exceeds maximum size (%d bytes)", filepath.Base(abs), maxBytes)
		}
	} else {
		data, err = io.ReadAll(f)
		if err != nil {
			return nil, nil, err
		}
	}
	return data, info, nil
}

// checkNotSymlinkDir ensures path p, if it exists, is a real directory and not a symlink.
func checkNotSymlinkDir(p string) error {
	info, err := os.Lstat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("editor: checkpoint: refusing symlink directory %s", filepath.Base(p))
	}
	if !info.IsDir() {
		return fmt.Errorf("editor: checkpoint: %s is not a directory", filepath.Base(p))
	}
	return nil
}

// projectDirPath returns the open project's root directory, or "" without a project.
func (e *Editor) projectDirPath() string {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ""
	}
	return filepath.Dir(e.ProjectPath)
}

// CheckpointsDir returns the OS path to the open project's .nsgo/checkpoints directory, or "" without a project.
func (e *Editor) CheckpointsDir() string {
	root := e.projectDirPath()
	if root == "" {
		return ""
	}
	return filepath.Join(root, filepath.FromSlash(CheckpointsDir))
}

func (e *Editor) effectiveCheckpointMaxBytes() int64 {
	if e != nil && e.CheckpointMaxBytes > 0 {
		return e.CheckpointMaxBytes
	}
	return DefaultCheckpointMaxBytes
}

func ensureCheckpointsGitIgnore(projectDir string) error {
	nsgoDir := filepath.Join(projectDir, ".nsgo")
	if err := checkNotSymlinkDir(nsgoDir); err != nil {
		return err
	}
	cpDir := filepath.Join(projectDir, filepath.FromSlash(CheckpointsDir))
	if err := checkNotSymlinkDir(cpDir); err != nil {
		return err
	}
	if err := os.MkdirAll(cpDir, 0o755); err != nil {
		return err
	}
	cpIgnore := filepath.Join(cpDir, ".gitignore")
	if info, err := os.Lstat(cpIgnore); errors.Is(err, fs.ErrNotExist) {
		if err := writeFileAtomic(cpIgnore, []byte("*\n")); err != nil {
			return err
		}
	} else if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("editor: checkpoint: refusing symlink .nsgo/checkpoints/.gitignore")
	}
	nsgoIgnore := filepath.Join(nsgoDir, ".gitignore")
	if info, err := os.Lstat(nsgoIgnore); errors.Is(err, fs.ErrNotExist) {
		if err := writeFileAtomic(nsgoIgnore, []byte("checkpoints/\n")); err != nil {
			return err
		}
	} else if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("editor: checkpoint: refusing symlink .nsgo/.gitignore")
	}
	return nil
}

func (e *Editor) nextCheckpointID(cpRoot string, now time.Time) string {
	stamp := now.UTC().Format("20060102T150405Z")
	for {
		e.checkpoints.seq++
		id := fmt.Sprintf("%s-%04d", stamp, e.checkpoints.seq)
		if _, err := os.Lstat(filepath.Join(cpRoot, id)); errors.Is(err, fs.ErrNotExist) {
			return id
		}
	}
}

// ActiveTurnID returns the checkpoint ID of the currently active agent turn, or "" when no turn is active.
func (e *Editor) ActiveTurnID() string {
	if e == nil || e.checkpoints.active == nil {
		return ""
	}
	return e.checkpoints.active.manifest.ID
}

// ActiveTurnFileCount reports how many files have been recorded into the currently active checkpoint turn.
func (e *Editor) ActiveTurnFileCount() int {
	if e == nil || e.checkpoints.active == nil {
		return 0
	}
	return len(e.checkpoints.active.manifest.Files)
}

// BeginCheckpointTurn starts an agent turn checkpoint over the open project. Before the turn first writes
// any file (scene, prefab, data, game.yaml, Go code, or binary media), its original bytes are stored
// under .nsgo/checkpoints/<id>/. Call EndCheckpointTurn when the turn finishes.
func (e *Editor) BeginCheckpointTurn(title string) (string, error) {
	return e.BeginCheckpointTurnWith(CheckpointOptions{
		Who:   HistoryAuthorAgent,
		Kind:  CheckpointKindTurn,
		Title: title,
	})
}

// BeginCheckpointTurnWith starts a persistent checkpoint turn configured by opts.
func (e *Editor) BeginCheckpointTurnWith(opts CheckpointOptions) (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	projectDir := e.projectDirPath()
	if projectDir == "" {
		return "", ErrNoProject
	}
	if e.checkpoints.active != nil {
		return "", fmt.Errorf("editor: checkpoint turn %q is already active", e.checkpoints.active.manifest.ID)
	}
	who := opts.Who
	if who == "" {
		who = HistoryAuthorAgent
	}
	kind := opts.Kind
	if kind == "" {
		kind = CheckpointKindTurn
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		switch kind {
		case CheckpointKindTask:
			title = "Background task · checkpoint before start"
		case CheckpointKindAutomation:
			title = "Automation · checkpoint before start"
		default:
			title = "Agent turn"
		}
	}
	detail := strings.TrimSpace(opts.Detail)
	now := e.HistoryNow().UTC()
	cpRoot := filepath.Join(projectDir, filepath.FromSlash(CheckpointsDir))
	id := e.nextCheckpointID(cpRoot, now)
	turn := &activeCheckpointTurn{
		manifest: CheckpointManifest{
			Version:   CheckpointVersion,
			ID:        id,
			CreatedAt: now.Format(time.RFC3339),
			Who:       who,
			Kind:      kind,
			Title:     title,
			Detail:    detail,
		},
		dir:          filepath.Join(cpRoot, id),
		recorded:     make(map[string]bool),
		keepEmpty:    kind == CheckpointKindTask || kind == CheckpointKindAutomation,
		countedAgent: who == HistoryAuthorAgent,
		projectDir:   projectDir,
	}
	e.checkpoints.active = turn
	if turn.countedAgent {
		e.EnterAgentContext()
	}

	if len(opts.PreCaptureFiles) > 0 {
		for _, f := range opts.PreCaptureFiles {
			if err := e.RecordCheckpointFile(f); err != nil {
				e.abortActiveTurn()
				return "", err
			}
		}
	}
	if turn.keepEmpty && !turn.persisted {
		if err := e.persistActiveTurnManifest(); err != nil {
			e.abortActiveTurn()
			return "", err
		}
	}
	return id, nil
}

func (e *Editor) abortActiveTurn() {
	if e == nil || e.checkpoints.active == nil {
		return
	}
	turn := e.checkpoints.active
	e.checkpoints.active = nil
	if turn.countedAgent {
		e.LeaveAgentContext()
	}
	if turn.persisted && turn.dir != "" {
		_ = os.RemoveAll(turn.dir)
	}
}

// AbortCheckpointTurn cancels the currently active checkpoint turn and removes its incomplete checkpoint directory.
func (e *Editor) AbortCheckpointTurn() {
	if e == nil || e.checkpoints.active == nil {
		return
	}
	e.abortActiveTurn()
	_, _ = e.LoadCheckpoints()
	e.notify()
}

// FinishCheckpointTurnOnError finalizes the active checkpoint turn after a failure or panic:
// if no files were captured (and the turn is not a pre-start task/automation checkpoint), it discards
// the empty checkpoint; otherwise it persists the checkpoint with detail so partial edits stay restorable.
func (e *Editor) FinishCheckpointTurnOnError(detail string) (*CheckpointSummary, error) {
	if e == nil || e.checkpoints.active == nil {
		return nil, nil
	}
	if len(e.checkpoints.active.manifest.Files) == 0 && !e.checkpoints.active.keepEmpty {
		e.AbortCheckpointTurn()
		return nil, nil
	}
	if strings.TrimSpace(detail) == "" {
		detail = "interrupted"
	}
	return e.EndCheckpointTurn(detail)
}

// RecordCheckpointFile snapshots the pre-turn bytes of relPath into the active checkpoint before
// the turn first modifies relPath. Repeated calls for the same path in the same turn are no-ops,
// preserving the original pre-turn bytes. Binary files are stored verbatim.
// Refuses paths outside the project root, symlinks, .nsgo/, .git/, and You-only files.
func (e *Editor) RecordCheckpointFile(relPath string) error {
	if e == nil || e.checkpoints.active == nil {
		return nil
	}
	turn := e.checkpoints.active
	clean, err := cleanCheckpointRelPath(strings.ReplaceAll(relPath, `\`, "/"))
	if err != nil {
		return fmt.Errorf("editor: checkpoint: %w", err)
	}
	if err := e.checkCheckpointYouOnly(clean); err != nil {
		return err
	}
	if turn.recorded[clean] {
		return nil
	}
	if len(turn.manifest.Files) >= maxCheckpointFilesPerTurn {
		return fmt.Errorf("editor: checkpoint: reached maximum %d files per turn", maxCheckpointFilesPerTurn)
	}
	abs, err := resolvePathNoSymlinks(turn.projectDir, clean)
	if err != nil {
		return err
	}

	// Capture the exact pre-turn bytes. If the file is an open dirty scene tab or dirty game.yaml,
	// capture the editor's current bytes; otherwise read the raw bytes from disk so comments,
	// hand-formatting, and binary media are preserved byte-for-byte.
	var (
		data    []byte
		existed bool
		mode    os.FileMode = 0o644
	)
	if dirtyBytes, ok := e.dirtyOpenFileBytes(clean); ok {
		if int64(len(dirtyBytes)) > maxCheckpointFileBytes {
			return fmt.Errorf("editor: checkpoint: %s exceeds maximum file size (%d bytes)", clean, maxCheckpointFileBytes)
		}
		data = dirtyBytes
		existed = true
		if info, statErr := os.Lstat(abs); statErr == nil && info.Mode().IsRegular() {
			mode = info.Mode().Perm()
		}
	} else {
		raw, info, readErr := readFileBoundedNoSymlink(abs, maxCheckpointFileBytes)
		switch {
		case readErr == nil:
			data = raw
			existed = true
			mode = info.Mode().Perm()
		case errors.Is(readErr, fs.ErrNotExist):
			existed = false
		default:
			return fmt.Errorf("editor: checkpoint: read %s: %w", clean, readErr)
		}
	}

	if err := ensureCheckpointsGitIgnore(turn.projectDir); err != nil {
		return err
	}
	if err := checkNotSymlinkDir(turn.dir); err != nil {
		return err
	}
	if err := os.MkdirAll(turn.dir, 0o755); err != nil {
		return err
	}

	entry := CheckpointFile{
		Path:    clean,
		Existed: existed,
	}
	if existed {
		sum := sha256.Sum256(data)
		entry.Size = int64(len(data))
		entry.SHA256 = hex.EncodeToString(sum[:])
		entry.Mode = formatOctalFileMode(mode)

		filesRoot := filepath.Join(turn.dir, "files")
		if err := checkNotSymlinkDir(filesRoot); err != nil {
			return err
		}
		if err := os.MkdirAll(filesRoot, 0o755); err != nil {
			return err
		}
		dst, derr := resolvePathNoSymlinks(filesRoot, clean)
		if derr != nil {
			return derr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(dst, data); err != nil {
			return err
		}
	}

	turn.manifest.Files = append(turn.manifest.Files, entry)
	slices.SortFunc(turn.manifest.Files, func(a, b CheckpointFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	turn.recorded[clean] = true
	return e.persistActiveTurnManifest()
}

func (e *Editor) persistActiveTurnManifest() error {
	turn := e.checkpoints.active
	if turn == nil {
		return nil
	}
	if err := ensureCheckpointsGitIgnore(turn.projectDir); err != nil {
		return err
	}
	if err := checkNotSymlinkDir(turn.dir); err != nil {
		return err
	}
	if err := os.MkdirAll(turn.dir, 0o755); err != nil {
		return err
	}
	data, err := EncodeCheckpointManifest(&turn.manifest)
	if err != nil {
		return err
	}
	mPath := filepath.Join(turn.dir, CheckpointManifestFile)
	if info, lerr := os.Lstat(mPath); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("editor: checkpoint: refusing symlink manifest.yaml")
	}
	if err := writeFileAtomic(mPath, data); err != nil {
		return err
	}
	turn.persisted = true
	return nil
}

// dirtyOpenFileBytes returns the in-memory bytes for relPath if it corresponds to an open dirty
// editor document (scene tab or game.yaml). Clean documents return false so callers read verbatim disk bytes.
func (e *Editor) dirtyOpenFileBytes(relPath string) ([]byte, bool) {
	if e == nil {
		return nil, false
	}
	if relPath == project.FileName && e.ProjectDirty() {
		if data, err := project.Encode(e.Project); err == nil {
			return data, true
		}
	}
	e.syncActiveTab()
	for i, t := range e.tabs {
		if t.scenePath == relPath && e.tabDirtyAt(i) && t.doc != nil {
			if data, err := sceneformat.Encode(t.doc); err == nil {
				return data, true
			}
		}
	}
	return nil, false
}

// WriteAgentFile records relPath's pre-turn bytes into the active checkpoint (if any) before
// writing data atomically to relPath inside the open project. Open scene tabs or game.yaml
// matching relPath are reloaded cleanly to stay synchronized with disk.
// Refuses You-only files, symlinks, paths outside the project root, and edits while playing or previewing history.
func (e *Editor) WriteAgentFile(relPath string, data []byte) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	if e.StartScreenOpen() || e.MergeConflictOpen() {
		return errors.New("editor: file edits are unavailable while a full-window screen is open")
	}
	projectDir := e.projectDirPath()
	if projectDir == "" {
		return ErrNoProject
	}
	if int64(len(data)) > maxCheckpointFileBytes {
		return fmt.Errorf("editor: file %q exceeds maximum write size (%d bytes)", relPath, maxCheckpointFileBytes)
	}
	clean, err := cleanCheckpointRelPath(strings.ReplaceAll(relPath, `\`, "/"))
	if err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	if err := e.checkCheckpointYouOnly(clean); err != nil {
		return err
	}
	abs, err := resolvePathNoSymlinks(projectDir, clean)
	if err != nil {
		return err
	}
	if e.checkpoints.active != nil {
		if err := e.RecordCheckpointFile(clean); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(abs, data); err != nil {
		return err
	}
	e.syncOpenDocumentsFromRestoredFile(clean, abs, data, true)
	e.filesRev++
	e.assetsRev++
	e.snapshotProjectTree()
	e.invalidateProjectIndex()
	e.RefreshChangesCount()
	e.notify()
	return nil
}

// DeleteAgentFile records relPath's pre-turn bytes into the active checkpoint (if any) before
// removing relPath from the open project.
// Refuses You-only files, symlinks, paths outside the project root, and edits while playing or previewing history.
func (e *Editor) DeleteAgentFile(relPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	if e.StartScreenOpen() || e.MergeConflictOpen() {
		return errors.New("editor: file deletions are unavailable while a full-window screen is open")
	}
	projectDir := e.projectDirPath()
	if projectDir == "" {
		return ErrNoProject
	}
	clean, err := cleanCheckpointRelPath(strings.ReplaceAll(relPath, `\`, "/"))
	if err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	if err := e.checkCheckpointYouOnly(clean); err != nil {
		return err
	}
	abs, err := resolvePathNoSymlinks(projectDir, clean)
	if err != nil {
		return err
	}
	if e.checkpoints.active != nil {
		if err := e.RecordCheckpointFile(clean); err != nil {
			return err
		}
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	e.syncOpenDocumentsFromRestoredFile(clean, abs, nil, false)
	e.filesRev++
	e.assetsRev++
	e.snapshotProjectTree()
	e.invalidateProjectIndex()
	e.RefreshChangesCount()
	e.notify()
	return nil
}

// EndCheckpointTurn finalizes the currently active checkpoint turn, persists its manifest (or cleans up
// an empty regular turn that touched no files), enforces the checkpoint size cap, and refreshes History.
func (e *Editor) EndCheckpointTurn(detail string) (*CheckpointSummary, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	turn := e.checkpoints.active
	if turn == nil {
		return nil, nil
	}
	if d := strings.TrimSpace(detail); d != "" {
		turn.manifest.Detail = d
	}
	if len(turn.manifest.Files) == 0 && !turn.keepEmpty {
		e.abortActiveTurn()
		return nil, nil
	}
	if err := e.persistActiveTurnManifest(); err != nil {
		return nil, err
	}
	finishedID := turn.manifest.ID
	e.checkpoints.active = nil
	if turn.countedAgent {
		e.LeaveAgentContext()
	}

	if _, err := e.PruneCheckpoints(); err != nil {
		return nil, err
	}
	list, err := e.LoadCheckpoints()
	if err != nil {
		return nil, err
	}
	e.notify()
	for i := range list {
		if list[i].ID == finishedID {
			cp := list[i]
			return &cp, nil
		}
	}
	return nil, nil
}

// RunCheckpointTurn runs fn inside BeginCheckpointTurn / EndCheckpointTurn, recording every file fn touches
// into one persistent checkpoint. If fn returns an error or panics and no files were modified,
// the empty checkpoint is discarded; if files were already modified, the checkpoint is kept so
// the partial turn remains restorable.
func (e *Editor) RunCheckpointTurn(title string, fn func() error) (*CheckpointSummary, error) {
	if _, err := e.BeginCheckpointTurn(title); err != nil {
		return nil, err
	}
	completed := false
	defer func() {
		if !completed {
			_, _ = e.FinishCheckpointTurnOnError("interrupted")
		}
	}()
	runErr := fn()
	completed = true
	if runErr != nil {
		summary, _ := e.FinishCheckpointTurnOnError("interrupted")
		return summary, runErr
	}
	return e.EndCheckpointTurn("")
}

// defaultPreStartCaptureFiles returns the project-relative paths of open scenes and game.yaml
// to snapshot before an autonomous task or automation begins when no explicit file list is given.
func (e *Editor) defaultPreStartCaptureFiles() []string {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil
	}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		if clean, err := cleanCheckpointRelPath(p); err == nil && e.checkCheckpointYouOnly(clean) == nil && !slices.Contains(out, clean) {
			out = append(out, clean)
		}
	}
	e.syncActiveTab()
	for _, t := range e.tabs {
		add(t.scenePath)
	}
	add(project.FileName)
	slices.Sort(out)
	return out
}

// TakeCheckpointBeforeTask takes a persistent checkpoint under .nsgo/checkpoints/ immediately
// before an autonomous background task starts. When files is empty, the open scene(s) and game.yaml
// are captured. Any active turn must be ended first.
func (e *Editor) TakeCheckpointBeforeTask(title string, files ...string) (*CheckpointSummary, error) {
	return e.takePreStartCheckpoint(CheckpointKindTask, title, files...)
}

// TakeCheckpointBeforeAutomation takes a persistent checkpoint under .nsgo/checkpoints/ immediately
// before an automation runs. When files is empty, the open scene(s) and game.yaml are captured.
func (e *Editor) TakeCheckpointBeforeAutomation(title string, files ...string) (*CheckpointSummary, error) {
	return e.takePreStartCheckpoint(CheckpointKindAutomation, title, files...)
}

func (e *Editor) takePreStartCheckpoint(kind CheckpointKind, title string, files ...string) (*CheckpointSummary, error) {
	if len(files) == 0 {
		files = e.defaultPreStartCaptureFiles()
	}
	cleanTitle := formatPreStartCheckpointTitle(kind, title)
	if _, err := e.BeginCheckpointTurnWith(CheckpointOptions{
		Who:             HistoryAuthorAgent,
		Kind:            kind,
		Title:           cleanTitle,
		PreCaptureFiles: files,
	}); err != nil {
		return nil, err
	}
	return e.EndCheckpointTurn("")
}

// RunTaskWithCheckpoint takes a pre-start checkpoint before running autonomous task fn, keeps it
// active so every additional file fn touches is captured before its first write, and finalizes it.
func (e *Editor) RunTaskWithCheckpoint(title string, files []string, fn func() error) (*CheckpointSummary, error) {
	return e.runPreStartCheckpoint(CheckpointKindTask, title, files, fn)
}

// RunAutomationWithCheckpoint takes a pre-start checkpoint before running automation fn, keeps it
// active so every additional file fn touches is captured before its first write, and finalizes it.
func (e *Editor) RunAutomationWithCheckpoint(title string, files []string, fn func() error) (*CheckpointSummary, error) {
	return e.runPreStartCheckpoint(CheckpointKindAutomation, title, files, fn)
}

func formatPreStartCheckpointTitle(kind CheckpointKind, title string) string {
	cleanTitle := strings.TrimSpace(title)
	if cleanTitle == "" {
		if kind == CheckpointKindAutomation {
			return "Automation · checkpoint before start"
		}
		return "Task · checkpoint before start"
	}
	if !strings.Contains(strings.ToLower(cleanTitle), "checkpoint") {
		cleanTitle += " · checkpoint before start"
	}
	return cleanTitle
}

func (e *Editor) runPreStartCheckpoint(kind CheckpointKind, title string, files []string, fn func() error) (*CheckpointSummary, error) {
	if len(files) == 0 {
		files = e.defaultPreStartCaptureFiles()
	}
	if _, err := e.BeginCheckpointTurnWith(CheckpointOptions{
		Who:             HistoryAuthorAgent,
		Kind:            kind,
		Title:           formatPreStartCheckpointTitle(kind, title),
		PreCaptureFiles: files,
	}); err != nil {
		return nil, err
	}
	completed := false
	defer func() {
		if !completed {
			_, _ = e.FinishCheckpointTurnOnError("interrupted")
		}
	}()
	runErr := fn()
	completed = true
	detail := ""
	if runErr != nil {
		detail = "failed"
	}
	summary, endErr := e.EndCheckpointTurn(detail)
	if runErr != nil {
		return summary, runErr
	}
	return summary, endErr
}

// LoadCheckpoints scans .nsgo/checkpoints/ in the open project, parses each manifest, sorts newest
// first, and updates the editor's cached checkpoint list. Invalid, symlinked, or non-checkpoint directories are skipped.
func (e *Editor) LoadCheckpoints() ([]CheckpointSummary, error) {
	if e == nil {
		return nil, nil
	}
	projectDir := e.projectDirPath()
	if projectDir == "" {
		e.checkpoints.list = nil
		return nil, nil
	}
	nsgoDir := filepath.Join(projectDir, ".nsgo")
	if err := checkNotSymlinkDir(nsgoDir); err != nil {
		e.checkpoints.list = nil
		return nil, err
	}
	cpRoot := filepath.Join(projectDir, filepath.FromSlash(CheckpointsDir))
	if err := checkNotSymlinkDir(cpRoot); err != nil {
		e.checkpoints.list = nil
		return nil, err
	}
	entries, err := os.ReadDir(cpRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			e.checkpoints.list = nil
			return nil, nil
		}
		return nil, err
	}
	var out []CheckpointSummary
	scanned := 0
	for _, de := range entries {
		if scanned >= maxCheckpointDirs {
			break
		}
		if de.Type()&fs.ModeSymlink != 0 || !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		if validateCheckpointID(de.Name()) != nil {
			continue
		}
		scanned++
		dir := filepath.Join(cpRoot, de.Name())
		if checkNotSymlinkDir(dir) != nil {
			continue
		}
		mPath := filepath.Join(dir, CheckpointManifestFile)
		data, _, readErr := readFileBoundedNoSymlink(mPath, maxCheckpointManifestBytes)
		if readErr != nil {
			continue
		}
		m, parseErr := ParseCheckpointManifest(data)
		if parseErr != nil || m.ID != de.Name() {
			continue
		}
		total := dirSizeBytes(dir)
		out = append(out, CheckpointSummary{
			CheckpointManifest: *m,
			TotalBytes:         total,
		})
	}
	sortCheckpointsNewestFirst(out)
	e.checkpoints.list = out
	return slices.Clone(out), nil
}

func sortCheckpointsNewestFirst(list []CheckpointSummary) {
	slices.SortFunc(list, func(a, b CheckpointSummary) int {
		ta, errA := time.Parse(time.RFC3339, a.CreatedAt)
		tb, errB := time.Parse(time.RFC3339, b.CreatedAt)
		if errA == nil && errB == nil && !ta.Equal(tb) {
			if ta.After(tb) {
				return -1
			}
			return 1
		}
		return strings.Compare(b.ID, a.ID)
	})
}

func dirSizeBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// Checkpoints returns the open project's persisted checkpoints, newest first.
func (e *Editor) Checkpoints() []CheckpointSummary {
	if e == nil || len(e.checkpoints.list) == 0 {
		return nil
	}
	out := make([]CheckpointSummary, len(e.checkpoints.list))
	for i, c := range e.checkpoints.list {
		c.Files = slices.Clone(c.Files)
		out[i] = c
	}
	return out
}

// CheckpointManifest loads and validates the manifest of checkpoint id from .nsgo/checkpoints/<id>/manifest.yaml.
func (e *Editor) CheckpointManifest(id string) (*CheckpointManifest, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	if err := validateCheckpointID(id); err != nil {
		return nil, fmt.Errorf("editor: checkpoint: %w", err)
	}
	projectDir := e.projectDirPath()
	if projectDir == "" {
		return nil, ErrNoProject
	}
	if err := checkNotSymlinkDir(filepath.Join(projectDir, ".nsgo")); err != nil {
		return nil, err
	}
	cpRoot := filepath.Join(projectDir, filepath.FromSlash(CheckpointsDir))
	if err := checkNotSymlinkDir(cpRoot); err != nil {
		return nil, err
	}
	cpDir := filepath.Join(cpRoot, id)
	if err := checkNotSymlinkDir(cpDir); err != nil {
		return nil, err
	}
	data, _, err := readFileBoundedNoSymlink(filepath.Join(cpDir, CheckpointManifestFile), maxCheckpointManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("editor: checkpoint %q: %w", id, err)
	}
	m, err := ParseCheckpointManifest(data)
	if err != nil {
		return nil, err
	}
	if m.ID != id {
		return nil, fmt.Errorf("editor: checkpoint %q: manifest id %q does not match directory", id, m.ID)
	}
	return m, nil
}

// CheckpointFileBytes returns the saved pre-turn bytes of relPath from checkpoint id, whether it
// existed before the turn, and any error (including size or SHA-256 mismatch).
func (e *Editor) CheckpointFileBytes(id, relPath string) ([]byte, bool, error) {
	m, err := e.CheckpointManifest(id)
	if err != nil {
		return nil, false, err
	}
	clean, err := cleanCheckpointRelPath(strings.ReplaceAll(relPath, `\`, "/"))
	if err != nil {
		return nil, false, err
	}
	for _, f := range m.Files {
		if f.Path != clean {
			continue
		}
		if !f.Existed {
			return nil, false, nil
		}
		data, err := readVerifiedCheckpointFile(filepath.Join(e.CheckpointsDir(), id), f)
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}
	return nil, false, fmt.Errorf("editor: checkpoint %q does not record %q", id, clean)
}

func readVerifiedCheckpointFile(cpDir string, f CheckpointFile) ([]byte, error) {
	if !f.Existed {
		return nil, nil
	}
	filesRoot := filepath.Join(cpDir, "files")
	if err := checkNotSymlinkDir(filesRoot); err != nil {
		return nil, err
	}
	abs, err := resolvePathNoSymlinks(filesRoot, f.Path)
	if err != nil {
		return nil, err
	}
	data, _, err := readFileBoundedNoSymlink(abs, maxCheckpointFileBytes)
	if err != nil {
		return nil, fmt.Errorf("editor: checkpoint: read saved %s: %w", f.Path, err)
	}
	if int64(len(data)) != f.Size {
		return nil, fmt.Errorf("editor: checkpoint: %s size mismatch (got %d, want %d)", f.Path, len(data), f.Size)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != f.SHA256 {
		return nil, fmt.Errorf("editor: checkpoint: %s checksum mismatch (got %s, want %s)", f.Path, got, f.SHA256)
	}
	return data, nil
}

// PruneCheckpoints drops the oldest completed checkpoints under .nsgo/checkpoints/ until total
// storage is within CheckpointMaxBytes (defaulting to DefaultCheckpointMaxBytes), keeping at least
// the newest checkpoint and never deleting an active turn. Returns the number of dropped checkpoints.
func (e *Editor) PruneCheckpoints() (int, error) {
	if e == nil {
		return 0, nil
	}
	list, err := e.LoadCheckpoints()
	if err != nil {
		return 0, err
	}
	capBytes := e.effectiveCheckpointMaxBytes()
	if capBytes <= 0 || len(list) <= 1 {
		return 0, nil
	}
	var total int64
	for _, c := range list {
		total += c.TotalBytes
	}
	if total <= capBytes {
		return 0, nil
	}
	activeID := e.ActiveTurnID()
	cpRoot := e.CheckpointsDir()
	dropped := 0
	// list is newest first, so iterate from oldest (end) toward newest, leaving at least index 0.
	for i := len(list) - 1; i >= 1 && total > capBytes; i-- {
		c := list[i]
		if c.ID == activeID {
			continue
		}
		if err := os.RemoveAll(filepath.Join(cpRoot, c.ID)); err != nil {
			return dropped, err
		}
		total -= c.TotalBytes
		dropped++
	}
	if dropped > 0 {
		_, _ = e.LoadCheckpoints()
	}
	return dropped, nil
}

// fileRestoreState captures a project file's exact on-disk state plus any open-editor in-memory state.
type fileRestoreState struct {
	projectDir string
	relPath    string
	absPath    string
	existed    bool
	bytes      []byte
	mode       os.FileMode
	sceneDoc   *sceneformat.SceneDoc
	sceneSaved []byte
	hadScene   bool
	projDoc    *project.File
	projSaved  []byte
	hadProj    bool
}

// checkpointRestoreCmd restores every file recorded in a checkpoint back to its pre-turn bytes
// as one undoable step; Undo puts back every file (and open editor state) as it was before Restore.
type checkpointRestoreCmd struct {
	ed      *Editor
	label   string
	summary string
	detail  string
	files   []string
	cpID    string
	before  []fileRestoreState
	after   []fileRestoreState
	applied bool
}

func (c *checkpointRestoreCmd) Label() string { return c.label }
func (c *checkpointRestoreCmd) Do()           { _ = c.do() }
func (c *checkpointRestoreCmd) Undo()         { _ = c.undo() }

func (c *checkpointRestoreCmd) historyMeta() HistoryMeta {
	return HistoryMeta{
		Author:  HistoryAuthorYou,
		Summary: c.summary,
		Detail:  c.detail,
		Files:   slices.Clone(c.files),
	}
}

func (c *checkpointRestoreCmd) do() error {
	if err := applyFileRestoreStates(c.ed, c.after, c.before); err != nil {
		return err
	}
	c.applied = true
	return nil
}

func (c *checkpointRestoreCmd) undo() error {
	if err := applyFileRestoreStates(c.ed, c.before, c.after); err != nil {
		return err
	}
	c.applied = false
	return nil
}

func applyFileRestoreStates(ed *Editor, target, fallback []fileRestoreState) error {
	// Pre-validate every target path against symlinks and You-only boundaries before writing anything.
	for _, st := range target {
		if err := ed.checkCheckpointYouOnly(st.relPath); err != nil {
			return err
		}
		if _, err := resolvePathNoSymlinks(st.projectDir, st.relPath); err != nil {
			return err
		}
	}
	written := 0
	for i, st := range target {
		if err := writeOneRestoreState(st); err != nil {
			// Roll back previously written files to fallback state.
			for j := written - 1; j >= 0; j-- {
				_ = writeOneRestoreState(fallback[j])
			}
			return err
		}
		written = i + 1
	}
	for _, st := range target {
		if st.hadScene {
			ed.applyRestoredSceneTabState(st)
		} else if st.hadProj {
			ed.applyRestoredProjectState(st)
		} else {
			ed.syncOpenDocumentsFromRestoredFile(st.relPath, st.absPath, st.bytes, st.existed)
		}
	}
	ed.filesRev++
	ed.assetsRev++
	ed.snapshotProjectTree()
	ed.invalidateProjectIndex()
	ed.rebuildDependentTabs(ed.activeTabID())
	ed.RefreshChangesCount()
	ed.CheckProblems()
	return nil
}

func writeOneRestoreState(st fileRestoreState) error {
	abs, err := resolvePathNoSymlinks(st.projectDir, st.relPath)
	if err != nil {
		return fmt.Errorf("editor: restore %s: %w", st.relPath, err)
	}
	if !st.existed {
		if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("editor: restore %s: %w", st.relPath, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("editor: restore %s: %w", st.relPath, err)
	}
	if err := writeFileAtomic(abs, st.bytes); err != nil {
		return fmt.Errorf("editor: restore %s: %w", st.relPath, err)
	}
	if st.mode != 0 && st.mode != 0o644 {
		_ = os.Chmod(abs, st.mode)
	}
	return nil
}

func (e *Editor) applyRestoredSceneTabState(st fileRestoreState) {
	if e == nil || st.sceneDoc == nil {
		return
	}
	e.syncActiveTab()
	for i, t := range e.tabs {
		if t.scenePath != st.relPath && canonicalTabFilePath(t.filePath) != canonicalTabFilePath(st.absPath) {
			continue
		}
		cloned := st.sceneDoc.Clone()
		saved := append([]byte(nil), st.sceneSaved...)
		var diskSnap diskSnapshot
		if st.existed {
			diskSnap = makeDiskSnapshot(st.absPath, st.bytes)
		}
		t.doc = cloned
		t.sceneSaved = saved
		t.sceneDisk = diskSnap
		if i == e.activeTab {
			e.Doc = cloned
			e.sceneSaved = saved
			if st.existed {
				e.recordSceneDiskSnapshot(st.absPath, st.bytes)
			} else {
				e.clearSceneDiskSnapshot()
			}
			e.pruneSelection()
			e.keepValidActiveLayer()
			e.s3d.stale = true
			e.Rebuild()
			e.syncActiveTab()
		} else {
			if built, err := e.buildSceneDoc(cloned); err == nil {
				built.DrawAllTiles = true
				t.built = built
				t.buildErr = nil
			}
		}
	}
}

func (e *Editor) applyRestoredProjectState(st fileRestoreState) {
	if e == nil || st.projDoc == nil {
		return
	}
	e.Project = st.projDoc.Clone()
	e.projectSaved = append([]byte(nil), st.projSaved...)
	e.syncAudioMixer()
	if st.existed {
		e.recordProjectDiskSnapshot(st.absPath, st.bytes)
	} else {
		e.clearProjectDiskSnapshot()
	}
}

func (e *Editor) syncOpenDocumentsFromRestoredFile(relPath, absPath string, data []byte, existed bool) {
	if e == nil {
		return
	}
	if !existed {
		if relPath == project.FileName {
			e.clearProjectDiskSnapshot()
			return
		}
		e.syncActiveTab()
		for i, t := range e.tabs {
			if t.scenePath != relPath && canonicalTabFilePath(t.filePath) != canonicalTabFilePath(absPath) {
				continue
			}
			t.sceneDisk = diskSnapshot{}
			if i == e.activeTab {
				e.clearSceneDiskSnapshot()
			}
		}
		return
	}
	if relPath == project.FileName && e.Project != nil {
		if pf, err := project.Parse(data); err == nil {
			if enc, encErr := project.Encode(pf); encErr == nil {
				e.Project = pf
				e.projectSaved = enc
				e.syncAudioMixer()
				e.recordProjectDiskSnapshot(absPath, data)
			}
		}
		return
	}
	e.syncActiveTab()
	for i, t := range e.tabs {
		if t.scenePath != relPath && canonicalTabFilePath(t.filePath) != canonicalTabFilePath(absPath) {
			continue
		}
		doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
			File:          absPath,
			Components:    e.components,
			PhysicsLayers: e.effectivePhysicsLayers(),
		})
		if err != nil {
			continue
		}
		enc, err := sceneformat.Encode(doc)
		if err != nil {
			continue
		}
		t.doc = doc
		t.sceneSaved = enc
		t.sceneDisk = makeDiskSnapshot(absPath, data)
		if i == e.activeTab {
			e.Doc = doc
			e.sceneSaved = enc
			e.recordSceneDiskSnapshot(absPath, data)
			e.pruneSelection()
			e.keepValidActiveLayer()
			e.s3d.stale = true
			e.Rebuild()
			e.syncActiveTab()
		} else if built, berr := e.buildSceneDoc(doc); berr == nil {
			built.DrawAllTiles = true
			t.built = built
			t.buildErr = nil
		}
	}
}

func (e *Editor) keepValidActiveLayer() {
	if e == nil || e.Doc == nil {
		return
	}
	if e.ActiveLayer != "" {
		found := false
		for _, l := range e.Doc.Layers {
			if l.Name == e.ActiveLayer {
				found = true
				break
			}
		}
		if !found {
			if len(e.Doc.Layers) > 0 {
				e.ActiveLayer = e.Doc.Layers[0].Name
			} else {
				e.ActiveLayer = ""
			}
		}
	}
}

func (e *Editor) effectivePhysicsLayers() *sceneformat.PhysicsLayerTable {
	if e != nil && e.Project != nil {
		return e.Project.PhysicsLayerTable()
	}
	return nil
}

func (e *Editor) buildSceneDoc(doc *sceneformat.SceneDoc) (*sceneformat.BuiltScene, error) {
	return sceneformat.Build(doc, e.liveResolver())
}

func (e *Editor) captureCurrentRestoreState(projectDir, relPath string) (fileRestoreState, error) {
	if err := e.checkCheckpointYouOnly(relPath); err != nil {
		return fileRestoreState{}, err
	}
	abs, err := resolvePathNoSymlinks(projectDir, relPath)
	if err != nil {
		return fileRestoreState{}, err
	}
	st := fileRestoreState{
		projectDir: projectDir,
		relPath:    relPath,
		absPath:    abs,
		mode:       0o644,
	}
	raw, info, rerr := readFileBoundedNoSymlink(abs, maxCheckpointFileBytes)
	switch {
	case rerr == nil:
		st.existed = true
		st.bytes = raw
		st.mode = info.Mode().Perm()
	case errors.Is(rerr, fs.ErrNotExist):
		st.existed = false
	default:
		return fileRestoreState{}, fmt.Errorf("editor: read %s before restore: %w", relPath, rerr)
	}

	if relPath == project.FileName && e.Project != nil {
		st.hadProj = true
		st.projDoc = e.Project.Clone()
		st.projSaved = append([]byte(nil), e.projectSaved...)
	}
	e.syncActiveTab()
	for _, t := range e.tabs {
		if t.doc == nil {
			continue
		}
		if t.scenePath == relPath || canonicalTabFilePath(t.filePath) == canonicalTabFilePath(abs) {
			st.hadScene = true
			st.sceneDoc = t.doc.Clone()
			st.sceneSaved = append([]byte(nil), t.sceneSaved...)
			break
		}
	}
	return st, nil
}

// RestoreCheckpoint restores every file recorded in checkpoint id back to its pre-turn bytes
// (removing files that did not exist before the turn) as one undoable step in History.
// Calling Undo reverts every file and open editor tab back to the state right before Restore.
// Refuses You-only files, symlinks, and restores while playing, on full-window screens, or during an active turn.
func (e *Editor) RestoreCheckpoint(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.StartScreenOpen() || e.MergeConflictOpen() {
		return errors.New("editor: checkpoint restore is unavailable while a full-window screen is open")
	}
	if e.ActiveTurnID() != "" {
		return fmt.Errorf("editor: cannot restore checkpoint while turn %q is active", e.ActiveTurnID())
	}
	projectDir := e.projectDirPath()
	if projectDir == "" {
		return ErrNoProject
	}
	m, err := e.CheckpointManifest(id)
	if err != nil {
		return err
	}
	e.cancelTransientInteraction()
	e.ExitHistoryPreview()

	cpDir := filepath.Join(e.CheckpointsDir(), id)
	before := make([]fileRestoreState, 0, len(m.Files))
	after := make([]fileRestoreState, 0, len(m.Files))
	touched := make([]string, 0, len(m.Files))

	for _, f := range m.Files {
		if err := e.checkCheckpointYouOnly(f.Path); err != nil {
			return err
		}
		cur, err := e.captureCurrentRestoreState(projectDir, f.Path)
		if err != nil {
			return err
		}
		target := fileRestoreState{
			projectDir: projectDir,
			relPath:    f.Path,
			absPath:    cur.absPath,
			existed:    f.Existed,
			mode:       0o644,
		}
		if f.Existed {
			raw, err := readVerifiedCheckpointFile(cpDir, f)
			if err != nil {
				return err
			}
			target.bytes = raw
			if f.Mode != "" {
				if parsedMode, merr := parseOctalFileMode(f.Mode); merr == nil {
					target.mode = parsedMode
				}
			}
			if cur.hadScene {
				if doc, perr := sceneformat.ParseWith(raw, sceneformat.ParseOptions{
					File:          cur.absPath,
					Components:    e.components,
					PhysicsLayers: e.effectivePhysicsLayers(),
				}); perr == nil {
					if enc, eerr := sceneformat.Encode(doc); eerr == nil {
						target.hadScene = true
						target.sceneDoc = doc
						target.sceneSaved = enc
					}
				}
			}
			if cur.hadProj {
				if pf, perr := project.Parse(raw); perr == nil {
					if enc, eerr := project.Encode(pf); eerr == nil {
						target.hadProj = true
						target.projDoc = pf
						target.projSaved = enc
					}
				}
			}
		}
		before = append(before, cur)
		after = append(after, target)
		touched = append(touched, f.Path)
	}

	ts, _ := time.Parse(time.RFC3339, m.CreatedAt)
	ago := e.FormatHistoryRelativeTime(ts)
	summary := "Restored to " + ago
	detail := m.Title
	if detail == "" {
		detail = ago
	}

	cmd := &checkpointRestoreCmd{
		ed:      e,
		label:   "Restore checkpoint: " + m.Title,
		summary: summary,
		detail:  detail,
		files:   touched,
		cpID:    id,
		before:  before,
		after:   after,
	}
	if err := e.History.execute(cmd); err != nil {
		return err
	}

	wantProject := e.ProjectPath
	wantTabID := e.activeTabID()
	wantHistory := e.History
	wantCursor := e.History.Index()
	wantCmd := e.History.topUndoCommand()

	toastMsg := fmt.Sprintf("Restored to %s · %s", ago, detail)
	e.Status = toastMsg
	e.ShowToast(Toast{
		ID:             "history-restore",
		Kind:           ToastSuccess,
		Message:        toastMsg,
		ActionLabel:    e.T("toolbar.undo"),
		ActionShortcut: FormatShortcut(e.EffectiveShortcut(CmdUndo), e.defaultShortcutOS()),
		OnAction: func() {
			e.DismissToast()
			if e.Playing() || e.StartScreenOpen() || e.MergeConflictOpen() {
				return
			}
			if e.ProjectPath != wantProject || e.activeTabID() != wantTabID || e.History != wantHistory {
				return
			}
			if e.History == nil || e.History.Index() != wantCursor || e.History.topUndoCommand() != wantCmd {
				return
			}
			e.Undo()
		},
	})
	e.notify()
	return nil
}

// PreviewCheckpoint enters read-only History preview mode for persistent checkpoint id without
// mutating Doc or History. When the checkpoint contains the open scene, ActiveScene() renders its
// pre-turn scene snapshot read-only in the viewport.
func (e *Editor) PreviewCheckpoint(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.StartScreenOpen() || e.MergeConflictOpen() {
		return errors.New("editor: history preview is unavailable while a full-window screen is open")
	}
	m, err := e.CheckpointManifest(id)
	if err != nil {
		return err
	}

	var previewDoc *sceneformat.SceneDoc
	var previewScene *sceneformat.BuiltScene
	if e.ScenePath != "" {
		if raw, existed, rerr := e.CheckpointFileBytes(id, e.ScenePath); rerr == nil && existed {
			if doc, perr := sceneformat.ParseWith(raw, sceneformat.ParseOptions{
				File:          e.FilePath,
				Components:    e.components,
				PhysicsLayers: e.effectivePhysicsLayers(),
			}); perr == nil {
				if built, berr := e.buildSceneDoc(doc); berr == nil {
					built.DrawAllTiles = true
					previewDoc = doc
					previewScene = built
				}
			}
		}
	}

	entry := checkpointManifestToHistoryEntry(*m)
	e.cancelTransientInteraction()
	e.cancelAutoSaveTimer()
	e.historyPanel.preview = &historyPreviewState{
		checkpointID: id,
		entry:        entry,
		doc:          previewDoc,
		scene:        previewScene,
	}
	e.s3d.stale = true
	e.Status = fmt.Sprintf("Previewing %s · editing is off", e.FormatHistoryRelativeTime(entry.Timestamp))
	e.notify()
	return nil
}

func checkpointManifestToHistoryEntry(m CheckpointManifest) HistoryEntry {
	ts, _ := time.Parse(time.RFC3339, m.CreatedAt)
	files := make([]string, len(m.Files))
	for i, f := range m.Files {
		files[i] = f.Path
	}
	return HistoryEntry{
		Kind:           HistoryEntryCheckpoint,
		Author:         m.Who,
		Summary:        m.Title,
		Detail:         m.Detail,
		Files:          files,
		Timestamp:      ts,
		CheckpointID:   m.ID,
		CheckpointKind: m.Kind,
		Persistent:     true,
	}
}

// UnifiedHistoryEntries returns the merged History panel timeline combining the active scene's
// session entries, persisted project checkpoints from .nsgo/checkpoints/, and Git milestones,
// filtered by filter and sorted newest first. Session sub-commands captured inside a persisted
// checkpoint are folded into that checkpoint row so turns are never duplicated.
func (e *Editor) UnifiedHistoryEntries(filter HistoryFilter) []HistoryEntry {
	if e == nil {
		return nil
	}
	if filter == "" {
		filter = e.HistoryFilter()
	}

	cps := e.Checkpoints()
	cpSet := make(map[string]bool, len(cps))
	var out []HistoryEntry
	for _, cp := range cps {
		cpSet[cp.ID] = true
		entry := checkpointManifestToHistoryEntry(cp.CheckpointManifest)
		if !historyEntryMatchesFilter(entry, filter) {
			continue
		}
		out = append(out, entry)
	}

	for _, entry := range e.FilteredHistoryEntries(filter) {
		if entry.CheckpointID != "" && cpSet[entry.CheckpointID] {
			continue
		}
		out = append(out, entry)
	}

	if e.History != nil && filter == HistoryFilterAll {
		for _, m := range e.History.Milestones() {
			detail := m.Branch
			if len(m.Files) > 0 {
				detail = fmt.Sprintf("%d files · %s", len(m.Files), m.Branch)
			}
			out = append(out, HistoryEntry{
				Kind:      HistoryEntryEdit,
				Author:    HistoryAuthorGit,
				Summary:   m.Summary,
				Detail:    detail,
				Files:     slices.Clone(m.Files),
				Timestamp: m.Timestamp,
			})
		}
	}

	slices.SortStableFunc(out, func(a, b HistoryEntry) int {
		if !a.Timestamp.Equal(b.Timestamp) {
			if a.Timestamp.After(b.Timestamp) {
				return -1
			}
			return 1
		}
		if a.Persistent != b.Persistent {
			if a.Persistent {
				return -1
			}
			return 1
		}
		if a.CheckpointID != b.CheckpointID {
			return strings.Compare(b.CheckpointID, a.CheckpointID)
		}
		if a.ID != b.ID {
			if a.ID > b.ID {
				return -1
			}
			return 1
		}
		return 0
	})
	return out
}

func historyEntryMatchesFilter(entry HistoryEntry, filter HistoryFilter) bool {
	switch filter {
	case HistoryFilterYou:
		return entry.Kind != HistoryEntrySaved && entry.Author == HistoryAuthorYou
	case HistoryFilterAgent:
		return entry.Kind != HistoryEntrySaved && entry.Author == HistoryAuthorAgent
	default:
		return true
	}
}

// beforeHistoryExecute runs right before a History command mutates editor state.
// When an agent turn is active, it snapshots the open scene (or game.yaml for project edits)
// before the first write to that file.
func (e *Editor) beforeHistoryExecute(cmd Command) {
	if e == nil || e.checkpoints.active == nil {
		return
	}
	if _, isRestore := cmd.(*checkpointRestoreCmd); isRestore {
		return
	}
	if _, isProj := cmd.(*projectCmd); isProj {
		if e.Project != nil && e.ProjectPath != "" {
			_ = e.RecordCheckpointFile(project.FileName)
		}
		return
	}
	if e.ScenePath != "" {
		_ = e.RecordCheckpointFile(e.ScenePath)
	}
}

func (e *Editor) resetCheckpointsForProject() {
	if e == nil {
		return
	}
	e.abortActiveTurn()
	e.checkpoints.list = nil
	if e.Project != nil && e.ProjectPath != "" {
		_, _ = e.LoadCheckpoints()
	}
}
