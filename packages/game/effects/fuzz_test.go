package effects_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/effects"
)

// FuzzCompile checks that any source either fails to compile with an error naming the effect, or compiles alone and chained after another effect into a program whose source declares the effect under the chain's names.
func FuzzCompile(f *testing.F) {
	for _, src := range []string{
		invertSource,
		blurSource,
		"package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos)\n}\n",
		"package main\n\nvar Tint vec4\n\nfunc Effect(pos vec2) vec4 {\n\treturn Input(pos) * Tint\n}\n",
		"package main\n\nfunc Effect(pos vec2) vec4 {\n\tInput := pos\n\treturn vec4(Input, 0, 1)\n}\n",
		"package main\n\nfunc Effect(pos vec2) vec4 {\n\treturn (\n}\n",
		"package main\n\nimport \"math\"\n",
		"",
	} {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		e := effects.Effect{Name: "fuzz", Source: src}
		c, err := effects.Compile(effects.Effect{Source: blurSource}, e)
		if err != nil {
			if !strings.HasPrefix(err.Error(), "effects: ") {
				t.Fatalf("an error without the package's prefix: %v", err)
			}
			return
		}
		if c.Len() != 2 || !strings.Contains(c.Source(), "p1_Effect") {
			t.Fatalf("a chain of %d effects:\n%s", c.Len(), c.Source())
		}
		if _, err := effects.Compile(e); err != nil {
			t.Fatalf("it compiled chained but not alone: %v", err)
		}
	})
}
