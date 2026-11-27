package render3d

import (
	"errors"
	"testing"

	"github.com/nsgo-dev/nsgo/gpu"
)

// NSGo v0.3.0's devices, the software one included, report every core format as supported, so these paths cannot be reached through NewHDRTarget in a test; they are checked directly.
func TestHDRColorFormatFallback(t *testing.T) {
	if f, err := hdrColorFormat(true, true); err != nil || f != gpu.FormatRGBA16Float {
		t.Errorf("supported: %v, %v", f, err)
	}
	if f, err := hdrColorFormat(false, false); err != nil || f != gpu.FormatRGBA8Unorm {
		t.Errorf("unsupported with fallback: %v, %v, want RGBA8Unorm", f, err)
	}
	if _, err := hdrColorFormat(false, true); !errors.Is(err, gpu.ErrUnsupported) {
		t.Errorf("unsupported with DisableFallback: err = %v, want ErrUnsupported", err)
	}

	unsupported := gpu.Error{Code: gpu.ErrUnsupported, Message: "no float render targets"}
	if f, ok := hdrFallbackFormat(gpu.FormatRGBA16Float, false, unsupported); !ok || f != gpu.FormatRGBA8Unorm {
		t.Errorf("CreateRenderTarget ErrUnsupported: %v, %v, want retry with RGBA8Unorm", f, ok)
	}
	for _, tc := range []struct {
		name    string
		format  gpu.TextureFormat
		disable bool
		err     error
	}{
		{"success", gpu.FormatRGBA16Float, false, nil},
		{"fallback disabled", gpu.FormatRGBA16Float, true, unsupported},
		{"already RGBA8", gpu.FormatRGBA8Unorm, false, unsupported},
		{"other error", gpu.FormatRGBA16Float, false, gpu.Error{Code: gpu.ErrOutOfMemory, Message: "oom"}},
	} {
		if _, ok := hdrFallbackFormat(tc.format, tc.disable, tc.err); ok {
			t.Errorf("%s: retried, want no retry", tc.name)
		}
	}
}
