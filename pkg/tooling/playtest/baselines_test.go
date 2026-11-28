package playtest

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const baselinesHeader = "version: 1\nscope: {scene: level}\nruns: {count: 2, maxDuration: 10s}\nbot: {profile: runner}\n"

func TestBaselinesRoundTripByteForByte(t *testing.T) {
	cfg, err := ParseConfig([]byte(baselinesHeader + "baselines:\n  idle: {outcome: lose, within: 8s}\n  reference: {winRate: 0.8}\n  degraded: {reactionScale: 2, dropRate: 0.25, maxWinRate: 0.4, margin: 0.2}\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := &Baselines{
		Idle:      &IdleBaseline{Outcome: IdleOutcomeLose, Within: 8 * time.Second},
		Reference: &ReferenceBaseline{WinRate: 0.8},
		Degraded:  &DegradedBaseline{ReactionScale: 2, DropRate: 0.25, MaxWinRate: Float64(0.4), Margin: Float64(0.2)},
	}
	if !reflect.DeepEqual(cfg.Baselines, want) {
		t.Fatalf("baselines = %+v, want %+v", cfg.Baselines, want)
	}
	encoded, err := EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("baselines:\n  idle:\n    outcome: lose\n    within: 8s\n")) {
		t.Fatalf("encoded config lost or reordered baselines:\n%s", encoded)
	}
	again, err := ParseConfig(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Baselines, want) {
		t.Fatalf("round-tripped baselines = %+v, want %+v", again.Baselines, want)
	}
	reencoded, err := EncodeConfig(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("round trip changed bytes:\n%s\n---\n%s", encoded, reencoded)
	}
	clone := cfg.Clone()
	*clone.Baselines.Degraded.Margin = 0.5
	if *cfg.Baselines.Degraded.Margin != 0.2 {
		t.Fatal("Clone shares the degraded margin with the original")
	}
}

func TestBaselinesRefuseInvalidDeclarations(t *testing.T) {
	cases := map[string]struct {
		doc   string
		field string
	}{
		"unknown field":                   {baselinesHeader + "baselines: {idle: {outcome: lose, within: 8s, after: 1s}}\n", "after"},
		"unknown policy":                  {baselinesHeader + "baselines: {skilled: {winRate: 1}}\n", "skilled"},
		"idle within over maxDuration":    {baselinesHeader + "baselines: {idle: {outcome: lose, within: 11s}}\n", "baselines.idle.within"},
		"idle within over default 60s":    {"version: 1\nscope: {scene: level}\nruns: {count: 2}\nbot: {profile: runner}\nbaselines: {idle: {outcome: stall, within: 61s}}\n", "baselines.idle.within"},
		"idle within missing":             {baselinesHeader + "baselines: {idle: {outcome: lose}}\n", "baselines.idle.within"},
		"idle outcome win":                {baselinesHeader + "baselines: {idle: {outcome: win, within: 8s}}\n", "baselines.idle.outcome"},
		"reference winRate over 1":        {baselinesHeader + "baselines: {reference: {winRate: 1.5}}\n", "baselines.reference.winRate"},
		"degraded does not weaken":        {baselinesHeader + "baselines: {degraded: {maxWinRate: 0.4}}\n", "baselines.degraded"},
		"degraded without win-rate limit": {baselinesHeader + "baselines: {degraded: {dropRate: 0.25}}\n", "baselines.degraded"},
		"degraded dropRate over 1":        {baselinesHeader + "baselines: {degraded: {dropRate: 2, margin: 0.2}}\n", "baselines.degraded.dropRate"},
		"degraded margin over 1":          {baselinesHeader + "baselines: {degraded: {reactionScale: 2, margin: 1.5}}\n", "baselines.degraded.margin"},
		"degraded maxWinRate negative":    {baselinesHeader + "baselines: {degraded: {reactionScale: 2, maxWinRate: -0.1}}\n", "baselines.degraded.maxWinRate"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tc.doc))
			if err == nil {
				t.Fatal("invalid baselines accepted")
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error %q does not name %q", err, tc.field)
			}
		})
	}
}

func TestReportBaselinesRoundTrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "platformer.report.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ParseReport(data)
	if err != nil {
		t.Fatal(err)
	}
	rep.Baselines = []BaselineReport{
		{Name: BaselineIdle, Passed: true, Outcome: IdleOutcomeLose},
		{Name: BaselineReference, Passed: true, WinRate: Float64(0.9)},
		{Name: BaselineDegraded, Passed: true, WinRate: Float64(0), Note: "degraded bot lost every run"},
	}
	encoded, err := EncodeReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("  - name: degraded\n    passed: true\n    winRate: 0\n")) {
		t.Fatalf("encoded report dropped the degraded win rate of 0:\n%s", encoded)
	}
	again, err := ParseReport(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Baselines, rep.Baselines) {
		t.Fatalf("round-tripped baselines = %+v, want %+v", again.Baselines, rep.Baselines)
	}
	reencoded, err := EncodeReport(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatal("report round trip changed bytes")
	}

	for name, b := range map[string][]BaselineReport{
		"unknown name":     {{Name: "skilled", Passed: true}},
		"duplicate name":   {{Name: BaselineIdle, Passed: true}, {Name: BaselineIdle}},
		"winRate over one": {{Name: BaselineReference, WinRate: Float64(1.2)}},
	} {
		bad := cloneReport(rep)
		bad.Baselines = b
		if err := ValidateReport(bad); err == nil || !strings.Contains(err.Error(), "baselines") {
			t.Errorf("%s: ValidateReport error = %v, want a baselines error", name, err)
		}
	}
}
