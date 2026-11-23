// Package loaderrtest checks, in a loader's tests, that its errors are *loaderr.Error values an agent can act on: they name the file, the line and a fix.
package loaderrtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// Expect fails t unless err holds a *loaderr.Error whose File is file, whose Line is line, whose Fix is set and whose message starts with "file:line:". It returns that Error for further checks.
func Expect(t testing.TB, err error, file string, line int) *loaderr.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want an error at %s:%d", file, line)
	}
	e, ok := loaderr.As(err)
	if !ok {
		t.Fatalf("err = %v (%T), want a *loaderr.Error", err, err)
	}
	if e.File != file {
		t.Errorf("File = %q, want %q (err: %v)", e.File, file, err)
	}
	if e.Line != line {
		t.Errorf("Line = %d, want %d (err: %v)", e.Line, line, err)
	}
	if e.Fix == "" {
		t.Errorf("Fix is empty (err: %v)", err)
	}
	if want := fmt.Sprintf("%s:%d:", file, line); file != "" && line > 0 && !strings.HasPrefix(e.Error(), want) {
		t.Errorf("message %q does not start with %q", e.Error(), want)
	}
	return e
}

// ExpectField is Expect for an error about field in data, the document the loader read: it checks that the error names field and points at the line FindField finds for it, which is 1 for the document as a whole.
func ExpectField(t testing.TB, err error, file string, data []byte, field string) *loaderr.Error {
	t.Helper()
	line, _ := loaderr.FindField(data, field)
	if line == 0 {
		t.Fatalf("FindField(%q) found no line in the test document", field)
	}
	e := Expect(t, err, file, line)
	if e.Field != field {
		t.Errorf("Field = %q, want %q (err: %v)", e.Field, field, err)
	}
	return e
}

// Indent returns the JSON document data indented one field per line, so a test of a single-line document checks lines that differ from field to field. It returns data unchanged when data is not valid JSON.
func Indent(data string) []byte {
	var b bytes.Buffer
	if json.Indent(&b, []byte(data), "", "  ") != nil {
		return []byte(data)
	}
	return b.Bytes()
}
