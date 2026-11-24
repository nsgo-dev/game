package math3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const eps = 1e-4

func TestScalars(t *testing.T) {
	if !math3d.ApproxEqual(math3d.DegToRad(180), math.Pi, eps) {
		t.Fatalf("DegToRad(180) = %v, want pi", math3d.DegToRad(180))
	}
	if !math3d.ApproxEqual(math3d.RadToDeg(math.Pi/2), 90, eps) {
		t.Fatalf("RadToDeg(pi/2) = %v, want 90", math3d.RadToDeg(math.Pi/2))
	}
	if got := math3d.Clamp(-5, 0, 10); got != 0 {
		t.Fatalf("Clamp(-5) = %v, want 0", got)
	}
	if got := math3d.Clamp(15, 0, 10); got != 10 {
		t.Fatalf("Clamp(15) = %v, want 10", got)
	}
	if got := math3d.Clamp(4, 0, 10); got != 4 {
		t.Fatalf("Clamp(4) = %v, want 4", got)
	}
	if !math3d.ApproxEqual(math3d.Lerp(10, 20, 0.25), 12.5, eps) {
		t.Fatalf("Lerp(10, 20, 0.25) = %v, want 12.5", math3d.Lerp(10, 20, 0.25))
	}
}

func TestVec2Operations(t *testing.T) {
	a := math3d.Vec2{X: 3, Y: 4}
	b := math3d.Vec2{X: -1, Y: 2}

	if got := a.Add(b); !got.ApproxEqual(math3d.Vec2{X: 2, Y: 6}, eps) {
		t.Fatalf("Add = %v", got)
	}
	if got := a.Sub(b); !got.ApproxEqual(math3d.Vec2{X: 4, Y: 2}, eps) {
		t.Fatalf("Sub = %v", got)
	}
	if got := a.Mul(b); !got.ApproxEqual(math3d.Vec2{X: -3, Y: 8}, eps) {
		t.Fatalf("Mul = %v", got)
	}
	if got := a.Div(math3d.Vec2{X: 3, Y: 2}); !got.ApproxEqual(math3d.Vec2{X: 1, Y: 2}, eps) {
		t.Fatalf("Div = %v", got)
	}
	if got := a.Scale(2); !got.ApproxEqual(math3d.Vec2{X: 6, Y: 8}, eps) {
		t.Fatalf("Scale = %v", got)
	}
	if got := a.Neg(); !got.ApproxEqual(math3d.Vec2{X: -3, Y: -4}, eps) {
		t.Fatalf("Neg = %v", got)
	}
	if got := a.Dot(b); !math3d.ApproxEqual(got, 5, eps) {
		t.Fatalf("Dot = %v, want 5", got)
	}
	if got := a.LenSq(); !math3d.ApproxEqual(got, 25, eps) {
		t.Fatalf("LenSq = %v, want 25", got)
	}
	if got := a.Len(); !math3d.ApproxEqual(got, 5, eps) {
		t.Fatalf("Len = %v, want 5", got)
	}
	if got := a.Norm(); !got.ApproxEqual(math3d.Vec2{X: 0.6, Y: 0.8}, eps) {
		t.Fatalf("Norm = %v", got)
	}
	if got := math3d.Vec2Zero().Norm(); got != math3d.Vec2Zero() {
		t.Fatalf("zero Norm = %v, want zero", got)
	}
	if got := a.Dist(math3d.Vec2Zero()); !math3d.ApproxEqual(got, 5, eps) {
		t.Fatalf("Dist = %v, want 5", got)
	}
	if got := a.Lerp(b, 0.5); !got.ApproxEqual(math3d.Vec2{X: 1, Y: 3}, eps) {
		t.Fatalf("Lerp = %v", got)
	}
	if got := a.Min(b); !got.ApproxEqual(math3d.Vec2{X: -1, Y: 2}, eps) {
		t.Fatalf("Min = %v", got)
	}
	if got := a.Max(b); !got.ApproxEqual(math3d.Vec2{X: 3, Y: 4}, eps) {
		t.Fatalf("Max = %v", got)
	}
	if got := (math3d.Vec2{X: -5, Y: 10}).Clamp(math3d.Vec2Zero(), math3d.Vec2One()); !got.ApproxEqual(math3d.Vec2{X: 0, Y: 1}, eps) {
		t.Fatalf("Clamp = %v", got)
	}
	if got := a.Vec3(5); got != (math3d.Vec3{X: 3, Y: 4, Z: 5}) {
		t.Fatalf("Vec3 = %v", got)
	}
}

func TestVec3OperationsAndRightHandRule(t *testing.T) {
	// Right-handed basis: X x Y = Z, Y x Z = X, Z x X = Y.
	x, y, z := math3d.Vec3UnitX(), math3d.Vec3UnitY(), math3d.Vec3UnitZ()
	if !x.Cross(y).ApproxEqual(z, eps) {
		t.Fatalf("X x Y = %v, want Z", x.Cross(y))
	}
	if !y.Cross(z).ApproxEqual(x, eps) {
		t.Fatalf("Y x Z = %v, want X", y.Cross(z))
	}
	if !z.Cross(x).ApproxEqual(y, eps) {
		t.Fatalf("Z x X = %v, want Y", z.Cross(x))
	}
	if !math3d.Vec3Right().Cross(math3d.Vec3Up()).ApproxEqual(math3d.Vec3Forward().Neg(), eps) {
		t.Fatalf("Right x Up should be -Forward (+Z)")
	}

	a := math3d.Vec3{X: 1, Y: 2, Z: 2}
	b := math3d.Vec3{X: 4, Y: -1, Z: 3}
	if !a.Add(b).ApproxEqual(math3d.Vec3{X: 5, Y: 1, Z: 5}, eps) {
		t.Fatalf("Add failed")
	}
	if !a.Sub(b).ApproxEqual(math3d.Vec3{X: -3, Y: 3, Z: -1}, eps) {
		t.Fatalf("Sub failed")
	}
	if !a.Mul(b).ApproxEqual(math3d.Vec3{X: 4, Y: -2, Z: 6}, eps) {
		t.Fatalf("Mul failed")
	}
	if !a.Scale(3).ApproxEqual(math3d.Vec3{X: 3, Y: 6, Z: 6}, eps) {
		t.Fatalf("Scale failed")
	}
	if !a.Neg().ApproxEqual(math3d.Vec3{X: -1, Y: -2, Z: -2}, eps) {
		t.Fatalf("Neg failed")
	}
	if !math3d.ApproxEqual(a.Dot(b), 8, eps) {
		t.Fatalf("Dot = %v, want 8", a.Dot(b))
	}
	if !math3d.ApproxEqual(a.Len(), 3, eps) {
		t.Fatalf("Len = %v, want 3", a.Len())
	}
	if !math3d.ApproxEqual(a.Norm().Len(), 1, eps) {
		t.Fatalf("Norm Len = %v, want 1", a.Norm().Len())
	}
	if math3d.Vec3Zero().Norm() != math3d.Vec3Zero() {
		t.Fatalf("zero Vec3 Norm must be zero")
	}
	if !math3d.ApproxEqual(a.Dist(math3d.Vec3Zero()), 3, eps) {
		t.Fatalf("Dist = %v, want 3", a.Dist(math3d.Vec3Zero()))
	}
	if !a.Lerp(b, 0.5).ApproxEqual(math3d.Vec3{X: 2.5, Y: 0.5, Z: 2.5}, eps) {
		t.Fatalf("Lerp = %v", a.Lerp(b, 0.5))
	}
	if !a.Min(b).ApproxEqual(math3d.Vec3{X: 1, Y: -1, Z: 2}, eps) {
		t.Fatalf("Min = %v", a.Min(b))
	}
	if !a.Max(b).ApproxEqual(math3d.Vec3{X: 4, Y: 2, Z: 3}, eps) {
		t.Fatalf("Max = %v", a.Max(b))
	}
	if !(math3d.Vec3{X: -1, Y: 0.5, Z: 5}).Clamp(math3d.Vec3Zero(), math3d.Vec3One()).ApproxEqual(math3d.Vec3{X: 0, Y: 0.5, Z: 1}, eps) {
		t.Fatalf("Clamp failed")
	}
	if a.XY() != (math3d.Vec2{X: 1, Y: 2}) || a.Vec4(1) != (math3d.Vec4{X: 1, Y: 2, Z: 2, W: 1}) {
		t.Fatalf("XY/Vec4 conversion failed")
	}
}

func TestVec4Operations(t *testing.T) {
	a := math3d.Vec4{X: 1, Y: 2, Z: 2, W: 4}
	b := math3d.Vec4{X: 3, Y: 0, Z: -2, W: 2}
	if !a.Add(b).ApproxEqual(math3d.Vec4{X: 4, Y: 2, Z: 0, W: 6}, eps) {
		t.Fatalf("Add failed")
	}
	if !a.Sub(b).ApproxEqual(math3d.Vec4{X: -2, Y: 2, Z: 4, W: 2}, eps) {
		t.Fatalf("Sub failed")
	}
	if !a.Mul(b).ApproxEqual(math3d.Vec4{X: 3, Y: 0, Z: -4, W: 8}, eps) {
		t.Fatalf("Mul failed")
	}
	if !a.Scale(0.5).ApproxEqual(math3d.Vec4{X: 0.5, Y: 1, Z: 1, W: 2}, eps) {
		t.Fatalf("Scale failed")
	}
	if !a.Neg().ApproxEqual(math3d.Vec4{X: -1, Y: -2, Z: -2, W: -4}, eps) {
		t.Fatalf("Neg failed")
	}
	if !math3d.ApproxEqual(a.Dot(b), 7, eps) {
		t.Fatalf("Dot = %v, want 7", a.Dot(b))
	}
	if !math3d.ApproxEqual(a.LenSq(), 25, eps) || !math3d.ApproxEqual(a.Len(), 5, eps) {
		t.Fatalf("Len/LenSq failed")
	}
	if !math3d.ApproxEqual(a.Norm().Len(), 1, eps) {
		t.Fatalf("Norm failed")
	}
	if math3d.Vec4Zero().Norm() != math3d.Vec4Zero() {
		t.Fatalf("zero Vec4 Norm must be zero")
	}
	if !math3d.ApproxEqual(a.Dist(math3d.Vec4Zero()), 5, eps) {
		t.Fatalf("Dist failed")
	}
	if !a.Lerp(b, 0.5).ApproxEqual(math3d.Vec4{X: 2, Y: 1, Z: 0, W: 3}, eps) {
		t.Fatalf("Lerp failed")
	}
	if !a.Min(b).ApproxEqual(math3d.Vec4{X: 1, Y: 0, Z: -2, W: 2}, eps) {
		t.Fatalf("Min failed")
	}
	if !a.Max(b).ApproxEqual(math3d.Vec4{X: 3, Y: 2, Z: 2, W: 4}, eps) {
		t.Fatalf("Max failed")
	}
	if !a.Clamp(math3d.Vec4Zero(), math3d.Vec4One()).ApproxEqual(math3d.Vec4One(), eps) {
		t.Fatalf("Clamp failed")
	}
	if a.XYZ() != (math3d.Vec3{X: 1, Y: 2, Z: 2}) {
		t.Fatalf("XYZ failed")
	}
}

func TestMat3AlgebraAndInverse(t *testing.T) {
	m := math3d.Mat3FromCols(
		math3d.Vec3{X: 2, Y: 1, Z: 0},
		math3d.Vec3{X: -1, Y: 3, Z: 2},
		math3d.Vec3{X: 4, Y: 0, Z: 5},
	)
	if m.At(1, 0) != 1 || m.Col(1) != (math3d.Vec3{X: -1, Y: 3, Z: 2}) || m.Row(2) != (math3d.Vec3{X: 0, Y: 2, Z: 5}) {
		t.Fatalf("column-major accessors mismatch")
	}
	if !m.Transpose().Transpose().ApproxEqual(m, eps) {
		t.Fatalf("double transpose failed")
	}
	inv, ok := m.Inverse()
	if !ok {
		t.Fatalf("expected invertible Mat3")
	}
	if !m.Mul(inv).ApproxEqual(math3d.Mat3Identity(), eps) {
		t.Fatalf("M * M^-1 != I: %v", m.Mul(inv))
	}
	if !inv.Mul(m).ApproxEqual(math3d.Mat3Identity(), eps) {
		t.Fatalf("M^-1 * M != I: %v", inv.Mul(m))
	}
	if !math3d.ApproxEqual(m.Det()*inv.Det(), 1, eps) {
		t.Fatalf("det(M)*det(M^-1) = %v, want 1", m.Det()*inv.Det())
	}

	// Singular matrix.
	singular := math3d.Mat3FromCols(
		math3d.Vec3{X: 1, Y: 2, Z: 3},
		math3d.Vec3{X: 2, Y: 4, Z: 6},
		math3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	if _, ok := singular.Inverse(); ok {
		t.Fatalf("expected singular Mat3 Inverse to return false")
	}

	// NormalMatrix preserves normal perpendicularity under non-uniform scale + rotation.
	model := math3d.TRS(
		math3d.Vec3{X: 5, Y: -2, Z: 1},
		math3d.QuatFromAxisAngle(math3d.Vec3{X: 1, Y: 2, Z: -1}, 0.7),
		math3d.Vec3{X: 3, Y: 0.5, Z: 2},
	)
	nm, ok := math3d.NormalMatrix(model)
	if !ok {
		t.Fatalf("NormalMatrix failed")
	}
	n := math3d.Vec3{X: 1, Y: 1, Z: 0}.Norm()
	tangent := math3d.Vec3{X: -1, Y: 1, Z: 0}.Norm() // perpendicular to n
	worldN := nm.MulVec3(n).Norm()
	worldT := model.TransformDir(tangent).Norm()
	if !math3d.ApproxEqual(worldN.Dot(worldT), 0, eps) {
		t.Fatalf("transformed normal not perpendicular to tangent: dot=%v", worldN.Dot(worldT))
	}
}

func TestMat4InverseAndTransforms(t *testing.T) {
	// Test M * M^-1 == I across affine, perspective, and orthographic matrices.
	mats := []struct {
		name string
		m    math3d.Mat4
	}{
		{"identity", math3d.Mat4Identity()},
		{"TRS", math3d.TRS(
			math3d.Vec3{X: 3, Y: -4, Z: 5},
			math3d.QuatFromEuler(0.4, -1.1, 0.25),
			math3d.Vec3{X: 2, Y: 0.5, Z: 3},
		)},
		{"lookAt", math3d.LookAt(math3d.Vec3{X: 4, Y: 3, Z: 8}, math3d.Vec3{X: 1, Y: 0, Z: -2}, math3d.Vec3Up())},
		{"perspectiveZO", math3d.PerspectiveZO(math3d.DegToRad(60), 16.0/9.0, 0.1, 100)},
		{"perspectiveNO", math3d.PerspectiveNO(math3d.DegToRad(60), 16.0/9.0, 0.1, 100)},
		{"orthoZO", math3d.OrthoZO(-10, 12, -5, 8, 0.5, 50)},
		{"orthoNO", math3d.OrthoNO(-10, 12, -5, 8, 0.5, 50)},
	}

	for _, tc := range mats {
		t.Run(tc.name, func(t *testing.T) {
			inv, ok := tc.m.Inverse()
			if !ok {
				t.Fatalf("%s: expected invertible matrix", tc.name)
			}
			if !tc.m.Mul(inv).ApproxEqual(math3d.Mat4Identity(), 1e-3) {
				t.Fatalf("%s: M * M^-1 != I: %v", tc.name, tc.m.Mul(inv))
			}
			if !inv.Mul(tc.m).ApproxEqual(math3d.Mat4Identity(), 1e-3) {
				t.Fatalf("%s: M^-1 * M != I: %v", tc.name, inv.Mul(tc.m))
			}
			if !math3d.ApproxEqual(tc.m.Det()*inv.Det(), 1, 1e-3) {
				t.Fatalf("%s: det(M)*det(M^-1) = %v, want 1", tc.name, tc.m.Det()*inv.Det())
			}
		})
	}

	// Singular matrix returns false.
	if _, ok := (math3d.Mat4{}).Inverse(); ok {
		t.Fatalf("zero Mat4 should not be invertible")
	}

	// Verify right-handed rotations.
	halfPi := float32(math.Pi / 2)
	if got := math3d.RotateX(halfPi).TransformDir(math3d.Vec3Up()); !got.ApproxEqual(math3d.Vec3UnitZ(), eps) {
		t.Fatalf("RotateX(+90) * +Y = %v, want +Z", got)
	}
	if got := math3d.RotateY(halfPi).TransformDir(math3d.Vec3Forward()); !got.ApproxEqual(math3d.Vec3Right().Neg(), eps) {
		t.Fatalf("RotateY(+90) * -Z = %v, want -X", got)
	}
	if got := math3d.RotateZ(halfPi).TransformDir(math3d.Vec3Right()); !got.ApproxEqual(math3d.Vec3Up(), eps) {
		t.Fatalf("RotateZ(+90) * +X = %v, want +Y", got)
	}
	if !math3d.RotateAxis(math3d.Vec3UnitX(), 0.7).ApproxEqual(math3d.RotateX(0.7), eps) {
		t.Fatalf("RotateAxis(X) != RotateX")
	}
	if !math3d.RotateAxis(math3d.Vec3UnitY(), -1.2).ApproxEqual(math3d.RotateY(-1.2), eps) {
		t.Fatalf("RotateAxis(Y) != RotateY")
	}
	if !math3d.RotateAxis(math3d.Vec3UnitZ(), 2.3).ApproxEqual(math3d.RotateZ(2.3), eps) {
		t.Fatalf("RotateAxis(Z) != RotateZ")
	}
	if !math3d.RotateAxis(math3d.Vec3Zero(), 1.0).ApproxEqual(math3d.Mat4Identity(), eps) {
		t.Fatalf("RotateAxis(zero) should be identity")
	}

	// TransformPoint applies translation; TransformDir ignores translation.
	tr := math3d.Translate(math3d.Vec3{X: 10, Y: 20, Z: 30})
	if !tr.TransformPoint(math3d.Vec3{X: 1, Y: 2, Z: 3}).ApproxEqual(math3d.Vec3{X: 11, Y: 22, Z: 33}, eps) {
		t.Fatalf("TransformPoint with Translate failed")
	}
	if !tr.TransformDir(math3d.Vec3{X: 1, Y: 2, Z: 3}).ApproxEqual(math3d.Vec3{X: 1, Y: 2, Z: 3}, eps) {
		t.Fatalf("TransformDir should ignore translation")
	}
}

func TestCameraAndProjections(t *testing.T) {
	eye := math3d.Vec3{X: 0, Y: 2, Z: 5}
	target := math3d.Vec3{X: 0, Y: 2, Z: 0}
	v := math3d.LookAt(eye, target, math3d.Vec3Up())

	// Camera at eye looks down -Z in view space.
	if got := v.TransformPoint(eye); !got.ApproxEqual(math3d.Vec3Zero(), eps) {
		t.Fatalf("LookAt(eye) = %v, want 0", got)
	}
	if got := v.TransformPoint(target); !got.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: -5}, eps) {
		t.Fatalf("LookAt(target) = %v, want (0,0,-5)", got)
	}
	if got := v.TransformPoint(math3d.Vec3{X: 3, Y: 2, Z: 0}); !got.ApproxEqual(math3d.Vec3{X: 3, Y: 0, Z: -5}, eps) {
		t.Fatalf("LookAt(right) = %v, want (3,0,-5)", got)
	}

	// PerspectiveZO (default): near -> z=0, far -> z=1.
	pZO := math3d.Perspective(math3d.DegToRad(90), 2.0, 1.0, 100.0)
	if got := pZO.TransformPoint(math3d.Vec3{X: 0, Y: 0, Z: -1}); !got.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: 0}, eps) {
		t.Fatalf("PerspectiveZO near = %v, want (0,0,0)", got)
	}
	if got := pZO.TransformPoint(math3d.Vec3{X: 0, Y: 0, Z: -100}); !got.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: 1}, eps) {
		t.Fatalf("PerspectiveZO far = %v, want (0,0,1)", got)
	}
	// At fovY=90 deg, aspect=2, at z=-1: top is y=1 -> ndc y=1, right is x=2 -> ndc x=1.
	if got := pZO.TransformPoint(math3d.Vec3{X: 2, Y: 1, Z: -1}); !got.ApproxEqual(math3d.Vec3{X: 1, Y: 1, Z: 0}, eps) {
		t.Fatalf("PerspectiveZO top-right near corner = %v, want (1,1,0)", got)
	}

	// PerspectiveNO: near -> z=-1, far -> z=1.
	pNO := math3d.PerspectiveNO(math3d.DegToRad(90), 2.0, 1.0, 100.0)
	if got := pNO.TransformPoint(math3d.Vec3{X: 0, Y: 0, Z: -1}); !got.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: -1}, eps) {
		t.Fatalf("PerspectiveNO near = %v, want (0,0,-1)", got)
	}
	if got := pNO.TransformPoint(math3d.Vec3{X: 0, Y: 0, Z: -100}); !got.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: 1}, eps) {
		t.Fatalf("PerspectiveNO far = %v, want (0,0,1)", got)
	}

	// OrthoZO (default): [-10,10] x [-5,5] x [-50,-1] -> [-1,1] x [-1,1] x [0,1].
	oZO := math3d.Ortho(-10, 10, -5, 5, 1, 50)
	if got := oZO.TransformPoint(math3d.Vec3{X: -10, Y: -5, Z: -1}); !got.ApproxEqual(math3d.Vec3{X: -1, Y: -1, Z: 0}, eps) {
		t.Fatalf("OrthoZO min-near = %v, want (-1,-1,0)", got)
	}
	if got := oZO.TransformPoint(math3d.Vec3{X: 10, Y: 5, Z: -50}); !got.ApproxEqual(math3d.Vec3{X: 1, Y: 1, Z: 1}, eps) {
		t.Fatalf("OrthoZO max-far = %v, want (1,1,1)", got)
	}

	// OrthoNO: near -> z=-1, far -> z=1.
	oNO := math3d.OrthoNO(-10, 10, -5, 5, 1, 50)
	if got := oNO.TransformPoint(math3d.Vec3{X: 0, Y: 0, Z: -1}); !got.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: -1}, eps) {
		t.Fatalf("OrthoNO near = %v, want (0,0,-1)", got)
	}
	if got := oNO.TransformPoint(math3d.Vec3{X: 0, Y: 0, Z: -50}); !got.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: 1}, eps) {
		t.Fatalf("OrthoNO far = %v, want (0,0,1)", got)
	}
}

func TestTRSAndDecomposeRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		pos   math3d.Vec3
		rot   math3d.Quat
		scale math3d.Vec3
	}{
		{"identity", math3d.Vec3Zero(), math3d.QuatIdentity(), math3d.Vec3One()},
		{"general", math3d.Vec3{X: 5, Y: -3, Z: 12}, math3d.QuatFromEuler(0.3, -1.2, 0.8), math3d.Vec3{X: 2, Y: 0.5, Z: 4}},
		{"180deg", math3d.Vec3{X: -1, Y: 2, Z: 3}, math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi), math3d.Vec3{X: 1, Y: 1, Z: 1}},
		{"reflectionX", math3d.Vec3{X: 1, Y: 2, Z: 3}, math3d.QuatFromAxisAngle(math3d.Vec3{X: 1, Y: 1, Z: 1}, 1.0), math3d.Vec3{X: -2, Y: 3, Z: 4}},
		{"reflectionY", math3d.Vec3{X: 1, Y: 2, Z: 3}, math3d.QuatFromEuler(0.5, 0.7, -0.3), math3d.Vec3{X: 2, Y: -3, Z: 4}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := math3d.TRS(tc.pos, tc.rot, tc.scale)
			// Verify TRS equals explicit T * R * S.
			explicit := math3d.Translate(tc.pos).Mul(tc.rot.ToMat4()).Mul(math3d.Scale(tc.scale))
			if !m.ApproxEqual(explicit, eps) {
				t.Fatalf("TRS != T*R*S:\ngot:  %v\nwant: %v", m, explicit)
			}

			pos, rot, scale, ok := m.Decompose()
			if !ok {
				t.Fatalf("Decompose returned ok=false")
			}
			rebuilt := math3d.TRS(pos, rot, scale)
			if !rebuilt.ApproxEqual(m, eps) {
				t.Fatalf("TRS(Decompose(m)) != m:\ngot:  %v\nwant: %v", rebuilt, m)
			}
			if !pos.ApproxEqual(tc.pos, eps) {
				t.Fatalf("pos = %v, want %v", pos, tc.pos)
			}
			// When scale is positive (no reflection ambiguity), rot and scale match directly.
			if tc.scale.X > 0 && tc.scale.Y > 0 && tc.scale.Z > 0 {
				if !rot.ApproxEqual(tc.rot, eps) {
					t.Fatalf("rot = %v, want %v", rot, tc.rot)
				}
				if !scale.ApproxEqual(tc.scale, eps) {
					t.Fatalf("scale = %v, want %v", scale, tc.scale)
				}
			}
		})
	}

	// Zero-scale and projective matrices fail Decompose cleanly.
	if _, _, _, ok := math3d.TRS(math3d.Vec3Zero(), math3d.QuatIdentity(), math3d.Vec3{X: 1, Y: 0, Z: 1}).Decompose(); ok {
		t.Fatalf("expected zero-scale Decompose to return false")
	}
	if _, _, _, ok := math3d.Perspective(math3d.DegToRad(60), 1, 0.1, 100).Decompose(); ok {
		t.Fatalf("expected projective Decompose to return false")
	}
}

func TestQuatEquivalenceEulerSlerpAndLookRotation(t *testing.T) {
	// Axis-angle <-> Mat4 <-> QuatFromMat4 across all Shepperd branches (0, 90, 180 deg).
	rotations := []struct {
		axis  math3d.Vec3
		angle float32
	}{
		{math3d.Vec3UnitX(), 0},
		{math3d.Vec3UnitX(), math.Pi / 2},
		{math3d.Vec3UnitX(), math.Pi},
		{math3d.Vec3UnitY(), math.Pi},
		{math3d.Vec3UnitZ(), math.Pi},
		{math3d.Vec3{X: 1, Y: 2, Z: -3}, 1.234},
		{math3d.Vec3{X: -2, Y: 0.5, Z: 1}, 2.9},
	}
	v := math3d.Vec3{X: 1.5, Y: -2.0, Z: 3.25}
	for _, r := range rotations {
		q := math3d.QuatFromAxisAngle(r.axis, r.angle)
		mAxis := math3d.RotateAxis(r.axis, r.angle)
		mQuat := q.ToMat4()
		if !mQuat.ApproxEqual(mAxis, eps) {
			t.Fatalf("q.ToMat4() != RotateAxis(%v, %v)", r.axis, r.angle)
		}
		if !q.RotateVec3(v).ApproxEqual(mAxis.TransformDir(v), eps) {
			t.Fatalf("q.RotateVec3 != m.TransformDir for axis=%v angle=%v", r.axis, r.angle)
		}
		qBack := math3d.QuatFromMat4(mQuat)
		if !qBack.ApproxEqual(q, eps) {
			t.Fatalf("QuatFromMat4(q.ToMat4()) = %v, want %v", qBack, q)
		}
		// q * q^-1 == identity.
		if !q.Mul(q.Inverse()).ApproxEqual(math3d.QuatIdentity(), eps) {
			t.Fatalf("q * q^-1 != identity")
		}
	}

	// Composition equivalence: (q1 * q2).ToMat4() == q1.ToMat4() * q2.ToMat4().
	q1 := math3d.QuatFromAxisAngle(math3d.Vec3{X: 1, Y: 0, Z: 1}, 0.8)
	q2 := math3d.QuatFromAxisAngle(math3d.Vec3{X: 0, Y: 1, Z: -1}, -1.3)
	if !q1.Mul(q2).ToMat4().ApproxEqual(q1.ToMat4().Mul(q2.ToMat4()), eps) {
		t.Fatalf("quaternion composition does not match matrix composition")
	}
	if !q1.Mul(q2).RotateVec3(v).ApproxEqual(q1.RotateVec3(q2.RotateVec3(v)), eps) {
		t.Fatalf("(q1*q2).RotateVec3(v) != q1.RotateVec3(q2.RotateVec3(v))")
	}

	// Euler YXZ round-trip and matrix equivalence (including gimbal lock).
	eulerCases := [][3]float32{
		{0.3, -0.7, 0.5},
		{-0.9, 2.1, -1.4},
		{math.Pi / 2, 0.6, 0.0},
		{-math.Pi / 2, -0.8, 0.0},
	}
	for _, e := range eulerCases {
		q := math3d.QuatFromEuler(e[0], e[1], e[2])
		m := math3d.RotateY(e[1]).Mul(math3d.RotateX(e[0])).Mul(math3d.RotateZ(e[2]))
		if !q.ToMat4().ApproxEqual(m, eps) {
			t.Fatalf("QuatFromEuler(%v) != Ry*Rx*Rz", e)
		}
		p, y, r := q.ToEuler()
		q2 := math3d.QuatFromEuler(p, y, r)
		if !q2.ApproxEqual(q, 1e-3) {
			t.Fatalf("Euler YXZ round-trip mismatch for %v: got %v want %v", e, q2, q)
		}
	}

	// Euler XYZ round-trip and matrix equivalence.
	for _, e := range [][3]float32{{0.4, -0.5, 1.1}, {0.2, math.Pi / 2, 0.0}} {
		q := math3d.QuatFromEulerXYZ(e[0], e[1], e[2])
		m := math3d.RotateX(e[0]).Mul(math3d.RotateY(e[1])).Mul(math3d.RotateZ(e[2]))
		if !q.ToMat4().ApproxEqual(m, eps) {
			t.Fatalf("QuatFromEulerXYZ(%v) != Rx*Ry*Rz", e)
		}
		x, y, z := q.ToEulerXYZ()
		if !math3d.QuatFromEulerXYZ(x, y, z).ApproxEqual(q, 1e-3) {
			t.Fatalf("Euler XYZ round-trip mismatch for %v", e)
		}
	}

	// Slerp shortest-path hemisphere check:
	// a = 0 deg, b = +60 deg around Y, negatedB = -b (represents the same +60 deg rotation).
	// Shortest path midpoint MUST be +30 deg, NOT -150 deg!
	a := math3d.QuatIdentity()
	b := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(60))
	negB := math3d.Quat{X: -b.X, Y: -b.Y, Z: -b.Z, W: -b.W}
	wantMid := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(30))

	if got := a.Slerp(b, 0.5); !got.ApproxEqual(wantMid, eps) {
		t.Fatalf("Slerp(a, b, 0.5) = %v, want +30deg (%v)", got, wantMid)
	}
	if got := a.Slerp(negB, 0.5); !got.ApproxEqual(wantMid, eps) {
		t.Fatalf("Slerp shortest-path failed for negated target: got %v, want +30deg (%v)", got, wantMid)
	}
	if got := a.Nlerp(negB, 0.5); !got.ApproxEqual(wantMid, 1e-3) {
		t.Fatalf("Nlerp shortest-path failed for negated target: got %v, want %v", got, wantMid)
	}

	// LookRotation aligns local -Z with forward and is the inverse orientation of LookAt.
	eye := math3d.Vec3{X: 3, Y: 4, Z: 7}
	target := math3d.Vec3{X: -2, Y: 1, Z: -5}
	fwd := target.Sub(eye).Norm()
	lr := math3d.LookRotation(fwd, math3d.Vec3Up())
	if got := lr.RotateVec3(math3d.Vec3Forward()); !got.ApproxEqual(fwd, eps) {
		t.Fatalf("LookRotation(-Z) = %v, want %v", got, fwd)
	}
	camWorld := math3d.Transform{Pos: eye, Rot: lr, Scale: math3d.Vec3One()}.ToMat4()
	camView, ok := camWorld.Inverse()
	if !ok {
		t.Fatalf("camera world matrix not invertible")
	}
	lookAt := math3d.LookAt(eye, target, math3d.Vec3Up())
	if !camView.ApproxEqual(lookAt, eps) {
		t.Fatalf("Inverse(LookRotation camera) != LookAt:\ngot:  %v\nwant: %v", camView, lookAt)
	}
}

func TestTransformHierarchyAndOperations(t *testing.T) {
	parent := math3d.Transform{
		Pos:   math3d.Vec3{X: 10, Y: 2, Z: -5},
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90)),
		Scale: math3d.Vec3{X: 2, Y: 2, Z: 2},
	}
	child := math3d.Transform{
		Pos:   math3d.Vec3{X: 1, Y: 0, Z: -3},
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3Right(), math3d.DegToRad(45)),
		Scale: math3d.Vec3{X: 0.5, Y: 1.5, Z: 2},
	}

	combined := parent.Mul(child)
	combinedMat := parent.ToMat4().Mul(child.ToMat4())
	if !combined.ToMat4().ApproxEqual(combinedMat, eps) {
		t.Fatalf("parent.Mul(child).ToMat4() != parent.ToMat4() * child.ToMat4()")
	}

	p := math3d.Vec3{X: 2, Y: -1, Z: 4}
	if !combined.TransformPoint(p).ApproxEqual(combinedMat.TransformPoint(p), eps) {
		t.Fatalf("TransformPoint mismatch with Mat4")
	}

	// Inverse round-trip for uniform-scale transform.
	inv := parent.Inverse()
	if !parent.Mul(inv).ApproxEqual(math3d.TransformIdentity(), eps) {
		t.Fatalf("parent * parent^-1 != identity: %v", parent.Mul(inv))
	}
	if !inv.TransformPoint(parent.TransformPoint(p)).ApproxEqual(p, eps) {
		t.Fatalf("inv.TransformPoint(parent.TransformPoint(p)) != p")
	}

	// Lerp midpoint.
	t0 := math3d.TransformIdentity()
	t1 := math3d.Transform{
		Pos:   math3d.Vec3{X: 10, Y: 0, Z: 0},
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90)),
		Scale: math3d.Vec3{X: 3, Y: 3, Z: 3},
	}
	mid := t0.Lerp(t1, 0.5)
	if !mid.Pos.ApproxEqual(math3d.Vec3{X: 5, Y: 0, Z: 0}, eps) ||
		!mid.Rot.ApproxEqual(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(45)), eps) ||
		!mid.Scale.ApproxEqual(math3d.Vec3{X: 2, Y: 2, Z: 2}, eps) {
		t.Fatalf("Transform.Lerp midpoint mismatch: %v", mid)
	}
}

func TestAABBAndSphere(t *testing.T) {
	empty := math3d.AABBEmpty()
	if !empty.Empty() {
		t.Fatalf("AABBEmpty should be empty")
	}
	if empty.Contains(math3d.Vec3Zero()) {
		t.Fatalf("empty AABB should not contain origin")
	}

	b := empty.ExpandPoint(math3d.Vec3{X: -1, Y: 0, Z: 2}).ExpandPoint(math3d.Vec3{X: 3, Y: 4, Z: 6})
	if b.Empty() {
		t.Fatalf("expanded AABB should not be empty")
	}
	if !b.Center().ApproxEqual(math3d.Vec3{X: 1, Y: 2, Z: 4}, eps) {
		t.Fatalf("Center = %v", b.Center())
	}
	if !b.Size().ApproxEqual(math3d.Vec3{X: 4, Y: 4, Z: 4}, eps) {
		t.Fatalf("Size = %v", b.Size())
	}
	if !b.HalfExtents().ApproxEqual(math3d.Vec3{X: 2, Y: 2, Z: 2}, eps) {
		t.Fatalf("HalfExtents = %v", b.HalfExtents())
	}
	if !b.Contains(math3d.Vec3{X: 1, Y: 2, Z: 4}) || b.Contains(math3d.Vec3{X: 4, Y: 2, Z: 4}) {
		t.Fatalf("AABB.Contains failed")
	}

	b2 := math3d.AABB{Min: math3d.Vec3{X: 2, Y: 3, Z: 5}, Max: math3d.Vec3{X: 5, Y: 6, Z: 7}}
	if !b.Intersects(b2) {
		t.Fatalf("expected overlapping AABBs to intersect")
	}
	u := b.Union(b2)
	if !u.Min.ApproxEqual(math3d.Vec3{X: -1, Y: 0, Z: 2}, eps) || !u.Max.ApproxEqual(math3d.Vec3{X: 5, Y: 6, Z: 7}, eps) {
		t.Fatalf("Union = %v", u)
	}
	if b.Intersects(math3d.AABB{Min: math3d.Vec3{X: 10, Y: 10, Z: 10}, Max: math3d.Vec3{X: 12, Y: 12, Z: 12}}) {
		t.Fatalf("disjoint AABBs should not intersect")
	}

	// Tight transformed AABB verified against transforming all 8 corners.
	m := math3d.TRS(
		math3d.Vec3{X: 4, Y: -2, Z: 1},
		math3d.QuatFromEuler(0.4, 0.7, -0.3),
		math3d.Vec3{X: 2, Y: -1.5, Z: 0.5},
	)
	got := b.Transform(m)
	want := math3d.AABBEmpty()
	for _, x := range []float32{b.Min.X, b.Max.X} {
		for _, y := range []float32{b.Min.Y, b.Max.Y} {
			for _, z := range []float32{b.Min.Z, b.Max.Z} {
				want = want.ExpandPoint(m.TransformPoint(math3d.Vec3{X: x, Y: y, Z: z}))
			}
		}
	}
	if !got.Min.ApproxEqual(want.Min, eps) || !got.Max.ApproxEqual(want.Max, eps) {
		t.Fatalf("AABB.Transform = %v, want %v", got, want)
	}

	// Sphere tests.
	s1 := math3d.Sphere{Center: math3d.Vec3{X: 0, Y: 0, Z: 0}, Radius: 2}
	s2 := math3d.Sphere{Center: math3d.Vec3{X: 3, Y: 0, Z: 0}, Radius: 1.5}
	s3 := math3d.Sphere{Center: math3d.Vec3{X: 5, Y: 0, Z: 0}, Radius: 1}
	if !s1.Contains(math3d.Vec3{X: 1, Y: 1, Z: 1}) || s1.Contains(math3d.Vec3{X: 2, Y: 1, Z: 0}) {
		t.Fatalf("Sphere.Contains failed")
	}
	if !s1.IntersectsSphere(s2) || s1.IntersectsSphere(s3) {
		t.Fatalf("Sphere.IntersectsSphere failed")
	}
	if !s1.IntersectsAABB(math3d.AABB{Min: math3d.Vec3{X: 1.5, Y: 0, Z: 0}, Max: math3d.Vec3{X: 4, Y: 2, Z: 2}}) {
		t.Fatalf("expected sphere-AABB hit")
	}
	if s1.IntersectsAABB(math3d.AABB{Min: math3d.Vec3{X: 2.1, Y: 0, Z: 0}, Max: math3d.Vec3{X: 4, Y: 2, Z: 2}}) {
		t.Fatalf("expected sphere-AABB miss")
	}
}

func TestRayIntersections(t *testing.T) {
	// Ray-AABB slab test.
	box := math3d.AABB{Min: math3d.Vec3{X: -1, Y: -1, Z: -6}, Max: math3d.Vec3{X: 1, Y: 1, Z: -4}}
	ray := math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3Forward()}
	tMin, tMax, hit := ray.IntersectAABB(box)
	if !hit || !math3d.ApproxEqual(tMin, 4, eps) || !math3d.ApproxEqual(tMax, 6, eps) {
		t.Fatalf("Ray.IntersectAABB = (%v, %v, %v), want (4, 6, true)", tMin, tMax, hit)
	}
	// Ray origin inside box.
	inRay := math3d.Ray{Origin: math3d.Vec3{X: 0, Y: 0, Z: -5}, Dir: math3d.Vec3Forward()}
	tMin, tMax, hit = inRay.IntersectAABB(box)
	if !hit || !math3d.ApproxEqual(tMin, 0, eps) || !math3d.ApproxEqual(tMax, 1, eps) {
		t.Fatalf("inside Ray.IntersectAABB = (%v, %v, %v), want (0, 1, true)", tMin, tMax, hit)
	}
	// Ray pointing away from box.
	awayRay := math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3UnitZ()}
	if _, _, hit := awayRay.IntersectAABB(box); hit {
		t.Fatalf("expected ray pointing away to miss box")
	}
	// Ray missing parallel to slab.
	missRay := math3d.Ray{Origin: math3d.Vec3{X: 5, Y: 0, Z: 0}, Dir: math3d.Vec3Forward()}
	if _, _, hit := missRay.IntersectAABB(box); hit {
		t.Fatalf("expected parallel offset ray to miss box")
	}

	// Ray-Sphere test.
	sphere := math3d.Sphere{Center: math3d.Vec3{X: 0, Y: 0, Z: -5}, Radius: 1}
	ts, hit := ray.IntersectSphere(sphere)
	if !hit || !math3d.ApproxEqual(ts, 4, eps) {
		t.Fatalf("Ray.IntersectSphere = (%v, %v), want (4, true)", ts, hit)
	}
	if _, hit := awayRay.IntersectSphere(sphere); hit {
		t.Fatalf("expected ray pointing away to miss sphere")
	}
	// Origin inside sphere hits exit surface at t=1.
	ts, hit = inRay.IntersectSphere(sphere)
	if !hit || !math3d.ApproxEqual(ts, 1, eps) {
		t.Fatalf("inside Ray.IntersectSphere = (%v, %v), want (1, true)", ts, hit)
	}

	// Ray-Plane test.
	plane := math3d.PlaneFromPointNormal(math3d.Vec3{X: 0, Y: 0, Z: -5}, math3d.Vec3UnitZ())
	if !math3d.ApproxEqual(plane.SignedDist(math3d.Vec3Zero()), 5, eps) {
		t.Fatalf("Plane.SignedDist = %v, want 5", plane.SignedDist(math3d.Vec3Zero()))
	}
	tp, hit := ray.IntersectPlane(plane)
	if !hit || !math3d.ApproxEqual(tp, 5, eps) {
		t.Fatalf("Ray.IntersectPlane = (%v, %v), want (5, true)", tp, hit)
	}
	// Parallel to plane.
	parRay := math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3Right()}
	if _, hit := parRay.IntersectPlane(plane); hit {
		t.Fatalf("expected parallel ray to miss plane")
	}
	// Behind ray.
	if _, hit := awayRay.IntersectPlane(plane); hit {
		t.Fatalf("expected ray pointing away to miss plane")
	}

	// Ray-Triangle (Möller-Trumbore).
	v0 := math3d.Vec3{X: -1, Y: -1, Z: -5}
	v1 := math3d.Vec3{X: 1, Y: -1, Z: -5}
	v2 := math3d.Vec3{X: 0, Y: 1, Z: -5}
	triRay := math3d.Ray{Origin: math3d.Vec3{X: 0.25, Y: 0, Z: 0}, Dir: math3d.Vec3Forward()}
	tt, u, v, hit := triRay.IntersectTriangle(v0, v1, v2)
	if !hit || !math3d.ApproxEqual(tt, 5, eps) {
		t.Fatalf("Ray.IntersectTriangle = (t=%v, u=%v, v=%v, hit=%v)", tt, u, v, hit)
	}
	baryPoint := v0.Scale(1 - u - v).Add(v1.Scale(u)).Add(v2.Scale(v))
	if !triRay.At(tt).ApproxEqual(baryPoint, eps) {
		t.Fatalf("barycentric point %v != ray.At(t) %v", baryPoint, triRay.At(tt))
	}
	// Miss outside triangle.
	outTriRay := math3d.Ray{Origin: math3d.Vec3{X: 2, Y: 0, Z: 0}, Dir: math3d.Vec3Forward()}
	if _, _, _, hit := outTriRay.IntersectTriangle(v0, v1, v2); hit {
		t.Fatalf("expected ray outside triangle to miss")
	}
	// Triangle behind ray.
	if _, _, _, hit := awayRay.IntersectTriangle(v0, v1, v2); hit {
		t.Fatalf("expected ray pointing away to miss triangle")
	}
}

func TestFrustumCullingPerspectiveAndOrtho(t *testing.T) {
	// Camera at (0, 2, 10) looking at (0, 2, 0): view forward is -Z in world space.
	// View-space z = -near is at world z = 10 - near; z = -far is at world z = 10 - far.
	view := math3d.LookAt(math3d.Vec3{X: 0, Y: 2, Z: 10}, math3d.Vec3{X: 0, Y: 2, Z: 0}, math3d.Vec3Up())

	cameras := []struct {
		name    string
		frustum math3d.Frustum
	}{
		{
			name:    "PerspectiveZO",
			frustum: math3d.FrustumFromMat4(math3d.PerspectiveZO(math3d.DegToRad(90), 1.0, 1.0, 20.0).Mul(view)),
		},
		{
			name:    "PerspectiveNO",
			frustum: math3d.FrustumFromMat4NO(math3d.PerspectiveNO(math3d.DegToRad(90), 1.0, 1.0, 20.0).Mul(view)),
		},
		{
			name:    "OrthoZO",
			frustum: math3d.FrustumFromMat4(math3d.OrthoZO(-5, 5, -5, 5, 1.0, 20.0).Mul(view)),
		},
		{
			name:    "OrthoNO",
			frustum: math3d.FrustumFromMat4NO(math3d.OrthoNO(-5, 5, -5, 5, 1.0, 20.0).Mul(view)),
		},
	}

	for _, cam := range cameras {
		t.Run(cam.name, func(t *testing.T) {
			f := cam.frustum

			// Points: camera eye is at (0,2,10), near plane at z=9, far plane at z=-10.
			insidePt := math3d.Vec3{X: 0, Y: 2, Z: 0} // distance 10 from camera -> squarely inside
			if !f.ContainsPoint(insidePt) {
				t.Fatalf("expected %v inside frustum", insidePt)
			}
			outsidePts := []struct {
				label string
				p     math3d.Vec3
			}{
				{"before-near", math3d.Vec3{X: 0, Y: 2, Z: 9.5}},
				{"behind-camera", math3d.Vec3{X: 0, Y: 2, Z: 15}},
				{"beyond-far", math3d.Vec3{X: 0, Y: 2, Z: -11}},
				{"far-left", math3d.Vec3{X: -30, Y: 2, Z: 0}},
				{"far-right", math3d.Vec3{X: 30, Y: 2, Z: 0}},
				{"far-top", math3d.Vec3{X: 0, Y: 32, Z: 0}},
				{"far-bottom", math3d.Vec3{X: 0, Y: -28, Z: 0}},
			}
			for _, tc := range outsidePts {
				if f.ContainsPoint(tc.p) {
					t.Fatalf("expected %s point %v outside frustum", tc.label, tc.p)
				}
			}

			// Spheres: inside, straddling near/far, and completely outside.
			if !f.IntersectsSphere(math3d.Sphere{Center: insidePt, Radius: 1}) {
				t.Fatalf("expected interior sphere to intersect frustum")
			}
			if !f.IntersectsSphere(math3d.Sphere{Center: math3d.Vec3{X: 0, Y: 2, Z: 9.5}, Radius: 1}) {
				t.Fatalf("expected sphere straddling near plane to intersect frustum")
			}
			if !f.IntersectsSphere(math3d.Sphere{Center: math3d.Vec3{X: 0, Y: 2, Z: -10.5}, Radius: 1}) {
				t.Fatalf("expected sphere straddling far plane to intersect frustum")
			}
			if f.IntersectsSphere(math3d.Sphere{Center: math3d.Vec3{X: 0, Y: 2, Z: 12}, Radius: 1}) {
				t.Fatalf("expected sphere behind camera to be culled")
			}
			if f.IntersectsSphere(math3d.Sphere{Center: math3d.Vec3{X: 0, Y: 2, Z: -13}, Radius: 1}) {
				t.Fatalf("expected sphere beyond far plane to be culled")
			}
			if f.IntersectsSphere(math3d.Sphere{Center: math3d.Vec3{X: 50, Y: 2, Z: 0}, Radius: 1}) {
				t.Fatalf("expected sphere far right to be culled")
			}

			// AABBs: inside, straddling, enclosing, outside, empty.
			insideBox := math3d.AABB{Min: math3d.Vec3{X: -1, Y: 1, Z: -1}, Max: math3d.Vec3{X: 1, Y: 3, Z: 1}}
			if !f.IntersectsAABB(insideBox) {
				t.Fatalf("expected interior AABB to intersect frustum")
			}
			straddleNear := math3d.AABB{Min: math3d.Vec3{X: -1, Y: 1, Z: 8.5}, Max: math3d.Vec3{X: 1, Y: 3, Z: 10.5}}
			if !f.IntersectsAABB(straddleNear) {
				t.Fatalf("expected AABB straddling near plane to intersect frustum")
			}
			straddleFar := math3d.AABB{Min: math3d.Vec3{X: -1, Y: 1, Z: -12}, Max: math3d.Vec3{X: 1, Y: 3, Z: -9}}
			if !f.IntersectsAABB(straddleFar) {
				t.Fatalf("expected AABB straddling far plane to intersect frustum")
			}
			enclosing := math3d.AABB{Min: math3d.Vec3{X: -100, Y: -100, Z: -100}, Max: math3d.Vec3{X: 100, Y: 100, Z: 100}}
			if !f.IntersectsAABB(enclosing) {
				t.Fatalf("expected enclosing AABB to intersect frustum")
			}
			outsideFar := math3d.AABB{Min: math3d.Vec3{X: -1, Y: 1, Z: -15}, Max: math3d.Vec3{X: 1, Y: 3, Z: -11}}
			if f.IntersectsAABB(outsideFar) {
				t.Fatalf("expected AABB beyond far plane to be culled")
			}
			outsideLeft := math3d.AABB{Min: math3d.Vec3{X: -60, Y: 1, Z: -1}, Max: math3d.Vec3{X: -50, Y: 3, Z: 1}}
			if f.IntersectsAABB(outsideLeft) {
				t.Fatalf("expected AABB far left to be culled")
			}
			if f.IntersectsAABB(math3d.AABBEmpty()) {
				t.Fatalf("empty AABB should not intersect frustum")
			}
		})
	}
}

func TestAngledFrustumAndEdgeCases(t *testing.T) {
	// Oblique camera positioned off-axis looking diagonally across world space.
	eye := math3d.Vec3{X: 5, Y: 8, Z: -4}
	target := math3d.Vec3{X: -3, Y: 1, Z: 6}
	fwd := target.Sub(eye).Norm()
	view := math3d.LookAt(eye, target, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 1.0, 40.0)
	f := math3d.FrustumFromMat4(proj.Mul(view))

	// Point 10 units ahead of camera along fwd is inside; 0.5 units ahead is before near;
	// 45 units ahead is beyond far; -5 units is behind camera.
	if !f.ContainsPoint(eye.Add(fwd.Scale(10))) {
		t.Fatalf("expected point along angled camera axis inside frustum")
	}
	if f.ContainsPoint(eye.Add(fwd.Scale(0.5))) {
		t.Fatalf("expected point before near plane outside angled frustum")
	}
	if f.ContainsPoint(eye.Add(fwd.Scale(45))) {
		t.Fatalf("expected point beyond far plane outside angled frustum")
	}
	if f.ContainsPoint(eye.Add(fwd.Scale(-5))) {
		t.Fatalf("expected point behind camera outside angled frustum")
	}
	if !f.IntersectsSphere(math3d.Sphere{Center: eye.Add(fwd.Scale(0.5)), Radius: 1.0}) {
		t.Fatalf("expected sphere straddling oblique near plane to intersect")
	}

	// Parallel-to-up fallback for LookAt and LookRotation (looking straight up and straight down).
	for _, dir := range []math3d.Vec3{math3d.Vec3Up(), math3d.Vec3Up().Neg()} {
		q := math3d.LookRotation(dir, math3d.Vec3Up())
		if got := q.RotateVec3(math3d.Vec3Forward()); !got.ApproxEqual(dir, eps) {
			t.Fatalf("LookRotation(%v) rotated -Z to %v, want %v", dir, got, dir)
		}
		v := math3d.LookAt(math3d.Vec3Zero(), dir, math3d.Vec3Up())
		if got := v.TransformPoint(dir); !got.ApproxEqual(math3d.Vec3Forward(), eps) {
			t.Fatalf("LookAt(0, %v) mapped target to %v, want -Z", dir, got)
		}
	}

	// Projective AABB.Transform (exercises 8-corner path).
	box := math3d.AABB{Min: math3d.Vec3{X: -1, Y: -1, Z: -10}, Max: math3d.Vec3{X: 1, Y: 1, Z: -2}}
	ndcBox := box.Transform(math3d.Perspective(math3d.DegToRad(90), 1, 1, 100))
	if ndcBox.Empty() || ndcBox.Min.Z < 0 || ndcBox.Max.Z > 1 {
		t.Fatalf("projective AABB.Transform produced unexpected NDC box: %v", ndcBox)
	}

	// Slerp near-identical quaternions (dot > 0.9995 linear fallback).
	q0 := math3d.QuatIdentity()
	qSmall := math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0.001)
	qMid := q0.Slerp(qSmall, 0.5)
	if !qMid.ApproxEqual(math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0.0005), 1e-5) {
		t.Fatalf("Slerp small angle = %v", qMid)
	}

	// Zero-quaternion safety.
	zeroQ := math3d.Quat{}
	if zeroQ.Norm() != math3d.QuatIdentity() || zeroQ.Inverse() != math3d.QuatIdentity() {
		t.Fatalf("zero Quat Norm/Inverse should return QuatIdentity")
	}

	// Non-unit plane normalization & non-unit ray-sphere intersection.
	p := (math3d.Plane{Normal: math3d.Vec3{X: 0, Y: 3, Z: 0}, D: -6}).Normalized()
	if !p.Normal.ApproxEqual(math3d.Vec3Up(), eps) || !math3d.ApproxEqual(p.D, -2, eps) {
		t.Fatalf("Plane.Normalized = %v", p)
	}
	fastRay := math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3{X: 0, Y: 0, Z: -2}}
	ts, hit := fastRay.IntersectSphere(math3d.Sphere{Center: math3d.Vec3{X: 0, Y: 0, Z: -5}, Radius: 1})
	if !hit || !math3d.ApproxEqual(ts, 2, eps) {
		t.Fatalf("non-unit ray IntersectSphere = (%v, %v), want (2, true)", ts, hit)
	}

	// Back-facing triangle hit (double-sided Möller-Trumbore).
	v0 := math3d.Vec3{X: -1, Y: -1, Z: -5}
	v1 := math3d.Vec3{X: 0, Y: 1, Z: -5}
	v2 := math3d.Vec3{X: 1, Y: -1, Z: -5}
	backRay := math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3Forward()}
	if tt, _, _, hit := backRay.IntersectTriangle(v0, v1, v2); !hit || !math3d.ApproxEqual(tt, 5, eps) {
		t.Fatalf("back-facing triangle hit = (%v, %v), want (5, true)", tt, hit)
	}
}

func TestDecomposeRefusesShearAndAcceptsTinyScale(t *testing.T) {
	// #546: shear was silently accepted as TRS, rebuilding a different matrix.
	shear := math3d.Mat4{
		1, 0, 0, 0,
		0.5, 1, 0, 0,
		0, 0, 1, 0,
		5, 6, 7, 1,
	}
	if _, _, _, ok := shear.Decompose(); ok {
		t.Fatal("sheared matrix decomposed as TRS")
	}
	// A uniform 1e-8 scale is invertible and representable, despite a tiny determinant.
	tiny := math3d.Mat4{
		1e-8, 0, 0, 0,
		0, 1e-8, 0, 0,
		0, 0, 1e-8, 0,
		1, 2, 3, 1,
	}
	pos, rot, scale, ok := tiny.Decompose()
	if !ok {
		t.Fatal("tiny-scale matrix was refused")
	}
	if scale.X < 9e-9 || scale.X > 1.1e-8 {
		t.Fatalf("scale = %+v, want ~1e-8", scale)
	}
	rebuild := math3d.TRS(pos, rot, scale)
	if !rebuild.ApproxEqual(tiny, 1e-7) {
		t.Fatalf("rebuild = %+v, want %+v", rebuild, tiny)
	}
	inv, ok := tiny.Inverse()
	if !ok {
		t.Fatal("tiny invertible matrix refused by Inverse")
	}
	if !inv.Mul(tiny).ApproxEqual(math3d.Mat4Identity(), 1e-6) {
		t.Fatalf("inverse product = %+v, want identity", inv.Mul(tiny))
	}
}

func TestTransformInverseMat4NonUniformScaleWithRotation(t *testing.T) {
	tr := math3d.Transform{
		Pos:   math3d.Vec3{X: 3, Y: -2, Z: 5},
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3{X: 1, Y: 2, Z: 3}.Norm(), 0.9),
		Scale: math3d.Vec3{X: 1, Y: 2, Z: 4},
	}
	m := tr.ToMat4()
	inv, ok := tr.InverseMat4()
	if !ok {
		t.Fatal("InverseMat4 reported a singular transform")
	}
	if !inv.Mul(m).ApproxEqual(math3d.Mat4Identity(), 1e-5) || !m.Mul(inv).ApproxEqual(math3d.Mat4Identity(), 1e-5) {
		t.Fatalf("InverseMat4 * ToMat4 is not the identity: %v", inv.Mul(m))
	}
	general, _ := m.Inverse()
	if !inv.ApproxEqual(general, 1e-5) {
		t.Fatalf("InverseMat4 = %v, general inverse = %v", inv, general)
	}
	p := math3d.Vec3{X: 0.5, Y: -1.5, Z: 2}
	if got := inv.TransformPoint(tr.TransformPoint(p)); !got.ApproxEqual(p, 1e-5) {
		t.Fatalf("round trip through InverseMat4 = %v, want %v", got, p)
	}
	// The TRS Inverse cannot represent this inverse (it shears), which its doc comment says; it still undoes t under Mul.
	if got := tr.Inverse().TransformPoint(tr.TransformPoint(p)); got.ApproxEqual(p, 1e-3) {
		t.Fatalf("Transform.Inverse round-tripped %v exactly; the non-uniform case should need InverseMat4", p)
	}
	if !tr.Mul(tr.Inverse()).ApproxEqual(math3d.TransformIdentity(), 1e-5) {
		t.Fatal("t.Mul(t.Inverse()) is not the identity")
	}
}

func TestTransformInverseMat4RejectsSingularAndNonFinite(t *testing.T) {
	nan := float32(math.NaN())
	for _, tr := range []math3d.Transform{
		{Rot: math3d.QuatIdentity(), Scale: math3d.Vec3{X: 1, Y: 0, Z: 1}},
		{Rot: math3d.QuatIdentity(), Scale: math3d.Vec3{X: 1, Y: 1, Z: nan}},
		{Pos: math3d.Vec3{X: nan}, Rot: math3d.QuatIdentity(), Scale: math3d.Vec3One()},
		{Rot: math3d.Quat{W: float32(math.Inf(1))}, Scale: math3d.Vec3One()},
		// 1e-40 is subnormal in float32, so 1/s overflows to +Inf.
		{Rot: math3d.QuatIdentity(), Scale: math3d.Vec3{X: 1e-40, Y: 1, Z: 1}},
	} {
		if m, ok := tr.InverseMat4(); ok || m != (math3d.Mat4{}) {
			t.Fatalf("InverseMat4(%+v) = %v, %v; want the zero matrix and false", tr, m, ok)
		}
	}
	// A tiny but non-zero scale is still invertible.
	tiny := math3d.Transform{Rot: math3d.QuatIdentity(), Scale: math3d.Vec3{X: 1e-8, Y: 1, Z: 1}}
	inv, ok := tiny.InverseMat4()
	if !ok || !inv.Mul(tiny.ToMat4()).ApproxEqual(math3d.Mat4Identity(), 1e-5) {
		t.Fatalf("InverseMat4 of scale 1e-8 = %v, %v", inv, ok)
	}
}

func BenchmarkTransformInverseMat4(b *testing.B) {
	tr := math3d.Transform{Pos: math3d.Vec3{X: 1, Y: 2, Z: 3}, Rot: math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0.5), Scale: math3d.Vec3{X: 1, Y: 2, Z: 3}}
	var sink math3d.Mat4
	for b.Loop() {
		sink, _ = tr.InverseMat4()
	}
	_ = sink
}
