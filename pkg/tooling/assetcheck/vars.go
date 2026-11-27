package assetcheck

import (
	"path"
	"slices"

	"github.com/nsgo-dev/game/pkg/dialogue"
	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/loaderr"
)

// projectVars holds the declared game variables discovered under one project root.
type projectVars struct {
	files []string
	names map[string]string // var name -> file that declared it
}

func (pv *projectVars) has(name string) bool {
	if pv == nil {
		return false
	}
	_, ok := pv.names[name]
	return ok
}

// checkVars parses and validates a vars.yaml (or *.vars.yaml) file and registers its
// declared variables under its project root so dialogue scripts can be validated against them.
func (c *checker) checkVars(p string) {
	c.report.Vars++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	doc, err := flow.ParseVars(data)
	if err != nil {
		if le, ok := loaderr.As(err); ok {
			msg := le.Problem
			if le.Field != "" {
				msg = le.Field + ": " + msg
			}
			c.problemAt(p, le.Line, le.Column, "%s", msg)
			return
		}
		c.yamlError(p, data, err)
		return
	}

	root := c.projectRoot(path.Dir(p))
	pv := c.varsByRoot[root]
	if pv == nil {
		pv = &projectVars{names: make(map[string]string, len(doc.Vars))}
		c.varsByRoot[root] = pv
	}
	pv.files = append(pv.files, p)
	for i, decl := range doc.Vars {
		if prev, dup := pv.names[decl.Name]; dup {
			line, col := loaderr.FindField(data, loaderr.Path("vars", i, "name"))
			c.problemAt(p, line, col, "variable %q already declared in %s", decl.Name, prev)
			continue
		}
		pv.names[decl.Name] = p
	}
}

// checkYarn parses a Yarn Spinner (.yarn) dialogue script and, when the script's project root
// declares variables in vars.yaml, verifies that every variable reference in the script is declared.
func (c *checker) checkYarn(p string) {
	c.report.Dialogues++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	script, err := dialogue.ParseYarn(string(data))
	if err != nil {
		if le, ok := loaderr.As(err); ok {
			c.problemAt(p, le.Line, le.Column, "%s", le.Problem)
			return
		}
		c.problem(p, 0, "%v", err)
		return
	}
	root := c.projectRoot(path.Dir(p))
	pv := c.varsByRoot[root]
	if pv == nil {
		return
	}
	sourceList := pv.files[0]
	if len(pv.files) > 1 {
		sorted := slices.Clone(pv.files)
		slices.Sort(sorted)
		sourceList = sorted[0]
	}
	for _, ref := range script.VarRefs() {
		if !pv.has(ref.Name) {
			c.problem(p, ref.Line, "undeclared variable $%s (not declared in %s)", ref.Name, sourceList)
		}
	}
}
