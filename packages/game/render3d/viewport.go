package render3d

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ErrInvalidRenderTexture is wrapped by errors returned by [RenderTextureDesc.Validate],
// [NewRenderTexture], [RenderTexture.Resize], and [ViewportRenderer] when an offscreen
// render texture descriptor or handle is invalid.
var ErrInvalidRenderTexture = errors.New("render3d: invalid render texture")

// ErrInvalidViewport is wrapped by errors returned by [ViewportRect.Validate] and
// [ViewportRenderer] when a viewport rectangle, surface size, or camera view is invalid.
var ErrInvalidViewport = errors.New("render3d: invalid viewport")

// RenderTextureDesc configures an offscreen render-to-texture target created by
// [NewRenderTexture] (for mirrors, minimaps, portals, security cameras, or post-processing).
type RenderTextureDesc struct {
	// Label names the offscreen target's GPU attachments in capture tools.
	Label string
	// Width and Height are the texture dimensions in pixels; both must be positive.
	Width  uint32
	Height uint32
	// ColorFormat selects the color attachment format. Zero ([gpu.FormatUndefined])
	// defaults to [gpu.FormatRGBA8Unorm]. Depth/stencil formats are rejected.
	ColorFormat gpu.TextureFormat
	// DepthFormat optionally allocates a matching depth (or depth/stencil)
	// attachment for 3D depth testing, such as [gpu.FormatDepth24Plus]. Leave
	// zero ([gpu.FormatUndefined]) for a color-only render texture.
	DepthFormat gpu.TextureFormat
	// SampleCount sets MSAA sample count: 0 or 1 for single-sampled, or 4 for
	// 4x MSAA with an automatic single-sampled resolve texture exposed via
	// [RenderTexture.Texture] and [RenderTexture.GPUTexture].
	SampleCount int
	// Retained opts into automatic attachment recreation by NSGo after a GPU
	// device loss so the RenderTexture and any [BindTexture] bind group built
	// from [RenderTexture.GPUTexture] (with a retained sampler) stay valid.
	Retained bool
}

// Validate checks that d specifies positive dimensions, a valid color format,
// an optional depth/stencil format, and a supported MSAA sample count (0, 1, or 4).
// Every error wraps [ErrInvalidRenderTexture].
func (d RenderTextureDesc) Validate() error {
	if d.Width == 0 || d.Height == 0 {
		return fmt.Errorf("%w: dimensions %dx%d must be positive", ErrInvalidRenderTexture, d.Width, d.Height)
	}
	if d.ColorFormat != gpu.FormatUndefined && d.ColorFormat.IsDepthStencil() {
		return fmt.Errorf("%w: color format %s must not be depth/stencil", ErrInvalidRenderTexture, d.ColorFormat)
	}
	if d.DepthFormat != gpu.FormatUndefined && !d.DepthFormat.IsDepthStencil() {
		return fmt.Errorf("%w: depth format %s must be a depth/stencil format", ErrInvalidRenderTexture, d.DepthFormat)
	}
	if d.SampleCount < 0 || (d.SampleCount > 1 && d.SampleCount != 4) {
		return fmt.Errorf("%w: sample count %d must be 0, 1, or 4", ErrInvalidRenderTexture, d.SampleCount)
	}
	return nil
}

func (d RenderTextureDesc) effectiveColorFormat() gpu.TextureFormat {
	if d.ColorFormat == gpu.FormatUndefined {
		return gpu.FormatRGBA8Unorm
	}
	return d.ColorFormat
}

func (d RenderTextureDesc) effectiveSampleCount() int {
	if d.SampleCount <= 1 {
		return 1
	}
	return d.SampleCount
}

// RenderTexture is a scope-owned offscreen GPU render target whose color output
// is directly sampleable as a material texture via [RenderTexture.GPUTexture]
// and [BindTexture].
//
// Its underlying [*gpu.RenderTarget] belongs to the [*core.Scope] passed to
// [NewRenderTexture] and is released when that scope is disposed or earlier via
// [RenderTexture.Close]. Use it from the engine goroutine, like the [gpu.Device]
// it came from.
type RenderTexture struct {
	cx *core.Scope
	// owner is a child of cx that holds only the current target's cleanups. gpu registers a cleanup on the scope for every texture it creates and closing the texture early does not remove it, so Resize disposes the old target's child scope instead of letting cx collect one set of cleanups per resize. It is nil when cx is nil.
	owner       *core.Scope
	target      *gpu.RenderTarget
	gpuTex      *GPUTexture
	label       string
	width       uint32
	height      uint32
	colorFormat gpu.TextureFormat
	depthFormat gpu.TextureFormat
	sampleCount int
	retained    bool
	generation  uint64
}

// NewRenderTexture validates desc and allocates an offscreen [RenderTexture] on
// dev, scoped to cx (nil for headless tests closed explicitly). Invalid
// descriptors return an error wrapping [ErrInvalidRenderTexture] before creating
// any GPU resource; a nil or lost device returns [gpu.ErrDeviceLost].
func NewRenderTexture(cx *core.Scope, dev *gpu.Device, desc RenderTextureDesc) (*RenderTexture, error) {
	if err := desc.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	label := desc.Label
	if label == "" {
		label = "render3d.render_texture"
	}
	colorFmt := desc.effectiveColorFormat()
	samples := desc.effectiveSampleCount()
	owner := childScope(cx)
	target, err := dev.CreateRenderTarget(owner, gpu.RenderTargetDesc{
		Label:       label,
		Width:       desc.Width,
		Height:      desc.Height,
		ColorFormat: colorFmt,
		DepthFormat: desc.DepthFormat,
		SampleCount: samples,
		Retained:    desc.Retained,
	})
	if err != nil {
		disposeScope(owner)
		return nil, err
	}
	return &RenderTexture{
		cx:          cx,
		owner:       owner,
		target:      target,
		gpuTex:      wrapRenderTargetSampleTexture(target, desc.Width, desc.Height, colorFmt),
		label:       label,
		width:       desc.Width,
		height:      desc.Height,
		colorFormat: colorFmt,
		depthFormat: desc.DepthFormat,
		sampleCount: samples,
		retained:    desc.Retained,
		generation:  1,
	}, nil
}

func childScope(cx *core.Scope) *core.Scope {
	if cx == nil {
		return nil
	}
	return cx.Child()
}

func disposeScope(cx *core.Scope) {
	if cx != nil {
		cx.Dispose()
	}
}

func wrapRenderTargetSampleTexture(target *gpu.RenderTarget, w, h uint32, format gpu.TextureFormat) *GPUTexture {
	if target == nil {
		return nil
	}
	sampleTex := target.Color
	if target.Resolve != nil {
		sampleTex = target.Resolve
	}
	if sampleTex == nil {
		return nil
	}
	return &GPUTexture{
		tex:       sampleTex,
		width:     w,
		height:    h,
		mipLevels: 1,
		format:    format,
	}
}

// Target returns the underlying [*gpu.RenderTarget], or nil for a nil receiver.
func (rt *RenderTexture) Target() *gpu.RenderTarget {
	if rt == nil {
		return nil
	}
	return rt.target
}

// Texture returns the single-sampled [*gpu.Texture] suitable for shader sampling
// (the MSAA resolve texture when SampleCount > 1, otherwise the color texture),
// or nil for a nil receiver.
func (rt *RenderTexture) Texture() *gpu.Texture {
	if rt == nil || rt.gpuTex == nil {
		return nil
	}
	return rt.gpuTex.tex
}

// ColorTexture returns the renderable color attachment [*gpu.Texture], or nil
// for a nil receiver.
func (rt *RenderTexture) ColorTexture() *gpu.Texture {
	if rt == nil || rt.target == nil {
		return nil
	}
	return rt.target.Color
}

// DepthTexture returns the optional depth/stencil attachment [*gpu.Texture], or
// nil when created without a depth format or for a nil receiver.
func (rt *RenderTexture) DepthTexture() *gpu.Texture {
	if rt == nil || rt.target == nil {
		return nil
	}
	return rt.target.Depth
}

// GPUTexture returns a [*GPUTexture] view of rt's sampleable color output so it
// can be passed directly to [BindTexture] or any material texture slot.
//
// The returned handle is owned by rt; do not close it independently. After a
// [RenderTexture.Resize] that returns resized == true, re-fetch GPUTexture and
// recreate any bind groups built from the previous handle.
func (rt *RenderTexture) GPUTexture() *GPUTexture {
	if rt == nil {
		return nil
	}
	return rt.gpuTex
}

// Width returns the render texture width in pixels, or zero for a nil receiver.
func (rt *RenderTexture) Width() uint32 {
	if rt == nil {
		return 0
	}
	return rt.width
}

// Height returns the render texture height in pixels, or zero for a nil receiver.
func (rt *RenderTexture) Height() uint32 {
	if rt == nil {
		return 0
	}
	return rt.height
}

// Aspect returns Width / Height, or zero when either dimension is zero.
func (rt *RenderTexture) Aspect() float32 {
	if rt == nil || rt.width == 0 || rt.height == 0 {
		return 0
	}
	return float32(rt.width) / float32(rt.height)
}

// ColorFormat returns the resolved color attachment format, or [gpu.FormatUndefined]
// for a nil receiver.
func (rt *RenderTexture) ColorFormat() gpu.TextureFormat {
	if rt == nil {
		return gpu.FormatUndefined
	}
	return rt.colorFormat
}

// DepthFormat returns the depth/stencil attachment format, or [gpu.FormatUndefined]
// if created without a depth attachment or for a nil receiver.
func (rt *RenderTexture) DepthFormat() gpu.TextureFormat {
	if rt == nil {
		return gpu.FormatUndefined
	}
	return rt.depthFormat
}

// SampleCount returns the active MSAA sample count (1 or 4), or zero for a nil receiver.
func (rt *RenderTexture) SampleCount() int {
	if rt == nil {
		return 0
	}
	return rt.sampleCount
}

// Generation returns a monotonic counter starting at 1 that increments whenever
// [RenderTexture.Resize] replaces the underlying GPU textures, letting callers
// detect when cached [BindTexture] bind groups must be rebuilt.
func (rt *RenderTexture) Generation() uint64 {
	if rt == nil {
		return 0
	}
	return rt.generation
}

// ByteSize returns the approximate GPU memory occupied by rt's attachments
// (color, MSAA resolve when present, and optional depth), for [RenderStats] and
// [Budget] accounting. It is zero for a nil receiver.
func (rt *RenderTexture) ByteSize() uint64 {
	if rt == nil || rt.width == 0 || rt.height == 0 {
		return 0
	}
	samples := uint64(max(rt.sampleCount, 1))
	var total uint64
	if rt.colorFormat != gpu.FormatUndefined {
		colorBytes := rt.colorFormat.ByteSize(rt.width, rt.height)
		total += colorBytes * samples
		if samples > 1 {
			total += colorBytes
		}
	}
	if rt.depthFormat != gpu.FormatUndefined {
		total += rt.depthFormat.ByteSize(rt.width, rt.height) * samples
	}
	return total
}

// Valid reports whether rt is open and its underlying GPU attachments belong to
// a live device.
func (rt *RenderTexture) Valid() bool {
	return rt != nil && rt.target.Valid()
}

// Resize reallocates rt's offscreen attachments to width x height on dev,
// preserving format, sample count, label, retained flag, and owning scope.
//
// When rt is already valid at width x height, Resize is a no-op and returns
// (false, nil). If allocation of the replacement target fails, the previous
// target remains intact. On success, the previous target is closed, Generation
// increments, and (true, nil) is returned so callers know to rebind any
// [BindTexture] bind groups referencing the prior texture.
func (rt *RenderTexture) Resize(dev *gpu.Device, width, height uint32) (bool, error) {
	if rt == nil {
		return false, fmt.Errorf("%w: nil render texture", ErrInvalidRenderTexture)
	}
	if width == 0 || height == 0 {
		return false, fmt.Errorf("%w: dimensions %dx%d must be positive", ErrInvalidRenderTexture, width, height)
	}
	if dev == nil {
		return false, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if rt.Valid() && rt.width == width && rt.height == height {
		return false, nil
	}
	if rt.cx != nil && rt.cx.Disposed() {
		return false, fmt.Errorf("%w: owning scope is disposed", ErrInvalidRenderTexture)
	}
	nextOwner := childScope(rt.cx)
	next, err := dev.CreateRenderTarget(nextOwner, gpu.RenderTargetDesc{
		Label:       rt.label,
		Width:       width,
		Height:      height,
		ColorFormat: rt.colorFormat,
		DepthFormat: rt.depthFormat,
		SampleCount: rt.sampleCount,
		Retained:    rt.retained,
	})
	if err != nil {
		disposeScope(nextOwner)
		return false, err
	}
	old, oldOwner := rt.target, rt.owner
	rt.target, rt.owner = next, nextOwner
	rt.width = width
	rt.height = height
	rt.gpuTex = wrapRenderTargetSampleTexture(next, width, height, rt.colorFormat)
	rt.generation++
	if old != nil {
		_ = old.Close()
	}
	disposeScope(oldOwner)
	return true, nil
}

// PassDesc builds a [gpu.RenderPassDesc] targeting rt's color (and optional
// resolve and depth/stencil) attachments, clearing color to clearColor and
// depth to clearDepth.
func (rt *RenderTexture) PassDesc(label string, clearColor core.Color, clearDepth float32) gpu.RenderPassDesc {
	if rt == nil || rt.target == nil {
		return gpu.RenderPassDesc{Label: label}
	}
	if label == "" {
		label = rt.label
	}
	desc := gpu.RenderPassDesc{
		Label: label,
		ColorAttachments: []gpu.ColorAttachment{{
			Texture:       rt.target.Color,
			ResolveTarget: rt.target.Resolve,
			LoadOp:        gpu.LoadClear,
			StoreOp:       gpu.StoreStore,
			ClearColor:    clearColor,
		}},
	}
	if rt.target.Depth != nil {
		ds := &gpu.DepthStencilAttachment{
			Texture:      rt.target.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   clearDepth,
		}
		if rt.depthFormat.HasStencil() {
			ds.StencilLoadOp = gpu.LoadClear
			ds.StencilStoreOp = gpu.StoreDiscard
		}
		desc.DepthStencil = ds
	}
	return desc
}

// BeginPass begins a [gpu.RenderPass] on enc targeting rt, clearing color to
// clearColor and depth (when present) to 1. It returns nil if enc is nil or rt
// is not valid.
func (rt *RenderTexture) BeginPass(enc *gpu.Encoder, clearColor core.Color) *gpu.RenderPass {
	if enc == nil || !rt.Valid() {
		return nil
	}
	return enc.BeginRenderPass(rt.PassDesc(rt.label, clearColor, 1))
}

// BeginFramePass begins a [gpu.RenderPass] on frame targeting rt, clearing color
// to clearColor and depth (when present) to 1. It returns nil if frame is nil or
// rt is not valid.
func (rt *RenderTexture) BeginFramePass(frame *gpu.Frame, clearColor core.Color) *gpu.RenderPass {
	if frame == nil || !rt.Valid() {
		return nil
	}
	return frame.BeginRenderPass(rt.PassDesc(rt.label, clearColor, 1))
}

// Close releases rt's underlying GPU attachments immediately rather than waiting
// for its owning scope to dispose. It is safe to call on a nil receiver or more
// than once.
func (rt *RenderTexture) Close() error {
	if rt == nil || rt.target == nil {
		return nil
	}
	err := rt.target.Close()
	disposeScope(rt.owner)
	rt.owner = nil
	return err
}

// ViewportRect describes a normalized sub-rectangle of a render target in [0, 1]
// top-left coordinates (matching NSGo's GPU viewport and scissor convention),
// together with its clip-depth range [MinDepth, MaxDepth].
//
// The zero value (Width == 0 && Height == 0) represents the full target
// [0, 0, 1, 1] with depth range [0, 1].
type ViewportRect struct {
	X        float32
	Y        float32
	Width    float32
	Height   float32
	MinDepth float32
	MaxDepth float32
}

// FullViewport returns a [ViewportRect] covering the entire render target with
// depth range [0, 1].
func FullViewport() ViewportRect {
	return ViewportRect{X: 0, Y: 0, Width: 1, Height: 1, MinDepth: 0, MaxDepth: 1}
}

// ViewportFromPixels converts a pixel-space top-left rectangle into a normalized
// [ViewportRect] relative to a target of size targetWidth x targetHeight.
// Returns the zero ViewportRect if targetWidth or targetHeight is non-positive.
func ViewportFromPixels(x, y, width, height float32, targetWidth, targetHeight int) ViewportRect {
	if targetWidth <= 0 || targetHeight <= 0 {
		return ViewportRect{}
	}
	tw := float32(targetWidth)
	th := float32(targetHeight)
	return ViewportRect{
		X:        x / tw,
		Y:        y / th,
		Width:    width / tw,
		Height:   height / th,
		MinDepth: 0,
		MaxDepth: 1,
	}
}

// SplitScreenSideBySide returns count equal-width full-height [ViewportRect]
// columns arranged left to right across the target (for example, 2-player
// side-by-side split screen). Returns nil if count <= 0.
func SplitScreenSideBySide(count int) []ViewportRect {
	if count <= 0 {
		return nil
	}
	out := make([]ViewportRect, count)
	step := float32(1) / float32(count)
	for i := range count {
		x0 := float32(i) / float32(count)
		w := step
		if i == count-1 {
			w = 1 - x0
		}
		out[i] = ViewportRect{X: x0, Y: 0, Width: w, Height: 1, MinDepth: 0, MaxDepth: 1}
	}
	return out
}

// SplitScreenStacked returns count equal-height full-width [ViewportRect] rows
// arranged top to bottom down the target. Returns nil if count <= 0.
func SplitScreenStacked(count int) []ViewportRect {
	if count <= 0 {
		return nil
	}
	out := make([]ViewportRect, count)
	step := float32(1) / float32(count)
	for i := range count {
		y0 := float32(i) / float32(count)
		h := step
		if i == count-1 {
			h = 1 - y0
		}
		out[i] = ViewportRect{X: 0, Y: y0, Width: 1, Height: h, MinDepth: 0, MaxDepth: 1}
	}
	return out
}

// SplitScreenHorizontal arranges count viewports horizontally side by side
// across the X axis (alias of [SplitScreenSideBySide]).
func SplitScreenHorizontal(count int) []ViewportRect {
	return SplitScreenSideBySide(count)
}

// SplitScreenVertical arranges count viewports vertically stacked down the Y
// axis (alias of [SplitScreenStacked]).
func SplitScreenVertical(count int) []ViewportRect {
	return SplitScreenStacked(count)
}

// SplitScreenQuad returns four quadrant [ViewportRect] tiles ordered top-left,
// top-right, bottom-left, bottom-right for 4-player split screen.
func SplitScreenQuad() [4]ViewportRect {
	return [4]ViewportRect{
		{X: 0, Y: 0, Width: 0.5, Height: 0.5, MinDepth: 0, MaxDepth: 1},
		{X: 0.5, Y: 0, Width: 0.5, Height: 0.5, MinDepth: 0, MaxDepth: 1},
		{X: 0, Y: 0.5, Width: 0.5, Height: 0.5, MinDepth: 0, MaxDepth: 1},
		{X: 0.5, Y: 0.5, Width: 0.5, Height: 0.5, MinDepth: 0, MaxDepth: 1},
	}
}

// Validate checks that r has finite coordinates within [0, 1], valid non-zero
// dimensions (or an all-zero full-target default), and a valid depth range
// 0 <= MinDepth <= MaxDepth <= 1. Every error wraps [ErrInvalidViewport].
func (r ViewportRect) Validate() error {
	if !finite(r.X) || !finite(r.Y) || !finite(r.Width) || !finite(r.Height) || !finite(r.MinDepth) || !finite(r.MaxDepth) {
		return fmt.Errorf("%w: viewport contains non-finite values", ErrInvalidViewport)
	}
	const eps = 1e-5
	if r.MinDepth < 0 || r.MinDepth > 1 || r.MaxDepth < 0 || r.MaxDepth > 1 {
		return fmt.Errorf("%w: depth range [%g, %g] outside [0, 1]", ErrInvalidViewport, r.MinDepth, r.MaxDepth)
	}
	maxD := r.MaxDepth
	if r.MinDepth == 0 && maxD == 0 {
		maxD = 1
	}
	if r.MinDepth > maxD {
		return fmt.Errorf("%w: MinDepth %g exceeds MaxDepth %g", ErrInvalidViewport, r.MinDepth, maxD)
	}
	if r.X == 0 && r.Y == 0 && r.Width == 0 && r.Height == 0 {
		return nil
	}
	if r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 {
		return fmt.Errorf("%w: origin (%g, %g) must be >= 0 and size (%g, %g) must be > 0", ErrInvalidViewport, r.X, r.Y, r.Width, r.Height)
	}
	if r.X+r.Width > 1+eps || r.Y+r.Height > 1+eps {
		return fmt.Errorf("%w: rect (%g, %g, %g, %g) exceeds [0, 1] bounds", ErrInvalidViewport, r.X, r.Y, r.Width, r.Height)
	}
	return nil
}

// Normalized returns r with zero defaults filled in: an all-zero spatial rect
// becomes [0, 0, 1, 1] and a zero depth range becomes [0, 1].
func (r ViewportRect) Normalized() ViewportRect {
	if r.X == 0 && r.Y == 0 && r.Width == 0 && r.Height == 0 {
		r.Width = 1
		r.Height = 1
	}
	if r.MinDepth == 0 && r.MaxDepth == 0 {
		r.MaxDepth = 1
	}
	return r
}

// PixelViewport is a resolved, target-clamped pixel rectangle ready to apply to
// a [gpu.RenderPass] with [PixelViewport.Apply].
type PixelViewport struct {
	X        int32
	Y        int32
	Width    int32
	Height   int32
	MinDepth float32
	MaxDepth float32
}

// Resolve converts r into clamped pixel coordinates for a render target of size
// targetWidth x targetHeight. Both left/right and top/bottom edges are rounded
// independently so adjacent split-screen rects tile seamlessly with zero gaps
// and zero pixel overlap across seams. Returns (zero, false) if r is invalid,
// target dimensions are non-positive, or the resolved pixel area is empty.
func (r ViewportRect) Resolve(targetWidth, targetHeight int) (PixelViewport, bool) {
	if targetWidth <= 0 || targetHeight <= 0 || r.Validate() != nil {
		return PixelViewport{}, false
	}
	n := r.Normalized()
	tw := float64(targetWidth)
	th := float64(targetHeight)

	x0 := clampInt32(int(math.Round(float64(n.X)*tw)), 0, targetWidth)
	x1 := clampInt32(int(math.Round(float64(n.X+n.Width)*tw)), 0, targetWidth)
	y0 := clampInt32(int(math.Round(float64(n.Y)*th)), 0, targetHeight)
	y1 := clampInt32(int(math.Round(float64(n.Y+n.Height)*th)), 0, targetHeight)
	w := x1 - x0
	h := y1 - y0
	if w <= 0 || h <= 0 {
		return PixelViewport{}, false
	}
	return PixelViewport{
		X:        x0,
		Y:        y0,
		Width:    w,
		Height:   h,
		MinDepth: n.MinDepth,
		MaxDepth: n.MaxDepth,
	}, true
}

// Aspect returns the resolved pixel aspect ratio (width / height) of r inside a
// target of size targetWidth x targetHeight, or zero if resolution fails.
func (r ViewportRect) Aspect(targetWidth, targetHeight int) float32 {
	pv, ok := r.Resolve(targetWidth, targetHeight)
	if !ok {
		return 0
	}
	return pv.Aspect()
}

// Aspect returns Width / Height, or zero when Width or Height is non-positive.
func (p PixelViewport) Aspect() float32 {
	if p.Width <= 0 || p.Height <= 0 {
		return 0
	}
	return float32(p.Width) / float32(p.Height)
}

// Apply sets both the rasterizer viewport and the matching pixel scissor rect
// on pass so draws stay strictly inside p with zero bleed across split seams.
func (p PixelViewport) Apply(pass *gpu.RenderPass) {
	if pass == nil || p.Width <= 0 || p.Height <= 0 {
		return
	}
	pass.SetViewport(float32(p.X), float32(p.Y), float32(p.Width), float32(p.Height), p.MinDepth, p.MaxDepth)
	pass.SetScissor(p.X, p.Y, p.Width, p.Height)
}

// Bounds converts p from physical pixels to a [core.Bounds] in logical points for 2D overlay helpers such as [DebugDraw.Flush], [Gizmo.DrawGizmo], and [BillboardRenderer.Draw], which draw into a [*core.Drawing] measured in points. pixelsPerPoint is the surface's content scale: its width in pixels (frame.Size) divided by the view's width in points. A non-positive or non-finite pixelsPerPoint is treated as 1.
func (p PixelViewport) Bounds(pixelsPerPoint float32) core.Bounds {
	s := pixelsPerPoint
	if !(s > 0) || !finite(s) {
		s = 1
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(float32(p.X) / s), Y: core.Points(float32(p.Y) / s)},
		Size:   core.Size{Width: core.Points(float32(p.Width) / s), Height: core.Points(float32(p.Height) / s)},
	}
}

func clampInt32(v, lo, hi int) int32 {
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return int32(v)
}

// LayerMatches reports whether an object's layer bitmask intersects a camera's
// layer mask, using the same bitmask convention as [scene3d.Node.Layer].
func LayerMatches(objectLayer, cameraMask uint32) bool {
	return objectLayer&cameraMask != 0
}

// LayeredDrawItem pairs a [DrawItem] with a 32-bit layer bitmask matching
// [scene3d.Node.Layer] for per-camera layer filtering before [Queue.Build].
type LayeredDrawItem struct {
	Item  DrawItem
	Layer uint32
}

// FilterLayeredItems appends every item in src whose Layer intersects mask to
// dst and returns the extended slice. Pass dst[:0] with preallocated capacity
// for zero steady-state allocations per camera view.
func FilterLayeredItems(src []LayeredDrawItem, mask uint32, dst []DrawItem) []DrawItem {
	if mask == 0 {
		return dst
	}
	for i := range src {
		if src[i].Layer&mask != 0 {
			dst = append(dst, src[i].Item)
		}
	}
	return dst
}

// FilterNodesByLayer appends every non-nil node in src whose Layer intersects
// mask to dst and returns the extended slice. Pass dst[:0] with preallocated
// capacity for zero steady-state allocations per camera view.
func FilterNodesByLayer(src []*scene3d.Node, mask uint32, dst []*scene3d.Node) []*scene3d.Node {
	if mask == 0 {
		return dst
	}
	for _, n := range src {
		if n != nil && n.Layer&mask != 0 {
			dst = append(dst, n)
		}
	}
	return dst
}

// CameraView describes one camera pass rendered by [ViewportRenderer]: either
// an offscreen render-to-texture pass (Target != nil) or a viewport on the main
// surface (Target == nil).
type CameraView struct {
	// Name is an optional label used in GPU pass labels and diagnostics.
	Name string
	// Camera supplies the viewpoint and projection. Required for active views.
	Camera *scene3d.Camera
	// Target directs this camera into an offscreen [RenderTexture]. Offscreen
	// views always execute before main-surface views (Target == nil) so their
	// textures are ready to sample as material textures in the same frame.
	Target *RenderTexture
	// Viewport selects the normalized sub-rectangle of Target (or the main
	// surface) to render into. The zero value covers the entire target.
	Viewport ViewportRect
	// LayerMask selects which [scene3d.Node.Layer] bits this camera sees.
	// Zero defaults to [scene3d.LayerAll] via [CameraView.EffectiveLayerMask]
	// so struct literals render all layers unless restricted.
	LayerMask uint32
	// Priority orders views ascending within the offscreen phase and within the
	// main-surface phase; ties preserve input slice order.
	Priority int
	// ClearColor sets the clear color when this view begins a clearing pass.
	// For the first main-surface view, zero falls back to [SurfaceDesc.ClearColor].
	ClearColor core.Color
	// ClearDepth sets the depth clear value (0 defaults to 1.0).
	ClearDepth float32
	// LoadColor controls whether the first pass targeting this view's target in a frame clears color ([gpu.LoadClear], the zero default) or preserves existing contents ([gpu.LoadLoad]). A later view that returns to an offscreen target after views on another target always loads color and depth, so what the earlier views drew is kept; set ResetDepth on it to clear depth instead.
	LoadColor gpu.LoadOp
	// ResetDepth forces a continuation pass with preserved color ([gpu.LoadLoad])
	// and a fresh depth clear ([gpu.LoadClear]) when this view shares a target
	// with a preceding view (for example, an overlapping picture-in-picture
	// 3D minimap drawn over a full-screen 3D view). Leave false for non-overlapping
	// split-screen viewports so they share a single GPU render pass.
	ResetDepth bool
	// PreserveAspect keeps Camera.Projection.Aspect unchanged instead of
	// adjusting a per-view copy of the camera to match the resolved viewport's
	// pixel aspect ratio.
	PreserveAspect bool
	// Disabled skips this view when true.
	Disabled bool
}

// NewCameraView returns a [CameraView] targeting the full main surface with
// [scene3d.LayerAll] and ClearDepth 1.
func NewCameraView(cam *scene3d.Camera) CameraView {
	return CameraView{
		Camera:     cam,
		Viewport:   FullViewport(),
		LayerMask:  scene3d.LayerAll,
		ClearDepth: 1,
	}
}

// EffectiveLayerMask returns v.LayerMask, defaulting zero to [scene3d.LayerAll].
func (v CameraView) EffectiveLayerMask() uint32 {
	if v.LayerMask == 0 {
		return scene3d.LayerAll
	}
	return v.LayerMask
}

func effectiveClearDepth(d float32) float32 {
	if d == 0 {
		return 1
	}
	return d
}

// OrderCameraViews appends the indices of active (non-disabled) views from views
// to dst in deterministic execution order: offscreen views (Target != nil) first,
// then main-surface views (Target == nil), each ordered stably by ascending
// Priority. Passing dst[:0] with capacity len(views) allocates nothing.
func OrderCameraViews(views []CameraView, dst []int) []int {
	start := len(dst)
	for i := range views {
		if views[i].Disabled {
			continue
		}
		dst = append(dst, i)
	}
	slices.SortStableFunc(dst[start:], func(a, b int) int {
		aOff := views[a].Target != nil
		bOff := views[b].Target != nil
		if aOff != bOff {
			if aOff {
				return -1
			}
			return 1
		}
		return cmp.Compare(views[a].Priority, views[b].Priority)
	})
	return dst
}

// SurfaceDesc describes the main on-screen render surface passed to
// [ViewportRenderer.RenderEncoder] or [ViewportRenderer.RenderFrame].
type SurfaceDesc struct {
	// Width and Height in pixels. In [ViewportRenderer.RenderFrame], leaving
	// either non-positive fills both from frame.Size().
	Width  int
	Height int
	// HasDepth attaches the surface's depth buffer when true (set true when
	// gpu.ViewProps.DepthFormat or gpu.NewSoftwareRenderer's depth format is set).
	HasDepth bool
	// ClearColor is the fallback clear color for the main surface when the first
	// main-surface view has a zero ClearColor.
	ClearColor core.Color
	// ClearDepth is the fallback depth clear value for the main surface (0 defaults to 1).
	ClearDepth float32
}

// ViewportContext carries the resolved camera, layer mask, pixel viewport,
// per-view camera bind group, and active [gpu.RenderPass] for one view rendered
// by [ViewportRenderer].
type ViewportContext struct {
	// ViewIndex is the index of this view in the caller's original views slice.
	ViewIndex int
	// View is the CameraView being rendered.
	View CameraView
	// Camera is a per-view copy of View.Camera with Projection.Aspect matched to
	// Viewport (unless View.PreserveAspect is true). View.Camera is never mutated.
	Camera scene3d.Camera
	// Uniform is the [CameraUniform] uploaded into CameraBindGroup for this view.
	Uniform CameraUniform
	// LayerMask is View.EffectiveLayerMask().
	LayerMask uint32
	// Viewport is the resolved, clamped pixel rectangle already applied to Pass
	// via SetViewport and SetScissor.
	Viewport PixelViewport
	// CameraBindGroup is the per-view uniform bind group already bound at
	// [CameraGroup] on Pass.
	CameraBindGroup *gpu.BindGroup
	// Pass is the active [gpu.RenderPass]. Do not call Pass.End(); the renderer
	// ends passes automatically when switching targets or finishing the frame.
	Pass *gpu.RenderPass
}

type viewportCameraSlot struct {
	buf   *gpu.Buffer
	group *gpu.BindGroup
}

type resolvedCameraView struct {
	origIndex int
	view      CameraView
	cam       scene3d.Camera
	uniform   CameraUniform
	pixelVP   PixelViewport
	layerMask uint32
}

// ViewportRenderer orchestrates multi-camera frames combining offscreen
// render-to-texture passes ([RenderTexture]) and multiple viewports on the same
// target (such as side-by-side split screen or picture-in-picture overlays).
//
// Each active view receives its own retained camera uniform buffer bound at
// [CameraGroup] so multiple cameras rendered within one [gpu.Encoder] or
// [gpu.Frame] never overwrite each other's uniform data before GPU execution.
// Adjacent views targeting the same surface share a single [gpu.RenderPass]
// unless a view requests [CameraView.ResetDepth].
type ViewportRenderer struct {
	cx         *core.Scope
	dev        *gpu.Device
	camLayout  *gpu.BindGroupLayout
	ownsLayout bool
	slots      []viewportCameraSlot
	order      []int
	resolved   []resolvedCameraView
	started    []*RenderTexture
	scratch    [CameraUniformSize]byte
	closed     bool
}

// NewViewportRenderer creates a [ViewportRenderer] on dev scoped to cx, with a
// shared camera bind group layout matching [CameraGroup] and [CameraWGSL].
// Pipelines rendering through the renderer list [ViewportRenderer.CameraLayout]
// at [CameraGroup].
func NewViewportRenderer(cx *core.Scope, dev *gpu.Device) (*ViewportRenderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "render3d.camera",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageVertex | gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		return nil, err
	}
	return &ViewportRenderer{
		cx:         cx,
		dev:        dev,
		camLayout:  layout,
		ownsLayout: true,
	}, nil
}

// NewViewportRendererWithLayout creates a [ViewportRenderer] that binds its
// per-view camera uniform buffers against an existing camera bind group layout
// (for example, [CameraBuffer.Layout]). Closing the renderer does not close
// camLayout.
func NewViewportRendererWithLayout(cx *core.Scope, dev *gpu.Device, camLayout *gpu.BindGroupLayout) (*ViewportRenderer, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if camLayout == nil || !camLayout.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: invalid camera bind group layout"}
	}
	return &ViewportRenderer{
		cx:         cx,
		dev:        dev,
		camLayout:  camLayout,
		ownsLayout: false,
	}, nil
}

// CameraLayout returns the bind group layout pipelines list at [CameraGroup]
// when drawing through r, or nil for a nil receiver.
func (r *ViewportRenderer) CameraLayout() *gpu.BindGroupLayout {
	if r == nil {
		return nil
	}
	return r.camLayout
}

// Valid reports whether r is open and its camera bind group layout belongs to a
// live device.
func (r *ViewportRenderer) Valid() bool {
	return r != nil && !r.closed && r.camLayout.Valid()
}

// Close releases all GPU resources owned by r (its per-view uniform buffers,
// bind groups, and camera layout when created by [NewViewportRenderer]). Safe
// to call on a nil receiver or more than once.
func (r *ViewportRenderer) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	var first error
	record := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	for i := range r.slots {
		if r.slots[i].group != nil {
			record(r.slots[i].group.Close())
		}
		if r.slots[i].buf != nil {
			record(r.slots[i].buf.Close())
		}
	}
	r.slots = nil
	if r.ownsLayout && r.camLayout != nil {
		record(r.camLayout.Close())
	}
	return first
}

// RenderFrame renders all active views in execution order (offscreen
// [RenderTexture] views first, then main-surface viewports) into frame. When
// surface.Width or surface.Height is non-positive, both default to frame.Size().
// It does not call frame.Submit(); the caller submits once all work is recorded.
func (r *ViewportRenderer) RenderFrame(frame *gpu.Frame, surface SurfaceDesc, views []CameraView, draw func(ViewportContext)) error {
	if frame == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil frame"}
	}
	if surface.Width <= 0 || surface.Height <= 0 {
		surface.Width, surface.Height = frame.Size()
	}
	return r.renderWith(frame.Device(), surface, views, frame.BeginRenderPass, draw)
}

// RenderEncoder renders all active views in execution order (offscreen
// [RenderTexture] views first, then main-surface viewports) into enc (for
// example on [gpu.NewSoftwareRenderer]). The caller submits enc.Finish().
func (r *ViewportRenderer) RenderEncoder(enc *gpu.Encoder, surface SurfaceDesc, views []CameraView, draw func(ViewportContext)) error {
	if enc == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil encoder"}
	}
	return r.renderWith(r.dev, surface, views, enc.BeginRenderPass, draw)
}

func (r *ViewportRenderer) ensureSlot(dev *gpu.Device, index int) (viewportCameraSlot, error) {
	if index < len(r.slots) {
		s := r.slots[index]
		if s.buf.Valid() && s.group.Valid() {
			return s, nil
		}
		if s.group != nil {
			_ = s.group.Close()
		}
		if s.buf != nil {
			_ = s.buf.Close()
		}
	} else {
		for len(r.slots) <= index {
			r.slots = append(r.slots, viewportCameraSlot{})
		}
	}

	buf, err := dev.CreateBuffer(r.cx, gpu.BufferDesc{
		Label:    "render3d.viewport.camera",
		Size:     CameraUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: true,
	})
	if err != nil {
		return viewportCameraSlot{}, err
	}
	group, err := dev.CreateBindGroupDesc(r.cx, gpu.BindGroupDesc{
		Label:   "render3d.viewport.camera",
		Layout:  r.camLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: CameraUniformSize}},
	})
	if err != nil {
		_ = buf.Close()
		return viewportCameraSlot{}, err
	}
	slot := viewportCameraSlot{buf: buf, group: group}
	r.slots[index] = slot
	return slot, nil
}

func (r *ViewportRenderer) renderWith(
	dev *gpu.Device,
	surface SurfaceDesc,
	views []CameraView,
	beginPass func(gpu.RenderPassDesc) *gpu.RenderPass,
	draw func(ViewportContext),
) error {
	if r == nil || r.closed {
		return fmt.Errorf("%w: viewport renderer is closed", ErrInvalidViewport)
	}
	if dev == nil {
		dev = r.dev
	} else {
		r.dev = dev
	}
	if dev == nil || !r.camLayout.Valid() {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: viewport renderer device or camera layout is not valid"}
	}

	r.order = OrderCameraViews(views, r.order[:0])
	if len(r.order) == 0 {
		return nil
	}

	// Phase 1: Validate every active view and resolve pixel viewports & uniforms
	// before recording any GPU commands so errors leave the encoder untouched.
	r.resolved = r.resolved[:0]
	for _, idx := range r.order {
		v := views[idx]
		if v.Camera == nil {
			return fmt.Errorf("%w: view %d (%q) has nil camera", ErrInvalidViewport, idx, v.Name)
		}
		if err := v.Viewport.Validate(); err != nil {
			return fmt.Errorf("%w: view %d (%q): %v", ErrInvalidViewport, idx, v.Name, err)
		}
		var targetW, targetH int
		if v.Target != nil {
			if !v.Target.Valid() {
				return fmt.Errorf("%w: view %d (%q) target is not valid", ErrInvalidRenderTexture, idx, v.Name)
			}
			targetW = int(v.Target.Width())
			targetH = int(v.Target.Height())
		} else {
			if surface.Width <= 0 || surface.Height <= 0 {
				return fmt.Errorf("%w: main surface dimensions %dx%d must be positive", ErrInvalidViewport, surface.Width, surface.Height)
			}
			targetW = surface.Width
			targetH = surface.Height
		}
		pv, ok := v.Viewport.Resolve(targetW, targetH)
		if !ok {
			return fmt.Errorf("%w: view %d (%q) resolves to empty pixel viewport on %dx%d target", ErrInvalidViewport, idx, v.Name, targetW, targetH)
		}
		camCopy := *v.Camera
		if !v.PreserveAspect {
			camCopy.SetViewport(float32(pv.Width), float32(pv.Height))
		}
		r.resolved = append(r.resolved, resolvedCameraView{
			origIndex: idx,
			view:      v,
			cam:       camCopy,
			uniform:   NewCameraUniform(&camCopy),
			pixelVP:   pv,
			layerMask: v.EffectiveLayerMask(),
		})
	}

	// Phase 2: Ensure one uniform buffer slot per active view and upload every
	// camera uniform up front so passes submitted in one command buffer never
	// clobber each other's camera data.
	for i := range r.resolved {
		slot, err := r.ensureSlot(dev, i)
		if err != nil {
			return err
		}
		dev.WriteBuffer(slot.buf, 0, r.resolved[i].uniform.Encode(r.scratch[:]))
	}

	// Phase 3: Record render passes, sharing a pass across consecutive views on
	// the same target unless ResetDepth is set.
	var (
		activePass   *gpu.RenderPass
		hasTarget    bool
		activeTarget *RenderTexture
	)
	// started records the offscreen targets this call has already rendered into, so a view that returns to one after another target keeps what the earlier views drew instead of clearing it again.
	r.started = r.started[:0]
	defer func() { clear(r.started); r.started = r.started[:0] }()
	closePass := func() {
		if activePass != nil {
			activePass.End()
			activePass = nil
		}
	}
	defer closePass()

	for i := range r.resolved {
		rv := &r.resolved[i]
		v := rv.view
		sameTarget := hasTarget && v.Target == activeTarget

		if !sameTarget {
			closePass()
			var desc gpu.RenderPassDesc
			if v.Target != nil && slices.Contains(r.started, v.Target) {
				desc = buildReopenPassDesc(v)
			} else {
				desc = buildInitialPassDesc(v, surface)
				if v.Target != nil {
					r.started = append(r.started, v.Target)
				}
			}
			activePass = beginPass(desc)
			activeTarget = v.Target
			hasTarget = true
		} else if v.ResetDepth {
			closePass()
			desc := buildContinuationDepthClearPassDesc(v, surface)
			activePass = beginPass(desc)
		}

		rv.pixelVP.Apply(activePass)
		camBG := r.slots[i].group
		activePass.SetBindGroup(CameraGroup, camBG)

		if draw != nil {
			draw(ViewportContext{
				ViewIndex:       rv.origIndex,
				View:            v,
				Camera:          rv.cam,
				Uniform:         rv.uniform,
				LayerMask:       rv.layerMask,
				Viewport:        rv.pixelVP,
				CameraBindGroup: camBG,
				Pass:            activePass,
			})
		}
	}

	closePass()
	return nil
}

func buildInitialPassDesc(v CameraView, surface SurfaceDesc) gpu.RenderPassDesc {
	label := v.Name
	if v.Target != nil {
		if label == "" {
			label = v.Target.label
		}
		desc := v.Target.PassDesc(label, v.ClearColor, effectiveClearDepth(v.ClearDepth))
		if len(desc.ColorAttachments) > 0 {
			desc.ColorAttachments[0].LoadOp = v.LoadColor
		}
		return desc
	}
	if label == "" {
		label = "render3d.viewport.main"
	}
	clearColor := v.ClearColor
	if clearColor == 0 {
		clearColor = surface.ClearColor
	}
	clearDepth := v.ClearDepth
	if clearDepth == 0 {
		clearDepth = effectiveClearDepth(surface.ClearDepth)
	}
	desc := gpu.RenderPassDesc{
		Label: label,
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     v.LoadColor,
			StoreOp:    gpu.StoreStore,
			ClearColor: clearColor,
		}},
	}
	if surface.HasDepth {
		desc.DepthStencil = &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   clearDepth,
		}
	}
	return desc
}

// buildReopenPassDesc continues an offscreen target that an earlier, non-adjacent view already rendered into this frame: color and depth are loaded, and depth is cleared only when the view sets ResetDepth.
func buildReopenPassDesc(v CameraView) gpu.RenderPassDesc {
	label := v.Name
	if label == "" {
		label = v.Target.label
	}
	desc := v.Target.PassDesc(label, 0, effectiveClearDepth(v.ClearDepth))
	if len(desc.ColorAttachments) > 0 {
		desc.ColorAttachments[0].LoadOp = gpu.LoadLoad
	}
	if desc.DepthStencil != nil && !v.ResetDepth {
		desc.DepthStencil.DepthLoadOp = gpu.LoadLoad
	}
	return desc
}

func buildContinuationDepthClearPassDesc(v CameraView, surface SurfaceDesc) gpu.RenderPassDesc {
	label := v.Name
	clearDepth := v.ClearDepth
	if clearDepth == 0 {
		clearDepth = effectiveClearDepth(surface.ClearDepth)
	}
	if v.Target != nil {
		if label == "" {
			label = v.Target.label
		}
		desc := v.Target.PassDesc(label, 0, clearDepth)
		if len(desc.ColorAttachments) > 0 {
			desc.ColorAttachments[0].LoadOp = gpu.LoadLoad
		}
		return desc
	}
	if label == "" {
		label = "render3d.viewport.overlay"
	}
	desc := gpu.RenderPassDesc{
		Label: label,
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:  gpu.LoadLoad,
			StoreOp: gpu.StoreStore,
		}},
	}
	if surface.HasDepth {
		desc.DepthStencil = &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   clearDepth,
		}
	}
	return desc
}
