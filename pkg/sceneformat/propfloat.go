package sceneformat

import (
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// floatKinds returns doc for encoding with every whole-number float in its custom properties (ObjectDoc.Properties and ObjectOverride.Properties, nested lists and maps included) marked to be written with a decimal point. The YAML encoder writes float64(2) as "2", which reads back as an int, and unlike a component's fields or a connection's args, which their registered kinds turn back into floats, a property has no schema to restore its kind from. doc is not changed: the copy shares everything but the objects, overrides and property maps it rewrites.
func floatKinds(doc *SceneDoc) *SceneDoc {
	out := *doc
	out.Overrides = floatOverrides(doc.Overrides)
	if doc.Layers != nil {
		out.Layers = make([]LayerDoc, len(doc.Layers))
		for i, l := range doc.Layers {
			l.Objects = floatObjects(l.Objects)
			out.Layers[i] = l
		}
	}
	return &out
}

func floatObjects(objs []ObjectDoc) []ObjectDoc {
	if objs == nil {
		return nil
	}
	out := make([]ObjectDoc, len(objs))
	for i, o := range objs {
		o.Properties = floatMap(o.Properties)
		o.Overrides = floatOverrides(o.Overrides)
		o.Children = floatObjects(o.Children)
		out[i] = o
	}
	return out
}

func floatOverrides(m map[string]ObjectOverride) map[string]ObjectOverride {
	if m == nil {
		return nil
	}
	out := make(map[string]ObjectOverride, len(m))
	for k, ov := range m {
		ov.Properties = floatMap(ov.Properties)
		out[k] = ov
	}
	return out
}

func floatMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = floatValue(v)
	}
	return out
}

// floatValue returns v with each whole-number float in it, at any depth of the maps and lists YAML decodes into, replaced by a wholeFloat.
func floatValue(v any) any {
	switch x := v.(type) {
	case float64:
		if s := strconv.FormatFloat(x, 'g', -1, 64); isWholeNumber(s) {
			return wholeFloat(s)
		}
	case float32:
		if s := strconv.FormatFloat(float64(x), 'g', -1, 32); isWholeNumber(s) {
			return wholeFloat(s)
		}
	case map[string]any:
		return floatMap(x)
	case map[any]any:
		out := make(map[any]any, len(x))
		for k, e := range x {
			out[k] = floatValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = floatValue(e)
		}
		return out
	}
	return v
}

// isWholeNumber reports whether s, a float formatted by strconv, reads back as a YAML int: digits with an optional sign, and no point or exponent.
func isWholeNumber(s string) bool {
	return strings.TrimLeft(strings.TrimPrefix(s, "-"), "0123456789") == "" && s != "-"
}

// wholeFloat is a whole-number float, as strconv formats it, that encodes with ".0" appended so that it reads back as a float.
type wholeFloat string

// MarshalYAML writes the number as a plain !!float scalar, such as 2.0.
func (f wholeFloat) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: string(f) + ".0"}, nil
}
