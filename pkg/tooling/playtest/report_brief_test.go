package playtest_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

func TestApplyBriefTargetStatusesKeepsUnmeasuredPending(t *testing.T) {
	b := &project.Brief{Version: project.BriefVersion, Feel: &project.BriefFeel{Targets: []project.BriefPlaytestTarget{{Metric: "finished", Scene: "s", Basis: "target", Equal: project.Float64(1)}, {Metric: "deaths", Scene: "s", Basis: "assumption", Max: project.Float64(3)}}}}
	r := &playtest.Report{Version: playtest.ReportVersion, Scope: playtest.Scope{Scene: "s"}, Metrics: []playtest.MetricReport{{Name: "finished", Status: playtest.StatusMet, Aggregate: playtest.AggCount, Value: 1}}}
	playtest.ApplyBriefTargetStatuses(r, b)
	if len(r.BriefTargets) != 2 || r.BriefTargets[0].Metric != "deaths" || r.BriefTargets[0].Status != playtest.BriefTargetPending || r.BriefTargets[1].Metric != "finished" || r.BriefTargets[1].Status != playtest.BriefTargetMet {
		t.Fatalf("brief targets = %+v", r.BriefTargets)
	}
}
