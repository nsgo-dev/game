package ai

import (
	"bytes"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// UtilityVersion is the utility AI YAML schema version this package reads and writes.
const UtilityVersion = 1

// UtilityConfig is a versioned YAML definition of utility AI actions, considerations, and response curves.
type UtilityConfig struct {
	// Version is the schema version; it must equal UtilityVersion (1).
	Version int `yaml:"version"`
	// TopN enables weighted random selection among the top TopN positive-scoring actions when greater than 1.
	TopN int `yaml:"topN,omitempty"`
	// Momentum is the default bonus added to the currently active action's positive score.
	Momentum float64 `yaml:"momentum,omitempty"`
	// Actions are the candidate actions in fixed declaration order (used for deterministic tie-breaking).
	Actions []UtilityActionDef `yaml:"actions"`
}

// UtilityActionDef defines one action in a UtilityConfig file.
type UtilityActionDef struct {
	// Name uniquely identifies the action within the config.
	Name string `yaml:"name"`
	// Weight scales the compensated consideration score; 0 defaults to 1.
	Weight float64 `yaml:"weight,omitempty"`
	// Momentum is an optional per-action bonus added on top of UtilityConfig.Momentum while active.
	Momentum float64 `yaml:"momentum,omitempty"`
	// Considerations are the scoring considerations evaluated for this action.
	Considerations []ConsiderationDef `yaml:"considerations,omitempty"`
}

// ConsiderationDef defines one consideration in a UtilityConfig file.
type ConsiderationDef struct {
	// Name is an optional label for this consideration.
	Name string `yaml:"name,omitempty"`
	// Input names the input function bound through UtilityBindings.Inputs.
	Input string `yaml:"input"`
	// Min and Max optionally define the input range normalized into [0, 1]. When both are 0, the input is clamped directly to [0, 1].
	Min float64 `yaml:"min,omitempty"`
	Max float64 `yaml:"max,omitempty"`
	// Curve defines the response curve applied to the normalized input.
	Curve CurveDef `yaml:"curve"`
}

// CurveDef defines a response curve in a UtilityConfig file.
type CurveDef struct {
	// Kind selects the response curve: linear, polynomial, logistic, logit, step, or resource.
	Kind CurveKind `yaml:"kind"`
	// Slope scales the output (or defines high - low for step).
	Slope float64 `yaml:"slope,omitempty"`
	// Exponent sets the power for polynomial or steepness for logistic.
	Exponent float64 `yaml:"exponent,omitempty"`
	// ShiftX shifts the input (midpoint for logistic/logit or threshold for step).
	ShiftX float64 `yaml:"shiftX,omitempty"`
	// ShiftY offsets the output (or defines the low output for step).
	ShiftY float64 `yaml:"shiftY,omitempty"`
	// File names the resource curve file loaded via UtilityBindings.LoadCurve when Kind is CurveResource.
	File string `yaml:"file,omitempty"`
}

// ParseUtility decodes and validates a utility AI YAML document.
// Unknown fields, missing or unsupported versions, duplicate keys, and trailing YAML documents are refused.
func ParseUtility(data []byte) (*UtilityConfig, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add a utility AI config starting with version: %d", UtilityVersion), "ai: empty utility config"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg UtilityConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("ai: decode utility config: %w", err), "", data, "check the YAML syntax and field names in the utility AI config")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the utility AI config and remove any --- separator", "ai: unexpected trailing YAML document in utility config"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("ai: decode utility config: %w", err), "", data, "remove trailing YAML content after the utility AI config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	if cfg.Actions == nil {
		cfg.Actions = []UtilityActionDef{}
	}
	for i := range cfg.Actions {
		if len(cfg.Actions[i].Considerations) == 0 {
			cfg.Actions[i].Considerations = nil
		}
	}
	return &cfg, nil
}

// EncodeUtility validates cfg and encodes it as deterministic 2-space YAML in fixed field order.
func EncodeUtility(cfg *UtilityConfig) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("ai: encode utility config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("ai: encode utility config: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks that cfg has version UtilityVersion, valid parameters, unique action names, and well-formed considerations and curves.
func (cfg *UtilityConfig) Validate() error {
	if cfg == nil {
		return loaderr.Errorf("", "pass a non-nil *ai.UtilityConfig to Validate", "ai: nil utility config")
	}
	if cfg.Version != UtilityVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the utility AI config", UtilityVersion), "ai: unsupported utility config version %d (want %d)", cfg.Version, UtilityVersion)
	}
	if cfg.TopN < 0 {
		return loaderr.Errorf("topN", "set topN to 0 (deterministic best) or a positive integer", "ai: utility config topN %d must be non-negative", cfg.TopN)
	}
	if !isFiniteFloat64(cfg.Momentum) || cfg.Momentum < 0 {
		return loaderr.Errorf("momentum", "set momentum to a finite non-negative number", "ai: utility config momentum %v must be finite and non-negative", cfg.Momentum)
	}
	seen := make(map[string]bool, len(cfg.Actions))
	for i := range cfg.Actions {
		a := &cfg.Actions[i]
		aPath := fmt.Sprintf("actions[%d]", i)
		if a.Name == "" {
			return loaderr.Errorf(aPath+".name", "give each utility action a non-empty name", "ai: utility action %d has no name", i)
		}
		if seen[a.Name] {
			return loaderr.Errorf(aPath+".name", "give each utility action a unique name", "ai: utility action %q is defined twice", a.Name)
		}
		seen[a.Name] = true
		if !isFiniteFloat64(a.Weight) || a.Weight < 0 {
			return loaderr.Errorf(aPath+".weight", "set weight to a finite non-negative number (0 defaults to 1)", "ai: utility action %q weight %v must be finite and non-negative", a.Name, a.Weight)
		}
		if !isFiniteFloat64(a.Momentum) || a.Momentum < 0 {
			return loaderr.Errorf(aPath+".momentum", "set momentum to a finite non-negative number", "ai: utility action %q momentum %v must be finite and non-negative", a.Name, a.Momentum)
		}
		for j := range a.Considerations {
			if err := a.Considerations[j].validate(fmt.Sprintf("%s.considerations[%d]", aPath, j), a.Name, j); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *ConsiderationDef) validate(cPath, action string, index int) error {
	if c.Input == "" {
		return loaderr.Errorf(cPath+".input", "set input to a bound input function name", "ai: utility action %q consideration %d has no input", action, index)
	}
	if !isFiniteFloat64(c.Min) {
		return loaderr.Errorf(cPath+".min", "set min to a finite number", "ai: utility action %q consideration %d min/max must be finite", action, index)
	}
	if !isFiniteFloat64(c.Max) {
		return loaderr.Errorf(cPath+".max", "set max to a finite number", "ai: utility action %q consideration %d min/max must be finite", action, index)
	}
	if (c.Min != 0 || c.Max != 0) && c.Max <= c.Min {
		return loaderr.Errorf(cPath+".max", "set max greater than min, or omit both to use [0, 1]", "ai: utility action %q consideration %d max %v must be greater than min %v", action, index, c.Max, c.Min)
	}
	return c.Curve.validate(cPath+".curve", action, index)
}

func (c *CurveDef) validate(cPath, action string, index int) error {
	if !isFiniteFloat64(c.Slope) {
		return loaderr.Errorf(cPath+".slope", "set curve slope to a finite number", "ai: utility action %q consideration %d curve parameters must be finite", action, index)
	}
	if !isFiniteFloat64(c.Exponent) {
		return loaderr.Errorf(cPath+".exponent", "set curve exponent to a finite number", "ai: utility action %q consideration %d curve parameters must be finite", action, index)
	}
	if !isFiniteFloat64(c.ShiftX) {
		return loaderr.Errorf(cPath+".shiftX", "set curve shiftX to a finite number", "ai: utility action %q consideration %d curve parameters must be finite", action, index)
	}
	if !isFiniteFloat64(c.ShiftY) {
		return loaderr.Errorf(cPath+".shiftY", "set curve shiftY to a finite number", "ai: utility action %q consideration %d curve parameters must be finite", action, index)
	}
	switch c.Kind {
	case CurveLinear, CurveLogit, CurveStep:
		if c.File != "" {
			return loaderr.Errorf(cPath+".file", "remove file or set kind: resource", "ai: utility action %q consideration %d curve kind %q cannot specify file", action, index, c.Kind)
		}
		if c.Exponent != 0 {
			return loaderr.Errorf(cPath+".exponent", "remove exponent for linear, logit, and step curves", "ai: utility action %q consideration %d curve kind %q does not use exponent", action, index, c.Kind)
		}
	case CurvePolynomial, CurveLogistic:
		if c.File != "" {
			return loaderr.Errorf(cPath+".file", "remove file or set kind: resource", "ai: utility action %q consideration %d curve kind %q cannot specify file", action, index, c.Kind)
		}
	case CurveResource:
		if c.File == "" {
			return loaderr.Errorf(cPath+".file", "set file to a curve resource path when kind is resource", "ai: utility action %q consideration %d curve kind %q requires file", action, index, c.Kind)
		}
		if c.Slope != 0 || c.Exponent != 0 || c.ShiftX != 0 || c.ShiftY != 0 {
			return loaderr.Errorf(cPath, "remove analytic curve parameters (slope, exponent, shiftX, shiftY) when kind is resource", "ai: utility action %q consideration %d curve kind %q cannot mix analytic curve parameters with file", action, index, c.Kind)
		}
	default:
		return loaderr.Errorf(cPath+".kind", "set kind to linear, polynomial, logistic, logit, step, or resource", "ai: utility action %q consideration %d has unknown curve kind %q (want linear, polynomial, logistic, logit, step or resource)", action, index, c.Kind)
	}
	return nil
}

func isFiniteFloat64(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// UtilityBindings provides the Go input readers and optional resource curve loader used by BuildReasoner.
type UtilityBindings[C any] struct {
	// Inputs maps each ConsiderationDef.Input name to a function extracting that value from C.
	Inputs map[string]func(C) float64
	// LoadCurve resolves a CurveDef.File path into a CurveEvaluator (for example via resource.LoadAs[*resource.Curve]).
	// Required only when the UtilityConfig contains at least one CurveResource curve.
	LoadCurve func(path string) (CurveEvaluator, error)
}

// BuildReasoner validates cfg and resolves its input names and resource curve files into a Reasoner.
func BuildReasoner[C any](cfg *UtilityConfig, bind UtilityBindings[C]) (*Reasoner[C], error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	curveCache := map[string]CurveEvaluator{}
	actions := make([]UtilityAction[C], len(cfg.Actions))
	for i := range cfg.Actions {
		ad := &cfg.Actions[i]
		cons := make([]Consideration[C], len(ad.Considerations))
		for j := range ad.Considerations {
			cd := &ad.Considerations[j]
			inFn := bind.Inputs[cd.Input]
			if inFn == nil {
				return nil, fmt.Errorf("ai: utility action %q consideration %d references unbound input %q", ad.Name, j, cd.Input)
			}
			rc := ResponseCurve{
				Kind:     cd.Curve.Kind,
				Slope:    cd.Curve.Slope,
				Exponent: cd.Curve.Exponent,
				ShiftX:   cd.Curve.ShiftX,
				ShiftY:   cd.Curve.ShiftY,
			}
			if cd.Curve.Kind == CurveResource {
				eval, ok := curveCache[cd.Curve.File]
				if !ok {
					if bind.LoadCurve == nil {
						return nil, fmt.Errorf("ai: utility action %q consideration %d needs LoadCurve to load %q", ad.Name, j, cd.Curve.File)
					}
					var err error
					eval, err = bind.LoadCurve(cd.Curve.File)
					if err != nil {
						return nil, fmt.Errorf("ai: utility action %q consideration %d load curve %q: %w", ad.Name, j, cd.Curve.File, err)
					}
					if eval == nil {
						return nil, fmt.Errorf("ai: utility action %q consideration %d LoadCurve(%q) returned nil", ad.Name, j, cd.Curve.File)
					}
					curveCache[cd.Curve.File] = eval
				}
				rc.Evaluator = eval
			}
			cons[j] = Consideration[C]{
				Name:  cd.Name,
				Input: inFn,
				Min:   cd.Min,
				Max:   cd.Max,
				Curve: rc,
			}
		}
		actions[i] = UtilityAction[C]{
			Name:           ad.Name,
			Weight:         ad.Weight,
			Momentum:       ad.Momentum,
			Considerations: cons,
		}
	}
	r := &Reasoner[C]{
		TopN:     cfg.TopN,
		Momentum: cfg.Momentum,
		actions:  actions,
		current:  -1,
	}
	if r.TopN > 1 && len(actions) > 0 {
		r.topBuf = make([]scoredAction, 0, min(r.TopN, len(actions)))
	}
	return r, nil
}
