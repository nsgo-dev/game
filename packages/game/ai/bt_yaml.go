package ai

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// BTFormatVersion is the behavior tree YAML schema version this package reads and writes.
const BTFormatVersion = 1

// BTSchemaURL is where the JSON Schema of version 1 behavior tree documents is published.
const BTSchemaURL = "https://game.nsgo.dev/schemas/bt.v1.json"

// BTSchemaComment is the first line [EncodeBehaviorTree] writes so YAML language
// servers validate and complete behavior tree files automatically.
const BTSchemaComment = "# yaml-language-server: $schema=" + BTSchemaURL

const fixBTDoc = "correct the behavior tree document to match its version 1 schema in docs/en/ai.md"

// BTNodeType identifies the kind of node in a [BehaviorTreeDoc].
type BTNodeType string

const (
	// BTNodeSequence runs children in order until all succeed or one fails.
	BTNodeSequence BTNodeType = "sequence"
	// BTNodeSelector tries children in priority order until one succeeds or runs.
	BTNodeSelector BTNodeType = "selector"
	// BTNodeParallel ticks all running children each step according to Policy.
	BTNodeParallel BTNodeType = "parallel"
	// BTNodeRandomSelector tries children in a deterministic shuffled order from Stream.
	BTNodeRandomSelector BTNodeType = "random_selector"
	// BTNodeRandomSequence runs all children in a deterministic shuffled order from Stream.
	BTNodeRandomSequence BTNodeType = "random_sequence"
	// BTNodeWeightedSelector draws children without replacement by each child's Weight.
	BTNodeWeightedSelector BTNodeType = "weighted_selector"
	// BTNodeGuard ticks its child only while a registered condition holds, resetting child if it turns false.
	BTNodeGuard BTNodeType = "guard"
	// BTNodeCooldown blocks re-entry into its child for Duration after success.
	BTNodeCooldown BTNodeType = "cooldown"
	// BTNodeTimeLimit fails and resets its child if it stays running past Duration.
	BTNodeTimeLimit BTNodeType = "time_limit"
	// BTNodeRetry reruns a failing child up to Count additional times.
	BTNodeRetry BTNodeType = "retry"
	// BTNodeService invokes a registered callback on entry and periodically every Interval while its child runs.
	BTNodeService BTNodeType = "service"
	// BTNodeInverter flips StatusSuccess and StatusFailure from its child.
	BTNodeInverter BTNodeType = "inverter"
	// BTNodeSucceeder turns both StatusSuccess and StatusFailure from its child into StatusSuccess.
	BTNodeSucceeder BTNodeType = "succeeder"
	// BTNodeFailer turns both StatusSuccess and StatusFailure from its child into StatusFailure.
	BTNodeFailer BTNodeType = "failer"
	// BTNodeRepeater repeats its child Count times (or indefinitely when Count is 0).
	BTNodeRepeater BTNodeType = "repeater"
	// BTNodeUntilFail repeats its child until it returns StatusFailure, then succeeds.
	BTNodeUntilFail BTNodeType = "until_fail"
	// BTNodeBlackboardCondition checks a blackboard key with optional observer aborts and optional wrapped child.
	BTNodeBlackboardCondition BTNodeType = "blackboard_condition"
	// BTNodeWait returns StatusRunning until accumulated dt reaches Duration, then succeeds.
	BTNodeWait BTNodeType = "wait"
	// BTNodeAction invokes a game-registered leaf action factory.
	BTNodeAction BTNodeType = "action"
	// BTNodeCondition invokes a game-registered leaf condition factory.
	BTNodeCondition BTNodeType = "condition"
	// BTNodeSubtree includes another *.bt.yaml file by Path with optional Params and Keys bindings.
	BTNodeSubtree BTNodeType = "subtree"
)

// BTParallelPolicyName names the completion policy of a parallel node in YAML.
type BTParallelPolicyName string

const (
	// BTParallelRequireAll succeeds when all children succeed and fails as soon as any child fails.
	BTParallelRequireAll BTParallelPolicyName = "require_all"
	// BTParallelRequireOne succeeds as soon as any child succeeds and fails when every child fails.
	BTParallelRequireOne BTParallelPolicyName = "require_one"
)

// BehaviorTreeDoc is the root of a versioned YAML behavior tree document (*.bt.yaml).
type BehaviorTreeDoc struct {
	// Version is the schema version; must equal BTFormatVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Name is an optional human-readable identifier for this behavior tree.
	Name string `yaml:"name,omitempty"`
	// Params declares optional default parameter values substituted for "${name}" references in this tree.
	Params map[string]any `yaml:"params,omitempty"`
	// Blackboard declares typed blackboard keys and optional initial defaults used by this tree.
	Blackboard []BTBlackboardKeyDoc `yaml:"blackboard,omitempty"`
	// Root is the root node of the behavior tree.
	Root BTNodeDoc `yaml:"root" schema:"required"`

	sourceFile string
	sourceData []byte
}

// BTBlackboardKeyDoc declares one typed blackboard key and its optional initial default value.
type BTBlackboardKeyDoc struct {
	// Key is the blackboard key name.
	Key string `yaml:"key" schema:"required,minLength=1"`
	// Type is the value kind: bool, int, float, string, vec2, vec3, or entity.
	Type BlackboardKindName `yaml:"type" schema:"required,enum=bool|int|float|string|vec2|vec3|entity"`
	// Shared targets the parent (squad or team) blackboard instead of the agent's local blackboard when seeding Default.
	Shared bool `yaml:"shared,omitempty"`
	// Default is an optional initial value matching Type (scalar, [x, y] for vec2, or [x, y, z] for vec3).
	Default *any `yaml:"default,omitempty"`
}

// BTNodeDoc describes one composite, decorator, leaf, or subtree reference node in a [BehaviorTreeDoc].
type BTNodeDoc struct {
	// Type selects the behavior tree node kind.
	Type BTNodeType `yaml:"type" schema:"required,enum=sequence|selector|parallel|random_selector|random_sequence|weighted_selector|guard|cooldown|time_limit|retry|service|inverter|succeeder|failer|repeater|until_fail|blackboard_condition|wait|action|condition|subtree"`
	// Name is an optional label for authoring and debugging.
	Name string `yaml:"name,omitempty"`
	// Weight is the positive relative selection weight when this node is a direct child of weighted_selector.
	Weight float64 `yaml:"weight,omitempty" schema:"exclusiveMinimum=0"`
	// Policy selects the completion policy for parallel nodes: require_all (default) or require_one.
	Policy BTParallelPolicyName `yaml:"policy,omitempty" schema:"enum=require_all|require_one"`
	// Action names the game-registered action factory for action leaf nodes.
	Action string `yaml:"action,omitempty"`
	// Condition names the game-registered condition factory for condition leaves and guard decorators.
	Condition string `yaml:"condition,omitempty"`
	// Service names the game-registered service factory for service decorators.
	Service string `yaml:"service,omitempty"`
	// Duration is a Go duration string (such as "500ms" or "1.5s") or "${param}" reference for wait, cooldown, and time_limit nodes.
	Duration string `yaml:"duration,omitempty"`
	// Interval is a Go duration string or "${param}" reference controlling periodic ticks of a service decorator (omitted or "0s" runs every tick).
	Interval string `yaml:"interval,omitempty"`
	// Count is the iteration or retry count for repeater (0 repeats indefinitely) and retry (0 disables retries) decorators.
	Count int `yaml:"count,omitempty" schema:"minimum=0"`
	// Key is the blackboard key inspected by a blackboard_condition node.
	Key string `yaml:"key,omitempty"`
	// Op is the comparison operator for a blackboard_condition node: eq, ne, lt, lte, gt, gte, set, or unset.
	Op BlackboardOp `yaml:"op,omitempty" schema:"enum=eq|ne|lt|lte|gt|gte|set|unset"`
	// Value is the comparison operand for a blackboard_condition node (required for eq, ne, lt, lte, gt, gte; omitted for set and unset).
	Value *any `yaml:"value,omitempty"`
	// ValueType optionally specifies the blackboard type of Value (bool, int, float, string, vec2, vec3, or entity) when disambiguation is needed.
	ValueType BlackboardKindName `yaml:"valueType,omitempty" schema:"enum=bool|int|float|string|vec2|vec3|entity"`
	// Abort controls observer abort behavior for a blackboard_condition node: none (default), self, lower, or both.
	Abort AbortMode `yaml:"abort,omitempty" schema:"enum=none|self|lower|both"`
	// Path is the relative or filesystem path to another *.bt.yaml file for a subtree node.
	Path string `yaml:"path,omitempty"`
	// Params supplies typed parameters to action, condition, guard, service, or subtree nodes.
	Params map[string]any `yaml:"params,omitempty"`
	// Keys maps subtree-local blackboard key names to caller blackboard key names for a subtree node.
	Keys map[string]string `yaml:"keys,omitempty"`
	// Child is the wrapped child node for decorator nodes (optional on blackboard_condition).
	Child *BTNodeDoc `yaml:"child,omitempty"`
	// Children are the ordered child nodes for composite nodes.
	Children []BTNodeDoc `yaml:"children,omitempty"`
}

// ParseBehaviorTree decodes and validates a version 1 YAML behavior tree document.
// Unknown fields, unknown node types, invalid operators or abort modes, unsupported
// versions, and trailing YAML documents are refused with a [*loaderr.Error].
func ParseBehaviorTree(data []byte) (*BehaviorTreeDoc, error) {
	return ParseBehaviorTreeWith(data, "")
}

// ParseBehaviorTreeWith is [ParseBehaviorTree] with a source file name attached to
// returned errors and checked for direct self-referencing subtree cycles.
func ParseBehaviorTreeWith(data []byte, file string) (*BehaviorTreeDoc, error) {
	doc, err := parseBTDoc(data, file)
	if err != nil {
		return nil, loaderr.Locate(err, file, data, fixBTDoc)
	}
	return doc, nil
}

func parseBTDoc(data []byte, file string) (*BehaviorTreeDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{
			Problem: "ai: empty behavior tree document",
			Fix:     fixBTDoc,
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc BehaviorTreeDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("ai: decode behavior tree: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				Line:    btTrailingDocumentLine(data),
				Problem: "ai: unexpected trailing YAML document in behavior tree",
				Fix:     "keep one behavior tree per file; remove the --- and the document after it",
			}
		}
		return nil, fmt.Errorf("ai: decode behavior tree: %w", err)
	}
	if err := ValidateBehaviorTree(&doc); err != nil {
		return nil, err
	}
	if file != "" {
		cleanFile := cleanBTPath(file)
		if err := checkSelfCycle(&doc.Root, "root", cleanFile); err != nil {
			return nil, err
		}
	}
	doc.sourceFile = file
	doc.sourceData = bytes.Clone(data)
	return &doc, nil
}

func checkSelfCycle(n *BTNodeDoc, field, curFile string) error {
	if n.Type == BTNodeSubtree {
		target := resolveBTPath(curFile, n.Path)
		if target == curFile {
			return &loaderr.Error{
				Field:   loaderr.Join(field, "path"),
				Problem: fmt.Sprintf("ai: subtree cycle detected: %s -> %s", curFile, target),
				Fix:     "remove the circular subtree reference",
			}
		}
	}
	if n.Child != nil {
		if err := checkSelfCycle(n.Child, loaderr.Join(field, "child"), curFile); err != nil {
			return err
		}
	}
	for i := range n.Children {
		if err := checkSelfCycle(&n.Children[i], loaderr.Join(field, "children", i), curFile); err != nil {
			return err
		}
	}
	return nil
}

func btTrailingDocumentLine(data []byte) int {
	sawContent := false
	for i, line := range bytes.Split(data, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("---")); ok && (len(rest) == 0 || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r') {
			if sawContent {
				return i + 1
			}
			continue
		}
		if t := bytes.TrimSpace(line); len(t) > 0 && t[0] != '#' {
			sawContent = true
		}
	}
	return 0
}

// EncodeBehaviorTree validates doc and serializes it as deterministic 2-space YAML
// in fixed field order prefixed with [BTSchemaComment].
func EncodeBehaviorTree(doc *BehaviorTreeDoc) ([]byte, error) {
	if err := ValidateBehaviorTree(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(BTSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("ai: encode behavior tree: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("ai: encode behavior tree: %w", err)
	}
	return buf.Bytes(), nil
}

// ValidateBehaviorTree checks that doc conforms to the version 1 behavior tree schema.
func ValidateBehaviorTree(doc *BehaviorTreeDoc) error {
	if doc == nil {
		return &loaderr.Error{Problem: "ai: nil behavior tree document", Fix: fixBTDoc}
	}
	if doc.Version != BTFormatVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("ai: unsupported behavior tree version %d", doc.Version),
			Expected: strconv.Itoa(BTFormatVersion),
			Fix:      fmt.Sprintf("set version to %d", BTFormatVersion),
		}
	}
	if err := validateBTParamsMap(doc.Params, "params"); err != nil {
		return err
	}
	declaredKeys := make(map[string]BlackboardKind, len(doc.Blackboard))
	for i := range doc.Blackboard {
		entry := &doc.Blackboard[i]
		entryPath := loaderr.Path("blackboard", i)
		if entry.Key == "" {
			return &loaderr.Error{
				Field:    loaderr.Join(entryPath, "key"),
				Problem:  "ai: blackboard key must not be empty",
				Expected: "non-empty key name",
				Fix:      "set key to a non-empty blackboard key name",
			}
		}
		if _, dup := declaredKeys[entry.Key]; dup {
			return &loaderr.Error{
				Field:   loaderr.Join(entryPath, "key"),
				Problem: fmt.Sprintf("ai: blackboard key %q is declared twice", entry.Key),
				Fix:     "remove the duplicate blackboard key declaration",
			}
		}
		kind, ok := entry.Type.Kind()
		if !ok {
			return &loaderr.Error{
				Field:    loaderr.Join(entryPath, "type"),
				Problem:  fmt.Sprintf("ai: unknown blackboard key type %q", entry.Type),
				Expected: "one of bool, int, float, string, vec2, vec3, entity",
				Fix:      "set type to a supported blackboard type",
			}
		}
		declaredKeys[entry.Key] = kind
		if entry.Default != nil {
			if _, err := decodeBlackboardValue(*entry.Default, kind); err != nil {
				return &loaderr.Error{
					Field:   loaderr.Join(entryPath, "default"),
					Problem: fmt.Sprintf("ai: blackboard key %q default: %v", entry.Key, err),
					Fix:     fmt.Sprintf("provide a valid %s value for default", entry.Type),
				}
			}
		}
	}
	return validateBTNode(&doc.Root, "root", false, doc.Params, declaredKeys)
}

func validateBTParamsMap(m map[string]any, field string) error {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if k == "" {
			return &loaderr.Error{
				Field:   field,
				Problem: "ai: parameter name must not be empty",
				Fix:     "use non-empty parameter names",
			}
		}
		if err := validateBTParamValue(m[k]); err != nil {
			return &loaderr.Error{
				Field:   loaderr.Join(field, k),
				Problem: fmt.Sprintf("ai: parameter %q: %v", k, err),
				Fix:     "use a boolean, finite number, string, or numeric [x, y] / [x, y, z] vector",
			}
		}
	}
	return nil
}

func validateBTParamValue(v any) error {
	switch x := v.(type) {
	case bool, string, int, int64, uint64:
		return nil
	case float64:
		if !isFiniteFloat64(x) {
			return fmt.Errorf("non-finite float %v", x)
		}
		return nil
	case float32:
		if !isFiniteFloat64(float64(x)) {
			return fmt.Errorf("non-finite float %v", x)
		}
		return nil
	case []any:
		if len(x) != 2 && len(x) != 3 {
			return fmt.Errorf("vector parameter must have 2 or 3 elements, got %d", len(x))
		}
		for i, elem := range x {
			f, ok := toFiniteFloat64(elem)
			if !ok {
				return fmt.Errorf("vector element %d (%v) must be a finite number", i, elem)
			}
			_ = f
		}
		return nil
	default:
		return fmt.Errorf("unsupported parameter type %T", v)
	}
}

func validateBTNode(n *BTNodeDoc, field string, inWeighted bool, docParams map[string]any, declaredKeys map[string]BlackboardKind) error {
	if inWeighted {
		if !isFiniteFloat64(n.Weight) || n.Weight <= 0 {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "weight"),
				Problem:  fmt.Sprintf("ai: weighted_selector child weight %v must be finite and positive", n.Weight),
				Expected: "a positive finite number",
				Fix:      "set weight to a number greater than 0",
			}
		}
	} else if n.Weight != 0 {
		return &loaderr.Error{
			Field:   loaderr.Join(field, "weight"),
			Problem: "ai: weight is only valid on direct children of weighted_selector",
			Fix:     "remove weight or place this node inside a weighted_selector",
		}
	}

	// Refuse fields not belonging to this node type.
	reject := func(cond bool, key string) error {
		if !cond {
			return nil
		}
		return &loaderr.Error{
			Field:   loaderr.Join(field, key),
			Problem: fmt.Sprintf("ai: field %q is not valid on node type %q", key, n.Type),
			Fix:     fmt.Sprintf("remove %q from this %s node", key, n.Type),
		}
	}

	switch n.Type {
	case BTNodeSequence, BTNodeSelector, BTNodeRandomSelector, BTNodeRandomSequence, BTNodeWeightedSelector, BTNodeParallel:
		if err := firstErr(
			reject(n.Type != BTNodeParallel && n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Condition != "", "condition"),
			reject(n.Service != "", "service"),
			reject(n.Duration != "", "duration"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Params) > 0, "params"),
			reject(len(n.Keys) > 0, "keys"),
			reject(n.Child != nil, "child"),
		); err != nil {
			return err
		}
		if n.Type == BTNodeParallel && n.Policy != "" && n.Policy != BTParallelRequireAll && n.Policy != BTParallelRequireOne {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "policy"),
				Problem:  fmt.Sprintf("ai: unknown parallel policy %q", n.Policy),
				Expected: "require_all or require_one",
				Fix:      "set policy to require_all or require_one",
			}
		}
		if len(n.Children) == 0 {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "children"),
				Problem:  fmt.Sprintf("ai: %s node requires at least one child", n.Type),
				Expected: "non-empty children list",
				Fix:      "add one or more child nodes under children",
			}
		}
		childWeighted := n.Type == BTNodeWeightedSelector
		for i := range n.Children {
			if err := validateBTNode(&n.Children[i], loaderr.Join(field, "children", i), childWeighted, docParams, declaredKeys); err != nil {
				return err
			}
		}
		return nil

	case BTNodeInverter, BTNodeSucceeder, BTNodeFailer, BTNodeUntilFail:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Condition != "", "condition"),
			reject(n.Service != "", "service"),
			reject(n.Duration != "", "duration"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Params) > 0, "params"),
			reject(len(n.Keys) > 0, "keys"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		if n.Child == nil {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "child"),
				Problem:  fmt.Sprintf("ai: %s decorator requires a child", n.Type),
				Expected: "a child node",
				Fix:      "add a child node",
			}
		}
		return validateBTNode(n.Child, loaderr.Join(field, "child"), false, docParams, declaredKeys)

	case BTNodeRepeater, BTNodeRetry:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Condition != "", "condition"),
			reject(n.Service != "", "service"),
			reject(n.Duration != "", "duration"),
			reject(n.Interval != "", "interval"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Params) > 0, "params"),
			reject(len(n.Keys) > 0, "keys"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		if n.Count < 0 {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "count"),
				Problem:  fmt.Sprintf("ai: %s count %d must be non-negative", n.Type, n.Count),
				Expected: "integer >= 0",
				Fix:      "set count to 0 or a positive integer",
			}
		}
		if n.Child == nil {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "child"),
				Problem:  fmt.Sprintf("ai: %s decorator requires a child", n.Type),
				Expected: "a child node",
				Fix:      "add a child node",
			}
		}
		return validateBTNode(n.Child, loaderr.Join(field, "child"), false, docParams, declaredKeys)

	case BTNodeCooldown, BTNodeTimeLimit:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Condition != "", "condition"),
			reject(n.Service != "", "service"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Params) > 0, "params"),
			reject(len(n.Keys) > 0, "keys"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		mustPositive := n.Type == BTNodeTimeLimit
		if err := validateBTDurationField(n.Duration, loaderr.Join(field, "duration"), true, mustPositive, docParams); err != nil {
			return err
		}
		if n.Child == nil {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "child"),
				Problem:  fmt.Sprintf("ai: %s decorator requires a child", n.Type),
				Expected: "a child node",
				Fix:      "add a child node",
			}
		}
		return validateBTNode(n.Child, loaderr.Join(field, "child"), false, docParams, declaredKeys)

	case BTNodeGuard:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Service != "", "service"),
			reject(n.Duration != "", "duration"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Keys) > 0, "keys"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		if n.Condition == "" {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "condition"),
				Problem:  "ai: guard decorator requires a condition name",
				Expected: "non-empty condition name",
				Fix:      "set condition to the name of a registered condition",
			}
		}
		if err := validateBTParamsMap(n.Params, loaderr.Join(field, "params")); err != nil {
			return err
		}
		if n.Child == nil {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "child"),
				Problem:  "ai: guard decorator requires a child",
				Expected: "a child node",
				Fix:      "add a child node",
			}
		}
		return validateBTNode(n.Child, loaderr.Join(field, "child"), false, docParams, declaredKeys)

	case BTNodeService:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Condition != "", "condition"),
			reject(n.Duration != "", "duration"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Keys) > 0, "keys"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		if n.Service == "" {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "service"),
				Problem:  "ai: service decorator requires a service name",
				Expected: "non-empty service name",
				Fix:      "set service to the name of a registered service callback",
			}
		}
		if err := validateBTDurationField(n.Interval, loaderr.Join(field, "interval"), false, false, docParams); err != nil {
			return err
		}
		if err := validateBTParamsMap(n.Params, loaderr.Join(field, "params")); err != nil {
			return err
		}
		if n.Child == nil {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "child"),
				Problem:  "ai: service decorator requires a child",
				Expected: "a child node",
				Fix:      "add a child node",
			}
		}
		return validateBTNode(n.Child, loaderr.Join(field, "child"), false, docParams, declaredKeys)

	case BTNodeBlackboardCondition:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Condition != "", "condition"),
			reject(n.Service != "", "service"),
			reject(n.Duration != "", "duration"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Path != "", "path"),
			reject(len(n.Params) > 0, "params"),
			reject(len(n.Keys) > 0, "keys"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		if n.Key == "" {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "key"),
				Problem:  "ai: blackboard_condition requires a key",
				Expected: "non-empty blackboard key name",
				Fix:      "set key to the blackboard key to evaluate",
			}
		}
		if !n.Op.Valid() {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "op"),
				Problem:  fmt.Sprintf("ai: unknown blackboard_condition op %q", n.Op),
				Expected: "one of eq, ne, lt, lte, gt, gte, set, unset",
				Fix:      "set op to a supported comparison operator",
			}
		}
		if !n.Abort.Valid() {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "abort"),
				Problem:  fmt.Sprintf("ai: unknown blackboard_condition abort mode %q", n.Abort),
				Expected: "one of none, self, lower, both",
				Fix:      "set abort to none, self, lower, or both",
			}
		}
		var explicitKind BlackboardKind
		if n.ValueType != "" {
			if !n.Op.NeedsValue() {
				return &loaderr.Error{
					Field:   loaderr.Join(field, "valueType"),
					Problem: fmt.Sprintf("ai: valueType cannot be set for op %q", n.Op),
					Fix:     "remove valueType when using set or unset",
				}
			}
			k, ok := n.ValueType.Kind()
			if !ok {
				return &loaderr.Error{
					Field:    loaderr.Join(field, "valueType"),
					Problem:  fmt.Sprintf("ai: unknown valueType %q", n.ValueType),
					Expected: "one of bool, int, float, string, vec2, vec3, entity",
					Fix:      "set valueType to a supported blackboard type",
				}
			}
			explicitKind = k
		}
		if n.Op.NeedsValue() {
			if n.Value == nil {
				return &loaderr.Error{
					Field:    loaderr.Join(field, "value"),
					Problem:  fmt.Sprintf("ai: blackboard_condition op %q requires a value", n.Op),
					Expected: "comparison value",
					Fix:      "add value for the comparison",
				}
			}
			targetKind := explicitKind
			if targetKind == BlackboardNone {
				targetKind = declaredKeys[n.Key]
			}
			if paramName, isPlaceholder := parseParamPlaceholder(*n.Value); isPlaceholder {
				if def, hasDef := docParams[paramName]; hasDef {
					val, err := decodeBlackboardValue(def, targetKind)
					if err != nil {
						return &loaderr.Error{
							Field:   loaderr.Join(field, "value"),
							Problem: fmt.Sprintf("ai: default for param %q: %v", paramName, err),
							Fix:     "set a compatible default value or valueType",
						}
					}
					if err := checkOpKindCompatible(n.Op, val.Kind, loaderr.Join(field, "op")); err != nil {
						return err
					}
				}
			} else {
				val, err := decodeBlackboardValue(*n.Value, targetKind)
				if err != nil {
					return &loaderr.Error{
						Field:   loaderr.Join(field, "value"),
						Problem: fmt.Sprintf("ai: invalid blackboard_condition value: %v", err),
						Fix:     "provide a valid scalar or [x, y] / [x, y, z] vector value",
					}
				}
				if err := checkOpKindCompatible(n.Op, val.Kind, loaderr.Join(field, "op")); err != nil {
					return err
				}
			}
		} else if n.Value != nil {
			return &loaderr.Error{
				Field:   loaderr.Join(field, "value"),
				Problem: fmt.Sprintf("ai: blackboard_condition op %q does not take a value", n.Op),
				Fix:     "remove value when using set or unset",
			}
		}
		if n.Child != nil {
			return validateBTNode(n.Child, loaderr.Join(field, "child"), false, docParams, declaredKeys)
		}
		return nil

	case BTNodeWait:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Condition != "", "condition"),
			reject(n.Service != "", "service"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Params) > 0, "params"),
			reject(len(n.Keys) > 0, "keys"),
			reject(n.Child != nil, "child"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		return validateBTDurationField(n.Duration, loaderr.Join(field, "duration"), true, false, docParams)

	case BTNodeAction:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Condition != "", "condition"),
			reject(n.Service != "", "service"),
			reject(n.Duration != "", "duration"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Keys) > 0, "keys"),
			reject(n.Child != nil, "child"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		if n.Action == "" {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "action"),
				Problem:  "ai: action node requires an action name",
				Expected: "non-empty action name",
				Fix:      "set action to the name of a registered action",
			}
		}
		return validateBTParamsMap(n.Params, loaderr.Join(field, "params"))

	case BTNodeCondition:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Service != "", "service"),
			reject(n.Duration != "", "duration"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Path != "", "path"),
			reject(len(n.Keys) > 0, "keys"),
			reject(n.Child != nil, "child"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		if n.Condition == "" {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "condition"),
				Problem:  "ai: condition node requires a condition name",
				Expected: "non-empty condition name",
				Fix:      "set condition to the name of a registered condition",
			}
		}
		return validateBTParamsMap(n.Params, loaderr.Join(field, "params"))

	case BTNodeSubtree:
		if err := firstErr(
			reject(n.Policy != "", "policy"),
			reject(n.Action != "", "action"),
			reject(n.Condition != "", "condition"),
			reject(n.Service != "", "service"),
			reject(n.Duration != "", "duration"),
			reject(n.Interval != "", "interval"),
			reject(n.Count != 0, "count"),
			reject(n.Key != "", "key"),
			reject(n.Op != "", "op"),
			reject(n.Value != nil, "value"),
			reject(n.ValueType != "", "valueType"),
			reject(n.Abort != "", "abort"),
			reject(n.Child != nil, "child"),
			reject(len(n.Children) > 0, "children"),
		); err != nil {
			return err
		}
		if n.Path == "" {
			return &loaderr.Error{
				Field:    loaderr.Join(field, "path"),
				Problem:  "ai: subtree node requires a path",
				Expected: "path to a *.bt.yaml file",
				Fix:      "set path to the referenced behavior tree file",
			}
		}
		if err := validateBTParamsMap(n.Params, loaderr.Join(field, "params")); err != nil {
			return err
		}
		for k, v := range n.Keys {
			if k == "" || v == "" {
				return &loaderr.Error{
					Field:   loaderr.Join(field, "keys"),
					Problem: "ai: subtree key bindings must map non-empty local key names to non-empty target keys",
					Fix:     "remove empty key names from keys",
				}
			}
		}
		return nil

	default:
		return &loaderr.Error{
			Field:    loaderr.Join(field, "type"),
			Problem:  fmt.Sprintf("ai: unknown behavior tree node type %q", n.Type),
			Expected: "sequence, selector, parallel, random_selector, random_sequence, weighted_selector, guard, cooldown, time_limit, retry, service, inverter, succeeder, failer, repeater, until_fail, blackboard_condition, wait, action, condition, or subtree",
			Fix:      "set type to a supported behavior tree node kind",
		}
	}
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func checkOpKindCompatible(op BlackboardOp, kind BlackboardKind, field string) error {
	switch op {
	case BBOpLt, BBOpLte, BBOpGt, BBOpGte:
		switch kind {
		case BlackboardInt, BlackboardFloat, BlackboardString, BlackboardEntity:
			return nil
		default:
			return &loaderr.Error{
				Field:   field,
				Problem: fmt.Sprintf("ai: operator %q is not supported for %s values", op, kind),
				Fix:     "use eq or ne for bool, vec2, and vec3 values",
			}
		}
	default:
		return nil
	}
}

func validateBTDurationField(raw, field string, required, mustPositive bool, docParams map[string]any) error {
	if raw == "" {
		if required {
			return &loaderr.Error{
				Field:    field,
				Problem:  "ai: duration is required",
				Expected: `a Go duration string such as "500ms" or "1.5s"`,
				Fix:      "set duration to a valid Go duration string",
			}
		}
		return nil
	}
	if paramName, ok := parseParamPlaceholder(raw); ok {
		if def, hasDef := docParams[paramName]; hasDef {
			s, ok := def.(string)
			if !ok {
				return &loaderr.Error{
					Field:    field,
					Problem:  fmt.Sprintf("ai: default for param %q is %T, want duration string", paramName, def),
					Expected: "a Go duration string",
					Fix:      fmt.Sprintf("set params.%s to a valid Go duration string", paramName),
				}
			}
			return validateLiteralDuration(s, field, mustPositive)
		}
		return nil
	}
	return validateLiteralDuration(raw, field, mustPositive)
}

func validateLiteralDuration(raw, field string, mustPositive bool) error {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return &loaderr.Error{
			Field:    field,
			Problem:  fmt.Sprintf("ai: invalid duration %q: %v", raw, err),
			Expected: `a Go duration string such as "250ms" or "2s"`,
			Fix:      "use a valid Go duration string",
			Err:      err,
		}
	}
	if mustPositive && d <= 0 {
		return &loaderr.Error{
			Field:    field,
			Problem:  fmt.Sprintf("ai: duration %q must be positive", raw),
			Expected: "duration > 0",
			Fix:      "set a positive duration",
		}
	}
	if !mustPositive && d < 0 {
		return &loaderr.Error{
			Field:    field,
			Problem:  fmt.Sprintf("ai: duration %q must be non-negative", raw),
			Expected: "duration >= 0",
			Fix:      "set a non-negative duration",
		}
	}
	return nil
}

// parseParamPlaceholder recognizes "${ident}" placeholders.
func parseParamPlaceholder(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || len(s) < 4 || !strings.HasPrefix(s, "${") || !strings.HasSuffix(s, "}") {
		return "", false
	}
	name := s[2 : len(s)-1]
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
				return "", false
			}
		} else {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
				return "", false
			}
		}
	}
	return name, true
}

func decodeBlackboardValue(raw any, expected BlackboardKind) (BlackboardValue, error) {
	if expected == BlackboardNone {
		switch x := raw.(type) {
		case bool:
			return BBBool(x), nil
		case int:
			return BBInt(x), nil
		case int64:
			return BBInt(int(x)), nil
		case uint64:
			return BBEntity(x), nil
		case float64:
			if !isFiniteFloat64(x) {
				return BlackboardValue{}, fmt.Errorf("non-finite float %v", x)
			}
			return BBFloat(x), nil
		case float32:
			if !isFiniteFloat64(float64(x)) {
				return BlackboardValue{}, fmt.Errorf("non-finite float %v", x)
			}
			return BBFloat(float64(x)), nil
		case string:
			return BBString(x), nil
		case []any:
			switch len(x) {
			case 2:
				v, err := decodeVec2(x)
				if err != nil {
					return BlackboardValue{}, err
				}
				return BBVec2(v), nil
			case 3:
				v, err := decodeVec3(x)
				if err != nil {
					return BlackboardValue{}, err
				}
				return BBVec3(v), nil
			default:
				return BlackboardValue{}, fmt.Errorf("vector must have 2 or 3 elements, got %d", len(x))
			}
		default:
			return BlackboardValue{}, fmt.Errorf("unsupported value type %T", raw)
		}
	}

	switch expected {
	case BlackboardBool:
		b, ok := raw.(bool)
		if !ok {
			return BlackboardValue{}, fmt.Errorf("want bool, got %T", raw)
		}
		return BBBool(b), nil
	case BlackboardInt:
		n, ok := toExactInt(raw)
		if !ok {
			return BlackboardValue{}, fmt.Errorf("want integer, got %v (%T)", raw, raw)
		}
		return BBInt(n), nil
	case BlackboardFloat:
		f, ok := toFiniteFloat64(raw)
		if !ok {
			return BlackboardValue{}, fmt.Errorf("want finite number, got %v (%T)", raw, raw)
		}
		return BBFloat(f), nil
	case BlackboardString:
		s, ok := raw.(string)
		if !ok {
			return BlackboardValue{}, fmt.Errorf("want string, got %T", raw)
		}
		return BBString(s), nil
	case BlackboardVec2:
		arr, ok := raw.([]any)
		if !ok || len(arr) != 2 {
			return BlackboardValue{}, fmt.Errorf("want [x, y] vector of 2 numbers")
		}
		v, err := decodeVec2(arr)
		if err != nil {
			return BlackboardValue{}, err
		}
		return BBVec2(v), nil
	case BlackboardVec3:
		arr, ok := raw.([]any)
		if !ok || len(arr) != 3 {
			return BlackboardValue{}, fmt.Errorf("want [x, y, z] vector of 3 numbers")
		}
		v, err := decodeVec3(arr)
		if err != nil {
			return BlackboardValue{}, err
		}
		return BBVec3(v), nil
	case BlackboardEntity:
		id, ok := toEntityID(raw)
		if !ok {
			return BlackboardValue{}, fmt.Errorf("want non-negative integer entity ID, got %v (%T)", raw, raw)
		}
		return BBEntity(id), nil
	default:
		return BlackboardValue{}, fmt.Errorf("unknown blackboard kind %v", expected)
	}
}

func decodeVec2(arr []any) (math2d.Vec2, error) {
	if len(arr) != 2 {
		return math2d.Vec2{}, fmt.Errorf("vec2 requires 2 numbers, got %d", len(arr))
	}
	x, ok := toFiniteFloat32(arr[0])
	if !ok {
		return math2d.Vec2{}, fmt.Errorf("vec2[0] must be a finite float32")
	}
	y, ok := toFiniteFloat32(arr[1])
	if !ok {
		return math2d.Vec2{}, fmt.Errorf("vec2[1] must be a finite float32")
	}
	return math2d.Vec2{X: x, Y: y}, nil
}

func decodeVec3(arr []any) (Vec3, error) {
	if len(arr) != 3 {
		return Vec3{}, fmt.Errorf("vec3 requires 3 numbers, got %d", len(arr))
	}
	x, ok := toFiniteFloat32(arr[0])
	if !ok {
		return Vec3{}, fmt.Errorf("vec3[0] must be a finite float32")
	}
	y, ok := toFiniteFloat32(arr[1])
	if !ok {
		return Vec3{}, fmt.Errorf("vec3[1] must be a finite float32")
	}
	z, ok := toFiniteFloat32(arr[2])
	if !ok {
		return Vec3{}, fmt.Errorf("vec3[2] must be a finite float32")
	}
	return Vec3{X: x, Y: y, Z: z}, nil
}

func toExactInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case uint64:
		if x > math.MaxInt {
			return 0, false
		}
		return int(x), true
	default:
		return 0, false
	}
}

func toEntityID(v any) (uint64, bool) {
	switch x := v.(type) {
	case uint64:
		return x, true
	case int:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case int64:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	default:
		return 0, false
	}
}

func toFiniteFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, isFiniteFloat64(x)
	case float32:
		f := float64(x)
		return f, isFiniteFloat64(f)
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	default:
		return 0, false
	}
}

func toFiniteFloat32(v any) (float32, bool) {
	f, ok := toFiniteFloat64(v)
	if !ok || math.Abs(f) > math.MaxFloat32 {
		return 0, false
	}
	return float32(f), true
}

func cleanBTPath(p string) string {
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}

func resolveBTPath(curFile, subPath string) string {
	sub := strings.ReplaceAll(subPath, "\\", "/")
	if curFile == "" || strings.HasPrefix(sub, "/") {
		return path.Clean(strings.TrimPrefix(sub, "/"))
	}
	dir := path.Dir(cleanBTPath(curFile))
	if dir == "." {
		return path.Clean(sub)
	}
	return path.Clean(path.Join(dir, sub))
}

// LoadBehaviorTreeFS reads, parses, and validates the behavior tree at name in fsys,
// including checking all transitively referenced subtrees for parse errors and cycles.
func LoadBehaviorTreeFS(fsys fs.FS, name string) (*BehaviorTreeDoc, error) {
	if fsys == nil {
		return nil, &loaderr.Error{File: name, Problem: "ai: nil filesystem", Fix: "pass a non-nil fs.FS"}
	}
	cleanName := cleanBTPath(name)
	data, err := fs.ReadFile(fsys, cleanName)
	if err != nil {
		return nil, &loaderr.Error{
			File:    cleanName,
			Problem: fmt.Sprintf("ai: read behavior tree %q: %v", cleanName, err),
			Fix:     "ensure the behavior tree file exists in the filesystem",
			Err:     err,
		}
	}
	doc, err := ParseBehaviorTreeWith(data, cleanName)
	if err != nil {
		return nil, err
	}
	if err := validateSubtreesFS(fsys, doc, []string{cleanName}); err != nil {
		return nil, err
	}
	return doc, nil
}

func validateSubtreesFS(fsys fs.FS, doc *BehaviorTreeDoc, stack []string) error {
	curFile := stack[len(stack)-1]
	var walk func(n *BTNodeDoc, field string) error
	walk = func(n *BTNodeDoc, field string) error {
		if n.Type == BTNodeSubtree {
			target := resolveBTPath(curFile, n.Path)
			if slices.Contains(stack, target) {
				chain := strings.Join(append(slices.Clone(stack), target), " -> ")
				return loaderr.Locate(&loaderr.Error{
					Field:   loaderr.Join(field, "path"),
					Problem: fmt.Sprintf("ai: subtree cycle detected: %s", chain),
					Fix:     "remove the circular subtree reference",
				}, curFile, doc.sourceData, fixBTDoc)
			}
			subData, err := fs.ReadFile(fsys, target)
			if err != nil {
				return loaderr.Locate(&loaderr.Error{
					Field:   loaderr.Join(field, "path"),
					Problem: fmt.Sprintf("ai: load subtree %q: %v", target, err),
					Fix:     "ensure the referenced subtree file exists",
					Err:     err,
				}, curFile, doc.sourceData, fixBTDoc)
			}
			subDoc, err := ParseBehaviorTreeWith(subData, target)
			if err != nil {
				return err
			}
			if err := validateSubtreesFS(fsys, subDoc, append(stack, target)); err != nil {
				return err
			}
		}
		if n.Child != nil {
			if err := walk(n.Child, loaderr.Join(field, "child")); err != nil {
				return err
			}
		}
		for i := range n.Children {
			if err := walk(&n.Children[i], loaderr.Join(field, "children", i)); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(&doc.Root, "root")
}

// BTParams holds resolved typed parameters passed to a leaf or service factory.
type BTParams map[string]any

// Bool returns the bool parameter name, or (false, false) if absent or not a bool.
func (p BTParams) Bool(name string) (bool, bool) {
	v, ok := p[name]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// Int returns the integer parameter name, or (0, false) if absent or not an integer.
func (p BTParams) Int(name string) (int, bool) {
	v, ok := p[name]
	if !ok {
		return 0, false
	}
	return toExactInt(v)
}

// Float returns the numeric parameter name as float64, accepting int or float values.
func (p BTParams) Float(name string) (float64, bool) {
	v, ok := p[name]
	if !ok {
		return 0, false
	}
	return toFiniteFloat64(v)
}

// String returns the string parameter name, or ("", false) if absent or not a string.
func (p BTParams) String(name string) (string, bool) {
	v, ok := p[name]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// Duration parses a Go duration string parameter name, returning (0, false) if absent or invalid.
func (p BTParams) Duration(name string) (time.Duration, bool) {
	s, ok := p.String(name)
	if !ok {
		return 0, false
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return d, true
}

// Vec2 returns a 2-element numeric array parameter as a [math2d.Vec2].
func (p BTParams) Vec2(name string) (math2d.Vec2, bool) {
	v, ok := p[name]
	if !ok {
		return math2d.Vec2{}, false
	}
	arr, ok := v.([]any)
	if !ok {
		return math2d.Vec2{}, false
	}
	vec, err := decodeVec2(arr)
	return vec, err == nil
}

// Point returns a 2-element numeric array parameter as a [core.Point].
func (p BTParams) Point(name string) (core.Point, bool) {
	vec, ok := p.Vec2(name)
	if !ok {
		return core.Point{}, false
	}
	return vec.Point(), true
}

// Vec3 returns a 3-element numeric array parameter as an [ai.Vec3].
func (p BTParams) Vec3(name string) (Vec3, bool) {
	v, ok := p[name]
	if !ok {
		return Vec3{}, false
	}
	arr, ok := v.([]any)
	if !ok {
		return Vec3{}, false
	}
	vec, err := decodeVec3(arr)
	return vec, err == nil
}

// Entity returns a non-negative integer parameter as a uint64 entity ID.
func (p BTParams) Entity(name string) (uint64, bool) {
	v, ok := p[name]
	if !ok {
		return 0, false
	}
	return toEntityID(v)
}

// BTNodeContext supplies runtime dependencies and resolved parameter and key
// bindings to game-registered action, condition, and service factories.
type BTNodeContext struct {
	// Blackboard is the agent's blackboard bound to this behavior tree instance.
	Blackboard *Blackboard
	// Stream is the optional deterministic random stream passed in BTBindings.
	Stream *random.Stream
	// Params holds the node's resolved parameters after any subtree "${param}" substitutions.
	Params BTParams
	// KeyBindings maps subtree-local blackboard key names to caller blackboard key names.
	KeyBindings map[string]string
}

// Key resolves a blackboard key name through any enclosing subtree key bindings,
// returning name unchanged when no remapping applies.
func (c BTNodeContext) Key(name string) string {
	if mapped, ok := c.KeyBindings[name]; ok {
		return mapped
	}
	return name
}

// BTBindings provides the blackboard, deterministic random stream, leaf factories,
// and optional subtree loader used by [BuildBehaviorTree].
type BTBindings struct {
	// Blackboard is bound to blackboard_condition nodes and passed to leaf factories.
	// When nil, BuildBehaviorTree creates a fresh local Blackboard.
	Blackboard *Blackboard
	// Stream is passed to random_selector, random_sequence, and weighted_selector nodes.
	Stream *random.Stream
	// Actions maps action names to factories returning a leaf Node.
	Actions map[string]func(BTNodeContext) (Node, error)
	// Conditions maps condition names to factories returning a predicate func() bool
	// (used by condition leaves and guard decorators).
	Conditions map[string]func(BTNodeContext) (func() bool, error)
	// Services maps service names to factories returning a func(dt time.Duration) callback
	// (used by service decorators).
	Services map[string]func(BTNodeContext) (func(time.Duration), error)
	// FS is an optional filesystem used to load referenced subtree files relative to the parent tree.
	FS fs.FS
	// LoadSubtree optionally overrides subtree file loading when custom storage is used.
	LoadSubtree func(path string) ([]byte, error)
}

// BuildBehaviorTreeFS loads the behavior tree at name in fsys and builds its runtime [Node].
func BuildBehaviorTreeFS(fsys fs.FS, name string, bind BTBindings) (Node, error) {
	doc, err := LoadBehaviorTreeFS(fsys, name)
	if err != nil {
		return nil, err
	}
	if bind.FS == nil {
		bind.FS = fsys
	}
	return BuildBehaviorTree(doc, bind)
}

// BuildBehaviorTree validates doc, seeds any unset declared blackboard defaults,
// resolves subtrees and bindings, and returns the executable [Node] tree.
func BuildBehaviorTree(doc *BehaviorTreeDoc, bind BTBindings) (Node, error) {
	if err := ValidateBehaviorTree(doc); err != nil {
		return nil, loaderr.Locate(err, doc.sourceFile, doc.sourceData, fixBTDoc)
	}
	bb := bind.Blackboard
	if bb == nil {
		bb = NewBlackboard()
	}
	var stack []string
	if doc.sourceFile != "" {
		stack = []string{cleanBTPath(doc.sourceFile)}
	}
	b := &btBuilder{
		bb:   bb,
		bind: bind,
	}
	return b.buildDoc(doc, nil, nil, stack)
}

type btBuilder struct {
	bb   *Blackboard
	bind BTBindings
}

func (b *btBuilder) buildDoc(doc *BehaviorTreeDoc, paramOverrides map[string]any, keyBindings map[string]string, stack []string) (Node, error) {
	// Merge subtree defaults with caller overrides.
	activeParams := make(map[string]any, len(doc.Params)+len(paramOverrides))
	for k, v := range doc.Params {
		activeParams[k] = v
	}
	for k, v := range paramOverrides {
		activeParams[k] = v
	}

	declaredKeys := make(map[string]BlackboardKind, len(doc.Blackboard))
	for i := range doc.Blackboard {
		entry := &doc.Blackboard[i]
		kind, _ := entry.Type.Kind()
		declaredKeys[entry.Key] = kind
		if entry.Default != nil {
			val, err := decodeBlackboardValue(*entry.Default, kind)
			if err != nil {
				return nil, loaderr.Locate(&loaderr.Error{
					Field:   loaderr.Path("blackboard", i, "default"),
					Problem: fmt.Sprintf("ai: blackboard key %q default: %v", entry.Key, err),
					Fix:     fmt.Sprintf("provide a valid %s value", entry.Type),
				}, doc.sourceFile, doc.sourceData, fixBTDoc)
			}
			resolvedKey := resolveKey(entry.Key, keyBindings)
			if entry.Shared {
				target := b.bb
				if target.parent != nil {
					target = target.parent
				}
				if !target.HasLocal(resolvedKey) {
					target.Set(resolvedKey, val)
				}
			} else if !b.bb.Has(resolvedKey) {
				b.bb.Set(resolvedKey, val)
			}
		}
	}

	return b.buildNode(doc, &doc.Root, "root", activeParams, keyBindings, declaredKeys, stack)
}

func resolveKey(name string, bindings map[string]string) string {
	if mapped, ok := bindings[name]; ok {
		return mapped
	}
	return name
}

func (b *btBuilder) locate(doc *BehaviorTreeDoc, err error) error {
	return loaderr.Locate(err, doc.sourceFile, doc.sourceData, fixBTDoc)
}

func (b *btBuilder) resolveValue(doc *BehaviorTreeDoc, raw any, field string, activeParams map[string]any) (any, error) {
	if paramName, ok := parseParamPlaceholder(raw); ok {
		val, found := activeParams[paramName]
		if !found {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   field,
				Problem: fmt.Sprintf("ai: unbound parameter %q", paramName),
				Fix:     fmt.Sprintf("declare %q in params or pass it from the parent subtree node", paramName),
			})
		}
		return val, nil
	}
	return raw, nil
}

func (b *btBuilder) resolveDuration(doc *BehaviorTreeDoc, raw, field string, mustPositive bool, activeParams map[string]any) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	resolved, err := b.resolveValue(doc, raw, field, activeParams)
	if err != nil {
		return 0, err
	}
	s, ok := resolved.(string)
	if !ok {
		return 0, b.locate(doc, &loaderr.Error{
			Field:    field,
			Problem:  fmt.Sprintf("ai: duration parameter resolved to %T, want duration string", resolved),
			Expected: `a Go duration string such as "250ms"`,
			Fix:      "pass a valid Go duration string for this parameter",
		})
	}
	if err := validateLiteralDuration(s, field, mustPositive); err != nil {
		return 0, b.locate(doc, err)
	}
	d, _ := time.ParseDuration(s)
	return d, nil
}

func (b *btBuilder) resolveNodeParams(doc *BehaviorTreeDoc, nodeParams map[string]any, field string, activeParams map[string]any) (BTParams, error) {
	if len(nodeParams) == 0 && len(activeParams) == 0 {
		return nil, nil
	}
	out := make(BTParams, len(nodeParams))
	keys := make([]string, 0, len(nodeParams))
	for k := range nodeParams {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		val, err := b.resolveValue(doc, nodeParams[k], loaderr.Join(field, k), activeParams)
		if err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, nil
}

func (b *btBuilder) buildNode(
	doc *BehaviorTreeDoc,
	n *BTNodeDoc,
	field string,
	activeParams map[string]any,
	keyBindings map[string]string,
	declaredKeys map[string]BlackboardKind,
	stack []string,
) (Node, error) {
	buildChildren := func() ([]Node, error) {
		children := make([]Node, len(n.Children))
		for i := range n.Children {
			ch, err := b.buildNode(doc, &n.Children[i], loaderr.Join(field, "children", i), activeParams, keyBindings, declaredKeys, stack)
			if err != nil {
				return nil, err
			}
			children[i] = ch
		}
		return children, nil
	}

	makeCtx := func(params BTParams) BTNodeContext {
		var kb map[string]string
		if len(keyBindings) > 0 {
			kb = make(map[string]string, len(keyBindings))
			for k, v := range keyBindings {
				kb[k] = v
			}
		}
		return BTNodeContext{
			Blackboard:  b.bb,
			Stream:      b.bind.Stream,
			Params:      params,
			KeyBindings: kb,
		}
	}

	wrap := func(built Node, err error) (Node, error) {
		if err != nil {
			return nil, err
		}
		if n.Name != "" {
			return Named(n.Name, built), nil
		}
		return built, nil
	}

	switch n.Type {
	case BTNodeSequence:
		children, err := buildChildren()
		if err != nil {
			return nil, err
		}
		return wrap(Sequence(children...), nil)

	case BTNodeSelector:
		children, err := buildChildren()
		if err != nil {
			return nil, err
		}
		return wrap(Selector(children...), nil)

	case BTNodeParallel:
		children, err := buildChildren()
		if err != nil {
			return nil, err
		}
		policy := RequireAll
		if n.Policy == BTParallelRequireOne {
			policy = RequireOne
		}
		return wrap(Parallel(policy, children...), nil)

	case BTNodeRandomSelector:
		children, err := buildChildren()
		if err != nil {
			return nil, err
		}
		return wrap(RandomSelector(b.bind.Stream, children...), nil)

	case BTNodeRandomSequence:
		children, err := buildChildren()
		if err != nil {
			return nil, err
		}
		return wrap(RandomSequence(b.bind.Stream, children...), nil)

	case BTNodeWeightedSelector:
		opts := make([]WeightedNode, len(n.Children))
		for i := range n.Children {
			ch, err := b.buildNode(doc, &n.Children[i], loaderr.Join(field, "children", i), activeParams, keyBindings, declaredKeys, stack)
			if err != nil {
				return nil, err
			}
			opts[i] = WeightedNode{Node: ch, Weight: n.Children[i].Weight}
		}
		return wrap(WeightedSelector(b.bind.Stream, opts...), nil)

	case BTNodeInverter:
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(Inverter(ch), nil)

	case BTNodeSucceeder:
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(Succeeder(ch), nil)

	case BTNodeFailer:
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(Failer(ch), nil)

	case BTNodeUntilFail:
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(UntilFail(ch), nil)

	case BTNodeRepeater:
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(Repeater(n.Count, ch), nil)

	case BTNodeRetry:
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(Retry(n.Count, ch), nil)

	case BTNodeCooldown:
		d, err := b.resolveDuration(doc, n.Duration, loaderr.Join(field, "duration"), false, activeParams)
		if err != nil {
			return nil, err
		}
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(Cooldown(d, ch), nil)

	case BTNodeTimeLimit:
		d, err := b.resolveDuration(doc, n.Duration, loaderr.Join(field, "duration"), true, activeParams)
		if err != nil {
			return nil, err
		}
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(TimeLimit(d, ch), nil)

	case BTNodeGuard:
		factory := b.bind.Conditions[n.Condition]
		if factory == nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "condition"),
				Problem: fmt.Sprintf("ai: unbound condition %q", n.Condition),
				Fix:     fmt.Sprintf("register %q in BTBindings.Conditions", n.Condition),
			})
		}
		params, err := b.resolveNodeParams(doc, n.Params, loaderr.Join(field, "params"), activeParams)
		if err != nil {
			return nil, err
		}
		fn, err := factory(makeCtx(params))
		if err != nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "condition"),
				Problem: fmt.Sprintf("ai: build condition %q: %v", n.Condition, err),
				Err:     err,
			})
		}
		if fn == nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "condition"),
				Problem: fmt.Sprintf("ai: condition factory %q returned nil", n.Condition),
				Fix:     "return a non-nil func() bool from the condition factory",
			})
		}
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(Guard(fn, ch), nil)

	case BTNodeService:
		factory := b.bind.Services[n.Service]
		if factory == nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "service"),
				Problem: fmt.Sprintf("ai: unbound service %q", n.Service),
				Fix:     fmt.Sprintf("register %q in BTBindings.Services", n.Service),
			})
		}
		interval, err := b.resolveDuration(doc, n.Interval, loaderr.Join(field, "interval"), false, activeParams)
		if err != nil {
			return nil, err
		}
		params, err := b.resolveNodeParams(doc, n.Params, loaderr.Join(field, "params"), activeParams)
		if err != nil {
			return nil, err
		}
		fn, err := factory(makeCtx(params))
		if err != nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "service"),
				Problem: fmt.Sprintf("ai: build service %q: %v", n.Service, err),
				Err:     err,
			})
		}
		if fn == nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "service"),
				Problem: fmt.Sprintf("ai: service factory %q returned nil", n.Service),
				Fix:     "return a non-nil func(time.Duration) from the service factory",
			})
		}
		ch, err := b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
		if err != nil {
			return nil, err
		}
		return wrap(Service(interval, fn, ch), nil)

	case BTNodeBlackboardCondition:
		var bbVal BlackboardValue
		if n.Op.NeedsValue() {
			resolved, err := b.resolveValue(doc, *n.Value, loaderr.Join(field, "value"), activeParams)
			if err != nil {
				return nil, err
			}
			var targetKind BlackboardKind
			if n.ValueType != "" {
				targetKind, _ = n.ValueType.Kind()
			} else {
				targetKind = declaredKeys[n.Key]
			}
			bbVal, err = decodeBlackboardValue(resolved, targetKind)
			if err != nil {
				return nil, b.locate(doc, &loaderr.Error{
					Field:   loaderr.Join(field, "value"),
					Problem: fmt.Sprintf("ai: invalid blackboard_condition value: %v", err),
					Fix:     "provide a value matching the blackboard key type",
				})
			}
			if err := checkOpKindCompatible(n.Op, bbVal.Kind, loaderr.Join(field, "op")); err != nil {
				return nil, b.locate(doc, err)
			}
		}
		var ch Node
		if n.Child != nil {
			var err error
			ch, err = b.buildNode(doc, n.Child, loaderr.Join(field, "child"), activeParams, keyBindings, declaredKeys, stack)
			if err != nil {
				return nil, err
			}
		}
		resolvedKey := resolveKey(n.Key, keyBindings)
		return wrap(BlackboardCondition(b.bb, resolvedKey, n.Op, bbVal, n.Abort, ch), nil)

	case BTNodeWait:
		d, err := b.resolveDuration(doc, n.Duration, loaderr.Join(field, "duration"), false, activeParams)
		if err != nil {
			return nil, err
		}
		return wrap(Wait(d), nil)

	case BTNodeAction:
		factory := b.bind.Actions[n.Action]
		if factory == nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "action"),
				Problem: fmt.Sprintf("ai: unbound action %q", n.Action),
				Fix:     fmt.Sprintf("register %q in BTBindings.Actions", n.Action),
			})
		}
		params, err := b.resolveNodeParams(doc, n.Params, loaderr.Join(field, "params"), activeParams)
		if err != nil {
			return nil, err
		}
		node, err := factory(makeCtx(params))
		if err != nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "action"),
				Problem: fmt.Sprintf("ai: build action %q: %v", n.Action, err),
				Err:     err,
			})
		}
		if node == nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "action"),
				Problem: fmt.Sprintf("ai: action factory %q returned nil", n.Action),
				Fix:     "return a non-nil ai.Node from the action factory",
			})
		}
		return wrap(node, nil)

	case BTNodeCondition:
		factory := b.bind.Conditions[n.Condition]
		if factory == nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "condition"),
				Problem: fmt.Sprintf("ai: unbound condition %q", n.Condition),
				Fix:     fmt.Sprintf("register %q in BTBindings.Conditions", n.Condition),
			})
		}
		params, err := b.resolveNodeParams(doc, n.Params, loaderr.Join(field, "params"), activeParams)
		if err != nil {
			return nil, err
		}
		fn, err := factory(makeCtx(params))
		if err != nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "condition"),
				Problem: fmt.Sprintf("ai: build condition %q: %v", n.Condition, err),
				Err:     err,
			})
		}
		if fn == nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "condition"),
				Problem: fmt.Sprintf("ai: condition factory %q returned nil", n.Condition),
				Fix:     "return a non-nil func() bool from the condition factory",
			})
		}
		return wrap(Condition(fn), nil)

	case BTNodeSubtree:
		var curFile string
		if len(stack) > 0 {
			curFile = stack[len(stack)-1]
		}
		target := resolveBTPath(curFile, n.Path)
		if slices.Contains(stack, target) {
			chain := strings.Join(append(slices.Clone(stack), target), " -> ")
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "path"),
				Problem: fmt.Sprintf("ai: subtree cycle detected: %s", chain),
				Fix:     "remove the circular subtree reference",
			})
		}
		var subData []byte
		var err error
		switch {
		case b.bind.LoadSubtree != nil:
			subData, err = b.bind.LoadSubtree(target)
		case b.bind.FS != nil:
			subData, err = fs.ReadFile(b.bind.FS, target)
		default:
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "path"),
				Problem: fmt.Sprintf("ai: subtree %q requires BTBindings.FS or BTBindings.LoadSubtree", target),
				Fix:     "provide FS or LoadSubtree in BTBindings",
			})
		}
		if err != nil {
			return nil, b.locate(doc, &loaderr.Error{
				Field:   loaderr.Join(field, "path"),
				Problem: fmt.Sprintf("ai: load subtree %q: %v", target, err),
				Fix:     "ensure the referenced subtree file exists",
				Err:     err,
			})
		}
		subDoc, err := ParseBehaviorTreeWith(subData, target)
		if err != nil {
			return nil, err
		}
		subParams, err := b.resolveNodeParams(doc, n.Params, loaderr.Join(field, "params"), activeParams)
		if err != nil {
			return nil, err
		}
		// Compose transitive key bindings: subtree local key -> caller key -> outer caller key.
		var subKeys map[string]string
		if len(n.Keys) > 0 || len(keyBindings) > 0 {
			subKeys = make(map[string]string, len(n.Keys)+len(keyBindings))
			for k, v := range keyBindings {
				subKeys[k] = v
			}
			for localKey, callerKey := range n.Keys {
				subKeys[localKey] = resolveKey(callerKey, keyBindings)
			}
		}
		return wrap(b.buildDoc(subDoc, subParams, subKeys, append(stack, target)))

	default:
		return nil, b.locate(doc, &loaderr.Error{
			Field:   loaderr.Join(field, "type"),
			Problem: fmt.Sprintf("ai: unknown behavior tree node type %q", n.Type),
			Fix:     "set type to a supported behavior tree node kind",
		})
	}
}
