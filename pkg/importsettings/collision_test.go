package importsettings_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/importsettings"
)

func TestCollisionAndPointsRoundTrip(t *testing.T) {
	in := &importsettings.File{
		Version: 1,
		Image: &importsettings.Image{
			Slice: &importsettings.Slice{Width: 16, Height: 16, Count: 4},
			Collision: &importsettings.Collision{
				Mode:        "alpha",
				Tolerance:   16,
				MaxVertices: 6,
				Cells: []importsettings.CellCollision{
					{Cell: 1, Mode: "box", Tolerance: 8},
					{Cell: 2, Mode: "polygon", MaxVertices: 8, Points: []importsettings.Point2D{{2, 2}, {14, 2}, {14, 14}, {2, 14}}},
					{Cell: 3, Mode: "none"},
				},
			},
			Points: map[string]importsettings.Point2D{
				"feet":   {8, 16},
				"muzzle": {14, 6},
			},
			CellPoints: []importsettings.CellPoints{
				{
					Cell: 1,
					Points: map[string]importsettings.Point2D{
						"muzzle": {15, 5},
					},
				},
			},
		},
	}

	data, err := importsettings.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	parsed, err := importsettings.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(parsed, in) {
		t.Fatalf("round-trip mismatch:\ngot:  %+v\nwant: %+v", parsed.Image, in.Image)
	}
	again, err := importsettings.Encode(parsed)
	if err != nil {
		t.Fatalf("second Encode: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("Encode not deterministic:\nfirst:\n%s\nsecond:\n%s", data, again)
	}

	// Verify ForCell lookup and PointsForCell merging.
	spec0, ok := parsed.Image.Collision.ForCell(0)
	if !ok || spec0.Mode != "alpha" || spec0.Tolerance != 16 || spec0.MaxVertices != 6 {
		t.Fatalf("cell 0 collision = %+v, %v", spec0, ok)
	}
	spec1, ok := parsed.Image.Collision.ForCell(1)
	if !ok || spec1.Mode != "box" || spec1.Tolerance != 8 {
		t.Fatalf("cell 1 collision = %+v, %v", spec1, ok)
	}
	if _, ok := parsed.Image.Collision.ForCell(3); ok {
		t.Fatal("cell 3 mode=none should return ok=false")
	}

	pts0 := parsed.Image.PointsForCell(0)
	if pts0["muzzle"] != (importsettings.Point2D{14, 6}) || pts0["feet"] != (importsettings.Point2D{8, 16}) {
		t.Fatalf("cell 0 points = %v", pts0)
	}
	pts1 := parsed.Image.PointsForCell(1)
	if pts1["muzzle"] != (importsettings.Point2D{15, 5}) || pts1["feet"] != (importsettings.Point2D{8, 16}) {
		t.Fatalf("cell 1 points = %v", pts1)
	}
}

func TestParseRefusesInvalidCollisionAndPoints(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"empty collision", "version: 1\nimage:\n  collision: {}\n", "must set mode or at least one cell"},
		{"unknown collision field", "version: 1\nimage:\n  collision:\n    mode: alpha\n    extra: 1\n", "extra"},
		{"bad collision mode", "version: 1\nimage:\n  collision:\n    mode: diamond\n", "mode"},
		{"tolerance negative", "version: 1\nimage:\n  collision:\n    mode: alpha\n    tolerance: -1\n", "tolerance"},
		{"tolerance over 255", "version: 1\nimage:\n  collision:\n    mode: alpha\n    tolerance: 256\n", "tolerance"},
		{"maxVertices 2", "version: 1\nimage:\n  collision:\n    mode: alpha\n    maxVertices: 2\n", "maxVertices"},
		{"maxVertices 9", "version: 1\nimage:\n  collision:\n    mode: alpha\n    maxVertices: 9\n", "maxVertices"},
		{"polygon too few points", "version: 1\nimage:\n  collision:\n    mode: polygon\n    points: [[0, 0], [8, 0]]\n", "at least 3"},
		{"points on box mode", "version: 1\nimage:\n  collision:\n    mode: box\n    points: [[0, 0], [8, 0], [8, 8]]\n", "polygon mode"},
		{"collision cells without slice", "version: 1\nimage:\n  collision:\n    cells:\n      - cell: 0\n        mode: box\n", "require image.slice"},
		{"collision cell unknown field", "version: 1\nimage:\n  slice: {width: 8, height: 8}\n  collision:\n    cells:\n      - cell: 0\n        mode: box\n        bogus: true\n", "bogus"},
		{"collision cell unsorted", "version: 1\nimage:\n  slice: {width: 8, height: 8}\n  collision:\n    cells:\n      - cell: 2\n        mode: box\n      - cell: 1\n        mode: box\n", "ascending"},
		{"collision cell duplicate", "version: 1\nimage:\n  slice: {width: 8, height: 8}\n  collision:\n    cells:\n      - cell: 1\n        mode: box\n      - cell: 1\n        mode: alpha\n", "duplicate"},
		{"collision cell out of range", "version: 1\nimage:\n  slice: {width: 8, height: 8, count: 2}\n  collision:\n    cells:\n      - cell: 2\n        mode: box\n", "out of range"},
		{"point wrong length", "version: 1\nimage:\n  points:\n    muzzle: [14]\n", "sequence of 2"},
		{"point non numeric", "version: 1\nimage:\n  points:\n    muzzle: [14, \"six\"]\n", "number"},
		{"point empty name", "version: 1\nimage:\n  points:\n    \"\": [1, 2]\n", "empty"},
		{"cellPoints without slice", "version: 1\nimage:\n  cellPoints:\n    - cell: 0\n      points: {muzzle: [1, 2]}\n", "require image.slice"},
		{"cellPoints unknown field", "version: 1\nimage:\n  slice: {width: 8, height: 8}\n  cellPoints:\n    - cell: 0\n      points: {muzzle: [1, 2]}\n      foo: bar\n", "foo"},
		{"cellPoints empty map", "version: 1\nimage:\n  slice: {width: 8, height: 8}\n  cellPoints:\n    - cell: 0\n      points: {}\n", "at least one point"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := importsettings.Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Parse accepted invalid YAML:\n%s", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
