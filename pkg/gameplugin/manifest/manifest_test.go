package manifest_test

import (
	"bytes"
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/gameplugin/manifest"
	"github.com/nsgo-dev/game/pkg/loaderr"
)

func validFullManifest() manifest.Manifest {
	return manifest.Manifest{
		ManifestVersion: manifest.ManifestVersion,
		Name:            "healthkit",
		Module:          "example.com/healthkit",
		Description:     "Health component, ward panel and admit tool",
		License:         "MIT",
		Authors:         []string{"Jane Doe"},
		Repository:      "https://github.com/example/healthkit",
		Engine:          ">=0.4.0 <0.5.0",
		API:             1,
		Platforms:       []string{"all"},
		Entries: manifest.Entries{
			Runtime: "example.com/healthkit/health",
			Editor:  "example.com/healthkit/healtheditor",
			CLI: []manifest.CLIEntry{
				{Name: "healthkit", Package: "example.com/healthkit/cmd/game-healthkit"},
			},
		},
		Requires: map[string]string{
			"example.com/navkit": "^0.3.0",
		},
		Conflicts: []string{"example.com/oldhealth"},
		Contributes: &manifest.Contributes{
			Components: []string{"health"},
			Editor: &manifest.EditorContributes{
				Panels:   []string{"ward"},
				Tools:    []string{"admit"},
				Commands: []string{"heal-all"},
			},
			AgentTools: []string{"plugin.healthkit.heal_all"},
		},
	}
}

func TestParseAndEncodeFullRoundTrip(t *testing.T) {
	want := validFullManifest()
	data, err := manifest.Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch:\ngot  = %#v\nwant = %#v", got, want)
	}
	again, err := manifest.Encode(got)
	if err != nil {
		t.Fatalf("Encode again: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("second encode differs:\nfirst:\n%s\nsecond:\n%s", data, again)
	}
}

func TestEncodeSetsDefaultVersionAndSortsByteOrder(t *testing.T) {
	authors := []string{"Zoe", "Ada"}
	platforms := []string{"windows", "android", "macos"}
	conflicts := []string{"example.com/p2", "example.com/p10", "example.com/p1"}
	cli := []manifest.CLIEntry{
		{Name: "zcmd", Package: "example.com/healthkit/cmd/game-zcmd"},
		{Name: "acmd", Package: "example.com/healthkit/cmd/game-acmd"},
	}
	m := manifest.Manifest{
		// ManifestVersion left 0: Encode fills ManifestVersion (1).
		Name:      "healthkit",
		Module:    "example.com/healthkit",
		Authors:   authors,
		Engine:    ">=0.4.0 <0.5.0",
		API:       1,
		Platforms: platforms,
		Entries: manifest.Entries{
			Runtime: "example.com/healthkit",
			CLI:     cli,
		},
		Requires: map[string]string{
			"example.com/m2":  "^1.0.0",
			"example.com/m10": "^1.0.0",
			"example.com/m1":  "^1.0.0",
		},
		Conflicts: conflicts,
		Contributes: &manifest.Contributes{
			Components: []string{"zcomp", "acomp"},
			Editor: &manifest.EditorContributes{
				Panels:   []string{"zpanel", "apanel"},
				Tools:    []string{"ztool", "atool"},
				Commands: []string{"zcmd", "acmd"},
			},
			AgentTools: []string{"plugin.healthkit.z", "plugin.healthkit.a"},
		},
	}

	out, err := manifest.Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Ensure caller's slices were not mutated.
	if !slices.Equal(authors, []string{"Zoe", "Ada"}) {
		t.Fatalf("Encode mutated Authors: %v", authors)
	}
	if !slices.Equal(platforms, []string{"windows", "android", "macos"}) {
		t.Fatalf("Encode mutated Platforms: %v", platforms)
	}
	if !slices.Equal(conflicts, []string{"example.com/p2", "example.com/p10", "example.com/p1"}) {
		t.Fatalf("Encode mutated Conflicts: %v", conflicts)
	}

	// Verify byte order (m1 < m10 < m2, not numeric sort m1 < m2 < m10).
	text := string(out)
	idxM1 := strings.Index(text, "example.com/m1:")
	idxM10 := strings.Index(text, "example.com/m10:")
	idxM2 := strings.Index(text, "example.com/m2:")
	if !(idxM1 > 0 && idxM1 < idxM10 && idxM10 < idxM2) {
		t.Fatalf("requires keys not in byte order (m1 < m10 < m2):\n%s", text)
	}

	got, err := manifest.Parse(out)
	if err != nil {
		t.Fatalf("Parse encoded output: %v\n%s", err, text)
	}
	if got.ManifestVersion != manifest.ManifestVersion {
		t.Fatalf("ManifestVersion = %d, want %d", got.ManifestVersion, manifest.ManifestVersion)
	}
	if !slices.Equal(got.Authors, []string{"Ada", "Zoe"}) {
		t.Fatalf("Authors = %v, want [Ada Zoe]", got.Authors)
	}
	if !slices.Equal(got.Platforms, []string{"android", "macos", "windows"}) {
		t.Fatalf("Platforms = %v, want [android macos windows]", got.Platforms)
	}
	if !slices.Equal(got.Conflicts, []string{"example.com/p1", "example.com/p10", "example.com/p2"}) {
		t.Fatalf("Conflicts = %v, want byte order", got.Conflicts)
	}
	if len(got.Entries.CLI) != 2 || got.Entries.CLI[0].Name != "acmd" || got.Entries.CLI[1].Name != "zcmd" {
		t.Fatalf("CLI = %+v, want sorted by Name", got.Entries.CLI)
	}
	if !slices.Equal(got.Contributes.Components, []string{"acomp", "zcomp"}) {
		t.Fatalf("Components = %v", got.Contributes.Components)
	}
	if !slices.Equal(got.Contributes.Editor.Panels, []string{"apanel", "zpanel"}) {
		t.Fatalf("Panels = %v", got.Contributes.Editor.Panels)
	}
	if !slices.Equal(got.Contributes.Editor.Tools, []string{"atool", "ztool"}) {
		t.Fatalf("Tools = %v", got.Contributes.Editor.Tools)
	}
	if !slices.Equal(got.Contributes.Editor.Commands, []string{"acmd", "zcmd"}) {
		t.Fatalf("Commands = %v", got.Contributes.Editor.Commands)
	}
	if !slices.Equal(got.Contributes.AgentTools, []string{"plugin.healthkit.a", "plugin.healthkit.z"}) {
		t.Fatalf("AgentTools = %v", got.Contributes.AgentTools)
	}
}

func TestEncodeOmitsEmptyContributes(t *testing.T) {
	m := manifest.Manifest{
		Name:   "healthkit",
		Module: "example.com/healthkit",
		Engine: ">=0.4.0 <0.5.0",
		API:    1,
		Entries: manifest.Entries{
			Editor: "example.com/healthkit/editor",
		},
		Contributes: &manifest.Contributes{
			Editor: &manifest.EditorContributes{},
		},
	}
	out, err := manifest.Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(out), "contributes") {
		t.Fatalf("Encode wrote empty contributes block:\n%s", out)
	}
}

func TestSupportsPlatform(t *testing.T) {
	omitted := manifest.Manifest{}
	if !omitted.SupportsPlatform("ios") || !omitted.SupportsPlatform("web") {
		t.Fatal("omitted platforms should support every valid platform")
	}
	if omitted.SupportsPlatform("unknown") {
		t.Fatal("SupportsPlatform accepted unknown platform")
	}

	all := manifest.Manifest{Platforms: []string{"all"}}
	for _, p := range manifest.Platforms {
		if !all.SupportsPlatform(p) {
			t.Errorf("[all] should support %q", p)
		}
	}

	desktop := manifest.Manifest{Platforms: []string{"macos", "linux"}}
	if !desktop.SupportsPlatform("macos") || !desktop.SupportsPlatform("linux") {
		t.Fatal("desktop manifest should support macos and linux")
	}
	if desktop.SupportsPlatform("ios") || desktop.SupportsPlatform("web") {
		t.Fatal("desktop manifest should not support ios or web")
	}
}

func TestParseFS(t *testing.T) {
	validYAML := "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0 <0.5.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n"
	badYAML := "manifestVersion: 1\nname: BadName\nmodule: example.com/healthkit\nengine: '>=0.4.0 <0.5.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n"

	fsys := fstest.MapFS{
		"plugin.yaml":            {Data: []byte(validYAML)},
		"sub/plugin.yaml":        {Data: []byte(validYAML)},
		"broken/plugin.yaml":     {Data: []byte(badYAML)},
		"other/something.txt":    {Data: []byte("hi")},
		"sub/nested/plugin.yaml": {Data: []byte(validYAML)},
	}

	for _, dir := range []string{"", ".", "sub", "sub/nested"} {
		m, err := manifest.ParseFS(fsys, dir)
		if err != nil {
			t.Fatalf("ParseFS(%q): %v", dir, err)
		}
		if m.Name != "healthkit" {
			t.Fatalf("ParseFS(%q).Name = %q, want healthkit", dir, m.Name)
		}
	}

	_, err := manifest.ParseFS(fsys, "broken")
	if !errors.Is(err, manifest.ErrInvalid) {
		t.Fatalf("ParseFS(broken) err = %v, want ErrInvalid", err)
	}
	le, ok := loaderr.As(err)
	if !ok {
		t.Fatalf("ParseFS(broken) err = %T, want *loaderr.Error", err)
	}
	if le.File != "broken/plugin.yaml" {
		t.Fatalf("loaderr.File = %q, want broken/plugin.yaml", le.File)
	}
	if le.Field != "name" {
		t.Fatalf("loaderr.Field = %q, want name", le.Field)
	}
	if le.Line != 2 {
		t.Fatalf("loaderr.Line = %d, want 2", le.Line)
	}
	if le.Fix == "" {
		t.Fatal("loaderr.Fix is empty")
	}

	if _, err := manifest.ParseFS(fsys, "other"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ParseFS(other) err = %v, want fs.ErrNotExist", err)
	}
	if _, err := manifest.ParseFS(nil, "."); !errors.Is(err, manifest.ErrInvalid) {
		t.Fatalf("ParseFS(nil) err = %v, want ErrInvalid", err)
	}
}

func TestParseRefusals(t *testing.T) {
	base := func(override string) string {
		return "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0 <0.5.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n" + override
	}

	cases := []struct {
		name      string
		yaml      string
		wantField string
		wantSub   string
	}{
		{
			name:    "empty file",
			yaml:    "   \n\t\n",
			wantSub: "empty manifest",
		},
		{
			name:    "non-mapping root",
			yaml:    "- item1\n- item2\n",
			wantSub: "!!seq",
		},
		{
			name:    "unknown top-level field",
			yaml:    base("unknownKey: true\n"),
			wantSub: "unknownKey",
		},
		{
			name:    "disallowed version field",
			yaml:    base("version: 1.0.0\n"),
			wantSub: "version",
		},
		{
			name:    "unknown nested entries field",
			yaml:    "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n  extra: foo\n",
			wantSub: "extra",
		},
		{
			name:    "unknown nested contributes field",
			yaml:    base("contributes:\n  extra: [a]\n"),
			wantSub: "extra",
		},
		{
			name:    "duplicate key",
			yaml:    base("name: other\n"),
			wantSub: "already defined",
		},
		{
			name:    "second YAML document",
			yaml:    base("---\nmanifestVersion: 1\n"),
			wantSub: "trailing YAML document",
		},
		{
			name:      "missing manifestVersion",
			yaml:      "name: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "manifestVersion",
			wantSub:   "manifestVersion",
		},
		{
			name:      "manifestVersion zero",
			yaml:      "manifestVersion: 0\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "manifestVersion",
			wantSub:   "manifestVersion",
		},
		{
			name:      "manifestVersion future",
			yaml:      "manifestVersion: 2\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "manifestVersion",
			wantSub:   "manifestVersion",
		},
		{
			name:      "manifestVersion float 1.0",
			yaml:      "manifestVersion: 1.0\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "manifestVersion",
			wantSub:   "manifestVersion",
		},
		{
			name:      "manifestVersion hex 0x1",
			yaml:      "manifestVersion: 0x1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "manifestVersion",
			wantSub:   "manifestVersion",
		},
		{
			name:      "manifestVersion string",
			yaml:      "manifestVersion: \"1\"\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "manifestVersion",
			wantSub:   "expected int",
		},
		{
			name:      "api float 1.0",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1.0\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "api",
			wantSub:   "api",
		},
		{
			name:      "api hex 0x1",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 0x1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "api",
			wantSub:   "api",
		},
		{
			name:      "api zero",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 0\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "api",
			wantSub:   "positive integer",
		},
		{
			name:      "api negative",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: -1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "api",
			wantSub:   "positive integer",
		},
		{
			name:      "invalid name uppercase",
			yaml:      "manifestVersion: 1\nname: HealthKit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "name",
			wantSub:   "HealthKit",
		},
		{
			name:      "invalid name leading hyphen",
			yaml:      "manifestVersion: 1\nname: -healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "name",
			wantSub:   "-healthkit",
		},
		{
			name:      "invalid name underscore",
			yaml:      "manifestVersion: 1\nname: health_kit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "name",
			wantSub:   "health_kit",
		},
		{
			name:      "invalid name too long",
			yaml:      "manifestVersion: 1\nname: " + strings.Repeat("a", 65) + "\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "name",
			wantSub:   "1 to 64",
		},
		{
			name:      "invalid module uppercase host",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: Example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: Example.com/healthkit/health\n",
			wantField: "module",
			wantSub:   "Example.com/healthkit",
		},
		{
			name:      "invalid module v1 suffix",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit/v1\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/v1\n",
			wantField: "module",
			wantSub:   "v1",
		},
		{
			name:      "invalid module traversal",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/../healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/../healthkit\n",
			wantField: "module",
			wantSub:   "..",
		},
		{
			name:      "multiline description",
			yaml:      base("description: \"line1\\nline2\"\n"),
			wantField: "description",
			wantSub:   "single line",
		},
		{
			name:      "invalid repository scheme",
			yaml:      base("repository: git://github.com/example/healthkit\n"),
			wantField: "repository",
			wantSub:   "http:// or https://",
		},
		{
			name:      "duplicate author",
			yaml:      base("authors: [Jane Doe, Jane Doe]\n"),
			wantField: "authors[1]",
			wantSub:   "listed twice",
		},
		{
			name:      "missing engine",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "engine",
			wantSub:   "required",
		},
		{
			name:      "bad engine constraint",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>==0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n",
			wantField: "engine",
			wantSub:   "invalid engine constraint",
		},
		{
			name:      "unknown platform",
			yaml:      base("platforms: [freebsd]\n"),
			wantField: "platforms[0]",
			wantSub:   "unknown platform",
		},
		{
			name:      "duplicate platform",
			yaml:      base("platforms: [macos, macos]\n"),
			wantField: "platforms[1]",
			wantSub:   "listed twice",
		},
		{
			name:      "all mixed with concrete platform",
			yaml:      base("platforms: [all, macos]\n"),
			wantField: "platforms",
			wantSub:   "cannot be combined",
		},
		{
			name:      "empty entries",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries: {}\n",
			wantField: "entries",
			wantSub:   "at least one entry",
		},
		{
			name:      "runtime entry outside module",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/other/health\n",
			wantField: "entries.runtime",
			wantSub:   "outside module",
		},
		{
			name:      "runtime entry prefix trap",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/healthkit2/health\n",
			wantField: "entries.runtime",
			wantSub:   "outside module",
		},
		{
			name:      "editor entry outside module",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  editor: example.com/other/editor\n",
			wantField: "entries.editor",
			wantSub:   "outside module",
		},
		{
			name:      "cli entry outside module",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  cli:\n    - name: healthkit\n      package: example.com/other/cmd/game-healthkit\n",
			wantField: "entries.cli[0].package",
			wantSub:   "outside module",
		},
		{
			name:      "cli entry invalid name",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  cli:\n    - name: HealthKit\n      package: example.com/healthkit/cmd/game-healthkit\n",
			wantField: "entries.cli[0].name",
			wantSub:   "HealthKit",
		},
		{
			name:      "cli entry duplicate name",
			yaml:      "manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0'\napi: 1\nentries:\n  cli:\n    - name: healthkit\n      package: example.com/healthkit/cmd/a\n    - name: healthkit\n      package: example.com/healthkit/cmd/b\n",
			wantField: "entries.cli[1].name",
			wantSub:   "listed twice",
		},
		{
			name:      "requires bad constraint",
			yaml:      base("requires:\n  example.com/navkit: 'not-a-version'\n"),
			wantField: `requires["example.com/navkit"]`,
			wantSub:   "constraint",
		},
		{
			name:      "requires empty constraint",
			yaml:      base("requires:\n  example.com/navkit: ''\n"),
			wantField: `requires["example.com/navkit"]`,
			wantSub:   "empty version constraint",
		},
		{
			name:      "requires self",
			yaml:      base("requires:\n  example.com/healthkit: '^1.0.0'\n"),
			wantField: `requires["example.com/healthkit"]`,
			wantSub:   "cannot require itself",
		},
		{
			name:      "requires invalid module key",
			yaml:      base("requires:\n  BadHost/navkit: '^1.0.0'\n"),
			wantField: "requires.BadHost/navkit",
			wantSub:   "invalid required module",
		},
		{
			name:      "self-conflict",
			yaml:      base("conflicts: [example.com/healthkit]\n"),
			wantField: "conflicts[0]",
			wantSub:   "cannot conflict with itself",
		},
		{
			name:      "duplicate conflict",
			yaml:      base("conflicts: [example.com/old, example.com/old]\n"),
			wantField: "conflicts[1]",
			wantSub:   "listed twice",
		},
		{
			name:      "requires and conflicts overlap",
			yaml:      base("requires:\n  example.com/navkit: '^0.3.0'\nconflicts: [example.com/navkit]\n"),
			wantField: "conflicts[0]",
			wantSub:   "both requires and conflicts",
		},
		{
			name:      "contributes duplicate component",
			yaml:      base("contributes:\n  components: [health, health]\n"),
			wantField: "contributes.components[1]",
			wantSub:   "twice",
		},
		{
			name:      "contributes whitespace agent tool",
			yaml:      base("contributes:\n  agentTools: ['plugin.healthkit.heal all']\n"),
			wantField: "contributes.agentTools[0]",
			wantSub:   "spaces or control characters",
		},
		{
			name:      "contributes duplicate editor command",
			yaml:      base("contributes:\n  editor:\n    commands: [heal-all, heal-all]\n"),
			wantField: "contributes.editor.commands[1]",
			wantSub:   "twice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manifest.Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Parse accepted invalid input:\n%s", tc.yaml)
			}
			if !errors.Is(err, manifest.ErrInvalid) {
				t.Fatalf("err = %v, want errors.Is(_, ErrInvalid)", err)
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("err = %T (%v), want *loaderr.Error", err, err)
			}
			if le.Fix == "" {
				t.Errorf("loaderr.Error has empty Fix: %v", le)
			}
			if tc.wantField != "" && le.Field != tc.wantField {
				t.Errorf("Field = %q, want %q (err: %v)", le.Field, tc.wantField, le)
			}
			if tc.wantSub != "" && !strings.Contains(le.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", le.Error(), tc.wantSub)
			}
		})
	}
}

func TestValidateModulePathEdgeCases(t *testing.T) {
	valid := []string{
		"example.com/healthkit",
		"github.com/nsgo-dev/game/examples/fieldmedic",
		"example.com/healthkit/v2",
		"example.com/healthkit/v10",
		"example.com/MyOrg/HealthKit",
		"localmod",
	}
	for _, p := range valid {
		if err := manifest.ValidateModulePath(p); err != nil {
			t.Errorf("ValidateModulePath(%q) = %v, want nil", p, err)
		}
	}

	invalid := []string{
		"",
		"/example.com/healthkit",
		"example.com/healthkit/",
		"example.com//healthkit",
		"example.com/./healthkit",
		"example.com/../healthkit",
		"example.com/.hidden",
		"example.com/trailing.",
		"-example.com/healthkit",
		"Example.com/healthkit",
		"example.com/health kit",
		"example.com/health!kit",
		"example.com/healthkit/v0",
		"example.com/healthkit/v1",
		"example.com/healthkit/v02",
		"example.com/CON/healthkit",
		"example.com/com1.txt/healthkit",
		"example.com/foo~1/healthkit",
	}
	for _, p := range invalid {
		if err := manifest.ValidateModulePath(p); err == nil {
			t.Errorf("ValidateModulePath(%q) = nil, want error", p)
		}
	}
}
