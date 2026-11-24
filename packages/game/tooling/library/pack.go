package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/project"
)

// ManifestFileName is a pack's manifest, at the root of its directory.
const ManifestFileName = "pack.yaml"

// ManifestVersion is the manifest schema version this package reads and writes.
const ManifestVersion = 1

// MaxFileSize is the largest file a pack may hold, in bytes.
const MaxFileSize = 64 << 20

// MaxFiles is the most files a pack's manifest may list.
const MaxFiles = 4096

// Manifest is a pack's versioned pack.yaml.
type Manifest struct {
	// Version is the schema version; it must equal ManifestVersion (1).
	Version int `yaml:"version"`
	// Name identifies the pack, as project.ValidatePackName accepts it.
	Name string `yaml:"name"`
	// Kind is one of project.PackKinds.
	Kind string `yaml:"kind"`
	// PackVersion is the pack's own version, such as 1.2.0.
	PackVersion string `yaml:"packVersion"`
	// Description says in one line what the pack holds.
	Description string `yaml:"description"`
	// License is the license of the pack's files, as an SPDX identifier such as CC0-1.0.
	License string `yaml:"license"`
	// Into is the slash-separated project directory the pack's files are written to; empty is DefaultDir(Name).
	Into string `yaml:"into,omitempty"`
	// Files are the slash-separated paths of the pack's files relative to its directory, pack.yaml aside; no path may be another's directory.
	Files []string `yaml:"files,omitempty"`
	// Autoload lists the pack's files that installing it adds to game.yaml's autoload section; nil adds none.
	Autoload *Autoload `yaml:"autoload,omitempty"`
	// Plugins names the editor plugins a plugin pack turns on under game.yaml's editor.plugins; a plugin pack names at least one, and the other kinds none.
	Plugins []string `yaml:"plugins,omitempty,flow"`
}

// Autoload lists the pack files installing a pack autoloads.
type Autoload struct {
	// Scenes are scene files to autoload; each path is one of Manifest.Files, ending in .yaml or .yml.
	Scenes []project.Entry `yaml:"scenes,omitempty"`
	// Resources are other files to autoload; each path is one of Manifest.Files.
	Resources []project.Entry `yaml:"resources,omitempty"`
}

// DefaultDir is the project directory a pack called name installs into when neither its manifest nor the installer names one.
func DefaultDir(name string) string { return "packs/" + name }

// ParseManifest decodes a YAML pack manifest and validates it. Unknown fields and trailing YAML documents are refused.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := decodeStrict(data, &m); err != nil {
		return nil, fmt.Errorf("library: %s: %w", ManifestFileName, err)
	}
	if err := ValidateManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// EncodeManifest validates m and serializes it as deterministic 2-space YAML that ParseManifest reads back unchanged.
func EncodeManifest(m *Manifest) ([]byte, error) {
	if err := ValidateManifest(m); err != nil {
		return nil, err
	}
	return encode(m)
}

// ValidateManifest checks that m conforms to the version 1 schema.
func ValidateManifest(m *Manifest) error {
	if m == nil {
		return errors.New("library: nil manifest")
	}
	if err := validateManifest(m); err != nil {
		return fmt.Errorf("library: %s: %w", ManifestFileName, err)
	}
	return nil
}

func validateManifest(m *Manifest) error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("unsupported version %d (want %d)", m.Version, ManifestVersion)
	}
	if err := project.ValidatePackName(m.Name); err != nil {
		return err
	}
	if err := validateKind(m.Kind); err != nil {
		return err
	}
	if err := project.ValidatePackVersion(m.PackVersion); err != nil {
		return err
	}
	if err := validateLine("description", m.Description, true); err != nil {
		return err
	}
	if err := validateLicense(m.License); err != nil {
		return err
	}
	if m.Into != "" && !isPackPath(m.Into) {
		return fmt.Errorf("into %q must be a slash-separated directory inside the project", m.Into)
	}
	if len(m.Files) > MaxFiles {
		return fmt.Errorf("lists %d files, more than %d", len(m.Files), MaxFiles)
	}
	files := make(map[string]struct{}, len(m.Files))
	for _, f := range m.Files {
		if !isPackPath(f) {
			return fmt.Errorf("file %q must be a slash-separated path inside the pack", f)
		}
		if f == ManifestFileName {
			return fmt.Errorf("lists %s, which every pack has", ManifestFileName)
		}
		if _, dup := files[f]; dup {
			return fmt.Errorf("file %q is listed twice", f)
		}
		files[f] = struct{}{}
	}
	for f := range files {
		for dir := path.Dir(f); dir != "."; dir = path.Dir(dir) {
			if _, ok := files[dir]; ok || dir == ManifestFileName {
				return fmt.Errorf("file %q is inside %q, which is a file", f, dir)
			}
		}
	}
	if a := m.Autoload; a != nil {
		names := map[string]struct{}{}
		for _, list := range []struct {
			kind    string
			entries []project.Entry
		}{{"scene", a.Scenes}, {"resource", a.Resources}} {
			for _, e := range list.entries {
				if e.Name == "" || strings.IndexFunc(e.Name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
					return fmt.Errorf("autoload %s name %q must be non-empty without spaces or control characters", list.kind, e.Name)
				}
				if _, dup := names[list.kind+" "+e.Name]; dup {
					return fmt.Errorf("autoload %s %q is listed twice", list.kind, e.Name)
				}
				names[list.kind+" "+e.Name] = struct{}{}
				if _, ok := files[e.Path]; !ok {
					return fmt.Errorf("autoload %s %q path %q is not one of the pack's files", list.kind, e.Name, e.Path)
				}
				if list.kind == "scene" && !strings.HasSuffix(e.Path, ".yaml") && !strings.HasSuffix(e.Path, ".yml") {
					return fmt.Errorf("autoload scene %q path %q must be a .yaml or .yml file", e.Name, e.Path)
				}
			}
		}
	}
	if m.Kind == project.PackPlugin && len(m.Plugins) == 0 {
		return errors.New("a plugin pack must name the editor plugins it turns on under plugins")
	}
	if m.Kind != project.PackPlugin && len(m.Plugins) > 0 {
		return fmt.Errorf("only a plugin pack may turn on editor plugins, and this is a %s pack", m.Kind)
	}
	seen := make(map[string]struct{}, len(m.Plugins))
	for _, p := range m.Plugins {
		if p == "" || strings.IndexFunc(p, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return fmt.Errorf("plugin %q must be non-empty without spaces or control characters", p)
		}
		if _, dup := seen[p]; dup {
			return fmt.Errorf("plugin %q is listed twice", p)
		}
		seen[p] = struct{}{}
	}
	return nil
}

// isPackPath reports whether p is a slash-separated relative path that stays inside the directory it is relative to and is not that directory itself.
func isPackPath(p string) bool {
	return p != "." && fs.ValidPath(p) && !strings.Contains(p, `\`)
}

// Pack is a pack read into memory: its manifest and the content of every file it holds.
type Pack struct {
	// Manifest is the parsed pack.yaml.
	Manifest *Manifest
	// Files maps each file's slash-separated path in the pack to its content, pack.yaml included.
	Files map[string][]byte
}

// Names returns the paths of p's files, pack.yaml included, sorted.
func (p *Pack) Names() []string {
	names := make([]string, 0, len(p.Files))
	for name := range p.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Digest returns p's digest, as the package function Digest computes it from p.Files.
func (p *Pack) Digest() string { return Digest(p.Files) }

// Digest returns the digest of a pack's files: the SHA-256, in lower-case hexadecimal, of one line per file sorted by path, each the file's own SHA-256 in lower-case hexadecimal, two spaces and its slash-separated path, ended by a newline. The listing is what sha256sum prints for the files named in byte order, so the digest can be checked without this package; the game library digest command prints it for a pack's directory.
func Digest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	h := sha256.New()
	for _, name := range names {
		sum := sha256.Sum256(files[name])
		fmt.Fprintf(h, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ReadPack reads the pack whose directory is dir in fsys: its pack.yaml and every file the manifest lists. A file the manifest does not list is not part of the pack.
func ReadPack(fsys fs.FS, dir string) (*Pack, error) {
	return readPack(func(name string) ([]byte, error) {
		return readLimited(fsys, path.Join(dir, name))
	})
}

// readPack reads a pack's manifest and files with read, which takes a path relative to the pack's directory.
func readPack(read func(name string) ([]byte, error)) (*Pack, error) {
	data, err := read(ManifestFileName)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	p := &Pack{Manifest: m, Files: map[string][]byte{ManifestFileName: data}}
	for _, name := range m.Files {
		data, err := read(name)
		if err != nil {
			return nil, fmt.Errorf("library: pack %s: %w", m.Name, err)
		}
		p.Files[name] = data
	}
	return p, nil
}

// readLimited reads name from fsys, refusing a file larger than MaxFileSize.
func readLimited(fsys fs.FS, name string) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readAllLimited(f, name)
}

// readAllLimited reads r to its end, refusing more than MaxFileSize bytes.
func readAllLimited(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, MaxFileSize)
	}
	return data, nil
}
