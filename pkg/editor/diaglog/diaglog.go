// Package diaglog writes bounded, redacted editor diagnostics.
package diaglog

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

const DefaultMaxBytes int64 = 4 << 20
const DefaultMaxFiles = 3

// Redactor removes secrets and machine-local identifiers from diagnostics.
type Redactor struct {
	Home, Project, Data string
	Secrets             []string
}

func (r Redactor) Redact(s string) string {
	for _, pair := range [][2]string{{r.Home, "<home>"}, {r.Project, "<project>"}, {r.Data, "<data>"}} {
		if pair[0] != "" {
			s = strings.ReplaceAll(s, pair[0], pair[1])
		}
	}
	for _, secret := range r.Secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "<redacted>")
		}
	}
	s = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`).ReplaceAllString(s, `${1}<redacted>`)
	s = regexp.MustCompile(`(?i)(authorization|cookie)(\s*[:=]\s*)[^\s,;]+`).ReplaceAllString(s, `${1}${2}<redacted>`)
	s = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`).ReplaceAllString(s, "<redacted-private-key>")
	s = regexp.MustCompile(`(?i)\b[A-Z][A-Z0-9_]*(?:KEY|SECRET|TOKEN|PASSWORD)\s*=\S+`).ReplaceAllString(s, "<redacted-env>")
	s = regexp.MustCompile(`https?://[^\s/?]+[^\s]*`).ReplaceAllStringFunc(s, func(v string) string {
		if i := strings.Index(v, "?"); i >= 0 {
			v = v[:i]
		}
		if i := strings.Index(v, "#"); i >= 0 {
			v = v[:i]
		}
		return v
	})
	s = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`).ReplaceAllString(s, "<redacted-email>")
	return s
}

// Writer writes one redacted line stream and rotates old files by size.
type Writer struct {
	mu          sync.Mutex
	dir, stream string
	maxBytes    int64
	maxFiles    int
	redactor    Redactor
	file        *os.File
	size        int64
}

func NewWriter(dir, stream string, redactor Redactor, maxBytes int64, maxFiles int) (*Writer, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, stream: stream, maxBytes: maxBytes, maxFiles: maxFiles, redactor: redactor}
	return w, nil
}
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}
func (w *Writer) WriteLine(line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	line = w.redactor.Redact(line)
	if len(line) > 64<<10 {
		line = line[:64<<10] + "<truncated>"
	}
	line += "\n"
	if w.file == nil || w.size+int64(len(line)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	n, err := w.file.WriteString(line)
	w.size += int64(n)
	return err
}
func (w *Writer) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	base := filepath.Join(w.dir, w.stream+".log")
	for i := w.maxFiles - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", base, i)
		next := base
		if i > 1 {
			next = fmt.Sprintf("%s.%d", base, i-1)
		}
		if _, err := os.Stat(old); err == nil {
			_ = os.Remove(next)
			_ = os.Rename(old, next)
		}
	}
	_ = os.Remove(base + ".1")
	if _, err := os.Stat(base); err == nil {
		_ = os.Rename(base, base+".1")
	}
	f, err := os.OpenFile(base, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.file = f
	st, _ := f.Stat()
	w.size = st.Size()
	return nil
}

// Manifest is the exported diagnostics metadata.
type Manifest struct {
	Version        int     `yaml:"version"`
	EditorVersion  string  `yaml:"editorVersion"`
	GoVersion      string  `yaml:"goVersion"`
	NSGoVersion    string  `yaml:"nsgoVersion"`
	OS             string  `yaml:"os"`
	Arch           string  `yaml:"arch"`
	ProjectVersion string  `yaml:"projectVersion,omitempty"`
	Entries        []Entry `yaml:"entries"`
}
type Entry struct {
	Name string `yaml:"name"`
	Size int64  `yaml:"size"`
}

func EncodeManifest(m Manifest) ([]byte, error) {
	if m.Version != 1 {
		return nil, fmt.Errorf("diaglog: unsupported manifest version %d", m.Version)
	}
	return yaml.Marshal(m)
}
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return m, err
	}
	if m.Version != 1 {
		return m, fmt.Errorf("diaglog: unsupported manifest version %d", m.Version)
	}
	return m, nil
}

// Export writes redacted log files and manifest into a zip archive.
func Export(logDir, out string, m Manifest, redactor Redactor) error {
	data, err := EncodeManifest(m)
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(logDir, "*.log*"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	h, err := zw.Create("manifest.yaml")
	if err != nil {
		return err
	}
	if _, err = h.Write(data); err != nil {
		return err
	}
	for _, path := range files {
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		name := filepath.Base(path)
		entry, e := zw.Create(name)
		if e != nil {
			return e
		}
		if _, e = io.WriteString(entry, redactor.Redact(string(raw))); e != nil {
			return e
		}
	}
	return zw.Close()
}

var _ = time.Time{}
