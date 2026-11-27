package gameui

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

func TestDeviceClassFilterDisjointAndSubtree(t *testing.T) {
	allClasses := []DeviceClass{DevicePhone, DeviceTablet, DeviceDesktop, DeviceUltrawide, ""}

	unrestricted := intersectDeviceClasses(deviceClassFilter{}, nil)
	if unrestricted.isEmpty() {
		t.Fatal("unrestricted filter reported isEmpty")
	}
	for _, dc := range allClasses {
		if !unrestricted.matches(dc) {
			t.Errorf("unrestricted.matches(%q) = false, want true", dc)
		}
	}

	phoneOnly := intersectDeviceClasses(unrestricted, []DeviceClass{DevicePhone})
	disjoint := intersectDeviceClasses(phoneOnly, []DeviceClass{DeviceTablet})
	if !disjoint.isEmpty() {
		t.Fatalf("disjoint filter isEmpty = false, got %+v", disjoint)
	}
	grandchildUnrestricted := intersectDeviceClasses(disjoint, nil)
	grandchildPhone := intersectDeviceClasses(disjoint, []DeviceClass{DevicePhone})

	for _, tc := range []struct {
		name   string
		filter deviceClassFilter
	}{
		{"disjoint", disjoint},
		{"grandchild_unrestricted", grandchildUnrestricted},
		{"grandchild_phone", grandchildPhone},
	} {
		for _, dc := range allClasses {
			if tc.filter.matches(dc) {
				t.Errorf("%s.matches(%q) = true, want false", tc.name, dc)
			}
		}
	}
}

func TestBuildScreenDisjointSubtreeHiddenOnEveryDeviceClassAndSetDeviceClass(t *testing.T) {
	// Bypass ValidateScreen via unexported buildScreen to prove runtime filtering
	// never treats an empty parent/child intersection as "matches every device".
	doc := &ScreenDoc{
		Version:      ScreenFormatVersion,
		Name:         "disjoint",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []ScreenNodeDoc{
			{
				ID:            "phone_box",
				Type:          NodeVBox,
				Anchor:        AnchorTopLeft,
				Width:         120,
				Height:        60,
				DeviceClasses: []DeviceClass{DevicePhone},
				Children: []ScreenNodeDoc{
					{
						ID:            "tablet_lbl",
						Type:          NodeLabel,
						Width:         100,
						Height:        20,
						TextKey:       "hud.tablet",
						DeviceClasses: []DeviceClass{DeviceTablet},
					},
					{
						ID:            "tablet_sub",
						Type:          NodeVBox,
						Width:         100,
						Height:        20,
						DeviceClasses: []DeviceClass{DeviceTablet},
						Children: []ScreenNodeDoc{
							{
								ID:      "grandchild_unrestricted",
								Type:    NodeLabel,
								Width:   80,
								Height:  16,
								TextKey: "hud.grandchild",
							},
							{
								ID:            "grandchild_phone",
								Type:          NodeLabel,
								Width:         80,
								Height:        16,
								TextKey:       "hud.grandchild_phone",
								DeviceClasses: []DeviceClass{DevicePhone},
							},
						},
					},
				},
			},
		},
	}

	allClasses := []DeviceClass{DevicePhone, DeviceTablet, DeviceDesktop, DeviceUltrawide, ""}
	disjointIDs := []string{"tablet_lbl", "tablet_sub", "grandchild_unrestricted", "grandchild_phone"}
	bounds := core.Bounds{Size: core.Size{Width: 640, Height: 360}}

	// 1. Check initial Build on every device class (including empty).
	for _, dc := range allClasses {
		built, err := buildScreen(doc, ScreenBuildOptions{DeviceClass: dc})
		if err != nil {
			t.Fatalf("buildScreen(%q): %v", dc, err)
		}
		built.Layout(bounds)

		wantParentHidden := dc != "" && dc != DevicePhone
		if got := built.Widget("phone_box").spec().Hidden; got != wantParentHidden {
			t.Errorf("build(%q): phone_box Hidden = %v, want %v", dc, got, wantParentHidden)
		}
		for _, id := range disjointIDs {
			w := built.Widget(id)
			if !w.spec().Hidden {
				t.Errorf("build(%q): disjoint node %q Hidden = false, want true", dc, id)
			}
			if b := w.Bounds(); b.Size.Width != 0 || b.Size.Height != 0 {
				t.Errorf("build(%q): disjoint node %q bounds = %+v, want zero", dc, id, b)
			}
		}
	}

	// 2. Build once and cycle SetDeviceClass through every device class (and empty).
	built, err := buildScreen(doc, ScreenBuildOptions{DeviceClass: DevicePhone})
	if err != nil {
		t.Fatalf("buildScreen: %v", err)
	}
	for _, dc := range []DeviceClass{DevicePhone, DeviceTablet, DeviceDesktop, DeviceUltrawide, "", DevicePhone} {
		built.SetDeviceClass(dc)
		built.Layout(bounds)

		wantParentHidden := dc != "" && dc != DevicePhone
		if got := built.Widget("phone_box").spec().Hidden; got != wantParentHidden {
			t.Errorf("SetDeviceClass(%q): phone_box Hidden = %v, want %v", dc, got, wantParentHidden)
		}
		for _, id := range disjointIDs {
			w := built.Widget(id)
			if !w.spec().Hidden {
				t.Errorf("SetDeviceClass(%q): disjoint node %q Hidden = false, want true", dc, id)
			}
			if b := w.Bounds(); b.Size.Width != 0 || b.Size.Height != 0 {
				t.Errorf("SetDeviceClass(%q): disjoint node %q bounds = %+v, want zero", dc, id, b)
			}
		}
	}
}
