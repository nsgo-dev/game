package editor

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// SetProjectPhysicsGravity2D updates game.yaml's physics.gravity2D through EditProject.
func (e *Editor) SetProjectPhysicsGravity2D(x, y float32) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Physics != nil && e.Project.Physics.Gravity2D != nil &&
		e.Project.Physics.Gravity2D.X == x && e.Project.Physics.Gravity2D.Y == y {
		return nil
	}
	return e.EditProject("Set 2D Gravity", func(f *project.File) {
		if f.Physics == nil {
			f.Physics = &project.Physics{}
		}
		f.Physics.Gravity2D = &project.Vec2{X: x, Y: y}
	})
}

// SetProjectPhysicsGravity3D updates game.yaml's physics.gravity3D through EditProject.
func (e *Editor) SetProjectPhysicsGravity3D(x, y, z float32) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Physics != nil && e.Project.Physics.Gravity3D != nil &&
		e.Project.Physics.Gravity3D.X == x && e.Project.Physics.Gravity3D.Y == y && e.Project.Physics.Gravity3D.Z == z {
		return nil
	}
	return e.EditProject("Set 3D Gravity", func(f *project.File) {
		if f.Physics == nil {
			f.Physics = &project.Physics{}
		}
		f.Physics.Gravity3D = &project.Vec3{X: x, Y: y, Z: z}
	})
}

// SetProjectPhysicsSolverIterations updates game.yaml's physics.solverIterations2D and solverIterations3D through EditProject.
func (e *Editor) SetProjectPhysicsSolverIterations(iter2D, iter3D int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Physics != nil &&
		e.Project.Physics.SolverIterations2D == iter2D &&
		e.Project.Physics.SolverIterations3D == iter3D {
		return nil
	}
	return e.EditProject("Set Physics Solver Iterations", func(f *project.File) {
		if f.Physics == nil {
			f.Physics = &project.Physics{}
		}
		f.Physics.SolverIterations2D = iter2D
		f.Physics.SolverIterations3D = iter3D
		cleanEmptyPhysics(f)
	})
}

// AddProjectPhysicsLayer appends a named collision layer to game.yaml's physics.layers through EditProject.
func (e *Editor) AddProjectPhysicsLayer(name string, bit int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	return e.EditProject("Add Physics Layer "+name, func(f *project.File) {
		if f.Physics == nil {
			f.Physics = &project.Physics{}
		}
		f.Physics.Layers = append(f.Physics.Layers, project.PhysicsLayer{Name: name, Bit: bit})
	})
}

// RemoveProjectPhysicsLayer removes the physics layer at index through EditProject.
func (e *Editor) RemoveProjectPhysicsLayer(index int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Physics == nil || index < 0 || index >= len(e.Project.Physics.Layers) {
		return fmt.Errorf("editor: physics layer index %d out of range", index)
	}
	name := e.Project.Physics.Layers[index].Name
	return e.EditProject("Remove Physics Layer "+name, func(f *project.File) {
		f.Physics.Layers = slices.Delete(f.Physics.Layers, index, index+1)
		if len(f.Physics.Layers) == 0 {
			f.Physics.Layers = nil
		}
		cleanEmptyPhysics(f)
	})
}

func cleanEmptyPhysics(f *project.File) {
	if f.Physics != nil &&
		f.Physics.Gravity2D == nil &&
		f.Physics.Gravity3D == nil &&
		f.Physics.SolverIterations2D == 0 &&
		f.Physics.SolverIterations3D == 0 &&
		len(f.Physics.Layers) == 0 {
		f.Physics = nil
	}
}

func psPhysicsSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	newName := core.NewSignal(cx, "")
	newBit := core.NewSignal(cx, 0)
	layerErr := core.NewSignal(cx, "")

	readPhys := func() *project.Physics {
		_ = rev.Get()
		if ed.Project == nil {
			return nil
		}
		return ed.Project.Physics
	}

	numRow := func(testID, caption string, step float64, read func() float64, apply func(float64) error) core.Node {
		return psFieldLayout(colors, caption, ui.NumberInput{
			TestID:   testID,
			Label:    caption,
			Step:     step,
			Disabled: disabled,
			Value:    read,
			OnChange: func(v float64) {
				if err := apply(v); err != nil {
					ed.report(err)
				}
			},
		}, nil)
	}

	layerList := core.Dynamic(func(cx *core.Scope) core.Node {
		p := readPhys()
		if p == nil || len(p.Layers) == 0 {
			return core.Text{
				TestID:  "project-settings-physics-layers-empty",
				Content: "No named collision layers declared",
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		rows := make([]core.Node, 0, len(p.Layers))
		for i, l := range p.Layers {
			idx := i
			rows = append(rows, ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.Text{
						TestID:  "project-settings-physics-layer-" + strconv.Itoa(idx),
						Content: fmt.Sprintf("Bit %d: %s", l.Bit, l.Name),
						Style:   core.Style{FontSize: 12, Color: colors.Text(), FlexGrow: 1},
					},
					ui.Button{
						TestID:   "project-settings-physics-layer-remove-" + strconv.Itoa(idx),
						Text:     "Remove",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonGhost,
						Disabled: disabled,
						OnPress: func() {
							if err := ed.RemoveProjectPhysicsLayer(idx); err != nil {
								layerErr.Set(err.Error())
								ed.report(err)
								return
							}
							layerErr.Set("")
						},
					},
				},
			})
		}
		return core.View{Style: core.Style{Gap: 4}, Children: rows}
	})

	addLayer := func() {
		name := strings.TrimSpace(newName.Get())
		if err := ed.AddProjectPhysicsLayer(name, newBit.Get()); err != nil {
			layerErr.Set(err.Error())
			ed.report(err)
			return
		}
		layerErr.Set("")
		newName.Set("")
		if b := newBit.Get(); b < 31 {
			newBit.Set(b + 1)
		}
	}

	return core.View{
		TestID: "project-settings-physics",
		Style:  core.Style{Gap: 8},
		Children: []core.Node{
			psSectionHeading(colors, "PHYSICS 2D"),
			numRow("project-settings-physics-g2x", "Gravity 2D X", 10, func() float64 {
				if p := readPhys(); p != nil && p.Gravity2D != nil {
					return float64(p.Gravity2D.X)
				}
				return 0
			}, func(v float64) error {
				var y float32
				if p := ed.Project.Physics; p != nil && p.Gravity2D != nil {
					y = p.Gravity2D.Y
				}
				return ed.SetProjectPhysicsGravity2D(float32(v), y)
			}),
			numRow("project-settings-physics-g2y", "Gravity 2D Y", 10, func() float64 {
				if p := readPhys(); p != nil && p.Gravity2D != nil {
					return float64(p.Gravity2D.Y)
				}
				return 0
			}, func(v float64) error {
				var x float32
				if p := ed.Project.Physics; p != nil && p.Gravity2D != nil {
					x = p.Gravity2D.X
				}
				return ed.SetProjectPhysicsGravity2D(x, float32(v))
			}),
			numRow("project-settings-physics-iter2d", "Solver Iterations 2D", 1, func() float64 {
				if p := readPhys(); p != nil {
					return float64(p.SolverIterations2D)
				}
				return 0
			}, func(v float64) error {
				var i3 int
				if p := ed.Project.Physics; p != nil {
					i3 = p.SolverIterations3D
				}
				return ed.SetProjectPhysicsSolverIterations(int(math.Round(v)), i3)
			}),
			psSectionHeading(colors, "PHYSICS 3D"),
			numRow("project-settings-physics-g3x", "Gravity 3D X", 0.5, func() float64 {
				if p := readPhys(); p != nil && p.Gravity3D != nil {
					return float64(p.Gravity3D.X)
				}
				return 0
			}, func(v float64) error {
				y, z := float32(-9.81), float32(0)
				if p := ed.Project.Physics; p != nil && p.Gravity3D != nil {
					y, z = p.Gravity3D.Y, p.Gravity3D.Z
				}
				return ed.SetProjectPhysicsGravity3D(float32(v), y, z)
			}),
			numRow("project-settings-physics-g3y", "Gravity 3D Y", 0.5, func() float64 {
				if p := readPhys(); p != nil && p.Gravity3D != nil {
					return float64(p.Gravity3D.Y)
				}
				return -9.81
			}, func(v float64) error {
				x, z := float32(0), float32(0)
				if p := ed.Project.Physics; p != nil && p.Gravity3D != nil {
					x, z = p.Gravity3D.X, p.Gravity3D.Z
				}
				return ed.SetProjectPhysicsGravity3D(x, float32(v), z)
			}),
			numRow("project-settings-physics-g3z", "Gravity 3D Z", 0.5, func() float64 {
				if p := readPhys(); p != nil && p.Gravity3D != nil {
					return float64(p.Gravity3D.Z)
				}
				return 0
			}, func(v float64) error {
				x, y := float32(0), float32(-9.81)
				if p := ed.Project.Physics; p != nil && p.Gravity3D != nil {
					x, y = p.Gravity3D.X, p.Gravity3D.Y
				}
				return ed.SetProjectPhysicsGravity3D(x, y, float32(v))
			}),
			numRow("project-settings-physics-iter3d", "Solver Iterations 3D", 1, func() float64 {
				if p := readPhys(); p != nil {
					return float64(p.SolverIterations3D)
				}
				return 0
			}, func(v float64) error {
				var i2 int
				if p := ed.Project.Physics; p != nil {
					i2 = p.SolverIterations2D
				}
				return ed.SetProjectPhysicsSolverIterations(i2, int(math.Round(v)))
			}),
			psSectionHeading(colors, "COLLISION LAYERS"),
			layerList,
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{ui.Input{
							TestID:      "project-settings-physics-layer-name",
							Label:       "Layer Name",
							Placeholder: "player",
							Disabled:    disabled,
							Value:       newName.Get,
							OnChange:    newName.Set,
							OnSubmit:    func(string) { addLayer() },
						}},
					},
					core.View{
						Style: core.Style{Width: core.Pt(72)},
						Children: []core.Node{ui.NumberInput{
							TestID:   "project-settings-physics-layer-bit",
							Label:    "Bit",
							Min:      0,
							Max:      31,
							Step:     1,
							Disabled: disabled,
							Value:    func() float64 { return float64(newBit.Get()) },
							OnChange: func(v float64) { newBit.Set(int(math.Round(v))) },
						}},
					},
					ui.Button{
						TestID:   "project-settings-physics-layer-add",
						Text:     "Add",
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress:  addLayer,
					},
				},
			},
			psErrorText(colors, "project-settings-physics-layer-error", layerErr),
		},
	}
}
