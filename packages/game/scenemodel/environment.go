package scenemodel

import (
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// Light is a 3D light built from a scene object (sceneformat.ObjectDoc.Light), attached to its scene3d.Node so moving or rotating the node updates the light's world position and direction (-Z in local space).
type Light struct {
	// Node is the scene3d node placing and aiming the light.
	Node *scene3d.Node
	// Object is the ID of the scene object the light belongs to.
	Object string
	// Doc is a copy of the source light definition.
	Doc sceneformat.LightDoc
}

// Camera is a 3D camera built from a scene object (sceneformat.ObjectDoc.Camera), attached to its scene3d.Node so moving or rotating the node updates the camera's viewpoint.
type Camera struct {
	// Node is the scene3d node placing and orienting the camera.
	Node *scene3d.Node
	// Object is the ID of the scene object the camera belongs to.
	Object string
	// Doc is a copy of the source camera definition.
	Doc sceneformat.Camera3DDoc
}

// SceneCamera constructs a *scene3d.Camera from c's current node world transform and projection settings for the given viewport aspect ratio (width / height).
func (c *Camera) SceneCamera(aspect float32) *scene3d.Camera {
	if c == nil {
		return nil
	}
	if aspect <= 0 {
		aspect = 1
	}
	near := c.Doc.EffectiveNear()
	far := c.Doc.EffectiveFar()
	var cam *scene3d.Camera
	if c.Doc.EffectiveProjection() == sceneformat.ProjectionOrthographic {
		cam = scene3d.NewOrthographicCamera(math3d.Vec3Zero(), c.Doc.EffectiveOrthoHeight(), aspect, near, far)
	} else {
		cam = scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(c.Doc.EffectiveFOV()), aspect, near, far)
	}
	if c.Node != nil {
		cam.SyncFromNode(c.Node)
	}
	return cam
}

// Environment is the resolved 3D environment built from sceneformat.SceneDoc.Environment, ready to drive render3d's sky, ambient fill, image-based lighting, fog, tone mapping and exposure.
type Environment struct {
	// Present reports whether the scene declared an environment section.
	Present bool
	// Doc is a copy of SceneDoc.Environment when Present is true.
	Doc sceneformat.EnvironmentDoc
	// Ambient is the linear ambient fill light.
	Ambient render3d.AmbientLight
	// SkyKind is the resolved sky kind, or empty when Doc.Sky is nil.
	SkyKind sceneformat.SkyKind
	// SkyColor is the linear RGB color of a SkyColor sky.
	SkyColor math3d.Vec3
	// SkyClearColor is the display-space tone-mapped clear color for SkyColor.
	SkyClearColor core.Color
	// SkyCubemap is the linear HDR cubemap for SkyGradient or SkyHDR skies, or nil.
	SkyCubemap *render3d.HDRCubemap
	// IBLIrradiance is the L2 spherical-harmonic diffuse irradiance when IBLIntensity > 0.
	IBLIrradiance render3d.IrradianceSH
	// IBLPrefilter is the GGX prefiltered specular cubemap when IBLIntensity > 0.
	IBLPrefilter *render3d.HDRCubemap
	// IBLIntensity is the effective IBL multiplier (>= 0).
	IBLIntensity float32
	// Fog is the resolved render3d.Fog configuration.
	Fog render3d.Fog
	// HasFog reports whether distance or height fog is active.
	HasFog bool
	// ToneMap holds the tone mapping operator and exposure.
	ToneMap render3d.ToneMapOptions
}

// HasSceneLighting reports whether the built scene declares any 3D lights or an environment section.
func (b *Built) HasSceneLighting() bool {
	return b != nil && (len(b.Lights) > 0 || b.Environment.Present)
}

// ActiveCameraObject returns the scene's active 3D camera object: the one marked Current, or the first 3D camera in document order when none is marked.
func (b *Built) ActiveCameraObject() (*Camera, bool) {
	if b == nil || len(b.Cameras) == 0 {
		return nil, false
	}
	for _, c := range b.Cameras {
		if c.Doc.Current {
			return c, true
		}
	}
	return b.Cameras[0], true
}

// ActiveCamera builds a *scene3d.Camera from the scene's active 3D camera object (ActiveCameraObject) for the given aspect ratio (width / height).
func (b *Built) ActiveCamera(aspect float32) (*scene3d.Camera, bool) {
	c, ok := b.ActiveCameraObject()
	if !ok {
		return nil, false
	}
	return c.SceneCamera(aspect), true
}

// LightRig evaluates every visible, enabled light node in b at its current world transform and returns a render3d.LightRig combining b.Environment.Ambient with the scene's directional, point and spot lights. Within each category, shadow-casting lights come first so slot 0 matches render3d's shadow sampling slots.
func (b *Built) LightRig() render3d.LightRig {
	if b == nil {
		return render3d.LightRig{}
	}
	rig := render3d.LightRig{
		Ambient: b.Environment.Ambient,
	}
	var dirShadow, dirOther []render3d.DirectionalLight
	var ptShadow, ptOther []render3d.PointLight
	var spotShadow, spotOther []render3d.SpotLight

	for _, l := range b.Lights {
		if l == nil || l.Node == nil || !l.Node.EffectiveVisible() || !l.Node.EffectiveEnabled() {
			continue
		}
		wt := l.Node.WorldTransform()
		dir := wt.Rot.Norm().RotateVec3(math3d.Vec3Forward())
		if dir.LenSq() <= 1e-12 {
			dir = math3d.Vec3Forward()
		}
		col := parseCSSLinearRGB(l.Doc.EffectiveColor())
		intensity := l.Doc.EffectiveIntensity()

		switch l.Doc.Kind {
		case sceneformat.LightDirectional:
			dl := render3d.DirectionalLight{
				Direction: dir,
				Color:     col,
				Intensity: intensity,
			}
			if l.Doc.Shadows {
				dirShadow = append(dirShadow, dl)
			} else {
				dirOther = append(dirOther, dl)
			}
		case sceneformat.LightPoint:
			pl := render3d.PointLight{
				Position:  wt.Pos,
				Color:     col,
				Intensity: intensity,
				Range:     l.Doc.Range,
			}
			if l.Doc.Shadows {
				ptShadow = append(ptShadow, pl)
			} else {
				ptOther = append(ptOther, pl)
			}
		case sceneformat.LightSpot:
			sl := render3d.SpotLight{
				Position:       wt.Pos,
				Direction:      dir,
				Color:          col,
				Intensity:      intensity,
				Range:          l.Doc.Range,
				InnerConeAngle: math3d.DegToRad(l.Doc.InnerCone),
				OuterConeAngle: math3d.DegToRad(l.Doc.EffectiveOuterCone()),
			}
			if l.Doc.Shadows {
				spotShadow = append(spotShadow, sl)
			} else {
				spotOther = append(spotOther, sl)
			}
		}
	}

	dirAll := append(dirShadow, dirOther...)
	if len(dirAll) > render3d.MaxDirectionalLights {
		dirAll = dirAll[:render3d.MaxDirectionalLights]
	}
	rig.Directional = dirAll

	ptAll := append(ptShadow, ptOther...)
	if len(ptAll) > render3d.MaxPointLights {
		ptAll = ptAll[:render3d.MaxPointLights]
	}
	rig.Point = ptAll

	spotAll := append(spotShadow, spotOther...)
	if len(spotAll) > render3d.MaxSpotLights {
		spotAll = spotAll[:render3d.MaxSpotLights]
	}
	rig.Spot = spotAll
	return rig
}

// ShadowLights returns the first visible shadow-casting directional, point and spot lights in b, matching slot 0 of each LightRig slice.
func (b *Built) ShadowLights() (dir *render3d.DirectionalLight, point *render3d.PointLight, spot *render3d.SpotLight) {
	if b == nil {
		return nil, nil, nil
	}
	for _, l := range b.Lights {
		if l == nil || !l.Doc.Shadows || l.Node == nil || !l.Node.EffectiveVisible() || !l.Node.EffectiveEnabled() {
			continue
		}
		wt := l.Node.WorldTransform()
		fwd := wt.Rot.Norm().RotateVec3(math3d.Vec3Forward())
		if fwd.LenSq() <= 1e-12 {
			fwd = math3d.Vec3Forward()
		}
		col := parseCSSLinearRGB(l.Doc.EffectiveColor())
		intensity := l.Doc.EffectiveIntensity()
		switch l.Doc.Kind {
		case sceneformat.LightDirectional:
			if dir == nil {
				dl := render3d.DirectionalLight{Direction: fwd, Color: col, Intensity: intensity}
				dir = &dl
			}
		case sceneformat.LightPoint:
			if point == nil {
				pl := render3d.PointLight{Position: wt.Pos, Color: col, Intensity: intensity, Range: l.Doc.Range}
				point = &pl
			}
		case sceneformat.LightSpot:
			if spot == nil {
				sl := render3d.SpotLight{
					Position:       wt.Pos,
					Direction:      fwd,
					Color:          col,
					Intensity:      intensity,
					Range:          l.Doc.Range,
					InnerConeAngle: math3d.DegToRad(l.Doc.InnerCone),
					OuterConeAngle: math3d.DegToRad(l.Doc.EffectiveOuterCone()),
				}
				spot = &sl
			}
		}
	}
	return dir, point, spot
}

// WorldBounds returns the union of the world-space AABBs of every visible, enabled drawable in b, or false when b has none.
func (b *Built) WorldBounds() (math3d.AABB, bool) {
	if b == nil {
		return math3d.AABB{}, false
	}
	bounds := math3d.AABBEmpty()
	any := false
	for _, d := range b.Drawables {
		if !d.Node.EffectiveVisible() || !d.Node.EffectiveEnabled() {
			continue
		}
		world := d.Node.WorldMatrix()
		for pi := range d.Parts {
			bounds = bounds.Union(d.Parts[pi].Mesh.Bounds().Transform(world))
			any = true
		}
	}
	return bounds, any
}

var errNoHDRResolver = errors.New("naming an HDR sky requires an HDRResolver")

// buildEnvironment converts a validated sceneformat.EnvironmentDoc into a runtime Environment, loading any HDR sky panorama through resolver when it implements HDRResolver.
func buildEnvironment(doc *sceneformat.EnvironmentDoc, resolver ModelResolver) (Environment, error) {
	if doc == nil {
		return Environment{
			ToneMap: render3d.ToneMapOptions{
				Operator: render3d.ToneMapNone,
				Exposure: 1,
			},
		}, nil
	}
	env := Environment{
		Present: true,
		Doc:     *doc.Clone(),
		Ambient: render3d.AmbientLight{
			Color:     parseCSSLinearRGB(doc.EffectiveAmbientColor()),
			Intensity: doc.EffectiveAmbientIntensity(),
		},
		IBLIntensity: doc.EffectiveIBLIntensity(),
		ToneMap: render3d.ToneMapOptions{
			Operator: tonemapOperator(doc.EffectiveTonemapping()),
			Exposure: doc.EffectiveExposure(),
		},
	}

	if doc.Fog != nil {
		f := render3d.Fog{
			Mode:          fogMode(doc.Fog.EffectiveMode()),
			Color:         parseCSSLinearRGB(doc.Fog.EffectiveColor()),
			Start:         doc.Fog.Start,
			End:           doc.Fog.End,
			Density:       doc.Fog.Density,
			MaxOpacity:    doc.Fog.EffectiveMaxOpacity(),
			HeightEnabled: doc.Fog.HeightEnabled,
			HeightBase:    doc.Fog.HeightBase,
			HeightFalloff: doc.Fog.HeightFalloff,
			HeightDensity: doc.Fog.HeightDensity,
		}
		if err := f.Validate(); err != nil {
			return Environment{}, fmt.Errorf("scenemodel: environment fog: %w", err)
		}
		env.Fog = f
		env.HasFog = f.Mode != render3d.FogOff || f.HeightEnabled
	}

	if doc.Sky != nil {
		env.SkyKind = doc.Sky.Kind
		switch doc.Sky.Kind {
		case sceneformat.SkyColor:
			lin := parseCSSLinearRGB(doc.Sky.EffectiveColor())
			env.SkyColor = lin
			mapped := render3d.ApplyToneMap(lin, env.ToneMap)
			env.SkyClearColor = srgbColorFromVec3(mapped)
			if env.IBLIntensity > 0 {
				cube, err := solidHDRCubemap(lin, 4)
				if err != nil {
					return Environment{}, fmt.Errorf("scenemodel: environment sky IBL: %w", err)
				}
				if err := computeEnvIBL(&env, cube); err != nil {
					return Environment{}, err
				}
			}
		case sceneformat.SkyGradient:
			z := vec3ToArray(parseCSSLinearRGB(doc.Sky.EffectiveZenith()))
			h := vec3ToArray(parseCSSLinearRGB(doc.Sky.EffectiveHorizon()))
			g := vec3ToArray(parseCSSLinearRGB(doc.Sky.EffectiveGround()))
			cube, err := render3d.GradientSkyHDRCubemap(z, h, g, 16)
			if err != nil {
				return Environment{}, fmt.Errorf("scenemodel: environment gradient sky: %w", err)
			}
			env.SkyCubemap = cube
			if env.IBLIntensity > 0 {
				if err := computeEnvIBL(&env, cube); err != nil {
					return Environment{}, err
				}
			}
		case sceneformat.SkyHDR:
			hr, ok := resolver.(HDRResolver)
			if !ok {
				return Environment{}, fmt.Errorf("scenemodel: environment sky hdr %q: %w", doc.Sky.HDR, errNoHDRResolver)
			}
			img, err := hr.HDR(doc.Sky.HDR)
			if err != nil {
				return Environment{}, fmt.Errorf("scenemodel: environment sky hdr %q: %w", doc.Sky.HDR, err)
			}
			if img == nil {
				return Environment{}, fmt.Errorf("scenemodel: environment sky hdr %q: HDRResolver returned nil", doc.Sky.HDR)
			}
			cube, err := render3d.CubemapFromHDREquirectangular(img, 32)
			if err != nil {
				return Environment{}, fmt.Errorf("scenemodel: environment sky hdr %q: %w", doc.Sky.HDR, err)
			}
			env.SkyCubemap = cube
			if env.IBLIntensity > 0 {
				if err := computeEnvIBL(&env, cube); err != nil {
					return Environment{}, err
				}
			}
		}
	}

	return env, nil
}

func computeEnvIBL(env *Environment, cube *render3d.HDRCubemap) error {
	sh, err := render3d.ComputeIrradianceSHHDR(cube)
	if err != nil {
		return fmt.Errorf("scenemodel: environment IBL irradiance: %w", err)
	}
	prefilter, err := render3d.PrefilterEnvironmentHDR(cube, render3d.PrefilterOptions{
		BaseSize:    16,
		MipLevels:   4,
		SampleCount: 16,
	})
	if err != nil {
		return fmt.Errorf("scenemodel: environment IBL prefilter: %w", err)
	}
	env.IBLIrradiance = sh
	env.IBLPrefilter = prefilter
	return nil
}

func solidHDRCubemap(rgb math3d.Vec3, size int) (*render3d.HDRCubemap, error) {
	cm, err := render3d.NewHDRCubemap(size)
	if err != nil {
		return nil, err
	}
	col := [3]float32{rgb.X, rgb.Y, rgb.Z}
	for face := range 6 {
		for i := range cm.Faces[face] {
			cm.Faces[face][i] = col
		}
	}
	return cm, nil
}

func parseCSSLinearRGB(css string) math3d.Vec3 {
	c, err := core.ParseColor(css)
	if err != nil {
		return math3d.Vec3{X: 1, Y: 1, Z: 1}
	}
	r, g, b, _ := c.Components()
	return math3d.Vec3{
		X: srgbToLinear(r),
		Y: srgbToLinear(g),
		Z: srgbToLinear(b),
	}
}

func vec3ToArray(v math3d.Vec3) [3]float32 {
	return [3]float32{v.X, v.Y, v.Z}
}

func srgbColorFromVec3(v math3d.Vec3) core.Color {
	clampByte := func(x float32) uint32 {
		if x <= 0 || math.IsNaN(float64(x)) {
			return 0
		}
		if x >= 1 {
			return 255
		}
		return uint32(math.Round(float64(x) * 255))
	}
	hex := (clampByte(v.X) << 16) | (clampByte(v.Y) << 8) | clampByte(v.Z)
	return core.RGBA(hex, 1)
}

func fogMode(m sceneformat.FogMode) render3d.FogMode {
	switch m {
	case sceneformat.FogLinear:
		return render3d.FogLinear
	case sceneformat.FogExp:
		return render3d.FogExp
	case sceneformat.FogExp2:
		return render3d.FogExp2
	default:
		return render3d.FogOff
	}
}

func tonemapOperator(m sceneformat.TonemapMode) render3d.ToneMapOperator {
	switch m {
	case sceneformat.TonemapReinhard:
		return render3d.ToneMapReinhard
	case sceneformat.TonemapACES:
		return render3d.ToneMapACES
	case sceneformat.TonemapNeutral:
		return render3d.ToneMapNeutral
	case sceneformat.TonemapAgX:
		return render3d.ToneMapAgX
	default:
		return render3d.ToneMapNone
	}
}
