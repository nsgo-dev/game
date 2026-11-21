# math2d

`game/math2d` provides small, zero-allocation 2D value types shared across NSGo game packages: `Vec2`, axis-aligned `Rect`, `Circle`, and 3×2 `Affine` transforms, with lossless conversions to and from NSGo's `core` geometry types.

```go
import "github.com/nsgo-dev/game/packages/game/math2d"
```

All coordinates use `float32` to match `core.Points`, `core.Point`, `core.Size`, `core.Bounds`, and `core.Drawing`. Angles are in radians, turning from `+X` toward `+Y` (clockwise in NSGo's Y-down canvas coordinates).

## Vectors (`Vec2`)

Construct vectors with `V2(x, y)`, `Zero()`, `One()`, `UnitX()`, `UnitY()`, or `FromAngle(rad)`, and convert losslessly to and from NSGo `core`:

```go
pos := math2d.FromPoint(pointerPos)
dir := target.Sub(pos).Normalize()
vel := dir.Scale(220).ClampLen(240)
next := pos.Add(vel.Scale(float32(dt.Seconds())))

// Convert back to NSGo core types with zero loss:
pt := next.Point()
sz := math2d.V2(32, 48).Size()
_, _ = pt, sz
```

| Method | Behavior |
| --- | --- |
| `Add`, `Sub`, `Mul`, `Div`, `Scale`, `Neg` | Component-wise arithmetic and scalar scaling. |
| `Dot`, `Cross`, `Len`, `LenSq`, `Dist`, `DistSq` | Products, lengths, and distances (`Cross` returns the 2D scalar `X*o.Y - Y*o.X`). |
| `Normalize`, `ClampLen` | Unit vector (returns `Zero()` for zero-length) and magnitude clamping. |
| `Lerp`, `MoveTowards` | Linear interpolation and fixed-step pursuit toward a target. |
| `Angle`, `Rotate`, `Perp`, `Reflect`, `Project` | Polar angle, rotation in radians, 90° perpendicular, reflection, and projection. |
| `Min`, `Max`, `Clamp`, `ApproxEqual` | Component-wise bounds and epsilon comparison. |

## Rectangles and circles (`Rect`, `Circle`)

`Rect` stores `{X, Y, W, H}` using half-open containment `[X, X+W) × [Y, Y+H)`, matching `core.Bounds`. Any `Rect` with `W <= 0` or `H <= 0` (or `NaN`) is empty.

```go
box := math2d.FromCenter(math2d.V2(160, 90), math2d.V2(40, 24))
expanded := box.Expand(4)

if hit, ok := expanded.Intersection(math2d.FromBounds(viewBounds)); ok {
	coreRect := hit.Bounds()
	_ = coreRect
}

blast := math2d.Circle{Center: math2d.V2(180, 96), Radius: 36}
if blast.OverlapsRect(box) {
	closest := box.ClosestPoint(blast.Center)
	_ = closest
}
```

- Construct rectangles with `FromBounds(b)`, `FromCenter(center, size)`, or `FromMinMax(lo, hi)`, and convert back via `r.Bounds()`.
- Query rectangles with `Min`, `Max`, `Center`, `Size`, `Normalized`, `Empty`, `Contains`, `ContainsRect`, `Overlaps`, `Intersection`, `Union`, `Expand`, `Translate`, `ClampPoint`, and `ClosestPoint`.
- Query circles with `Contains`, `Overlaps`, `OverlapsRect`, `Bounds`, `Translate`, and `ApproxEqual`.

## 2D affine transforms (`Affine`)

`Affine` stores a 3×2 matrix `{A, B, C, D, Tx, Ty}` in the same column order as `core.Drawing.Transform` and `core.Matrix3D.Affine` (`Canvas 2D` / SVG `matrix(a, b, c, d, e, f)`):

```go
// Scale first, then rotate, then translate:
m := math2d.TRS(math2d.V2(120, 80), 0.35, math2d.V2(2, 2))
worldPt := m.TransformPoint(math2d.V2(8, 0))
worldBox := m.TransformRect(localBounds)

// Invert to map screen/world points back into local space:
if inv, ok := m.Invert(); ok {
	localPt := inv.TransformPoint(worldPt)
	_ = localPt
}

// Lossless conversion to and from core.Matrix3D:
mat3d := m.ToCore()
back, ok := math2d.FromCore(mat3d)
_, _, _ = worldBox, back, ok
```

Use `Identity()`, `Translate(x, y)`, `Scale(sx, sy)`, `Rotate(rad)`, `RotateAround(rad, cx, cy)`, `TRS(pos, rot, scale)`, and `m.Mul(other)` (right-to-left composition: `other` applied first, then `m`). `TransformVector(v)` applies only the linear part without translation.
