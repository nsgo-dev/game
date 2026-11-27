//go:build release

package physics_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDebugDrawReleaseEmitsZeroOps(t *testing.T) {
	grid := physics.NewGrid(4, 4, 16, 16)
	grid.Set(0, 0, physics.TileSolid)
	grid.Set(1, 0, physics.TileOneWay)

	body := physics.NewBoxBody(8, 8, 16, 16)
	body.VX, body.VY = 10, -5
	world := &physics.World{
		Tiles:  grid,
		Bodies: []*physics.Body{body, physics.NewCircleBody(40, 20, 6)},
	}
	contact := physics.Contact{Hit: true, PointX: 16, PointY: 16, NormalX: 0, NormalY: -1, Depth: 2}
	tileContacts := []physics.TileContact{{Col: 0, Row: 0, Flags: physics.TileSolid, NormalY: -1}}
	visible := core.Bounds{Size: core.Size{Width: 64, Height: 64}}
	from := core.Point{X: 0, Y: 16}
	to := core.Point{X: 64, Y: 16}
	hits := world.Raycast(from, to, physics.QueryAll)

	style := physics.DefaultDebugStyle()
	style.LineWidth = 2
	style.VelocityScale = 0.5

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "release",
			Style:  core.Style{Width: core.Pt(200), Height: core.Pt(200)},
			Draw: func(d *core.Drawing, _ core.Size) {
				physics.DrawShape(d, body.Shape, style)
				physics.DrawBody(d, body, style)
				physics.DrawContact(d, contact, style)
				physics.DrawTileContacts(d, grid, tileContacts, style)
				physics.DrawTileGrid(d, grid, visible, style)
				physics.DrawWorld(d, world, visible, []physics.Contact{contact}, style)
				physics.DrawRaycast(d, from, to, hits, style)
			},
		}
	})

	if ops := app.Find("release").Ops; len(ops) != 0 {
		t.Fatalf("release build emitted %d draw ops, want 0: %+v", len(ops), ops)
	}
}
