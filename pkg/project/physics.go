package project

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// Physics holds the project's default 2D and 3D gravity, solver iterations, named collision layers, and LDtk IntGrid collision flag mappings shared by scenes.
type Physics struct {
	// Gravity2D is the 2D gravity acceleration in points per second squared (for example {y: 980} in +Y-down screen space); nil leaves 2D gravity at (0, 0).
	Gravity2D *Vec2 `yaml:"gravity2D,omitempty,flow"`
	// Gravity3D is the 3D gravity acceleration in units per second squared (for example {y: -9.81} in Y-up 3D space); nil leaves 3D gravity at physics3d's default (0, -9.81, 0).
	Gravity3D *Vec3 `yaml:"gravity3D,omitempty,flow"`
	// SolverIterations2D is the number of sequential-impulse iterations per 2D rigid step; 0 uses physics.DefaultSolverIterations.
	SolverIterations2D int `yaml:"solverIterations2D,omitempty" schema:"minimum=0"`
	// SolverIterations3D is the number of solver iterations per 3D physics step; 0 uses physics3d's default.
	SolverIterations3D int `yaml:"solverIterations3D,omitempty" schema:"minimum=0"`
	// Layers maps human-readable collision layer names (such as "player", "walls", and "coins") to bit indices in [0, 31], in declaration order.
	Layers []PhysicsLayer `yaml:"layers,omitempty"`
	// LDtkIntGrid maps LDtk IntGrid value identifiers, group identifiers, or numeric values to tilemap.TileFlags when loading .ldtk projects.
	LDtkIntGrid []LDtkIntGridRule `yaml:"ldtkIntGrid,omitempty"`
}

// LDtkIntGridRule maps one LDtk IntGrid value (by identifier/group name or positive numeric value) to tile collision flags.
// Exactly one of Name or Value must be set; leaving Solid, OneWay, and Flags all unset explicitly maps matching cells to 0 (non-colliding).
type LDtkIntGridRule struct {
	// Name matches an LDtk IntGrid value identifier or IntGrid value group identifier (case-insensitive).
	Name string `yaml:"name,omitempty"`
	// Value matches a positive numeric LDtk IntGrid value (1, 2, ...).
	Value int `yaml:"value,omitempty" schema:"minimum=0"`
	// Solid sets tilemap.TileSolid on matching IntGrid cells.
	Solid bool `yaml:"solid,omitempty"`
	// OneWay sets tilemap.TileOneWay on matching IntGrid cells.
	OneWay bool `yaml:"oneWay,omitempty"`
	// Flags sets additional game-defined tilemap.TileFlags bits on matching IntGrid cells alongside Solid and OneWay.
	Flags uint32 `yaml:"flags,omitempty"`
}

// TileFlags returns the combined tilemap.TileFlags produced by r.
func (r LDtkIntGridRule) TileFlags() tilemap.TileFlags {
	var f tilemap.TileFlags
	if r.Solid {
		f |= tilemap.TileSolid
	}
	if r.OneWay {
		f |= tilemap.TileOneWay
	}
	f |= tilemap.TileFlags(r.Flags)
	return f
}

// Vec2 is a two-dimensional vector in game.yaml, such as 2D gravity.
type Vec2 struct {
	// X is the horizontal component.
	X float32 `yaml:"x,omitempty"`
	// Y is the vertical component.
	Y float32 `yaml:"y,omitempty"`
}

// Vec3 is a three-dimensional vector in game.yaml, such as 3D gravity.
type Vec3 struct {
	// X is the X-axis component.
	X float32 `yaml:"x,omitempty"`
	// Y is the Y-axis component.
	Y float32 `yaml:"y,omitempty"`
	// Z is the Z-axis component.
	Z float32 `yaml:"z,omitempty"`
}

// PhysicsLayer assigns a human-readable collision layer name to a bit index in [0, 31].
type PhysicsLayer struct {
	// Name uniquely identifies the collision layer, such as "player", "walls", or "coins".
	Name string `yaml:"name" schema:"required"`
	// Bit is the collision bit index in [0, 31], matching LayerBit(Bit).
	Bit int `yaml:"bit" schema:"minimum=0,maximum=31"`
}

func clonePhysics(p *Physics) *Physics {
	if p == nil {
		return nil
	}
	out := &Physics{
		Gravity2D:          clonePtr(p.Gravity2D),
		Gravity3D:          clonePtr(p.Gravity3D),
		SolverIterations2D: p.SolverIterations2D,
		SolverIterations3D: p.SolverIterations3D,
	}
	if p.Layers != nil {
		out.Layers = append([]PhysicsLayer(nil), p.Layers...)
	}
	if p.LDtkIntGrid != nil {
		out.LDtkIntGrid = append([]LDtkIntGridRule(nil), p.LDtkIntGrid...)
	}
	return out
}

func validatePhysics(p *Physics) error {
	if p.Gravity2D != nil {
		if !isFinite32(p.Gravity2D.X) || !isFinite32(p.Gravity2D.Y) {
			return fmt.Errorf("project: physics gravity2D (%v, %v) must be finite", p.Gravity2D.X, p.Gravity2D.Y)
		}
	}
	if p.Gravity3D != nil {
		if !isFinite32(p.Gravity3D.X) || !isFinite32(p.Gravity3D.Y) || !isFinite32(p.Gravity3D.Z) {
			return fmt.Errorf("project: physics gravity3D (%v, %v, %v) must be finite", p.Gravity3D.X, p.Gravity3D.Y, p.Gravity3D.Z)
		}
	}
	if p.SolverIterations2D < 0 {
		return fmt.Errorf("project: physics solverIterations2D %d must not be negative", p.SolverIterations2D)
	}
	if p.SolverIterations3D < 0 {
		return fmt.Errorf("project: physics solverIterations3D %d must not be negative", p.SolverIterations3D)
	}
	names := make(map[string]struct{}, len(p.Layers))
	bits := make(map[int]string, len(p.Layers))
	for i, l := range p.Layers {
		if err := validateName(fmt.Sprintf("physics layer %d", i), l.Name); err != nil {
			return err
		}
		if _, dup := names[l.Name]; dup {
			return fmt.Errorf("project: duplicate physics layer %q", l.Name)
		}
		if l.Bit < 0 || l.Bit > 31 {
			return fmt.Errorf("project: physics layer %q bit %d must be in [0, 31]", l.Name, l.Bit)
		}
		if prev, dup := bits[l.Bit]; dup {
			return fmt.Errorf("project: physics layer %q reuses bit %d (already used by %q)", l.Name, l.Bit, prev)
		}
		names[l.Name] = struct{}{}
		bits[l.Bit] = l.Name
	}
	intGridNames := make(map[string]struct{}, len(p.LDtkIntGrid))
	intGridValues := make(map[int]struct{}, len(p.LDtkIntGrid))
	for i, r := range p.LDtkIntGrid {
		hasName := r.Name != ""
		hasValue := r.Value != 0
		if hasName && hasValue {
			return fmt.Errorf("project: physics ldtkIntGrid[%d] cannot set both name %q and value %d", i, r.Name, r.Value)
		}
		if !hasName && !hasValue {
			return fmt.Errorf("project: physics ldtkIntGrid[%d] must set name or a positive value", i)
		}
		if r.Value < 0 {
			return fmt.Errorf("project: physics ldtkIntGrid[%d] value %d must be positive", i, r.Value)
		}
		if hasName {
			trimmed := strings.TrimSpace(r.Name)
			if trimmed == "" || trimmed != r.Name {
				return fmt.Errorf("project: physics ldtkIntGrid[%d] name %q must not be empty or have leading/trailing whitespace", i, r.Name)
			}
			key := strings.ToLower(trimmed)
			if _, dup := intGridNames[key]; dup {
				return fmt.Errorf("project: duplicate physics ldtkIntGrid name %q", r.Name)
			}
			intGridNames[key] = struct{}{}
		}
		if hasValue {
			if _, dup := intGridValues[r.Value]; dup {
				return fmt.Errorf("project: duplicate physics ldtkIntGrid value %d", r.Value)
			}
			intGridValues[r.Value] = struct{}{}
		}
	}
	return nil
}

// LayerBit returns the bit index in [0, 31] assigned to name, and false when p is nil or name is not declared.
func (p *Physics) LayerBit(name string) (uint, bool) {
	if p == nil {
		return 0, false
	}
	for _, l := range p.Layers {
		if l.Name == name {
			return uint(l.Bit), true
		}
	}
	return 0, false
}

// LayerTable returns p's declared layers as a *sceneformat.PhysicsLayerTable, or nil when p is nil.
func (p *Physics) LayerTable() *sceneformat.PhysicsLayerTable {
	if p == nil {
		return nil
	}
	defs := make([]sceneformat.PhysicsLayerDef, len(p.Layers))
	for i, l := range p.Layers {
		defs[i] = sceneformat.PhysicsLayerDef{Name: l.Name, Bit: l.Bit}
	}
	t, err := sceneformat.NewPhysicsLayerTable(defs)
	if err != nil {
		return nil
	}
	return t
}

// LayerMask2D resolves layer names into a 2D physics.LayerMask.
func (p *Physics) LayerMask2D(names ...string) (physics.LayerMask, error) {
	return p.LayerTable().Mask2D(names...)
}

// LayerMask3D resolves layer names into a 3D physics3d.LayerMask.
func (p *Physics) LayerMask3D(names ...string) (physics3d.LayerMask, error) {
	return p.LayerTable().Mask3D(names...)
}

// PhysicsLayerTable returns the project's named collision layer table, or nil when File.Physics is nil.
func (f *File) PhysicsLayerTable() *sceneformat.PhysicsLayerTable {
	if f == nil || f.Physics == nil {
		return nil
	}
	return f.Physics.LayerTable()
}

// PhysicsSettings converts File.Physics into a *sceneformat.PhysicsSettings for sceneformat.BuildPhysics.
// It returns nil when f or f.Physics is nil.
func (f *File) PhysicsSettings() *sceneformat.PhysicsSettings {
	if f == nil || f.Physics == nil {
		return nil
	}
	p := f.Physics
	s := &sceneformat.PhysicsSettings{
		SolverIterations2D: p.SolverIterations2D,
		SolverIterations3D: p.SolverIterations3D,
		Layers:             p.LayerTable(),
	}
	if p.Gravity2D != nil {
		s.HasGravity2D = true
		s.Gravity2D = math2d.Vec2{X: p.Gravity2D.X, Y: p.Gravity2D.Y}
	}
	if p.Gravity3D != nil {
		s.HasGravity3D = true
		s.Gravity3D = physics3d.Vec3{X: p.Gravity3D.X, Y: p.Gravity3D.Y, Z: p.Gravity3D.Z}
	}
	return s
}

// ConfigurePhysicsWorld applies the project's 2D gravity to w when Physics.Gravity2D is set.
func (f *File) ConfigurePhysicsWorld(w *physics.World) {
	if f == nil || f.Physics == nil || w == nil {
		return
	}
	if f.Physics.Gravity2D != nil {
		w.GravityX = f.Physics.Gravity2D.X
		w.GravityY = f.Physics.Gravity2D.Y
	}
}

// ConfigureRigidWorld applies the project's 2D gravity and solver iterations to w.
func (f *File) ConfigureRigidWorld(w *physics.RigidWorld) {
	if f == nil || f.Physics == nil || w == nil {
		return
	}
	if f.Physics.Gravity2D != nil {
		w.GravityX = f.Physics.Gravity2D.X
		w.GravityY = f.Physics.Gravity2D.Y
	}
	if f.Physics.SolverIterations2D > 0 {
		w.SolverIterations = f.Physics.SolverIterations2D
	}
}

// LDtkOptions builds a tilemap.LDtkOptions using p's LDtkIntGrid rules and resolveImage.
func (p *Physics) LDtkOptions(resolveImage func(relPath string) core.ImageSource) tilemap.LDtkOptions {
	opts := tilemap.LDtkOptions{ResolveImage: resolveImage}
	if p == nil || len(p.LDtkIntGrid) == 0 {
		return opts
	}
	for _, r := range p.LDtkIntGrid {
		flags := r.TileFlags()
		if r.Name != "" {
			if opts.IntGridFlags == nil {
				opts.IntGridFlags = make(map[string]tilemap.TileFlags)
			}
			opts.IntGridFlags[r.Name] = flags
		} else if r.Value > 0 {
			if opts.IntGridValueFlags == nil {
				opts.IntGridValueFlags = make(map[int]tilemap.TileFlags)
			}
			opts.IntGridValueFlags[r.Value] = flags
		}
	}
	return opts
}

// LDtkOptions builds a tilemap.LDtkOptions using f's physics.ldtkIntGrid rules and resolveImage.
func (f *File) LDtkOptions(resolveImage func(relPath string) core.ImageSource) tilemap.LDtkOptions {
	if f == nil || f.Physics == nil {
		return tilemap.LDtkOptions{ResolveImage: resolveImage}
	}
	return f.Physics.LDtkOptions(resolveImage)
}

// ConfigurePhysics3DWorld applies the project's 3D gravity and solver iterations to w.
func (f *File) ConfigurePhysics3DWorld(w *physics3d.World) {
	if f == nil || f.Physics == nil || w == nil {
		return
	}
	if f.Physics.Gravity3D != nil {
		w.Gravity = physics3d.Vec3{X: f.Physics.Gravity3D.X, Y: f.Physics.Gravity3D.Y, Z: f.Physics.Gravity3D.Z}
	}
	if f.Physics.SolverIterations3D > 0 {
		w.SolverIterations = f.Physics.SolverIterations3D
	}
}
