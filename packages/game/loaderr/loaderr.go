package loaderr

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Error describes one problem in a file a loader read: where it is, what is wrong and how to fix it.
//
// Its message reads "file:line:column: field: problem; expected E; fix: F", leaving out the parts it does not know. Line and Column are 1-based and 0 when the problem is not at one place in the text, such as a file that cannot be read.
type Error struct {
	File     string // the file's path or name, empty until a loader or its caller knows it
	Line     int    // 1-based line in File, 0 when unknown
	Column   int    // 1-based column in Line, 0 when unknown
	Field    string // where in the document, as Path writes it ("layers[2].space"), empty for the document as a whole
	Problem  string // what is wrong, as the loader says it
	Expected string // what the loader expected there, empty when Problem already says it
	Fix      string // what to change so the file loads
	Err      error  // the underlying error, if any
}

// Error returns the message described on Error.
func (e *Error) Error() string {
	var b strings.Builder
	if e.File != "" || e.Line > 0 {
		if e.File != "" {
			b.WriteString(e.File)
		} else {
			b.WriteString("<input>")
		}
		if e.Line > 0 {
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(e.Line))
			if e.Column > 0 {
				b.WriteByte(':')
				b.WriteString(strconv.Itoa(e.Column))
			}
		}
		b.WriteString(": ")
	}
	if e.Field != "" {
		b.WriteString(e.Field)
		b.WriteString(": ")
	}
	switch {
	case e.Problem != "":
		b.WriteString(e.Problem)
	case e.Err != nil:
		b.WriteString(e.Err.Error())
	default:
		b.WriteString("invalid value")
	}
	if e.Expected != "" {
		b.WriteString("; expected ")
		b.WriteString(e.Expected)
	}
	if e.Fix != "" {
		b.WriteString("; fix: ")
		b.WriteString(e.Fix)
	}
	return b.String()
}

// Unwrap returns the underlying error, so errors.Is and errors.As see through an Error.
func (e *Error) Unwrap() error { return e.Err }

// Errorf returns an Error for the value at field whose Problem is format formatted with args, as fmt.Errorf formats it, and whose fix is fix. A %w verb keeps the wrapped error as Err; with two or more, Err is the formatted error, which wraps them all, so errors.Is and errors.As find each one.
func Errorf(field, fix, format string, args ...any) *Error {
	err := fmt.Errorf(format, args...)
	wrapped := errors.Unwrap(err)
	if _, many := err.(interface{ Unwrap() []error }); many {
		wrapped = err
	}
	return &Error{Field: field, Problem: err.Error(), Fix: fix, Err: wrapped}
}

// As returns the first *Error in err's chain.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// WithFile returns a copy of err with File set to file, when err is itself an *Error whose File is empty. Any other error, including one that only wraps an *Error, is returned unchanged, so the context and sentinels around it still match with errors.Is; so is nil.
//
// A caller that read a file and handed its bytes to a loader, whose entry point returns an *Error, uses it to name the file in the loader's errors.
func WithFile(err error, file string) error {
	e, ok := err.(*Error)
	if !ok || e == nil || e.File != "" {
		return err
	}
	c := *e
	c.File = file
	return &c
}

// Path builds a field path from keys and indexes: a string is a key and an int an index, so Path("layers", 2, "objects", 0, "id") is "layers[2].objects[0].id". A key that is empty or holds '.', '[', ']' or '"' is written quoted in brackets, as in `tilemaps["a.b"]`.
func Path(parts ...any) string {
	var b strings.Builder
	for _, p := range parts {
		switch v := p.(type) {
		case int:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(v))
			b.WriteByte(']')
		case string:
			if v == "" || strings.ContainsAny(v, ".[]\"") {
				b.WriteByte('[')
				b.WriteString(strconv.Quote(v))
				b.WriteByte(']')
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(v)
		default:
			panic(fmt.Sprintf("loaderr: Path part %v is a %T, want string or int", p, p))
		}
	}
	return b.String()
}

// Join returns the path base, as Path writes it, followed by parts, so a validator can extend the path of the value it was handed: Join("layers[2]", "objects", 0) is "layers[2].objects[0]".
func Join(base string, parts ...any) string {
	rest := Path(parts...)
	switch {
	case base == "":
		return rest
	case rest == "":
		return base
	case rest[0] == '[':
		return base + rest
	}
	return base + "." + rest
}

// segment is one step of a field path: a key, or an index when isIndex.
type segment struct {
	key     string
	index   int
	isIndex bool
}

// parsePath splits a path that Path wrote into its segments. It reports false for text Path cannot write.
func parsePath(path string) ([]segment, bool) {
	var segs []segment
	for i := 0; i < len(path); {
		switch path[i] {
		case '.':
			if i == 0 || i == len(path)-1 {
				return nil, false
			}
			i++
		case '[':
			end := i + 1
			if end < len(path) && path[end] == '"' {
				q, err := strconv.QuotedPrefix(path[end:])
				if err != nil {
					return nil, false
				}
				key, _ := strconv.Unquote(q)
				end += len(q)
				if end >= len(path) || path[end] != ']' {
					return nil, false
				}
				segs = append(segs, segment{key: key})
				i = end + 1
				continue
			}
			for end < len(path) && path[end] != ']' {
				end++
			}
			if end >= len(path) {
				return nil, false
			}
			n, err := strconv.Atoi(path[i+1 : end])
			if err != nil || n < 0 {
				return nil, false
			}
			segs = append(segs, segment{index: n, isIndex: true})
			i = end + 1
		default:
			end := i
			for end < len(path) && path[end] != '.' && path[end] != '[' {
				end++
			}
			segs = append(segs, segment{key: path[i:end]})
			i = end
		}
	}
	return segs, true
}
