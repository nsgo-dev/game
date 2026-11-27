package refs

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/resource"
	"go.yaml.in/yaml/v3"
)

// IndexFileName is the project index's name at a project's root, beside game.yaml.
const IndexFileName = "game.index.yaml"

// IndexVersion is the index schema version this package reads and writes.
const IndexVersion = 1

// indexComment is the first line EncodeIndex writes, telling a person who opens the file what it is.
const indexComment = "# The stable IDs of this project's files, kept by game index, game mv and the editor. Keep it in version control.\n"

// Index is the project index: where each file a reference may name was the last time a tool looked. Its files are sorted by path, and each has either a UID or a SHA256.
type Index struct {
	// Version is the schema version; it must equal IndexVersion (1).
	Version int `yaml:"version"`
	// Files are the indexed files in path order.
	Files []IndexEntry `yaml:"files,omitempty"`
}

// IndexEntry is one indexed file.
type IndexEntry struct {
	// Path is the file's slash-separated path from the project root.
	Path string `yaml:"path"`
	// UID is the stable ID in the header of a scene or resource file.
	UID string `yaml:"uid,omitempty"`
	// SHA256 is the lower-case hexadecimal SHA-256 of the contents of any other file, such as an image.
	SHA256 string `yaml:"sha256,omitempty"`
}

// ParseIndex decodes a YAML project index and validates it. Unknown fields and trailing YAML documents are refused.
func ParseIndex(data []byte) (*Index, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("refs: empty index")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var ix Index
	if err := dec.Decode(&ix); err != nil {
		return nil, fmt.Errorf("refs: decode index: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("refs: unexpected trailing YAML document in the index")
		}
		return nil, fmt.Errorf("refs: decode index: %w", err)
	}
	if err := ix.Validate(); err != nil {
		return nil, err
	}
	return &ix, nil
}

// EncodeIndex validates ix and writes it as deterministic 2-space YAML after a one-line comment saying what the file is, which ParseIndex reads back unchanged.
func EncodeIndex(ix *Index) ([]byte, error) {
	if err := ix.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(indexComment)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(ix); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("refs: encode index: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("refs: encode index: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks that ix conforms to the version 1 schema: valid relative paths in strictly increasing order, and for each exactly one of a valid UID and a SHA-256 of 64 lower-case hexadecimal digits, with no UID used twice.
func (ix *Index) Validate() error {
	if ix == nil {
		return errors.New("refs: nil index")
	}
	if ix.Version != IndexVersion {
		return fmt.Errorf("refs: unsupported index version %d (want %d)", ix.Version, IndexVersion)
	}
	uids := make(map[string]string, len(ix.Files))
	for i, e := range ix.Files {
		if !validPath(e.Path) {
			return fmt.Errorf("refs: index file %d path %q must be a slash-separated path inside the project", i, e.Path)
		}
		if i > 0 && ix.Files[i-1].Path >= e.Path {
			return fmt.Errorf("refs: index file %q must come after %q: files are sorted by path, each once", ix.Files[i-1].Path, e.Path)
		}
		switch {
		case e.UID != "" && e.SHA256 != "":
			return fmt.Errorf("refs: index file %q has both a uid and a sha256", e.Path)
		case e.UID != "":
			if !resource.ValidUID(e.UID) {
				return fmt.Errorf("refs: index file %q uid %q is not a uid", e.Path, e.UID)
			}
			if other, dup := uids[e.UID]; dup {
				return fmt.Errorf("refs: index files %q and %q have the same uid %q", other, e.Path, e.UID)
			}
			uids[e.UID] = e.Path
		case e.SHA256 != "":
			if !validSHA256(e.SHA256) {
				return fmt.Errorf("refs: index file %q sha256 %q must be 64 lower-case hexadecimal digits", e.Path, e.SHA256)
			}
		default:
			return fmt.Errorf("refs: index file %q needs a uid or a sha256", e.Path)
		}
	}
	return nil
}

// Lookup returns the entry for path, a slash-separated path from the project root.
func (ix *Index) Lookup(path string) (IndexEntry, bool) {
	if ix == nil {
		return IndexEntry{}, false
	}
	i, ok := slices.BinarySearchFunc(ix.Files, path, func(e IndexEntry, p string) int { return cmp.Compare(e.Path, p) })
	if !ok {
		return IndexEntry{}, false
	}
	return ix.Files[i], true
}

// validSHA256 reports whether s is 64 lower-case hexadecimal digits.
func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validPath reports whether p is a slash-separated path of a file inside the project, as fs.ValidPath accepts it, other than the root itself.
func validPath(p string) bool {
	return p != "." && fs.ValidPath(p) && !strings.ContainsRune(p, '\\')
}
