package editormcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/game/skills"
)

// Every skill resource reads back the embedded bytes as Markdown.
func TestSkillResourcesReturnTheEmbeddedFiles(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)
	files := skills.Files()
	if len(files) == 0 {
		t.Fatal("skills.Files() is empty")
	}
	for _, rel := range files {
		want, err := skills.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}
		uri := editormcp.SkillURIPrefix + rel
		res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("ReadResource(%s): %v", uri, err)
		}
		if len(res.Contents) != 1 || res.Contents[0].URI != uri || res.Contents[0].MIMEType != "text/markdown" || res.Contents[0].Text != string(want) {
			t.Fatalf("ReadResource(%s) = %d contents, first %q %q, %d bytes; want the %d embedded bytes", uri, len(res.Contents), res.Contents[0].URI, res.Contents[0].MIMEType, len(res.Contents[0].Text), len(want))
		}
	}
	if _, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.SkillURIPrefix + "references/missing.md"}); err == nil {
		t.Fatal("ReadResource of a file the skill does not have succeeded")
	}
}

// skill_get returns one section by its exact heading, skips # lines inside fenced code, and refuses an unknown file, an unknown section, an unsafe path and an unknown argument.
func TestSkillGet(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	res := call(t, cs, editormcp.ToolSkillGet, map[string]any{"file": "SKILL.md", "section": "Core rules"})
	got := text(res)
	if res.IsError || !strings.HasPrefix(got, "## Core rules") || strings.Contains(got, "## Quick-start workflow") {
		t.Fatalf("skill_get SKILL.md Core rules = error:%v text:%q", res.IsError, got)
	}

	// The section's fenced block holds "# One-time client setup", which must not end it.
	res = call(t, cs, editormcp.ToolSkillGet, map[string]any{"file": "nsgo-game/references/mcp-tools.md", "section": "Connecting an MCP client"})
	got = text(res)
	if res.IsError || !strings.Contains(got, "# Start the game with automation enabled") || strings.Contains(got, "## Wiring") {
		t.Fatalf("skill_get mcp-tools.md Connecting an MCP client = error:%v text:%q", res.IsError, got)
	}

	// A section reaches past deeper headings to the next one of its own level.
	res = call(t, cs, editormcp.ToolSkillGet, map[string]any{"file": editormcp.SkillURIPrefix + "references/mcp-tools.md", "section": "Tool catalog"})
	got = text(res)
	if res.IsError || !strings.Contains(got, "### 10. Logs and recovered panics") || strings.Contains(got, "## Recipe: check your change") {
		t.Fatalf("skill_get mcp-tools.md Tool catalog = error:%v text:%q", res.IsError, got)
	}

	for _, args := range []map[string]any{
		{"file": "SKILL.md", "section": "Missing"},
		{"file": "SKILL.md", "section": "core rules"},
		{"file": "SKILL.md", "section": ""},
		{"file": "MISSING.md", "section": "Core rules"},
		{"file": "../go.mod", "section": "module"},
		{"file": "/SKILL.md", "section": "Core rules"},
		{"file": "SKILL.md", "section": "Core rules", "extra": true},
	} {
		if res := call(t, cs, editormcp.ToolSkillGet, args); !res.IsError {
			t.Errorf("skill_get %v accepted: %q", args, text(res))
		}
	}
}

// The proxy lists and reads the skill from its own copy while no editor has the project open.
func TestProxyServesTheSkillWithoutAnEditor(t *testing.T) {
	endpointDir(t)
	dir := t.TempDir()
	writeProject(t, dir)
	cs := startProxy(t, mustRoot(t, dir), nil, "")
	listed := map[string]bool{}
	for r, err := range cs.Resources(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		listed[r.URI] = true
	}
	for _, rel := range skills.Files() {
		uri := editormcp.SkillURIPrefix + rel
		if !listed[uri] {
			t.Errorf("proxy does not list %s", uri)
		}
	}
	want, err := skills.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.SkillURIPrefix + "SKILL.md"})
	if err != nil || len(res.Contents) != 1 || res.Contents[0].Text != string(want) {
		t.Fatalf("proxy ReadResource(SKILL.md) = %v, %+v", err, res)
	}
}
