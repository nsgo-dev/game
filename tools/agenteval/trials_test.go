package main

import (
	"math"
	"testing"
)

func TestAggregateTaskReports(t *testing.T) {
	got := AggregateTaskReports([]TaskReport{{ID: "x", Score: 100, Passed: true}, {ID: "x", Score: 80, Passed: false}, {ID: "x", Score: 90, Passed: true}})
	if got.Trials != 3 || got.PassRate != 2.0/3.0 || got.MeanScore != 90 || math.Abs(got.StdDev-math.Sqrt(200.0/3.0)) > 1e-9 {
		t.Fatalf("aggregate = %+v", got)
	}
	if got.Passed {
		t.Fatal("partial pass rate should not pass")
	}
}
