package health_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/examples/fieldmedic/health"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func TestReadFillsDefaultsAndClampsToMax(t *testing.T) {
	cases := []struct {
		name string
		comp any
		want health.Stats
	}{
		{"unset", map[string]any{}, health.Stats{HP: health.DefaultHP, Max: health.DefaultMax}},
		{"empty", nil, health.Stats{HP: health.DefaultHP, Max: health.DefaultMax}},
		{"hp only", map[string]any{"hp": 4}, health.Stats{HP: 4, Max: health.DefaultMax}},
		{"both", map[string]any{"hp": 3, "max": 12}, health.Stats{HP: 3, Max: 12}},
		{"above max", map[string]any{"hp": 20, "max": 12}, health.Stats{HP: 12, Max: 12}},
	}
	for _, c := range cases {
		o := &sceneformat.ObjectDoc{ID: "p", Components: map[string]any{health.Name: c.comp}}
		got, ok, err := health.Read(o)
		if err != nil || !ok || got != c.want {
			t.Errorf("%s: Read = %+v, %v, %v; want %+v, true, nil", c.name, got, ok, err, c.want)
		}
	}
}

func TestReadReportsObjectsWithoutHealthAndBadComponents(t *testing.T) {
	if _, ok, err := health.Read(&sceneformat.ObjectDoc{ID: "rock"}); ok || err != nil {
		t.Fatalf("Read of an object without health = %v, %v; want false, nil", ok, err)
	}
	for name, comp := range map[string]any{
		"not a mapping": 3,
		"unknown field": map[string]any{"armor": 2},
		"out of range":  map[string]any{"hp": -1},
		"wrong kind":    map[string]any{"max": "lots"},
	} {
		_, ok, err := health.Read(&sceneformat.ObjectDoc{ID: "p", Components: map[string]any{health.Name: comp}})
		if !ok || err == nil || !strings.Contains(err.Error(), `"p"`) {
			t.Errorf("%s: Read = %v, %v; want true and an error naming the object", name, ok, err)
		}
	}
}

func TestComponentsChecksScenesOnLoad(t *testing.T) {
	_, err := sceneformat.ParseWith([]byte("version: 1\nlayers:\n  - name: ward\n    objects:\n      - id: p\n        components:\n          health:\n            hpp: 3\n"), sceneformat.ParseOptions{File: "ward.yaml", Components: health.Components()})
	if err == nil || !strings.Contains(err.Error(), "hpp") || !strings.Contains(err.Error(), "ward.yaml") {
		t.Fatalf("a misspelled field loaded: %v", err)
	}
}

func TestObjectsListsTopLevelPatientsInDocumentOrder(t *testing.T) {
	doc := &sceneformat.SceneDoc{Layers: []sceneformat.LayerDoc{
		{Name: "a", Objects: []sceneformat.ObjectDoc{
			{ID: "bed", Children: []sceneformat.ObjectDoc{{ID: "child", Components: map[string]any{health.Name: map[string]any{}}}}},
			{ID: "ada", Components: map[string]any{health.Name: map[string]any{}}},
		}},
		{Name: "b", Objects: []sceneformat.ObjectDoc{{ID: "bram", Components: map[string]any{health.Name: map[string]any{}}}}},
	}}
	if got, want := health.Objects(doc), []string{"ada", "bram"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Objects = %v, want %v", got, want)
	}
	if got := health.Objects(nil); got != nil {
		t.Fatalf("Objects(nil) = %v", got)
	}
}
