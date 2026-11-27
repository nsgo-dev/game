package sceneformat

import (
	"fmt"
	"reflect"
	"time"

	"github.com/nsgo-dev/game/pkg/jsonschema"
)

// ComponentTypeSchema returns a draft-07 JSON Schema for a single component mapping of type t. Every FieldKind is mapped to its JSON Schema representation, along with each field's description, default (when set), numeric bounds and string enum. Unknown field keys are refused.
func ComponentTypeSchema(t ComponentType) *jsonschema.Schema {
	props := make([]jsonschema.Property, 0, len(t.Fields))
	for _, f := range t.Fields {
		fs := fieldSchema(f)
		props = append(props, jsonschema.Property{Name: f.Name, Schema: fs})
	}
	return &jsonschema.Schema{
		Description:          t.Doc,
		Types:                []jsonschema.Type{jsonschema.TypeObject},
		Properties:           props,
		AdditionalProperties: jsonschema.False(),
	}
}

// JSONSchema returns the schema of a scene's components map for r's types, in Names order. When r is non-nil, unregistered component names are refused; when r is nil, any component mapping is accepted.
func (r *ComponentRegistry) JSONSchema() *jsonschema.Schema {
	if r == nil {
		return &jsonschema.Schema{
			Types:                []jsonschema.Type{jsonschema.TypeObject},
			AdditionalProperties: &jsonschema.Schema{},
		}
	}
	names := r.Names()
	props := make([]jsonschema.Property, 0, len(names))
	for _, name := range names {
		props = append(props, jsonschema.Property{
			Name:   name,
			Schema: ComponentTypeSchema(*r.types[name]),
		})
	}
	return &jsonschema.Schema{
		Types:                []jsonschema.Type{jsonschema.TypeObject},
		Properties:           props,
		AdditionalProperties: jsonschema.False(),
	}
}

// SceneSchema returns a draft-07 JSON Schema for scene YAML files (versions MinVersion through CurrentVersion) whose objects' components are checked against reg. When reg is non-nil, ObjectDoc.components and ObjectOverride.components reference reg.JSONSchema(), refusing unregistered component names and validating every registered field. When reg is nil, built-in scene components are typed while any custom component mapping remains allowed.
func SceneSchema(reg *ComponentRegistry) (*jsonschema.Schema, error) {
	s, err := jsonschema.Reflect(reflect.TypeFor[SceneDoc](), jsonschema.Options{
		Title: "NSGo game scene",
		Types: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[time.Duration](): {Types: []jsonschema.Type{jsonschema.TypeString, jsonschema.TypeInteger}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sceneformat: scene schema: %w", err)
	}
	if v := s.Property("version"); v != nil {
		v.Minimum = Float64(float64(MinVersion))
		v.Maximum = Float64(float64(CurrentVersion))
	}
	if conn := s.Definitions["ConnectionDoc"]; conn != nil {
		if args := conn.Property("args"); args != nil {
			args.AdditionalProperties = &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeBoolean, jsonschema.TypeNumber, jsonschema.TypeString}}
		}
	}
	var compSchema *jsonschema.Schema
	if reg != nil {
		compSchema = reg.JSONSchema()
	} else {
		compSchema = builtinComponentsSchema()
	}
	if s.Definitions == nil {
		s.Definitions = make(map[string]*jsonschema.Schema)
	}
	s.Definitions["Components"] = compSchema
	for _, defName := range []string{"ObjectDoc", "ObjectOverride"} {
		if def := s.Definitions[defName]; def != nil {
			if comps := def.Property("components"); comps != nil {
				*comps = jsonschema.Schema{Ref: "#/definitions/Components"}
			}
		}
	}
	return s, nil
}

// builtinComponentsSchema returns the components schema used by SceneSchema(nil): built-in physics, skeletal and 2D nav-region components are typed, and additionalProperties allows any project-defined component.
func builtinComponentsSchema() *jsonschema.Schema {
	reg := PhysicsComponentRegistry()
	_ = RegisterSkeletalComponent(reg)
	_ = RegisterNavRegion2DComponent(reg)
	s := reg.JSONSchema()
	s.AdditionalProperties = &jsonschema.Schema{}
	return s
}

func fieldSchema(f Field) *jsonschema.Schema {
	fs := &jsonschema.Schema{
		Description: f.Doc,
		Default:     schemaDefault(f),
	}
	switch f.Kind {
	case FieldBool:
		fs.Types = []jsonschema.Type{jsonschema.TypeBoolean}
	case FieldInt:
		fs.Types = []jsonschema.Type{jsonschema.TypeInteger}
		fs.Minimum = cloneFloatPtr(f.Min)
		fs.Maximum = cloneFloatPtr(f.Max)
	case FieldFloat:
		fs.Types = []jsonschema.Type{jsonschema.TypeNumber}
		fs.Minimum = cloneFloatPtr(f.Min)
		fs.Maximum = cloneFloatPtr(f.Max)
	case FieldString:
		fs.Types = []jsonschema.Type{jsonschema.TypeString}
		if len(f.Enum) > 0 {
			fs.Enum = make([]any, len(f.Enum))
			for i, v := range f.Enum {
				fs.Enum[i] = v
			}
		}
	case FieldStringList:
		fs.Types = []jsonschema.Type{jsonschema.TypeArray}
		fs.Items = &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeString}}
	case FieldPointList:
		fs.Types = []jsonschema.Type{jsonschema.TypeArray}
		two := 2
		fs.Items = &jsonschema.Schema{
			Types:    []jsonschema.Type{jsonschema.TypeArray},
			MinItems: &two,
			MaxItems: &two,
			Items:    &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeNumber}},
		}
	case FieldPoint:
		fs.Types = []jsonschema.Type{jsonschema.TypeObject}
		fs.Properties = pointProperties()
		fs.AdditionalProperties = jsonschema.False()
	case FieldRect:
		fs.Types = []jsonschema.Type{jsonschema.TypeObject}
		fs.Properties = rectProperties()
		fs.AdditionalProperties = jsonschema.False()
	case FieldCircle:
		fs.Types = []jsonschema.Type{jsonschema.TypeObject}
		fs.Properties = circleProperties()
		fs.AdditionalProperties = jsonschema.False()
	case FieldPolygon:
		fs.Types = []jsonschema.Type{jsonschema.TypeArray}
		fs.Items = &jsonschema.Schema{
			Types:                []jsonschema.Type{jsonschema.TypeObject},
			Properties:           pointProperties(),
			AdditionalProperties: jsonschema.False(),
		}
	}
	return fs
}

func pointProperties() []jsonschema.Property {
	return []jsonschema.Property{
		{Name: "x", Schema: &jsonschema.Schema{Description: "Horizontal coordinate in object-local space.", Types: []jsonschema.Type{jsonschema.TypeNumber}}},
		{Name: "y", Schema: &jsonschema.Schema{Description: "Vertical coordinate in object-local space.", Types: []jsonschema.Type{jsonschema.TypeNumber}}},
	}
}

func rectProperties() []jsonschema.Property {
	return []jsonschema.Property{
		{Name: "x", Schema: &jsonschema.Schema{Description: "Left edge coordinate in object-local space.", Types: []jsonschema.Type{jsonschema.TypeNumber}}},
		{Name: "y", Schema: &jsonschema.Schema{Description: "Top edge coordinate in object-local space.", Types: []jsonschema.Type{jsonschema.TypeNumber}}},
		{Name: "width", Schema: &jsonschema.Schema{Description: "Horizontal extent in object-local space (non-negative).", Types: []jsonschema.Type{jsonschema.TypeNumber}, Minimum: Float64(0)}},
		{Name: "height", Schema: &jsonschema.Schema{Description: "Vertical extent in object-local space (non-negative).", Types: []jsonschema.Type{jsonschema.TypeNumber}, Minimum: Float64(0)}},
	}
}

func circleProperties() []jsonschema.Property {
	return []jsonschema.Property{
		{Name: "x", Schema: &jsonschema.Schema{Description: "Horizontal center coordinate in object-local space.", Types: []jsonschema.Type{jsonschema.TypeNumber}}},
		{Name: "y", Schema: &jsonschema.Schema{Description: "Vertical center coordinate in object-local space.", Types: []jsonschema.Type{jsonschema.TypeNumber}}},
		{Name: "radius", Schema: &jsonschema.Schema{Description: "Circle radius in object-local space (non-negative).", Types: []jsonschema.Type{jsonschema.TypeNumber}, Minimum: Float64(0)}},
	}
}

func cloneFloatPtr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	return Float64(*p)
}

func schemaDefault(f Field) any {
	if f.Default == nil {
		return nil
	}
	norm, msg := f.check(f.Default)
	if msg != "" {
		return nil
	}
	switch v := norm.(type) {
	case float64:
		return normZero64(v)
	case []string:
		if len(v) == 0 {
			return nil
		}
		return v
	case [][2]float64:
		if len(v) == 0 {
			return nil
		}
		out := make([][2]float64, len(v))
		for i, pt := range v {
			out[i] = [2]float64{normZero64(pt[0]), normZero64(pt[1])}
		}
		return out
	case []Point:
		if len(v) == 0 {
			return nil
		}
		return v
	default:
		return norm
	}
}
