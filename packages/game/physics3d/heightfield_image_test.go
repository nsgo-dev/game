package physics3d_test

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

func TestNewHeightfieldFromImageFormatsAndChannels(t *testing.T) {
	t.Run("Gray16 linear ramp with min/max height and raycast", func(t *testing.T) {
		// 5x5 16-bit ramp: height(c, r) = -2 + ((c + r) / 8) * 10  in [-2, 8].
		img := image.NewGray16(image.Rect(0, 0, 5, 5))
		for r := range 5 {
			for c := range 5 {
				v := uint16(math.Round((float64(c+r) / 8.0) * 65535.0))
				img.SetGray16(c, r, color.Gray16{Y: v})
			}
		}

		hf, err := physics3d.NewHeightfieldFromImage(img, physics3d.HeightfieldImageOptions{
			WorldWidth: 8.0, // CellSize = 8 / 4 = 2.0
			MinHeight:  -2.0,
			MaxHeight:  8.0,
			Origin:     physics3d.Vec3{X: -4, Y: 1, Z: -4},
			Tag:        "terrain",
		})
		if err != nil {
			t.Fatalf("NewHeightfieldFromImage: %v", err)
		}
		if hf.Cols != 5 || hf.Rows != 5 || !approx(hf.CellSize, 2.0, 1e-5) {
			t.Fatalf("grid = (%dx%d, cell=%v), want (5x5, cell=2)", hf.Cols, hf.Rows, hf.CellSize)
		}

		// Plane formula: y = Origin.Y + (-2) + ((x - (-4))/2 + (z - (-4))/2) * (10/8)
		//                  = -1 + 0.625*(x+4) + 0.625*(z+4)
		for _, pt := range [][2]float32{{-4, -4}, {0, 0}, {4, 4}, {-1.3, 2.7}, {2.4, -0.8}} {
			gotY, ok := hf.HeightAt(pt[0], pt[1])
			if !ok {
				t.Fatalf("HeightAt(%v, %v) out of bounds", pt[0], pt[1])
			}
			wantY := float32(-1.0) + 0.625*(pt[0]+4) + 0.625*(pt[1]+4)
			if !approx(gotY, wantY, 1e-2) {
				t.Fatalf("HeightAt(%v, %v) = %v, want %v", pt[0], pt[1], gotY, wantY)
			}

			hit, hitOK := hf.Raycast(physics3d.Ray{
				Origin:  physics3d.Vec3{X: pt[0], Y: 20, Z: pt[1]},
				Dir:     physics3d.Vec3{Y: -1},
				MaxDist: 40,
			})
			if !hitOK || !approx(hit.Point.Y, wantY, 1e-2) {
				t.Fatalf("Raycast at (%v, %v) = (%+v, %v), want Y=%v", pt[0], pt[1], hit, hitOK, wantY)
			}
		}
	})

	t.Run("NRGBA channel selection and sub-image bounds", func(t *testing.T) {
		// Sub-image with non-zero Bounds().Min = (10, 20).
		full := image.NewNRGBA(image.Rect(0, 0, 20, 30))
		for y := 20; y < 24; y++ {
			for x := 10; x < 14; x++ {
				full.SetNRGBA(x, y, color.NRGBA{R: 51, G: 102, B: 204, A: 255}) // 0.2, 0.4, 0.8, 1.0
			}
		}
		sub := full.SubImage(image.Rect(10, 20, 14, 24))

		cases := []struct {
			ch   physics3d.HeightmapChannel
			want float32
		}{
			{physics3d.HeightmapRed, 2.0},
			{physics3d.HeightmapGreen, 4.0},
			{physics3d.HeightmapBlue, 8.0},
			{physics3d.HeightmapAlpha, 10.0},
		}
		for _, tc := range cases {
			hf, err := physics3d.NewHeightfieldFromImage(sub, physics3d.HeightfieldImageOptions{
				CellSize:  1.0,
				MaxHeight: 10.0,
				Channel:   tc.ch,
			})
			if err != nil {
				t.Fatalf("channel %d: %v", tc.ch, err)
			}
			y, ok := hf.HeightAt(1.5, 1.5)
			if !ok || !approx(y, tc.want, 0.05) {
				t.Fatalf("channel %d: HeightAt = (%v, %v), want %v", tc.ch, y, ok, tc.want)
			}
		}
	})

	t.Run("bilinear vs nearest resampling and FlipZ", func(t *testing.T) {
		// 2x2 Gray image: (0,0)=0, (1,0)=255, (0,1)=0, (1,1)=255.
		img := image.NewGray(image.Rect(0, 0, 2, 2))
		img.SetGray(0, 0, color.Gray{Y: 0})
		img.SetGray(1, 0, color.Gray{Y: 255})
		img.SetGray(0, 1, color.Gray{Y: 0})
		img.SetGray(1, 1, color.Gray{Y: 255})

		hfBilin, err := physics3d.NewHeightfieldFromImage(img, physics3d.HeightfieldImageOptions{
			Cols:      5,
			Rows:      5,
			CellSize:  1.0,
			MaxHeight: 4.0,
			Bilinear:  true,
		})
		if err != nil {
			t.Fatalf("bilinear: %v", err)
		}
		// Midpoint column cx=2 must interpolate smoothly to 2.0.
		if y, ok := hfBilin.HeightAt(2.0, 2.0); !ok || !approx(y, 2.0, 1e-3) {
			t.Fatalf("bilinear midpoint height = (%v, %v), want 2.0", y, ok)
		}

		// FlipZ check on vertical gradient image: row 0 = 0, row 1 = 255.
		grad := image.NewGray(image.Rect(0, 0, 2, 2))
		grad.SetGray(0, 0, color.Gray{Y: 0})
		grad.SetGray(1, 0, color.Gray{Y: 0})
		grad.SetGray(0, 1, color.Gray{Y: 255})
		grad.SetGray(1, 1, color.Gray{Y: 255})
		hfFlip, err := physics3d.NewHeightfieldFromImage(grad, physics3d.HeightfieldImageOptions{
			CellSize:  1.0,
			MaxHeight: 10.0,
			FlipZ:     true,
		})
		if err != nil {
			t.Fatalf("FlipZ: %v", err)
		}
		yNear, _ := hfFlip.HeightAt(0.5, 0.0)
		yFar, _ := hfFlip.HeightAt(0.5, 1.0)
		if !approx(yNear, 10.0, 1e-3) || !approx(yFar, 0.0, 1e-3) {
			t.Fatalf("FlipZ heights = (z=0: %v, z=1: %v), want (10, 0)", yNear, yFar)
		}
	})

	t.Run("physics collision with dropped body", func(t *testing.T) {
		// Flat elevated plateau at height 3.0 from Gray image.
		img := image.NewGray(image.Rect(0, 0, 9, 9))
		for y := range 9 {
			for x := range 9 {
				img.SetGray(x, y, color.Gray{Y: 153}) // 153/255 = 0.6 -> 0.6 * 5 = 3.0
			}
		}
		hf, err := physics3d.NewHeightfieldFromImage(img, physics3d.HeightfieldImageOptions{
			CellSize:  2.0,
			MaxHeight: 5.0,
			Origin:    physics3d.Vec3{X: -8, Y: 0, Z: -8},
		})
		if err != nil {
			t.Fatalf("NewHeightfieldFromImage: %v", err)
		}

		w := physics3d.NewWorld()
		w.AddHeightfield(hf)

		ball := w.AddBody(physics3d.NewDynamicBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
			physics3d.Vec3{X: 0, Y: 8, Z: 0},
			1.0,
		))
		dt := time.Second / 60
		for range 180 {
			w.Step(dt)
		}
		// Ball radius 0.5 resting on terrain height 3.0 -> center Y ~ 3.5.
		if !approx(ball.Pos.Y, 3.5, 0.05) {
			t.Fatalf("dropped ball settled at Y=%v, want ~3.5", ball.Pos.Y)
		}
	})

	t.Run("validation errors", func(t *testing.T) {
		if _, err := physics3d.NewHeightfieldFromImage(nil, physics3d.HeightfieldImageOptions{}); err == nil {
			t.Fatal("expected error for nil image")
		}
		onePx := image.NewGray(image.Rect(0, 0, 1, 1))
		if _, err := physics3d.NewHeightfieldFromImage(onePx, physics3d.HeightfieldImageOptions{}); err == nil {
			t.Fatal("expected error for 1x1 image without resampling")
		}
		img := image.NewGray(image.Rect(0, 0, 4, 4))
		if _, err := physics3d.NewHeightfieldFromImage(img, physics3d.HeightfieldImageOptions{Channel: physics3d.HeightmapChannel(99)}); err == nil {
			t.Fatal("expected error for invalid channel")
		}
		if _, err := physics3d.NewHeightfieldFromImage(img, physics3d.HeightfieldImageOptions{Cols: 1}); err == nil {
			t.Fatal("expected error for Cols=1")
		}
		if _, err := physics3d.NewHeightfieldFromImage(img, physics3d.HeightfieldImageOptions{CellSize: -1}); err == nil {
			t.Fatal("expected error for negative CellSize")
		}
	})
}
