package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
	"go.yaml.in/yaml/v3"
)

// RecentProjectsVersion is the schema version of the per-user recent projects YAML file.
const RecentProjectsVersion = 1

// RecentProjectsFileName is the filename of the per-user recent projects list.
const RecentProjectsFileName = "recent-projects.yaml"

// MaxRecentProjects caps how many recent project entries are retained.
const MaxRecentProjects = 20

// RecentProjectsFile is the strict versioned YAML document storing projects recently opened by a user outside any project directory.
type RecentProjectsFile struct {
	// Version is the schema version; it must equal RecentProjectsVersion (1).
	Version int `yaml:"version"`
	// Projects lists recently opened projects, most recently opened first.
	Projects []RecentProject `yaml:"projects,omitempty"`
}

// RecentProject records one project opened in the editor.
type RecentProject struct {
	// Name is the project's display name (from game.yaml or its folder name).
	Name string `yaml:"name"`
	// Path is the absolute path to the project directory.
	Path string `yaml:"path"`
	// LastOpened is the RFC3339 UTC timestamp when the project was last opened.
	LastOpened string `yaml:"lastOpened"`
}

// RecentProjectEntry pairs a persisted RecentProject with whether its project folder is missing on disk.
type RecentProjectEntry struct {
	RecentProject
	// Missing reports whether the project directory or its game.yaml no longer exists on disk.
	Missing bool
}

// DefaultRecentProjectsPath returns the default per-user path for recent-projects.yaml under the OS user config directory.
func DefaultRecentProjectsPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("editor: user config dir: %w", err)
	}
	return filepath.Join(cfgDir, "nsgo-game", RecentProjectsFileName), nil
}

// ParseRecentProjects strictly decodes and validates a versioned recent-projects YAML document,
// refusing unknown fields, duplicate keys, trailing YAML documents, and unsupported versions.
func ParseRecentProjects(data []byte) (*RecentProjectsFile, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add a recent-projects YAML document starting with version: %d", RecentProjectsVersion), "editor: recent projects: empty document (missing version)"), "", data, "")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("editor: recent projects: parse: %w", err), "", data, "check the YAML syntax in recent-projects.yaml")
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("write recent-projects.yaml as a YAML mapping starting with version: %d", RecentProjectsVersion), "editor: recent projects: document must be a YAML mapping with version"), "", data, "")
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
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("add version: %d at the top of recent-projects.yaml", RecentProjectsVersion), "editor: recent projects: missing required field version"), "", data, "")
	}

	var f RecentProjectsFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("editor: recent projects: decode: %w", err), "", data, "check the YAML syntax and field names in recent-projects.yaml")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in recent-projects.yaml and remove any --- separator", "editor: recent projects: unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("editor: recent projects: decode: %w", err), "", data, "remove trailing YAML content after recent-projects.yaml")
	}
	if err := ValidateRecentProjects(&f); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &f, nil
}

// checkDuplicateYAMLKeys walks a YAML AST and rejects repeated keys in any mapping node.
func checkDuplicateYAMLKeys(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		seen := make(map[string]bool, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if seen[k] {
				return &loaderr.Error{
					Line:    n.Content[i].Line,
					Column:  n.Content[i].Column,
					Field:   k,
					Problem: fmt.Sprintf("line %d: duplicate key %q", n.Content[i].Line, k),
					Fix:     fmt.Sprintf("remove the duplicate %q key", k),
				}
			}
			seen[k] = true
			if err := checkDuplicateYAMLKeys(n.Content[i+1]); err != nil {
				return err
			}
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range n.Content {
			if err := checkDuplicateYAMLKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateRecentProjects checks that f conforms to version 1 of the recent projects schema.
func ValidateRecentProjects(f *RecentProjectsFile) error {
	if f == nil {
		return loaderr.Errorf("", "pass a non-nil *editor.RecentProjectsFile to ValidateRecentProjects", "editor: recent projects: nil document")
	}
	if f.Version != RecentProjectsVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of recent-projects.yaml", RecentProjectsVersion), "editor: recent projects: unsupported version %d (want %d)", f.Version, RecentProjectsVersion)
	}
	seen := make(map[string]bool, len(f.Projects))
	for i, p := range f.Projects {
		pPath := fmt.Sprintf("projects[%d]", i)
		if err := validateRecentText("name", p.Name); err != nil {
			return loaderr.Errorf(pPath+".name", "set name to a non-empty project name without surrounding spaces or control characters", "editor: recent projects: projects[%d]: %w", i, err)
		}
		if err := validateRecentText("path", p.Path); err != nil {
			return loaderr.Errorf(pPath+".path", "set path to the project directory path without surrounding spaces or control characters", "editor: recent projects: projects[%d]: %w", i, err)
		}
		if seen[p.Path] {
			return loaderr.Errorf(pPath+".path", "remove the duplicate project path entry", "editor: recent projects: duplicate path %q", p.Path)
		}
		seen[p.Path] = true
		if p.LastOpened == "" {
			return loaderr.Errorf(pPath+".lastOpened", "set lastOpened to an RFC3339 UTC timestamp such as 2026-01-02T15:04:05Z", "editor: recent projects: projects[%d]: lastOpened is required", i)
		}
		if _, err := time.Parse(time.RFC3339, p.LastOpened); err != nil {
			return loaderr.Errorf(pPath+".lastOpened", "format lastOpened as an RFC3339 timestamp such as 2026-01-02T15:04:05Z", "editor: recent projects: projects[%d]: lastOpened %q must be RFC3339: %w", i, p.LastOpened, err)
		}
	}
	return nil
}

func validateRecentText(field, s string) error {
	if s == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s %q must have no surrounding spaces or control characters", field, s)
	}
	return nil
}

// EncodeRecentProjects validates f and encodes it as deterministic 2-space YAML that ParseRecentProjects reads back unchanged.
func EncodeRecentProjects(f *RecentProjectsFile) ([]byte, error) {
	if err := ValidateRecentProjects(f); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("editor: recent projects: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: recent projects: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadRecentProjectsFile reads and parses the recent projects YAML file at path.
// When the file does not exist, it returns an empty version 1 RecentProjectsFile and a nil error.
func LoadRecentProjectsFile(path string) (*RecentProjectsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &RecentProjectsFile{Version: RecentProjectsVersion}, nil
		}
		return nil, err
	}
	f, err := ParseRecentProjects(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return f, nil
}

// SaveRecentProjectsFile encodes f and atomically writes it to path, creating parent directories if needed.
func SaveRecentProjectsFile(path string, f *RecentProjectsFile) error {
	data, err := EncodeRecentProjects(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// SetRecentProjectsPath configures where the editor persists its per-user recent projects YAML file
// and loads any existing entries from path (an absent file starts with an empty list).
func (e *Editor) SetRecentProjectsPath(path string) error {
	if e == nil {
		return nil
	}
	e.RecentProjectsPath = path
	return e.LoadRecentProjects()
}

// LoadRecentProjects loads the recent projects list from Editor.RecentProjectsPath when set.
func (e *Editor) LoadRecentProjects() error {
	if e == nil || e.RecentProjectsPath == "" {
		return nil
	}
	f, err := LoadRecentProjectsFile(e.RecentProjectsPath)
	if err != nil {
		return err
	}
	e.recentProjects = append([]RecentProject(nil), f.Projects...)
	e.bumpRecentRev()
	e.notify()
	return nil
}

func (e *Editor) bumpRecentRev() {
	if e == nil {
		return
	}
	e.startScreen.mu.Lock()
	e.startScreen.recentRev++
	e.startScreen.mu.Unlock()
}

func (e *Editor) recentProjectsRev() uint64 {
	if e == nil {
		return 0
	}
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.recentRev
}

func (e *Editor) hasRecentProjects() bool {
	return e != nil && len(e.recentProjects) > 0
}

// RecentProjects returns the editor's recent projects in most-recently-opened order,
// marking each entry whose directory or game.yaml is currently missing on disk.
func (e *Editor) RecentProjects() []RecentProjectEntry {
	if e == nil || len(e.recentProjects) == 0 {
		return nil
	}
	out := make([]RecentProjectEntry, len(e.recentProjects))
	for i, rp := range e.recentProjects {
		out[i] = RecentProjectEntry{
			RecentProject: rp,
			Missing:       !projectDirExists(rp.Path),
		}
	}
	return out
}

func projectDirExists(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	finfo, err := os.Stat(filepath.Join(dir, project.FileName))
	return err == nil && !finfo.IsDir()
}

// RecordRecentProject adds or moves the project at dir to the top of the recent projects list
// with the current UTC timestamp, and persists the list to Editor.RecentProjectsPath when set.
func (e *Editor) RecordRecentProject(name, dir string) error {
	if e == nil {
		return nil
	}
	cleanDir := strings.TrimSpace(dir)
	if cleanDir == "" {
		return errors.New("editor: recent project path is empty")
	}
	if abs, err := filepath.Abs(cleanDir); err == nil {
		cleanDir = abs
	}
	cleanDir = filepath.Clean(cleanDir)
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		cleanName = filepath.Base(cleanDir)
	}
	now := e.now().UTC().Format(time.RFC3339)

	next := make([]RecentProject, 0, min(len(e.recentProjects)+1, MaxRecentProjects))
	next = append(next, RecentProject{
		Name:       cleanName,
		Path:       cleanDir,
		LastOpened: now,
	})
	for _, existing := range e.recentProjects {
		if filepath.Clean(existing.Path) == cleanDir {
			continue
		}
		if len(next) >= MaxRecentProjects {
			break
		}
		next = append(next, existing)
	}
	e.recentProjects = next
	e.bumpRecentRev()
	if e.RecentProjectsPath != "" {
		if err := SaveRecentProjectsFile(e.RecentProjectsPath, &RecentProjectsFile{
			Version:  RecentProjectsVersion,
			Projects: next,
		}); err != nil {
			return err
		}
	}
	e.notify()
	return nil
}

// RemoveRecentProject removes the project at dir from the recent projects list and persists the change when RecentProjectsPath is set.
func (e *Editor) RemoveRecentProject(dir string) error {
	if e == nil {
		return nil
	}
	target := filepath.Clean(dir)
	next := make([]RecentProject, 0, len(e.recentProjects))
	removed := false
	for _, existing := range e.recentProjects {
		if existing.Path == dir || filepath.Clean(existing.Path) == target {
			removed = true
			continue
		}
		next = append(next, existing)
	}
	if !removed {
		return nil
	}
	e.recentProjects = next
	e.bumpRecentRev()
	if e.RecentProjectsPath != "" {
		if err := SaveRecentProjectsFile(e.RecentProjectsPath, &RecentProjectsFile{
			Version:  RecentProjectsVersion,
			Projects: next,
		}); err != nil {
			return err
		}
	}
	e.notify()
	return nil
}

func (e *Editor) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// formatRelativeTime formats an RFC3339 timestamp relative to now (matching the Start screen design).
func formatRelativeTime(rfc3339 string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	case d < 24*time.Hour:
		hrs := int(d.Hours())
		if hrs == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hrs)
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 14*24*time.Hour:
		return "last week"
	default:
		return t.UTC().Format("2006-01-02")
	}
}
