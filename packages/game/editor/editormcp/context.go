package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
)

const (
	// ToolProjectContext returns the project's shared agent context: brief.yaml (with gaps and playtest targets) and AGENTS.md.
	ToolProjectContext = "project_context"
	// ToolBriefEdit validates and proposes an edit to brief.yaml as a reviewable change without modifying disk until kept.
	ToolBriefEdit = "brief_edit"
	// ToolRulesEdit proposes an edit or new rule for AGENTS.md as a reviewable change without modifying disk until kept.
	ToolRulesEdit = "rules_edit"
)

// BriefContext is the brief.yaml section of ProjectContext.
type BriefContext struct {
	Exists          bool                          `json:"exists"`
	File            string                        `json:"file"`
	YAML            string                        `json:"yaml,omitempty"`
	Parsed          *project.Brief                `json:"parsed,omitempty"`
	Gaps            []project.BriefSection        `json:"gaps,omitempty"`
	PlaytestTargets []project.BriefPlaytestTarget `json:"playtestTargets,omitempty"`
	Error           string                        `json:"error,omitempty"`
}

// RulesContext is the AGENTS.md section of ProjectContext.
type RulesContext struct {
	Exists    bool   `json:"exists"`
	File      string `json:"file"`
	Markdown  string `json:"markdown,omitempty"`
	RuleCount int    `json:"ruleCount"`
}

// ProjectContext is the combined context payload given to the agent before requests
// through project_context and the editor://context resource.
type ProjectContext struct {
	Project        string             `json:"project,omitempty"`
	Language       string             `json:"language,omitempty"`
	Brief          BriefContext       `json:"brief"`
	Rules          RulesContext       `json:"rules"`
	PendingReviews []editor.DocReview `json:"pendingReviews,omitempty"`
}

// DocEditResult is returned by brief_edit and rules_edit when queuing a reviewable change.
type DocEditResult struct {
	Status string           `json:"status"`
	Review editor.DocReview `json:"review"`
}

const briefEditSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "yaml": {"type": "string", "description": "Full brief.yaml YAML document to validate strictly and propose for review"},
    "pitch": {"type": "string", "description": "Updated pitch text"},
    "genre": {"type": "array", "items": {"type": "string"}, "description": "Updated genre labels"},
    "tags": {"type": "array", "items": {"type": "string"}, "description": "Updated tags"},
    "platforms": {"type": "array", "items": {"type": "string"}, "description": "Updated platforms"},
    "artDirection": {"type": "string", "description": "Updated art direction"},
    "toneAndAudio": {"type": "string", "description": "Updated tone and audio direction"},
    "feel": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "summary": {"type": "string"},
        "targets": {
          "type": "array",
          "items": {
            "type": "object",
            "additionalProperties": false,
            "required": ["metric"],
            "properties": {
              "metric": {"type": "string", "minLength": 1},
              "scene": {"type": "string"},
              "summary": {"type": "string"},
              "unit": {"type": "string"},
              "min": {"type": "number"},
              "max": {"type": "number"},
              "equal": {"type": "number"}
            }
          }
        }
      }
    },
    "summary": {"type": "string", "description": "Short summary shown on the review card"},
    "rationale": {"type": "string", "description": "Why this edit is proposed"}
  }
}`

const rulesEditSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "markdown": {"type": "string", "description": "Full replacement Markdown for AGENTS.md"},
    "appendRule": {"type": "string", "description": "Single bullet rule to append to AGENTS.md"},
    "summary": {"type": "string", "description": "Short summary shown on the review card"},
    "rationale": {"type": "string", "description": "Why this rule change is proposed"}
  }
}`

func contextTools() []Tool {
	return []Tool{
		{
			Name:        ToolProjectContext,
			Title:       "Read project brief and rules",
			Description: "Read the shared project context the agent follows before every request: brief.yaml (pitch, genre, platforms, art direction, feel and difficulty, playtest targets, and missing sections) and AGENTS.md (plain Markdown project rules).",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{}}`),
			Action:      ActionRead,
			Handler:     projectContextTool,
		},
		{
			Name:        ToolBriefEdit,
			Title:       "Propose a game brief edit",
			Description: "Validate and propose an edit to brief.yaml (either full YAML or individual fields). Queues a reviewable diff without modifying brief.yaml on disk until the person keeps it.",
			InputSchema: []byte(briefEditSchema),
			Action:      ActionEdit,
			Handler:     briefEditTool,
		},
		{
			Name:        ToolRulesEdit,
			Title:       "Propose a project rules edit",
			Description: "Propose an edit or new bullet rule for AGENTS.md. Queues a reviewable diff without modifying AGENTS.md on disk until the person keeps it.",
			InputSchema: []byte(rulesEditSchema),
			Action:      ActionEdit,
			Handler:     rulesEditTool,
		},
	}
}

func readProjectContext(ed *editor.Editor) ProjectContext {
	ctx := ProjectContext{
		Language: ed.ResolvedLanguage(),
		Brief: BriefContext{
			Exists: ed.BriefExists(),
			File:   project.BriefFileName,
			Gaps:   ed.BriefGaps(),
		},
		Rules: RulesContext{
			Exists:    ed.ProjectRulesExists(),
			File:      editor.ProjectRulesFileName,
			Markdown:  ed.ProjectRules(),
			RuleCount: ed.ProjectRulesCount(),
		},
		PendingReviews: ed.PendingDocReviews(""),
	}
	if ed.Project != nil {
		ctx.Project = ed.Project.Name
	}
	if err := ed.BriefLoadErr(); err != nil {
		ctx.Brief.Error = err.Error()
	}
	if ed.BriefExists() {
		ctx.Brief.Parsed = ed.Brief()
		if raw, err := ed.BriefYAML(); err == nil {
			ctx.Brief.YAML = string(raw)
		}
		ctx.Brief.PlaytestTargets = ed.BriefPlaytestTargets("")
	}
	return ctx
}

func contextJSON(ed *editor.Editor) []byte {
	data, err := json.Marshal(readProjectContext(ed))
	if err != nil {
		return nil
	}
	return data
}

func briefResourceText(ed *editor.Editor) string {
	if !ed.BriefExists() {
		return ""
	}
	raw, err := ed.BriefYAML()
	if err != nil {
		return ""
	}
	return string(raw)
}

func projectContextTool(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("project_context: %w", err)
	}
	var payload ProjectContext
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		payload = readProjectContext(ed)
		return nil
	}); err != nil {
		return nil, err
	}
	var b strings.Builder
	if payload.Project != "" {
		fmt.Fprintf(&b, "Project: %s\n", payload.Project)
	}
	b.WriteString("\n## brief.yaml\n")
	if payload.Brief.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", payload.Brief.Error)
	} else if !payload.Brief.Exists {
		b.WriteString("(missing — no brief.yaml in project root)\n")
	} else {
		b.WriteString(payload.Brief.YAML)
		if !strings.HasSuffix(payload.Brief.YAML, "\n") {
			b.WriteByte('\n')
		}
		if len(payload.Brief.Gaps) > 0 {
			gaps := make([]string, len(payload.Brief.Gaps))
			for i, g := range payload.Brief.Gaps {
				gaps[i] = string(g)
			}
			fmt.Fprintf(&b, "Gaps: %s\n", strings.Join(gaps, ", "))
		}
	}
	b.WriteString("\n## AGENTS.md\n")
	if !payload.Rules.Exists || strings.TrimSpace(payload.Rules.Markdown) == "" {
		b.WriteString("(missing — no AGENTS.md in project root)\n")
	} else {
		b.WriteString(payload.Rules.Markdown)
		if !strings.HasSuffix(payload.Rules.Markdown, "\n") {
			b.WriteByte('\n')
		}
	}
	return &Result{Text: b.String(), Structured: payload}, nil
}

type briefEditArgs struct {
	YAML         *string            `json:"yaml"`
	Pitch        *string            `json:"pitch"`
	Genre        *[]string          `json:"genre"`
	Tags         *[]string          `json:"tags"`
	Platforms    *[]string          `json:"platforms"`
	ArtDirection *string            `json:"artDirection"`
	ToneAndAudio *string            `json:"toneAndAudio"`
	Feel         *project.BriefFeel `json:"feel"`
	Summary      string             `json:"summary"`
	Rationale    string             `json:"rationale"`
}

func briefEditTool(ctx context.Context, c *Call) (*Result, error) {
	var args briefEditArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("brief_edit: %w", err)
	}
	hasFields := args.Pitch != nil || args.Genre != nil || args.Tags != nil || args.Platforms != nil ||
		args.ArtDirection != nil || args.ToneAndAudio != nil || args.Feel != nil
	if args.YAML == nil && !hasFields {
		return nil, errors.New("brief_edit: provide yaml or at least one brief field to update")
	}
	if args.YAML != nil && hasFields {
		return nil, errors.New("brief_edit: provide either yaml or structured fields, not both")
	}

	var review *editor.DocReview
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var next *project.Brief
		if args.YAML != nil {
			parsed, err := project.ParseBrief([]byte(*args.YAML))
			if err != nil {
				return fmt.Errorf("brief_edit: %w", err)
			}
			next = parsed
		} else {
			if ed.BriefExists() {
				next = ed.Brief()
			} else {
				next = &project.Brief{Version: project.BriefVersion}
			}
			if args.Pitch != nil {
				next.Pitch = strings.TrimSpace(*args.Pitch)
			}
			if args.Genre != nil {
				next.Genre = append([]string(nil), (*args.Genre)...)
			}
			if args.Tags != nil {
				next.Tags = append([]string(nil), (*args.Tags)...)
			}
			if args.Platforms != nil {
				next.Platforms = append([]string(nil), (*args.Platforms)...)
			}
			if args.ArtDirection != nil {
				next.ArtDirection = strings.TrimSpace(*args.ArtDirection)
			}
			if args.ToneAndAudio != nil {
				next.ToneAndAudio = strings.TrimSpace(*args.ToneAndAudio)
			}
			if args.Feel != nil {
				f := *args.Feel
				next.Feel = &f
			}
		}
		r, err := ed.ProposeBriefReview(next, args.Summary, args.Rationale, "agent")
		if err != nil {
			return fmt.Errorf("brief_edit: %w", err)
		}
		review = r
		return nil
	}); err != nil {
		return nil, err
	}

	res := DocEditResult{Status: "pending_review", Review: *review}
	text := fmt.Sprintf("Proposed edit to brief.yaml (%s) queued for review; brief.yaml on disk was not modified yet.\n%s", review.ID, review.Diff)
	return &Result{Text: text, Structured: res}, nil
}

type rulesEditArgs struct {
	Markdown   *string `json:"markdown"`
	AppendRule *string `json:"appendRule"`
	Summary    string  `json:"summary"`
	Rationale  string  `json:"rationale"`
}

func rulesEditTool(ctx context.Context, c *Call) (*Result, error) {
	var args rulesEditArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("rules_edit: %w", err)
	}
	if args.Markdown == nil && args.AppendRule == nil {
		return nil, errors.New("rules_edit: provide markdown or appendRule")
	}
	if args.Markdown != nil && args.AppendRule != nil {
		return nil, errors.New("rules_edit: provide either markdown or appendRule, not both")
	}

	var review *editor.DocReview
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var next string
		summary := args.Summary
		if args.Markdown != nil {
			next = *args.Markdown
		} else {
			rule := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(*args.AppendRule, "- "), "* "))
			if rule == "" {
				return errors.New("rules_edit: appendRule must not be empty")
			}
			cur := strings.ReplaceAll(ed.ProjectRules(), "\r\n", "\n")
			if strings.TrimSpace(cur) == "" {
				name := "this project"
				if ed.Project != nil && strings.TrimSpace(ed.Project.Name) != "" {
					name = ed.Project.Name
				}
				next = fmt.Sprintf("# AGENTS.md · how to work on %s\n\n- %s\n", name, rule)
			} else {
				if !strings.HasSuffix(cur, "\n") {
					cur += "\n"
				}
				next = cur + "- " + rule + "\n"
			}
			if summary == "" {
				summary = "Add rule: " + rule
			}
		}
		r, err := ed.ProposeProjectRulesReview(next, summary, args.Rationale, "agent")
		if err != nil {
			return fmt.Errorf("rules_edit: %w", err)
		}
		review = r
		return nil
	}); err != nil {
		return nil, err
	}

	res := DocEditResult{Status: "pending_review", Review: *review}
	text := fmt.Sprintf("Proposed edit to AGENTS.md (%s) queued for review; AGENTS.md on disk was not modified yet.\n%s", review.ID, review.Diff)
	return &Result{Text: text, Structured: res}, nil
}
