package editor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// CanonicalGenerationParameter names the stable vocabulary shared by asset
// generators. Provider-specific names are mapped to these keys at the edge.
type CanonicalGenerationParameter string

const (
	ParamPrompt          CanonicalGenerationParameter = "prompt"
	ParamNegativePrompt  CanonicalGenerationParameter = "negativePrompt"
	ParamSeed            CanonicalGenerationParameter = "seed"
	ParamCount           CanonicalGenerationParameter = "count"
	ParamWidth           CanonicalGenerationParameter = "width"
	ParamHeight          CanonicalGenerationParameter = "height"
	ParamAspectRatio     CanonicalGenerationParameter = "aspectRatio"
	ParamBackground      CanonicalGenerationParameter = "background"
	ParamReferenceImages CanonicalGenerationParameter = "referenceImages"
	ParamDuration        CanonicalGenerationParameter = "duration"
	ParamLoop            CanonicalGenerationParameter = "loop"
	ParamVoice           CanonicalGenerationParameter = "voice"
	ParamInstrumental    CanonicalGenerationParameter = "instrumental"
	ParamFrameCount      CanonicalGenerationParameter = "frameCount"
	ParamFaceLimit       CanonicalGenerationParameter = "faceLimit"
)

// ParameterNames returns canonical keys in deterministic order.
func ParameterNames() []CanonicalGenerationParameter {
	return []CanonicalGenerationParameter{ParamPrompt, ParamNegativePrompt, ParamSeed, ParamCount, ParamWidth, ParamHeight, ParamAspectRatio, ParamBackground, ParamReferenceImages, ParamDuration, ParamLoop, ParamVoice, ParamInstrumental, ParamFrameCount, ParamFaceLimit}
}

// ParameterSchema is the subset of JSON Schema used to validate generator
// parameters before an MCP call.
type ParameterSchema struct {
	Type       string
	Enum       []any
	Minimum    *float64
	Maximum    *float64
	Properties map[string]ParameterSchema
}

// GeneratorParameterSchema is an MCP tool's object input schema.
type GeneratorParameterSchema struct {
	Properties map[string]ParameterSchema
	Required   []string
}

// ParseGeneratorParameterSchema parses an MCP tool input schema.
func ParseGeneratorParameterSchema(raw []byte) (GeneratorParameterSchema, error) {
	var doc struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return GeneratorParameterSchema{}, fmt.Errorf("generator parameter schema: %w", err)
	}
	if doc.Type != "" && doc.Type != "object" {
		return GeneratorParameterSchema{}, fmt.Errorf("generator parameter schema: root type must be object")
	}
	out := GeneratorParameterSchema{Properties: make(map[string]ParameterSchema), Required: append([]string(nil), doc.Required...)}
	for name, encoded := range doc.Properties {
		var property struct {
			Type       string                     `json:"type"`
			Enum       []any                      `json:"enum"`
			Minimum    *float64                   `json:"minimum"`
			Maximum    *float64                   `json:"maximum"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(encoded, &property); err != nil {
			return GeneratorParameterSchema{}, fmt.Errorf("generator parameter %q: %w", name, err)
		}
		nested := make(map[string]ParameterSchema)
		for nestedName, nestedRaw := range property.Properties {
			var nestedSchema ParameterSchema
			if err := json.Unmarshal(nestedRaw, &nestedSchema); err != nil {
				return GeneratorParameterSchema{}, fmt.Errorf("generator parameter %q.%q: %w", name, nestedName, err)
			}
			nested[nestedName] = nestedSchema
		}
		out.Properties[name] = ParameterSchema{Type: property.Type, Enum: property.Enum, Minimum: property.Minimum, Maximum: property.Maximum, Properties: nested}
	}
	return out, nil
}

var parameterAliases = map[CanonicalGenerationParameter][]string{
	ParamPrompt:          {"prompt", "text", "description"},
	ParamNegativePrompt:  {"negativePrompt", "negative_prompt", "negative"},
	ParamSeed:            {"seed", "random_seed", "randomSeed"},
	ParamCount:           {"count", "num_images", "numImages", "quantity"},
	ParamWidth:           {"width", "image_width", "imageWidth"},
	ParamHeight:          {"height", "image_height", "imageHeight"},
	ParamAspectRatio:     {"aspectRatio", "aspect_ratio", "ratio"},
	ParamBackground:      {"background", "background_color", "backgroundColor"},
	ParamReferenceImages: {"referenceImages", "reference_images", "images", "image_urls"},
	ParamDuration:        {"duration", "duration_seconds", "seconds"},
	ParamLoop:            {"loop", "looping", "seamless"},
	ParamVoice:           {"voice", "voice_id", "voiceId"},
	ParamInstrumental:    {"instrumental", "is_instrumental", "isInstrumental"},
	ParamFrameCount:      {"frameCount", "frame_count", "frames"},
	ParamFaceLimit:       {"faceLimit", "face_limit", "max_faces", "maxFaces"},
}

// MapGeneratorParameters resolves canonical keys to schema-declared wire keys.
// Overrides take precedence and are keyed by canonical name.
func MapGeneratorParameters(schema GeneratorParameterSchema, values map[string]any, overrides map[string]string) (map[string]any, error) {
	out := make(map[string]any, len(values))
	for canonical, value := range values {
		key := CanonicalGenerationParameter(canonical)
		wire := strings.TrimSpace(overrides[canonical])
		if wire == "" {
			wire = findWireKey(schema.Properties, key)
		}
		if wire == "" {
			if parent, child, ok := findNestedWireKey(schema.Properties, key); ok {
				property := schema.Properties[parent].Properties[child]
				if err := validateValue(parent+"."+child, property, value); err != nil {
					return nil, err
				}
				nested, _ := out[parent].(map[string]any)
				if nested == nil {
					nested = make(map[string]any)
				}
				nested[child] = value
				out[parent] = nested
				continue
			}
		}
		if wire == "" {
			wire = canonical
		}
		property, ok := schema.Properties[wire]
		if ok {
			if err := validateValue(wire, property, value); err != nil {
				return nil, err
			}
		}
		out[wire] = value
	}
	return out, nil
}

// ValidateGeneratorArguments validates a complete wire argument object against
// the connected tool schema, including required fields.
func ValidateGeneratorArguments(schema GeneratorParameterSchema, values map[string]any) error {
	for _, required := range schema.Required {
		if _, ok := values[required]; !ok {
			return fmt.Errorf("generator parameter %q is required", required)
		}
	}
	for key, value := range values {
		if property, ok := schema.Properties[key]; ok {
			if err := validateValue(key, property, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func findWireKey(properties map[string]ParameterSchema, key CanonicalGenerationParameter) string {
	for _, alias := range parameterAliases[key] {
		if _, ok := properties[alias]; ok {
			return alias
		}
	}
	return ""
}

func findNestedWireKey(properties map[string]ParameterSchema, key CanonicalGenerationParameter) (parent, child string, ok bool) {
	for candidate, property := range properties {
		if property.Type != "object" || len(property.Properties) == 0 {
			continue
		}
		for _, childName := range []string{string(key), strings.ToLower(string(key))} {
			if _, exists := property.Properties[childName]; exists {
				return candidate, childName, true
			}
		}
	}
	return "", "", false
}

func validateValue(name string, schema ParameterSchema, value any) error {
	if schema.Type != "" && !jsonTypeMatches(schema.Type, value) {
		return fmt.Errorf("generator parameter %q has type %T, want %s", name, value, schema.Type)
	}
	if len(schema.Enum) > 0 {
		found := false
		for _, allowed := range schema.Enum {
			if parameterValuesEqual(allowed, value) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("generator parameter %q has value %v outside enum", name, value)
		}
	}
	if number, ok := numericValue(value); ok {
		if schema.Minimum != nil && number < *schema.Minimum {
			return fmt.Errorf("generator parameter %q is %v, below minimum %v", name, number, *schema.Minimum)
		}
		if schema.Maximum != nil && number > *schema.Maximum {
			return fmt.Errorf("generator parameter %q is %v, above maximum %v", name, number, *schema.Maximum)
		}
	}
	return nil
}

func jsonTypeMatches(want string, value any) bool {
	switch want {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		n, ok := numericValue(value)
		return ok && math.Trunc(n) == n
	case "number":
		_, ok := numericValue(value)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return true
	}
}

func numericValue(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case float32:
		return float64(v), true
	default:
		return 0, false
	}
}

func parameterValuesEqual(a, b any) bool {
	left, leftOK := jsonNumber(a)
	right, rightOK := jsonNumber(b)
	if leftOK && rightOK {
		return left == right
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func jsonNumber(value any) (float64, bool) { return numericValue(value) }

// CanonicalParameterMap returns a stable copy suitable for recording in a
// source sidecar, with keys sorted by JSON encoding.
func CanonicalParameterMap(values map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(values))
	for key, value := range values {
		if !isCanonicalKey(key) {
			return nil, fmt.Errorf("unknown canonical generator parameter %q", key)
		}
		out[key] = value
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	var stable map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(encoded), &stable); err != nil {
		return nil, err
	}
	return stable, nil
}

func isCanonicalKey(key string) bool {
	for _, known := range ParameterNames() {
		if key == string(known) {
			return true
		}
	}
	return false
}

// SortedParameterNames returns canonical keys present in values.
func SortedParameterNames(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
