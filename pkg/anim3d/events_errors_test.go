package anim3d_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/anim3d"
	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/loaderr"
)

func TestEventLoadersLocateErrors(t *testing.T) {
	cases := []struct {
		name        string
		load        func([]byte) (map[string][]anim3d.ClipEvent, error)
		file        string
		data        string
		line        int
		wantField   string
		wantProblem string
	}{
		{
			name: "sidecar syntax",
			load: anim3d.LoadEventsSidecar,
			file: "hero.events.json",
			data: "{\n  \"version\": 1,\n  \"clips\": {\n    \"run\": [\n      {\"time\": 0.5,, \"name\": \"step\"}\n    ]\n  }\n}",
			line: 5,
		},
		{
			name: "sidecar trailing data",
			load: anim3d.LoadEventsSidecar,
			file: "hero.events.json",
			data: "{\n  \"version\": 1,\n  \"clips\": {}\n}\nextra",
			line: 5,
		},
		{
			name:        "sidecar null root",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "\nnull",
			line:        2,
			wantProblem: "null",
		},
		{
			name: "sidecar scalar root",
			load: anim3d.LoadEventsSidecar,
			file: "hero.events.json",
			data: "\n123",
			line: 2,
		},
		{
			name:        "sidecar missing version",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "{\n  \"clips\": {\n    \"run\": []\n  }\n}",
			line:        1,
			wantField:   "version",
			wantProblem: "missing sidecar version",
		},
		{
			name:        "sidecar zero version",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "{\n  \"version\": 0,\n  \"clips\": {}\n}",
			line:        2,
			wantField:   "version",
			wantProblem: "unsupported sidecar version 0",
		},
		{
			name:        "sidecar future version",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "{\n  \"version\": 7,\n  \"clips\": {}\n}",
			line:        2,
			wantField:   "version",
			wantProblem: "unsupported sidecar version 7",
		},
		{
			name:      "sidecar version wrong type",
			load:      anim3d.LoadEventsSidecar,
			file:      "hero.events.json",
			data:      "{\n  \"version\": \"1\",\n  \"clips\": {}\n}",
			line:      2,
			wantField: "version",
		},
		{
			name:        "sidecar misspelled clips",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "{\n  \"version\": 1,\n  \"clip\": {\n    \"attack\": []\n  }\n}",
			line:        3,
			wantField:   "clip",
			wantProblem: `unknown field "clip"`,
		},
		{
			name:        "sidecar unversioned flat map",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "{\n  \"run\": [\n    {\"time\": 0.1, \"name\": \"step\"}\n  ]\n}",
			line:        2,
			wantField:   "run",
			wantProblem: `unknown field "run"`,
		},
		{
			name:        "sidecar unknown top-level key",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "{\n  \"version\": 1,\n  \"clips\": {},\n  \"extra\": true\n}",
			line:        4,
			wantField:   "extra",
			wantProblem: `unknown field "extra"`,
		},
		{
			name:        "sidecar unknown event key",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "{\n  \"version\": 1,\n  \"clips\": {\n    \"attack\": [\n      {\n        \"tme\": 0.4,\n        \"name\": \"hit\"\n      }\n    ]\n  }\n}",
			line:        6,
			wantField:   "clips.attack[0].tme",
			wantProblem: `unknown field "tme"`,
		},
		{
			name:        "sidecar unknown event key shadowed in data",
			load:        anim3d.LoadEventsSidecar,
			file:        "hero.events.json",
			data:        "{\n  \"version\": 1,\n  \"clips\": {\n    \"attack\": [\n      {\n        \"tme\": 0.4,\n        \"name\": \"hit\",\n        \"data\": {\"tme\": 1}\n      }\n    ]\n  }\n}",
			line:        6,
			wantField:   "clips.attack[0].tme",
			wantProblem: `unknown field "tme"`,
		},
		{
			name:      "sidecar string time",
			load:      anim3d.LoadEventsSidecar,
			file:      "hero.events.json",
			data:      "{\n  \"version\": 1,\n  \"clips\": {\n    \"run\": [\n      {\"time\": \"0.4\", \"name\": \"step\"}\n    ]\n  }\n}",
			line:      5,
			wantField: "clips.run[0].time",
		},
		{
			name:      "sidecar clip events not an array",
			load:      anim3d.LoadEventsSidecar,
			file:      "hero.events.json",
			data:      "{\n  \"version\": 1,\n  \"clips\": {\n    \"run\": \"bad\"\n  }\n}",
			line:      4,
			wantField: "clips.run",
		},
		{
			name:      "sidecar null event entry",
			load:      anim3d.LoadEventsSidecar,
			file:      "hero.events.json",
			data:      "{\n  \"version\": 1,\n  \"clips\": {\n    \"run\": [\n      null\n    ]\n  }\n}",
			line:      5,
			wantField: "clips.run[0]",
		},
		{
			name:      "sidecar duplicate version key",
			load:      anim3d.LoadEventsSidecar,
			file:      "hero.events.json",
			data:      "{\n  \"version\": 2,\n  \"version\": 1,\n  \"clips\": {}\n}",
			line:      3,
			wantField: "version",
		},
		{
			name: "gltf syntax",
			load: anim3d.LoadEventsFromGLTFExtras,
			file: "hero.gltf",
			data: "{\n  \"animations\": [\n    {\"name\": \"run\"\n  ]\n}",
			line: 4,
		},
		{
			name:      "gltf extras events string time",
			load:      anim3d.LoadEventsFromGLTFExtras,
			file:      "hero.gltf",
			data:      "{\n  \"animations\": [\n    {\n      \"name\": \"idle\",\n      \"extras\": {\"blender\": \"4.2\"}\n    },\n    {\n      \"name\": \"attack\",\n      \"extras\": {\n        \"events\": [\n          {\n            \"time\": \"0.4\",\n            \"name\": \"hit\"\n          }\n        ]\n      }\n    }\n  ]\n}",
			line:      12,
			wantField: "animations[1].extras.events[0].time",
		},
		{
			name:      "gltf extras nsgo_events string time",
			load:      anim3d.LoadEventsFromGLTFExtras,
			file:      "hero.gltf",
			data:      "{\n  \"animations\": [\n    {\n      \"name\": \"attack\",\n      \"extras\": {\n        \"nsgo_events\": [\n          {\n            \"time\": \"0.4\",\n            \"name\": \"hit\"\n          }\n        ]\n      }\n    }\n  ]\n}",
			line:      8,
			wantField: "animations[0].extras.nsgo_events[0].time",
		},
		{
			name:        "gltf extras unknown event key",
			load:        anim3d.LoadEventsFromGLTFExtras,
			file:        "hero.gltf",
			data:        "{\n  \"animations\": [\n    {\n      \"name\": \"attack\",\n      \"extras\": {\n        \"events\": [\n          {\n            \"tme\": 0.4,\n            \"name\": \"hit\"\n          }\n        ]\n      }\n    }\n  ]\n}",
			line:        8,
			wantField:   "animations[0].extras.events[0].tme",
			wantProblem: `unknown field "tme"`,
		},
		{
			name:      "gltf extras events not an array",
			load:      anim3d.LoadEventsFromGLTFExtras,
			file:      "hero.gltf",
			data:      "{\n  \"animations\": [\n    {\n      \"name\": \"attack\",\n      \"extras\": {\n        \"events\": {\"time\": 0.4, \"name\": \"hit\"}\n      }\n    }\n  ]\n}",
			line:      6,
			wantField: "animations[0].extras.events",
		},
		{
			name:      "gltf extras null event entry",
			load:      anim3d.LoadEventsFromGLTFExtras,
			file:      "hero.gltf",
			data:      "{\n  \"animations\": [\n    {\n      \"name\": \"attack\",\n      \"extras\": {\n        \"events\": [\n          null\n        ]\n      }\n    }\n  ]\n}",
			line:      7,
			wantField: "animations[0].extras.events[0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.load([]byte(tc.data))
			e := loaderrtest.Expect(t, loaderr.WithFile(err, tc.file), tc.file, tc.line)
			if tc.wantField != "" && e.Field != tc.wantField {
				t.Errorf("Field = %q, want %q (err: %v)", e.Field, tc.wantField, err)
			}
			if tc.wantProblem != "" && !strings.Contains(e.Problem, tc.wantProblem) {
				t.Errorf("Problem = %q, want substring %q (err: %v)", e.Problem, tc.wantProblem, err)
			}
		})
	}
}

func TestEventsSidecarRoundTripAndValidCases(t *testing.T) {
	input := map[string][]anim3d.ClipEvent{
		"Run": {
			{Time: 0.75, Name: "foot_r", Payload: "right"},
			{Time: 0.25, Name: "foot_l", Payload: "left", Data: map[string]any{"surface": "stone", "volume": 0.9}},
		},
		"run": {
			{Time: 0.5, Name: "half"},
		},
		"empty": {},
	}
	data, err := anim3d.MarshalEventsSidecar(input)
	if err != nil {
		t.Fatalf("MarshalEventsSidecar: %v", err)
	}
	got, err := anim3d.LoadEventsSidecar(data)
	if err != nil {
		t.Fatalf("LoadEventsSidecar: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("clip count = %d, want 3", len(got))
	}
	if len(got["Run"]) != 2 || got["Run"][0].Name != "foot_l" || got["Run"][1].Name != "foot_r" {
		t.Fatalf("Run events = %+v, want sorted [foot_l foot_r]", got["Run"])
	}
	if got["Run"][0].Data["surface"] != "stone" {
		t.Fatalf("Run[0].Data = %+v, want surface=stone", got["Run"][0].Data)
	}
	if len(got["run"]) != 1 || got["run"][0].Name != "half" {
		t.Fatalf("run events = %+v, want [half]", got["run"])
	}
	if len(got["empty"]) != 0 {
		t.Fatalf("empty events = %+v, want empty", got["empty"])
	}

	// Empty clips map and omitted clips key with version 1 both load cleanly.
	for _, raw := range []string{`{"version": 1, "clips": {}}`, `{"version": 1}`} {
		got, err := anim3d.LoadEventsSidecar([]byte(raw))
		if err != nil {
			t.Fatalf("LoadEventsSidecar(%s): %v", raw, err)
		}
		if len(got) != 0 {
			t.Fatalf("LoadEventsSidecar(%s) = %+v, want empty", raw, got)
		}
	}
}

func TestLoadEventsFromGLTFExtrasIgnoresNonObjectAndUnrelatedKeys(t *testing.T) {
	gltfJSON := []byte(`{
		"animations": [
			{"name": "no_extras"},
			{"name": "null_extras", "extras": null},
			{"name": "string_extras", "extras": "exported by blender"},
			{"name": "number_extras", "extras": 42},
			{"name": "array_extras", "extras": [1, 2, 3]},
			{"name": "bool_extras", "extras": true},
			{"name": "unrelated_object", "extras": {"blender_action": "Idle", "tags": ["loop"], "custom": {"nested": 1}}},
			{
				"name": "mixed_object",
				"extras": {
					"blender_action": "Attack",
					"events": [{"time": 0.4, "name": "hit", "data": {"damage": 25}}],
					"nsgo_events": [{"time": 0.1, "name": "windup", "payload": "whoosh"}]
				}
			}
		]
	}`)

	got, err := anim3d.LoadEventsFromGLTFExtras(gltfJSON)
	if err != nil {
		t.Fatalf("LoadEventsFromGLTFExtras: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded %d clips, want 1: %+v", len(got), got)
	}
	evs := got["mixed_object"]
	if len(evs) != 2 || evs[0].Name != "windup" || evs[1].Name != "hit" {
		t.Fatalf("mixed_object events = %+v, want [windup hit]", evs)
	}
}

func TestClipFromGLTFExtrasValidation(t *testing.T) {
	t.Run("malformed extras events return loaderr naming animation index", func(t *testing.T) {
		doc := &gltf.Document{
			Animations: []gltf.Animation{
				{Name: "Idle", Duration: 1.0, Extras: json.RawMessage(`{"blender": "ok"}`)},
				{Name: "Attack", Duration: 1.0, Extras: json.RawMessage(`{"events": [{"time": "0.4", "name": "hit"}]}`)},
			},
		}
		if _, err := anim3d.ClipFromGLTF(doc, 0); err != nil {
			t.Fatalf("ClipFromGLTF(0) with unrelated extras failed: %v", err)
		}
		_, err := anim3d.ClipFromGLTF(doc, 1)
		if err == nil {
			t.Fatal("ClipFromGLTF(1) succeeded on string time, want error")
		}
		e, ok := loaderr.As(err)
		if !ok {
			t.Fatalf("err = %v (%T), want *loaderr.Error", err, err)
		}
		if e.Field != "animations[1].extras.events[0].time" {
			t.Errorf("Field = %q, want %q", e.Field, "animations[1].extras.events[0].time")
		}
		if e.Fix == "" {
			t.Errorf("Fix is empty (err: %v)", err)
		}
		if _, err := anim3d.ClipsFromGLTF(doc); err == nil {
			t.Fatal("ClipsFromGLTF succeeded on malformed extras, want error")
		}
	})

	t.Run("malformed extras unknown key returns loaderr", func(t *testing.T) {
		doc := &gltf.Document{
			Animations: []gltf.Animation{
				{Name: "Attack", Duration: 1.0, Extras: json.RawMessage(`{"nsgo_events": [{"tme": 0.4, "name": "hit"}]}`)},
			},
		}
		_, err := anim3d.ClipFromGLTF(doc, 0)
		if err == nil {
			t.Fatal("ClipFromGLTF succeeded on unknown event key, want error")
		}
		e, ok := loaderr.As(err)
		if !ok {
			t.Fatalf("err = %v (%T), want *loaderr.Error", err, err)
		}
		if e.Field != "animations[0].extras.nsgo_events[0].tme" {
			t.Errorf("Field = %q, want %q", e.Field, "animations[0].extras.nsgo_events[0].tme")
		}
		if !strings.Contains(e.Problem, `unknown field "tme"`) {
			t.Errorf("Problem = %q, want unknown field", e.Problem)
		}
	})

	t.Run("non-object extras are ignored", func(t *testing.T) {
		doc := &gltf.Document{
			Animations: []gltf.Animation{
				{Name: "Idle", Duration: 1.0, Extras: json.RawMessage(`"just a note"`)},
				{Name: "Walk", Duration: 1.0, Extras: json.RawMessage(`[1, 2, 3]`)},
			},
		}
		clips, err := anim3d.ClipsFromGLTF(doc)
		if err != nil {
			t.Fatalf("ClipsFromGLTF failed on non-object extras: %v", err)
		}
		if len(clips) != 2 || len(clips[0].Events) != 0 || len(clips[1].Events) != 0 {
			t.Fatalf("unexpected clips/events: %+v", clips)
		}
	})
}
