# lights2d

`game/lights2d` adds 2D lighting to NSGo canvases: an ambient darkness layer, point and directional cone lights, exact 2D visibility-polygon shadows from occluders, and CPU tangent-space normal-mapped sprite shading.

```go
import "github.com/nsgo-dev/game/pkg/lights2d"
```

## Ambient layer, point lights, and spotlights

Create a `Scene` with an `Ambient` color and a slice of `*Light`, then draw it over your 2D world inside `Canvas.Draw`:

```go
torch := lights2d.NewLight(160, 120, 180, core.RGB(0xffc478))
torch.CastShadows = true

flashlight := lights2d.NewLight(240, 140, 260, core.RGB(0xdcebff))
flashlight.ConeAngle = 0          // radians: 0 = +X, pi/2 = +Y
flashlight.ConeSpread = math.Pi / 3
flashlight.CastShadows = true

lighting := &lights2d.Scene{
	Ambient: core.RGBA(0x0c101e, 0.85),
	Lights:  []*lights2d.Light{torch, flashlight},
}

// Inside Canvas.Draw, after drawing tiles and sprites:
lighting.Draw(d, worldBounds)
```

- `NewLight(x, y, radius, color)` creates an enabled omnidirectional light with `Intensity: 1` and quadratic `Falloff: 2`.
- Setting `ConeSpread` in `(0, 2*math.Pi)` turns a light into a directional spotlight with smoothstep angular falloff around `ConeAngle`. `Scene.Draw` approximates that angular smoothstep with 16 concentric nested radial-gradient wedges (within about 3% of `AttenuationAt` / `EvaluateAt` across the cone).
- Light glows default to additive `core.CompositeLighter` when `Scene.LightComposite` is unset (`0`). Assign a non-zero blend such as `core.CompositeScreen` for softer lights, or call `lighting.SetLightComposite(core.CompositeSourceOver)` for alpha-over glows (since `core.CompositeSourceOver` is the zero value). Query the active mode with `lighting.EffectiveLightComposite()`.
- Query exact CPU illumination at any world coordinate with `light.AttenuationAt(x, y)` or `scene.EvaluateAt(x, y)`.

## Visibility-polygon shadow casters

Populate `Scene.Occluders` from walls, pillars, or tile colliders using `RectOccluder`, `PolygonOccluder`, or `SegmentOccluder`:

```go
lighting.Occluders = []lights2d.Occluder{
	lights2d.RectOccluder(core.Bounds{Origin: core.Point{X: 200, Y: 90}, Size: core.Size{Width: 48, Height: 64}}),
	lights2d.SegmentOccluder(lights2d.Segment{
		A: core.Point{X: 80, Y: 180},
		B: core.Point{X: 160, Y: 180},
	}),
}
```

For a tilemap, a [`tileshape.View`](tileshape.md) merges solid cells and per-tile collision shapes into occluders that cast the same shadows with far fewer segments: assign `lighting.Occluders = view.Occluders()`, and again after the map changes.

Set `Scene.DisableShadows` to skip occlusion for every light without touching each light's `CastShadows`, as a preset with shadows off does (`lights.DisableShadows = !preset.Shadows`, see [quality](quality.md)). `Draw`, `EvaluateAt`, `ShadeNormalMap` and `NormalSprite` all honor it, and toggling it re-shades a `NormalSprite`.

When `light.CastShadows` is `true`, `Scene.Draw` clips that light against `VisibilityPolygon(origin, radius, occluders)`. You can also call the geometry helpers directly for stealth, line-of-sight, or fog-of-war checks:

| Helper | Purpose |
| --- | --- |
| `VisibilityPolygon(origin, radius, occluders)` | Returns the ordered 2D polygon visible from `origin` within `radius`. |
| `Visible(origin, target, occluders)` | Reports whether the segment from `origin` to `target` is unobstructed. |
| `LineOfSight(&occluders)` | Returns `Visible` against the occluders as an `ai.LineOfSight`, so they block AI perception too ([ai](ai.md#perception)). |
| `PointInPolygon(p, poly)` | Tests whether point `p` lies inside a visibility polygon. |

## Normal-mapped sprites

Give 2D pixel art or painted sprites directional rim and bump highlights using a tangent-space normal map (`*image.NRGBA`):

```go
knight := lights2d.NewNormalSprite(diffuseImg, normalMapImg)
knight.X = 160
knight.Y = 120
knight.LightZ = 48 // virtual Z height of lights above the sprite plane

// Inside Canvas.Draw:
knight.Draw(d, lighting)
```

`NormalSprite` caches the CPU-shaded pixel buffer and only re-shades when the sprite position, `LightZ`, ambient color, or affecting lights/occluders change. Call `knight.Invalidate()` if you mutate `Diffuse` or `NormalMap` pixels in place, or use `ShadeNormalMap`, `ShadeNormalMapWithOccluders`, and `scene.ShadeNormalMap` directly when baking custom buffers.

## Authoring lights and occluders in scenes

You can also declare `ambientColor`, `light2d` point/spot lights, and `occluder2d` rect/polygon/tilemap shadow casters directly in `*.scene.yaml` files and preview them live with interactive viewport handles in [`game/editor`](editor.md#built-in-2d-lighting-and-occluder-components-887). `sceneformat.Build` turns them into `BuiltScene.Lights2D`, which renders the same shadows in the editor preview and in the game.
