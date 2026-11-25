package assetcheck

import (
	"errors"
	"path"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"go.yaml.in/yaml/v3"
)

// checkStates validates a *.states.yaml sprite state machine file:
// - strict YAML schema, valid entry/from/to states and compiled parameter conditions (reported at exact YAML lines),
// - every state's clip file exists and parses as a valid *resource.Animation,
// - every declared parameter is read by at least one transition.
func (c *checker) checkStates(p string) *animclip.StateMachineDoc {
	if doc, seen := c.statesDocs[p]; seen {
		return doc
	}
	c.report.States++
	c.statesDocs[p] = nil
	data, ok := c.readFile(p)
	if !ok {
		return nil
	}
	var root yaml.Node
	hasRoot := yaml.Unmarshal(data, &root) == nil
	lines := yamlLineCount(data)

	doc, err := animclip.ParseStateMachine(data)
	if err != nil {
		if sme, ok := errors.AsType[*animclip.StateMachineError](err); ok {
			line := 0
			if hasRoot {
				line = min(yamlLine(&root, sme.Path), lines)
			}
			c.problem(p, line, "%s", sme.Message)
			return nil
		}
		c.yamlError(p, data, err)
		return nil
	}
	c.statesDocs[p] = doc
	projRoot := c.projectRoot(path.Dir(p))

	// Check every state's animation clip reference.
	for i, st := range doc.States {
		line := 0
		if hasRoot {
			line = min(yamlLine(&root, []string{"states", strconv.Itoa(i), "clip"}), lines)
		}
		clipPath, ok := c.fileRef(p, line, projRoot, st.Clip, "clip")
		if !ok {
			continue
		}
		c.checkClipFile(clipPath)
	}

	// Refuse declared parameters that no transition condition can read.
	for _, idx := range doc.UnusedParameters() {
		param := doc.Parameters[idx]
		line := 0
		if hasRoot {
			line = min(yamlLine(&root, []string{"parameters", strconv.Itoa(idx), "name"}), lines)
		}
		c.problem(p, line, "parameter %q is not read by any transition", param.Name)
	}

	return doc
}

func (c *checker) checkClipFile(p string) {
	if c.clips[p] {
		return
	}
	c.clips[p] = true
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	if _, err := resource.ParseAs[*resource.Animation](data); err != nil {
		c.yamlError(p, data, err)
	}
}

// animator checks an object's built-in "animator" component: its fields, states file reference and optional startClip.
func (s *sceneCheck) animator(o *sceneformat.ObjectDoc, steps ...string) {
	if o == nil || o.Components == nil {
		return
	}
	if _, has := o.Components[sceneformat.AnimatorComponentName]; !has {
		return
	}
	cfg, _, err := sceneformat.ResolveAnimatorComponent(o)
	if err != nil {
		if ce, ok := errors.AsType[*sceneformat.ComponentError](err); ok {
			st := append(slices.Clip(steps), "components", sceneformat.AnimatorComponentName)
			if ce.Field != "" {
				st = append(st, ce.Field)
			}
			s.problem(s.path, s.line(st...), "%s", ce.Msg)
			return
		}
		s.problem(s.path, s.line(append(slices.Clip(steps), "components", sceneformat.AnimatorComponentName)...), "%v", err)
		return
	}
	if cfg.States == "" {
		s.problem(s.path, s.line(append(slices.Clip(steps), "components", sceneformat.AnimatorComponentName)...), "animator component must set states")
		return
	}
	statesLine := s.line(append(slices.Clip(steps), "components", sceneformat.AnimatorComponentName, "states")...)
	statesPath, ok := s.fileRef(s.path, statesLine, s.project, cfg.States, "states")
	if !ok {
		return
	}
	doc := s.checkStates(statesPath)
	if doc == nil || cfg.StartClip == "" {
		return
	}
	if doc.State(cfg.StartClip) != nil {
		return
	}
	for _, st := range doc.States {
		if st.Clip == cfg.StartClip {
			return
		}
	}
	startLine := s.line(append(slices.Clip(steps), "components", sceneformat.AnimatorComponentName, "startClip")...)
	s.problem(s.path, startLine, "startClip %q is not a state or clip in %q", cfg.StartClip, cfg.States)
}
