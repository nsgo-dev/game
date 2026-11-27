package assetcheck_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

func TestCheckVarsAndYarnValidation(t *testing.T) {
	const validVars = `version: 1
vars:
  - name: bridge_repaired
    kind: bool
  - name: gold
    kind: int
    default: 50
    min: 0
    max: 9999
`
	const validYarn = `title: Start
---
<<if $bridge_repaired>>
    Guard: Bridge is open! You have {$gold} gold.
<<else>>
    <<set $gold += 10>>
    Guard: Still broken.
<<endif>>
===
`
	const typoYarn = `title: Start
---
<<if $quest_stat>>
    Guard: Hello!
<<endif>>
<<set $gold += 1>>
-> Pay <<if $missing_flag>>
    Paid {$typo_interp}
===
`

	t.Run("valid vars and yarn pass", func(t *testing.T) {
		fsys := fstest.MapFS{
			"vars.yaml":          {Data: []byte(validVars)},
			"data/dialogue.yarn": {Data: []byte(validYarn)},
		}
		rep, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !rep.OK() {
			t.Fatalf("unexpected problems: %+v", rep.Problems)
		}
		if rep.Vars != 1 || rep.Dialogues != 1 {
			t.Fatalf("counts = (Vars:%d, Dialogues:%d), want (1, 1)", rep.Vars, rep.Dialogues)
		}
	})

	t.Run("undeclared yarn variable reports file and line", func(t *testing.T) {
		fsys := fstest.MapFS{
			"vars.yaml":          {Data: []byte(validVars)},
			"data/dialogue.yarn": {Data: []byte(typoYarn)},
		}
		rep, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if len(rep.Problems) != 3 {
			t.Fatalf("Problems = %+v, want 3 undeclared variable problems", rep.Problems)
		}
		want := []struct {
			line int
			name string
		}{
			{3, "$quest_stat"},
			{7, "$missing_flag"},
			{8, "$typo_interp"},
		}
		for i, w := range want {
			p := rep.Problems[i]
			if p.File != "data/dialogue.yarn" || p.Line != w.line || !strings.Contains(p.Message, w.name) {
				t.Errorf("problem[%d] = %+v, want data/dialogue.yarn:%d containing %s", i, p, w.line, w.name)
			}
		}
	})

	t.Run("malformed vars.yaml reports file and line", func(t *testing.T) {
		badVars := "version: 1\nvars:\n  - name: gold\n    kind: int\n    bogus: 1\n"
		fsys := fstest.MapFS{
			"vars.yaml": {Data: []byte(badVars)},
		}
		rep, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if len(rep.Problems) != 1 {
			t.Fatalf("Problems = %+v, want 1", rep.Problems)
		}
		if rep.Problems[0].File != "vars.yaml" || rep.Problems[0].Line != 5 {
			t.Errorf("problem = %+v, want vars.yaml:5", rep.Problems[0])
		}
	})

	t.Run("yarn without vars.yaml only checks syntax", func(t *testing.T) {
		fsys := fstest.MapFS{
			"data/dialogue.yarn": {Data: []byte(typoYarn)},
		}
		rep, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !rep.OK() {
			t.Fatalf("unexpected problems without vars.yaml: %+v", rep.Problems)
		}
	})

	t.Run("duplicate variable across two vars files reported", func(t *testing.T) {
		fsys := fstest.MapFS{
			"vars.yaml":       {Data: []byte(validVars)},
			"extra.vars.yaml": {Data: []byte("version: 1\nvars:\n  - name: gold\n    kind: int\n")},
		}
		rep, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0].Message, "already declared") {
			t.Fatalf("Problems = %+v, want duplicate declaration problem", rep.Problems)
		}
	})
}
