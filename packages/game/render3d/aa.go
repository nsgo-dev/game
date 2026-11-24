package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// AAMode selects the anti-aliasing strategy for a 3D render pass (#249).
type AAMode uint8

const (
	// AANone disables anti-aliasing (single-sampled, no post-processing).
	AANone AAMode = iota
	// AAFXAA applies a full-screen FXAA 3.11 post-processing pass over a
	// single-sampled color texture, suitable for deferred pipelines, post-FX
	// chains, or devices without hardware MSAA.
	AAFXAA
	// AAMSAA2x requests 2x hardware multisample anti-aliasing. Because WebGPU
	// and NSGo's portable GPU API expose 1x and 4x sample counts (guarded by
	// Features.MSAA4x), [ClampSampleCount] promotes 2x to 4x when MSAA4x is
	// supported and falls back to 1x otherwise.
	AAMSAA2x
	// AAMSAA4x requests 4x hardware multisample anti-aliasing, resolving to 4x
	// when the device supports Features.MSAA4x and falling back to 1x otherwise.
	AAMSAA4x
)

// String returns a stable lowercase identifier for m ("none", "fxaa",
// "msaa2x", "msaa4x", or "unknown").
func (m AAMode) String() string {
	switch m {
	case AANone:
		return "none"
	case AAFXAA:
		return "fxaa"
	case AAMSAA2x:
		return "msaa2x"
	case AAMSAA4x:
		return "msaa4x"
	default:
		return "unknown"
	}
}

// SampleCount returns the requested hardware sample count for m (1 for AANone
// and AAFXAA, 2 for AAMSAA2x, 4 for AAMSAA4x). Pass it through
// [ClampSampleCount] before creating a GPU render target or pipeline.
func (m AAMode) SampleCount() int {
	switch m {
	case AAMSAA2x:
		return 2
	case AAMSAA4x:
		return 4
	default:
		return 1
	}
}

// IsMSAA reports whether m requests hardware multisampling.
func (m AAMode) IsMSAA() bool {
	return m == AAMSAA2x || m == AAMSAA4x
}

// UsesFXAA reports whether m requests the FXAA post-processing pass.
func (m AAMode) UsesFXAA() bool {
	return m == AAFXAA
}

// ClampSampleCount returns a sample count accepted by dev for requested: 1
// when requested <= 1 or dev is nil or lacks Features.MSAA4x, and 4 when
// requested >= 2 and dev supports Features.MSAA4x. This matches WebGPU and
// NSGo's portable 1x/4x sample-count set so the returned value is always valid
// for [gpu.TextureDesc.SampleCount], [gpu.RenderTargetDesc.SampleCount], and
// [gpu.PipelineDesc.SampleCount].
func ClampSampleCount(dev *gpu.Device, requested int) int {
	if requested <= 1 || dev == nil {
		return 1
	}
	if dev.Features().MSAA4x {
		return 4
	}
	return 1
}

// MSAATargetDesc configures [NewMSAATarget].
type MSAATargetDesc struct {
	// Label names the underlying GPU textures in capture tools.
	Label string
	// Width and Height are the initial dimensions in physical pixels (both > 0).
	Width  uint32
	Height uint32
	// ColorFormat is the color attachment format. FormatUndefined defaults to
	// dev.SurfaceFormat(), or FormatRGBA8Unorm if that is undefined.
	ColorFormat gpu.TextureFormat
	// DepthFormat is an optional depth/stencil format (e.g. FormatDepth24Plus);
	// FormatUndefined disables the depth attachment.
	DepthFormat gpu.TextureFormat
	// SampleCount is the requested MSAA sample count (e.g. AAMSAA4x.SampleCount()).
	// It is clamped to a device-supported count via [ClampSampleCount].
	SampleCount int
	// Retained opts the color, resolve, and depth textures into automatic
	// recreation by NSGo after a device loss.
	Retained bool
}

// MSAATarget manages an offscreen 3D color (and optional depth) render target
// with automatic MSAA resolve and resize support (#249).
//
// When created with SampleCount > 1 on a device supporting Features.MSAA4x,
// the color attachment is multisampled and [MSAATarget.ColorAttachment] binds
// the single-sampled resolve texture as ResolveTarget (with StoreDiscard on the
// transient multisample buffer). When SampleCount clamps to 1, ColorAttachment
// targets the single-sampled color texture directly with StoreStore. In both
// cases, [MSAATarget.OutputTexture] returns the single-sampled texture ready
// for sampling in subsequent passes (such as [FXAAPass] or tone mapping) or
// CPU readback.
type MSAATarget struct {
	cx          *core.Scope
	dev         *gpu.Device
	rt          *gpu.RenderTarget
	label       string
	width       uint32
	height      uint32
	colorFormat gpu.TextureFormat
	depthFormat gpu.TextureFormat
	sampleCount int
	retained    bool
}

// NewMSAATarget allocates an [MSAATarget] on dev scoped to cx. It clamps
// desc.SampleCount via [ClampSampleCount] so callers do not need separate
// error-handling paths for devices without MSAA4x.
func NewMSAATarget(cx *core.Scope, dev *gpu.Device, desc MSAATargetDesc) (*MSAATarget, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if desc.Width == 0 || desc.Height == 0 {
		return nil, gpu.Error{
			Code:    gpu.ErrInvalidDescriptor,
			Message: fmt.Sprintf("render3d: MSAATarget dimensions must be positive (got %dx%d)", desc.Width, desc.Height),
		}
	}
	colorFmt := desc.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
		if colorFmt == gpu.FormatUndefined {
			colorFmt = gpu.FormatRGBA8Unorm
		}
	}
	label := desc.Label
	if label == "" {
		label = "render3d.msaa"
	}
	samples := ClampSampleCount(dev, desc.SampleCount)
	rt, err := dev.CreateRenderTarget(cx, gpu.RenderTargetDesc{
		Label:       label,
		Width:       desc.Width,
		Height:      desc.Height,
		ColorFormat: colorFmt,
		DepthFormat: desc.DepthFormat,
		SampleCount: samples,
		Retained:    desc.Retained,
	})
	if err != nil {
		return nil, err
	}
	return &MSAATarget{
		cx:          cx,
		dev:         dev,
		rt:          rt,
		label:       label,
		width:       desc.Width,
		height:      desc.Height,
		colorFormat: colorFmt,
		depthFormat: desc.DepthFormat,
		sampleCount: samples,
		retained:    desc.Retained,
	}, nil
}

// SetDevice updates the device used by future [MSAATarget.Resize] calls after
// NSGo restores a lost device onto a new [*gpu.Device] for a retained target.
func (t *MSAATarget) SetDevice(dev *gpu.Device) {
	if t == nil {
		return
	}
	t.dev = dev
}

// Resize reallocates the underlying textures to width x height. If the target
// is already valid at that exact size, Resize is a zero-allocation no-op. If
// allocation fails, the existing textures remain intact and the error is
// returned.
func (t *MSAATarget) Resize(width, height uint32) error {
	if t == nil || t.dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: MSAATarget has no device"}
	}
	if width == 0 || height == 0 {
		return gpu.Error{
			Code:    gpu.ErrInvalidDescriptor,
			Message: fmt.Sprintf("render3d: MSAATarget resize dimensions must be positive (got %dx%d)", width, height),
		}
	}
	if t.rt != nil && t.rt.Valid() && t.width == width && t.height == height {
		return nil
	}
	samples := ClampSampleCount(t.dev, t.sampleCount)
	next, err := t.dev.CreateRenderTarget(t.cx, gpu.RenderTargetDesc{
		Label:       t.label,
		Width:       width,
		Height:      height,
		ColorFormat: t.colorFormat,
		DepthFormat: t.depthFormat,
		SampleCount: samples,
		Retained:    t.retained,
	})
	if err != nil {
		return err
	}
	if t.rt != nil {
		_ = t.rt.Close()
	}
	t.rt = next
	t.width = width
	t.height = height
	t.sampleCount = samples
	return nil
}

// ColorAttachment returns a [gpu.ColorAttachment] configured to clear the
// target to clearColor and, when multisampled, automatically resolve into the
// single-sampled resolve texture at pass end (discarding the transient
// multisampled backing store). For a single-sampled target it stores directly
// into the color texture.
func (t *MSAATarget) ColorAttachment(clearColor core.Color) gpu.ColorAttachment {
	if t == nil || t.rt == nil {
		return gpu.ColorAttachment{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: clearColor,
		}
	}
	if t.rt.Resolve != nil {
		return gpu.ColorAttachment{
			Texture:       t.rt.Color,
			ResolveTarget: t.rt.Resolve,
			LoadOp:        gpu.LoadClear,
			StoreOp:       gpu.StoreDiscard,
			ClearColor:    clearColor,
		}
	}
	return gpu.ColorAttachment{
		Texture:    t.rt.Color,
		LoadOp:     gpu.LoadClear,
		StoreOp:    gpu.StoreStore,
		ClearColor: clearColor,
	}
}

// DepthStencilAttachment returns a [gpu.DepthStencilAttachment] clearing the
// target's depth buffer to clearDepth, or nil when no depth format was
// configured.
func (t *MSAATarget) DepthStencilAttachment(clearDepth float32) *gpu.DepthStencilAttachment {
	if t == nil || t.rt == nil || t.rt.Depth == nil {
		return nil
	}
	return &gpu.DepthStencilAttachment{
		Texture:      t.rt.Depth,
		DepthLoadOp:  gpu.LoadClear,
		DepthStoreOp: gpu.StoreDiscard,
		ClearDepth:   clearDepth,
	}
}

// RenderPassDesc builds a [gpu.RenderPassDesc] targeting t with
// [MSAATarget.ColorAttachment] and (if present) [MSAATarget.DepthStencilAttachment].
func (t *MSAATarget) RenderPassDesc(clearColor core.Color, clearDepth float32) gpu.RenderPassDesc {
	label := "render3d.msaa"
	if t != nil && t.label != "" {
		label = t.label
	}
	return gpu.RenderPassDesc{
		Label:            label,
		ColorAttachments: []gpu.ColorAttachment{t.ColorAttachment(clearColor)},
		DepthStencil:     t.DepthStencilAttachment(clearDepth),
	}
}

// OutputTexture returns the single-sampled color texture holding the resolved
// render output: Resolve when multisampled, Color when single-sampled, or nil
// if the target is nil or closed.
func (t *MSAATarget) OutputTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	if t.rt.Resolve != nil {
		return t.rt.Resolve
	}
	return t.rt.Color
}

// ColorTexture returns the primary color attachment texture (multisampled when
// SampleCount > 1), or nil if closed.
func (t *MSAATarget) ColorTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	return t.rt.Color
}

// ResolveTexture returns the single-sampled resolve texture, or nil when the
// target is single-sampled or closed.
func (t *MSAATarget) ResolveTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	return t.rt.Resolve
}

// DepthTexture returns the depth/stencil texture, or nil when none was
// configured or the target is closed.
func (t *MSAATarget) DepthTexture() *gpu.Texture {
	if t == nil || t.rt == nil {
		return nil
	}
	return t.rt.Depth
}

// RenderTarget returns the underlying [*gpu.RenderTarget], or nil if closed.
func (t *MSAATarget) RenderTarget() *gpu.RenderTarget {
	if t == nil {
		return nil
	}
	return t.rt
}

// SampleCount returns the effective (clamped) MSAA sample count (1 or 4), or 0
// for a nil target.
func (t *MSAATarget) SampleCount() int {
	if t == nil {
		return 0
	}
	return t.sampleCount
}

// Width returns the target's width in physical pixels.
func (t *MSAATarget) Width() uint32 {
	if t == nil {
		return 0
	}
	return t.width
}

// Height returns the target's height in physical pixels.
func (t *MSAATarget) Height() uint32 {
	if t == nil {
		return 0
	}
	return t.height
}

// ColorFormat returns the resolved color attachment format.
func (t *MSAATarget) ColorFormat() gpu.TextureFormat {
	if t == nil {
		return gpu.FormatUndefined
	}
	return t.colorFormat
}

// DepthFormat returns the configured depth/stencil format (FormatUndefined if
// none).
func (t *MSAATarget) DepthFormat() gpu.TextureFormat {
	if t == nil {
		return gpu.FormatUndefined
	}
	return t.depthFormat
}

// Retained reports whether the target's textures were created with automatic
// recreation across device loss enabled.
func (t *MSAATarget) Retained() bool {
	if t == nil {
		return false
	}
	return t.retained
}

// Valid reports whether the target's underlying GPU textures are open and
// belong to a live device.
func (t *MSAATarget) Valid() bool {
	return t != nil && t.rt != nil && t.rt.Valid()
}

// Close releases the underlying GPU textures immediately instead of waiting
// for scope disposal. It is safe to call more than once or on a nil target.
func (t *MSAATarget) Close() error {
	if t == nil || t.rt == nil {
		return nil
	}
	err := t.rt.Close()
	t.rt = nil
	return err
}

// FXAAUniformSize is the byte size of the FXAAParams uniform block declared by
// [FXAAWGSL] (two vec4<f32> fields = 32 bytes).
const FXAAUniformSize = 32

const (
	defaultFXAASubpixelQuality  = float32(0.75)
	defaultFXAAEdgeThreshold    = float32(0.166)
	defaultFXAAEdgeThresholdMin = float32(0.0833)
)

// FXAAOptions configures FXAA 3.11 edge detection and sub-pixel anti-aliasing.
// Zero values fall back to [DefaultFXAAOptions] (SubpixelQuality 0.75,
// EdgeThreshold 0.166, EdgeThresholdMin 0.0833).
type FXAAOptions struct {
	// SubpixelQuality controls sub-pixel aliasing removal in [0, 1]. Zero uses
	// the FXAA 3.11 default (0.75) unless DisableSubpixel is true. Negative or
	// non-finite values clamp to 0; values above 1 clamp to 1.
	SubpixelQuality float32
	// DisableSubpixel forces SubpixelQuality to 0 (edge-only smoothing) even
	// when SubpixelQuality is zero.
	DisableSubpixel bool
	// EdgeThreshold is the minimum relative local luma contrast required to
	// process a pixel as an edge, in (0, 1]. Non-positive or non-finite values
	// default to 0.166; values above 1 clamp to 1.
	EdgeThreshold float32
	// EdgeThresholdMin skips very dark regions whose local luma range falls
	// below this absolute floor, in (0, 1]. Non-positive or non-finite values
	// default to 0.0833; values above 1 clamp to 1.
	EdgeThresholdMin float32
}

// DefaultFXAAOptions returns the standard FXAA 3.11 quality defaults
// (SubpixelQuality: 0.75, EdgeThreshold: 0.166, EdgeThresholdMin: 0.0833).
func DefaultFXAAOptions() FXAAOptions {
	return FXAAOptions{
		SubpixelQuality:  defaultFXAASubpixelQuality,
		EdgeThreshold:    defaultFXAAEdgeThreshold,
		EdgeThresholdMin: defaultFXAAEdgeThresholdMin,
	}
}

// Sanitize returns a copy of o with zero, out-of-range, and non-finite fields
// resolved to valid FXAA 3.11 parameters.
func (o FXAAOptions) Sanitize() FXAAOptions {
	out := o
	switch {
	case out.DisableSubpixel:
		out.SubpixelQuality = 0
	case out.SubpixelQuality == 0:
		out.SubpixelQuality = defaultFXAASubpixelQuality
	case !aaFinite(out.SubpixelQuality) || out.SubpixelQuality < 0:
		out.SubpixelQuality = 0
	case out.SubpixelQuality > 1:
		out.SubpixelQuality = 1
	}

	if !aaFinite(out.EdgeThreshold) || out.EdgeThreshold <= 0 {
		out.EdgeThreshold = defaultFXAAEdgeThreshold
	} else if out.EdgeThreshold > 1 {
		out.EdgeThreshold = 1
	}

	if !aaFinite(out.EdgeThresholdMin) || out.EdgeThresholdMin <= 0 {
		out.EdgeThresholdMin = defaultFXAAEdgeThresholdMin
	} else if out.EdgeThresholdMin > 1 {
		out.EdgeThresholdMin = 1
	}
	return out
}

func encodeFXAAUniform(dst *[FXAAUniformSize]byte, width, height uint32, opts FXAAOptions) {
	s := opts.Sanitize()
	var invW, invH float32
	if width > 0 {
		invW = 1.0 / float32(width)
	}
	if height > 0 {
		invH = 1.0 / float32(height)
	}
	vals := [8]float32{
		invW, invH, s.SubpixelQuality, s.EdgeThreshold,
		s.EdgeThresholdMin, 0, 0, 0,
	}
	for i, f := range vals {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(f))
	}
}

// FXAAWGSL is a portable FXAA 3.11 full-screen post-processing WGSL shader
// module compiled by [NewFXAAPass].
//
// It draws a single bufferless full-screen triangle via @builtin(vertex_index)
// (Draw(3, 0)), uses only textureSampleLevel(..., 0.0) so non-uniform early
// exits and edge-search loops remain valid on every GPU backend (Metal,
// Direct3D 11, OpenGL 3.3, WebGPU, WebGL 2, OpenGL ES 3.0, and the software
// device), and matches [ApplyFXAA] on the CPU.
//
// Bind group 0 layout:
//   - @binding(0): uniform FXAAParams (32 bytes: params0 = (1/w, 1/h, subpixel, edgeThreshold), params1 = (edgeThresholdMin, 0, 0, 0))
//   - @binding(1): texture_2d<f32> source color texture (single-sampled)
//   - @binding(2): sampler (FilterLinear, AddressClampToEdge)
const FXAAWGSL = `struct FXAAParams {
	params0: vec4<f32>,
	params1: vec4<f32>,
}

@group(0) @binding(0) var<uniform> fxaa_params: FXAAParams;
@group(0) @binding(1) var fxaa_tex: texture_2d<f32>;
@group(0) @binding(2) var fxaa_sampler: sampler;

struct FXAAVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32) -> FXAAVertexOut {
	var out: FXAAVertexOut;
	let x = select(-1.0, 3.0, vid == 1u);
	let y = select(-1.0, 3.0, vid == 2u);
	out.clip = vec4<f32>(x, y, 0.0, 1.0);
	out.uv = vec2<f32>(x * 0.5 + 0.5, 0.5 - y * 0.5);
	return out;
}

fn fxaa_luma(rgb: vec3<f32>) -> f32 {
	return dot(rgb, vec3<f32>(0.299, 0.587, 0.114));
}

fn fxaa_step(i: i32) -> f32 {
	if (i < 4) {
		return 1.0;
	}
	if (i == 4) {
		return 1.5;
	}
	if (i < 9) {
		return 2.0;
	}
	if (i == 9) {
		return 4.0;
	}
	return 8.0;
}

@fragment
fn fs_main(in: FXAAVertexOut) -> @location(0) vec4<f32> {
	let inv = fxaa_params.params0.xy;
	let subpixQuality = fxaa_params.params0.z;
	let edgeThreshold = fxaa_params.params0.w;
	let edgeThresholdMin = fxaa_params.params1.x;

	let uv = in.uv;
	let center = textureSampleLevel(fxaa_tex, fxaa_sampler, uv, 0.0);
	let lumaM = fxaa_luma(center.rgb);
	let lumaN = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv + vec2<f32>(0.0, -inv.y), 0.0).rgb);
	let lumaS = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv + vec2<f32>(0.0, inv.y), 0.0).rgb);
	let lumaW = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv + vec2<f32>(-inv.x, 0.0), 0.0).rgb);
	let lumaE = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv + vec2<f32>(inv.x, 0.0), 0.0).rgb);

	let lumaMin = min(lumaM, min(min(lumaN, lumaS), min(lumaW, lumaE)));
	let lumaMax = max(lumaM, max(max(lumaN, lumaS), max(lumaW, lumaE)));
	let lumaRange = lumaMax - lumaMin;
	if (lumaRange < max(edgeThresholdMin, lumaMax * edgeThreshold)) {
		return center;
	}

	let lumaNW = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv + vec2<f32>(-inv.x, -inv.y), 0.0).rgb);
	let lumaNE = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv + vec2<f32>(inv.x, -inv.y), 0.0).rgb);
	let lumaSW = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv + vec2<f32>(-inv.x, inv.y), 0.0).rgb);
	let lumaSE = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv + vec2<f32>(inv.x, inv.y), 0.0).rgb);

	let lumaDownUp = lumaN + lumaS;
	let lumaLeftRight = lumaW + lumaE;
	let lumaLeftCorners = lumaNW + lumaSW;
	let lumaRightCorners = lumaNE + lumaSE;
	let lumaTopCorners = lumaNW + lumaNE;
	let lumaBottomCorners = lumaSW + lumaSE;

	let lumaAvg = (2.0 * (lumaDownUp + lumaLeftRight) + (lumaLeftCorners + lumaRightCorners)) * (1.0 / 12.0);
	let subpix1 = clamp(abs(lumaAvg - lumaM) / lumaRange, 0.0, 1.0);
	let subpix2 = (-2.0 * subpix1 + 3.0) * subpix1 * subpix1;
	let subpixOffset = subpix2 * subpix2 * subpixQuality;

	let edgeHorz = abs(-2.0 * lumaW + lumaLeftCorners) +
		2.0 * abs(-2.0 * lumaM + lumaDownUp) +
		abs(-2.0 * lumaE + lumaRightCorners);
	let edgeVert = abs(-2.0 * lumaN + lumaTopCorners) +
		2.0 * abs(-2.0 * lumaM + lumaLeftRight) +
		abs(-2.0 * lumaS + lumaBottomCorners);
	let isHorizontal = edgeHorz >= edgeVert;

	let luma1 = select(lumaW, lumaN, isHorizontal);
	let luma2 = select(lumaE, lumaS, isHorizontal);
	let grad1 = abs(luma1 - lumaM);
	let grad2 = abs(luma2 - lumaM);
	let is1Steepest = grad1 >= grad2;
	let gradScaled = 0.25 * max(grad1, grad2);

	var stepLength = select(inv.x, inv.y, isHorizontal);
	var lumaLocalAvg = 0.5 * (luma2 + lumaM);
	if (is1Steepest) {
		stepLength = -stepLength;
		lumaLocalAvg = 0.5 * (luma1 + lumaM);
	}

	var currentUv = uv;
	if (isHorizontal) {
		currentUv.y = currentUv.y + stepLength * 0.5;
	} else {
		currentUv.x = currentUv.x + stepLength * 0.5;
	}

	let offset = select(vec2<f32>(0.0, inv.y), vec2<f32>(inv.x, 0.0), isHorizontal);
	var uv1 = currentUv - offset;
	var uv2 = currentUv + offset;

	var lumaEnd1 = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv1, 0.0).rgb) - lumaLocalAvg;
	var lumaEnd2 = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv2, 0.0).rgb) - lumaLocalAvg;
	var reached1 = abs(lumaEnd1) >= gradScaled;
	var reached2 = abs(lumaEnd2) >= gradScaled;

	for (var i: i32 = 1; i < 11; i = i + 1) {
		if (reached1 && reached2) {
			break;
		}
		let step = fxaa_step(i);
		if (!reached1) {
			uv1 = uv1 - offset * step;
			lumaEnd1 = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv1, 0.0).rgb) - lumaLocalAvg;
			reached1 = abs(lumaEnd1) >= gradScaled;
		}
		if (!reached2) {
			uv2 = uv2 + offset * step;
			lumaEnd2 = fxaa_luma(textureSampleLevel(fxaa_tex, fxaa_sampler, uv2, 0.0).rgb) - lumaLocalAvg;
			reached2 = abs(lumaEnd2) >= gradScaled;
		}
	}

	let dist1 = select(uv.y - uv1.y, uv.x - uv1.x, isHorizontal);
	let dist2 = select(uv2.y - uv.y, uv2.x - uv.x, isHorizontal);
	let isDir1 = dist1 < dist2;
	let distFinal = min(dist1, dist2);
	let edgeThickness = dist1 + dist2;

	let lumaCenterSmaller = lumaM < lumaLocalAvg;
	let endLuma = select(lumaEnd2, lumaEnd1, isDir1);
	let endSmaller = endLuma < 0.0;
	let correctVariation = (endSmaller && !lumaCenterSmaller) || (!endSmaller && lumaCenterSmaller);

	var pixelOffset = 0.0;
	if (correctVariation && edgeThickness > 0.0) {
		pixelOffset = max(-distFinal / edgeThickness + 0.5, 0.0);
	}
	let finalOffset = max(pixelOffset, subpixOffset);

	var finalUv = uv;
	if (isHorizontal) {
		finalUv.y = finalUv.y + finalOffset * stepLength;
	} else {
		finalUv.x = finalUv.x + finalOffset * stepLength;
	}
	let finalRgb = textureSampleLevel(fxaa_tex, fxaa_sampler, finalUv, 0.0).rgb;
	return vec4<f32>(finalRgb, center.a);
}
`

// FXAAPassDesc configures [NewFXAAPass].
type FXAAPassDesc struct {
	// Label names the pass's GPU resources in capture tools.
	Label string
	// TargetFormat is the color format of the render pass into which [FXAAPass]
	// draws. FormatUndefined defaults to dev.SurfaceFormat() (or FormatRGBA8Unorm).
	TargetFormat gpu.TextureFormat
	// Options configures FXAA 3.11 edge detection and sub-pixel blending. Zero
	// uses [DefaultFXAAOptions].
	Options FXAAOptions
	// Retained opts the pass's GPU resources into automatic recreation after a
	// device loss.
	Retained bool
}

// FXAAPass owns the compiled pipeline, sampler, uniform buffer, and cached
// bind group for full-screen FXAA 3.11 post-processing on a [gpu.Device] (#249).
//
// Create it once in OnReady and call [FXAAPass.Draw] (inside an open single-sampled
// render pass) or [FXAAPass.Encode] (to open and close the render pass
// automatically) with any single-sampled color texture (such as an offscreen
// render target or [MSAATarget.OutputTexture]). Calling Draw repeatedly with
// the same texture reuses the cached bind group and uniform data without
// allocating.
type FXAAPass struct {
	cx           *core.Scope
	dev          *gpu.Device
	label        string
	targetFormat gpu.TextureFormat
	options      FXAAOptions
	retained     bool

	sampler    *gpu.Sampler
	uniformBuf *gpu.Buffer
	layout     *gpu.BindGroupLayout
	pipeline   *gpu.Pipeline

	boundTex     *gpu.Texture
	bindGroup    *gpu.BindGroup
	lastUniform  [FXAAUniformSize]byte
	uniformValid bool
	uniformEpoch uint32
}

// NewFXAAPass compiles [FXAAWGSL] and creates the sampler, uniform buffer,
// bind group layout, and full-screen pipeline on dev, scoped to cx.
func NewFXAAPass(cx *core.Scope, dev *gpu.Device, desc FXAAPassDesc) (*FXAAPass, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	targetFmt := desc.TargetFormat
	if targetFmt == gpu.FormatUndefined {
		targetFmt = dev.SurfaceFormat()
		if targetFmt == gpu.FormatUndefined {
			targetFmt = gpu.FormatRGBA8Unorm
		}
	}
	label := desc.Label
	if label == "" {
		label = "render3d.fxaa"
	}

	mod, err := gpu.CompileWGSL(FXAAWGSL)
	if err != nil {
		return nil, fmt.Errorf("render3d: compile FXAA shader: %w", err)
	}

	sampler, err := dev.CreateSampler(cx, gpu.SamplerDesc{
		Label:     label + ".sampler",
		MinFilter: gpu.FilterLinear,
		MagFilter: gpu.FilterLinear,
		AddressU:  gpu.AddressClampToEdge,
		AddressV:  gpu.AddressClampToEdge,
		Retained:  desc.Retained,
	})
	if err != nil {
		return nil, err
	}

	uniformBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + ".params",
		Size:     FXAAUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: desc.Retained,
	})
	if err != nil {
		_ = sampler.Close()
		return nil, err
	}

	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + ".layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
		},
	})
	if err != nil {
		_ = uniformBuf.Close()
		_ = sampler.Close()
		return nil, err
	}

	pipeline, err := dev.CreatePipeline(cx, gpu.PipelineDesc{
		Label:       label + ".pipeline",
		Vertex:      gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:    gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:     []*gpu.BindGroupLayout{layout},
		Primitive:   gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		Targets:     []gpu.ColorTargetState{{Format: targetFmt, WriteMask: gpu.ColorWriteAll}},
		SampleCount: 1,
		Retained:    desc.Retained,
	})
	if err != nil {
		_ = layout.Close()
		_ = uniformBuf.Close()
		_ = sampler.Close()
		return nil, err
	}

	return &FXAAPass{
		cx:           cx,
		dev:          dev,
		label:        label,
		targetFormat: targetFmt,
		options:      desc.Options.Sanitize(),
		retained:     desc.Retained,
		sampler:      sampler,
		uniformBuf:   uniformBuf,
		layout:       layout,
		pipeline:     pipeline,
	}, nil
}

// SetDevice updates the device pointer after NSGo restores a lost device onto
// a fresh [*gpu.Device] for a retained pass.
func (p *FXAAPass) SetDevice(dev *gpu.Device) {
	if p == nil {
		return
	}
	p.dev = dev
	p.uniformValid = false
}

// SetOptions updates the FXAA 3.11 parameters used by subsequent draws.
func (p *FXAAPass) SetOptions(opts FXAAOptions) {
	if p == nil {
		return
	}
	p.options = opts.Sanitize()
}

// Options returns the current sanitized FXAA 3.11 options.
func (p *FXAAPass) Options() FXAAOptions {
	if p == nil {
		return DefaultFXAAOptions()
	}
	return p.options
}

// Pipeline returns the underlying full-screen FXAA pipeline.
func (p *FXAAPass) Pipeline() *gpu.Pipeline {
	if p == nil {
		return nil
	}
	return p.pipeline
}

// Layout returns the bind group layout at group 0 (uniform, texture, sampler).
func (p *FXAAPass) Layout() *gpu.BindGroupLayout {
	if p == nil {
		return nil
	}
	return p.layout
}

// Sampler returns the linear clamp-to-edge sampler used by the pass.
func (p *FXAAPass) Sampler() *gpu.Sampler {
	if p == nil {
		return nil
	}
	return p.sampler
}

// TargetFormat returns the color format the pipeline was built for.
func (p *FXAAPass) TargetFormat() gpu.TextureFormat {
	if p == nil {
		return gpu.FormatUndefined
	}
	return p.targetFormat
}

// Retained reports whether the pass was created with device-loss retention.
func (p *FXAAPass) Retained() bool {
	if p == nil {
		return false
	}
	return p.retained
}

// Valid reports whether the pass's GPU resources are open and belong to a live
// device.
func (p *FXAAPass) Valid() bool {
	return p != nil &&
		p.pipeline != nil && p.pipeline.Valid() &&
		p.layout != nil && p.layout.Valid() &&
		p.uniformBuf != nil && p.uniformBuf.Valid() &&
		p.sampler != nil && p.sampler.Valid()
}

func (p *FXAAPass) prepare(src *gpu.Texture) (*gpu.BindGroup, error) {
	if !p.Valid() {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: FXAAPass is closed or device is lost"}
	}
	if src == nil || !src.Valid() {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: FXAAPass source texture is nil or invalid"}
	}
	td := src.Desc()
	if td.SampleCount > 1 {
		return nil, gpu.Error{
			Code:    gpu.ErrInvalidDescriptor,
			Message: fmt.Sprintf("render3d: FXAAPass requires a single-sampled source texture (got SampleCount=%d)", td.SampleCount),
		}
	}
	if td.Width == 0 || td.Height == 0 {
		return nil, gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: FXAAPass source texture has zero size"}
	}

	var encoded [FXAAUniformSize]byte
	encodeFXAAUniform(&encoded, td.Width, td.Height, p.options)
	epoch := p.uniformBuf.Epoch()
	if !p.uniformValid || p.uniformEpoch != epoch || encoded != p.lastUniform {
		if p.dev == nil || p.dev.IsLost() {
			return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: FXAAPass device is lost; call SetDevice after restoration"}
		}
		p.dev.WriteBuffer(p.uniformBuf, 0, encoded[:])
		p.lastUniform = encoded
		p.uniformEpoch = epoch
		p.uniformValid = true
	}

	if p.boundTex == src && p.bindGroup != nil && p.bindGroup.Valid() && p.bindGroup.Epoch() == src.Epoch() {
		return p.bindGroup, nil
	}
	if p.dev == nil || p.dev.IsLost() {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: FXAAPass device is lost; call SetDevice after restoration"}
	}
	bg, err := p.dev.CreateBindGroupDesc(p.cx, gpu.BindGroupDesc{
		Label:  p.label + ".bindgroup",
		Layout: p.layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: p.uniformBuf, Size: FXAAUniformSize},
			{Binding: 1, Texture: src},
			{Binding: 2, Sampler: p.sampler},
		},
	})
	if err != nil {
		return nil, err
	}
	if p.bindGroup != nil {
		_ = p.bindGroup.Close()
	}
	p.boundTex = src
	p.bindGroup = bg
	return bg, nil
}

// Draw binds the FXAA pipeline and source texture on pass and issues a
// bufferless full-screen triangle draw (Draw(3, 0)). The caller owns the
// render pass, which must target a single-sampled attachment matching
// [FXAAPass.TargetFormat].
func (p *FXAAPass) Draw(pass *gpu.RenderPass, src *gpu.Texture) error {
	if pass == nil {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: FXAAPass.Draw called with nil render pass"}
	}
	bg, err := p.prepare(src)
	if err != nil {
		return err
	}
	pass.SetPipeline(p.pipeline)
	pass.SetBindGroup(0, bg)
	pass.Draw(3, 0)
	return nil
}

// Encode opens a single-attachment render pass on enc targeting dst (or the
// view's swapchain when dst == nil), draws FXAA from src, and ends the pass.
func (p *FXAAPass) Encode(enc *gpu.Encoder, src, dst *gpu.Texture) error {
	if enc == nil {
		return gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "render3d: FXAAPass.Encode called with nil encoder"}
	}
	bg, err := p.prepare(src)
	if err != nil {
		return err
	}
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: p.label,
		ColorAttachments: []gpu.ColorAttachment{{
			Texture: dst,
			LoadOp:  gpu.LoadClear,
			StoreOp: gpu.StoreStore,
		}},
	})
	pass.SetPipeline(p.pipeline)
	pass.SetBindGroup(0, bg)
	pass.Draw(3, 0)
	pass.End()
	return nil
}

// Close releases the pass's GPU resources immediately instead of waiting for
// scope disposal. It is safe to call more than once or on a nil pass.
func (p *FXAAPass) Close() error {
	if p == nil {
		return nil
	}
	var err error
	if p.bindGroup != nil {
		err = errors.Join(err, p.bindGroup.Close())
		p.bindGroup = nil
		p.boundTex = nil
	}
	if p.pipeline != nil {
		err = errors.Join(err, p.pipeline.Close())
		p.pipeline = nil
	}
	if p.layout != nil {
		err = errors.Join(err, p.layout.Close())
		p.layout = nil
	}
	if p.uniformBuf != nil {
		err = errors.Join(err, p.uniformBuf.Close())
		p.uniformBuf = nil
	}
	if p.sampler != nil {
		err = errors.Join(err, p.sampler.Close())
		p.sampler = nil
	}
	p.uniformValid = false
	return err
}

// ApplyFXAA executes the CPU reference implementation of [FXAAWGSL] (FXAA 3.11
// Quality) on src and returns a newly allocated anti-aliased [*image.RGBA] with
// the same bounds. Flat regions below the contrast threshold copy their source
// pixels unchanged; diagonal edges are smoothed along the detected edge span
// using bilinear clamp-to-edge sampling identical to the GPU pass.
func ApplyFXAA(src *image.RGBA, opts FXAAOptions) *image.RGBA {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(b)
	if w <= 0 || h <= 0 {
		return dst
	}

	s := opts.Sanitize()
	invX := float32(1.0) / float32(w)
	invY := float32(1.0) / float32(h)
	wf := float32(w)
	hf := float32(h)
	pix := src.Pix
	stride := src.Stride

	readTexel := func(x, y int) (float32, float32, float32) {
		if x < 0 {
			x = 0
		} else if x >= w {
			x = w - 1
		}
		if y < 0 {
			y = 0
		} else if y >= h {
			y = h - 1
		}
		off := y*stride + x*4
		const inv255 = float32(1.0 / 255.0)
		return float32(pix[off]) * inv255, float32(pix[off+1]) * inv255, float32(pix[off+2]) * inv255
	}

	sampleLinear := func(u, v float32) (float32, float32, float32) {
		if u < 0 {
			u = 0
		} else if u > 1 {
			u = 1
		}
		if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		fx := u*wf - 0.5
		fy := v*hf - 0.5
		x0 := int(math.Floor(float64(fx)))
		y0 := int(math.Floor(float64(fy)))
		tx := fx - float32(x0)
		ty := fy - float32(y0)
		r00, g00, b00 := readTexel(x0, y0)
		r10, g10, b10 := readTexel(x0+1, y0)
		r01, g01, b01 := readTexel(x0, y0+1)
		r11, g11, b11 := readTexel(x0+1, y0+1)
		oneMinusTX := 1.0 - tx
		oneMinusTY := 1.0 - ty
		r := (r00*oneMinusTX+r10*tx)*oneMinusTY + (r01*oneMinusTX+r11*tx)*ty
		g := (g00*oneMinusTX+g10*tx)*oneMinusTY + (g01*oneMinusTX+g11*tx)*ty
		bl := (b00*oneMinusTX+b10*tx)*oneMinusTY + (b01*oneMinusTX+b11*tx)*ty
		return r, g, bl
	}

	lumaAt := func(x, y int) float32 {
		r, g, bl := readTexel(x, y)
		return fxaaLuma(r, g, bl)
	}

	lumaSample := func(u, v float32) float32 {
		r, g, bl := sampleLinear(u, v)
		return fxaaLuma(r, g, bl)
	}

	for y := range h {
		srcRow := y * stride
		dstRow := y * dst.Stride
		uvY := (float32(y) + 0.5) * invY
		for x := range w {
			lumaM := lumaAt(x, y)
			lumaN := lumaAt(x, y-1)
			lumaS := lumaAt(x, y+1)
			lumaW := lumaAt(x-1, y)
			lumaE := lumaAt(x+1, y)

			lumaMin := min(lumaM, min(min(lumaN, lumaS), min(lumaW, lumaE)))
			lumaMax := max(lumaM, max(max(lumaN, lumaS), max(lumaW, lumaE)))
			lumaRange := lumaMax - lumaMin

			so := srcRow + x*4
			do := dstRow + x*4
			if lumaRange < max(s.EdgeThresholdMin, lumaMax*s.EdgeThreshold) {
				dst.Pix[do+0] = pix[so+0]
				dst.Pix[do+1] = pix[so+1]
				dst.Pix[do+2] = pix[so+2]
				dst.Pix[do+3] = pix[so+3]
				continue
			}

			lumaNW := lumaAt(x-1, y-1)
			lumaNE := lumaAt(x+1, y-1)
			lumaSW := lumaAt(x-1, y+1)
			lumaSE := lumaAt(x+1, y+1)

			lumaDownUp := lumaN + lumaS
			lumaLeftRight := lumaW + lumaE
			lumaLeftCorners := lumaNW + lumaSW
			lumaRightCorners := lumaNE + lumaSE
			lumaTopCorners := lumaNW + lumaNE
			lumaBottomCorners := lumaSW + lumaSE

			lumaAvg := (2.0*(lumaDownUp+lumaLeftRight) + (lumaLeftCorners + lumaRightCorners)) * (float32(1.0) / float32(12.0))
			subpix1 := aaAbs(lumaAvg-lumaM) / lumaRange
			if subpix1 < 0 {
				subpix1 = 0
			} else if subpix1 > 1 {
				subpix1 = 1
			}
			subpix2 := (-2.0*subpix1 + 3.0) * subpix1 * subpix1
			subpixOffset := subpix2 * subpix2 * s.SubpixelQuality

			edgeHorz := aaAbs(-2.0*lumaW+lumaLeftCorners) +
				2.0*aaAbs(-2.0*lumaM+lumaDownUp) +
				aaAbs(-2.0*lumaE+lumaRightCorners)
			edgeVert := aaAbs(-2.0*lumaN+lumaTopCorners) +
				2.0*aaAbs(-2.0*lumaM+lumaLeftRight) +
				aaAbs(-2.0*lumaS+lumaBottomCorners)
			isHorizontal := edgeHorz >= edgeVert

			var luma1, luma2 float32
			if isHorizontal {
				luma1, luma2 = lumaN, lumaS
			} else {
				luma1, luma2 = lumaW, lumaE
			}
			grad1 := aaAbs(luma1 - lumaM)
			grad2 := aaAbs(luma2 - lumaM)
			is1Steepest := grad1 >= grad2
			gradScaled := float32(0.25) * max(grad1, grad2)

			var stepLength float32
			if isHorizontal {
				stepLength = invY
			} else {
				stepLength = invX
			}
			lumaLocalAvg := float32(0.5) * (luma2 + lumaM)
			if is1Steepest {
				stepLength = -stepLength
				lumaLocalAvg = float32(0.5) * (luma1 + lumaM)
			}

			uvX := (float32(x) + 0.5) * invX
			curX, curY := uvX, uvY
			var offX, offY float32
			if isHorizontal {
				curY += stepLength * 0.5
				offX = invX
			} else {
				curX += stepLength * 0.5
				offY = invY
			}

			uv1X, uv1Y := curX-offX, curY-offY
			uv2X, uv2Y := curX+offX, curY+offY

			lumaEnd1 := lumaSample(uv1X, uv1Y) - lumaLocalAvg
			lumaEnd2 := lumaSample(uv2X, uv2Y) - lumaLocalAvg
			reached1 := aaAbs(lumaEnd1) >= gradScaled
			reached2 := aaAbs(lumaEnd2) >= gradScaled

			for i := 1; i < 11; i++ {
				if reached1 && reached2 {
					break
				}
				step := fxaaStep(i)
				if !reached1 {
					uv1X -= offX * step
					uv1Y -= offY * step
					lumaEnd1 = lumaSample(uv1X, uv1Y) - lumaLocalAvg
					reached1 = aaAbs(lumaEnd1) >= gradScaled
				}
				if !reached2 {
					uv2X += offX * step
					uv2Y += offY * step
					lumaEnd2 = lumaSample(uv2X, uv2Y) - lumaLocalAvg
					reached2 = aaAbs(lumaEnd2) >= gradScaled
				}
			}

			var dist1, dist2 float32
			if isHorizontal {
				dist1 = uvX - uv1X
				dist2 = uv2X - uvX
			} else {
				dist1 = uvY - uv1Y
				dist2 = uv2Y - uvY
			}
			isDir1 := dist1 < dist2
			distFinal := min(dist1, dist2)
			edgeThickness := dist1 + dist2

			lumaCenterSmaller := lumaM < lumaLocalAvg
			endLuma := lumaEnd2
			if isDir1 {
				endLuma = lumaEnd1
			}
			correctVariation := (endLuma < 0) != lumaCenterSmaller

			var pixelOffset float32
			if correctVariation && edgeThickness > 0 {
				pixelOffset = max(-distFinal/edgeThickness+0.5, 0)
			}
			finalOffset := max(pixelOffset, subpixOffset)

			finalX, finalY := uvX, uvY
			if isHorizontal {
				finalY += finalOffset * stepLength
			} else {
				finalX += finalOffset * stepLength
			}
			r, g, bl := sampleLinear(finalX, finalY)
			dst.Pix[do+0] = fxaaToByte(r)
			dst.Pix[do+1] = fxaaToByte(g)
			dst.Pix[do+2] = fxaaToByte(bl)
			dst.Pix[do+3] = pix[so+3]
		}
	}
	return dst
}

func fxaaLuma(r, g, b float32) float32 {
	return r*0.299 + g*0.587 + b*0.114
}

func fxaaStep(i int) float32 {
	switch {
	case i < 4:
		return 1.0
	case i == 4:
		return 1.5
	case i < 9:
		return 2.0
	case i == 9:
		return 4.0
	default:
		return 8.0
	}
}

func fxaaToByte(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255.0 + 0.5)
}

func aaFinite(f float32) bool {
	return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
}

func aaAbs(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
