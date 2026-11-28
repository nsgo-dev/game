package sceneformat_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

const whenScene = `version: 1
name: Vault
layers:
  - name: props
    objects:
      - id: button
        components:
          button: {}
      - id: door
        components:
          door: {}
      - id: gate
        components:
          door: {}
connections:
  - from: button
    event: pressed
    to: gate
    handler: open
    once: true
    when: has_key
  - from: button
    event: pressed
    to: door
    handler: close
    when: score >= 10
  - from: button
    event: pressed
    to: door
    handler: open
`

func TestConnectionWhenRoundTripsAndGatesHandlers(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	doc, err := sceneformat.ParseWith([]byte(whenScene), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if want := sceneformat.SchemaComment + "\n" + whenScene; string(out) != want {
		t.Fatalf("round trip changed the scene:\n%s\n--- want ---\n%s", out, want)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	rules := sceneformat.RuleContext{Numbers: map[string]float64{"score": 3}, Bools: map[string]bool{"has_key": false}}
	sig.SetRuleContext(rules)
	targets := func() []string {
		var got []string
		for _, c := range calls {
			got = append(got, c.Target.ID+"."+c.Handler)
		}
		calls = calls[:0]
		return got
	}
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := targets(), []string{"door.open"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("with both conditions false: calls %v, want %v", got, want)
	}
	// The maps are read when the event fires, and a once connection whose condition was false is still connected.
	rules.Numbers["score"] = 12
	rules.Bools["has_key"] = true
	for range 2 {
		if err := sig.Emit("button", "pressed", nil); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := targets(), []string{"gate.open", "door.close", "door.open", "door.close", "door.open"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("with both conditions true: calls %v, want %v", got, want)
	}
}

func TestConnectionWhenErrors(t *testing.T) {
	var calls []sceneformat.Call
	reg := doorRegistry(t, &calls)
	bad := strings.Replace(whenScene, "when: score >= 10", "when: score >=", 1)
	if _, err := sceneformat.ParseWith([]byte(bad), sceneformat.ParseOptions{Components: reg}); err == nil || !strings.Contains(err.Error(), "when") {
		t.Fatalf("ParseWith accepted a malformed when: %v", err)
	}
	doc, err := sceneformat.ParseWith([]byte(whenScene), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// Without a rule context neither name has a value: both connections are skipped and reported, and the unconditional one still runs.
	err = sig.Emit("button", "pressed", nil)
	if err == nil || !strings.Contains(err.Error(), `"has_key"`) || !strings.Contains(err.Error(), `"score"`) {
		t.Fatalf("Emit = %v, want errors naming has_key and score", err)
	}
	if len(calls) != 1 || calls[0].Target.ID != "door" || calls[0].Handler != "open" {
		t.Fatalf("calls = %d, want only door.open", len(calls))
	}
	if err := sig.Enqueue("button", "pressed", nil); err != nil {
		t.Fatal(err)
	}
	if err := sig.Flush(); err == nil || !strings.Contains(err.Error(), `"score"`) {
		t.Fatalf("Flush = %v, want the when errors", err)
	}
}

func TestEvaluateWhen(t *testing.T) {
	ctx := sceneformat.RuleContext{Numbers: map[string]float64{"score": 12}, Bools: map[string]bool{"has_key": true}, Strings: map[string]string{"mode": "combat"}}
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"", true}, {"true", true}, {"false", false}, {"has_key", true},
		{"score >= 10", true}, {"score < 10", false}, {"score == 12", true}, {"score != 12", false}, {"score > 12", false}, {"score <= 12", true},
		{"has_key == true", true}, {"has_key != true", false},
		{"mode == combat", true}, {`mode == "combat"`, true}, {"mode != town", true},
	} {
		got, err := sceneformat.EvaluateWhen(tc.expr, ctx)
		if err != nil || got != tc.want {
			t.Errorf("%q = %v, %v; want %v", tc.expr, got, err, tc.want)
		}
	}
	for _, expr := range []string{"missing == 1", "missing", "score", "score >= ten", "mode < town", "has_key > false", "has_key == maybe", "score ~ 1", "score >= 1 2"} {
		if _, err := sceneformat.EvaluateWhen(expr, ctx); err == nil {
			t.Errorf("%q: no error", expr)
		}
	}
}
