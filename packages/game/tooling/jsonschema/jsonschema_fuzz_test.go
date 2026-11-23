package jsonschema_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
)

func FuzzValidateYAML(f *testing.F) {
	f.Add([]byte("version: 2\nmode: fast\nroot: {name: r, children: [{name: c}]}\n"))
	f.Add([]byte("a: &a [1, 2]\nb: *a\nc: {<<: {x: 1}}\n"))
	f.Add([]byte("version: .nan\ncount: 1e400\ntags: !!binary aGk=\n"))
	f.Add([]byte("- [[[[[]]]]]\n"))
	f.Add([]byte(""))
	s, err := jsonschema.Reflect(reflect.TypeFor[Doc](), jsonschema.Options{})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		err := s.ValidateYAML(data)
		var verr *jsonschema.ValidationError
		if errors.As(err, &verr) && len(verr.Issues) == 0 {
			t.Fatal("a ValidationError with no issues")
		}
	})
}
