package sceneformat

// BehaviorComponent returns the schema for an input-driven controller component.
func BehaviorComponent(name string) (ComponentType, bool) {
	fields := map[string][]Field{
		"platform":        {{Name: "version", Kind: FieldInt, Default: 1}, {Name: "left", Kind: FieldString}, {Name: "right", Kind: FieldString}, {Name: "jump", Kind: FieldString}},
		"eight_direction": {{Name: "version", Kind: FieldInt, Default: 1}, {Name: "maxSpeed", Kind: FieldFloat, Default: 120}, {Name: "acceleration", Kind: FieldFloat, Default: 800}, {Name: "deceleration", Kind: FieldFloat, Default: 1000}, {Name: "directions", Kind: FieldInt, Default: 8}, {Name: "setAngle", Kind: FieldBool, Default: false}, {Name: "axis", Kind: FieldString}},
		"tile_movement":   {{Name: "version", Kind: FieldInt, Default: 1}, {Name: "tileSize", Kind: FieldFloat, Default: 16}, {Name: "speed", Kind: FieldFloat, Default: 8}, {Name: "axis", Kind: FieldString}, {Name: "tilemapLayer", Kind: FieldString}},
		"car":             {{Name: "version", Kind: FieldInt, Default: 1}, {Name: "maxSpeed", Kind: FieldFloat, Default: 200}, {Name: "acceleration", Kind: FieldFloat, Default: 300}, {Name: "steerSpeed", Kind: FieldFloat, Default: 180}, {Name: "drift", Kind: FieldFloat, Default: .2}, {Name: "axis", Kind: FieldString}},
		"drag_drop":       {{Name: "version", Kind: FieldInt, Default: 1}, {Name: "axes", Kind: FieldString, Default: "both", Enum: []string{"x", "y", "both"}}},
		"turret":          {{Name: "version", Kind: FieldInt, Default: 1}, {Name: "range", Kind: FieldFloat, Default: 160}, {Name: "rotateSpeed", Kind: FieldFloat, Default: 180}, {Name: "fireRate", Kind: FieldFloat, Default: 2}, {Name: "targets", Kind: FieldStringList}},
		"line_of_sight":   {{Name: "version", Kind: FieldInt, Default: 1}, {Name: "range", Kind: FieldFloat, Default: 200}, {Name: "coneOfView", Kind: FieldFloat, Default: 90}, {Name: "obstacles", Kind: FieldString}},
		"path_agent":      {{Name: "version", Kind: FieldInt, Default: 1}, {Name: "maxSpeed", Kind: FieldFloat, Default: 80}, {Name: "rotateObject", Kind: FieldBool, Default: true}},
	}
	f, ok := fields[name]
	if !ok {
		return ComponentType{}, false
	}
	return ComponentType{Name: name, Doc: "Input-driven controller behavior", Fields: f}, true
}

// RegisterBehaviorComponents registers all built-in input behavior schemas.
func RegisterBehaviorComponents(reg *ComponentRegistry) error {
	for _, n := range []string{"platform", "eight_direction", "tile_movement", "car", "drag_drop", "turret", "line_of_sight", "path_agent"} {
		t, _ := BehaviorComponent(n)
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
