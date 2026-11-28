package render3d_test

import (
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/nsgo/gpu"
	"testing"
)

func TestAttachPipelineCacheNeedsDevice(t *testing.T) {
	if _, err := render3d.AttachPipelineCache(nil, nil); err != gpu.ErrDeviceLost {
		t.Fatalf("err=%v", err)
	}
}
