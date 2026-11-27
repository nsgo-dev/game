//go:build !release

package debug_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestLoadTunablesErrorsNameKeyLineAndFix(t *testing.T) {
	cases := []struct {
		name, json string
		line       int
	}{
		{"range", "{\n  \"lives\": 3,\n  \"gravity\": 999\n}", 3},
		{"int type", "{\n  \"lives\": 2.5\n}", 2},
		{"bool type", "{\n  \"gravity\": 1,\n  \"debug_ai\": \"yes\"\n}", 3},
		{"syntax", "{\n  \"gravity\": 1\n  \"lives\": 2\n}", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} },
				nsgotest.Storage(map[string]string{"dev.tunables": tc.json}))
			c := debug.NewConsole()
			g, n, b := 1.0, 1, false
			c.RegisterFloat("gravity", 0, 100, &g)
			c.RegisterInt("lives", 1, 99, &n)
			c.RegisterBool("debug_ai", &b)
			loaderrtest.Expect(t, c.LoadTunables(app.Storage(), "dev.tunables"), "dev.tunables", tc.line)
		})
	}
}
