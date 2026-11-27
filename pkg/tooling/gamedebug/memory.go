//go:build !release

package gamedebug

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

type trackedAsset struct {
	kind  AssetKind
	bytes int64
}

// MemoryTracker accounts for decoded asset memory grouped by owner (such as level or scene) and validates per-group and total byte budgets.
type MemoryTracker struct {
	mu          sync.Mutex
	groups      map[string]map[string]trackedAsset
	budgets     map[string]int64
	totalBudget int64
	totalBytes  int64
}

// NewMemoryTracker returns an empty asset MemoryTracker.
func NewMemoryTracker() *MemoryTracker {
	return &MemoryTracker{
		groups:  make(map[string]map[string]trackedAsset),
		budgets: make(map[string]int64),
	}
}

// Track records or updates an asset named name in group with its AssetKind and size in bytes. Negative byte counts are clamped to 0.
func (m *MemoryTracker) Track(group, name string, kind AssetKind, bytes int64) {
	if m == nil || group == "" || name == "" {
		return
	}
	if bytes < 0 {
		bytes = 0
	}
	m.mu.Lock()
	assets := m.groups[group]
	if assets == nil {
		assets = make(map[string]trackedAsset)
		m.groups[group] = assets
	}
	if prev, ok := assets[name]; ok {
		m.totalBytes -= prev.bytes
	}
	assets[name] = trackedAsset{kind: kind, bytes: bytes}
	m.totalBytes += bytes
	m.mu.Unlock()
}

// Untrack removes a previously tracked asset from group.
func (m *MemoryTracker) Untrack(group, name string) {
	if m == nil || group == "" || name == "" {
		return
	}
	m.mu.Lock()
	if assets := m.groups[group]; assets != nil {
		if prev, ok := assets[name]; ok {
			m.totalBytes -= prev.bytes
			delete(assets, name)
			if len(assets) == 0 {
				delete(m.groups, group)
			}
		}
	}
	m.mu.Unlock()
}

// ReleaseGroup drops all tracked assets and bytes belonging to group. Any configured budget for group is retained.
func (m *MemoryTracker) ReleaseGroup(group string) {
	if m == nil || group == "" {
		return
	}
	m.mu.Lock()
	if assets := m.groups[group]; assets != nil {
		for _, a := range assets {
			m.totalBytes -= a.bytes
		}
		delete(m.groups, group)
	}
	if m.totalBytes < 0 {
		m.totalBytes = 0
	}
	m.mu.Unlock()
}

// SetBudget sets the maximum allowed bytes for group. Passing maxBytes <= 0 removes the budget for group.
func (m *MemoryTracker) SetBudget(group string, maxBytes int64) {
	if m == nil || group == "" {
		return
	}
	m.mu.Lock()
	if maxBytes <= 0 {
		delete(m.budgets, group)
	} else {
		m.budgets[group] = maxBytes
	}
	m.mu.Unlock()
}

// SetTotalBudget sets the maximum allowed total bytes across all groups. Passing maxBytes <= 0 removes the total budget.
func (m *MemoryTracker) SetTotalBudget(maxBytes int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if maxBytes <= 0 {
		m.totalBudget = 0
	} else {
		m.totalBudget = maxBytes
	}
	m.mu.Unlock()
}

// TotalBudget returns the configured total byte budget, or 0 when none is set.
func (m *MemoryTracker) TotalBudget() int64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	b := m.totalBudget
	m.mu.Unlock()
	return b
}

// GroupBytes returns the total tracked bytes currently held by group.
func (m *MemoryTracker) GroupBytes(group string) int64 {
	if m == nil || group == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.groupBytesLocked(group)
}

func (m *MemoryTracker) groupBytesLocked(group string) int64 {
	var sum int64
	for _, a := range m.groups[group] {
		sum += a.bytes
	}
	return sum
}

// TotalBytes returns the total tracked bytes across all asset groups.
func (m *MemoryTracker) TotalBytes() int64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	total := m.totalBytes
	m.mu.Unlock()
	return total
}

// Groups returns a sorted snapshot of all groups with tracked assets and their per-kind byte breakdowns.
func (m *MemoryTracker) Groups() []GroupMemory {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.groups) == 0 {
		return nil
	}
	names := make([]string, 0, len(m.groups))
	for name := range m.groups {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]GroupMemory, 0, len(names))
	for _, name := range names {
		assets := m.groups[name]
		byKind := make(map[AssetKind]int64, 4)
		var total int64
		for _, a := range assets {
			total += a.bytes
			byKind[a.kind] += a.bytes
		}
		budget := m.budgets[name]
		out = append(out, GroupMemory{
			Group:      name,
			TotalBytes: total,
			Budget:     budget,
			OverBudget: budget > 0 && total > budget,
			Count:      len(assets),
			ByKind:     byKind,
		})
	}
	return out
}

// CheckBudget returns an error if group has a configured budget and its tracked bytes exceed that budget.
func (m *MemoryTracker) CheckBudget(group string) error {
	if m == nil || group == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	budget := m.budgets[group]
	if budget <= 0 {
		return nil
	}
	used := m.groupBytesLocked(group)
	if used > budget {
		return fmt.Errorf("gamedebug: asset group %q uses %d bytes (%s), exceeding budget %d bytes (%s)", group, used, formatBytes(used), budget, formatBytes(budget))
	}
	return nil
}

// CheckAllBudgets validates the total budget and every configured per-group budget, returning a joined error if any are exceeded.
func (m *MemoryTracker) CheckAllBudgets() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	if m.totalBudget > 0 && m.totalBytes > m.totalBudget {
		errs = append(errs, fmt.Errorf("gamedebug: total asset memory %d bytes (%s) exceeds budget %d bytes (%s)", m.totalBytes, formatBytes(m.totalBytes), m.totalBudget, formatBytes(m.totalBudget)))
	}

	budgeted := make([]string, 0, len(m.budgets))
	for group := range m.budgets {
		budgeted = append(budgeted, group)
	}
	sort.Strings(budgeted)
	for _, group := range budgeted {
		budget := m.budgets[group]
		if budget <= 0 {
			continue
		}
		used := m.groupBytesLocked(group)
		if used > budget {
			errs = append(errs, fmt.Errorf("gamedebug: asset group %q uses %d bytes (%s), exceeding budget %d bytes (%s)", group, used, formatBytes(used), budget, formatBytes(budget)))
		}
	}
	return errors.Join(errs...)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
