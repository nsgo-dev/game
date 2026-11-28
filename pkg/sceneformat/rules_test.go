package sceneformat

import "testing"

func TestEvaluateWhenDeterministicComparisons(t *testing.T) {
	ctx := RuleContext{Numbers: map[string]float64{"score": 12}, Bools: map[string]bool{"has_key": true}, Strings: map[string]string{"mode": "combat"}}
	for _, tc := range []struct {
		expr string
		want bool
	}{{"score >= 10", true}, {"score < 10", false}, {"has_key == true", true}, {"mode == combat", true}, {"mode != town", true}} {
		got, err := EvaluateWhen(tc.expr, ctx)
		if err != nil || got != tc.want {
			t.Fatalf("%q = %v, %v", tc.expr, got, err)
		}
	}
	if _, err := EvaluateWhen("missing == 1", ctx); err == nil {
		t.Fatal("unknown value accepted")
	}
}
