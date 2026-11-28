package assetbuild

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// cacheVersion is the schema version of the cache file.
const cacheVersion = 1

// cache is the YAML build cache: for each target, the key it was last built with and the files it wrote.
type cache struct {
	Version int          `yaml:"version"`
	Targets []cacheEntry `yaml:"targets,omitempty"`
}

// cacheEntry is one target's record in the cache.
type cacheEntry struct {
	// Target is the target's ID.
	Target string `yaml:"target"`
	// Key is the hash of what the target was built from.
	Key string `yaml:"key"`
	// Outputs are the files the target wrote, sorted by path.
	Outputs []cacheOutput `yaml:"outputs"`
	// Warnings are non-fatal diagnostics recorded when the target was built, such as requested font characters absent from the source font file.
	Warnings []string `yaml:"warnings,omitempty"`
}

// cacheOutput is one file a target wrote.
type cacheOutput struct {
	// Path is slash-separated and relative to the project directory.
	Path string `yaml:"path"`
	// SHA256 is the hex SHA-256 of the content written.
	SHA256 string `yaml:"sha256"`
}

// lookup returns the entry for the target id, or nil.
func (c *cache) lookup(id string) *cacheEntry {
	for i := range c.Targets {
		if c.Targets[i].Target == id {
			return &c.Targets[i]
		}
	}
	return nil
}

// paths returns the paths of e's outputs.
func (e *cacheEntry) paths() []string {
	out := make([]string, len(e.Outputs))
	for i, o := range e.Outputs {
		out[i] = o.Path
	}
	return out
}

// loadCache reads the cache at name. A missing, unreadable or malformed cache, or one of another version, is an empty cache: it only costs a full rebuild, after which it is written afresh.
func loadCache(name string) *cache {
	data, err := os.ReadFile(name)
	if err != nil {
		return &cache{Version: cacheVersion}
	}
	c, err := parseCache(data)
	if err != nil {
		return &cache{Version: cacheVersion}
	}
	return c
}

// parseCache decodes a cache file, refusing unknown fields, trailing documents and a version other than cacheVersion.
func parseCache(data []byte) (*cache, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("delete the cache file to rebuild it or start it with version: %d", cacheVersion), "assetbuild: cache: empty file"), "", data, "")
	}
	var c cache
	if err := decodeStrict(data, &c); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("assetbuild: cache: %w", err), "", data, "delete the build cache file so assetbuild regenerates it")
	}
	if c.Version != cacheVersion {
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("set version: %d or delete the build cache file to regenerate it", cacheVersion), "assetbuild: cache: unsupported version %d (want %d)", c.Version, cacheVersion), "", data, "")
	}
	return &c, nil
}

// encodeCache writes c as deterministic YAML, its targets sorted by ID.
func encodeCache(c *cache) ([]byte, error) {
	sorted := cache{Version: c.Version, Targets: slices.Clone(c.Targets)}
	slices.SortFunc(sorted.Targets, func(x, y cacheEntry) int { return strings.Compare(x.Target, y.Target) })
	return encode(&sorted)
}

// saveCache writes c to name unless the file already holds the same bytes, replacing it atomically so an interrupted run never leaves a half-written cache.
func saveCache(name string, c *cache) error {
	data, err := encodeCache(c)
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(name); err == nil && bytes.Equal(cur, data) {
		return nil
	}
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("assetbuild: save cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".assets-cache-*.yaml")
	if err != nil {
		return fmt.Errorf("assetbuild: save cache: %w", err)
	}
	_, werr := tmp.Write(data)
	merr := tmp.Chmod(0o644)
	cerr := tmp.Close()
	if err := errors.Join(werr, merr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("assetbuild: save cache: %w", err)
	}
	if err := os.Rename(tmp.Name(), name); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("assetbuild: save cache: %w", err)
	}
	return nil
}

// decodeStrict decodes the single YAML document in data into out, refusing unknown fields and trailing documents.
func decodeStrict(data []byte, out any) error {
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

// encode writes v as 2-space YAML.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("assetbuild: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("assetbuild: encode: %w", err)
	}
	return buf.Bytes(), nil
}
