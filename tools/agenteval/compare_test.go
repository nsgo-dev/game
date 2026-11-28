package main

import "testing"

func TestCompareSuiteReportsFlagsDropsAndIgnoresModel(t *testing.T) {
	previous := SuiteReport{Mode: ModeAgent, Model: "model-a", Tasks: []TaskReport{{ID: "menu", Criteria: []CriterionResult{{Name: "builds", Passed: true}, {Name: "docs", Passed: true}}}}}
	current := SuiteReport{Mode: ModeAgent, Model: "model-a", Tasks: []TaskReport{{ID: "menu", Criteria: []CriterionResult{{Name: "builds", Passed: false}, {Name: "docs", Passed: true}}}}}
	changes := CompareSuiteReports(current, previous)
	if len(changes) != 1 || changes[0].TaskID != "menu" || changes[0].Criterion != "builds" || !changes[0].PreviousPassed || changes[0].CurrentPassed {
		t.Fatalf("changes = %+v", changes)
	}
	current.Model = "model-b"
	if got := CompareSuiteReports(current, previous); len(got) != 0 {
		t.Fatalf("different model comparison = %+v", got)
	}
}
