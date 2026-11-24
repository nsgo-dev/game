package mcp

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
)

// ToolScreenshot captures the game view as a PNG image, optionally with aligned sprite bounds,
// physics collision shapes, reference grid, camera frame and label overlays drawn on top.
const ToolScreenshot = "game_screenshot"

const (
	maxScreenshotDim    = 8192
	maxScreenshotPixels = 4096 * 4096
	defaultGridStep     = 32
)

// OverlayStyle defines the colors drawn by game_screenshot diagnostic overlays.
type OverlayStyle struct {
	BoundsColor     core.Color
	BoxColor        core.Color
	CircleColor     core.Color
	TriggerColor    core.Color
	SolidTileColor  core.Color
	OneWayTileColor core.Color
	GridColor       core.Color
	CameraColor     core.Color
	LabelTextColor  core.Color
	LabelBgColor    core.Color
}

// DefaultOverlayStyle returns the default high-contrast colors for game_screenshot overlays.
// Collision shape colors match physics.DefaultDebugStyle.
func DefaultOverlayStyle() OverlayStyle {
	pdef := physics.DefaultDebugStyle()
	boxColor := pdef.BoxColor
	if boxColor == 0 {
		boxColor = core.RGB(0x22c55e)
	}
	circleColor := pdef.CircleColor
	if circleColor == 0 {
		circleColor = core.RGB(0x38bdf8)
	}
	solidTileColor := pdef.SolidTileColor
	if solidTileColor == 0 {
		solidTileColor = core.RGB(0xf59e0b)
	}
	oneWayTileColor := pdef.OneWayTileColor
	if oneWayTileColor == 0 {
		oneWayTileColor = core.RGB(0xc084fc)
	}
	return OverlayStyle{
		BoundsColor:     core.RGB(0x00e5ff),
		BoxColor:        boxColor,
		CircleColor:     circleColor,
		TriggerColor:    core.RGB(0xf472b6),
		SolidTileColor:  solidTileColor,
		OneWayTileColor: oneWayTileColor,
		GridColor:       core.RGBA(0xffffff, 0x40),
		CameraColor:     core.RGB(0xfacc15),
		LabelTextColor:  core.RGB(0xffffff),
		LabelBgColor:    core.RGBA(0x000000, 0xc0),
	}
}

// ScreenshotOverlays configures diagnostic overlays drawn on top of a game_screenshot capture.
type ScreenshotOverlays struct {
	// Bounds draws crisp bounding rectangles aligned with every visible sprite in the scene.
	Bounds bool `json:"bounds,omitempty"`
	// CollisionShapes draws 2D physics collision shapes (boxes, circles, triggers, rigid bodies and collision tiles).
	CollisionShapes bool `json:"collision_shapes,omitempty"`
	// Physics is an alias for CollisionShapes; setting either enables collision-shape overlays.
	Physics bool `json:"physics,omitempty"`
	// Grid draws a reference grid in world space (or view space when no camera is active).
	Grid bool `json:"grid,omitempty"`
	// GridStep sets the spacing of the reference grid in world points when Grid is true (default 32).
	GridStep float32 `json:"grid_step,omitempty"`
	// Camera draws the active camera's visible frame and center crosshair.
	Camera bool `json:"camera,omitempty"`
	// Labels draws compact text labels next to drawn sprite bounds and physics bodies.
	Labels bool `json:"labels,omitempty"`
}

// OverlaysDrawn summarizes which overlays were rendered on a screenshot and how many primitives each drew.
type OverlaysDrawn struct {
	Bounds          bool `json:"bounds"`
	CollisionShapes bool `json:"collision_shapes"`
	Grid            bool `json:"grid,omitempty"`
	Camera          bool `json:"camera,omitempty"`
	Labels          bool `json:"labels,omitempty"`
	SpriteBounds    int  `json:"sprite_bounds,omitempty"`
	PhysicsBoxes    int  `json:"physics_boxes,omitempty"`
	PhysicsCircles  int  `json:"physics_circles,omitempty"`
	PhysicsTriggers int  `json:"physics_triggers,omitempty"`
	PhysicsTiles    int  `json:"physics_tiles,omitempty"`
}

// ScreenshotResult is the JSON result of game_screenshot.
type ScreenshotResult struct {
	Width         int           `json:"width"`
	Height        int           `json:"height"`
	Format        string        `json:"format"`
	Scene         string        `json:"scene,omitempty"`
	Path          string        `json:"path,omitempty"`
	Bytes         int           `json:"bytes"`
	PNGBase64     string        `json:"png_base64,omitempty"`
	OverlaysDrawn OverlaysDrawn `json:"overlays_drawn"`
}

// SetPhysics replaces the static 2D physics world configured on b.
// If Config.PhysicsFunc is set and returns a non-nil world, PhysicsFunc continues to take precedence.
func (b *Bridge) SetPhysics(w *physics.World) {
	if b == nil {
		return
	}
	b.cfg.Physics = w
}

// SetRigidPhysics replaces the static 2D rigid-body physics world configured on b.
// If Config.RigidPhysicsFunc is set and returns a non-nil world, RigidPhysicsFunc continues to take precedence.
func (b *Bridge) SetRigidPhysics(w *physics.RigidWorld) {
	if b == nil {
		return
	}
	b.cfg.RigidPhysics = w
}

func (b *Bridge) activePhysics() *physics.World {
	if b == nil {
		return nil
	}
	if b.cfg.PhysicsFunc != nil {
		if w := b.cfg.PhysicsFunc(); w != nil {
			return w
		}
	}
	return b.cfg.Physics
}

func (b *Bridge) activeRigidPhysics() *physics.RigidWorld {
	if b == nil {
		return nil
	}
	if b.cfg.RigidPhysicsFunc != nil {
		if w := b.cfg.RigidPhysicsFunc(); w != nil {
			return w
		}
	}
	return b.cfg.RigidPhysics
}

var schemaScreenshot = json.RawMessage(`{
	"type": "object",
	"additionalProperties": false,
	"properties": {
		"scene": {
			"type": "string",
			"description": "Optional scene name from game_scenes_list to capture; omit to capture the current active game view."
		},
		"width": {
			"type": "integer",
			"minimum": 1,
			"maximum": 8192,
			"description": "Output width in pixels (must be paired with height; omit to infer from CaptureFrame, Viewport or Camera)."
		},
		"height": {
			"type": "integer",
			"minimum": 1,
			"maximum": 8192,
			"description": "Output height in pixels (must be paired with width; omit to infer from CaptureFrame, Viewport or Camera)."
		},
		"format": {
			"type": "string",
			"enum": ["png"],
			"description": "Image encoding format (default \"png\")."
		},
		"path": {
			"type": "string",
			"description": "Optional relative file path inside Config.ScreenshotDir to write the PNG instead of returning png_base64."
		},
		"bounds": {
			"type": "boolean",
			"description": "Draw sprite bounding rectangles aligned with visible sprites in the scene."
		},
		"collision_shapes": {
			"type": "boolean",
			"description": "Draw 2D physics collision shapes (boxes, circles, triggers, rigid bodies and collision tiles) aligned with the scene camera and viewport."
		},
		"physics": {
			"type": "boolean",
			"description": "Alias for collision_shapes."
		},
		"grid": {
			"type": "boolean",
			"description": "Draw a world-space reference grid."
		},
		"camera": {
			"type": "boolean",
			"description": "Draw the active camera's visible frame and center crosshair."
		},
		"labels": {
			"type": "boolean",
			"description": "Draw compact text labels next to drawn sprite bounds and physics bodies."
		},
		"overlays": {
			"type": "object",
			"additionalProperties": false,
			"description": "Structured overlay flags merged with top-level overlay booleans.",
			"properties": {
				"bounds": {
					"type": "boolean",
					"description": "Draw sprite bounding rectangles aligned with visible sprites."
				},
				"collision_shapes": {
					"type": "boolean",
					"description": "Draw 2D physics collision shapes."
				},
				"physics": {
					"type": "boolean",
					"description": "Alias for collision_shapes."
				},
				"grid": {
					"type": "boolean",
					"description": "Draw a world-space reference grid."
				},
				"grid_step": {
					"type": "number",
					"exclusiveMinimum": 0,
					"description": "Spacing of the reference grid in world points (default 32)."
				},
				"camera": {
					"type": "boolean",
					"description": "Draw the active camera's visible frame and center crosshair."
				},
				"labels": {
					"type": "boolean",
					"description": "Draw compact text labels next to drawn overlays."
				}
			}
		}
	}
}`)

func (b *Bridge) screenshotTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolScreenshot,
			Description: "Capture the game view as a PNG image (returned as base64 or written inside ScreenshotDir), optionally with sprite bounds, physics collision shapes, grid, camera frame and labels aligned with sprites.",
			InputSchema: schemaScreenshot,
			Handler:     b.handleScreenshot,
		},
	}
}

type screenshotArgs struct {
	Scene           string              `json:"scene"`
	Width           int                 `json:"width"`
	Height          int                 `json:"height"`
	Format          string              `json:"format"`
	Path            string              `json:"path"`
	Bounds          bool                `json:"bounds"`
	CollisionShapes bool                `json:"collision_shapes"`
	Physics         bool                `json:"physics"`
	Grid            bool                `json:"grid"`
	Camera          bool                `json:"camera"`
	Labels          bool                `json:"labels"`
	Overlays        *ScreenshotOverlays `json:"overlays"`
}

func (b *Bridge) handleScreenshot(ctx context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[screenshotArgs](raw)
	if err != nil {
		return nil, err
	}

	format := args.Format
	if format == "" {
		format = "png"
	}
	if format != "png" {
		return nil, fmt.Errorf("mcp: unsupported screenshot format %q (supported: \"png\")", format)
	}

	if args.Width < 0 || args.Height < 0 {
		return nil, fmt.Errorf("mcp: screenshot dimensions must be positive (got %dx%d)", args.Width, args.Height)
	}
	if (args.Width == 0) != (args.Height == 0) {
		return nil, fmt.Errorf("mcp: width and height must both be provided when overriding dimensions (got %dx%d)", args.Width, args.Height)
	}
	if args.Width > maxScreenshotDim || args.Height > maxScreenshotDim || int64(args.Width)*int64(args.Height) > maxScreenshotPixels {
		return nil, fmt.Errorf("mcp: screenshot dimensions %dx%d exceed maximum (%dx%d, %d pixels)", args.Width, args.Height, maxScreenshotDim, maxScreenshotDim, maxScreenshotPixels)
	}

	gridStep := float32(defaultGridStep)
	if args.Overlays != nil && args.Overlays.GridStep != 0 {
		gs := float64(args.Overlays.GridStep)
		if math.IsNaN(gs) || math.IsInf(gs, 0) || gs <= 0 {
			return nil, fmt.Errorf("mcp: overlays.grid_step must be a positive finite number (got %v)", args.Overlays.GridStep)
		}
		gridStep = args.Overlays.GridStep
	}

	var cleanRelPath string
	if args.Path != "" {
		if strings.TrimSpace(b.cfg.ScreenshotDir) == "" {
			return nil, errors.New("mcp: screenshot path requested but Config.ScreenshotDir is not configured")
		}
		cleanRelPath, err = sanitizeScreenshotPath(args.Path)
		if err != nil {
			return nil, err
		}
	}

	wantBounds := args.Bounds || (args.Overlays != nil && args.Overlays.Bounds)
	wantCollision := args.CollisionShapes || args.Physics || (args.Overlays != nil && (args.Overlays.CollisionShapes || args.Overlays.Physics))
	wantGrid := args.Grid || (args.Overlays != nil && args.Overlays.Grid)
	wantCamera := args.Camera || (args.Overlays != nil && args.Overlays.Camera)
	wantLabels := args.Labels || (args.Overlays != nil && args.Overlays.Labels)

	sceneName, drawnScenes, err := b.resolveScreenshotScenes(args.Scene)
	if err != nil {
		return nil, err
	}
	activeCam := screenshotCamera(drawnScenes)

	var (
		img    *image.RGBA
		width  = args.Width
		height = args.Height
	)

	if b.cfg.CaptureFrame != nil && args.Scene == "" {
		captured, err := b.cfg.CaptureFrame(ctx)
		if err != nil {
			return nil, fmt.Errorf("mcp: capture frame: %w", err)
		}
		if captured == nil || captured.Bounds().Empty() {
			return nil, errors.New("mcp: CaptureFrame returned an empty image")
		}
		imgW, imgH := captured.Bounds().Dx(), captured.Bounds().Dy()
		if width > 0 && (width != imgW || height != imgH) {
			return nil, fmt.Errorf("mcp: CaptureFrame returned %dx%d image, does not match requested %dx%d", imgW, imgH, width, height)
		}
		if imgW > maxScreenshotDim || imgH > maxScreenshotDim || int64(imgW)*int64(imgH) > maxScreenshotPixels {
			return nil, fmt.Errorf("mcp: captured image dimensions %dx%d exceed maximum", imgW, imgH)
		}
		width, height = imgW, imgH
		img = cloneRGBA(captured)
	} else {
		if width == 0 && height == 0 {
			width, height, err = b.inferScreenshotSize(activeCam)
			if err != nil {
				return nil, err
			}
		}
		hasScene := len(drawnScenes) > 0 || (b.cfg.Scenes != nil && b.cfg.Scenes.Depth() > 0 && args.Scene == "")
		hasPhysics := b.activePhysics() != nil || b.activeRigidPhysics() != nil
		if b.cfg.DrawFrame == nil && !hasScene && !hasPhysics {
			return nil, errors.New("mcp: no screenshot source configured (set CaptureFrame, DrawFrame, Scene/Scenes, or Physics)")
		}

		var d core.Drawing
		switch {
		case args.Scene != "":
			b.drawScenesInto(&d, width, height, drawnScenes)
		case b.cfg.DrawFrame != nil:
			b.cfg.DrawFrame(&d, width, height)
		case b.cfg.Scenes != nil && b.cfg.Scenes.Depth() > 0:
			b.drawManagerInto(&d, width, height)
		default:
			b.drawScenesInto(&d, width, height, drawnScenes)
		}
		img = rasterizeOps(width, height, d.Ops, b.cfg.ScreenshotBackground)
	}

	drawn := OverlaysDrawn{
		Bounds:          wantBounds,
		CollisionShapes: wantCollision,
		Grid:            wantGrid,
		Camera:          wantCamera,
		Labels:          wantLabels,
	}

	if wantBounds || wantCollision || wantGrid || wantCamera || wantLabels {
		b.compositeOverlays(img, drawnScenes, activeCam, overlayRequest{
			bounds:          wantBounds,
			collisionShapes: wantCollision,
			grid:            wantGrid,
			gridStep:        gridStep,
			camera:          wantCamera,
			labels:          wantLabels,
		}, &drawn)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("mcp: encode screenshot png: %w", err)
	}
	pngBytes := buf.Bytes()

	res := ScreenshotResult{
		Width:         width,
		Height:        height,
		Format:        format,
		Scene:         sceneName,
		Bytes:         len(pngBytes),
		OverlaysDrawn: drawn,
	}

	if cleanRelPath != "" {
		if err := writeConfinedFile(b.cfg.ScreenshotDir, cleanRelPath, pngBytes); err != nil {
			return nil, err
		}
		res.Path = cleanRelPath
	} else {
		res.PNGBase64 = base64.StdEncoding.EncodeToString(pngBytes)
	}
	return res, nil
}

func (b *Bridge) resolveScreenshotScenes(requested string) (string, []*scene.Scene, error) {
	_, resolved := b.collectScenes()
	if requested != "" {
		var target *resolvedScene
		for i := len(resolved) - 1; i >= 0; i-- {
			if resolved[i].info.Active && resolved[i].info.Name == requested {
				target = &resolved[i]
				break
			}
		}
		if target == nil {
			for i := range resolved {
				if resolved[i].info.Name == requested {
					target = &resolved[i]
					break
				}
			}
		}
		if target == nil {
			avail := make([]string, len(resolved))
			for i, r := range resolved {
				avail[i] = r.info.Name
			}
			if len(avail) == 0 {
				return "", nil, fmt.Errorf("mcp: scene %q not found (no scenes configured)", requested)
			}
			return "", nil, fmt.Errorf("mcp: scene %q not found (available: %s)", requested, strings.Join(avail, ", "))
		}
		if target.scene == nil {
			return target.info.Name, nil, nil
		}
		return target.info.Name, []*scene.Scene{target.scene}, nil
	}

	if b.cfg.Scenes != nil && b.cfg.Scenes.Depth() > 0 {
		var active []resolvedScene
		for _, r := range resolved {
			if r.info.Active {
				active = append(active, r)
			}
		}
		if len(active) == 0 {
			return "", nil, nil
		}
		start := len(active) - 1
		for start > 0 && active[start].info.Transparent {
			start--
		}
		var out []*scene.Scene
		for i := start; i < len(active); i++ {
			if active[i].scene != nil {
				out = append(out, active[i].scene)
			}
		}
		return active[len(active)-1].info.Name, out, nil
	}

	if sc := b.standaloneScene(); sc != nil {
		return b.nameForStandaloneScene(sc), []*scene.Scene{sc}, nil
	}
	return "", nil, nil
}

func screenshotCamera(scenes []*scene.Scene) *tilemap.Camera {
	for i := len(scenes) - 1; i >= 0; i-- {
		if scenes[i] != nil && scenes[i].Camera != nil {
			return scenes[i].Camera
		}
	}
	return nil
}

func (b *Bridge) inferScreenshotSize(cam *tilemap.Camera) (int, int, error) {
	if b.cfg.Viewport != nil && b.cfg.Viewport.Width > 0 && b.cfg.Viewport.Height > 0 {
		w := int(math.Round(float64(b.cfg.Viewport.Width)))
		h := int(math.Round(float64(b.cfg.Viewport.Height)))
		if w > 0 && h > 0 {
			return w, h, nil
		}
	}
	if cam != nil && cam.Width > 0 && cam.Height > 0 {
		w := int(math.Round(float64(cam.Width)))
		h := int(math.Round(float64(cam.Height)))
		if w > 0 && h > 0 {
			return w, h, nil
		}
	}
	return 0, 0, errors.New("mcp: cannot determine screenshot dimensions (pass width and height, or configure Viewport, a scene Camera with Width/Height, or CaptureFrame)")
}

func (b *Bridge) drawManagerInto(d *core.Drawing, width, height int) {
	draw := func(dst *core.Drawing, size core.Size) {
		cur := b.cfg.Scenes.Current()
		if cur != nil && cur.Draw == nil && cur.Scene == nil {
			if sc := b.standaloneScene(); sc != nil {
				sc.Draw(dst)
				return
			}
		}
		b.cfg.Scenes.Draw(dst, size, 0)
	}
	win := core.Size{Width: core.Points(width), Height: core.Points(height)}
	if b.cfg.Viewport != nil {
		b.cfg.Viewport.Draw(d, win, draw)
		return
	}
	draw(d, win)
}

func (b *Bridge) drawScenesInto(d *core.Drawing, width, height int, scenes []*scene.Scene) {
	if len(scenes) == 0 {
		return
	}
	draw := func(dst *core.Drawing, _ core.Size) {
		for _, sc := range scenes {
			if sc != nil {
				sc.Draw(dst)
			}
		}
	}
	win := core.Size{Width: core.Points(width), Height: core.Points(height)}
	if b.cfg.Viewport != nil {
		b.cfg.Viewport.Draw(d, win, draw)
		return
	}
	draw(d, win)
}

func sanitizeScreenshotPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("mcp: empty screenshot path")
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("mcp: invalid screenshot path %q: contains NUL byte", p)
	}
	if strings.ContainsAny(p, "\\:") {
		return "", fmt.Errorf("mcp: invalid screenshot path %q: must be a relative slash-separated path", p)
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("mcp: invalid screenshot path %q: absolute paths are not allowed", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("mcp: invalid screenshot path %q: parent traversal is not allowed", p)
		}
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("mcp: invalid screenshot path %q: path escapes ScreenshotDir", p)
	}
	return cleaned, nil
}

func writeConfinedFile(dir, relSlash string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mcp: create ScreenshotDir: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("mcp: open ScreenshotDir: %w", err)
	}
	defer root.Close()

	relOS := filepath.FromSlash(relSlash)
	if parent := path.Dir(relSlash); parent != "." && parent != "" {
		var cur string
		for _, seg := range strings.Split(parent, "/") {
			if cur == "" {
				cur = seg
			} else {
				cur = filepath.Join(cur, seg)
			}
			if err := root.Mkdir(cur, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("mcp: create screenshot directory %q: %w", cur, err)
			}
		}
	}
	f, err := root.OpenFile(relOS, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("mcp: write screenshot %q: %w", relSlash, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("mcp: write screenshot %q: %w", relSlash, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("mcp: close screenshot %q: %w", relSlash, err)
	}
	return nil
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		sOff := src.PixOffset(sb.Min.X, sb.Min.Y+y)
		dOff := dst.PixOffset(0, y)
		copy(dst.Pix[dOff:dOff+w*4], src.Pix[sOff:sOff+w*4])
	}
	return dst
}

// --- Overlay compositing ---

type overlayRequest struct {
	bounds          bool
	collisionShapes bool
	grid            bool
	gridStep        float32
	camera          bool
	labels          bool
}

type overlayLabel struct {
	x, y int
	text string
}

func (b *Bridge) viewportTransform(width, height int) (affine2D, image.Rectangle) {
	full := image.Rect(0, 0, width, height)
	if b.cfg.Viewport == nil {
		return identityAffine2D(), full
	}
	win := core.Size{Width: core.Points(width), Height: core.Points(height)}
	pl := b.cfg.Viewport.Layout(win)
	if pl.ScaleX <= 0 || pl.ScaleY <= 0 {
		return identityAffine2D(), image.Rectangle{}
	}
	m := affine2D{
		a: float64(pl.ScaleX),
		d: float64(pl.ScaleY),
		e: float64(pl.Rect.Origin.X),
		f: float64(pl.Rect.Origin.Y),
	}
	clip := full
	if len(pl.Bars) > 0 {
		r := image.Rect(
			int(math.Ceil(float64(pl.Rect.Origin.X)-0.5)),
			int(math.Ceil(float64(pl.Rect.Origin.Y)-0.5)),
			int(math.Ceil(float64(pl.Rect.Origin.X+pl.Rect.Size.Width)-0.5)),
			int(math.Ceil(float64(pl.Rect.Origin.Y+pl.Rect.Size.Height)-0.5)),
		)
		clip = full.Intersect(r)
	}
	return m, clip
}

func cameraAffine2D(cam *tilemap.Camera) affine2D {
	if cam == nil {
		return identityAffine2D()
	}
	var d core.Drawing
	cam.Apply(&d)
	m := identityAffine2D()
	for _, op := range d.Ops {
		m = applyTransformOp(m, op)
	}
	return m
}

func (b *Bridge) compositeOverlays(img *image.RGBA, scenes []*scene.Scene, activeCam *tilemap.Camera, req overlayRequest, drawn *OverlaysDrawn) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	vpM, vpClip := b.viewportTransform(w, h)
	if vpClip.Empty() {
		return
	}
	style := DefaultOverlayStyle()
	worldM := vpM.mul(cameraAffine2D(activeCam))

	var labels []overlayLabel

	if req.grid {
		drawGridOverlay(img, vpClip, worldM, float64(req.gridStep), style.GridColor)
	}

	if req.camera && activeCam != nil {
		drawCameraOverlay(img, vpClip, worldM, activeCam, style.CameraColor)
	}

	if req.bounds {
		drawSceneBoundsOverlays(img, vpClip, vpM, scenes, style.BoundsColor, req.labels, &labels, drawn)
	}

	if req.collisionShapes {
		b.drawPhysicsOverlays(img, vpClip, worldM, activeCam, style, req.labels, &labels, drawn)
	}

	if req.labels && len(labels) > 0 {
		for _, lbl := range labels {
			drawTinyLabel(img, vpClip, lbl.x, lbl.y, lbl.text, style.LabelTextColor, style.LabelBgColor)
		}
	}
}

func drawGridOverlay(dst *image.RGBA, clip image.Rectangle, worldM affine2D, step float64, col core.Color) {
	if step <= 0 {
		step = defaultGridStep
	}
	inv, ok := worldM.invert()
	if !ok {
		return
	}
	// Map the four clip corners back to world space to find the visible world range.
	x0, y0 := inv.apply(float64(clip.Min.X), float64(clip.Min.Y))
	x1, y1 := inv.apply(float64(clip.Max.X), float64(clip.Min.Y))
	x2, y2 := inv.apply(float64(clip.Max.X), float64(clip.Max.Y))
	x3, y3 := inv.apply(float64(clip.Min.X), float64(clip.Max.Y))

	minX := min(min(x0, x1), min(x2, x3))
	maxX := max(max(x0, x1), max(x2, x3))
	minY := min(min(y0, y1), min(y2, y3))
	maxY := max(max(y0, y1), max(y2, y3))

	// Double spacing if zoomed far out so grid drawing stays bounded.
	for (maxX-minX)/step > 256 || (maxY-minY)/step > 256 {
		step *= 2
	}

	sr, sg, sb, sa := premulFromColor(col, 1)
	if sa <= 0 {
		return
	}

	startX := math.Floor(minX/step) * step
	for gx := startX; gx <= maxX; gx += step {
		ax, ay := worldM.apply(gx, minY)
		bx, by := worldM.apply(gx, maxY)
		drawLineBresenham(dst, clip, int(math.Floor(ax)), int(math.Floor(ay)), int(math.Floor(bx)), int(math.Floor(by)), sr, sg, sb, sa)
	}
	startY := math.Floor(minY/step) * step
	for gy := startY; gy <= maxY; gy += step {
		ax, ay := worldM.apply(minX, gy)
		bx, by := worldM.apply(maxX, gy)
		drawLineBresenham(dst, clip, int(math.Floor(ax)), int(math.Floor(ay)), int(math.Floor(bx)), int(math.Floor(by)), sr, sg, sb, sa)
	}
}

func drawCameraOverlay(dst *image.RGBA, clip image.Rectangle, worldM affine2D, cam *tilemap.Camera, col core.Color) {
	vb := cam.VisibleBounds()
	drawCrispRectOutline(dst, clip, worldM, float64(vb.Origin.X), float64(vb.Origin.Y), float64(vb.Size.Width), float64(vb.Size.Height), col)
	shake := cam.ShakeOffset()
	cx, cy := worldM.apply(float64(cam.X)+float64(shake.X), float64(cam.Y)+float64(shake.Y))
	px, py := int(math.Floor(cx)), int(math.Floor(cy))
	sr, sg, sb, sa := premulFromColor(col, 1)
	drawLineBresenham(dst, clip, px-4, py, px+4, py, sr, sg, sb, sa)
	drawLineBresenham(dst, clip, px, py-4, px, py+4, sr, sg, sb, sa)
}

func drawSceneBoundsOverlays(dst *image.RGBA, clip image.Rectangle, vpM affine2D, scenes []*scene.Scene, col core.Color, wantLabels bool, labels *[]overlayLabel, drawn *OverlaysDrawn) {
	for _, sc := range scenes {
		if sc == nil || len(sc.Layers) == 0 {
			continue
		}
		layers := make([]*scene.Layer, 0, len(sc.Layers))
		for _, l := range sc.Layers {
			if l != nil && l.Visible {
				layers = append(layers, l)
			}
		}
		slices.SortStableFunc(layers, func(a, b *scene.Layer) int {
			return cmp.Compare(a.Order, b.Order)
		})

		for _, l := range layers {
			drawLayerSpriteBounds(dst, clip, vpM, sc.Camera, l, col, wantLabels, labels, drawn)
		}
	}
}

func drawLayerSpriteBounds(dst *image.RGBA, clip image.Rectangle, vpM affine2D, cam *tilemap.Camera, l *scene.Layer, col core.Color, wantLabels bool, labels *[]overlayLabel, drawn *OverlaysDrawn) {
	if len(l.Items) == 0 {
		return
	}
	drawItemsWith := func(layerM affine2D) {
		for _, it := range l.Items {
			if it.Batch == nil || it.Batch.Atlas == nil {
				continue
			}
			for _, sp := range it.Batch.Sprites {
				if !spriteVisible(sp) {
					continue
				}
				r := sp.Bounds()
				if r.W <= 0 || r.H <= 0 {
					continue
				}
				x0, y0, ok := drawCrispRectOutline(dst, clip, layerM, float64(r.X), float64(r.Y), float64(r.W), float64(r.H), col)
				drawn.SpriteBounds++
				if wantLabels && ok {
					text := sp.Frame.Name
					if text == "" {
						text = l.Name
					}
					if text == "" {
						text = "SPR"
					}
					*labels = append(*labels, overlayLabel{x: x0, y: y0, text: text})
				}
			}
		}
	}

	if l.Space == scene.SpaceWorld && cam != nil {
		px, py := l.Parallax()
		pcam := cam.Parallax(px, py)
		pcam.X -= l.OffsetX
		pcam.Y -= l.OffsetY
		baseM := vpM.mul(cameraAffine2D(&pcam))
		minCol, maxCol, minRow, maxRow, rx, ry := layerRepeatRanges(l, pcam.VisibleBounds())
		if !rx && !ry {
			drawItemsWith(baseM)
			return
		}
		for row := minRow; row <= maxRow; row++ {
			var ty float64
			if ry {
				ty = float64(row) * float64(l.RepeatHeight)
			}
			for colIdx := minCol; colIdx <= maxCol; colIdx++ {
				var tx float64
				if rx {
					tx = float64(colIdx) * float64(l.RepeatWidth)
				}
				drawItemsWith(baseM.mul(affine2D{a: 1, d: 1, e: tx, f: ty}))
			}
		}
		return
	}

	layerM := vpM
	if l.OffsetX != 0 || l.OffsetY != 0 {
		layerM = layerM.mul(affine2D{a: 1, d: 1, e: float64(l.OffsetX), f: float64(l.OffsetY)})
	}
	drawItemsWith(layerM)
}

func spriteVisible(sp *sprite.Sprite) bool {
	if sp == nil {
		return false
	}
	f := sp.Frame
	if f.W <= 0 || f.H <= 0 || sp.Alpha <= 0 || sp.ScaleX == 0 || sp.ScaleY == 0 {
		return false
	}
	if sp.Tint != 0 && sp.Tint != core.RGB(0xffffff) && sp.Tint&0xff == 0 {
		return false
	}
	for _, v := range [...]float32{sp.X, sp.Y, sp.OriginX, sp.OriginY, sp.Rotation, sp.ScaleX, sp.ScaleY, sp.Alpha} {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
	}
	return true
}

func layerRepeatRanges(l *scene.Layer, vb core.Bounds) (minCol, maxCol, minRow, maxRow int, repeatX, repeatY bool) {
	if l.RepeatX && l.RepeatWidth > 0 && vb.Size.Width > 0 {
		w := float64(l.RepeatWidth)
		minX := float64(vb.Origin.X)
		maxX := minX + float64(vb.Size.Width)
		minCol = int(math.Floor(minX / w))
		maxCol = int(math.Ceil(maxX/w)) - 1
		if maxCol < minCol {
			maxCol = minCol
		}
		repeatX = true
	}
	if l.RepeatY && l.RepeatHeight > 0 && vb.Size.Height > 0 {
		h := float64(l.RepeatHeight)
		minY := float64(vb.Origin.Y)
		maxY := minY + float64(vb.Size.Height)
		minRow = int(math.Floor(minY / h))
		maxRow = int(math.Ceil(maxY/h)) - 1
		if maxRow < minRow {
			maxRow = minRow
		}
		repeatY = true
	}
	return minCol, maxCol, minRow, maxRow, repeatX, repeatY
}

func (b *Bridge) drawPhysicsOverlays(dst *image.RGBA, clip image.Rectangle, worldM affine2D, cam *tilemap.Camera, style OverlayStyle, wantLabels bool, labels *[]overlayLabel, drawn *OverlaysDrawn) {
	if pw := b.activePhysics(); pw != nil {
		if pw.Tiles != nil {
			vis := visibleWorldBounds(clip, worldM, cam)
			drawPhysicsTilesOverlay(dst, clip, worldM, pw.Tiles, vis, style, drawn)
		}
		for _, body := range pw.Bodies {
			if body == nil || body.Shape.Empty() {
				continue
			}
			x0, y0, ok := drawPhysicsShapeOverlay(dst, clip, worldM, body.Shape, style.BoxColor, style.CircleColor, drawn, false)
			if wantLabels && ok && body.Tag != "" {
				*labels = append(*labels, overlayLabel{x: x0, y: y0, text: body.Tag})
			}
		}
		for _, tr := range pw.Triggers {
			if tr == nil || tr.Shape.Empty() {
				continue
			}
			x0, y0, ok := drawPhysicsShapeOverlay(dst, clip, worldM, tr.Shape, style.TriggerColor, style.TriggerColor, drawn, true)
			if wantLabels && ok && tr.Tag != "" {
				*labels = append(*labels, overlayLabel{x: x0, y: y0, text: tr.Tag})
			}
		}
	}

	if rw := b.activeRigidPhysics(); rw != nil {
		for _, rb := range rw.Bodies {
			if rb == nil || rb.Shape.Empty() {
				continue
			}
			bodyM := worldM
			if rb.Angle != 0 {
				bodyM = worldM.mul(rotateAroundAffine2D(float64(rb.X), float64(rb.Y), float64(rb.Angle)))
			}
			x0, y0, ok := drawPhysicsShapeOverlay(dst, clip, bodyM, rb.Shape, style.BoxColor, style.CircleColor, drawn, false)
			if wantLabels && ok && rb.Tag != "" {
				*labels = append(*labels, overlayLabel{x: x0, y: y0, text: rb.Tag})
			}
		}
	}
}

func visibleWorldBounds(clip image.Rectangle, worldM affine2D, cam *tilemap.Camera) core.Bounds {
	if cam != nil {
		return cam.VisibleBounds()
	}
	inv, ok := worldM.invert()
	if !ok {
		return core.Bounds{}
	}
	x0, y0 := inv.apply(float64(clip.Min.X), float64(clip.Min.Y))
	x1, y1 := inv.apply(float64(clip.Max.X), float64(clip.Min.Y))
	x2, y2 := inv.apply(float64(clip.Max.X), float64(clip.Max.Y))
	x3, y3 := inv.apply(float64(clip.Min.X), float64(clip.Max.Y))
	minX := min(min(x0, x1), min(x2, x3))
	maxX := max(max(x0, x1), max(x2, x3))
	minY := min(min(y0, y1), min(y2, y3))
	maxY := max(max(y0, y1), max(y2, y3))
	return core.Bounds{
		Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
		Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
	}
}

func drawPhysicsTilesOverlay(dst *image.RGBA, clip image.Rectangle, worldM affine2D, grid physics.TileGrid, visible core.Bounds, style OverlayStyle, drawn *OverlaysDrawn) {
	if grid == nil || visible.Size.Width <= 0 || visible.Size.Height <= 0 {
		return
	}
	tileW, tileH := grid.TileSize()
	cols, rows := grid.GridSize()
	if tileW <= 0 || tileH <= 0 || cols <= 0 || rows <= 0 {
		return
	}
	tw, th := float64(tileW), float64(tileH)
	visMinX := float64(visible.Origin.X)
	visMinY := float64(visible.Origin.Y)
	visMaxX := visMinX + float64(visible.Size.Width)
	visMaxY := visMinY + float64(visible.Size.Height)

	minCol := max(0, int(math.Floor(visMinX/tw)))
	maxCol := min(cols-1, int(math.Ceil(visMaxX/tw))-1)
	minRow := max(0, int(math.Floor(visMinY/th)))
	maxRow := min(rows-1, int(math.Ceil(visMaxY/th))-1)
	if minCol > maxCol || minRow > maxRow {
		return
	}

	owR, owG, owB, owA := premulFromColor(style.OneWayTileColor, 1)
	tick := th * 0.25

	for row := minRow; row <= maxRow; row++ {
		y := float64(row) * th
		for col := minCol; col <= maxCol; col++ {
			flags := grid.TileFlagsAt(col, row)
			if flags&physics.TileSolid != 0 {
				drawCrispRectOutline(dst, clip, worldM, float64(col)*tw, y, tw, th, style.SolidTileColor)
				drawn.PhysicsTiles++
			} else if flags&physics.TileOneWay != 0 {
				drawOneWayTileOverlay(dst, clip, worldM, float64(col)*tw, y, tw, tick, owR, owG, owB, owA)
				drawn.PhysicsTiles++
			}
		}
	}
}

func drawPhysicsShapeOverlay(dst *image.RGBA, clip image.Rectangle, m affine2D, s physics.Shape, boxColor, circleColor core.Color, drawn *OverlaysDrawn, isTrigger bool) (int, int, bool) {
	switch s.Kind {
	case physics.ShapeBox:
		b := s.Box
		if b.Empty() {
			return 0, 0, false
		}
		x0, y0, ok := drawCrispRectOutline(dst, clip, m, float64(b.X), float64(b.Y), float64(b.W), float64(b.H), boxColor)
		if isTrigger {
			drawn.PhysicsTriggers++
		} else {
			drawn.PhysicsBoxes++
		}
		return x0, y0, ok
	case physics.ShapeCircle:
		c := s.Circle
		if c.Empty() {
			return 0, 0, false
		}
		x0, y0, ok := drawCrispCircleOutline(dst, clip, m, float64(c.X), float64(c.Y), float64(c.R), circleColor)
		if isTrigger {
			drawn.PhysicsTriggers++
		} else {
			drawn.PhysicsCircles++
		}
		return x0, y0, ok
	default:
		return 0, 0, false
	}
}

func drawOneWayTileOverlay(dst *image.RGBA, clip image.Rectangle, m affine2D, x, y, tw, tick, sr, sg, sb, sa float64) {
	if math.Abs(m.b) < 1e-6 && math.Abs(m.c) < 1e-6 {
		sx0, sy0 := m.apply(x, y)
		sx1, sy1 := m.apply(x+tw, y+tick)
		minX, maxX := min(sx0, sx1), max(sx0, sx1)
		minY, maxY := min(sy0, sy1), max(sy0, sy1)
		px0 := int(math.Ceil(minX - 0.5))
		px1 := max(px0, int(math.Ceil(maxX-0.5))-1)
		py0 := int(math.Ceil(minY - 0.5))
		py1 := max(py0, int(math.Ceil(maxY-0.5))-1)
		drawLineBresenham(dst, clip, px0, py1, px0, py0, sr, sg, sb, sa)
		drawLineBresenham(dst, clip, px0, py0, px1, py0, sr, sg, sb, sa)
		drawLineBresenham(dst, clip, px1, py0, px1, py1, sr, sg, sb, sa)
		return
	}
	drawWorldSegment(dst, clip, m, x, y+tick, x, y, sr, sg, sb, sa)
	drawWorldSegment(dst, clip, m, x, y, x+tw, y, sr, sg, sb, sa)
	drawWorldSegment(dst, clip, m, x+tw, y, x+tw, y+tick, sr, sg, sb, sa)
}

func drawWorldSegment(dst *image.RGBA, clip image.Rectangle, m affine2D, x0, y0, x1, y1, sr, sg, sb, sa float64) {
	sx0, sy0 := m.apply(x0, y0)
	sx1, sy1 := m.apply(x1, y1)
	drawLineBresenham(dst, clip, int(math.Floor(sx0)), int(math.Floor(sy0)), int(math.Floor(sx1)), int(math.Floor(sy1)), sr, sg, sb, sa)
}

// drawCrispRectOutline draws a pixel-aligned 1-pixel border matching the exact pixel coverage of
// a filled rectangle [x, x+w) x [y, y+h) transformed by m, clipped to clip.
// It returns the top-left screen pixel coordinate and whether any part of the rectangle lies within clip.
func drawCrispRectOutline(dst *image.RGBA, clip image.Rectangle, m affine2D, x, y, w, h float64, col core.Color) (int, int, bool) {
	x, y, w, h = normalizeRect2D(x, y, w, h)
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	sr, sg, sb, sa := premulFromColor(col, 1)
	if sa <= 0 {
		return 0, 0, false
	}

	// Axis-aligned fast path (covers all unrotated cameras/viewports/sprites/boxes).
	if math.Abs(m.b) < 1e-6 && math.Abs(m.c) < 1e-6 {
		sx0, sy0 := m.apply(x, y)
		sx1, sy1 := m.apply(x+w, y+h)
		minX, maxX := min(sx0, sx1), max(sx0, sx1)
		minY, maxY := min(sy0, sy1), max(sy0, sy1)

		// Exact inclusive pixel bounds matching pixel-center (px+0.5, py+0.5) coverage of [minX, maxX) x [minY, maxY).
		px0 := int(math.Ceil(minX - 0.5))
		px1 := int(math.Ceil(maxX-0.5)) - 1
		py0 := int(math.Ceil(minY - 0.5))
		py1 := int(math.Ceil(maxY-0.5)) - 1
		if px1 < px0 {
			px1 = px0
		}
		if py1 < py0 {
			py1 = py0
		}
		if px1 < clip.Min.X || px0 >= clip.Max.X || py1 < clip.Min.Y || py0 >= clip.Max.Y {
			return px0, py0, false
		}

		// Top and bottom horizontal edges.
		hx0, hx1 := max(px0, clip.Min.X), min(px1, clip.Max.X-1)
		if py0 >= clip.Min.Y && py0 < clip.Max.Y {
			for px := hx0; px <= hx1; px++ {
				blendOver2D(dst, px, py0, sr, sg, sb, sa)
			}
		}
		if py1 != py0 && py1 >= clip.Min.Y && py1 < clip.Max.Y {
			for px := hx0; px <= hx1; px++ {
				blendOver2D(dst, px, py1, sr, sg, sb, sa)
			}
		}
		// Left and right vertical edges (skip corners already blended on top/bottom).
		vy0, vy1 := max(py0+1, clip.Min.Y), min(py1-1, clip.Max.Y-1)
		if px0 >= clip.Min.X && px0 < clip.Max.X {
			for py := vy0; py <= vy1; py++ {
				blendOver2D(dst, px0, py, sr, sg, sb, sa)
			}
		}
		if px1 != px0 && px1 >= clip.Min.X && px1 < clip.Max.X {
			for py := vy0; py <= vy1; py++ {
				blendOver2D(dst, px1, py, sr, sg, sb, sa)
			}
		}
		return px0, py0, true
	}

	// Rotated / sheared path: transform all four corners and draw four crisp 1px edges.
	c0x, c0y := m.apply(x, y)
	c1x, c1y := m.apply(x+w, y)
	c2x, c2y := m.apply(x+w, y+h)
	c3x, c3y := m.apply(x, y+h)

	p0x, p0y := int(math.Floor(c0x)), int(math.Floor(c0y))
	p1x, p1y := int(math.Floor(c1x)), int(math.Floor(c1y))
	p2x, p2y := int(math.Floor(c2x)), int(math.Floor(c2y))
	p3x, p3y := int(math.Floor(c3x)), int(math.Floor(c3y))

	minPX := min(min(p0x, p1x), min(p2x, p3x))
	maxPX := max(max(p0x, p1x), max(p2x, p3x))
	minPY := min(min(p0y, p1y), min(p2y, p3y))
	maxPY := max(max(p0y, p1y), max(p2y, p3y))
	if maxPX < clip.Min.X || minPX >= clip.Max.X || maxPY < clip.Min.Y || minPY >= clip.Max.Y {
		return minPX, minPY, false
	}

	drawLineBresenham(dst, clip, p0x, p0y, p1x, p1y, sr, sg, sb, sa)
	drawLineBresenham(dst, clip, p1x, p1y, p2x, p2y, sr, sg, sb, sa)
	drawLineBresenham(dst, clip, p2x, p2y, p3x, p3y, sr, sg, sb, sa)
	drawLineBresenham(dst, clip, p3x, p3y, p0x, p0y, sr, sg, sb, sa)
	return minPX, minPY, true
}

// drawCrispCircleOutline draws a closed 1-pixel circle/ellipse outline aligned to [cx-r, cx+r) x [cy-r, cy+r)
// under transform m, plus the +X orientation radius tick matching physics.DrawShape.
func drawCrispCircleOutline(dst *image.RGBA, clip image.Rectangle, m affine2D, cx, cy, r float64, col core.Color) (int, int, bool) {
	if r <= 0 {
		return 0, 0, false
	}
	sr, sg, sb, sa := premulFromColor(col, 1)
	if sa <= 0 {
		return 0, 0, false
	}

	scx, scy := m.apply(cx, cy)
	ux, uy := m.a*r, m.b*r
	vx, vy := m.c*r, m.d*r
	rx := math.Hypot(ux, uy)
	ry := math.Hypot(vx, vy)
	maxR := max(rx, ry)
	if maxR <= 0 {
		return 0, 0, false
	}

	// Inset semi-axes by 0.5px so the 1px border stays strictly inside the circle's bounding pixels.
	if rx > 0.5 {
		scale := (rx - 0.5) / rx
		ux *= scale
		uy *= scale
	}
	if ry > 0.5 {
		scale := (ry - 0.5) / ry
		vx *= scale
		vy *= scale
	}

	minPX := int(math.Floor(scx - maxR))
	maxPX := int(math.Ceil(scx + maxR))
	minPY := int(math.Floor(scy - maxR))
	maxPY := int(math.Ceil(scy + maxR))
	if maxPX < clip.Min.X || minPX >= clip.Max.X || maxPY < clip.Min.Y || minPY >= clip.Max.Y {
		return minPX, minPY, false
	}

	steps := max(32, int(math.Ceil(2*math.Pi*maxR*2)))
	prevX := int(math.Floor(scx + ux))
	prevY := int(math.Floor(scy + uy))
	for i := 1; i <= steps; i++ {
		ang := 2 * math.Pi * float64(i) / float64(steps)
		sn, cs := math.Sincos(ang)
		nx := int(math.Floor(scx + ux*cs + vx*sn))
		ny := int(math.Floor(scy + uy*cs + vy*sn))
		if nx != prevX || ny != prevY {
			drawLineBresenham(dst, clip, prevX, prevY, nx, ny, sr, sg, sb, sa)
			prevX, prevY = nx, ny
		}
	}

	// Draw +X orientation tick from center to +X edge (matching physics.DrawShape).
	drawLineBresenham(dst, clip, int(math.Floor(scx)), int(math.Floor(scy)), int(math.Floor(scx+ux)), int(math.Floor(scy+uy)), sr, sg, sb, sa)
	return minPX, minPY, true
}

func drawLineBresenham(dst *image.RGBA, clip image.Rectangle, x0, y0, x1, y1 int, sr, sg, sb, sa float64) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx - dy
	for {
		if x0 >= clip.Min.X && x0 < clip.Max.X && y0 >= clip.Min.Y && y0 < clip.Max.Y {
			blendOver2D(dst, x0, y0, sr, sg, sb, sa)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// --- Compact 3x5 bitmap font for diagnostic overlay labels ---

var tinyGlyphs = map[rune][5]uint8{
	'0': {0b111, 0b101, 0b101, 0b101, 0b111},
	'1': {0b010, 0b110, 0b010, 0b010, 0b111},
	'2': {0b111, 0b001, 0b111, 0b100, 0b111},
	'3': {0b111, 0b001, 0b111, 0b001, 0b111},
	'4': {0b101, 0b101, 0b111, 0b001, 0b001},
	'5': {0b111, 0b100, 0b111, 0b001, 0b111},
	'6': {0b111, 0b100, 0b111, 0b101, 0b111},
	'7': {0b111, 0b001, 0b001, 0b010, 0b010},
	'8': {0b111, 0b101, 0b111, 0b101, 0b111},
	'9': {0b111, 0b101, 0b111, 0b001, 0b111},
	'A': {0b010, 0b101, 0b111, 0b101, 0b101},
	'B': {0b110, 0b101, 0b110, 0b101, 0b110},
	'C': {0b011, 0b100, 0b100, 0b100, 0b011},
	'D': {0b110, 0b101, 0b101, 0b101, 0b110},
	'E': {0b111, 0b100, 0b110, 0b100, 0b111},
	'F': {0b111, 0b100, 0b110, 0b100, 0b100},
	'G': {0b011, 0b100, 0b101, 0b101, 0b011},
	'H': {0b101, 0b101, 0b111, 0b101, 0b101},
	'I': {0b111, 0b010, 0b010, 0b010, 0b111},
	'J': {0b001, 0b001, 0b001, 0b101, 0b010},
	'K': {0b101, 0b101, 0b110, 0b101, 0b101},
	'L': {0b100, 0b100, 0b100, 0b100, 0b111},
	'M': {0b101, 0b111, 0b101, 0b101, 0b101},
	'N': {0b110, 0b101, 0b101, 0b101, 0b101},
	'O': {0b010, 0b101, 0b101, 0b101, 0b010},
	'P': {0b110, 0b101, 0b110, 0b100, 0b100},
	'Q': {0b010, 0b101, 0b101, 0b011, 0b001},
	'R': {0b110, 0b101, 0b110, 0b101, 0b101},
	'S': {0b011, 0b100, 0b010, 0b001, 0b110},
	'T': {0b111, 0b010, 0b010, 0b010, 0b010},
	'U': {0b101, 0b101, 0b101, 0b101, 0b111},
	'V': {0b101, 0b101, 0b101, 0b101, 0b010},
	'W': {0b101, 0b101, 0b101, 0b111, 0b101},
	'X': {0b101, 0b101, 0b010, 0b101, 0b101},
	'Y': {0b101, 0b101, 0b010, 0b010, 0b010},
	'Z': {0b111, 0b001, 0b010, 0b100, 0b111},
	'-': {0b000, 0b000, 0b111, 0b000, 0b000},
	'_': {0b000, 0b000, 0b000, 0b000, 0b111},
	':': {0b000, 0b010, 0b000, 0b010, 0b000},
	'.': {0b000, 0b000, 0b000, 0b000, 0b010},
	'/': {0b001, 0b001, 0b010, 0b100, 0b100},
	' ': {0b000, 0b000, 0b000, 0b000, 0b000},
}

func drawTinyLabel(dst *image.RGBA, clip image.Rectangle, x, y int, text string, fg, bg core.Color) {
	text = strings.ToUpper(strings.TrimSpace(text))
	if text == "" {
		return
	}
	if len(text) > 16 {
		text = text[:16]
	}
	runes := []rune(text)
	pillW := len(runes)*4 + 1
	pillH := 7

	// Place label immediately above (x, y) when room exists, otherwise inside top-left.
	ox := x
	oy := y - pillH - 1
	if oy < clip.Min.Y {
		oy = y + 1
	}
	if ox < clip.Min.X {
		ox = clip.Min.X
	}
	if ox+pillW > clip.Max.X {
		ox = max(clip.Min.X, clip.Max.X-pillW)
	}
	if oy+pillH > clip.Max.Y {
		oy = max(clip.Min.Y, clip.Max.Y-pillH)
	}

	br, bgC, bb, ba := premulFromColor(bg, 1)
	if ba > 0 {
		for py := max(oy, clip.Min.Y); py < min(oy+pillH, clip.Max.Y); py++ {
			for px := max(ox, clip.Min.X); px < min(ox+pillW, clip.Max.X); px++ {
				blendOver2D(dst, px, py, br, bgC, bb, ba)
			}
		}
	}

	fr, fgC, fb, fa := premulFromColor(fg, 1)
	if fa <= 0 {
		return
	}
	for i, ch := range runes {
		g, ok := tinyGlyphs[ch]
		if !ok {
			g = tinyGlyphs[' ']
		}
		gx := ox + 1 + i*4
		gy := oy + 1
		for row := range 5 {
			bits := g[row]
			for col := range 3 {
				if bits&(1<<(2-col)) != 0 {
					px, py := gx+col, gy+row
					if px >= clip.Min.X && px < clip.Max.X && py >= clip.Min.Y && py < clip.Max.Y {
						blendOver2D(dst, px, py, fr, fgC, fb, fa)
					}
				}
			}
		}
	}
}

// --- Self-contained deterministic software rasterizer for core.DrawOp ---

type affine2D struct {
	a, b, c, d, e, f float64
}

func identityAffine2D() affine2D {
	return affine2D{a: 1, d: 1}
}

func rotateAroundAffine2D(cx, cy, angle float64) affine2D {
	sin, cos := math.Sincos(angle)
	t1 := affine2D{a: 1, d: 1, e: cx, f: cy}
	rot := affine2D{a: cos, b: sin, c: -sin, d: cos}
	t2 := affine2D{a: 1, d: 1, e: -cx, f: -cy}
	return t1.mul(rot).mul(t2)
}

func (m affine2D) mul(n affine2D) affine2D {
	return affine2D{
		a: m.a*n.a + m.c*n.b,
		b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d,
		d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e,
		f: m.b*n.e + m.d*n.f + m.f,
	}
}

func (m affine2D) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

func (m affine2D) invert() (affine2D, bool) {
	det := m.a*m.d - m.b*m.c
	if math.Abs(det) < 1e-12 {
		return affine2D{}, false
	}
	invDet := 1 / det
	return affine2D{
		a: m.d * invDet,
		b: -m.b * invDet,
		c: -m.c * invDet,
		d: m.a * invDet,
		e: (m.c*m.f - m.d*m.e) * invDet,
		f: (m.b*m.e - m.a*m.f) * invDet,
	}, true
}

func applyTransformOp(m affine2D, op core.DrawOp) affine2D {
	switch op.Kind {
	case core.DrawTranslate:
		return m.mul(affine2D{a: 1, d: 1, e: float64(op.X), f: float64(op.Y)})
	case core.DrawScale:
		return m.mul(affine2D{a: float64(op.X), d: float64(op.Y)})
	case core.DrawRotate:
		sin, cos := math.Sincos(float64(op.A))
		return m.mul(affine2D{a: cos, b: sin, c: -sin, d: cos})
	case core.DrawTransform:
		return m.mul(affine2D{
			a: float64(op.X1), b: float64(op.Y1),
			c: float64(op.X2), d: float64(op.Y2),
			e: float64(op.X), f: float64(op.Y),
		})
	case core.DrawSetTransform:
		return affine2D{
			a: float64(op.X1), b: float64(op.Y1),
			c: float64(op.X2), d: float64(op.Y2),
			e: float64(op.X), f: float64(op.Y),
		}
	default:
		return m
	}
}

type pathRect2D struct {
	x, y, w, h float64
	m          affine2D
}

type pathSeg2D struct {
	x0, y0, x1, y1 float64
}

type rasterState struct {
	m           affine2D
	alpha       float64
	fillColor   core.Color
	strokeColor core.Color
	lineWidth   float64
	clip        image.Rectangle
}

func rasterizeOps(width, height int, ops []core.DrawOp, bg core.Color) *image.RGBA {
	if width <= 0 || height <= 0 {
		return image.NewRGBA(image.Rectangle{})
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if bg != 0 && bg != core.Transparent {
		r, g, b, a := premulFromColor(bg, 1)
		if a > 0 {
			rb, gb, bb, ab := toByte2D(r), toByte2D(g), toByte2D(b), toByte2D(a)
			for y := range height {
				row := dst.Pix[y*dst.Stride:]
				for x := range width {
					off := x * 4
					row[off] = rb
					row[off+1] = gb
					row[off+2] = bb
					row[off+3] = ab
				}
			}
		}
	}

	fullClip := image.Rect(0, 0, width, height)
	cur := rasterState{
		m:           identityAffine2D(),
		alpha:       1,
		fillColor:   core.RGB(0x000000),
		strokeColor: core.RGB(0x000000),
		lineWidth:   1,
		clip:        fullClip,
	}
	var stack []rasterState
	var rects []pathRect2D
	var segs []pathSeg2D
	var hasPen bool
	var penX, penY, subX, subY float64
	decoded := make(map[string]image.Image)

	resolveImage := func(src *core.ImageSource) image.Image {
		if src == nil {
			return nil
		}
		if src.Pixels != nil {
			return src.Pixels
		}
		if len(src.Data) > 0 {
			key := string(src.Data)
			if img, ok := decoded[key]; ok {
				return img
			}
			img, _, err := image.Decode(bytes.NewReader(src.Data))
			if err != nil {
				return nil
			}
			decoded[key] = img
			return img
		}
		return nil
	}

	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, cur)
		case core.DrawRestore:
			if n := len(stack); n > 0 {
				cur = stack[n-1]
				stack = stack[:n-1]
			}
		case core.DrawTranslate, core.DrawScale, core.DrawRotate, core.DrawTransform, core.DrawSetTransform:
			cur.m = applyTransformOp(cur.m, op)
		case core.DrawAlpha:
			cur.alpha = clamp01Float(float64(op.A))
		case core.DrawFillColor:
			cur.fillColor = op.Color
		case core.DrawStrokeColor:
			cur.strokeColor = op.Color
		case core.DrawLineWidth:
			if op.W > 0 {
				cur.lineWidth = float64(op.W)
			}
		case core.DrawBeginPath:
			rects = rects[:0]
			segs = segs[:0]
			hasPen = false
		case core.DrawRect:
			rects = append(rects, pathRect2D{
				x: float64(op.X), y: float64(op.Y),
				w: float64(op.W), h: float64(op.H),
				m: cur.m,
			})
		case core.DrawMoveTo:
			penX, penY = cur.m.apply(float64(op.X), float64(op.Y))
			subX, subY = penX, penY
			hasPen = true
		case core.DrawLineTo:
			nx, ny := cur.m.apply(float64(op.X), float64(op.Y))
			if hasPen {
				segs = append(segs, pathSeg2D{x0: penX, y0: penY, x1: nx, y1: ny})
			} else {
				subX, subY = nx, ny
				hasPen = true
			}
			penX, penY = nx, ny
		case core.DrawClosePath:
			if hasPen && (penX != subX || penY != subY) {
				segs = append(segs, pathSeg2D{x0: penX, y0: penY, x1: subX, y1: subY})
				penX, penY = subX, subY
			}
		case core.DrawArc:
			r := float64(op.R)
			if r <= 0 {
				continue
			}
			a0, a1 := float64(op.A0), float64(op.A1)
			sweep := a1 - a0
			if op.CCW {
				if sweep > 0 {
					sweep -= 2 * math.Pi * math.Ceil(sweep/(2*math.Pi))
				}
			} else {
				if sweep < 0 {
					sweep += 2 * math.Pi * math.Ceil(-sweep/(2*math.Pi))
				}
			}
			if math.Abs(sweep) > 2*math.Pi {
				if op.CCW {
					sweep = -2 * math.Pi
				} else {
					sweep = 2 * math.Pi
				}
			}
			scale := max(math.Hypot(cur.m.a, cur.m.b), math.Hypot(cur.m.c, cur.m.d))
			steps := max(16, int(math.Ceil(math.Abs(sweep)*r*scale)))
			cx, cy := float64(op.X), float64(op.Y)
			for i := 0; i <= steps; i++ {
				ang := a0 + sweep*float64(i)/float64(steps)
				sn, cs := math.Sincos(ang)
				px, py := cur.m.apply(cx+r*cs, cy+r*sn)
				if i == 0 {
					if hasPen {
						segs = append(segs, pathSeg2D{x0: penX, y0: penY, x1: px, y1: py})
					} else {
						subX, subY = px, py
						hasPen = true
					}
				} else {
					segs = append(segs, pathSeg2D{x0: penX, y0: penY, x1: px, y1: py})
				}
				penX, penY = px, py
			}
		case core.DrawClip:
			if clipR, ok := pathBoundsRect(rects, segs, fullClip); ok {
				cur.clip = cur.clip.Intersect(clipR)
			}
		case core.DrawFill:
			sr, sg, sb, sa := premulFromColor(cur.fillColor, cur.alpha)
			if sa <= 0 || cur.clip.Empty() {
				continue
			}
			for _, r := range rects {
				fillTransformedRect2D(dst, cur.clip, r.m, r.x, r.y, r.w, r.h, sr, sg, sb, sa)
			}
			if len(segs) >= 3 {
				fillPolygon2D(dst, cur.clip, segs, sr, sg, sb, sa)
			}
		case core.DrawStroke:
			sr, sg, sb, sa := premulFromColor(cur.strokeColor, cur.alpha)
			if sa <= 0 || cur.lineWidth <= 0 || cur.clip.Empty() {
				continue
			}
			for _, r := range rects {
				strokeTransformedRect2D(dst, cur.clip, r.m, r.x, r.y, r.w, r.h, cur.lineWidth, sr, sg, sb, sa)
			}
			if len(segs) > 0 {
				scale := math.Sqrt(math.Hypot(cur.m.a, cur.m.b) * math.Hypot(cur.m.c, cur.m.d))
				lw := max(1, cur.lineWidth*scale)
				for _, seg := range segs {
					strokeSegment2D(dst, cur.clip, seg.x0, seg.y0, seg.x1, seg.y1, lw, sr, sg, sb, sa)
				}
			}
		case core.DrawClear:
			if cur.clip.Empty() {
				continue
			}
			clearTransformedRect2D(dst, cur.clip, cur.m, float64(op.X), float64(op.Y), float64(op.W), float64(op.H))
		case core.DrawImage:
			if cur.alpha <= 0 || cur.clip.Empty() {
				continue
			}
			img := resolveImage(op.Image)
			if img == nil || img.Bounds().Empty() {
				continue
			}
			b := img.Bounds()
			sx, sy, sw, sh := float64(op.X1), float64(op.Y1), float64(op.X2), float64(op.Y2)
			if sw == 0 && sh == 0 {
				sw, sh = float64(b.Dx()), float64(b.Dy())
			}
			dx, dy, dw, dh := float64(op.X), float64(op.Y), float64(op.W), float64(op.H)
			if dw == 0 && dh == 0 {
				dw, dh = sw, sh
			}
			drawTransformedImage2D(dst, cur.clip, cur.m, img, sx, sy, sw, sh, dx, dy, dw, dh, cur.alpha, op.Tinted, op.Color)
		case core.DrawAtlas:
			if cur.alpha <= 0 || op.Atlas == nil || cur.clip.Empty() {
				continue
			}
			img := resolveImage(op.Image)
			if img == nil || img.Bounds().Empty() {
				continue
			}
			for _, s := range *op.Atlas {
				alpha := cur.alpha * float64(s.Alpha)
				if alpha <= 0 {
					continue
				}
				t := s.Transform
				m := cur.m.mul(affine2D{
					a: float64(t.SCos), b: float64(t.SSin),
					c: -float64(t.SSin), d: float64(t.SCos),
					e: float64(t.TX), f: float64(t.TY),
				})
				sw, sh := float64(s.Rect.Size.Width), float64(s.Rect.Size.Height)
				drawTransformedImage2D(dst, cur.clip, m, img, float64(s.Rect.Origin.X), float64(s.Rect.Origin.Y), sw, sh, 0, 0, sw, sh, alpha, false, 0)
			}
		}
	}

	return dst
}

func pathBoundsRect(rects []pathRect2D, segs []pathSeg2D, full image.Rectangle) (image.Rectangle, bool) {
	has := false
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	addPt := func(x, y float64) {
		has = true
		minX = min(minX, x)
		minY = min(minY, y)
		maxX = max(maxX, x)
		maxY = max(maxY, y)
	}
	for _, r := range rects {
		x0, y0 := r.m.apply(r.x, r.y)
		x1, y1 := r.m.apply(r.x+r.w, r.y)
		x2, y2 := r.m.apply(r.x+r.w, r.y+r.h)
		x3, y3 := r.m.apply(r.x, r.y+r.h)
		addPt(x0, y0)
		addPt(x1, y1)
		addPt(x2, y2)
		addPt(x3, y3)
	}
	for _, s := range segs {
		addPt(s.x0, s.y0)
		addPt(s.x1, s.y1)
	}
	if !has {
		return image.Rectangle{}, false
	}
	r := image.Rect(
		int(math.Ceil(minX-0.5)),
		int(math.Ceil(minY-0.5)),
		int(math.Ceil(maxX-0.5)),
		int(math.Ceil(maxY-0.5)),
	)
	return r.Intersect(full), true
}

func transformedBounds2D(m affine2D, x, y, w, h float64, clip image.Rectangle) (minX, minY, maxX, maxY int, ok bool) {
	x0, y0 := m.apply(x, y)
	x1, y1 := m.apply(x+w, y)
	x2, y2 := m.apply(x, y+h)
	x3, y3 := m.apply(x+w, y+h)

	loX := min(min(x0, x1), min(x2, x3))
	hiX := max(max(x0, x1), max(x2, x3))
	loY := min(min(y0, y1), min(y2, y3))
	hiY := max(max(y0, y1), max(y2, y3))

	minX = max(clip.Min.X, int(math.Floor(loX)))
	minY = max(clip.Min.Y, int(math.Floor(loY)))
	maxX = min(clip.Max.X, int(math.Ceil(hiX)))
	maxY = min(clip.Max.Y, int(math.Ceil(hiY)))
	return minX, minY, maxX, maxY, minX < maxX && minY < maxY
}

func normalizeRect2D(x, y, w, h float64) (float64, float64, float64, float64) {
	if w < 0 {
		x += w
		w = -w
	}
	if h < 0 {
		y += h
		h = -h
	}
	return x, y, w, h
}

func fillTransformedRect2D(dst *image.RGBA, clip image.Rectangle, m affine2D, x, y, w, h, sr, sg, sb, sa float64) {
	x, y, w, h = normalizeRect2D(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	minX, minY, maxX, maxY, visible := transformedBounds2D(m, x, y, w, h, clip)
	if !visible {
		return
	}
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			lx, ly := inv.apply(float64(px)+0.5, cy)
			if lx >= x && lx < x+w && ly >= y && ly < y+h {
				blendOver2D(dst, px, py, sr, sg, sb, sa)
			}
		}
	}
}

func strokeTransformedRect2D(dst *image.RGBA, clip image.Rectangle, m affine2D, x, y, w, h, lineWidth, sr, sg, sb, sa float64) {
	x, y, w, h = normalizeRect2D(x, y, w, h)
	if (w <= 0 && h <= 0) || lineWidth <= 0 {
		return
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	half := lineWidth / 2
	ox, oy, ow, oh := x-half, y-half, w+lineWidth, h+lineWidth
	ix0, iy0, ix1, iy1 := x+half, y+half, x+w-half, y+h-half
	hasHole := ix0 < ix1 && iy0 < iy1

	minX, minY, maxX, maxY, visible := transformedBounds2D(m, ox, oy, ow, oh, clip)
	if !visible {
		return
	}
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			lx, ly := inv.apply(float64(px)+0.5, cy)
			if lx < ox || lx >= ox+ow || ly < oy || ly >= oy+oh {
				continue
			}
			if hasHole && lx > ix0 && lx < ix1 && ly > iy0 && ly < iy1 {
				continue
			}
			blendOver2D(dst, px, py, sr, sg, sb, sa)
		}
	}
}

func clearTransformedRect2D(dst *image.RGBA, clip image.Rectangle, m affine2D, x, y, w, h float64) {
	x, y, w, h = normalizeRect2D(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	minX, minY, maxX, maxY, visible := transformedBounds2D(m, x, y, w, h, clip)
	if !visible {
		return
	}
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			lx, ly := inv.apply(float64(px)+0.5, cy)
			if lx >= x && lx < x+w && ly >= y && ly < y+h {
				off := dst.PixOffset(px, py)
				dst.Pix[off] = 0
				dst.Pix[off+1] = 0
				dst.Pix[off+2] = 0
				dst.Pix[off+3] = 0
			}
		}
	}
}

func fillPolygon2D(dst *image.RGBA, clip image.Rectangle, segs []pathSeg2D, sr, sg, sb, sa float64) {
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, s := range segs {
		minY = min(minY, min(s.y0, s.y1))
		maxY = max(maxY, max(s.y0, s.y1))
	}
	y0 := max(clip.Min.Y, int(math.Floor(minY)))
	y1 := min(clip.Max.Y, int(math.Ceil(maxY)))
	var xs []float64
	for py := y0; py < y1; py++ {
		cy := float64(py) + 0.5
		xs = xs[:0]
		for _, s := range segs {
			if (s.y0 <= cy && s.y1 > cy) || (s.y1 <= cy && s.y0 > cy) {
				t := (cy - s.y0) / (s.y1 - s.y0)
				xs = append(xs, s.x0+t*(s.x1-s.x0))
			}
		}
		if len(xs) < 2 {
			continue
		}
		slices.Sort(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			px0 := max(clip.Min.X, int(math.Ceil(xs[i]-0.5)))
			px1 := min(clip.Max.X, int(math.Ceil(xs[i+1]-0.5)))
			for px := px0; px < px1; px++ {
				blendOver2D(dst, px, py, sr, sg, sb, sa)
			}
		}
	}
}

func strokeSegment2D(dst *image.RGBA, clip image.Rectangle, x0, y0, x1, y1, lw, sr, sg, sb, sa float64) {
	if lw <= 1.25 {
		drawLineBresenham(dst, clip, int(math.Floor(x0)), int(math.Floor(y0)), int(math.Floor(x1)), int(math.Floor(y1)), sr, sg, sb, sa)
		return
	}
	half := lw / 2
	minX := max(clip.Min.X, int(math.Floor(min(x0, x1)-half)))
	maxX := min(clip.Max.X, int(math.Ceil(max(x0, x1)+half)))
	minY := max(clip.Min.Y, int(math.Floor(min(y0, y1)-half)))
	maxY := min(clip.Max.Y, int(math.Ceil(max(y0, y1)+half)))
	dx, dy := x1-x0, y1-y0
	lenSq := dx*dx + dy*dy
	halfSq := half * half
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			cx := float64(px) + 0.5
			var t float64
			if lenSq > 0 {
				t = clamp01Float(((cx-x0)*dx + (cy-y0)*dy) / lenSq)
			}
			qx, qy := x0+t*dx, y0+t*dy
			ddx, ddy := cx-qx, cy-qy
			if ddx*ddx+ddy*ddy <= halfSq {
				blendOver2D(dst, px, py, sr, sg, sb, sa)
			}
		}
	}
}

func drawTransformedImage2D(dst *image.RGBA, clip image.Rectangle, m affine2D, src image.Image, sx, sy, sw, sh, dx, dy, dw, dh, alpha float64, tinted bool, tint core.Color) {
	if sw == 0 || sh == 0 || dw == 0 || dh == 0 {
		return
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	nx, ny, nw, nh := normalizeRect2D(dx, dy, dw, dh)
	minX, minY, maxX, maxY, visible := transformedBounds2D(m, nx, ny, nw, nh, clip)
	if !visible {
		return
	}

	var tr, tg, tb, ta float64 = 1, 1, 1, 1
	if tinted && tint != 0 && tint != core.RGB(0xffffff) {
		tr = float64(tint>>24&0xff) / 255
		tg = float64(tint>>16&0xff) / 255
		tb = float64(tint>>8&0xff) / 255
		ta = float64(tint&0xff) / 255
		if ta <= 0 {
			return
		}
	}

	sb := src.Bounds()
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			lx, ly := inv.apply(float64(px)+0.5, cy)
			if lx < nx || lx >= nx+nw || ly < ny || ly >= ny+nh {
				continue
			}
			u := (lx - dx) / dw
			v := (ly - dy) / dh
			ix := sb.Min.X + int(math.Floor(sx+u*sw))
			iy := sb.Min.Y + int(math.Floor(sy+v*sh))
			ix = min(max(ix, sb.Min.X), sb.Max.X-1)
			iy = min(max(iy, sb.Min.Y), sb.Max.Y-1)

			r16, g16, b16, a16 := src.At(ix, iy).RGBA()
			if a16 == 0 {
				continue
			}
			effA := alpha * ta
			sr := (float64(r16) / 65535) * tr * effA
			sg := (float64(g16) / 65535) * tg * effA
			sbVal := (float64(b16) / 65535) * tb * effA
			sa := (float64(a16) / 65535) * effA
			blendOver2D(dst, px, py, sr, sg, sbVal, sa)
		}
	}
}

func premulFromColor(c core.Color, globalAlpha float64) (r, g, b, a float64) {
	if c == core.Transparent {
		return 0, 0, 0, 0
	}
	a = (float64(c&0xff) / 255) * globalAlpha
	r = (float64(c>>24&0xff) / 255) * a
	g = (float64(c>>16&0xff) / 255) * a
	b = (float64(c>>8&0xff) / 255) * a
	return r, g, b, a
}

func blendOver2D(dst *image.RGBA, x, y int, sr, sg, sb, sa float64) {
	off := dst.PixOffset(x, y)
	if sa >= 1 {
		dst.Pix[off] = toByte2D(sr)
		dst.Pix[off+1] = toByte2D(sg)
		dst.Pix[off+2] = toByte2D(sb)
		dst.Pix[off+3] = 255
		return
	}
	invA := 1 - sa
	dr := float64(dst.Pix[off]) / 255
	dg := float64(dst.Pix[off+1]) / 255
	db := float64(dst.Pix[off+2]) / 255
	da := float64(dst.Pix[off+3]) / 255

	dst.Pix[off] = toByte2D(sr + dr*invA)
	dst.Pix[off+1] = toByte2D(sg + dg*invA)
	dst.Pix[off+2] = toByte2D(sb + db*invA)
	dst.Pix[off+3] = toByte2D(sa + da*invA)
}

func clamp01Float(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func toByte2D(v float64) uint8 {
	return uint8(math.Round(clamp01Float(v) * 255))
}
