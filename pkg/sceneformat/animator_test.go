package sceneformat_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

type stubAnimator struct {
	states   []string
	triggers []string
}

func (s *stubAnimator) SetState(name string) bool {
	s.states = append(s.states, name)
	return true
}

func (s *stubAnimator) Trigger(name string) {
	s.triggers = append(s.triggers, name)
}

type stubAnimatorProvider map[string]sceneformat.AnimatorController

func (p stubAnimatorProvider) Animator(id string) sceneformat.AnimatorController {
	return p[id]
}

func TestAnimatorComponentConnectDefaultRegistryAndHandlers(t *testing.T) {
	reg := sceneformat.AnimatorComponentRegistry()
	var stateEvents []sceneformat.AnimatorStateChangedEvent
	var clipEvents []sceneformat.AnimatorClipEvent
	if err := reg.Register(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed"}, {Name: "reset"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name: "listener",
		Handlers: []sceneformat.Handler{
			{
				Name: "onState",
				Func: func(c sceneformat.Call) {
					if ev, ok := c.Payload.(sceneformat.AnimatorStateChangedEvent); ok {
						stateEvents = append(stateEvents, ev)
					}
				},
			},
			{
				Name: "onCue",
				Func: func(c sceneformat.Call) {
					if ev, ok := c.Payload.(sceneformat.AnimatorClipEvent); ok {
						clipEvents = append(clipEvents, ev)
					}
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	const src = `version: 2
layers:
  - name: main
    objects:
      - id: button
        components:
          button: {}
      - id: knight
        components:
          animator:
            states: anim/knight.states.yaml
      - id: audio
        components:
          listener: {}
connections:
  - from: button
    event: pressed
    to: knight
    handler: animator.trigger
    args:
      name: attack
  - from: button
    event: reset
    to: knight
    handler: animator.setState
    args:
      state: idle
  - from: knight
    event: animator.stateChanged
    to: audio
    handler: onState
  - from: knight
    event: animator.clipEvent
    to: audio
    handler: onCue
`
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}

	// Connect must succeed with the built-in animator component registered as-is (no custom handler Func override).
	sig, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		t.Fatalf("Connect with default animator registry: %v", err)
	}

	// Emitting before any controller is bound is a safe no-op.
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatalf("Emit before BindAnimator: %v", err)
	}

	ctrl := &stubAnimator{}
	if err := sig.BindAnimator("knight", ctrl); err != nil {
		t.Fatalf("BindAnimator: %v", err)
	}
	if sig.Animator("knight") != ctrl {
		t.Fatal("Signals.Animator did not return bound controller")
	}

	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatalf("Emit pressed: %v", err)
	}
	if err := sig.Emit("button", "reset", nil); err != nil {
		t.Fatalf("Emit reset: %v", err)
	}
	if !slices.Equal(ctrl.triggers, []string{"attack"}) {
		t.Fatalf("triggers = %v, want [attack]", ctrl.triggers)
	}
	if !slices.Equal(ctrl.states, []string{"idle"}) {
		t.Fatalf("states = %v, want [idle]", ctrl.states)
	}

	wantState := sceneformat.AnimatorStateChangedEvent{ObjectID: "knight", From: "idle", To: "attack"}
	if err := sig.Emit("knight", "animator.stateChanged", wantState); err != nil {
		t.Fatalf("Emit stateChanged: %v", err)
	}
	wantCue := sceneformat.AnimatorClipEvent{ObjectID: "knight", State: "attack", Name: "slash"}
	if err := sig.Emit("knight", "animator.clipEvent", wantCue); err != nil {
		t.Fatalf("Emit clipEvent: %v", err)
	}
	if !reflect.DeepEqual(stateEvents, []sceneformat.AnimatorStateChangedEvent{wantState}) {
		t.Fatalf("stateEvents = %+v, want [%+v]", stateEvents, wantState)
	}
	if !reflect.DeepEqual(clipEvents, []sceneformat.AnimatorClipEvent{wantCue}) {
		t.Fatalf("clipEvents = %+v, want [%+v]", clipEvents, wantCue)
	}

	// Unbinding with nil stops driving ctrl.
	if err := sig.BindAnimator("knight", nil); err != nil {
		t.Fatalf("unbind BindAnimator: %v", err)
	}
	if sig.Animator("knight") != nil {
		t.Fatal("Signals.Animator non-nil after unbind")
	}
	if err := sig.Emit("button", "pressed", nil); err != nil {
		t.Fatalf("Emit after unbind: %v", err)
	}
	if len(ctrl.triggers) != 1 {
		t.Fatalf("unbound controller still received trigger: %v", ctrl.triggers)
	}
}

func TestAnimatorHandlersResolveContextAndPrecedence(t *testing.T) {
	reg := sceneformat.AnimatorComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "go"}},
	})
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{
				{ID: "button", Components: map[string]any{"button": map[string]any{}}},
				{ID: "knight", Components: map[string]any{"animator": map[string]any{"states": "anim/knight.states.yaml"}}},
			},
		}},
		Connections: []sceneformat.ConnectionDoc{
			{From: "button", Event: "go", To: "knight", Handler: "animator.setState", Args: map[string]any{"state": "run"}},
			{From: "button", Event: "go", To: "knight", Handler: "animator.trigger", Args: map[string]any{"name": "jump"}},
		},
	}

	// 1. BuiltScene implements AnimatorProvider when passed as Call.Context.
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctxCtrl := &stubAnimator{}
	built.SetAnimator("knight", ctxCtrl)
	sig, err := sceneformat.Connect(built.Doc, reg, built)
	if err != nil {
		t.Fatalf("Connect with BuiltScene context: %v", err)
	}
	if err := sig.Emit("button", "go", nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ctxCtrl.states, []string{"run"}) || !slices.Equal(ctxCtrl.triggers, []string{"jump"}) {
		t.Fatalf("BuiltScene context controller got states=%v triggers=%v", ctxCtrl.states, ctxCtrl.triggers)
	}

	// 2. Signals.BindAnimator takes precedence over Call.Context.
	boundCtrl := &stubAnimator{}
	if err := sig.BindAnimator("knight", boundCtrl); err != nil {
		t.Fatal(err)
	}
	if err := sig.Emit("button", "go", nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(boundCtrl.states, []string{"run"}) || !slices.Equal(boundCtrl.triggers, []string{"jump"}) {
		t.Fatalf("bound controller got states=%v triggers=%v", boundCtrl.states, boundCtrl.triggers)
	}
	if len(ctxCtrl.states) != 1 || len(ctxCtrl.triggers) != 1 {
		t.Fatalf("context controller called while Signals binding active: states=%v triggers=%v", ctxCtrl.states, ctxCtrl.triggers)
	}

	// 3. Custom AnimatorProvider as Call.Context works too.
	customCtrl := &stubAnimator{}
	sigCustom, err := sceneformat.Connect(doc, reg, stubAnimatorProvider{"knight": customCtrl})
	if err != nil {
		t.Fatal(err)
	}
	if err := sigCustom.Emit("button", "go", nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(customCtrl.states, []string{"run"}) || !slices.Equal(customCtrl.triggers, []string{"jump"}) {
		t.Fatalf("custom provider controller got states=%v triggers=%v", customCtrl.states, customCtrl.triggers)
	}

	// 4. Typed-nil *BuiltScene as Call.Context must not panic.
	var nilBuilt *sceneformat.BuiltScene
	sigNilCtx, err := sceneformat.Connect(doc, reg, nilBuilt)
	if err != nil {
		t.Fatal(err)
	}
	if err := sigNilCtx.Emit("button", "go", nil); err != nil {
		t.Fatal(err)
	}

	// 5. Validation errors on BindAnimator.
	var nilSig *sceneformat.Signals
	if err := nilSig.BindAnimator("knight", boundCtrl); err == nil {
		t.Fatal("BindAnimator on nil Signals succeeded, want error")
	}
	if err := sig.BindAnimator("missing", boundCtrl); err == nil {
		t.Fatal("BindAnimator on missing object succeeded, want error")
	}
}
