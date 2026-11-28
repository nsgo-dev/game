package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/nsgo/core"
)

func makeRectFrame(w, h int, bg color.RGBA, rect image.Rectangle, fg color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := bg
			if image.Pt(x, y).In(rect) {
				c = fg
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestScreenshotCompareIdenticalAndMovedSprite(t *testing.T) {
	dir := t.TempDir()
	bg := color.RGBA{R: 16, G: 24, B: 40, A: 255}
	fg := color.RGBA{R: 240, G: 80, B: 60, A: 255}

	spriteRect := image.Rect(10, 12, 26, 28) // 16x16 sprite at (10,12)
	current := makeRectFrame(120, 80, bg, spriteRect, fg)

	b := mcp.New(mcp.Config{
		CaptureFrame: func(context.Context) (*image.RGBA, error) {
			return current, nil
		},
		ScreenshotDir: dir,
	})

	call := func(args map[string]any) mcp.ScreenshotResult {
		t.Helper()
		payload, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := b.CallTool(context.Background(), mcp.ToolScreenshot, payload)
		if err != nil {
			t.Fatalf("game_screenshot(%s): %v", payload, err)
		}
		var res mcp.ScreenshotResult
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	// 1. Save initial frame as baseline "before".
	baseRes := call(map[string]any{"baseline": "before"})
	if baseRes.Baseline != "before" || baseRes.Comparison != nil {
		t.Fatalf("baseRes = %+v", baseRes)
	}

	// 2. Identical frame reports zero changed pixels, gametest_match=true, within_tolerance=true, no boxes.
	sameRes := call(map[string]any{"compare_to": "before"})
	if sameRes.Comparison == nil {
		t.Fatal("expected Comparison on compare_to")
	}
	cmpSame := sameRes.Comparison
	if cmpSame.Source != "baseline" || cmpSame.ChangedPixels != 0 || cmpSame.ExactChangedPixels != 0 || cmpSame.ChangedPercent != 0 || !cmpSame.GametestMatch || !cmpSame.WithinTolerance {
		t.Fatalf("identical compare = %+v", cmpSame)
	}
	if cmpSame.ChangedBounds != nil || len(cmpSame.ChangedRegions) != 0 || cmpSame.TotalRegions != 0 {
		t.Fatalf("expected no changed regions for identical frame: %+v", cmpSame)
	}

	// 3. Move the 16x16 sprite to (60, 30) — non-overlapping, producing two 16x16 changed boxes!
	beforeCopy := makeRectFrame(120, 80, bg, spriteRect, fg)
	movedRect := image.Rect(60, 30, 76, 46)
	current = makeRectFrame(120, 80, bg, movedRect, fg)

	movedRes := call(map[string]any{
		"baseline":           "after",
		"compare_to":         "before",
		"diff_path":          "diffs/moved.png",
		"include_diff_image": true, // diff_path takes precedence over inline base64
	})
	cmpMoved := movedRes.Comparison
	if cmpMoved == nil {
		t.Fatal("expected Comparison")
	}
	wantChanged := 16*16 + 16*16
	if cmpMoved.ChangedPixels != wantChanged || cmpMoved.ExactChangedPixels != wantChanged {
		t.Fatalf("changed_pixels=%d exact=%d, want %d", cmpMoved.ChangedPixels, cmpMoved.ExactChangedPixels, wantChanged)
	}
	if cmpMoved.GametestMatch || cmpMoved.WithinTolerance {
		t.Fatalf("expected match=false for moved sprite: %+v", cmpMoved)
	}
	if cmpMoved.TotalRegions != 2 || len(cmpMoved.ChangedRegions) != 2 {
		t.Fatalf("changed_regions = %+v (total=%d), want 2 regions", cmpMoved.ChangedRegions, cmpMoved.TotalRegions)
	}
	// Deterministic ordering: tied at 256 pixels, sorted by Y asc then X asc.
	wantR0 := mcp.DiffRegion{X: 10, Y: 12, Width: 16, Height: 16, Pixels: 256}
	wantR1 := mcp.DiffRegion{X: 60, Y: 30, Width: 16, Height: 16, Pixels: 256}
	if cmpMoved.ChangedRegions[0] != wantR0 || cmpMoved.ChangedRegions[1] != wantR1 {
		t.Fatalf("regions = %+v, want [%+v, %+v]", cmpMoved.ChangedRegions, wantR0, wantR1)
	}
	wantUnion := mcp.DiffRegion{X: 10, Y: 12, Width: 66, Height: 34, Pixels: 512}
	if cmpMoved.ChangedBounds == nil || *cmpMoved.ChangedBounds != wantUnion {
		t.Fatalf("changed_bounds = %+v, want %+v", cmpMoved.ChangedBounds, wantUnion)
	}

	// Verify exact agreement with gametest.DiffImage!
	gtDiff, gtMismatched := gametest.DiffImage(current, beforeCopy)
	if cmpMoved.ExactChangedPixels != gtMismatched {
		t.Fatalf("ExactChangedPixels=%d != gametest.DiffImage=%d", cmpMoved.ExactChangedPixels, gtMismatched)
	}
	if cmpMoved.DiffPath != "diffs/moved.png" || cmpMoved.DiffPNGBase64 != "" || cmpMoved.DiffBytes == 0 {
		t.Fatalf("diff output metadata = %+v", cmpMoved)
	}
	diffDiskBytes, err := os.ReadFile(filepath.Join(dir, "diffs", "moved.png"))
	if err != nil {
		t.Fatalf("read diff file: %v", err)
	}
	diffDiskImg, err := png.Decode(bytes.NewReader(diffDiskBytes))
	if err != nil {
		t.Fatalf("decode diff png: %v", err)
	}
	if _, diffOfDiff := gametest.DiffImage(diffDiskImg, gtDiff); diffOfDiff != 0 {
		t.Fatalf("written diff PNG differed from gametest.DiffImage by %d pixels", diffOfDiff)
	}

	// 4. Overlapping sprite move merges into a single changed bounding box, and inline diff_png_base64 works.
	current = makeRectFrame(120, 80, bg, image.Rect(18, 12, 34, 28), fg)
	overlapRes := call(map[string]any{
		"compare_to":         "before",
		"include_diff_image": true,
	})
	cmpOverlap := overlapRes.Comparison
	if cmpOverlap.TotalRegions != 2 {
		// Note: interior [18..26) stays identical fg color, so left strip [10..18) and right strip [26..34) are two disjoint 8x16 strips!
		t.Fatalf("overlap regions = %+v", cmpOverlap.ChangedRegions)
	}
	if cmpOverlap.ChangedBounds == nil || cmpOverlap.ChangedBounds.X != 10 || cmpOverlap.ChangedBounds.Y != 12 || cmpOverlap.ChangedBounds.Width != 24 || cmpOverlap.ChangedBounds.Height != 16 {
		t.Fatalf("overlap union bounds = %+v", cmpOverlap.ChangedBounds)
	}
	if cmpOverlap.DiffPNGBase64 == "" {
		t.Fatal("expected inline diff_png_base64")
	}
	rawDiffPNG, err := base64.StdEncoding.DecodeString(cmpOverlap.DiffPNGBase64)
	if err != nil {
		t.Fatalf("decode diff_png_base64: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(rawDiffPNG)); err != nil {
		t.Fatalf("invalid diff PNG in base64: %v", err)
	}
}

func TestScreenshotCompareToleranceDimensionMismatchAndProjectFile(t *testing.T) {
	projectDir := t.TempDir()
	screenshotDir := t.TempDir()

	// Write a project-relative golden PNG under ProjectDir.
	goldenRel := "testdata/golden/frame.png"
	goldenAbs := filepath.Join(projectDir, filepath.FromSlash(goldenRel))
	if err := os.MkdirAll(filepath.Dir(goldenAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	baseFrame := makeRectFrame(40, 20, color.RGBA{R: 100, G: 100, B: 100, A: 255}, image.Rect(5, 5, 15, 15), color.RGBA{R: 200, G: 50, B: 50, A: 255})
	if err := gametest.WritePNG(goldenAbs, baseFrame); err != nil {
		t.Fatal(err)
	}

	// Slight 3-unit channel shift on 10x10 sprite (100 pixels out of 800 = 12.5%).
	slightShift := makeRectFrame(40, 20, color.RGBA{R: 100, G: 100, B: 100, A: 255}, image.Rect(5, 5, 15, 15), color.RGBA{R: 203, G: 50, B: 50, A: 255})
	current := slightShift

	b := mcp.New(mcp.Config{
		CaptureFrame: func(context.Context) (*image.RGBA, error) {
			return current, nil
		},
		ProjectDir:    projectDir,
		ScreenshotDir: screenshotDir,
	})

	call := func(args map[string]any) (mcp.ScreenshotResult, error) {
		payload, _ := json.Marshal(args)
		raw, err := b.CallTool(context.Background(), mcp.ToolScreenshot, payload)
		if err != nil {
			return mcp.ScreenshotResult{}, err
		}
		var res mcp.ScreenshotResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return mcp.ScreenshotResult{}, err
		}
		return res, nil
	}

	// Exact comparison against project-relative file fails gametest_match & within_tolerance.
	exactRes, err := call(map[string]any{"compare_to": goldenRel})
	if err != nil {
		t.Fatalf("compare_to file: %v", err)
	}
	if exactRes.Comparison.Source != "file" || exactRes.Comparison.ExactChangedPixels != 100 || exactRes.Comparison.ChangedPixels != 100 || exactRes.Comparison.MaxChannelDiff != 3 || exactRes.Comparison.GametestMatch || exactRes.Comparison.WithinTolerance {
		t.Fatalf("exactRes = %+v", exactRes.Comparison)
	}

	// Per-channel tolerance=3 absorbs the 3-unit delta (within_tolerance=true, gametest_match=false).
	tolRes, err := call(map[string]any{"compare_to": goldenRel, "tolerance": 3})
	if err != nil {
		t.Fatalf("compare_to with tolerance: %v", err)
	}
	if tolRes.Comparison.ChangedPixels != 0 || tolRes.Comparison.ExactChangedPixels != 100 || tolRes.Comparison.GametestMatch || !tolRes.Comparison.WithinTolerance {
		t.Fatalf("tolRes = %+v", tolRes.Comparison)
	}

	// Pixel-count & percent thresholds.
	pctPass, err := call(map[string]any{"compare_to": goldenRel, "max_mismatched_pixels": 100, "max_mismatched_percent": 12.5})
	if err != nil || !pctPass.Comparison.WithinTolerance {
		t.Fatalf("pctPass = %+v, err=%v", pctPass.Comparison, err)
	}
	pctFail, err := call(map[string]any{"compare_to": goldenRel, "max_mismatched_pixels": 99})
	if err != nil || pctFail.Comparison.WithinTolerance {
		t.Fatalf("pctFail = %+v, err=%v", pctFail.Comparison, err)
	}

	// Dimension mismatch never passes even with generous tolerance.
	current = makeRectFrame(50, 25, color.RGBA{R: 100, G: 100, B: 100, A: 255}, image.Rectangle{}, color.RGBA{})
	dimRes, err := call(map[string]any{
		"compare_to":             goldenRel,
		"tolerance":              255,
		"max_mismatched_percent": 100,
		"include_diff_image":     true,
	})
	if err != nil {
		t.Fatalf("dim mismatch compare: %v", err)
	}
	dc := dimRes.Comparison
	if !dc.DimensionMismatch || dc.GametestMatch || dc.WithinTolerance || dc.BaselineWidth != 40 || dc.BaselineHeight != 20 || dc.TotalPixels != 50*25 {
		t.Fatalf("dimRes = %+v", dc)
	}
	diffRaw, err := base64.StdEncoding.DecodeString(dc.DiffPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	diffImg, err := png.Decode(bytes.NewReader(diffRaw))
	if err != nil {
		t.Fatal(err)
	}
	if diffImg.Bounds().Dx() != 50 || diffImg.Bounds().Dy() != 25 {
		t.Fatalf("diff image bounds = %v, want 50x25", diffImg.Bounds())
	}

	// A PNG whose header declares a frame larger than any screenshot is refused before it is decoded.
	if err := os.WriteFile(filepath.Join(projectDir, "huge.png"), pngWithDeclaredSize(t, 20000, 20000), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := call(map[string]any{"compare_to": "huge.png"}); err == nil || !strings.Contains(err.Error(), "exceed maximum") {
		t.Fatalf("compare_to huge.png: err = %v, want a dimensions error", err)
	}

	// Path traversal and validation errors are rejected cleanly.
	for _, bad := range []map[string]any{
		{"compare_to": "../escape.png"},
		{"compare_to": "/etc/passwd.png"},
		{"compare_to": "no-such-baseline"},
		{"tolerance": 5},
		{"include_diff_image": true},
		{"compare_to": goldenRel, "tolerance": 300},
		{"compare_to": goldenRel, "max_mismatched_percent": 101},
	} {
		if _, err := call(bad); err == nil {
			t.Errorf("expected error for %v, got nil", bad)
		}
	}
}

// pngWithDeclaredSize encodes a 1x1 PNG and rewrites its IHDR width and height (and the chunk's CRC), so the header declares w x h while the file stays a few bytes long.
func pngWithDeclaredSize(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	// 8-byte signature, then IHDR: 4-byte length, 4-byte type, 13-byte data, 4-byte CRC.
	const ihdrType, ihdrData = 12, 16
	binary.BigEndian.PutUint32(data[ihdrData:], w)
	binary.BigEndian.PutUint32(data[ihdrData+4:], h)
	binary.BigEndian.PutUint32(data[ihdrData+13:], crc32.ChecksumIEEE(data[ihdrType:ihdrData+13]))
	return data
}

func TestScreenshotBaselineEviction(t *testing.T) {
	b := mcp.New(mcp.Config{
		DrawFrame: func(d *core.Drawing, w, h int) {
			d.SetFillColor(core.RGB(0x112233))
			d.FillRect(0, 0, float32(w), float32(h))
		},
	})
	for i := range mcp.MaxScreenshotBaselines + 2 {
		payload, _ := json.Marshal(map[string]any{
			"width":    16,
			"height":   16,
			"baseline": fmt.Sprintf("b-%02d", i),
		})
		if _, err := b.CallTool(context.Background(), mcp.ToolScreenshot, payload); err != nil {
			t.Fatalf("save baseline %d: %v", i, err)
		}
	}
	// Oldest baseline "b-00" was evicted; newest "b-17" is still present.
	if _, err := b.CallTool(context.Background(), mcp.ToolScreenshot, json.RawMessage(`{"width":16,"height":16,"compare_to":"b-00"}`)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected b-00 evicted error, got %v", err)
	}
	if _, err := b.CallTool(context.Background(), mcp.ToolScreenshot, json.RawMessage(`{"width":16,"height":16,"compare_to":"b-17"}`)); err != nil {
		t.Fatalf("compare to newest baseline failed: %v", err)
	}
}
