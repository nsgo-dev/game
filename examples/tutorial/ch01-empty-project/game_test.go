package main

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestShowsTitleOverPlayfield(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(360, 640))
	app.Find("text:Star Catcher")

	ops := app.Find("game-canvas").Ops
	if !hasFillColor(ops, skyColor) {
		t.Fatal("canvas did not paint the sky")
	}
	if !hasFillColor(ops, groundColor) {
		t.Fatal("canvas did not paint the ground")
	}
}

func TestGroundFollowsWindowSize(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(360, 640))
	if !hasRect(app.Find("game-canvas").Ops, 0, 640-groundHeight, 360, groundHeight) {
		t.Fatal("ground is not along the bottom of a 360x640 window")
	}

	app.Resize(800, 600)
	if !hasRect(app.Find("game-canvas").Ops, 0, 600-groundHeight, 800, groundHeight) {
		t.Fatal("ground did not follow the window to 800x600")
	}
}

func hasFillColor(ops []core.DrawOp, c core.Color) bool {
	for _, op := range ops {
		if op.Kind == core.DrawFillColor && op.Color == c {
			return true
		}
	}
	return false
}

func hasRect(ops []core.DrawOp, x, y, w, h float32) bool {
	for _, op := range ops {
		if op.Kind == core.DrawRect && op.X == x && op.Y == y && op.W == w && op.H == h {
			return true
		}
	}
	return false
}
