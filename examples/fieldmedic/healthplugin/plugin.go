// Package healthplugin is Field Medic's scene editor plugin for its health component (package health): a Ward panel listing every patient's hit points with a button that heals them all in one undoable step, an inspector that edits a patient's hit points with a bar of what is left, and an Admit tool that places a new patient where the viewport is pressed. The game's own editor command (cmd/editor) registers it, and game.yaml turns it on under editor.plugins.
package healthplugin

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/examples/fieldmedic/health"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Name is the plugin's name, which game.yaml lists under editor.plugins to turn it on.
const Name = "health"

// AdmitHP is the hit points of a patient the Admit tool places.
const AdmitHP = 5

// Plugin returns the health plugin, for Editor.RegisterPlugin.
func Plugin() editor.Plugin {
	return editor.Plugin{
		Name: Name,
		Panels: []editor.Panel{{
			ID:    "ward",
			Title: "Ward",
			Dock:  editor.DockRight,
			View:  wardPanel,
		}},
		Inspectors: []editor.Inspector{{
			Component: health.Name,
			View:      inspector,
		}},
		Tools: []editor.PluginTool{{
			ID:    "admit",
			Label: "Admit",
			Press: admit,
			Draw:  drawPatients,
		}},
	}
}

// stats reads the health of objectID through the editor, so an unset field reads its registered default.
func stats(ed *editor.Editor, objectID string) health.Stats {
	var s health.Stats
	if v, ok := ed.ComponentField(objectID, health.Name, health.FieldHP); ok {
		s.HP, _ = v.(int)
	}
	if v, ok := ed.ComponentField(objectID, health.Name, health.FieldMax); ok {
		s.Max, _ = v.(int)
	}
	return s
}

// Summary returns the Ward panel's list: each patient's ID with its hit points and max, in scene order, one per line.
func Summary(ed *editor.Editor) string {
	var rows []string
	for _, id := range health.Objects(ed.Doc) {
		s := stats(ed, id)
		rows = append(rows, fmt.Sprintf("%s %d/%d", id, s.HP, s.Max))
	}
	if len(rows) == 0 {
		return "No patients"
	}
	return strings.Join(rows, "\n")
}

// Bar returns a text bar of s: ten cells, filled in proportion to the hit points left.
func Bar(s health.Stats) string {
	const cells = 10
	filled := 0
	if s.Max > 0 {
		filled = min(cells, max(0, (s.HP*cells+s.Max-1)/s.Max))
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", cells-filled)
}

func wardPanel(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64]) core.Node {
	return core.View{
		Style: core.Style{Gap: 6},
		Children: []core.Node{
			core.Text{
				TestID:      "health-ward",
				Style:       core.Style{FontFamily: core.FontMonospace, FontSize: 12},
				ContentFunc: func() string { _ = rev.Get(); return Summary(ed) },
			},
			ui.Button{
				TestID: "health-heal-all",
				Text:   "Heal All",
				Disabled: func() bool {
					_ = rev.Get()
					return ed.Playing() || len(health.Objects(ed.Doc)) == 0
				},
				OnPress: func() { ed.Report(HealAll(ed)) },
			},
		},
	}
}

func inspector(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64], id string) core.Node {
	playing := func() bool { _ = rev.Get(); return ed.Playing() }
	field := func(testID, label, name string) core.Node {
		return ui.NumberInput{
			TestID:   testID,
			Label:    label,
			Step:     1,
			Disabled: playing,
			Value: func() float64 {
				_ = rev.Get()
				v, _ := ed.ComponentField(id, health.Name, name)
				n, _ := v.(int)
				return float64(n)
			},
			OnChange: func(v float64) { ed.Report(ed.SetComponentField(id, health.Name, name, int(v))) },
		}
	}
	return core.View{
		Style: core.Style{Gap: 6},
		Children: []core.Node{
			field("health-hp", "Hit points", health.FieldHP),
			field("health-max", "Max", health.FieldMax),
			core.Text{
				TestID: "health-bar",
				Style:  core.Style{FontFamily: core.FontMonospace, FontSize: 12},
				ContentFunc: func() string {
					_ = rev.Get()
					s := stats(ed, id)
					return fmt.Sprintf("%s %d of %d", Bar(s), s.HP, s.Max)
				},
			},
		},
	}
}

// admit places a patient with AdmitHP hit points where the viewport is pressed, on the active layer.
func admit(ed *editor.Editor, x, y float32) {
	wx, wy := ed.CanvasToWorld(x, y)
	_, err := ed.PlaceObject(ed.ActiveLayer, sceneformat.ObjectDoc{
		Name:       "Patient",
		X:          wx,
		Y:          wy,
		Components: map[string]any{health.Name: map[string]any{health.FieldHP: AdmitHP}},
	})
	ed.Report(err)
}

// drawPatients outlines every top-level patient while the Admit tool is active, red when it is at a quarter of its max or below.
func drawPatients(ed *editor.Editor, d *core.Drawing) {
	if ed.Doc == nil {
		return
	}
	const half = 12
	for _, layer := range ed.Doc.Layers {
		for i := range layer.Objects {
			o := &layer.Objects[i]
			s, ok, err := health.Read(o)
			if !ok || err != nil {
				continue
			}
			color := core.RGBA(0x22C55E, 1)
			if s.HP*4 <= s.Max {
				color = core.RGBA(0xEF4444, 1)
			}
			d.SetStrokeColor(color).StrokeRect(o.X-half, o.Y-half, 2*half, 2*half)
		}
	}
}

// HealAll sets every patient's hit points to its max as one undoable step, and returns editor.ErrPlaying while the scene plays. Patients already at their max are left as they are, and it adds no step when every patient is.
func HealAll(ed *editor.Editor) error {
	if ed.Playing() {
		return editor.ErrPlaying
	}
	cmd := &healAll{ed: ed, before: map[string]any{}}
	for _, id := range health.Objects(ed.Doc) {
		s := stats(ed, id)
		if s.HP >= s.Max {
			continue
		}
		o, _, _ := ed.FindObject(id)
		comp, _ := o.Components[health.Name].(map[string]any)
		before, set := comp[health.FieldHP]
		if !set {
			before = nil
		}
		cmd.ids = append(cmd.ids, id)
		cmd.before[id] = before
		cmd.max = append(cmd.max, s.Max)
	}
	if len(cmd.ids) == 0 {
		return nil
	}
	ed.History.Execute(cmd)
	return nil
}

// healAll sets the hit points of ids to max, and puts back what each had before: a value, or nil when the scene left the field unset.
type healAll struct {
	ed     *editor.Editor
	ids    []string
	max    []int
	before map[string]any
}

func (c *healAll) Label() string { return "Heal All" }

func (c *healAll) Do() {
	for i, id := range c.ids {
		c.set(id, c.max[i])
	}
}

func (c *healAll) Undo() {
	for _, id := range c.ids {
		c.set(id, c.before[id])
	}
}

// set gives objectID's health component a new map with hp set to v, or without hp when v is nil, so a map an earlier step holds is not changed under it.
func (c *healAll) set(objectID string, v any) {
	o, _, _ := c.ed.FindObject(objectID)
	if o == nil {
		return
	}
	old, _ := o.Components[health.Name].(map[string]any)
	next := make(map[string]any, len(old)+1)
	for k, val := range old {
		next[k] = val
	}
	if v == nil {
		delete(next, health.FieldHP)
	} else {
		next[health.FieldHP] = v
	}
	o.Components[health.Name] = next
}
