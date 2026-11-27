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
	f := conforming()
	checks, err := f.checks(false)
	if err != nil {
		t.Fatal(err)
	}
	runChecks(t, checks)
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
	checks, err := f.checks(false)
	if err != nil {
		t.Fatal(err)
	}
	runChecks(t, checks)

	// The same format checked against version, a string, cannot be.
	f.VersionField = ""
	checks, err = f.checks(false)
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

// teamPage is a reference page with an entry for the test format, whose example is in canonical form, and one for another format after it.
const teamPage = `# File formats

<!-- format: internal/formattest -->
### Team

` + "```yaml" + `
version: 1
name: red
scores:
    a: 1
` + "```" + `

<!-- format: internal/formattest#other -->
### Other

` + "```yaml" + `
not: a team
` + "```" + `
`

// docFailures returns the names of the failing doc checks of f on the page data, without the page name, and each failure's message.
func docFailures(t *testing.T, f Format[team], data string) ([]string, []string) {
	t.Helper()
	checks, err := f.docChecks("internal/formattest", []page{{name: "docs/en/formats.md", data: []byte(data)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) == 0 {
		t.Fatal("no doc checks")
	}
	var names, msgs []string
	for _, c := range checks {
		if err := c.run(); err != nil {
			names = append(names, c.name[strings.LastIndex(c.name, "/")+1:])
			msgs = append(msgs, err.Error())
		}
	}
	return names, msgs
}

func TestDocEntryPasses(t *testing.T) {
	f := conforming()
	f.Doc = "internal/formattest"
	checks, err := f.docChecks("internal/formattest", []page{{name: "docs/en/formats.md", data: []byte(teamPage)}})
	if err != nil {
		t.Fatal(err)
	}
	// The entry ends at the next marker, so the other format's example is not checked against this one.
	if len(checks) != 1 || checks[0].name != "docs/en/formats.md/example_1" {
		t.Fatalf("checks = %v, want one example check", checks)
	}
	runChecks(t, checks)
}

func TestDocEntryFailures(t *testing.T) {
	tests := []struct {
		name  string
		page  string
		want  string
		words []string
	}{
		{
			name:  "no entry",
			page:  strings.ReplaceAll(teamPage, "<!-- format: internal/formattest -->", ""),
			want:  "entry",
			words: []string{"no entry for internal/formattest", "<!-- format: internal/formattest -->"},
		},
		{
			name:  "marked twice",
			page:  teamPage + "\n<!-- format: internal/formattest -->\n",
			want:  "entry",
			words: []string{"marked twice", "lines 3 and"},
		},
		{
			name:  "no YAML example",
			page:  strings.Replace(teamPage, "```yaml", "```json", 1),
			want:  "entry",
			words: []string{"has no YAML example"},
		},
		{
			name:  "example ends at the next level-2 heading",
			page:  strings.Replace(teamPage, "### Team\n", "### Team\n\n## Programs\n", 1),
			want:  "entry",
			words: []string{"has no YAML example"},
		},
		{
			name:  "non-canonical example",
			page:  strings.Replace(teamPage, "    a: 1", "  a: 1", 1),
			want:  "example_1",
			words: []string{"docs/en/formats.md:7:", "changes when loaded and saved", "    a: 1"},
		},
		{
			name:  "example the format refuses",
			page:  strings.Replace(teamPage, "version: 1\nname: red", "version: 1\nname: red\ncolour: red", 1),
			want:  "example_1",
			words: []string{"does not load", "colour"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := conforming()
			f.Doc = "internal/formattest"
			names, msgs := docFailures(t, f, tt.page)
			if !slices.Equal(names, []string{tt.want}) {
				t.Fatalf("failed checks = %v, want [%s]", names, tt.want)
			}
			for _, w := range tt.words {
				if !strings.Contains(msgs[0], w) {
					t.Errorf("message %q does not contain %q", msgs[0], w)
				}
			}
		})
	}
}

func TestDocCodeBlocksAreSkipped(t *testing.T) {
	// A heading or marker inside another code block does not end the entry, and only yaml blocks are examples.
	page := strings.Replace(teamPage, "### Team\n", "### Team\n\n```go\n## not a heading\n<!-- format: x -->\n```\n", 1)
	f := conforming()
	f.Doc = "internal/formattest"
	if names, msgs := docFailures(t, f, page); names != nil {
		t.Errorf("failed checks = %v (%v), want none", names, msgs)
	}
}

func TestDocNames(t *testing.T) {
	f := conforming()
	pages := []page{{name: "docs/en/formats.md", data: []byte(teamPage)}}
	if _, err := f.docChecks("internal/formattest", pages); err == nil || !strings.Contains(err.Error(), "Format needs Doc") {
		t.Errorf("docChecks without Doc = %v, want an error asking for it", err)
	}
	f.Doc = "quest"
	if _, err := f.docChecks("internal/formattest", pages); err == nil || !strings.Contains(err.Error(), `start Doc with "internal/formattest"`) {
		t.Errorf("docChecks with another package's Doc = %v, want an error naming the package", err)
	}
	f.Doc = "internal/formattest#other"
	checks, err := f.docChecks("internal/formattest", pages)
	if err != nil {
		t.Fatal(err)
	}
	// The other entry's example is not a team, so it fails to load.
	if len(checks) != 1 || checks[0].run() == nil {
		t.Errorf("checks of the other entry = %d, want one that fails", len(checks))
	}
}

func TestDocPagesFromPackageDir(t *testing.T) {
	pkg, pages, err := docPages()
	if err != nil {
		t.Fatal(err)
	}
	if pkg != "internal/formattest" {
		t.Errorf("package = %q, want internal/formattest", pkg)
	}
	if len(pages) != len(DocPages) || pages[0].name != "docs/en/formats.md" || pages[1].name != "docs/zh/formats.md" {
		t.Errorf("pages = %d, want docs/en/formats.md and docs/zh/formats.md", len(pages))
	}
}

func TestParseEntries(t *testing.T) {
	const fence = "```"
	page := "# Formats\n\n<!-- format: a -->\n### A\n\n" + fence + "yaml \n" + "x: 1\n" + fence + "  \n\n" + fence + "json\n{}\n" + fence + "\n\n" + fence + "go\n<!-- format: a -->\n<!-- format: quoted -->\n## not a heading\n" + fence + "\n\n## Next\n\n" + fence + "yaml\nlost: 1\n" + fence + "\n\n<!-- format: b#c -->\n" + fence + "yaml title=\"b\"\ny: 2\n" + fence + "\n"
	entries, err := ParseEntries([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{ID: "a", Line: 3, Blocks: []Block{{Lang: "yaml", Data: []byte("x: 1\n"), Line: 7}, {Lang: "json", Data: []byte("{}\n"), Line: 11}, {Lang: "go", Data: []byte("<!-- format: a -->\n<!-- format: quoted -->\n## not a heading\n"), Line: 15}}},
		{ID: "b#c", Line: 26, Blocks: []Block{{Lang: "yaml", Data: []byte("y: 2\n"), Line: 28}}},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
	for i, e := range entries {
		w := want[i]
		if e.ID != w.ID || e.Line != w.Line || len(e.Blocks) != len(w.Blocks) {
			t.Fatalf("entry %d = %+v, want %+v", i, e, w)
		}
		for j, b := range e.Blocks {
			if b.Lang != w.Blocks[j].Lang || !bytes.Equal(b.Data, w.Blocks[j].Data) || b.Line != w.Blocks[j].Line {
				t.Errorf("entry %s block %d = %q %q line %d, want %q %q line %d", e.ID, j, b.Lang, b.Data, b.Line, w.Blocks[j].Lang, w.Blocks[j].Data, w.Blocks[j].Line)
			}
		}
	}

	crlf := strings.ReplaceAll(page, "\n", "\r\n")
	entries, err = ParseEntries([]byte(crlf))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !bytes.Equal(entries[0].Blocks[0].Data, []byte("x: 1\n")) {
		t.Errorf("CRLF page entries = %+v, want the same entries without carriage returns", entries)
	}
}

func TestParseEntriesFailures(t *testing.T) {
	const fence = "```"
	tests := []struct {
		name, page string
		words      []string
	}{
		{"indented marker", "  <!-- format: a -->\n", []string{"line 1:", "malformed entry marker", "start of the line"}},
		{"no space", "<!--format: a -->\n", []string{"line 1:", "malformed entry marker"}},
		{"no closing", "<!-- format: a\n", []string{"malformed entry marker"}},
		{"space in ID", "<!-- format: a b -->\n", []string{"malformed entry marker"}},
		{"marked twice", "<!-- format: a -->\n\n<!-- format: a -->\n", []string{"marked twice", "lines 1 and 3"}},
		{"yml", "<!-- format: a -->\n" + fence + "yml\nx: 1\n" + fence + "\n", []string{"line 2:", "write ```yaml"}},
		{"unclosed", "<!-- format: a -->\n" + fence + "yaml\nx: 1\n", []string{"line 2:", "not closed"}},
		{"closing fence with text", "<!-- format: a -->\n" + fence + "yaml\nx: 1\n" + fence + " x\n", []string{"line 2:", "not closed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEntries([]byte(tt.page))
			if err == nil {
				t.Fatal("ParseEntries succeeded, want an error")
			}
			for _, w := range tt.words {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}
