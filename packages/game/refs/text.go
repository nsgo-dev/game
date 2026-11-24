package refs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// syntax is how a reference's value is written in its file, which decides how a new value is written in its place.
type syntax uint8

const (
	syntaxPlain syntax = iota
	syntaxDouble
	syntaxSingle
	syntaxJSON
)

// span is the bytes of a value in its file's data: the whole token, quotes included.
type span struct {
	start, end int
	syntax     syntax
}

// lineStarts returns the byte offset at which each line of data starts. Lines break at LF, CR, CRLF, NEL and the Unicode line and paragraph separators, as YAML counts them; JSON only breaks at LF and CR, which also break YAML's lines.
func lineStarts(data []byte) []int {
	starts := []int{0}
	for i := 0; i < len(data); {
		c := data[i]
		switch {
		case c == '\n':
			i++
		case c == '\r':
			i++
			if i < len(data) && data[i] == '\n' {
				i++
			}
		case c >= 0x80:
			r, size := utf8.DecodeRune(data[i:])
			i += size
			if r != '\u0085' && r != ' ' && r != ' ' {
				continue
			}
		default:
			i++
			continue
		}
		starts = append(starts, i)
	}
	return starts
}

// position returns the 1-based line and column, counted in characters, of offset in data, whose lines start at starts.
func position(data []byte, starts []int, offset int) (line, column int) {
	i, found := slices.BinarySearch(starts, offset)
	if !found {
		i--
	}
	return i + 1, utf8.RuneCount(data[starts[i]:offset]) + 1
}

// offset returns the byte offset of the 1-based line and character column in data, whose lines start at starts, or -1 when data has no such place.
func offset(data []byte, starts []int, line, column int) int {
	if line < 1 || line > len(starts) || column < 1 {
		return -1
	}
	o := starts[line-1]
	for range column - 1 {
		if o >= len(data) {
			return -1
		}
		_, size := utf8.DecodeRune(data[o:])
		o += size
	}
	return o
}

// yamlSpan returns where the scalar n is written in data, whose lines start at starts: a plain, single-quoted or double-quoted scalar on one line. A scalar written any other way, such as a block scalar, one carrying a tag or anchor, or a plain or quoted scalar folded over several lines, is refused, since it cannot be rewritten in place.
func yamlSpan(data []byte, starts []int, n *yaml.Node) (span, error) {
	o := offset(data, starts, n.Line, n.Column)
	if o < 0 {
		return span{}, fmt.Errorf("line %d column %d is outside the file", n.Line, n.Column)
	}
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		if o >= len(data) || data[o] != '"' {
			break
		}
		for i := o + 1; i < len(data); i++ {
			switch data[i] {
			case '\\':
				i++
			case '"':
				return span{o, i + 1, syntaxDouble}, nil
			case '\n', '\r':
				return span{}, errors.New("the quoted value spans several lines")
			}
		}
	case n.Style&yaml.SingleQuotedStyle != 0:
		if o >= len(data) || data[o] != '\'' {
			break
		}
		for i := o + 1; i < len(data); i++ {
			switch data[i] {
			case '\'':
				if i+1 < len(data) && data[i+1] == '\'' {
					i++
					continue
				}
				return span{o, i + 1, syntaxSingle}, nil
			case '\n', '\r':
				return span{}, errors.New("the quoted value spans several lines")
			}
		}
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0:
		if bytes.HasPrefix(data[o:], []byte(n.Value)) && n.Value != "" {
			return span{o, o + len(n.Value), syntaxPlain}, nil
		}
	}
	return span{}, errors.New("the value is not a plain or quoted scalar on one line")
}

// yamlFlowChars are the characters that end or change a plain scalar in YAML's flow context, which a rewritten plain value avoids by being quoted.
const yamlFlowChars = ",[]{}#&*!|>'\"%@`"

// formatValue returns v written in the syntax s replaces: plain when it reads back as the same string in block and flow context alike, else double-quoted; single-quoted when it has no control characters, else double-quoted; double-quoted; or as a JSON string.
func formatValue(s syntax, v string) string {
	switch s {
	case syntaxJSON:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v) // A string always encodes.
		return strings.TrimSuffix(buf.String(), "\n")
	case syntaxPlain:
		if out, err := yaml.Marshal(v); err == nil && string(out) == v+"\n" && !strings.ContainsAny(v, yamlFlowChars) && !strings.Contains(v, ": ") {
			return v
		}
	case syntaxSingle:
		if strings.IndexFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 }) < 0 {
			return "'" + strings.ReplaceAll(v, "'", "''") + "'"
		}
	}
	return strconv.Quote(v)
}

// edit replaces data[start:end] with text.
type edit struct {
	start, end int
	text       string
}

// applyEdits returns data with edits made, which must not overlap.
func applyEdits(data []byte, edits []edit) []byte {
	slices.SortFunc(edits, func(a, b edit) int { return a.start - b.start })
	var out bytes.Buffer
	out.Grow(len(data))
	last := 0
	for _, e := range edits {
		out.Write(data[last:e.start])
		out.WriteString(e.text)
		last = e.end
	}
	out.Write(data[last:])
	return out.Bytes()
}

// jsonStrings calls visit with the path of keys and decimal indexes to every string value in the JSON document data, and the span it is written in.
func jsonStrings(data []byte, visit func(path []string, s span, value string)) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var walk func(path []string) error
	walk = func(path []string) error {
		before := int(dec.InputOffset())
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				for dec.More() {
					key, err := dec.Token()
					if err != nil {
						return err
					}
					k, ok := key.(string)
					if !ok {
						return fmt.Errorf("refs: object key %v is not a string", key)
					}
					if err := walk(append(slices.Clip(path), k)); err != nil {
						return err
					}
				}
			case '[':
				for i := 0; dec.More(); i++ {
					if err := walk(append(slices.Clip(path), strconv.Itoa(i))); err != nil {
						return err
					}
				}
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
		case string:
			start := before
			for start < len(data) && data[start] != '"' {
				start++
			}
			visit(path, span{start, int(dec.InputOffset()), syntaxJSON}, t)
		}
		return nil
	}
	if err := walk(nil); err != nil {
		return err
	}
	if _, err := dec.Token(); err == nil {
		return errors.New("refs: trailing data after the JSON value")
	}
	return nil
}
