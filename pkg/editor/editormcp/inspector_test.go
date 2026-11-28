package editormcp_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestObjectSetFieldAndLocateYAML(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	f.ed.SelectObject("hero")

	// Locate selected object's exact 1-based YAML line and trigger Open YAML hook.
	var openedPath string
	var openedLine int
	f.ed.OnOpenFileAtLine = func(path string, line int) {
		openedPath = path
		openedLine = line
	}
	locRes := call(t, cs, editormcp.ToolObjectLocateYAML, map[string]any{"open": true})
	mustOK(t, locRes)
	var loc editormcp.ObjectYAMLLocation
	structured(t, locRes, &loc)
	if loc.Object != "hero" || loc.Line <= 1 || !loc.Opened {
		t.Fatalf("object_locate_yaml = %+v", loc)
	}
	if openedPath != loc.File || openedLine != loc.Line {
		t.Fatalf("OnOpenFileAtLine got (%q, %d), want (%q, %d)", openedPath, openedLine, loc.File, loc.Line)
	}
	if !strings.Contains(loc.YAML, "- id: hero") {
		t.Fatalf("object_locate_yaml YAML missing hero:\n%s", loc.YAML)
	}

	// Edit object field via object_set_field; outside an agent turn it names its object, since the selection is never a write target.
	beforeIdx := f.ed.History.Index()
	res := call(t, cs, editormcp.ToolObjectSetField, map[string]any{
		"id":    "hero",
		"field": "x",
		"value": 128,
	})
	mustOK(t, res)
	var out editormcp.ObjectFieldResult
	structured(t, res, &out)
	if out.Object != "hero" || out.Field != "x" {
		t.Fatalf("object_set_field x = %+v", out)
	}
	hero, _, _ := f.ed.FindObject("hero")
	if hero.X != 128 {
		t.Fatalf("hero.X = %v, want 128", hero.X)
	}
	if f.ed.History.Index() != beforeIdx+1 {
		t.Fatalf("history index = %d, want %d", f.ed.History.Index(), beforeIdx+1)
	}

	// Edit component field via object_set_field.
	res = call(t, cs, editormcp.ToolObjectSetField, map[string]any{
		"id":        "hero",
		"component": "body",
		"field":     "speed",
		"value":     45,
	})
	mustOK(t, res)
	if got := hero.Components["body"].(map[string]any)["speed"]; got != 45.0 {
		t.Fatalf("hero.body.speed = %#v, want 45.0", got)
	}

	// Edit custom property via object_set_field.
	res = call(t, cs, editormcp.ToolObjectSetField, map[string]any{
		"id":       "hero",
		"property": true,
		"field":    "tier",
		"value":    3,
	})
	mustOK(t, res)
	if got := hero.Properties["tier"]; got != 3 {
		t.Fatalf("hero.properties.tier = %#v, want 3", got)
	}

	// Edit 3D light and camera via scene_edit parity.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{
				"op": "set",
				"id": "hero",
				"fields": map[string]any{
					"light": map[string]any{"kind": "point", "color": "#ffd2a1", "intensity": 4, "range": 8},
				},
			},
		},
	}))
	if hero.Light == nil || hero.Light.Kind != "point" {
		t.Fatalf("hero.Light after scene_edit = %+v", hero.Light)
	}

	// Undo steps cleanly revert.
	f.ed.Undo() // light
	f.ed.Undo() // tier property
	f.ed.Undo() // body.speed
	f.ed.Undo() // x
	if hero.X != 64 || hero.Light != nil {
		t.Fatalf("after undoing 4 edits: x=%v light=%+v", hero.X, hero.Light)
	}

	// object_locate_yaml with open=true reports Opened=false when OnOpenFileAtLine is nil
	// and refuses while playing.
	f.ed.OnOpenFileAtLine = nil
	locRes = call(t, cs, editormcp.ToolObjectLocateYAML, map[string]any{"open": true})
	mustOK(t, locRes)
	var locNoHook editormcp.ObjectYAMLLocation
	structured(t, locRes, &locNoHook)
	if locNoHook.Opened {
		t.Fatalf("object_locate_yaml without OnOpenFileAtLine set Opened=true: %+v", locNoHook)
	}

	var rootScope *core.Scope
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(800, 600))
	if err := f.ed.PlayIn(rootScope); err != nil {
		t.Fatalf("PlayIn: %v", err)
	}
	defer func() { _ = f.ed.Stop() }()
	playRes := call(t, cs, editormcp.ToolObjectLocateYAML, map[string]any{"open": true})
	if !playRes.IsError {
		t.Fatalf("expected object_locate_yaml open=true to fail during Play mode, got %+v", playRes)
	}
}
