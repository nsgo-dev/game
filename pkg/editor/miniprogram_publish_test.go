package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
)

func TestValidateMiniProgramPreflight(t *testing.T) {
	base := editor.MiniProgramPreflight{Provider: "wechat", AppID: "wx-demo", Version: "1.2.3", UploadKey: "0123456789abcdef", RemoteIP: "192.168.1.4", IPAllowlist: []string{"192.168.1.0/24"}}
	if err := editor.ValidateMiniProgramPreflight(base); err != nil {
		t.Fatal(err)
	}
	base.RemoteIP = "10.0.0.2"
	if err := editor.ValidateMiniProgramPreflight(base); err == nil {
		t.Fatal("out-of-range source IP accepted")
	}
	base.UploadKey = "short"
	if err := editor.ValidateMiniProgramPreflight(base); err == nil {
		t.Fatal("short upload key accepted")
	}
}

func TestMiniProgramPreviewQR(t *testing.T) {
	a, err := editor.EncodeMiniProgramPreviewQR("wechat", "wx-demo", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	b, err := editor.EncodeMiniProgramPreviewQR("wechat", "wx-demo", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != b.Size || a.Version != b.Version || a.Mask != b.Mask {
		t.Fatal("mini-program preview QR metadata is not deterministic")
	}
	for y := 0; y < a.Size; y++ {
		for x := 0; x < a.Size; x++ {
			if a.Black(x, y) != b.Black(x, y) {
				t.Fatal("mini-program preview QR matrix is not deterministic")
			}
		}
	}
}
