package sceneformat_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func TestSceneParametersRoundTripAndTypeValidation(t *testing.T) {
	raw := []byte("# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v4.json\nversion: 4\nparameters:\n  - name: speed\n    type: float\n    default: 3.5\n    description: Move speed\n    bind:\n      - object: body\n        component: mover\n        field: speed\nlayers:\n  - name: main\n    objects:\n      - id: body\n        components:\n          mover:\n            speed: 3.5\n")
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("round trip changed scene:\n%s\nwant:\n%s", encoded, raw)
	}
}

func TestSceneParametersRejectUnknownWrongTypeAndMissingBindingObject(t *testing.T) {
	cases := map[string]string{
		"unknown type":   "version: 4\nparameters:\n  - name: speed\n    type: duration\n    default: 1\n    bind: [{object: body, field: speed}]\nlayers: [{name: main, objects: [{id: body}]}]\n",
		"wrong type":     "version: 4\nparameters:\n  - name: speed\n    type: float\n    default: fast\n    bind: [{object: body, field: speed}]\nlayers: [{name: main, objects: [{id: body}]}]\n",
		"missing object": "version: 4\nparameters:\n  - name: speed\n    type: float\n    default: 1\n    bind: [{object: missing, field: speed}]\nlayers: [{name: main, objects: [{id: body}]}]\n",
	}
	for name, raw := range cases {
		_, err := sceneformat.Parse([]byte(raw))
		if err == nil {
			t.Errorf("%s: Parse succeeded", name)
			continue
		}
		var field *sceneformat.FieldError
		if !errors.As(err, &field) || !strings.Contains(err.Error(), "parameter") {
			t.Errorf("%s: error = %v, want parameter FieldError", name, err)
		}
	}
}
