package loaderr

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Fixes Locate gives the decode errors it recognises.
const (
	FixSyntax       = "correct the syntax at this position; the rest of the file is not read until it parses"
	FixUnknownField = "remove the field or correct its spelling; this format refuses fields it does not define"
	FixType         = "write a value of the expected type"
	FixDuplicateKey = "remove one of the two keys; a key may appear once in a mapping"
	FixTruncated    = "complete the document; it ends in the middle of a value, as a file cut short does"
)

// Locate returns err as an *Error that names file and, when data holds the text the loader decoded, the line and column of the problem. Loaders call it once on every error their entry point returns.
//
//   - An *Error in err's chain is copied with File set to file when it has none, Line and Column set from where its Field is in data, and Fix set to fix when it has none. When the field itself is missing from data, the line is that of the nearest enclosing value that is there. The copy keeps that Error's Problem and Err, not the text of errors wrapping it.
//   - A decode error from encoding/json (syntax, unknown field, wrong type) or from YAML (syntax, unknown field, wrong type, duplicate key, an unexpected end) becomes an Error at its line and column, with the field it was in, when known, and a fix for its kind.
//   - Any other error becomes an Error that names file, with fix as its Fix and err as its Err, at line 0.
//
// Locate returns nil for a nil err.
func Locate(err error, file string, data []byte, fix string) error {
	if err == nil {
		return nil
	}
	if e, ok := As(err); ok {
		c := *e
		if c.File == "" {
			c.File = file
		}
		if c.Line == 0 && data != nil {
			c.Line, c.Column = FindField(data, c.Field)
		}
		if c.Fix == "" {
			c.Fix = fix
		}
		return &c
	}
	if d := fromDecode(err, data); d != nil {
		d.File = file
		if d.Fix == "" {
			d.Fix = fix
		}
		return d
	}
	return &Error{File: file, Problem: err.Error(), Fix: fix, Err: err}
}

// FindField returns the 1-based line and column of the value at field (a path as Path writes it) in data, a YAML or JSON document. For a mapping key it is the key's position. When field is not in data it returns the position of the nearest enclosing value that is, the document's start for an empty field, and 0, 0 when data does not parse.
func FindField(data []byte, field string) (line, column int) {
	segs, ok := parsePath(field)
	if !ok {
		return 0, 0
	}
	best := -1
	walk(data, func(path []segment, l, c int) bool {
		n := commonPrefix(path, segs)
		if n == len(path) && n > best {
			best, line, column = n, l, c
		}
		return best < len(segs)
	})
	// YAML places an empty document's implicit value after a final line break, on a line the text does not have.
	if n := lineCount(data); line > n {
		line, column = n, 1
	}
	return line, column
}

// fieldAt returns the path of the deepest value that starts on line, as Path writes it, or "" when there is none.
func fieldAt(data []byte, line int) string {
	var found []segment
	walk(data, func(path []segment, l, _ int) bool {
		if l == line {
			found = append(found[:0], path...)
		}
		return l <= line
	})
	return formatPath(found)
}

// findKey returns the position of the first mapping key named key in data, in document order.
func findKey(data []byte, key string) (line, column int, field string) {
	walk(data, func(path []segment, l, c int) bool {
		if len(path) > 0 && !path[len(path)-1].isIndex && path[len(path)-1].key == key {
			line, column, field = l, c, formatPath(path)
			return false
		}
		return true
	})
	return line, column, field
}

func formatPath(segs []segment) string {
	parts := make([]any, len(segs))
	for i, s := range segs {
		if s.isIndex {
			parts[i] = s.index
		} else {
			parts[i] = s.key
		}
	}
	return Path(parts...)
}

func commonPrefix(a, b []segment) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// walk calls visit with the path and position of every value in data, a JSON or YAML document, in document order, until visit returns false. A mapping value's position is its key's. JSON is walked as JSON so offsets are exact; anything else, or JSON that does not parse, is walked as YAML.
func walk(data []byte, visit func(path []segment, line, column int) bool) {
	trimmed := bytes.TrimLeft(data, " \t\r\n\ufeff")
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if walkJSON(data, visit) {
			return
		}
	}
	walkYAML(data, visit)
}

// position turns byte offsets into 1-based lines and columns, scanning forward from the last offset it was asked about.
type position struct {
	data         []byte
	off          int
	line, column int
}

func (p *position) at(off int) (int, int) {
	if off < p.off {
		p.off, p.line, p.column = 0, 1, 1
	}
	off = min(off, len(p.data))
	for ; p.off < off; p.off++ {
		if p.data[p.off] == '\n' {
			p.line++
			p.column = 1
		} else {
			p.column++
		}
	}
	return p.line, p.column
}

// walkJSON walks data as JSON and reports whether the whole document parsed.
func walkJSON(data []byte, visit func([]segment, int, int) bool) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	pos := &position{data: data, line: 1, column: 1}
	// start is where the next token begins: Token leaves the offset after the previous one, before any separators.
	start := func() int {
		off := int(dec.InputOffset())
		for off < len(data) && strings.IndexByte(" \t\r\n,:", data[off]) >= 0 {
			off++
		}
		return off
	}
	stopped := false
	var path []segment
	var value func(keyed bool) bool
	value = func(keyed bool) bool {
		off := start()
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if !keyed {
			l, c := pos.at(off)
			if !stopped && !visit(path, l, c) {
				stopped = true
			}
		}
		d, ok := tok.(json.Delim)
		if !ok {
			return true
		}
		switch d {
		case '{':
			for dec.More() {
				off := start()
				k, err := dec.Token()
				if err != nil {
					return false
				}
				key, _ := k.(string)
				path = append(path, segment{key: key})
				l, c := pos.at(off)
				if !stopped && !visit(path, l, c) {
					stopped = true
				}
				if !value(true) {
					return false
				}
				path = path[:len(path)-1]
			}
		case '[':
			for i := 0; dec.More(); i++ {
				path = append(path, segment{index: i, isIndex: true})
				if !value(false) {
					return false
				}
				path = path[:len(path)-1]
			}
		}
		_, err = dec.Token()
		return err == nil
	}
	return value(false)
}

func walkYAML(data []byte, visit func([]segment, int, int) bool) {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil {
		return
	}
	var path []segment
	seen := map[*yaml.Node]bool{}
	var node func(n *yaml.Node, line, column int) bool
	node = func(n *yaml.Node, line, column int) bool {
		if !visit(path, line, column) {
			return false
		}
		for n.Kind == yaml.AliasNode && n.Alias != nil {
			n = n.Alias
		}
		// An alias may point at a node that holds it; walk each node's children once.
		if seen[n] {
			return true
		}
		seen[n] = true
		defer delete(seen, n)
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				path = append(path, segment{key: k.Value})
				ok := node(v, k.Line, k.Column)
				path = path[:len(path)-1]
				if !ok {
					return false
				}
			}
		case yaml.SequenceNode:
			for i, v := range n.Content {
				path = append(path, segment{index: i, isIndex: true})
				ok := node(v, v.Line, v.Column)
				path = path[:len(path)-1]
				if !ok {
					return false
				}
			}
		}
		return true
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root := doc.Content[0]
		node(root, root.Line, root.Column)
	}
}

var (
	jsonUnknownField = regexp.MustCompile(`json: unknown field "((?:[^"\\]|\\.)*)"`)
	yamlLine         = regexp.MustCompile(`^(?:yaml: )?line (\d+): (.*)$`)
	yamlNotFound     = regexp.MustCompile(`^field (\S+) not found in type`)
	yamlDuplicate    = regexp.MustCompile(`^mapping key "?(.*?)"? already defined`)
	yamlCannot       = regexp.MustCompile("^cannot unmarshal (\\S+)(?: `(.*)`)? into (.+)$")
)

// fromDecode returns the Error for a decode error from encoding/json or YAML in err's chain, or nil when there is none.
func fromDecode(err error, data []byte) *Error {
	var csvErr *csv.ParseError
	if errors.As(err, &csvErr) {
		return &Error{Line: csvErr.Line, Column: csvErr.Column, Problem: "invalid CSV: " + csvErr.Err.Error(), Fix: FixSyntax, Err: err}
	}
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		e := &Error{Problem: "invalid JSON: " + syn.Error(), Fix: FixSyntax, Err: err}
		if data != nil && int(syn.Offset) >= len(bytes.TrimRight(data, " \t\r\n")) {
			e.Fix = FixTruncated
		}
		e.Line, e.Column = offsetPosition(data, int(syn.Offset)-1)
		return e
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		problem := fmt.Sprintf("cannot use a JSON %s here", typ.Value)
		if typ.Err != nil {
			// A custom UnmarshalJSON said why.
			problem = typ.Err.Error()
		}
		e := &Error{
			Field:    jsonFieldPath(typ.Field),
			Problem:  problem,
			Expected: describeType(typ.Type),
			Fix:      FixType,
			Err:      err,
		}
		if e.Field != "" {
			e.Line, e.Column = FindField(data, e.Field)
		}
		if e.Line == 0 {
			e.Line, e.Column = offsetPosition(data, int(typ.Offset)-1)
		}
		return e
	}
	if m := jsonUnknownField.FindStringSubmatch(err.Error()); m != nil {
		name, uerr := strconv.Unquote(`"` + m[1] + `"`)
		if uerr != nil {
			name = m[1]
		}
		e := &Error{Problem: fmt.Sprintf("unknown field %q", name), Fix: FixUnknownField, Err: err}
		e.Line, e.Column, e.Field = findKey(data, name)
		return e
	}
	var yt *yaml.TypeError
	if errors.As(err, &yt) && len(yt.Errors) > 0 {
		return fromYAMLMessage(yt.Errors[0], data, err)
	}
	if e := fromYAMLMessage(err.Error(), data, err); e != nil {
		return e
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		e := &Error{Problem: "unexpected end of the document", Fix: FixTruncated, Err: err}
		e.Line, e.Column = offsetPosition(data, len(data))
		return e
	}
	return nil
}

// yamlParserProblems are the problems yaml.v3's parser (rather than its scanner) reports. For these it writes the 0-based line of the construct the problem is in (go.yaml.in/yaml/v3 decode.go, parser.fail, adds 1 only for scanner errors), so their line is one more than the message says.
var yamlParserProblems = []string{
	"did not find expected <document start>",
	"did not find expected node content",
	"did not find expected key",
	"did not find expected '-' indicator",
	"did not find expected ',' or ']'",
	"did not find expected ',' or '}'",
}

func isYAMLParserProblem(problem string) bool {
	return slices.Contains(yamlParserProblems, problem)
}

// fromYAMLMessage reads one YAML decode message, "line N: problem", as yaml.v3 writes them in a TypeError and in syntax errors.
func fromYAMLMessage(msg string, data []byte, err error) *Error {
	// A wrapped YAML error carries the loader's prefix before "yaml: line".
	if i := strings.Index(msg, "yaml: line "); i > 0 {
		msg = msg[i:]
	}
	var line int
	var problem string
	if m := yamlLine.FindStringSubmatch(msg); m != nil {
		line, _ = strconv.Atoi(m[1])
		problem = m[2]
		if isYAMLParserProblem(problem) {
			line++
		}
	} else if p, ok := strings.CutPrefix(msg, "yaml: "); ok && isYAMLParserProblem(p) {
		// A parser error whose context starts on the first line carries no line number.
		line, problem = 1, p
	} else {
		return nil
	}
	// A problem at the end of the stream can be numbered past the last line.
	if n := lineCount(data); data != nil && line > n {
		line = n
	}
	e := &Error{Line: line, Err: err}
	switch {
	case yamlNotFound.MatchString(problem):
		name := yamlNotFound.FindStringSubmatch(problem)[1]
		e.Problem = fmt.Sprintf("unknown field %q", name)
		e.Fix = FixUnknownField
		e.Field = fieldAt(data, line)
	case yamlDuplicate.MatchString(problem):
		e.Problem = problem
		e.Fix = FixDuplicateKey
		e.Field = fieldAt(data, line)
	case yamlCannot.MatchString(problem):
		c := yamlCannot.FindStringSubmatch(problem)
		if c[2] != "" {
			e.Problem = fmt.Sprintf("cannot use %s `%s` here", c[1], c[2])
		} else {
			e.Problem = fmt.Sprintf("cannot use a %s here", c[1])
		}
		e.Expected = c[3]
		e.Fix = FixType
		e.Field = fieldAt(data, line)
	default:
		e.Problem = "invalid YAML: " + problem
		e.Fix = FixSyntax
	}
	if e.Field != "" {
		if _, c := FindField(data, e.Field); c > 0 {
			e.Column = c
		}
	}
	return e
}

// jsonFieldPath turns the dotted field encoding/json reports, such as "layers.0.order", into a path as Path writes it, "layers[0].order".
func jsonFieldPath(field string) string {
	if field == "" {
		return ""
	}
	parts := strings.Split(field, ".")
	segs := make([]any, len(parts))
	for i, p := range parts {
		if n, err := strconv.Atoi(p); err == nil && n >= 0 {
			segs[i] = n
		} else {
			segs[i] = p
		}
	}
	return Path(segs...)
}

// lineCount returns the number of lines in data as YAML counts them, with "\n", "\r\n" and a lone "\r" each ending a line, not counting the empty line after a final break.
func lineCount(data []byte) int {
	n := 0
	for i, b := range data {
		if b == '\n' || b == '\r' && (i+1 == len(data) || data[i+1] != '\n') {
			n++
		}
	}
	if last := len(data) - 1; last >= 0 && data[last] != '\n' && data[last] != '\r' {
		n++
	}
	return max(n, 1)
}

func offsetPosition(data []byte, off int) (int, int) {
	off = max(0, min(off, len(data)))
	p := &position{data: data, line: 1, column: 1}
	return p.at(off)
}

// describeType names a Go type as the JSON value it decodes from.
func describeType(t reflect.Type) string {
	if t == nil {
		return ""
	}
	switch t.Kind() {
	case reflect.Bool:
		return "a boolean (true or false)"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return fmt.Sprintf("a whole number that fits %s", t.Kind())
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Slice, reflect.Array:
		return "an array"
	case reflect.Map, reflect.Struct:
		return "an object"
	case reflect.Pointer:
		return describeType(t.Elem())
	}
	return t.String()
}
