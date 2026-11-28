package project_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
)

const deliveryYAML = assetsHead + `export:
  presets:
    - platform: miniprogram
      targets: [miniprogram_wechat]
      delivery:
        main: [boot, ui]
        subpackages:
          chapter1: [level1, level2]
          chapter2: [level3]
        remote: [music]
        caps:
          main: 4194304
          total: 31457280
`

func TestAssetDeliveryRoundTripsAndClonesDeep(t *testing.T) {
	f, err := project.Parse([]byte(deliveryYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	d := f.Export.Presets[0].Delivery
	want := &project.AssetDelivery{
		Main:        []string{"boot", "ui"},
		Subpackages: map[string][]string{"chapter1": {"level1", "level2"}, "chapter2": {"level3"}},
		Remote:      []string{"music"},
		Caps:        &project.AssetCaps{Main: 4 << 20, Total: 30 << 20},
	}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("Delivery = %+v, want %+v", d, want)
	}
	out, err := project.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	again, err := project.Parse(out)
	if err != nil {
		t.Fatalf("Parse(Encode): %v", err)
	}
	if !reflect.DeepEqual(again, f) {
		t.Fatalf("Parse(Encode(f)) = %+v, want %+v", again, f)
	}
	if out2, err := project.Encode(again); err != nil || !bytes.Equal(out2, out) {
		t.Fatalf("Encode is not stable (%v):\n%s\nwant:\n%s", err, out2, out)
	}

	c := f.Clone()
	if !reflect.DeepEqual(c, f) {
		t.Fatal("Clone differs from the original")
	}
	cd := c.Export.Presets[0].Delivery
	cd.Main[0] = "other"
	cd.Subpackages["chapter1"][0] = "other"
	cd.Subpackages["chapter3"] = []string{"x"}
	cd.Remote[0] = "other"
	cd.Caps.Main = 1
	if !reflect.DeepEqual(f, project.MustParse([]byte(deliveryYAML))) {
		t.Fatal("changing the clone's delivery changed the original")
	}
}

func TestAssetDeliveryOmittedStaysNil(t *testing.T) {
	f := project.MustParse([]byte(assetsHead + "export:\n  presets:\n    - platform: web\n"))
	if f.Export.Presets[0].Delivery != nil {
		t.Fatalf("Delivery = %+v, want nil", f.Export.Presets[0].Delivery)
	}
}

func TestAssetDeliveryRejectsInvalidPlacement(t *testing.T) {
	head := assetsHead + "export:\n  presets:\n    - platform: miniprogram\n      delivery:\n"
	for _, tc := range []struct {
		name, body, want string
	}{
		{"group in main and remote", "        main: [ui]\n        remote: [ui]\n", `asset group "ui" appears in both main and remote`},
		{"group in two subpackages", "        subpackages:\n          b: [lvl]\n          a: [lvl]\n", `asset group "lvl" appears in both subpackage a and subpackage b`},
		{"group twice in main", "        main: [ui, ui]\n", `asset group "ui" appears in both main and main`},
		{"empty group", "        remote: ['']\n", "empty asset group in remote"},
		{"subpackage name with a space", "        subpackages:\n          'chapter one': [lvl]\n", "must have no spaces"},
		{"negative main cap", "        caps: {main: -1}\n", "caps must be non-negative"},
		{"negative total cap", "        caps: {total: -1}\n", "caps must be non-negative"},
		{"unknown field", "        cdn: [music]\n", "cdn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := project.Parse([]byte(head + tc.body))
			if err == nil {
				t.Fatal("Parse accepted the file")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
