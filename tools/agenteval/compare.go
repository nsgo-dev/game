package main

// CriterionChange describes a criterion whose pass state changed between two
// comparable suite reports.
type CriterionChange struct {
	TaskID         string
	Criterion      string
	PreviousPassed bool
	CurrentPassed  bool
}

// CompareSuiteReports returns changed criterion states when mode and model
// match. Reports from another model are deliberately ignored.
func CompareSuiteReports(current, previous SuiteReport) []CriterionChange {
	if current.Mode != previous.Mode || current.Model != previous.Model {
		return nil
	}
	previousTasks := make(map[string]TaskReport, len(previous.Tasks))
	for _, task := range previous.Tasks {
		previousTasks[task.ID] = task
	}
	var changes []CriterionChange
	for _, task := range current.Tasks {
		old, ok := previousTasks[task.ID]
		if !ok {
			continue
		}
		oldCriteria := make(map[string]CriterionResult, len(old.Criteria))
		for _, criterion := range old.Criteria {
			oldCriteria[criterion.Name] = criterion
		}
		for _, criterion := range task.Criteria {
			before, ok := oldCriteria[criterion.Name]
			if ok && before.Passed != criterion.Passed {
				changes = append(changes, CriterionChange{TaskID: task.ID, Criterion: criterion.Name, PreviousPassed: before.Passed, CurrentPassed: criterion.Passed})
			}
		}
	}
	return changes
}
