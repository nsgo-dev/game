package mcp

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const (
	// MaxScreenshotBaselines is the maximum number of named screenshot baselines retained in memory on a Bridge.
	MaxScreenshotBaselines = 16
	// MaxScreenshotBaselineBytes is the maximum combined decoded RGBA byte size retained across all in-memory screenshot baselines.
	MaxScreenshotBaselineBytes = 64 << 20

	maxDiffRegions = 32
)

// DiffRegion is an axis-aligned pixel bounding box of one 8-connected changed region in a screenshot comparison.
type DiffRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
	Pixels int `json:"pixels"`
}

// ScreenshotCompareResult summarizes the pixel and region differences between a captured screenshot and a baseline or reference PNG.
type ScreenshotCompareResult struct {
	CompareTo            string       `json:"compare_to"`
	Source               string       `json:"source"` // "baseline" or "file"
	BaselineWidth        int          `json:"baseline_width"`
	BaselineHeight       int          `json:"baseline_height"`
	DimensionMismatch    bool         `json:"dimension_mismatch,omitempty"`
	TotalPixels          int          `json:"total_pixels"`
	ChangedPixels        int          `json:"changed_pixels"`
	ExactChangedPixels   int          `json:"exact_changed_pixels"`
	ChangedPercent       float64      `json:"changed_percent"`
	MaxChannelDiff       uint8        `json:"max_channel_diff"`
	MeanChannelDiff      float64      `json:"mean_channel_diff"`
	Tolerance            uint8        `json:"tolerance,omitempty"`
	MaxMismatchedPixels  int          `json:"max_mismatched_pixels,omitempty"`
	MaxMismatchedPercent float64      `json:"max_mismatched_percent,omitempty"`
	GametestMatch        bool         `json:"gametest_match"`
	WithinTolerance      bool         `json:"within_tolerance"`
	ChangedBounds        *DiffRegion  `json:"changed_bounds,omitempty"`
	ChangedRegions       []DiffRegion `json:"changed_regions,omitempty"`
	TotalRegions         int          `json:"total_regions,omitempty"`
	DiffPath             string       `json:"diff_path,omitempty"`
	DiffPNGBase64        string       `json:"diff_png_base64,omitempty"`
	DiffBytes            int          `json:"diff_bytes,omitempty"`
}

type storedScreenshotBaseline struct {
	name      string
	img       image.Image
	rgbaBytes int
}

type screenshotBaselineStore struct {
	mu         sync.Mutex
	maxCount   int
	maxTotal   int
	totalBytes int
	order      []string
	byName     map[string]*storedScreenshotBaseline
}

func newScreenshotBaselineStore() *screenshotBaselineStore {
	return &screenshotBaselineStore{
		maxCount: MaxScreenshotBaselines,
		maxTotal: MaxScreenshotBaselineBytes,
		byName:   make(map[string]*storedScreenshotBaseline),
	}
}

func (s *screenshotBaselineStore) put(name string, img image.Image) ([]string, error) {
	if s == nil {
		return nil, errors.New("mcp: screenshot baseline store unavailable")
	}
	b := img.Bounds()
	size := b.Dx() * b.Dy() * 4
	if size <= 0 {
		return nil, errors.New("mcp: cannot store empty screenshot baseline")
	}
	if size > s.maxTotal {
		return nil, fmt.Errorf("mcp: screenshot baseline %q is %d RGBA bytes, exceeds maximum %d bytes", name, size, s.maxTotal)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if prev, ok := s.byName[name]; ok {
		s.totalBytes -= prev.rgbaBytes
		s.order = slices.DeleteFunc(s.order, func(k string) bool { return k == name })
		delete(s.byName, name)
	}
	var evicted []string
	for len(s.order) > 0 && (len(s.order) >= s.maxCount || s.totalBytes+size > s.maxTotal) {
		oldest := s.order[0]
		s.order = s.order[1:]
		if old, ok := s.byName[oldest]; ok {
			s.totalBytes -= old.rgbaBytes
			delete(s.byName, oldest)
			evicted = append(evicted, oldest)
		}
	}
	s.byName[name] = &storedScreenshotBaseline{
		name:      name,
		img:       img,
		rgbaBytes: size,
	}
	s.order = append(s.order, name)
	s.totalBytes += size
	return evicted, nil
}

func (s *screenshotBaselineStore) get(name string) (image.Image, []string) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.byName[name]; ok {
		return entry.img, nil
	}
	names := make([]string, 0, len(s.byName))
	for k := range s.byName {
		names = append(names, k)
	}
	slices.Sort(names)
	return nil, names
}

func (b *Bridge) ensureScreenshotBaselineStore() *screenshotBaselineStore {
	if b.screenshotBaselines == nil {
		b.screenshotBaselines = newScreenshotBaselineStore()
	}
	return b.screenshotBaselines
}

func (b *Bridge) resolveCompareTarget(target string) (img image.Image, source string, err error) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return nil, "", errors.New("mcp: compare_to is empty")
	}
	if stored, _ := b.ensureScreenshotBaselineStore().get(target); stored != nil {
		return stored, "baseline", nil
	}
	// If it looks like a relative PNG file path, try confined disk reads under ProjectDir / ScreenshotDir.
	if strings.HasSuffix(strings.ToLower(target), ".png") || strings.Contains(target, "/") {
		rel, pathErr := sanitizeScreenshotPath(target)
		if pathErr != nil {
			return nil, "", fmt.Errorf("mcp: compare_to %q: %w", target, pathErr)
		}
		var dirs []string
		if d := strings.TrimSpace(b.cfg.ProjectDir); d != "" {
			dirs = append(dirs, d)
		}
		if d := strings.TrimSpace(b.cfg.ScreenshotDir); d != "" && d != strings.TrimSpace(b.cfg.ProjectDir) {
			dirs = append(dirs, d)
		}
		if len(dirs) == 0 {
			return nil, "", fmt.Errorf("mcp: compare_to %q is not a stored baseline and neither Config.ProjectDir nor Config.ScreenshotDir is configured", target)
		}
		var lastErr error
		for _, dir := range dirs {
			data, readErr := readConfinedFile(dir, rel)
			if readErr == nil {
				decoded, decErr := png.Decode(bytes.NewReader(data))
				if decErr != nil {
					return nil, "", fmt.Errorf("mcp: decode compare_to PNG %q: %w", rel, decErr)
				}
				return decoded, "file", nil
			}
			if !errors.Is(readErr, os.ErrNotExist) {
				return nil, "", readErr
			}
			lastErr = readErr
		}
		return nil, "", fmt.Errorf("mcp: compare_to PNG %q not found: %w", rel, lastErr)
	}

	_, avail := b.ensureScreenshotBaselineStore().get(target)
	if len(avail) == 0 {
		return nil, "", fmt.Errorf("mcp: screenshot baseline %q not found (no baselines stored; pass baseline to game_screenshot or pass a project-relative .png path)", target)
	}
	return nil, "", fmt.Errorf("mcp: screenshot baseline %q not found (available: %s)", target, strings.Join(avail, ", "))
}

func readConfinedFile(dir, relSlash string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("mcp: open directory for compare_to: %w", err)
	}
	defer root.Close()

	f, err := root.Open(filepath.FromSlash(relSlash))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	const maxPNGBytes = 64 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxPNGBytes+1))
	if err != nil {
		return nil, fmt.Errorf("mcp: read compare_to %q: %w", relSlash, err)
	}
	if len(data) > maxPNGBytes {
		return nil, fmt.Errorf("mcp: compare_to %q exceeds %d bytes", relSlash, maxPNGBytes)
	}
	return data, nil
}

type screenshotDiffOptions struct {
	compareTo            string
	source               string
	channelTolerance     uint8
	maxMismatchedPixels  int
	maxMismatchedPercent float64
	buildDiffImage       bool
	diffDir              string
	diffRelPath          string
	includeDiffBase64    bool
}

type scanRun struct {
	x0, x1 int
	root   int
}

type regionAccum struct {
	parent int
	minX   int
	minY   int
	maxX   int
	maxY   int
	pixels int
}

// compareScreenshotImages compares got against want using gametest.DiffImage pixel semantics,
// optional per-channel tolerance, 8-connected changed-region bounding boxes, and optional visual diff PNG output.
func compareScreenshotImages(got, want image.Image, opts screenshotDiffOptions) (*ScreenshotCompareResult, error) {
	var gw, gh, ww, wh int
	var gb, wb image.Rectangle
	if got != nil {
		gb = got.Bounds()
		gw, gh = gb.Dx(), gb.Dy()
	}
	if want != nil {
		wb = want.Bounds()
		ww, wh = wb.Dx(), wb.Dy()
	}
	w := max(gw, ww)
	h := max(gh, wh)
	totalPixels := w * h
	dimMismatch := gw != ww || gh != wh

	var diffImg *image.RGBA
	if opts.buildDiffImage {
		diffImg = image.NewRGBA(image.Rect(0, 0, w, h))
	}

	magenta := color.RGBA{R: 255, G: 0, B: 255, A: 255}
	tol := int(opts.channelTolerance)

	var (
		exactChanged int
		changed      int
		maxDelta     uint8
		sumDelta     uint64

		hasBounds   bool
		unionMinX   int
		unionMinY   int
		unionMaxX   int
		unionMaxY   int
		unionPixels int

		uf       []regionAccum
		prevRuns []scanRun
		curRuns  []scanRun
	)

	findRoot := func(id int) int {
		r := id
		for uf[r].parent != r {
			r = uf[r].parent
		}
		for uf[id].parent != r {
			next := uf[id].parent
			uf[id].parent = r
			id = next
		}
		return r
	}
	unionRoots := func(a, b int) int {
		ra, rb := findRoot(a), findRoot(b)
		if ra == rb {
			return ra
		}
		if uf[ra].pixels < uf[rb].pixels {
			ra, rb = rb, ra
		}
		uf[rb].parent = ra
		uf[ra].minX = min(uf[ra].minX, uf[rb].minX)
		uf[ra].minY = min(uf[ra].minY, uf[rb].minY)
		uf[ra].maxX = max(uf[ra].maxX, uf[rb].maxX)
		uf[ra].maxY = max(uf[ra].maxY, uf[rb].maxY)
		uf[ra].pixels += uf[rb].pixels
		return ra
	}

	for y := 0; y < h; y++ {
		curRuns = curRuns[:0]
		inRun := false
		runStart := 0

		for x := 0; x < w; x++ {
			inGot := x < gw && y < gh
			inWant := x < ww && y < wh
			var (
				isExactDiff bool
				isChanged   bool
				gr, gg, gbV uint32
				ga          uint32
			)
			if !inGot || !inWant {
				isExactDiff = true
				isChanged = true
				maxDelta = 255
				sumDelta += 255
			} else {
				var wr, wg, wbV, wa uint32
				gr, gg, gbV, ga = got.At(gb.Min.X+x, gb.Min.Y+y).RGBA()
				wr, wg, wbV, wa = want.At(wb.Min.X+x, wb.Min.Y+y).RGBA()
				isExactDiff = gr != wr || gg != wg || gbV != wbV || ga != wa
				if isExactDiff {
					dr := absInt(int(gr>>8) - int(wr>>8))
					dg := absInt(int(gg>>8) - int(wg>>8))
					db := absInt(int(gbV>>8) - int(wbV>>8))
					da := absInt(int(ga>>8) - int(wa>>8))
					d := max(max(dr, dg), max(db, da))
					if d == 0 {
						// 16-bit channels differed below 8-bit quantization.
						d = 1
					}
					if uint8(d) > maxDelta {
						maxDelta = uint8(d)
					}
					sumDelta += uint64(d)
					if tol == 0 {
						isChanged = true
					} else {
						// Per-channel 8-bit tolerance: max(|dR|,|dG|,|dB|,|dA|) <= tol matches.
						d8 := max(max(dr, dg), max(db, da))
						isChanged = d8 > tol
					}
				}
			}

			if isExactDiff {
				exactChanged++
			}
			if isChanged {
				changed++
				if !hasBounds {
					hasBounds = true
					unionMinX, unionMaxX = x, x+1
					unionMinY, unionMaxY = y, y+1
				} else {
					unionMinX = min(unionMinX, x)
					unionMaxX = max(unionMaxX, x+1)
					unionMinY = min(unionMinY, y)
					unionMaxY = max(unionMaxY, y+1)
				}
				unionPixels++
				if !inRun {
					inRun = true
					runStart = x
				}
				if diffImg != nil {
					diffImg.SetRGBA(x, y, magenta)
				}
			} else {
				if inRun {
					id := len(uf)
					uf = append(uf, regionAccum{
						parent: id,
						minX:   runStart,
						minY:   y,
						maxX:   x,
						maxY:   y + 1,
						pixels: x - runStart,
					})
					curRuns = append(curRuns, scanRun{x0: runStart, x1: x, root: id})
					inRun = false
				}
				if diffImg != nil {
					// Dimmed grayscale context for unchanged pixels, matching gametest.DiffImage.
					lum := (299*(gr>>8) + 587*(gg>>8) + 114*(gbV>>8)) / 1000
					dim := uint8(lum / 3)
					alpha := uint8(ga >> 8)
					diffImg.SetRGBA(x, y, color.RGBA{R: dim, G: dim, B: dim, A: alpha})
				}
			}
		}
		if inRun {
			id := len(uf)
			uf = append(uf, regionAccum{
				parent: id,
				minX:   runStart,
				minY:   y,
				maxX:   w,
				maxY:   y + 1,
				pixels: w - runStart,
			})
			curRuns = append(curRuns, scanRun{x0: runStart, x1: w, root: id})
		}

		// Two-pointer 8-connected merge between prevRuns (row y-1) and curRuns (row y).
		pi, ci := 0, 0
		for pi < len(prevRuns) && ci < len(curRuns) {
			pr := prevRuns[pi]
			cr := curRuns[ci]
			if pr.x1 < cr.x0 {
				pi++
				continue
			}
			if cr.x1 < pr.x0 {
				ci++
				continue
			}
			curRuns[ci].root = unionRoots(pr.root, cr.root)
			if pr.x1 <= cr.x1 {
				pi++
			} else {
				ci++
			}
		}
		prevRuns, curRuns = curRuns, prevRuns
	}

	var regions []DiffRegion
	for i := range uf {
		if uf[i].parent == i {
			regions = append(regions, DiffRegion{
				X:      uf[i].minX,
				Y:      uf[i].minY,
				Width:  uf[i].maxX - uf[i].minX,
				Height: uf[i].maxY - uf[i].minY,
				Pixels: uf[i].pixels,
			})
		}
	}
	slices.SortFunc(regions, func(a, b DiffRegion) int {
		if c := cmp.Compare(b.Pixels, a.Pixels); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Y, b.Y); c != 0 {
			return c
		}
		return cmp.Compare(a.X, b.X)
	})
	totalRegions := len(regions)
	if len(regions) > maxDiffRegions {
		regions = regions[:maxDiffRegions]
	}

	var changedPercent float64
	var meanDiff float64
	if totalPixels > 0 {
		changedPercent = (float64(changed) / float64(totalPixels)) * 100
		meanDiff = float64(sumDelta) / float64(totalPixels)
	}

	gametestMatch := !dimMismatch && exactChanged == 0
	withinTolerance := !dimMismatch
	if withinTolerance {
		switch {
		case opts.maxMismatchedPixels == 0 && opts.maxMismatchedPercent == 0:
			withinTolerance = changed == 0
		case opts.maxMismatchedPixels > 0 && opts.maxMismatchedPercent == 0:
			withinTolerance = changed <= opts.maxMismatchedPixels
		case opts.maxMismatchedPixels == 0 && opts.maxMismatchedPercent > 0:
			withinTolerance = changedPercent <= opts.maxMismatchedPercent
		default:
			withinTolerance = changed <= opts.maxMismatchedPixels && changedPercent <= opts.maxMismatchedPercent
		}
	}

	res := &ScreenshotCompareResult{
		CompareTo:            opts.compareTo,
		Source:               opts.source,
		BaselineWidth:        ww,
		BaselineHeight:       wh,
		DimensionMismatch:    dimMismatch,
		TotalPixels:          totalPixels,
		ChangedPixels:        changed,
		ExactChangedPixels:   exactChanged,
		ChangedPercent:       roundFloat(changedPercent, 6),
		MaxChannelDiff:       maxDelta,
		MeanChannelDiff:      roundFloat(meanDiff, 6),
		Tolerance:            opts.channelTolerance,
		MaxMismatchedPixels:  opts.maxMismatchedPixels,
		MaxMismatchedPercent: opts.maxMismatchedPercent,
		GametestMatch:        gametestMatch,
		WithinTolerance:      withinTolerance,
		ChangedRegions:       regions,
		TotalRegions:         totalRegions,
	}
	if hasBounds {
		res.ChangedBounds = &DiffRegion{
			X:      unionMinX,
			Y:      unionMinY,
			Width:  unionMaxX - unionMinX,
			Height: unionMaxY - unionMinY,
			Pixels: unionPixels,
		}
	}

	if diffImg != nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, diffImg); err != nil {
			return nil, fmt.Errorf("mcp: encode diff png: %w", err)
		}
		diffBytes := buf.Bytes()
		res.DiffBytes = len(diffBytes)
		if opts.diffRelPath != "" {
			if err := writeConfinedFile(opts.diffDir, opts.diffRelPath, diffBytes); err != nil {
				return nil, err
			}
			res.DiffPath = opts.diffRelPath
		} else if opts.includeDiffBase64 {
			res.DiffPNGBase64 = base64.StdEncoding.EncodeToString(diffBytes)
		}
	}

	return res, nil
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func roundFloat(v float64, places int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	pow := math.Pow(10, float64(places))
	return math.Round(v*pow) / pow
}
