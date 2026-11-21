# math2d

`game/math2d` 提供在 NSGo 游戏包之间共享的轻量、零堆分配 2D 值类型：`Vec2` 向量、轴对齐矩形 `Rect`、圆 `Circle` 以及 3×2 仿射变换 `Affine`，并支持与 NSGo `core` 几何类型之间的无损互转。

```go
import "github.com/nsgo-dev/game/packages/game/math2d"
```

所有坐标均采用 `float32`，与 `core.Points`、`core.Point`、`core.Size`、`core.Bounds` 和 `core.Drawing` 保持一致。角度单位为弧度，从 `+X` 转向 `+Y`（在 NSGo 向下为 `+Y` 的画布坐标系中即顺时针方向）。

## 向量（`Vec2`）

可使用 `V2(x, y)`、`Zero()`、`One()`、`UnitX()`、`UnitY()` 或 `FromAngle(rad)` 构造向量，并与 NSGo `core` 无损转换：

```go
pos := math2d.FromPoint(pointerPos)
dir := target.Sub(pos).Normalize()
vel := dir.Scale(220).ClampLen(240)
next := pos.Add(vel.Scale(float32(dt.Seconds())))

// 无损转回 NSGo core 类型：
pt := next.Point()
sz := math2d.V2(32, 48).Size()
_, _ = pt, sz
```

| 方法 | 作用 |
| --- | --- |
| `Add`、`Sub`、`Mul`、`Div`、`Scale`、`Neg` | 分量四则运算与标量缩放。 |
| `Dot`、`Cross`、`Len`、`LenSq`、`Dist`、`DistSq` | 点积、2D 标量叉积（`X*o.Y - Y*o.X`）、长度与距离。 |
| `Normalize`、`ClampLen` | 单位化（零向量返回 `Zero()`）与模长上限钳制。 |
| `Lerp`、`MoveTowards` | 线性插值与固定步长朝目标逼近。 |
| `Angle`、`Rotate`、`Perp`、`Reflect`、`Project` | 极角、弧度旋转、90° 垂线、反射与投影。 |
| `Min`、`Max`、`Clamp`、`ApproxEqual` | 分量上下界与误差容限比较。 |

## 矩形与圆（`Rect`、`Circle`）

`Rect` 存储 `{X, Y, W, H}`，包含判定采用半开区间 `[X, X+W) × [Y, Y+H)`，与 `core.Bounds` 一致。当 `W <= 0`、`H <= 0` 或任意字段为 `NaN` 时视为空矩形。

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

- 用 `FromBounds(b)`、`FromCenter(center, size)` 或 `FromMinMax(lo, hi)` 创建矩形，用 `r.Bounds()` 转回 `core.Bounds`。
- 矩形查询涵盖 `Min`、`Max`、`Center`、`Size`、`Normalized`、`Empty`、`Contains`、`ContainsRect`、`Overlaps`、`Intersection`、`Union`、`Expand`、`Translate`、`ClampPoint` 与 `ClosestPoint`。
- 圆查询涵盖 `Contains`、`Overlaps`、`OverlapsRect`、`Bounds`、`Translate` 与 `ApproxEqual`。

## 2D 仿射变换（`Affine`）

`Affine` 以 `{A, B, C, D, Tx, Ty}` 的列序保存 3×2 矩阵，与 `core.Drawing.Transform` 和 `core.Matrix3D.Affine`（Canvas 2D / SVG `matrix(a, b, c, d, e, f)`）完全一致：

```go
// 先缩放、再旋转、最后平移：
m := math2d.TRS(math2d.V2(120, 80), 0.35, math2d.V2(2, 2))
worldPt := m.TransformPoint(math2d.V2(8, 0))
worldBox := m.TransformRect(localBounds)

// 求逆以把屏幕/世界坐标映回局部空间：
if inv, ok := m.Invert(); ok {
	localPt := inv.TransformPoint(worldPt)
	_ = localPt
}

// 与 core.Matrix3D 无损互转：
mat3d := m.ToCore()
back, ok := math2d.FromCore(mat3d)
_, _, _ = worldBox, back, ok
```

可使用 `Identity()`、`Translate(x, y)`、`Scale(sx, sy)`、`Rotate(rad)`、`RotateAround(rad, cx, cy)`、`TRS(pos, rot, scale)` 与 `m.Mul(other)`（自右向左生效：先应用 `other`，再应用 `m`）。`TransformVector(v)` 仅应用线性部分而忽略平移。
