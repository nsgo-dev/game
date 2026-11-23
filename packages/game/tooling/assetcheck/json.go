package assetcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// jsonKind is the kind of a JSON value.
type jsonKind uint8

const (
	jsonNull jsonKind = iota
	jsonBool
	jsonNumber
	jsonString
	jsonArray
	jsonObject
)

// jsonValue is a parsed JSON value that remembers the line it is on, so that a problem with a field can name its line. encoding/json keeps only byte offsets of syntax errors.
type jsonValue struct {
	// line is the 1-based line the value ends on, which for an array or object is the line of its opening bracket.
	line int
	kind jsonKind
	// text is a string's value or a number's literal.
	text string
	// fields are an object's members by name, the last one winning when a name repeats, as encoding/json decodes them.
	fields map[string]*jsonValue
	// items are an array's elements.
	items []*jsonValue
}

// field returns the member called name of v, or nil when v is not an object or has none.
func (v *jsonValue) field(name string) *jsonValue {
	if v == nil || v.kind != jsonObject {
		return nil
	}
	return v.fields[name]
}

// str returns the string member called name of v and its line, or "" when there is none or it is not a string.
func (v *jsonValue) str(name string) (string, int) {
	f := v.field(name)
	if f == nil || f.kind != jsonString {
		return "", 0
	}
	return f.text, f.line
}

// number returns v's value when it is a number.
func (v *jsonValue) number() (float64, bool) {
	if v == nil || v.kind != jsonNumber {
		return 0, false
	}
	f, err := strconv.ParseFloat(v.text, 64)
	return f, err == nil
}

// list returns the elements of v when it is an array, else nil.
func (v *jsonValue) list() []*jsonValue {
	if v == nil || v.kind != jsonArray {
		return nil
	}
	return v.items
}

// jsonError is a JSON syntax error at a line.
type jsonError struct {
	line int
	msg  string
}

func (e *jsonError) Error() string { return e.msg }

// maxJSONDepth bounds how deeply arrays and objects nest, so a crafted file cannot exhaust the stack; Tiled, LDtk and atlas files nest a dozen levels at most.
const maxJSONDepth = 1000

// parseJSON parses data, one JSON value with nothing after it, into a jsonValue tree. Its errors are *jsonError values.
func parseJSON(data []byte) (*jsonValue, error) {
	p := &jsonParser{dec: json.NewDecoder(bytes.NewReader(data)), data: data}
	p.dec.UseNumber()
	for i, b := range data {
		if b == '\n' {
			p.newlines = append(p.newlines, i)
		}
	}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	if _, err := p.dec.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, p.fail(err)
		}
		return nil, &jsonError{line: p.line(), msg: "unexpected data after the top-level JSON value"}
	}
	return v, nil
}

// jsonParser builds a jsonValue tree from a decoder's tokens.
type jsonParser struct {
	dec  *json.Decoder
	data []byte
	// newlines are the offsets of the newlines in data.
	newlines []int
}

// lineAt returns the 1-based line of the byte at offset.
func (p *jsonParser) lineAt(offset int64) int {
	return sort.SearchInts(p.newlines, int(offset)) + 1
}

// line returns the line of the last byte of the token just read.
func (p *jsonParser) line() int {
	return p.lineAt(max(p.dec.InputOffset()-1, 0))
}

// fail turns a decoder error into a jsonError at its line.
func (p *jsonParser) fail(err error) error {
	if se, ok := errors.AsType[*json.SyntaxError](err); ok {
		return &jsonError{line: p.lineAt(max(se.Offset-1, 0)), msg: "invalid JSON: " + se.Error()}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		if len(bytes.TrimSpace(p.data)) == 0 {
			return &jsonError{line: 0, msg: "empty file, want JSON"}
		}
		return &jsonError{line: p.lineAt(int64(len(p.data) - 1)), msg: "invalid JSON: unexpected end of file"}
	}
	return &jsonError{line: p.line(), msg: "invalid JSON: " + err.Error()}
}

// value reads one value at the given nesting depth.
func (p *jsonParser) value(depth int) (*jsonValue, error) {
	tok, err := p.dec.Token()
	if err != nil {
		return nil, p.fail(err)
	}
	v := &jsonValue{line: p.line()}
	switch t := tok.(type) {
	case nil:
		v.kind = jsonNull
	case bool:
		v.kind = jsonBool
	case json.Number:
		v.kind, v.text = jsonNumber, t.String()
	case string:
		v.kind, v.text = jsonString, t
	case json.Delim:
		if t != '[' && t != '{' {
			return nil, &jsonError{line: v.line, msg: fmt.Sprintf("invalid JSON: unexpected %q", rune(t))}
		}
		if depth >= maxJSONDepth {
			return nil, &jsonError{line: v.line, msg: fmt.Sprintf("JSON nests deeper than %d levels", maxJSONDepth)}
		}
		if t == '[' {
			v.kind = jsonArray
			for p.dec.More() {
				item, err := p.value(depth + 1)
				if err != nil {
					return nil, err
				}
				v.items = append(v.items, item)
			}
		} else {
			v.kind = jsonObject
			v.fields = make(map[string]*jsonValue)
			for p.dec.More() {
				key, err := p.dec.Token()
				if err != nil {
					return nil, p.fail(err)
				}
				name, ok := key.(string)
				if !ok {
					return nil, &jsonError{line: p.line(), msg: "invalid JSON: object key is not a string"}
				}
				member, err := p.value(depth + 1)
				if err != nil {
					return nil, err
				}
				v.fields[name] = member
			}
		}
		if _, err := p.dec.Token(); err != nil {
			return nil, p.fail(err)
		}
	}
	return v, nil
}
