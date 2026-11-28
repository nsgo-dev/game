package editor

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/project"
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
	pal := ed.Palette(cx)
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

	numRow := func(testID, caption, help string, step float64, read func() float64, apply func(float64) error) core.Node {
		return psNumberField(cx, ed, rev, testID, caption, help, caption, step, policyAgentMayChange, disabled, read, apply)
	}

	layerList := core.Dynamic(func(cx *core.Scope) core.Node {
		p := readPhys()
		if p == nil || len(p.Layers) == 0 {
			return core.Text{
				TestID:  "project-settings-physics-layers-empty",
				Content: ed.T("projectSettings.physics.layersEmpty"),
				Style:   core.Style{FontSize: 11.5, Color: pal.TextFaint},
			}
		}
		rows := make([]core.Node, 0, len(p.Layers))
		for i, l := range p.Layers {
			idx := i
			rows = append(rows, core.View{
				Style: core.Style{
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         8,
					Padding:     core.Symmetric(8, 0),
					Borders:     core.Edges{Bottom: 1},
					BorderColor: pal.Line,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "project-settings-physics-layer-" + strconv.Itoa(idx),
						Content: ed.T("projectSettings.physics.layerItem", l10n.Param("bit", l.Bit), l10n.Param("name", l.Name)),
						Style:   core.Style{FontSize: 12.5, Color: pal.TextSecondary, FlexGrow: 1},
					},
					settingsSpecButton(pal, "project-settings-physics-layer-remove-"+strconv.Itoa(idx), ed.T("projectSettings.remove"), ed.T("projectSettings.physics.removeLayerLabel", l10n.Param("name", l.Name)), false, disabled, func() {
						if err := ed.RemoveProjectPhysicsLayer(idx); err != nil {
							layerErr.Set(err.Error())
							ed.report(err)
							return
						}
						layerErr.Set("")
					}),
					settingsPolicyBadge(pal, ed, policyAgentMayChange),
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
		Style:  core.Style{Gap: 4},
		Children: []core.Node{
			psSectionHeading(pal, ed.T("projectSettings.physics.heading2D")),
			numRow("project-settings-physics-g2x", ed.T("projectSettings.physics.gravity2DX"), ed.T("projectSettings.physics.gravity2DXHelp"), 10, func() float64 {
				if p := readPhys(); p != nil && p.Gravity2D != nil {
					return float64(p.Gravity2D.X)
				}
				return 0
			}, func(v float64) error {
				var y float32
				if ed.Project != nil && ed.Project.Physics != nil && ed.Project.Physics.Gravity2D != nil {
					y = ed.Project.Physics.Gravity2D.Y
				}
				return ed.SetProjectPhysicsGravity2D(float32(v), y)
			}),
			numRow("project-settings-physics-g2y", ed.T("projectSettings.physics.gravity2DY"), ed.T("projectSettings.physics.gravity2DYHelp"), 10, func() float64 {
				if p := readPhys(); p != nil && p.Gravity2D != nil {
					return float64(p.Gravity2D.Y)
				}
				return 0
			}, func(v float64) error {
				var x float32
				if ed.Project != nil && ed.Project.Physics != nil && ed.Project.Physics.Gravity2D != nil {
					x = ed.Project.Physics.Gravity2D.X
				}
				return ed.SetProjectPhysicsGravity2D(x, float32(v))
			}),
			numRow("project-settings-physics-iter2d", ed.T("projectSettings.physics.solverIter2D"), ed.T("projectSettings.physics.solverIter2DHelp"), 1, func() float64 {
				if p := readPhys(); p != nil {
					return float64(p.SolverIterations2D)
				}
				return 0
			}, func(v float64) error {
				var i3 int
				if ed.Project != nil && ed.Project.Physics != nil {
					i3 = ed.Project.Physics.SolverIterations3D
				}
				return ed.SetProjectPhysicsSolverIterations(int(math.Round(v)), i3)
			}),
			psSectionHeading(pal, ed.T("projectSettings.physics.heading3D")),
			numRow("project-settings-physics-g3x", ed.T("projectSettings.physics.gravity3DX"), ed.T("projectSettings.physics.gravity3DXHelp"), 0.5, func() float64 {
				if p := readPhys(); p != nil && p.Gravity3D != nil {
					return float64(p.Gravity3D.X)
				}
				return 0
			}, func(v float64) error {
				y, z := float32(-9.81), float32(0)
				if ed.Project != nil && ed.Project.Physics != nil && ed.Project.Physics.Gravity3D != nil {
					y, z = ed.Project.Physics.Gravity3D.Y, ed.Project.Physics.Gravity3D.Z
				}
				return ed.SetProjectPhysicsGravity3D(float32(v), y, z)
			}),
			numRow("project-settings-physics-g3y", ed.T("projectSettings.physics.gravity3DY"), ed.T("projectSettings.physics.gravity3DYHelp"), 0.5, func() float64 {
				if p := readPhys(); p != nil && p.Gravity3D != nil {
					return float64(p.Gravity3D.Y)
				}
				return -9.81
			}, func(v float64) error {
				x, z := float32(0), float32(0)
				if ed.Project != nil && ed.Project.Physics != nil && ed.Project.Physics.Gravity3D != nil {
					x, z = ed.Project.Physics.Gravity3D.X, ed.Project.Physics.Gravity3D.Z
				}
				return ed.SetProjectPhysicsGravity3D(x, float32(v), z)
			}),
			numRow("project-settings-physics-g3z", ed.T("projectSettings.physics.gravity3DZ"), ed.T("projectSettings.physics.gravity3DZHelp"), 0.5, func() float64 {
				if p := readPhys(); p != nil && p.Gravity3D != nil {
					return float64(p.Gravity3D.Z)
				}
				return 0
			}, func(v float64) error {
				x, y := float32(0), float32(-9.81)
				if ed.Project != nil && ed.Project.Physics != nil && ed.Project.Physics.Gravity3D != nil {
					x, y = ed.Project.Physics.Gravity3D.X, ed.Project.Physics.Gravity3D.Y
				}
				return ed.SetProjectPhysicsGravity3D(x, y, float32(v))
			}),
			numRow("project-settings-physics-iter3d", ed.T("projectSettings.physics.solverIter3D"), ed.T("projectSettings.physics.solverIter3DHelp"), 1, func() float64 {
				if p := readPhys(); p != nil {
					return float64(p.SolverIterations3D)
				}
				return 0
			}, func(v float64) error {
				var i2 int
				if ed.Project != nil && ed.Project.Physics != nil {
					i2 = ed.Project.Physics.SolverIterations2D
				}
				return ed.SetProjectPhysicsSolverIterations(i2, int(math.Round(v)))
			}),
			psSectionHeading(pal, ed.T("projectSettings.physics.layersHeading")),
			layerList,
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1},
						Children: []core.Node{ui.Input{
							TestID:      "project-settings-physics-layer-name",
							Label:       ed.T("projectSettings.physics.layerNameLabel"),
							Placeholder: ed.T("projectSettings.physics.layerNamePlaceholder"),
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
							Label:    ed.T("projectSettings.physics.layerBitLabel"),
							Min:      0,
							Max:      31,
							Step:     1,
							Disabled: disabled,
							Value:    func() float64 { return float64(newBit.Get()) },
							OnChange: func(v float64) { newBit.Set(int(math.Round(v))) },
						}},
					},
					settingsSpecButton(pal, "project-settings-physics-layer-add", ed.T("projectSettings.physics.addLayer"), ed.T("projectSettings.physics.addLayerLabel"), false, disabled, addLayer),
				},
			},
			psErrorText(pal, "project-settings-physics-layer-error", layerErr),
		},
	}
}
