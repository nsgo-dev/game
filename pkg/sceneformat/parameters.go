package sceneformat

import (
	"fmt"
	"math"
	"strings"
)

// ParameterType is the declared type of a reusable scene parameter.
type ParameterType string

const (
	ParameterFloat  ParameterType = "float"
	ParameterInt    ParameterType = "int"
	ParameterBool   ParameterType = "bool"
	ParameterString ParameterType = "string"
	ParameterColor  ParameterType = "color"
	ParameterVec2   ParameterType = "vec2"
	ParameterVec3   ParameterType = "vec3"
	ParameterAsset  ParameterType = "asset"
)

// SceneParameter declares a value exposed by a reusable scene.
type SceneParameter struct {
	Name        string             `yaml:"name"`
	Type        ParameterType      `yaml:"type"`
	Default     any                `yaml:"default"`
	Description string             `yaml:"description,omitempty"`
	Bind        []ParameterBinding `yaml:"bind"`
}

// ParameterBinding identifies a field supplied by a scene parameter.
type ParameterBinding struct {
	Object    string `yaml:"object"`
	Component string `yaml:"component,omitempty"`
	Property  string `yaml:"property,omitempty"`
	Field     string `yaml:"field"`
}

func validateSceneParameters(doc *SceneDoc) error {
	seen := make(map[string]struct{}, len(doc.Parameters))
	objects := make(map[string]struct{})
	for i := range doc.Layers {
		WalkObjects(doc.Layers[i].Objects, func(o, _ *ObjectDoc) bool { objects[o.ID] = struct{}{}; return true })
	}
	for i := range doc.Parameters {
		p := &doc.Parameters[i]
		base := []string{"parameters", fmt.Sprint(i)}
		if strings.TrimSpace(p.Name) == "" {
			return fieldErr(at(base, "name"), "sceneformat: parameter name is required")
		}
		if _, ok := seen[p.Name]; ok {
			return fieldErr(at(base, "name"), "sceneformat: duplicate parameter name %q", p.Name)
		}
		seen[p.Name] = struct{}{}
		if !p.Type.valid() {
			return fieldErr(at(base, "type"), "sceneformat: invalid parameter type %q", p.Type)
		}
		if err := validateParameterValue(p.Type, p.Default); err != nil {
			return fieldErr(at(base, "default"), "sceneformat: parameter %q: %w", p.Name, err)
		}
		if len(p.Bind) == 0 {
			return fieldErr(at(base, "bind"), "sceneformat: parameter %q must bind at least one field", p.Name)
		}
		for j, b := range p.Bind {
			if strings.TrimSpace(b.Object) == "" || strings.TrimSpace(b.Field) == "" {
				return fieldErr(at(base, "bind", fmt.Sprint(j)), "sceneformat: parameter %q binding needs object and field", p.Name)
			}
			if _, ok := objects[b.Object]; !ok {
				return fieldErr(at(base, "bind", fmt.Sprint(j), "object"), "sceneformat: parameter %q binds missing object %q", p.Name, b.Object)
			}
		}
	}
	return nil
}

func (t ParameterType) valid() bool {
	switch t {
	case ParameterFloat, ParameterInt, ParameterBool, ParameterString, ParameterColor, ParameterVec2, ParameterVec3, ParameterAsset:
		return true
	default:
		return false
	}
}

func validateParameterValue(t ParameterType, value any) error {
	switch t {
	case ParameterFloat:
		if n, ok := number(value); ok && math.IsNaN(n) == false && math.IsInf(n, 0) == false {
			return nil
		}
	case ParameterInt:
		switch n := value.(type) {
		case int, int32, int64:
			return nil
		case uint, uint32, uint64:
			return nil
		case float32:
			if float32(int64(n)) == n {
				return nil
			}
		case float64:
			if float64(int64(n)) == n {
				return nil
			}
		}
	case ParameterBool:
		if _, ok := value.(bool); ok {
			return nil
		}
	case ParameterString, ParameterColor, ParameterAsset:
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return nil
		}
	case ParameterVec2:
		if validVector(value, 2) {
			return nil
		}
	case ParameterVec3:
		if validVector(value, 3) {
			return nil
		}
	}
	return fmt.Errorf("default has the wrong type for %s", t)
}

func number(value any) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func validVector(value any, size int) bool {
	items, ok := value.([]any)
	if !ok || len(items) != size {
		return false
	}
	for _, item := range items {
		if _, ok := number(item); !ok {
			return false
		}
	}
	return true
}
