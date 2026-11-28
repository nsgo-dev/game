package assets

import "fmt"

// MemoryBudgetError reports a group load rejected because decoded asset memory
// would exceed the manager's total or per-group budget.
type MemoryBudgetError struct {
	Group       string
	GroupBytes  int64
	TotalBytes  int64
	GroupBudget int64
	TotalBudget int64
}

func (e *MemoryBudgetError) Error() string {
	if e == nil {
		return "assets: memory budget exceeded"
	}
	return fmt.Sprintf("assets: loading group %q would use %d bytes (total %d), exceeding memory budget (group %d, total %d)", e.Group, e.GroupBytes, e.TotalBytes, e.GroupBudget, e.TotalBudget)
}
