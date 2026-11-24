// Package healthplugin is Field Medic's scene editor plugin for its health component (package health): a Ward panel listing every patient's hit points with a button that heals them all in one undoable step, an inspector that edits a patient's hit points with a bar of what is left, and an Admit tool that places a new patient where the viewport is pressed. The game's own editor command (cmd/editor) registers it, and game.yaml turns it on under editor.plugins.
package healthplugin

import (
	"fmt"
	"maps"
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

// stats reads the health of the patient objectID as the game does, through health.Read: an unset field reads its default and hp is capped at max. It returns zero stats when objectID is not a top-level object or its component does not match the type.
func stats(ed *editor.Editor, objectID string) health.Stats {
	o, _, _ := ed.FindObject(objectID)
	if o == nil {
		return health.Stats{}
	}
	s, _, _ := health.Read(o)
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
	// The steppers and scrub stay within the bounds the component type registers, as the generic fields do.
	field := func(testID, label, name string, lo float64) core.Node {
		return ui.NumberInput{
			TestID:   testID,
			Label:    label,
			Step:     1,
			Min:      lo,
			Max:      health.Limit,
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
			field("health-hp", "Hit points", health.FieldHP, 0),
			field("health-max", "Max", health.FieldMax, 1),
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

// worldLayer reports whether layer draws in world space, under the scene's camera, rather than in screen space.
func worldLayer(layer *sceneformat.LayerDoc) bool {
	return layer.EffectiveSpace() == sceneformat.SpaceWorld
}

// admit places a patient with AdmitHP hit points where the viewport is pressed, on the active layer: at the world point under the press on a world-space layer, and at the press itself on a screen-space one.
func admit(ed *editor.Editor, x, y float32) {
	wx, wy := x, y
	if ed.Doc != nil {
		for i := range ed.Doc.Layers {
			if ed.Doc.Layers[i].Name == ed.ActiveLayer && worldLayer(&ed.Doc.Layers[i]) {
				wx, wy = ed.CanvasToWorld(x, y)
			}
		}
	}
	_, err := ed.PlaceObject(ed.ActiveLayer, sceneformat.ObjectDoc{
		Name:       "Patient",
		X:          wx,
		Y:          wy,
		Components: map[string]any{health.Name: map[string]any{health.FieldHP: AdmitHP}},
	})
	ed.Report(err)
}

// OutlineHalf is half the side of the square the Admit tool outlines each patient with, in world units.
const OutlineHalf = 12

// drawPatients outlines every patient while the Admit tool is active, red when it is at a quarter of its max or below. A tool draws in canvas coordinates, so each corner of a patient on a world-space layer goes through the scene's camera (Editor.WorldToCanvas), and the outline pans, zooms and turns with the patient.
func drawPatients(ed *editor.Editor, d *core.Drawing) {
	if ed.Doc == nil {
		return
	}
	for li := range ed.Doc.Layers {
		layer := &ed.Doc.Layers[li]
		world := worldLayer(layer)
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
			d.SetStrokeColor(color).BeginPath()
			corners := [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
			for k, c := range corners {
				x, y := o.X+c[0]*OutlineHalf, o.Y+c[1]*OutlineHalf
				if world {
					x, y = ed.WorldToCanvas(x, y)
				}
				if k == 0 {
					d.MoveTo(x, y)
				} else {
					d.LineTo(x, y)
				}
			}
			d.ClosePath().Stroke()
		}
	}
}

// HealAll sets every patient's hit points to its max as one undoable step, and returns editor.ErrPlaying while the scene plays. Patients already at their max are left as they are, and it adds no step when every patient is.
func HealAll(ed *editor.Editor) error {
	if ed.Playing() {
		return editor.ErrPlaying
	}
	cmd := &healAll{ed: ed}
	for _, id := range health.Objects(ed.Doc) {
		s := stats(ed, id)
		if s.HP >= s.Max {
			continue
		}
		o, _, _ := ed.FindObject(id)
		cmd.ids = append(cmd.ids, id)
		cmd.before = append(cmd.before, o.Components[health.Name])
		cmd.max = append(cmd.max, s.Max)
	}
	if len(cmd.ids) == 0 {
		return nil
	}
	ed.History.Execute(cmd)
	return nil
}

// healAll sets the hit points of ids to max, and puts back each patient's component exactly as it was before: the same mapping, or the null a scene writes as `health:` with no fields.
type healAll struct {
	ed     *editor.Editor
	ids    []string
	max    []int
	before []any
}

func (c *healAll) Label() string { return "Heal All" }

// Do gives each patient's health component a new mapping with hp set to its max, so the mapping Undo puts back is not changed under it.
func (c *healAll) Do() {
	for i, id := range c.ids {
		o, _, _ := c.ed.FindObject(id)
		if o == nil {
			continue
		}
		old, _ := c.before[i].(map[string]any)
		next := make(map[string]any, len(old)+1)
		maps.Copy(next, old)
		next[health.FieldHP] = c.max[i]
		o.Components[health.Name] = next
	}
}

func (c *healAll) Undo() {
	for i, id := range c.ids {
		if o, _, _ := c.ed.FindObject(id); o != nil {
			o.Components[health.Name] = c.before[i]
		}
	}
}
