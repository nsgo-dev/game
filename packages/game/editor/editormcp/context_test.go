package editormcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/project"
)

// #1026: The editor's MCP server gives both brief.yaml and AGENTS.md to the agent as context
// through project_context and the editor://brief, editor://rules, and editor://context resources.
func TestMCPExposesBriefAndProjectRulesContext(t *testing.T) {
	f := openFixture(t)
	brief := &project.Brief{
		Version:      project.BriefVersion,
		Pitch:        "A short, tense dungeon platformer.",
		Genre:        []string{"2D platformer", "action"},
		Platforms:    []string{"desktop", "web"},
		ArtDirection: "16 px pixel art with warm torch light.",
		Feel: &project.BriefFeel{
			Summary: "Snappy jumps; 2-4 deaths on floor 3.",
			Targets: []project.BriefPlaytestTarget{
				{Metric: "deaths", Scene: "floor_03", Min: project.Float64(2), Max: project.Float64(4)},
			},
		},
	}
	if err := project.SaveBrief(filepath.Join(f.dir, project.BriefFileName), brief); err != nil {
		t.Fatal(err)
	}
	rules := "# AGENTS.md\n- Object ids are snake_case.\n- Never move the camera bounds.\n"
	if err := os.WriteFile(filepath.Join(f.dir, editor.ProjectRulesFileName), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.ReloadBrief(); err != nil {
		t.Fatal(err)
	}
	if err := f.ed.ReloadProjectRules(); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. project_context tool returns both brief.yaml and AGENTS.md with structured targets and gaps.
	res := call(t, cs, editormcp.ToolProjectContext, nil)
	out := mustOK(t, res)
	if !strings.Contains(out, "A short, tense dungeon platformer.") || !strings.Contains(out, "Never move the camera bounds.") {
		t.Fatalf("project_context text missing brief or rules:\n%s", out)
	}
	var payload editormcp.ProjectContext
	structured(t, res, &payload)
	if !payload.Brief.Exists || !payload.Rules.Exists || payload.Rules.RuleCount != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	if len(payload.Brief.PlaytestTargets) != 1 || payload.Brief.PlaytestTargets[0].Metric != "deaths" {
		t.Fatalf("playtest targets = %+v", payload.Brief.PlaytestTargets)
	}
	if len(payload.Brief.Gaps) != 1 || payload.Brief.Gaps[0] != project.BriefSectionToneAndAudio {
		t.Fatalf("gaps = %+v, want [toneAndAudio]", payload.Brief.Gaps)
	}

	// 2. MCP resources editor://brief, editor://rules, and editor://context return the same files.
	briefRes, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.BriefURI})
	if err != nil || len(briefRes.Contents) != 1 || !strings.Contains(briefRes.Contents[0].Text, "A short, tense dungeon platformer.") {
		t.Fatalf("editor://brief: err=%v res=%+v", err, briefRes)
	}
	rulesRes, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.RulesURI})
	if err != nil || len(rulesRes.Contents) != 1 || !strings.Contains(rulesRes.Contents[0].Text, "Object ids are snake_case.") {
		t.Fatalf("editor://rules: err=%v res=%+v", err, rulesRes)
	}
	ctxRes, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: editormcp.ContextURI})
	if err != nil || len(ctxRes.Contents) != 1 {
		t.Fatalf("editor://context: err=%v res=%+v", err, ctxRes)
	}
	var fromResource editormcp.ProjectContext
	if err := json.Unmarshal([]byte(ctxRes.Contents[0].Text), &fromResource); err != nil {
		t.Fatalf("decode editor://context: %v", err)
	}
	if !fromResource.Brief.Exists || !fromResource.Rules.Exists || fromResource.Brief.Parsed.Pitch != brief.Pitch {
		t.Fatalf("editor://context payload = %+v", fromResource)
	}
}

// #1026: Agent edits to brief.yaml and AGENTS.md via MCP go through review without modifying disk
// until kept, unknown YAML fields are refused with line numbers, and kept reviews undo cleanly.
func TestMCPBriefAndRulesEditsGoThroughReviewAndUndo(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Unknown field in brief_edit YAML is refused with line number.
	badRes := call(t, cs, editormcp.ToolBriefEdit, map[string]any{
		"yaml": "version: 1\npitch: Cave Run\nnotAField: bad\n",
	})
	if !badRes.IsError || !strings.Contains(text(badRes), "notAField") || !strings.Contains(text(badRes), ":3:") {
		t.Fatalf("expected line-numbered unknown field refusal, got isErr=%v text=%q", badRes.IsError, text(badRes))
	}

	// Valid brief_edit queues a review without writing brief.yaml to disk.
	res := call(t, cs, editormcp.ToolBriefEdit, map[string]any{
		"pitch":        "A cozy cave runner.",
		"genre":        []string{"2D platformer"},
		"platforms":    []string{"desktop"},
		"artDirection": "16 px warm stone.",
		"summary":      "Draft initial game brief",
	})
	mustOK(t, res)
	var briefEdit editormcp.DocEditResult
	structured(t, res, &briefEdit)
	if briefEdit.Status != "pending_review" || briefEdit.Review.Target != editor.DocReviewBrief {
		t.Fatalf("brief_edit result = %+v", briefEdit)
	}
	briefPath := filepath.Join(f.dir, project.BriefFileName)
	if _, err := os.Stat(briefPath); !os.IsNotExist(err) {
		t.Fatalf("brief.yaml should not exist on disk before Keep (err=%v)", err)
	}

	// Keeping the review writes canonical brief.yaml and is undoable.
	if err := f.ed.KeepDocReview(briefEdit.Review.ID); err != nil {
		t.Fatalf("KeepDocReview: %v", err)
	}
	if _, err := os.Stat(briefPath); err != nil {
		t.Fatalf("brief.yaml missing after Keep: %v", err)
	}
	if !f.ed.Undo() {
		t.Fatal("expected kept brief review to be undoable")
	}
	if _, err := os.Stat(briefPath); !os.IsNotExist(err) {
		t.Fatalf("brief.yaml should be removed on undo of creation (err=%v)", err)
	}

	// rules_edit queues a review for AGENTS.md without touching disk until kept.
	rres := call(t, cs, editormcp.ToolRulesEdit, map[string]any{
		"appendRule": "Keep floors under 400 objects.",
		"rationale":  "Performance target for phones.",
	})
	mustOK(t, rres)
	var rulesEdit editormcp.DocEditResult
	structured(t, rres, &rulesEdit)
	if rulesEdit.Status != "pending_review" || rulesEdit.Review.Target != editor.DocReviewRules {
		t.Fatalf("rules_edit result = %+v", rulesEdit)
	}
	rulesPath := filepath.Join(f.dir, editor.ProjectRulesFileName)
	if _, err := os.Stat(rulesPath); !os.IsNotExist(err) {
		t.Fatalf("AGENTS.md should not exist on disk before Keep (err=%v)", err)
	}
	if err := f.ed.KeepDocReview(rulesEdit.Review.ID); err != nil {
		t.Fatalf("KeepDocReview(rules): %v", err)
	}
	gotRules, err := os.ReadFile(rulesPath)
	if err != nil || !strings.Contains(string(gotRules), "Keep floors under 400 objects.") {
		t.Fatalf("AGENTS.md after Keep: err=%v content=%q", err, gotRules)
	}
}
