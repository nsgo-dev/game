package ai

import (
	"bytes"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// GOAPVersion is the GOAP action-set YAML schema version this package reads and writes.
const GOAPVersion = 1

// GOAPCompareOp identifies a comparison operator on an integer fact in a GOAP YAML file.
type GOAPCompareOp string

const (
	// GOAPOpEq requires fact == value.
	GOAPOpEq GOAPCompareOp = "eq"
	// GOAPOpGte requires fact >= value.
	GOAPOpGte GOAPCompareOp = "gte"
	// GOAPOpLte requires fact <= value.
	GOAPOpLte GOAPCompareOp = "lte"
	// GOAPOpGt requires fact > value.
	GOAPOpGt GOAPCompareOp = "gt"
	// GOAPOpLt requires fact < value.
	GOAPOpLt GOAPCompareOp = "lt"
)

// GOAPIntEffectOp identifies an integer effect operation in a GOAP YAML file.
type GOAPIntEffectOp string

const (
	// GOAPEffectSet sets an integer fact to a constant value.
	GOAPEffectSet GOAPIntEffectOp = "set"
	// GOAPEffectAdd adds a signed delta to an integer fact.
	GOAPEffectAdd GOAPIntEffectOp = "add"
)

// GOAPConfig is a versioned YAML definition of a GOAP fact schema, action set, and optional goals.
type GOAPConfig struct {
	// Version is the schema version; it must equal GOAPVersion (1).
	Version int `yaml:"version"`
	// NodeBudget overrides DefaultGOAPNodeBudget when greater than 0.
	NodeBudget int `yaml:"nodeBudget,omitempty"`
	// Facts declares the named boolean and small integer facts referenced by actions and goals.
	Facts GOAPFactsDef `yaml:"facts"`
	// Actions are the planning operators in fixed declaration order (used for deterministic tie-breaking).
	Actions []GOAPActionDef `yaml:"actions"`
	// Goals are optional prioritized target conditions compiled by BuildGOAP.
	Goals []GOAPGoalDef `yaml:"goals,omitempty"`
}

// GOAPFactsDef declares the named boolean and integer facts in a GOAPConfig file.
type GOAPFactsDef struct {
	Bools []string `yaml:"bools,omitempty"`
	Ints  []string `yaml:"ints,omitempty"`
}

// GOAPActionDef defines one GOAP action in a GOAPConfig file.
type GOAPActionDef struct {
	// Name uniquely identifies the action.
	Name string `yaml:"name"`
	// Cost is the positive planning cost of executing this action; 0 defaults to 1.
	Cost float64 `yaml:"cost,omitempty"`
	// Check optionally names a procedural precondition callback bound via GOAPBindings.Checks.
	Check string `yaml:"check,omitempty"`
	// Preconditions are the boolean and integer conditions required before the action can execute.
	Preconditions GOAPConditionsDef `yaml:"preconditions,omitempty"`
	// Effects are the boolean and integer changes produced when the action completes.
	Effects GOAPEffectsDef `yaml:"effects,omitempty"`
}

// GOAPGoalDef defines one prioritized goal in a GOAPConfig file.
type GOAPGoalDef struct {
	// Name uniquely identifies the goal.
	Name string `yaml:"name"`
	// Priority ranks competing goals (higher is attempted first; ties break in declaration order).
	Priority float64 `yaml:"priority,omitempty"`
	// Target specifies the desired world state for this goal.
	Target GOAPConditionsDef `yaml:"target"`
}

// GOAPConditionsDef defines boolean and integer requirements in YAML.
type GOAPConditionsDef struct {
	Bools []GOAPBoolFactDef     `yaml:"bools,omitempty"`
	Ints  []GOAPIntConditionDef `yaml:"ints,omitempty"`
}

// GOAPEffectsDef defines boolean and integer effects in YAML.
type GOAPEffectsDef struct {
	Bools []GOAPBoolFactDef  `yaml:"bools,omitempty"`
	Ints  []GOAPIntEffectDef `yaml:"ints,omitempty"`
}

// GOAPBoolFactDef binds a named boolean fact to a true/false value.
type GOAPBoolFactDef struct {
	Fact  string `yaml:"fact"`
	Value bool   `yaml:"value"`
}

// GOAPIntConditionDef compares a named integer fact against Value using Op.
type GOAPIntConditionDef struct {
	Fact  string        `yaml:"fact"`
	Op    GOAPCompareOp `yaml:"op"`
	Value int           `yaml:"value"`
}

// GOAPIntEffectDef modifies a named integer fact with Op (set or add) and Value.
type GOAPIntEffectDef struct {
	Fact  string          `yaml:"fact"`
	Op    GOAPIntEffectOp `yaml:"op"`
	Value int             `yaml:"value"`
}

// ParseGOAP decodes and validates a GOAP YAML document.
// Unknown fields, missing or unsupported versions, duplicate keys, unknown fact references, and trailing YAML documents are refused.
func ParseGOAP(data []byte) (*GOAPConfig, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add a GOAP config starting with version: %d", GOAPVersion), "ai: empty GOAP config"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg GOAPConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("ai: decode GOAP config: %w", err), "", data, "check the YAML syntax and field names in the GOAP config")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the GOAP config and remove any --- separator", "ai: unexpected trailing YAML document in GOAP config"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("ai: decode GOAP config: %w", err), "", data, "remove trailing YAML content after the GOAP config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	cfg.normalize()
	return &cfg, nil
}

func (cfg *GOAPConfig) normalize() {
	if len(cfg.Facts.Bools) == 0 {
		cfg.Facts.Bools = nil
	}
	if len(cfg.Facts.Ints) == 0 {
		cfg.Facts.Ints = nil
	}
	if cfg.Actions == nil {
		cfg.Actions = []GOAPActionDef{}
	}
	for i := range cfg.Actions {
		if len(cfg.Actions[i].Preconditions.Bools) == 0 {
			cfg.Actions[i].Preconditions.Bools = nil
		}
		if len(cfg.Actions[i].Preconditions.Ints) == 0 {
			cfg.Actions[i].Preconditions.Ints = nil
		}
		if len(cfg.Actions[i].Effects.Bools) == 0 {
			cfg.Actions[i].Effects.Bools = nil
		}
		if len(cfg.Actions[i].Effects.Ints) == 0 {
			cfg.Actions[i].Effects.Ints = nil
		}
	}
	if len(cfg.Goals) == 0 {
		cfg.Goals = nil
	} else {
		for i := range cfg.Goals {
			if len(cfg.Goals[i].Target.Bools) == 0 {
				cfg.Goals[i].Target.Bools = nil
			}
			if len(cfg.Goals[i].Target.Ints) == 0 {
				cfg.Goals[i].Target.Ints = nil
			}
		}
	}
}

// EncodeGOAP validates cfg and encodes it as deterministic 2-space YAML in fixed field order.
func EncodeGOAP(cfg *GOAPConfig) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("ai: encode GOAP config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("ai: encode GOAP config: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks that cfg has version GOAPVersion, valid facts, unique actions and goals, valid operators/ranges, and no undefined fact references.
func (cfg *GOAPConfig) Validate() error {
	_, err := cfg.buildSchemaAndValidate()
	return err
}

func (cfg *GOAPConfig) buildSchemaAndValidate() (*FactSchema, error) {
	if cfg == nil {
		return nil, loaderr.Errorf("", "pass a non-nil *ai.GOAPConfig to Validate", "ai: nil GOAP config")
	}
	if cfg.Version != GOAPVersion {
		return nil, loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the GOAP config", GOAPVersion), "ai: unsupported GOAP config version %d (want %d)", cfg.Version, GOAPVersion)
	}
	if cfg.NodeBudget < 0 {
		return nil, loaderr.Errorf("nodeBudget", "set nodeBudget to 0 (default) or a positive integer", "ai: GOAP config nodeBudget %d must be non-negative", cfg.NodeBudget)
	}

	schema := NewFactSchema()
	seenBools := make(map[string]bool, len(cfg.Facts.Bools))
	for i, name := range cfg.Facts.Bools {
		fPath := fmt.Sprintf("facts.bools[%d]", i)
		if name == "" {
			return nil, loaderr.Errorf(fPath, "set each boolean fact name to a non-empty string", "ai: GOAP facts.bools[%d] has empty name", i)
		}
		if seenBools[name] {
			return nil, loaderr.Errorf(fPath, "remove the duplicate boolean fact declaration", "ai: GOAP boolean fact %q is declared twice", name)
		}
		seenBools[name] = true
		if _, err := schema.Bool(name); err != nil {
			return nil, loaderr.Errorf(fPath, "reduce the number of boolean facts to fit the schema limit", "%s", err.Error())
		}
	}
	seenInts := make(map[string]bool, len(cfg.Facts.Ints))
	for i, name := range cfg.Facts.Ints {
		fPath := fmt.Sprintf("facts.ints[%d]", i)
		if name == "" {
			return nil, loaderr.Errorf(fPath, "set each integer fact name to a non-empty string", "ai: GOAP facts.ints[%d] has empty name", i)
		}
		if seenInts[name] {
			return nil, loaderr.Errorf(fPath, "remove the duplicate integer fact declaration", "ai: GOAP integer fact %q is declared twice", name)
		}
		seenInts[name] = true
		if _, err := schema.Int(name); err != nil {
			return nil, loaderr.Errorf(fPath, "reduce the number of integer facts to fit the schema limit", "%s", err.Error())
		}
	}

	seenActions := make(map[string]bool, len(cfg.Actions))
	for i := range cfg.Actions {
		a := &cfg.Actions[i]
		aPath := fmt.Sprintf("actions[%d]", i)
		if a.Name == "" {
			return nil, loaderr.Errorf(aPath+".name", "give each GOAP action a non-empty name", "ai: GOAP action %d has no name", i)
		}
		if seenActions[a.Name] {
			return nil, loaderr.Errorf(aPath+".name", "give each GOAP action a unique name", "ai: GOAP action %q is defined twice", a.Name)
		}
		seenActions[a.Name] = true
		if !isFiniteFloat64(a.Cost) || a.Cost < 0 {
			return nil, loaderr.Errorf(aPath+".cost", "set cost to a finite non-negative number (0 defaults to 1)", "ai: GOAP action %q cost %v must be finite and non-negative", a.Name, a.Cost)
		}
		if _, err := compileConditions(schema, a.Preconditions, aPath+".preconditions", fmt.Sprintf("action %q preconditions", a.Name)); err != nil {
			return nil, err
		}
		if _, err := compileEffects(schema, a.Effects, aPath+".effects", fmt.Sprintf("action %q effects", a.Name)); err != nil {
			return nil, err
		}
	}

	seenGoals := make(map[string]bool, len(cfg.Goals))
	for i := range cfg.Goals {
		g := &cfg.Goals[i]
		gPath := fmt.Sprintf("goals[%d]", i)
		if g.Name == "" {
			return nil, loaderr.Errorf(gPath+".name", "give each GOAP goal a non-empty name", "ai: GOAP goal %d has no name", i)
		}
		if seenGoals[g.Name] {
			return nil, loaderr.Errorf(gPath+".name", "give each GOAP goal a unique name", "ai: GOAP goal %q is defined twice", g.Name)
		}
		seenGoals[g.Name] = true
		if !isFiniteFloat64(g.Priority) {
			return nil, loaderr.Errorf(gPath+".priority", "set priority to a finite number", "ai: GOAP goal %q priority %v must be finite", g.Name, g.Priority)
		}
		if _, err := compileConditions(schema, g.Target, gPath+".target", fmt.Sprintf("goal %q target", g.Name)); err != nil {
			return nil, err
		}
	}

	return schema, nil
}

func compileConditions(schema *FactSchema, def GOAPConditionsDef, fieldPrefix, where string) (Conditions, error) {
	var c Conditions
	seenBool := make(map[string]bool, len(def.Bools))
	for i, bf := range def.Bools {
		bPath := fmt.Sprintf("%s.bools[%d]", fieldPrefix, i)
		if bf.Fact == "" {
			return Conditions{}, loaderr.Errorf(bPath+".fact", "set fact to a boolean fact declared under facts.bools", "ai: GOAP %s bools[%d] has empty fact name", where, i)
		}
		if seenBool[bf.Fact] {
			return Conditions{}, loaderr.Errorf(bPath+".fact", "list each boolean fact at most once in a condition block", "ai: GOAP %s lists boolean fact %q twice", where, bf.Fact)
		}
		seenBool[bf.Fact] = true
		f, ok := schema.LookupBool(bf.Fact)
		if !ok {
			return Conditions{}, loaderr.Errorf(bPath+".fact", fmt.Sprintf("declare %q under facts.bools or fix the fact name", bf.Fact), "ai: GOAP %s references unknown boolean fact %q", where, bf.Fact)
		}
		c = c.RequireBool(f, bf.Value)
	}
	for i, ic := range def.Ints {
		iPath := fmt.Sprintf("%s.ints[%d]", fieldPrefix, i)
		if ic.Fact == "" {
			return Conditions{}, loaderr.Errorf(iPath+".fact", "set fact to an integer fact declared under facts.ints", "ai: GOAP %s ints[%d] has empty fact name", where, i)
		}
		f, ok := schema.LookupInt(ic.Fact)
		if !ok {
			return Conditions{}, loaderr.Errorf(iPath+".fact", fmt.Sprintf("declare %q under facts.ints or fix the fact name", ic.Fact), "ai: GOAP %s references unknown integer fact %q", where, ic.Fact)
		}
		if ic.Value < math.MinInt16 || ic.Value > math.MaxInt16 {
			return Conditions{}, loaderr.Errorf(iPath+".value", fmt.Sprintf("set value within int16 range [%d, %d]", math.MinInt16, math.MaxInt16), "ai: GOAP %s integer fact %q value %d out of int16 range [%d, %d]", where, ic.Fact, ic.Value, math.MinInt16, math.MaxInt16)
		}
		switch ic.Op {
		case GOAPOpEq:
			c = c.RequireIntEq(f, ic.Value)
		case GOAPOpGte:
			c = c.RequireIntGte(f, ic.Value)
		case GOAPOpLte:
			c = c.RequireIntLte(f, ic.Value)
		case GOAPOpGt:
			c = c.RequireIntGt(f, ic.Value)
		case GOAPOpLt:
			c = c.RequireIntLt(f, ic.Value)
		default:
			return Conditions{}, loaderr.Errorf(iPath+".op", "set op to eq, gte, lte, gt, or lt", "ai: GOAP %s integer fact %q has unknown op %q (want eq, gte, lte, gt or lt)", where, ic.Fact, ic.Op)
		}
	}
	if c.isUnsatisfiable() {
		return Conditions{}, loaderr.Errorf(fieldPrefix+".ints", "adjust the integer comparisons so their bounds overlap", "ai: GOAP %s has contradictory integer bounds", where)
	}
	return c, nil
}

func compileEffects(schema *FactSchema, def GOAPEffectsDef, fieldPrefix, where string) (Effects, error) {
	var e Effects
	seenBool := make(map[string]bool, len(def.Bools))
	for i, bf := range def.Bools {
		bPath := fmt.Sprintf("%s.bools[%d]", fieldPrefix, i)
		if bf.Fact == "" {
			return Effects{}, loaderr.Errorf(bPath+".fact", "set fact to a boolean fact declared under facts.bools", "ai: GOAP %s bools[%d] has empty fact name", where, i)
		}
		if seenBool[bf.Fact] {
			return Effects{}, loaderr.Errorf(bPath+".fact", "list each boolean fact at most once in an effects block", "ai: GOAP %s lists boolean fact %q twice", where, bf.Fact)
		}
		seenBool[bf.Fact] = true
		f, ok := schema.LookupBool(bf.Fact)
		if !ok {
			return Effects{}, loaderr.Errorf(bPath+".fact", fmt.Sprintf("declare %q under facts.bools or fix the fact name", bf.Fact), "ai: GOAP %s references unknown boolean fact %q", where, bf.Fact)
		}
		e = e.SetBool(f, bf.Value)
	}
	seenInt := make(map[string]bool, len(def.Ints))
	for i, ie := range def.Ints {
		iPath := fmt.Sprintf("%s.ints[%d]", fieldPrefix, i)
		if ie.Fact == "" {
			return Effects{}, loaderr.Errorf(iPath+".fact", "set fact to an integer fact declared under facts.ints", "ai: GOAP %s ints[%d] has empty fact name", where, i)
		}
		if seenInt[ie.Fact] {
			return Effects{}, loaderr.Errorf(iPath+".fact", "list each integer fact at most once in an effects block", "ai: GOAP %s lists integer fact %q twice", where, ie.Fact)
		}
		seenInt[ie.Fact] = true
		f, ok := schema.LookupInt(ie.Fact)
		if !ok {
			return Effects{}, loaderr.Errorf(iPath+".fact", fmt.Sprintf("declare %q under facts.ints or fix the fact name", ie.Fact), "ai: GOAP %s references unknown integer fact %q", where, ie.Fact)
		}
		if ie.Value < math.MinInt16 || ie.Value > math.MaxInt16 {
			return Effects{}, loaderr.Errorf(iPath+".value", fmt.Sprintf("set value within int16 range [%d, %d]", math.MinInt16, math.MaxInt16), "ai: GOAP %s integer fact %q value %d out of int16 range [%d, %d]", where, ie.Fact, ie.Value, math.MinInt16, math.MaxInt16)
		}
		switch ie.Op {
		case GOAPEffectSet:
			e = e.SetInt(f, ie.Value)
		case GOAPEffectAdd:
			if ie.Value == 0 {
				return Effects{}, loaderr.Errorf(iPath+".value", "set a non-zero delta for op: add or remove the no-op effect", "ai: GOAP %s integer fact %q add value must be non-zero", where, ie.Fact)
			}
			e = e.AddInt(f, ie.Value)
		default:
			return Effects{}, loaderr.Errorf(iPath+".op", "set op to set or add", "ai: GOAP %s integer fact %q has unknown op %q (want set or add)", where, ie.Fact, ie.Op)
		}
	}
	return e, nil
}

// GOAPBindings provides the optional procedural precondition callbacks referenced by GOAPActionDef.Check.
type GOAPBindings struct {
	// Checks maps each GOAPActionDef.Check name to a procedural precondition callback.
	Checks map[string]func(WorldState) bool
}

// GOAPSet holds the compiled FactSchema, GOAPPlanner, and prioritized GOAPGoals produced by BuildGOAP.
type GOAPSet struct {
	Schema  *FactSchema
	Planner *GOAPPlanner
	Goals   []GOAPGoal
}

// BuildGOAP validates cfg and compiles its declared facts, actions, procedural checks, and goals into a GOAPSet.
func BuildGOAP(cfg *GOAPConfig, bind GOAPBindings) (*GOAPSet, error) {
	schema, err := cfg.buildSchemaAndValidate()
	if err != nil {
		return nil, err
	}

	actions := make([]GOAPAction, len(cfg.Actions))
	for i := range cfg.Actions {
		ad := &cfg.Actions[i]
		aPath := fmt.Sprintf("actions[%d]", i)
		pre, err := compileConditions(schema, ad.Preconditions, aPath+".preconditions", fmt.Sprintf("action %q preconditions", ad.Name))
		if err != nil {
			return nil, err
		}
		eff, err := compileEffects(schema, ad.Effects, aPath+".effects", fmt.Sprintf("action %q effects", ad.Name))
		if err != nil {
			return nil, err
		}
		var check func(WorldState) bool
		if ad.Check != "" {
			check = bind.Checks[ad.Check]
			if check == nil {
				return nil, fmt.Errorf("ai: GOAP action %q references unbound check %q", ad.Name, ad.Check)
			}
		}
		actions[i] = GOAPAction{
			Name:          ad.Name,
			Preconditions: pre,
			Effects:       eff,
			Cost:          ad.Cost,
			Check:         check,
		}
	}

	planner, err := NewGOAPPlanner(actions...)
	if err != nil {
		return nil, err
	}
	if cfg.NodeBudget > 0 {
		planner.NodeBudget = cfg.NodeBudget
	}

	goals := make([]GOAPGoal, len(cfg.Goals))
	for i := range cfg.Goals {
		gd := &cfg.Goals[i]
		target, err := compileConditions(schema, gd.Target, fmt.Sprintf("goals[%d].target", i), fmt.Sprintf("goal %q target", gd.Name))
		if err != nil {
			return nil, err
		}
		goals[i] = GOAPGoal{
			Name:     gd.Name,
			Priority: gd.Priority,
			Target:   target,
		}
	}

	return &GOAPSet{
		Schema:  schema,
		Planner: planner,
		Goals:   goals,
	}, nil
}
