package statedump

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// maxDepth bounds the nesting of component and value data in both directions, so a hostile file cannot exhaust the stack.
const maxDepth = 512

const (
	tagStr   = "!!str"
	tagInt   = "!!int"
	tagFloat = "!!float"
	tagBool  = "!!bool"
	tagNull  = "!!null"
	tagTime  = "!!timestamp"
)

// jsonToNode turns one JSON value into a YAML node, keeping object key order and the exact text of every number.
func jsonToNode(data []byte) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	n, err := jsonValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON after value")
	}
	return n, nil
}

func jsonValue(dec *json.Decoder, depth int) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := tok.(type) {
	case json.Delim:
		if depth >= maxDepth {
			return nil, fmt.Errorf("nesting deeper than %d", maxDepth)
		}
		switch v {
		case '{':
			n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			seen := make(map[string]struct{})
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("object key %v is not a string", kt)
				}
				if _, dup := seen[key]; dup {
					return nil, fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = struct{}{}
				val, err := jsonValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, strNode(key), val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		case '[':
			n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				val, err := jsonValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", v)
	case string:
		return strNode(v), nil
	case json.Number:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: numberTag(string(v)), Value: string(v)}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tagBool, Value: strconv.FormatBool(v)}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tagNull, Value: "null"}, nil
	}
	return nil, fmt.Errorf("unexpected JSON token %v", tok)
}

// strNode returns a string scalar, quoted when a YAML 1.1 reader would take it for a boolean or a base-60 number; YAML 1.2 reads such text as strings, but agents and tools often parse with YAML 1.1 libraries. Strings YAML 1.2 itself reads as another type are quoted by the encoder.
func strNode(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: s}
	if yaml11Bool[strings.ToLower(s)] || yaml11Base60.MatchString(s) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// yaml11Bool holds the plain words common YAML 1.1 libraries (PyYAML, js-yaml's 1.1 schema) resolve to booleans and YAML 1.2 does not. The single letters y and n, which the 1.1 spec also lists but those libraries do not, stay plain so coordinates read as x and y.
var yaml11Bool = map[string]bool{"yes": true, "no": true, "on": true, "off": true}

// yaml11Base60 matches YAML 1.1's sexagesimal ints and floats, such as 1:20 (80) or 1:30.5, the same pattern yaml.v3 quotes for its own strings; it covers the int form because its fraction is optional.
var yaml11Base60 = regexp.MustCompile(`^[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+(?:\.[0-9_]*)?$`)

// numberTag returns the tag YAML resolves a JSON number's plain text to, so the encoder writes it without an explicit tag.
func numberTag(s string) string {
	if strings.ContainsAny(s, ".eE") {
		return tagFloat
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return tagInt
	}
	if _, err := strconv.ParseUint(s, 10, 64); err == nil {
		return tagInt
	}
	return tagFloat
}

// nodeToJSON turns a YAML node back into compact JSON, refusing what JSON cannot hold.
func nodeToJSON(n *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, n, 0); err != nil {
		return nil, fmt.Errorf("statedump: line %d: %w", n.Line, err)
	}
	return buf.Bytes(), nil
}

func writeJSON(buf *bytes.Buffer, n *yaml.Node, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("nesting deeper than %d", maxDepth)
	}
	// yaml.v3 hands an unmarshaler the anchored node in place of an alias, so refusing anchors refuses aliases too.
	if n.Anchor != "" {
		return fmt.Errorf("line %d: anchors and aliases are not allowed in data", n.Line)
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) != 1 {
			return errors.New("empty document")
		}
		return writeJSON(buf, n.Content[0], depth)
	case yaml.MappingNode:
		buf.WriteByte('{')
		seen := make(map[string]struct{}, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != tagStr {
				return fmt.Errorf("line %d: mapping key must be a string (quote it)", k.Line)
			}
			if _, dup := seen[k.Value]; dup {
				return fmt.Errorf("line %d: duplicate mapping key %q", k.Line, k.Value)
			}
			seen[k.Value] = struct{}{}
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeString(buf, k.Value); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeJSON(buf, n.Content[i+1], depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case yaml.SequenceNode:
		buf.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSON(buf, c, depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case yaml.ScalarNode:
		return writeScalar(buf, n)
	case yaml.AliasNode:
		return fmt.Errorf("line %d: aliases are not allowed in data", n.Line)
	}
	return fmt.Errorf("line %d: unexpected YAML node kind %d", n.Line, n.Kind)
}

func writeScalar(buf *bytes.Buffer, n *yaml.Node) error {
	switch n.Tag {
	case tagStr, tagTime:
		return writeString(buf, n.Value)
	case tagNull:
		buf.WriteString("null")
		return nil
	case tagBool:
		var b bool
		if err := n.Decode(&b); err != nil {
			return fmt.Errorf("line %d: %w", n.Line, err)
		}
		buf.WriteString(strconv.FormatBool(b))
		return nil
	case tagInt, tagFloat:
		if isJSONNumber(n.Value) {
			buf.WriteString(n.Value)
			return nil
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return fmt.Errorf("line %d: %w", n.Line, err)
		}
		if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
			return fmt.Errorf("line %d: %s is not a JSON number", n.Line, n.Value)
		}
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("line %d: %w", n.Line, err)
		}
		buf.Write(data)
		return nil
	}
	return fmt.Errorf("line %d: tag %s is not allowed in data", n.Line, n.Tag)
}

func writeString(buf *bytes.Buffer, s string) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	buf.Write(data)
	return nil
}

// isJSONNumber reports whether s is a number literal in JSON's grammar.
func isJSONNumber(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == len(s)
}
