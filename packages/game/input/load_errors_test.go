package input_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestActionsLoadErrorsNameKeyLineAndFix(t *testing.T) {
	cases := []struct {
		name, json, field string
	}{
		{"version", `{"version": 9}`, "version"},
		{"input kind", `{"version": 1, "actions": {"jump": [{"kind": "none"}, {"kind": "keyboard"}]}}`, "actions.jump[1]"},
		{"motion axis", `{"version": 1, "motion_axes": {"aim": {"axis": "bogus"}}}`, "motion_axes.aim.axis"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} },
				nsgotest.Storage(map[string]string{"controls": string(data)}))
			_, err := input.NewActions().Load(app.Storage(), "controls")
			loaderrtest.ExpectField(t, err, "controls", data, tc.field)
		})
	}
	t.Run("syntax", func(t *testing.T) {
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} },
			nsgotest.Storage(map[string]string{"controls": "{\n  \"version\": 1,\n  \"actions\": {\n}"}))
		_, err := input.NewActions().Load(app.Storage(), "controls")
		loaderrtest.Expect(t, err, "controls", 4)
	})
}

func TestHapticsLoadErrorNamesKey(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} },
		nsgotest.Storage(map[string]string{"haptics.enabled": "notabool"}))
	_, err := (&input.Haptics{}).Load(app.Storage(), "haptics.enabled")
	loaderrtest.Expect(t, err, "haptics.enabled", 1)
}
