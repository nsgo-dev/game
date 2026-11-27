package editor

import (
	"path/filepath"
	"testing"
)

func TestParseFileLine(t *testing.T) {
	projDir := filepath.Join(t.TempDir(), "mygame")
	absScene := filepath.Join(projDir, "scenes", "boss.scene.yaml")

	cases := []struct {
		name     string
		text     string
		dir      string
		wantFile string
		wantLine int
		wantOK   bool
	}{
		{
			name:     "relative scene file:line:col",
			text:     "scenes/room.scene.yaml:12:4: sceneformat: unknown field",
			dir:      projDir,
			wantFile: "scenes/room.scene.yaml",
			wantLine: 12,
			wantOK:   true,
		},
		{
			name:     "prefixed with timestamp and Error label",
			text:     "2026/11/25 14:07:28 Error: scenes/other.scene.yaml:7: missing frame",
			dir:      projDir,
			wantFile: "scenes/other.scene.yaml",
			wantLine: 7,
			wantOK:   true,
		},
		{
			name:     "absolute path inside project",
			text:     "panic at " + absScene + ":23 +0x10",
			dir:      projDir,
			wantFile: "scenes/boss.scene.yaml",
			wantLine: 23,
			wantOK:   true,
		},
		{
			name:     "parenthesized file:line",
			text:     "check (game.yaml:5) for invalid audio bus",
			dir:      projDir,
			wantFile: "game.yaml",
			wantLine: 5,
			wantOK:   true,
		},
		{
			name:     "windows path with drive letter",
			text:     `C:\proj\scenes\room.scene.yaml:9: error`,
			dir:      "",
			wantFile: "C:/proj/scenes/room.scene.yaml",
			wantLine: 9,
			wantOK:   true,
		},
		{
			name:   "ignores URLs and timestamps",
			text:   "14:07:28 listening on https://example.com:8443/api asset:hero.png:1",
			dir:    projDir,
			wantOK: false,
		},
		{
			name:   "ignores zero or negative lines",
			text:   "scenes/room.scene.yaml:0: bad",
			dir:    projDir,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, line, ok := parseFileLine(tc.text, tc.dir)
			if ok != tc.wantOK || file != tc.wantFile || line != tc.wantLine {
				t.Fatalf("parseFileLine(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tc.text, file, line, ok, tc.wantFile, tc.wantLine, tc.wantOK)
			}
		})
	}
}
