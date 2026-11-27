package assets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// RemoteManifestVersion is the schema version of remote.yaml.
const RemoteManifestVersion = 1

// RemoteManifestFile is the canonical filename written beside content-hashed
// remote assets by `game assets`.
const RemoteManifestFile = "remote.yaml"

// RemoteManifest maps logical asset URIs (such as "asset:music/theme.m4a") to
// content-hashed paths on a remote origin, with each file's exact byte size
// and SHA-256 digest.
type RemoteManifest struct {
	// Version is the schema version; must equal RemoteManifestVersion (1).
	Version int `yaml:"version"`
	// Base is the default origin URL prefix (for example "https://cdn.example.com/game/")
	// joined with each entry's Path when RemoteOptions.Base is empty.
	Base string `yaml:"base,omitempty"`
	// Files lists the remotely delivered assets, sorted by URI in canonical output.
	Files []RemoteEntry `yaml:"files,omitempty"`
}

// RemoteEntry describes one loose asset file served from a remote origin.
type RemoteEntry struct {
	// URI is the logical asset URI (for example "asset:music/theme.m4a").
	URI string `yaml:"uri"`
	// Path is the content-hashed relative path under Base (for example "music/theme.9f86d081.m4a").
	Path string `yaml:"path"`
	// Size is the exact expected file size in bytes.
	Size int64 `yaml:"size"`
	// SHA256 is the lowercase 64-character hex-encoded SHA-256 digest of the file.
	SHA256 string `yaml:"sha256"`
}

// ParseRemoteManifest decodes a remote.yaml manifest, refusing unknown fields,
// trailing YAML documents, unsupported versions, duplicate URIs, invalid paths,
// negative sizes, and malformed SHA-256 digests.
func ParseRemoteManifest(data []byte) (*RemoteManifest, error) {
	const fix = "regenerate remote.yaml with go tool game assets or start it with version: 1"
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fix, "assets: remote manifest: empty document"), "", data, "")
	}
	var m RemoteManifest
	if err := decodeStrictYAML(data, &m); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("assets: remote manifest: %w", err), "", data, fix)
	}
	if err := validateRemoteManifest(&m); err != nil {
		return nil, loaderr.Locate(err, "", data, fix)
	}
	return &m, nil
}

// EncodeRemoteManifest encodes m as canonical 2-space YAML with Files sorted
// by URI so a round trip through ParseRemoteManifest and EncodeRemoteManifest
// changes nothing.
func EncodeRemoteManifest(m *RemoteManifest) ([]byte, error) {
	if m == nil {
		return nil, errors.New("assets: remote manifest is nil")
	}
	if err := validateRemoteManifest(m); err != nil {
		return nil, err
	}
	out := RemoteManifest{
		Version: m.Version,
		Base:    m.Base,
	}
	if len(m.Files) > 0 {
		out.Files = slices.Clone(m.Files)
		slices.SortFunc(out.Files, func(a, b RemoteEntry) int {
			return strings.Compare(a.URI, b.URI)
		})
	}
	return encodeYAML(&out)
}

// Lookup returns the manifest entry for uri (normalizing "asset:" paths), if
// present.
func (m *RemoteManifest) Lookup(uri string) (RemoteEntry, bool) {
	if m == nil || len(m.Files) == 0 {
		return RemoteEntry{}, false
	}
	want := normalizeURI(uri)
	for _, f := range m.Files {
		if normalizeURI(f.URI) == want {
			return f, true
		}
	}
	return RemoteEntry{}, false
}

// HashedPath inserts the first 8 lowercase hex characters of sha256Hex before
// the extension of relPath, producing "<name>.<hash8>.<ext>" (or
// "<name>.<hash8>" when relPath has no extension).
func HashedPath(relPath, sha256Hex string) string {
	clean := path.Clean(strings.TrimPrefix(strings.ReplaceAll(relPath, `\`, "/"), "/"))
	hash8 := strings.ToLower(strings.TrimSpace(sha256Hex))
	if len(hash8) > 8 {
		hash8 = hash8[:8]
	}
	dir := path.Dir(clean)
	base := path.Base(clean)
	ext := fileExt(base)
	stem := strings.TrimSuffix(base, ext)
	hashed := stem + "." + hash8 + ext
	if dir == "." || dir == "" {
		return hashed
	}
	return dir + "/" + hashed
}

// fileExt returns base's extension, treating a leading dot with no second dot
// (such as ".keep") as having no extension.
func fileExt(base string) string {
	ext := path.Ext(base)
	if ext == base {
		return ""
	}
	return ext
}

// RemoteURL joins an HTTP(S) base URL with a clean relative slash-separated
// asset path, escaping each path segment and refusing path traversal.
func RemoteURL(base, relPath string) (string, error) {
	u, err := parseRemoteBaseURL(base)
	if err != nil {
		return "", err
	}
	if err := validateRemoteRelPath(relPath); err != nil {
		return "", err
	}
	segments := strings.Split(relPath, "/")
	escaped := make([]string, len(segments))
	for i, seg := range segments {
		escaped[i] = url.PathEscape(seg)
	}
	basePath := u.EscapedPath()
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	joined := *u
	joined.Path = ""
	joined.RawPath = ""
	joined.RawQuery = ""
	joined.Fragment = ""
	return joined.Scheme + "://" + joined.Host + basePath + strings.Join(escaped, "/"), nil
}

func validateRemoteManifest(m *RemoteManifest) error {
	if m.Version != RemoteManifestVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d or regenerate remote.yaml with go tool game assets", RemoteManifestVersion), "assets: remote manifest: unsupported version %d (want %d)", m.Version, RemoteManifestVersion)
	}
	if m.Base != "" {
		if _, err := parseRemoteBaseURL(m.Base); err != nil {
			return loaderr.Errorf("base", "set base to an http:// or https:// URL without query or fragment", "assets: remote manifest: base: %w", err)
		}
	}
	seen := make(map[string]string, len(m.Files))
	for i, f := range m.Files {
		fieldPrefix := fmt.Sprintf("files[%d]", i)
		if strings.TrimSpace(f.URI) == "" {
			return loaderr.Errorf(fieldPrefix+".uri", "set a non-empty asset URI for every file entry", "assets: remote manifest: %s: uri is required", fieldPrefix)
		}
		norm := normalizeURI(f.URI)
		if prev, dup := seen[norm]; dup {
			return loaderr.Errorf(fieldPrefix+".uri", "list each normalized asset URI once in files", "assets: remote manifest: %s: duplicate uri %q (already listed as %q)", fieldPrefix, f.URI, prev)
		}
		seen[norm] = f.URI
		if err := validateRemoteRelPath(f.Path); err != nil {
			return loaderr.Errorf(fieldPrefix+".path", "set path to a clean slash-separated relative file path", "assets: remote manifest: %s (%s): path: %w", fieldPrefix, f.URI, err)
		}
		if f.Size < 0 {
			return loaderr.Errorf(fieldPrefix+".size", "set size to a non-negative byte count", "assets: remote manifest: %s (%s): negative size %d", fieldPrefix, f.URI, f.Size)
		}
		if err := validateLowerSHA256(f.SHA256); err != nil {
			return loaderr.Errorf(fieldPrefix+".sha256", "set sha256 to the 64-character lowercase hex SHA-256 digest", "assets: remote manifest: %s (%s): sha256: %w", fieldPrefix, f.URI, err)
		}
	}
	return nil
}

func parseRemoteBaseURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL %q must use http or https", raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL %q must include a host", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("base URL %q must not contain a query or fragment", raw)
	}
	return u, nil
}

func validateRemoteRelPath(p string) error {
	switch {
	case p == "":
		return errors.New("path is required")
	case strings.Contains(p, `\`):
		return fmt.Errorf("path %q must use forward slashes", p)
	case p == "." || !fs.ValidPath(p):
		return fmt.Errorf("path %q must be a clean relative path", p)
	}
	return nil
}

func validateLowerSHA256(s string) error {
	if len(s) != sha256.Size*2 {
		return fmt.Errorf("expected %d lowercase hex characters, got %d", sha256.Size*2, len(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return fmt.Errorf("must be lowercase hex, got %q", s)
		}
	}
	return nil
}

func sumSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func decodeStrictYAML(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing YAML document")
		}
		return err
	}
	return nil
}

func encodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
