package dialogue_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/dialogue"
	"github.com/nsgo-dev/game/pkg/flow"
)

const sampleVarsYAML = `version: 1
vars:
  - name: bridge_repaired
    kind: bool
    doc: True once the east bridge is fixed.
  - name: gold
    kind: int
    default: 50
    min: 0
    max: 9999
    doc: Player gold coins.
  - name: affinity
    kind: float
    default: 0.5
    min: 0
    max: 1
    doc: Relationship score with Mira.
  - name: hero_name
    kind: string
    default: Aria
    doc: Protagonist name.
`

func TestDialogueWithSharedVars(t *testing.T) {
	doc, err := flow.ParseVars([]byte(sampleVarsYAML))
	if err != nil {
		t.Fatal(err)
	}
	vars, err := flow.NewVars(doc)
	if err != nil {
		t.Fatal(err)
	}

	const yarn = `title: BridgeKeeper
---
<<declare $gold = 10>>
<<if $bridge_repaired>>
    Keeper: Safe travels, {$hero_name}!
<<else>>
    Keeper: Pay 20 gold to fix the bridge, {$hero_name}. You have {$gold}.
    -> Repair the bridge <<if $gold >= 20>>
        <<set $gold -= 20>>
        <<set $bridge_repaired = true>>
        <<set $affinity += 0.25>>
        Keeper: Done! {$gold} gold left.
    -> Leave
        Keeper: Come back later.
<<endif>>
===
`
	script, err := dialogue.ParseYarn(yarn)
	if err != nil {
		t.Fatalf("ParseYarn: %v", err)
	}

	var changes []flow.VarChange
	vars.OnChange("", func(ch flow.VarChange) {
		changes = append(changes, ch)
	})

	runner := dialogue.NewRunner(script, dialogue.Options{Vars: vars})

	// <<declare $gold = 10>> must NOT clobber the store's declared default of 50.
	step, err := runner.Start("BridgeKeeper")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if step.Kind != dialogue.StepLine || !strings.Contains(step.Line.Text, "You have 50.") {
		t.Fatalf("step 1 = %+v", step)
	}

	step, err = runner.Continue()
	if err != nil || step.Kind != dialogue.StepChoices || !step.Choices[0].Available {
		t.Fatalf("choices step = %+v, err=%v", step, err)
	}

	step, err = runner.Choose(0)
	if err != nil || step.Kind != dialogue.StepLine || !strings.Contains(step.Line.Text, "30 gold left.") {
		t.Fatalf("after choose = %+v, err=%v", step, err)
	}

	// Verify shared store was updated and OnChange fired in statement order.
	if g, _ := vars.Int("gold"); g != 30 {
		t.Fatalf("store gold = %d, want 30", g)
	}
	if b, _ := vars.Bool("bridge_repaired"); !b {
		t.Fatalf("store bridge_repaired = false, want true")
	}
	if a, _ := vars.Float("affinity"); a != 0.75 {
		t.Fatalf("store affinity = %g, want 0.75", a)
	}
	if len(changes) != 3 || changes[0].Name != "gold" || changes[1].Name != "bridge_repaired" || changes[2].Name != "affinity" {
		t.Fatalf("OnChange order = %+v", changes)
	}

	// A second Runner sharing the same store immediately sees the updated switch.
	runner2 := dialogue.NewRunner(script, dialogue.Options{Vars: vars})
	step, err = runner2.Start("BridgeKeeper")
	if err != nil || step.Kind != dialogue.StepLine || !strings.Contains(step.Line.Text, "Safe travels, Aria!") {
		t.Fatalf("runner2 step = %+v, err=%v", step, err)
	}
}

func TestDialogueRefusesUndeclaredVarWhenStoreGiven(t *testing.T) {
	doc, err := flow.ParseVars([]byte(sampleVarsYAML))
	if err != nil {
		t.Fatal(err)
	}
	vars, err := flow.NewVars(doc)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("undeclared declare", func(t *testing.T) {
		script, err := dialogue.ParseYarn("title: Start\n---\n<<declare $typo_var = true>>\nHi.\n===\n")
		if err != nil {
			t.Fatal(err)
		}
		runner := dialogue.NewRunner(script, dialogue.Options{Vars: vars})
		_, err = runner.Start("Start")
		if !errors.Is(err, flow.ErrUndeclaredVar) {
			t.Fatalf("err = %v, want ErrUndeclaredVar", err)
		}
	})

	t.Run("undeclared set", func(t *testing.T) {
		script, err := dialogue.ParseYarn("title: Start\n---\n<<set $typo_var = 1>>\nHi.\n===\n")
		if err != nil {
			t.Fatal(err)
		}
		runner := dialogue.NewRunner(script, dialogue.Options{Vars: vars})
		_, err = runner.Start("Start")
		if !errors.Is(err, flow.ErrUndeclaredVar) {
			t.Fatalf("err = %v, want ErrUndeclaredVar", err)
		}
	})

	t.Run("out of range set", func(t *testing.T) {
		script, err := dialogue.ParseYarn("title: Start\n---\n<<set $gold = -5>>\nHi.\n===\n")
		if err != nil {
			t.Fatal(err)
		}
		runner := dialogue.NewRunner(script, dialogue.Options{Vars: vars})
		_, err = runner.Start("Start")
		if !errors.Is(err, flow.ErrVarOutOfRange) {
			t.Fatalf("err = %v, want ErrVarOutOfRange", err)
		}
	})
}

func TestScriptVarRefs(t *testing.T) {
	const yarn = `title: Start
---
<<declare $gold = 10>>
<<if $bridge_repaired && visited(Other)>>
    Hello {$hero_name}!
<<elseif $affinity > 0.5>>
    <<set $gold += 5>>
    <<give_item $hero_name {$gold}>>
<<endif>>
-> Buy <<if $gold >= 10>>
    Hi {hero_name}
===
title: Other
---
Done.
===
`
	script, err := dialogue.ParseYarn(yarn)
	if err != nil {
		t.Fatal(err)
	}
	refs := script.VarRefs()
	type wantRef struct {
		name  string
		line  int
		write bool
		decl  bool
	}
	want := []wantRef{
		{"gold", 3, true, true},
		{"bridge_repaired", 4, false, false},
		{"hero_name", 5, false, false},
		{"affinity", 6, false, false},
		{"gold", 7, true, false},
		{"hero_name", 8, false, false},
		{"gold", 8, false, false},
		{"gold", 10, false, false},
		{"hero_name", 11, false, false},
	}
	if len(refs) != len(want) {
		t.Fatalf("VarRefs len = %d (%+v), want %d (%+v)", len(refs), refs, len(want), want)
	}
	for i, w := range want {
		got := refs[i]
		if got.Name != w.name || got.Line != w.line || got.Write != w.write || got.Declare != w.decl {
			t.Errorf("refs[%d] = %+v, want %+v", i, got, w)
		}
	}
}
