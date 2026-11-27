package render3d

import (
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/gpu"
)

func TestUploadMeshClosesVertexBufferWhenIndexBufferFails(t *testing.T) {
	// On a real device the index buffer fails only past Limits.MaxBufferSize (256 MiB on the software device), so the second create is failed directly instead of allocating that much.
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	m := Mesh{
		Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}},
		Indices:   []uint32{0, 1, 2},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	fail := gpu.Error{Code: gpu.ErrInvalidDescriptor, Message: "index buffer too large"}
	var created []*gpu.Buffer
	g, err := uploadMesh(m, MeshUpload{Label: "big"}, func(desc gpu.BufferDesc) (*gpu.Buffer, error) {
		if len(created) == 1 {
			if desc.Usage&gpu.UsageIndex == 0 {
				t.Errorf("second buffer usage = %v, want an index buffer", desc.Usage)
			}
			return nil, fail
		}
		buf, err := dev.CreateBuffer(nil, desc)
		created = append(created, buf)
		return buf, err
	})
	if g != nil {
		t.Fatal("uploadMesh returned a mesh after the index buffer failed")
	}
	var gerr gpu.Error
	if !errors.As(err, &gerr) || gerr.Code != gpu.ErrInvalidDescriptor {
		t.Fatalf("uploadMesh error = %v, want the index buffer's gpu.Error", err)
	}
	if len(created) != 1 || created[0] == nil {
		t.Fatalf("created %d vertex buffers, want 1", len(created))
	}
	if created[0].Valid() {
		t.Error("vertex buffer left open after the index buffer failed")
	}
}
