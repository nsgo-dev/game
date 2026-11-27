package render3d

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// TestLightmapRendererDropsBindGroupsOfClosedTextures streams lightmaps in and out and checks the texture bind-group cache does not keep groups for closed textures.
func TestLightmapRendererDropsBindGroupsOfClosedTextures(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	camBuf, err := NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	r, err := NewLightmapRenderer(nil, dev, camBuf.Layout(), LightmapRendererOptions{ColorFormat: gpu.FormatRGBA8Unorm, DisableDepth: true})
	if err != nil {
		t.Fatalf("NewLightmapRenderer: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	mesh, err := UploadLightmapMesh(nil, dev, LightmapMesh{
		Positions: []math3d.Vec3{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 0, Y: 1}},
	}, MeshUpload{})
	if err != nil {
		t.Fatalf("UploadLightmapMesh: %v", err)
	}
	t.Cleanup(func() { _ = mesh.Close() })

	for frame := range 5 {
		tex, err := UploadTexturePixels(nil, dev, PixelBuffer{Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{255, 255, 255, 255}}, TextureUpload{})
		if err != nil {
			t.Fatalf("frame %d UploadTexturePixels: %v", frame, err)
		}
		r.BeginFrame()
		if n := len(r.texGroups); n > 1 {
			t.Fatalf("frame %d: %d cached texture bind groups after BeginFrame, want at most 1", frame, n)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0)}}})
		mat := DefaultLightmapMaterial()
		mat.Lightmap = tex
		if err := r.Draw(pass, camBuf, mesh, math3d.Mat4Identity(), mat); err != nil {
			t.Fatalf("frame %d Draw: %v", frame, err)
		}
		pass.End()
		dev.Submit(enc.Finish())
		_ = tex.Close()
	}
	r.BeginFrame()
	if n := len(r.texGroups); n != 0 {
		t.Fatalf("%d cached texture bind groups after every lightmap was closed, want 0", n)
	}
}
