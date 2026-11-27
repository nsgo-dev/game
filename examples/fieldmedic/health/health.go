// Package health is Field Medic's health component: the hit points a patient has and the most it can hold, as a scene file declares them under an object's components. The game reads it to play, and the editor's health plugin (package healthplugin) reads and edits it.
package health

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/plugin"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// Name is the component's key under an object's components in a scene file and the runtime plugin's name.
const Name = "health"

// Module is the Go module path of the Field Medic health plugin.
const Module = "github.com/nsgo-dev/game/examples/fieldmedic"

// The component's field names: FieldHP holds the hit points a patient has, FieldMax the most it can be healed to.
const (
	FieldHP  = "hp"
	FieldMax = "max"
)

// The fields' defaults and bounds, which the component type registers.
const (
	DefaultHP  = 10
	DefaultMax = 10
	Limit      = 99
)

// Type returns the health component type: hp from 0 to Limit, and max from 1 to Limit, with a Spawn factory that attaches typed Stats to the spawned entity with hp clamped to max.
func Type() sceneformat.ComponentType {
	return sceneformat.ComponentType{
		Name: Name,
		Doc:  "Hit points a patient has and the most it can be healed to.",
		Fields: []sceneformat.Field{
			{Name: FieldHP, Kind: sceneformat.FieldInt, Default: DefaultHP, Min: sceneformat.Float64(0), Max: sceneformat.Float64(Limit), Doc: "Hit points left; a patient at 0 is lost."},
			{Name: FieldMax, Kind: sceneformat.FieldInt, Default: DefaultMax, Min: sceneformat.Float64(1), Max: sceneformat.Float64(Limit), Doc: "The most hit points healing brings the patient to."},
		},
		Spawn: spawn,
	}
}

func spawn(w *entities.World, e entities.Entity, fields map[string]any) error {
	hp, ok := fields[FieldHP].(int)
	if !ok {
		return fmt.Errorf("health: field %q must be int, got %T", FieldHP, fields[FieldHP])
	}
	maxHP, ok := fields[FieldMax].(int)
	if !ok {
		return fmt.Errorf("health: field %q must be int, got %T", FieldMax, fields[FieldMax])
	}
	entities.Set(w, e, Stats{HP: min(hp, maxHP), Max: maxHP})
	return nil
}

// Plugin returns the runtime plugin for the health component and its world snapshot codec, for plugin.Install.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		Name:       Name,
		Module:     Module,
		API:        plugin.APIVersion,
		Components: []sceneformat.ComponentType{Type()},
		Codecs: func(w *entities.World) error {
			return entities.RegisterCodec[Stats](w, Name)
		},
	}
}

// Components returns a registry holding the health component, the component types Field Medic's scenes use, for sceneformat.ParseWith and the editor's SetComponentRegistry.
func Components() *sceneformat.ComponentRegistry {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(Type())
	return reg
}

// registry resolves the defaults Read fills in.
var registry = Components()

// Stats are a patient's hit points with the component's defaults filled in.
type Stats struct {
	// HP is the hit points the patient has, at most Max.
	HP int
	// Max is the most hit points healing brings the patient to.
	Max int
}

// Read returns the stats of o's health component, with the defaults of the fields it leaves unset and hp lowered to max, and false when o has no health component. It returns an error when the component does not match Type, which ParseWith with Components already refuses when it loads a scene.
func Read(o *sceneformat.ObjectDoc) (Stats, bool, error) {
	raw, ok := o.Components[Name]
	if !ok {
		return Stats{}, false, nil
	}
	data, _ := raw.(map[string]any)
	if raw != nil && data == nil {
		return Stats{}, true, fmt.Errorf("health: object %q: component is %T, want a mapping", o.ID, raw)
	}
	fields, err := registry.Resolve(Name, data)
	if err != nil {
		return Stats{}, true, fmt.Errorf("health: object %q: %w", o.ID, err)
	}
	s := Stats{HP: fields[FieldHP].(int), Max: fields[FieldMax].(int)}
	s.HP = min(s.HP, s.Max)
	return s, true, nil
}

// Objects returns the IDs of the patients in doc: its top-level objects with a health component, in document order. A nested child with a health component is not a patient: the game plays only top-level objects, and the editor's FindObject and ComponentField see only those.
func Objects(doc *sceneformat.SceneDoc) []string {
	if doc == nil {
		return nil
	}
	var ids []string
	for _, layer := range doc.Layers {
		for i := range layer.Objects {
			if _, ok := layer.Objects[i].Components[Name]; ok {
				ids = append(ids, layer.Objects[i].ID)
			}
		}
	}
	return ids
}
