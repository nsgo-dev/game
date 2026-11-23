// Package effects draws a game's frame through a chain of shader passes, in order, with uniforms animated on the platform's frame clock.
//
// An Effect is a fragment program in Kage, Go's syntax for shaders (core.CompileShader), that defines
//
//	func Effect(pos vec2) vec4
//
// the premultiplied color of the frame's point pos, in the frame's pixels from its top left corner, and reads the pass before it with Input(pos) and the frame's size in pixels with InputSize(). The first effect's Input reads the frame itself: the pixel under pos, transparent outside the frame. Exported variables at the package level are the effect's uniforms.
//
// Compile fuses a chain's effects into one program, in which each effect's Input calls the effect before it, so the passes run in one draw with no texture between them: a pass that reads its input at several points runs the passes before it at each of them. View draws a frame, usually a pixel buffer the game writes (core.ImageSource.Pixels), through a chain.
package effects

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// Effect is one pass of a chain.
type Effect struct {
	// Name names the effect in errors; the effect's index names it when empty.
	Name string
	// Source is the effect's Kage program, as the package documentation describes it.
	Source string
	// Reference draws the effect on the CPU for Chain.Reference: the premultiplied color, red, green, blue and alpha from 0 to 1, of the frame's point (x, y) given the pass before it and the effect's uniforms, by the names its source declares, as Chain.Reference is given them (Float reads a number). A uniform left out is 0, as on the GPU. It is nil for an effect that has none, which a chain draws on the GPU only.
	Reference func(in Input, u core.Uniforms, x, y float64) [4]float64
}

// Chain is effects compiled into one program, which View draws a frame through.
type Chain struct {
	program  *core.ShaderProgram
	source   string
	effects  []Effect
	uniforms []map[string]bool
}

// The names an effect's source declares or reads that the chain gives meaning to.
const (
	entryName     = "Effect"
	inputName     = "Input"
	inputSizeName = "InputSize"
	fragmentName  = "Fragment"
)

// reservedName matches the names the chain gives effects' own declarations, which no source may use itself.
var reservedName = regexp.MustCompile(`^[pP][0-9]+_`)

// imageName matches the program's image functions, which only the chain reads.
var imageName = regexp.MustCompile(`^image[0-9]+(At|Size)$`)

// viewNames are the built-ins that describe the view, in points, rather than the frame: an effect works in the frame's pixels, which its Reference can draw.
var viewNames = map[string]bool{"rectSize": true, "pixelScale": true}

// Compile compiles effects into a chain that draws them in order, the first over the frame and each next over the one before it. No effects make a chain that draws the frame as it is.
//
// Each effect's source is checked and compiled on its own first, so an error names the effect and gives the line and column in its source.
func Compile(effects ...Effect) (*Chain, error) {
	c := &Chain{effects: slices.Clone(effects), uniforms: make([]map[string]bool, len(effects))}
	var fused strings.Builder
	fused.WriteString("package main\n")
	for i, e := range effects {
		f, err := parseEffect(e.Source)
		if err != nil {
			return nil, fmt.Errorf("effects: %s: %w", e.label(i), err)
		}
		// The effect alone, with the frame as its input, keeps its source's lines and columns in the compiler's errors.
		if _, err := core.CompileShader(e.Source + alone); err != nil {
			return nil, fmt.Errorf("effects: %s: %w", e.label(i), err)
		}
		c.uniforms[i] = uniformsOf(f)
		fmt.Fprintf(&fused, "\n// Effect %d.\n%s\n", i, rename(f, e.Source, i))
	}
	last := inputOf(len(effects))
	fmt.Fprintf(&fused, `
// Fragment maps the view's points onto the frame's pixels and draws the last pass there.
func Fragment(dst vec4, pos vec2) vec4 {
	return %s(pos / rectSize() * image0Size())
}
`, last)
	c.source = fused.String()
	p, err := core.CompileShader(c.source)
	if err != nil {
		return nil, fmt.Errorf("effects: chain: %w", err)
	}
	c.program = p
	return c, nil
}

// MustCompile is Compile for effects that are part of the program: it panics when they do not compile.
func MustCompile(effects ...Effect) *Chain {
	c, err := Compile(effects...)
	if err != nil {
		panic(err)
	}
	return c
}

// alone is what an effect's source is compiled with on its own: the frame as its input, and the entry point.
const alone = `

func Input(pos vec2) vec4 {
	return image0At(pos)
}

func InputSize() vec2 {
	return image0Size()
}

func Fragment(dst vec4, pos vec2) vec4 {
	return Effect(pos / rectSize() * image0Size())
}
`

// Len returns how many effects the chain draws.
func (c *Chain) Len() int { return len(c.effects) }

// Source returns the Kage program the chain's effects compile into, which is the same for the same effects.
func (c *Chain) Source() string { return c.source }

// Program returns the chain's compiled program, which reads the frame as image0 and the effects' uniforms under the names Uniforms gives them.
func (c *Chain) Program() *core.ShaderProgram { return c.program }

// HasUniform reports whether effect i of the chain declares a uniform named name.
func (c *Chain) HasUniform(i int, name string) bool {
	return i >= 0 && i < len(c.uniforms) && c.uniforms[i][name]
}

// Uniforms returns the program's uniforms from each effect's own, in the chain's order: perEffect[i] sets effect i's uniforms by the names its source declares. A value is what core.Uniforms takes, an AnimatedNode among them, which follows the platform's frame clock. It panics when an effect has no uniform of a name, or when perEffect has more entries than the chain has effects.
func (c *Chain) Uniforms(perEffect []core.Uniforms) core.Uniforms {
	if len(perEffect) > len(c.effects) {
		panic(fmt.Sprintf("effects: uniforms for %d effects, but the chain has %d", len(perEffect), len(c.effects)))
	}
	n := 0
	for _, u := range perEffect {
		n += len(u)
	}
	if n == 0 {
		return nil
	}
	out := make(core.Uniforms, n)
	for i, u := range perEffect {
		for name, v := range u {
			if !c.uniforms[i][name] {
				panic(fmt.Sprintf("effects: %s has no uniform %s", c.effects[i].label(i), name))
			}
			out[uniformPrefix(i)+name] = v
		}
	}
	return out
}

// label names effect i in errors.
func (e Effect) label(i int) string {
	if e.Name != "" {
		return fmt.Sprintf("effect %d (%s)", i, e.Name)
	}
	return fmt.Sprintf("effect %d", i)
}

// uniformPrefix and declPrefix start the names the chain gives effect i's uniforms and other declarations: a uniform's stays exported, as Kage's uniforms are.
func uniformPrefix(i int) string { return fmt.Sprintf("P%d_", i) }
func declPrefix(i int) string    { return fmt.Sprintf("p%d_", i) }

// inputOf returns what effect i reads as its input: the frame for the first, else the effect before it.
func inputOf(i int) string {
	if i == 0 {
		return "image0At"
	}
	return declPrefix(i-1) + entryName
}

// parseEffect parses an effect's source and checks the names it declares and reads.
func parseEffect(src string) (*ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if err != nil {
		var list scanner.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			return nil, list[0]
		}
		return nil, err
	}
	if len(f.Imports) > 0 {
		return nil, fmt.Errorf("%s: an effect imports nothing", fset.Position(f.Imports[0].Pos()))
	}
	for _, name := range []string{inputName, inputSizeName, fragmentName} {
		if o := f.Scope.Lookup(name); o != nil {
			return nil, fmt.Errorf("%s: %s is the chain's, which an effect does not declare", fset.Position(o.Pos()), name)
		}
	}
	if o := f.Scope.Lookup(entryName); o == nil || o.Kind != ast.Fun {
		return nil, fmt.Errorf("an effect declares func %s(pos vec2) vec4", entryName)
	}
	var bad error
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || bad != nil || id == f.Name {
			return bad == nil
		}
		switch {
		case reservedName.MatchString(id.Name):
			bad = fmt.Errorf("%s: %s: names starting with p or P, digits and _ are the chain's", fset.Position(id.Pos()), id.Name)
		case id.Obj == nil && imageName.MatchString(id.Name):
			bad = fmt.Errorf("%s: an effect reads the frame with %s and %s, not %s", fset.Position(id.Pos()), inputName, inputSizeName, id.Name)
		case id.Obj == nil && viewNames[id.Name]:
			bad = fmt.Errorf("%s: an effect works in the frame's pixels, which %s measures, not the view's %s", fset.Position(id.Pos()), inputSizeName, id.Name)
		case strings.HasPrefix(id.Name, "__"):
			bad = fmt.Errorf("%s: %s: names starting with __ are the shader compiler's", fset.Position(id.Pos()), id.Name)
		}
		return bad == nil
	})
	return f, bad
}

// uniformsOf returns the uniforms f declares: its package-level variables.
func uniformsOf(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for name, o := range f.Scope.Objects {
		if o.Kind == ast.Var {
			out[name] = true
		}
	}
	return out
}

// rename returns effect i's source, f parsed, without its package clause, with its package-level names prefixed for the chain and its Input and InputSize calling what it reads.
func rename(f *ast.File, src string, i int) string {
	type edit struct {
		at, end int
		to      string
	}
	var edits []edit
	pos := func(p token.Pos) int { return int(p) - int(f.FileStart) }
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			// A selector's name is a field or a swizzle, never a package-level name.
			ast.Inspect(n.X, visit)
			return false
		case *ast.Ident:
			to := ""
			switch {
			case n.Obj != nil && f.Scope.Lookup(n.Name) == n.Obj && n.Obj.Kind == ast.Var:
				to = uniformPrefix(i) + n.Name
			case n.Obj != nil && f.Scope.Lookup(n.Name) == n.Obj:
				to = declPrefix(i) + n.Name
			case n.Obj == nil && n.Name == inputName:
				to = inputOf(i)
			case n.Obj == nil && n.Name == inputSizeName:
				to = "image0Size"
			}
			if to != "" {
				at := pos(n.Pos())
				edits = append(edits, edit{at, at + len(n.Name), to})
			}
		}
		return true
	}
	for _, d := range f.Decls {
		ast.Inspect(d, visit)
	}
	slices.SortFunc(edits, func(a, b edit) int { return a.at - b.at })
	var b strings.Builder
	at := pos(f.Name.End())
	for _, e := range edits {
		b.WriteString(src[at:e.at])
		b.WriteString(e.to)
		at = e.end
	}
	b.WriteString(src[at:])
	return strings.TrimSpace(b.String())
}
