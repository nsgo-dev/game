package editormcp

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/skills"
)

// SkillURIPrefix starts the URI of every file of the embedded nsgo-game agent skill, such as skill://nsgo-game/SKILL.md and skill://nsgo-game/references/mcp-tools.md.
const SkillURIPrefix = "skill://nsgo-game/"

// skillResources are the embedded skill's files as text/markdown resources, which the editor's server and the proxy of go tool game mcp both serve from the copy built into their binary.
func skillResources() []*mcp.Resource {
	var out []*mcp.Resource
	for _, rel := range skills.Files() {
		out = append(out, &mcp.Resource{
			URI:         SkillURIPrefix + rel,
			Name:        "nsgo-game/" + rel,
			Title:       "nsgo-game skill: " + rel,
			Description: "The nsgo-game agent skill file " + rel + " in the version this binary was built with. It wins over an older locally installed copy of the skill.",
			MIMEType:    "text/markdown",
		})
	}
	return out
}

// readSkillResource serves a skill:// resource from the embedded copy.
func readSkillResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	rel, ok := strings.CutPrefix(uri, SkillURIPrefix)
	if !ok {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	data, err := skills.ReadFile(rel)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: string(data)}}}, nil
}

// skillGet is the skill_get tool: it returns one heading section of an embedded skill file, for clients that cannot read resources.
func skillGet(_ context.Context, c *Call) (*Result, error) {
	var args struct {
		File    string `json:"file"`
		Section string `json:"section"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("skill_get: %w", err)
	}
	file := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(args.File), SkillURIPrefix), "nsgo-game/")
	if file == "" || !fs.ValidPath(file) {
		return nil, fmt.Errorf("skill_get: invalid file %q; pass a path such as SKILL.md or references/mcp-tools.md", args.File)
	}
	data, err := skills.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("skill_get: unknown file %q; the skill has %s", args.File, strings.Join(skills.Files(), ", "))
	}
	section := strings.TrimSpace(args.Section)
	if section == "" {
		return nil, fmt.Errorf("skill_get: section is required")
	}
	text, ok := markdownSection(string(data), section)
	if !ok {
		return nil, fmt.Errorf("skill_get: unknown section %q in %s; pass a heading's text exactly as written", section, file)
	}
	return &Result{Text: text, Structured: map[string]any{"file": file, "section": section, "uri": SkillURIPrefix + file}}, nil
}

// markdownSection returns the ATX heading whose text is title and everything up to the next heading of the same or a higher level, ignoring lines inside fenced code blocks, where a shell comment starts with # too.
func markdownSection(doc, title string) (string, bool) {
	lines := strings.Split(doc, "\n")
	start, level := -1, 0
	fence := ""
	for i, line := range lines {
		l, text, ok := markdownHeading(line, &fence)
		if !ok {
			continue
		}
		if start < 0 {
			if text == title {
				start, level = i, l
			}
			continue
		}
		if l <= level {
			return strings.TrimSpace(strings.Join(lines[start:i], "\n")), true
		}
	}
	if start < 0 {
		return "", false
	}
	return strings.TrimSpace(strings.Join(lines[start:], "\n")), true
}

// markdownHeading reports whether line is an ATX heading outside a fenced code block, with its level and text; fence tracks the open fence's marker across calls.
func markdownHeading(line string, fence *string) (int, string, bool) {
	trim := strings.TrimSpace(line)
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trim, marker) {
			switch {
			case *fence == "":
				*fence = marker
			case *fence == marker:
				*fence = ""
			}
			return 0, "", false
		}
	}
	if *fence != "" || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
		return 0, "", false
	}
	level := len(trim) - len(strings.TrimLeft(trim, "#"))
	if level == 0 || level > 6 {
		return 0, "", false
	}
	rest := trim[level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	text := strings.TrimSpace(rest)
	// A closing run of # counts only after a space, so "C#" keeps its #.
	if closed := strings.TrimRight(text, "#"); closed == "" || strings.HasSuffix(closed, " ") || strings.HasSuffix(closed, "\t") {
		text = strings.TrimSpace(closed)
	}
	return level, text, true
}
