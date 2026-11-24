package formattest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// team is the document of the test formats: a version, a name and a map that a careless encoder writes in Go's random order.
type team struct {
	Version int            `yaml:"version"`
	Name    string         `yaml:"name"`
	Scores  map[string]int `yaml:"scores"`
}

// decodeTeam is a conforming loader: unknown fields refused, version required and equal to 1.
func decodeTeam(data []byte) (team, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var tm team
	if err := dec.Decode(&tm); err != nil {
		return team{}, err
	}
	if tm.Version != 1 {
		return team{}, fmt.Errorf("unsupported version %d (want 1)", tm.Version)
	}
	return tm, nil
}

// encodeTeam is a conforming saver: yaml.Marshal sorts map keys.
func encodeTeam(tm team) ([]byte, error) { return yaml.Marshal(tm) }

// conforming is the format every check passes.
func conforming() Format[team] {
	return Format[team]{Decode: decodeTeam, Encode: encodeTeam, Files: []string{"testdata/*.yaml"}}
}

// failures runs every check of f and returns the names of those that fail, without the file name.
func failures(t *testing.T, f Format[team]) []string {
	t.Helper()
	checks, err := f.checks(false)
	if err != nil {
		t.Fatal(err)
	}
	var failed []string
	for _, c := range checks {
		if err := c.run(); err != nil {
			failed = append(failed, c.name[strings.LastIndex(c.name, "/")+1:])
		}
	}
	return failed
}

func TestConformingFormatPasses(t *testing.T) {
	Run(t, conforming())
	if got := failures(t, conforming()); got != nil {
		t.Errorf("failed checks = %v, want none", got)
	}
}

func TestNondeterministicFormatsFail(t *testing.T) {
	tests := []struct {
		name   string
		encode func(team) ([]byte, error)
		want   []string
	}{
		{
			// A save counter stands for a timestamp or random ID: the bytes differ on every save.
			name: "save counter",
			encode: func() func(team) ([]byte, error) {
				n := 0
				return func(tm team) ([]byte, error) {
					n++
					out, err := encodeTeam(tm)
					return append(out, fmt.Sprintf("# save %d\n", n)...), err
				}
			}(),
			want: []string{"round_trip", "deterministic"},
		},
		{
			// Twenty keys in Go's random map order: eight saves in the same order are as good as impossible.
			name: "map order",
			encode: func(tm team) ([]byte, error) {
				var b strings.Builder
				fmt.Fprintf(&b, "version: %d\nname: %s\nscores:\n", tm.Version, tm.Name)
				for k, v := range tm.Scores {
					fmt.Fprintf(&b, "    %s: %d\n", k, v)
				}
				return []byte(b.String()), nil
			},
			want: []string{"round_trip", "deterministic"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := conforming()
			f.Encode = tt.encode
			if got := failures(t, f); !slices.Equal(got, tt.want) {
				t.Errorf("failed checks = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLenientLoadersFail(t *testing.T) {
	tests := []struct {
		name   string
		decode func([]byte) (team, error)
		want   []string
	}{
		{
			name: "unknown fields ignored",
			decode: func(data []byte) (team, error) {
				var tm team
				if err := yaml.Unmarshal(data, &tm); err != nil {
					return team{}, err
				}
				if tm.Version != 1 {
					return team{}, errors.New("unsupported version")
				}
				return tm, nil
			},
			want: []string{"unknown_field"},
		},
		{
			name: "missing version read as 1",
			decode: func(data []byte) (team, error) {
				var tm team
				err := yaml.Unmarshal(data, &tm)
				if tm.Version == 0 {
					tm.Version = 1
				}
				if err == nil && bytes.Contains(data, []byte(unknownField)) {
					err = errors.New("unknown field " + unknownField)
				}
				if err == nil && tm.Version != 1 {
					err = errors.New("unsupported version")
				}
				return tm, err
			},
			want: []string{"missing_version", "zero_version"},
		},
		{
			name: "any version accepted",
			decode: func(data []byte) (team, error) {
				var probe team
				if yaml.Unmarshal(data, &probe) == nil && probe.Version > 1 {
					return probe, nil
				}
				return decodeTeam(data)
			},
			want: []string{"future_version"},
		},
		{
			name: "errors that name no field",
			decode: func(data []byte) (team, error) {
				tm, err := decodeTeam(data)
				if err != nil {
					return team{}, errors.New("bad file")
				}
				return tm, nil
			},
			want: []string{"unknown_field", "missing_version", "zero_version", "future_version"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := conforming()
			f.Decode = tt.decode
			if got := failures(t, f); !slices.Equal(got, tt.want) {
				t.Errorf("failed checks = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNonCanonicalTestdataFailsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile("testdata/team.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Two-space indentation loads to the same value but is not what encodeTeam writes.
	loose := bytes.ReplaceAll(data, []byte("\n    "), []byte("\n  "))
	file := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(file, loose, 0o644); err != nil {
		t.Fatal(err)
	}
	f := conforming()
	f.Files = []string{filepath.Join(dir, "*.yaml")}
	if got, want := failures(t, f), []string{"round_trip"}; !slices.Equal(got, want) {
		t.Errorf("failed checks = %v, want %v", got, want)
	}

	// Update rewrites the file in canonical form, after which every check passes.
	if _, err := f.checks(true); err != nil {
		t.Fatal(err)
	}
	rewritten, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rewritten, data) {
		t.Errorf("update wrote\n%s\nwant\n%s", rewritten, data)
	}
	if got := failures(t, f); got != nil {
		t.Errorf("failed checks after update = %v, want none", got)
	}
}

func TestVersionField(t *testing.T) {
	// A format whose schema version is under another key, as a mod manifest's manifestVersion is.
	type manifest struct {
		ManifestVersion int    `yaml:"manifestVersion"`
		Version         string `yaml:"version"`
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "mod.yaml")
	if err := os.WriteFile(file, []byte("manifestVersion: 1\nversion: 2.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := Format[manifest]{
		Decode: func(data []byte) (manifest, error) {
			dec := yaml.NewDecoder(bytes.NewReader(data))
			dec.KnownFields(true)
			var m manifest
			if err := dec.Decode(&m); err != nil {
				return manifest{}, err
			}
			if m.ManifestVersion != 1 {
				return manifest{}, fmt.Errorf("unsupported manifestVersion %d", m.ManifestVersion)
			}
			return m, nil
		},
		Encode:       func(m manifest) ([]byte, error) { return yaml.Marshal(m) },
		Files:        []string{file},
		VersionField: "manifestVersion",
	}
	Run(t, f)

	// The same format checked against version, a string, cannot be.
	f.VersionField = ""
	checks, err := f.checks(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if strings.HasSuffix(c.name, "/future_version") {
			if err := c.run(); err == nil || !strings.Contains(err.Error(), "not an integer") {
				t.Errorf("future_version with a string version = %v, want an error that it is not an integer", err)
			}
		}
	}
}

func TestMaxVersion(t *testing.T) {
	// A format supporting versions 1 and 2 checks its version 1 testdata against version 3.
	f := conforming()
	f.Decode = func(data []byte) (team, error) {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		var tm team
		if err := dec.Decode(&tm); err != nil {
			return team{}, err
		}
		if tm.Version != 1 && tm.Version != 2 {
			return team{}, fmt.Errorf("unsupported version %d (want 1 or 2)", tm.Version)
		}
		return tm, nil
	}
	if got, want := failures(t, f), []string{"future_version"}; !slices.Equal(got, want) {
		t.Errorf("without MaxVersion, failed checks = %v, want %v", got, want)
	}
	f.MaxVersion = 2
	if got := failures(t, f); got != nil {
		t.Errorf("with MaxVersion=2, failed checks = %v, want none", got)
	}
}

func TestFormatErrors(t *testing.T) {
	f := conforming()
	f.Files = []string{"testdata/*.json"}
	if _, err := f.checks(false); err == nil || !strings.Contains(err.Error(), "match no testdata file") {
		t.Errorf("checks with no files = %v, want an error naming the patterns", err)
	}
	f = conforming()
	f.Encode = nil
	if _, err := f.checks(false); err == nil {
		t.Error("checks without Encode = nil error, want one")
	}
}
