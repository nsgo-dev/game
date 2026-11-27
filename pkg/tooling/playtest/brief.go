package playtest

import (
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/nsgo-dev/game/pkg/project"
)

// GoalFromBriefTarget converts a structured playtest target from brief.yaml into a playtest Goal
// whose Source is project.BriefFileName ("brief.yaml").
func GoalFromBriefTarget(t project.BriefPlaytestTarget) *Goal {
	g := &Goal{
		Summary: t.Summary,
		Source:  project.BriefFileName,
	}
	if t.Min != nil {
		v := cleanZero(*t.Min)
		g.Min = &v
	}
	if t.Max != nil {
		v := cleanZero(*t.Max)
		g.Max = &v
	}
	if t.Equal != nil {
		v := cleanZero(*t.Equal)
		g.Equal = &v
	}
	return g
}

// TargetsFromBrief returns a map of metric name to *Goal read from b's feel and difficulty targets
// for scene (empty scene includes every target in b, with later scene-specific entries taking precedence).
func TargetsFromBrief(b *project.Brief, scene string) map[string]*Goal {
	if b == nil {
		return nil
	}
	targets := b.TargetsForScene(scene)
	if len(targets) == 0 {
		return nil
	}
	out := make(map[string]*Goal, len(targets))
	for _, t := range targets {
		out[t.Metric] = GoalFromBriefTarget(t)
	}
	return out
}

// ApplyBriefTargets fills or refreshes Goals on cfg.Metrics from b's feel and difficulty targets
// matching cfg.Scope.Scene. Only metrics with a nil Goal or a Goal whose Source is "brief.yaml"
// are updated, leaving explicit playtest.yaml or game.yaml goals untouched. It returns the number
// of metrics updated.
func ApplyBriefTargets(cfg *Config, b *project.Brief) int {
	if cfg == nil || b == nil {
		return 0
	}
	goals := TargetsFromBrief(b, cfg.Scope.Scene)
	if len(goals) == 0 {
		return 0
	}
	updated := 0
	for i := range cfg.Metrics {
		g, ok := goals[cfg.Metrics[i].Name]
		if !ok {
			continue
		}
		if cfg.Metrics[i].Goal != nil && cfg.Metrics[i].Goal.Source != "" && cfg.Metrics[i].Goal.Source != project.BriefFileName {
			continue
		}
		cfg.Metrics[i].Goal = cloneGoal(g)
		updated++
	}
	return updated
}

// LoadBriefTargets reads brief.yaml at the root of projectDir and returns its playtest Goals for scene.
// When brief.yaml does not exist, it returns (nil, nil).
func LoadBriefTargets(projectDir, scene string) (map[string]*Goal, error) {
	b, err := project.LoadBrief(filepath.Join(projectDir, project.BriefFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return TargetsFromBrief(b, scene), nil
}
