package main

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name                   string
		paths                  []string
		docs                   map[string]DocDelta
		full, mobile, docsOnly bool
	}{
		{"go", []string{"pkg/game.go"}, nil, true, false, false},
		{"module", []string{"go.mod"}, nil, true, false, false},
		{"template", []string{"template/starter/x.go"}, nil, true, false, false},
		{"workflow", []string{".github/workflows/test.yml"}, nil, true, false, false},
		{"docs", []string{"docs/en/editor.md", "docs/zh/editor.md"}, map[string]DocDelta{"docs/en/editor.md": {Before: "old prose", After: "new prose"}}, false, false, true},
		{"shell block", []string{"docs/en/getting-started.md"}, map[string]DocDelta{"docs/en/getting-started.md": {Before: "```sh\ngo tool game new x\n```", After: "```sh\ngo tool game new y\n```"}}, true, false, true},
		{"command", []string{"README.md"}, map[string]DocDelta{"README.md": {Before: "go tool game new old", After: "go tool game new new"}}, true, false, true},
		{"rename", []string{"docs/en/old.md", "docs/en/new.md"}, nil, false, false, true},
		{"unknown", []string{"some/new.file"}, nil, true, false, false},
		{"empty", nil, nil, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := classify(tc.paths, tc.docs)
			if r.Full != tc.full || r.Mobile != tc.mobile || r.Docs != tc.docsOnly {
				t.Fatalf("%+v", r)
			}
		})
	}
}
