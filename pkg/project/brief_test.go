package project_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
)

func TestBriefParseRejectsInvalidDocumentsWithLineNumbers(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		wantLine int
		wantText string
	}{
		{
			name:     "unknown field",
			yaml:     "version: 1\npitch: Cave Run\nextraField: nope\n",
			wantLine: 3,
			wantText: "extraField",
		},
		{
			name:     "missing version",
			yaml:     "pitch: Cave Run\n",
			wantLine: 0,
			wantText: "version",
		},
		{
			name:     "unsupported version",
			yaml:     "version: 2\npitch: Cave Run\n",
			wantLine: 1,
			wantText: "version",
		},
		{
			name:     "duplicate key",
			yaml:     "version: 1\npitch: First\npitch: Second\n",
			wantLine: 3,
			wantText: "duplicate key",
		},
		{
			name:     "yaml anchor",
			yaml:     "version: 1\npitch: &p Cave Run\n",
			wantLine: 2,
			wantText: "anchor",
		},
		{
			name:     "trailing document",
			yaml:     "version: 1\npitch: Cave Run\n---\nversion: 1\n",
			wantLine: 3,
			wantText: "trailing",
		},
		{
			name:     "non-canonical version",
			yaml:     "version: 01\npitch: Cave Run\n",
			wantLine: 1,
			wantText: "version",
		},
		{
			name:     "target missing bounds",
			yaml:     "version: 1\nfeel:\n  targets:\n    - metric: deaths\n",
			wantLine: 4,
			wantText: "min, max, or equal",
		},
		{
			name:     "target inverted bounds",
			yaml:     "version: 1\nfeel:\n  targets:\n    - metric: deaths\n      min: 5\n      max: 2\n",
			wantLine: 6,
			wantText: "less than min",
		},
		{
			name:     "target mixes equal and max",
			yaml:     "version: 1\nfeel:\n  targets:\n    - metric: deaths\n      equal: 3\n      max: 5\n",
			wantLine: 4,
			wantText: "cannot combine equal",
		},
		{
			name:     "duplicate genre",
			yaml:     "version: 1\ngenre: [action, Action]\n",
			wantLine: 2,
			wantText: "duplicate",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := project.ParseBrief([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("ParseBrief succeeded, want error containing %q", tc.wantText)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantText)
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("error %T is not a *loaderr.Error: %v", err, err)
			}
			if tc.wantLine > 0 && le.Line != tc.wantLine {
				t.Fatalf("line = %d, want %d (err: %v)", le.Line, tc.wantLine, err)
			}
		})
	}
}

func TestBriefRoundTripAndSaveLoad(t *testing.T) {
	b := &project.Brief{
		Version:      project.BriefVersion,
		Pitch:        "A short, tense dungeon platformer.",
		Genre:        []string{"2D platformer", "action"},
		Tags:         []string{"dungeon", "pixel-art"},
		Platforms:    []string{"desktop", "phones, touch", "web"},
		ArtDirection: "16 px pixel art, 4-colour shading.",
		Feel: &project.BriefFeel{
			Summary: "Snappy, forgiving jumps (coyote time).",
			Targets: []project.BriefPlaytestTarget{
				{Metric: "median_time", Scene: "floor_01", Summary: "under a minute", Unit: "s", Max: project.Float64(60)},
				{Metric: "deaths", Scene: "floor_03", Summary: "2–4 deaths", Min: project.Float64(2), Max: project.Float64(4)},
			},
		},
	}

	raw, err := project.EncodeBrief(b)
	if err != nil {
		t.Fatalf("EncodeBrief: %v", err)
	}
	if !strings.HasPrefix(string(raw), project.BriefSchemaComment+"\n") {
		t.Fatalf("missing schema comment header:\n%s", raw)
	}

	parsed, err := project.ParseBrief(raw)
	if err != nil {
		t.Fatalf("ParseBrief: %v", err)
	}
	again, err := project.EncodeBrief(parsed)
	if err != nil {
		t.Fatalf("re-EncodeBrief: %v", err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("round-trip mismatch:\n--- got ---\n%s\n--- want ---\n%s", again, raw)
	}

	// MissingSections reports only toneAndAudio as missing ("1 gap").
	if gaps := parsed.MissingSections(); !slices.Equal(gaps, []project.BriefSection{project.BriefSectionToneAndAudio}) {
		t.Fatalf("MissingSections = %v, want [toneAndAudio]", gaps)
	}
	parsed.ToneAndAudio = "Warm torch crackle and low stone reverb."
	if gaps := parsed.MissingSections(); len(gaps) != 0 {
		t.Fatalf("MissingSections after filling toneAndAudio = %v, want empty", gaps)
	}

	// SaveBrief + LoadBrief round-trip on disk.
	p := filepath.Join(t.TempDir(), project.BriefFileName)
	if err := project.SaveBrief(p, parsed); err != nil {
		t.Fatalf("SaveBrief: %v", err)
	}
	loaded, err := project.LoadBrief(p)
	if err != nil {
		t.Fatalf("LoadBrief: %v", err)
	}
	diskBytes, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	reEncoded, err := project.EncodeBrief(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(diskBytes, reEncoded) {
		t.Fatalf("disk round-trip differs:\n%s\nvs\n%s", reEncoded, diskBytes)
	}

	// Validate rejects NaN.
	bad := parsed.Clone()
	bad.Feel.Targets[0].Max = project.Float64(math.NaN())
	if err := project.ValidateBrief(bad); err == nil {
		t.Fatal("ValidateBrief accepted NaN")
	}
}

func TestBriefTargetsForScenePrecedence(t *testing.T) {
	b := &project.Brief{
		Version: project.BriefVersion,
		Feel: &project.BriefFeel{
			Targets: []project.BriefPlaytestTarget{
				{Metric: "deaths", Summary: "global default", Max: project.Float64(5)},
				{Metric: "deaths", Scene: "floor_03", Summary: "floor 3 range", Min: project.Float64(2), Max: project.Float64(4)},
				{Metric: "median_time", Scene: "floor_01", Summary: "under a minute", Unit: "s", Max: project.Float64(60)},
			},
		},
	}
	floor3 := b.TargetsForScene("floor_03")
	if len(floor3) != 1 || floor3[0].Metric != "deaths" || floor3[0].Scene != "floor_03" || *floor3[0].Min != 2 {
		t.Fatalf("floor_03 targets = %+v", floor3)
	}
	floor2 := b.TargetsForScene("floor_02")
	if len(floor2) != 1 || floor2[0].Metric != "deaths" || floor2[0].Scene != "" || *floor2[0].Max != 5 {
		t.Fatalf("floor_02 targets = %+v", floor2)
	}
	all := b.TargetsForScene("")
	if len(all) != 3 {
		t.Fatalf("all targets len = %d, want 3", len(all))
	}
}

func TestBriefBasisAndChangesRoundTripAndRejectUnknownBasis(t *testing.T) {
	b := &project.Brief{Version: project.BriefVersion, Basis: "assumption", Pitch: "p", Feel: &project.BriefFeel{Basis: "target", Targets: []project.BriefPlaytestTarget{{Metric: "finished", Basis: "locked", Equal: project.Float64(1)}}}, Changes: []project.BriefChange{{Date: "2026-11-28", Field: "feel.targets.finished", From: "0", To: "1", Reason: "acceptance"}}}
	raw, err := project.EncodeBrief(b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := project.ParseBrief(raw)
	if err != nil || got.Basis != "assumption" || got.Feel.Targets[0].Basis != "locked" || len(got.Changes) != 1 {
		t.Fatalf("round trip = %+v, err=%v", got, err)
	}
	_, err = project.ParseBrief([]byte("version: 1\nbasis: maybe\n"))
	if err == nil || !strings.Contains(err.Error(), "basis") {
		t.Fatalf("unknown basis error = %v", err)
	}
}

func TestBriefLoadRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, project.BriefFileName)
	huge := "version: 1\npitch: " + strings.Repeat("a", (1<<20)+64) + "\n"
	if err := os.WriteFile(p, []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := project.LoadBrief(p); err == nil {
		t.Fatal("LoadBrief accepted file > 1 MiB")
	}
	if _, err := project.ReadBriefFS(os.DirFS(dir)); err == nil {
		t.Fatal("ReadBriefFS accepted file > 1 MiB")
	}
}

func TestBriefArtReferencesRoundTripAndValidation(t *testing.T) {
	b := &project.Brief{
		Version: project.BriefVersion,
		Pitch:   "Dungeon run",
		Art: &project.BriefArt{
			Direction: "chunky pixel art, 16 px tiles",
			References: []project.BriefArtReference{
				{Path: "art/reference/torch_ref.png"},
				{Path: "art/reference/spikes_ref.png", Note: "rusty iron spikes, three per tile"},
			},
		},
	}
	raw, err := project.EncodeBrief(b)
	if err != nil {
		t.Fatalf("EncodeBrief: %v", err)
	}
	parsed, err := project.ParseBrief(raw)
	if err != nil {
		t.Fatalf("ParseBrief: %v", err)
	}
	again, err := project.EncodeBrief(parsed)
	if err != nil {
		t.Fatalf("re-EncodeBrief: %v", err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("round-trip mismatch:\n%s\nvs\n%s", again, raw)
	}
	if slices.Contains(parsed.MissingSections(), project.BriefSectionArtDirection) {
		t.Fatalf("artDirection reported missing despite art.direction/references: %v", parsed.MissingSections())
	}

	invalid := []struct {
		name string
		yaml string
	}{
		{"traversal", "version: 1\nart:\n  references:\n    - path: ../escape.png\n"},
		{"leading slash", "version: 1\nart:\n  references:\n    - path: /etc/passwd\n"},
		{"backslash", "version: 1\nart:\n  references:\n    - path: art\\reference\\a.png\n"},
		{"duplicate", "version: 1\nart:\n  references:\n    - path: art/reference/a.png\n    - path: art/reference/a.png\n"},
		{"note newline", "version: 1\nart:\n  references:\n    - path: art/reference/a.png\n      note: \"line1\\nline2\"\n"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := project.ParseBrief([]byte(tc.yaml)); err == nil {
				t.Fatalf("ParseBrief accepted invalid %s", tc.name)
			}
		})
	}
}
