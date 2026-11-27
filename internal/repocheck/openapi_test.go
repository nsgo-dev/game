package repocheck_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestOpenAPISpecsPublished verifies that each service's embedded OpenAPI 3.1 description is published byte-for-byte under website/public/schemas/ and linked from both the English and Chinese service guides.
func TestOpenAPISpecsPublished(t *testing.T) {
	specs := []struct {
		source    string
		published string
		url       string
		docs      []string
	}{
		{
			source:    "pkg/online/cloudsave/openapi.yaml",
			published: "website/public/schemas/cloudsave-openapi.v1.yaml",
			url:       "https://game.nsgo.dev/schemas/cloudsave-openapi.v1.yaml",
			docs:      []string{"docs/en/cloudsave.md", "docs/zh/cloudsave.md"},
		},
		{
			source:    "pkg/online/leaderboard/selfhost/openapi.yaml",
			published: "website/public/schemas/leaderboard-selfhost-openapi.v1.yaml",
			url:       "https://game.nsgo.dev/schemas/leaderboard-selfhost-openapi.v1.yaml",
			docs:      []string{"docs/en/leaderboard-selfhost.md", "docs/zh/leaderboard-selfhost.md"},
		},
	}

	for _, s := range specs {
		want, err := os.ReadFile(s.source)
		if err != nil {
			t.Fatalf("read %s: %v", s.source, err)
		}
		got, err := os.ReadFile(s.published)
		if err != nil {
			t.Fatalf("read %s: %v", s.published, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from %s; copy %s to %s", s.published, s.source, s.source, s.published)
		}
		for _, docPath := range s.docs {
			doc, err := os.ReadFile(docPath)
			if err != nil {
				t.Fatalf("read %s: %v", docPath, err)
			}
			text := string(doc)
			if !strings.Contains(text, s.url) {
				t.Errorf("%s does not link %s", docPath, s.url)
			}
			if !strings.Contains(text, "/v1/openapi.yaml") {
				t.Errorf("%s does not document GET /v1/openapi.yaml", docPath)
			}
		}
	}
}
