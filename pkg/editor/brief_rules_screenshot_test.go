package editor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/playtest"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestBriefAndRulesMacOSScreenshots captures real macOS nsgotest screenshots of
// Settings › Game brief (empty & filled with 1 gap) and Settings › Project rules
// (with reverted-review rule suggestion, inline review diff, and "Agent on this page" card)
// beside design/editor/Settings.dc.html (#1026).
func TestBriefAndRulesMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	gameYAML := "version: 1\nname: Dungeon Run\nid: dev.nsgo.dungeonrun\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.WaitForProblems()
	_ = ed.SetAppearance(editor.AppearanceDark)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// 1. Empty Game brief page (project without brief.yaml).
	app.Post(func() { ed.OpenBriefSettings() })
	app.Settle()
	app.Screenshot("settings-brief-empty")

	// 2. Filled Game brief page matching Settings.dc.html (missing Tone and audio -> "1 gap", plus playtest targets & missed metric note).
	app.Post(func() {
		_ = ed.CreateBrief(project.Brief{
			Version:      project.BriefVersion,
			Pitch:        "A short, tense dungeon platformer: a knight with one sword and three hearts dives through five floors, each ending in a door that only opens once its lever is found.",
			Genre:        []string{"2D platformer", "action"},
			Tags:         []string{"dungeon", "pixel-art"},
			Platforms:    []string{"desktop", "phones, touch", "web"},
			ArtDirection: "16 px pixel art, 4-colour shading, cold stone with warm torch light. No outlines on tiles; 1 px dark outline on characters.",
			Feel: &project.BriefFeel{
				Summary: "Snappy, forgiving jumps (coyote time). A first-time player clears floor 1 in under a minute and dies 2–4 times on floor 3.",
				Targets: []project.BriefPlaytestTarget{
					{Metric: "median_time", Scene: "floor_01", Summary: "under a minute", Unit: "s", Max: project.Float64(60)},
					{Metric: "deaths", Scene: "floor_03", Summary: "2–4 deaths on floor 3", Min: project.Float64(2), Max: project.Float64(4)},
				},
			},
		})
		_ = ed.OpenPlaytestReport(editor.PlaytestReportOpenConfig{
			Report: &playtest.Report{
				Version:   playtest.ReportVersion,
				Scope:     playtest.Scope{Scene: "Floor 3"},
				SeedCount: 20,
				Metrics: []playtest.MetricReport{
					{
						Name:   "deaths",
						Label:  "Deaths",
						Value:  6.1,
						Status: playtest.StatusMissed,
						Goal:   &playtest.Goal{Source: project.BriefFileName, Min: playtest.Float64(2), Max: playtest.Float64(4)},
					},
				},
			},
		})
		ed.ClosePlaytestReport()
		ed.OpenBriefSettings()
	})
	app.Settle()
	app.Screenshot("settings-brief-filled")

	// 3. Project rules page matching Settings.dc.html (5 rules + reverted-review suggestion + Agent on this page card).
	app.Post(func() {
		rules := "# AGENTS.md · how to work on Dungeon Run\n" +
			"- Object ids are snake_case and say what the thing is: slime_02, lever_boss.\n" +
			"- Never edit files under art/ by hand; regenerate them with game assets.\n" +
			"- New enemies are prefab variants of prefabs/enemy.prefab.yaml.\n" +
			"- Keep floors under 400 objects; split rooms before that.\n" +
			"- Difficulty changes come with a 20-run playtest, before and after.\n"
		_ = ed.SetProjectRules("Seed Project Rules", rules)
		var outcomes []editor.ReviewOutcomeRecord
		for range 27 {
			outcomes = append(outcomes, editor.ReviewOutcomeRecord{Action: "placed enemy prefab", Kept: true})
		}
		for range 3 {
			outcomes = append(outcomes, editor.ReviewOutcomeRecord{Action: "moved the camera bounds", Kept: false})
		}
		ed.RecordReviewOutcomes(outcomes...)
		ed.OpenRulesSettings()
	})
	app.Settle()
	app.Screenshot("settings-rules-suggestion")

	// 4. Pressing "Add as a rule" opens an inline reviewable diff card with Keep / Revert.
	sugs := ed.SuggestedProjectRules()
	if len(sugs) > 0 {
		app.Press("id:rules-suggestion-add-" + sugs[0].ID)
		app.Settle()
		app.Screenshot("settings-rules-review")
	}
}
