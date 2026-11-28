package gameui

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// MinTouchTargetSize is the minimum touch target diameter in points required on phones and tablets.
	MinTouchTargetSize float32 = 44
	// MinTextContrastRatio is the minimum WCAG 2.1 contrast ratio required for screen text against its fill.
	MinTextContrastRatio float64 = 4.5
)

// ScreenIssueCode identifies the category of a screen check diagnostic.
type ScreenIssueCode string

const (
	// IssueTouchAreaTooSmall flags a touch control whose hit area is below MinTouchTargetSize (44 pt) on phones or tablets.
	IssueTouchAreaTooSmall ScreenIssueCode = "touch_area_too_small"
	// IssueTextOverflow flags a text node whose localized string exceeds its laid-out box width at one or more reference sizes.
	IssueTextOverflow ScreenIssueCode = "text_overflow"
	// IssueMissingTextKey flags a textKey absent from a project locale table.
	IssueMissingTextKey ScreenIssueCode = "missing_text_key"
	// IssueLowContrast flags text whose contrast ratio against its fill is under 4.5:1.
	IssueLowContrast ScreenIssueCode = "low_contrast"
)

// ScreenIssue is one problem CheckScreen finds in a ScreenDoc.
type ScreenIssue struct {
	// Code classifies the issue.
	Code ScreenIssueCode
	// NodeID is the ScreenNodeDoc.ID at fault.
	NodeID string
	// NodePath holds the YAML path steps from the document root to the offending node or field (for example ["nodes", "3", "textKey"]).
	NodePath []string
	// Locale is the BCP-47 locale tag involved, when the issue is locale-specific.
	Locale string
	// Devices names the reference device profiles involved, when the issue is device-specific.
	Devices []string
	// Message describes the issue for diagnostics and UI panels.
	Message string
}

// ScreenCheckOptions configures CheckScreen.
type ScreenCheckOptions struct {
	// TextScale is the largest text scale the game supports. When it is above the theme's own scale, text overflow is checked at both scales, and an overflow that only appears at the larger one names that scale in its message. It is clamped to [MinTextScale, MaxTextScale]; 0 checks the theme's scale only.
	TextScale float32
	// Locales maps normalized locale tags to their message string maps (for example from l10n.StringTable.Strings).
	Locales map[string]map[string]string
	// Plurals optionally maps normalized locale tags to their CLDR plural entries (for example from l10n.StringTable.Plurals).
	Plurals map[string]map[string]l10n.PluralForms
	// Devices optionally overrides the reference device profiles; empty uses ReferenceDevicesFor(doc.DesignWidth, doc.DesignHeight).
	Devices []DeviceProfile
	// Theme optionally overrides the theme used for default text/button colors and text measurement.
	Theme *Theme
	// FallbackBackdrop is the backdrop color used to composite translucent fills when no opaque ancestor fill exists (defaults to #101216).
	FallbackBackdrop core.Color
}

type flatScreenNode struct {
	doc          ScreenNodeDoc
	path         []string
	effectiveDC  deviceClassFilter
	inheritedBG  core.Color
	hasAncBG     bool
	isRootCanvas bool
}

// CheckScreen checks doc against the HUD/menu screen quality rules:
//   - touch areas under 44 pt on phones and tablets;
//   - localized text overflowing its box at any reference device size (Phone 19.5:9 with notch, Tablet 4:3, Desktop 16:9, Ultrawide 21:9);
//   - textKey entries missing from any project locale;
//   - text under 4.5:1 WCAG contrast against its own fill.
func CheckScreen(doc *ScreenDoc, opts ScreenCheckOptions) []ScreenIssue {
	if doc == nil {
		return nil
	}
	devices := opts.Devices
	if len(devices) == 0 {
		devices = ReferenceDevicesFor(doc.DesignWidth, doc.DesignHeight)
	}
	backdrop := opts.FallbackBackdrop
	if backdrop == 0 {
		backdrop = core.RGB(0x101216)
	}
	var th Theme
	if opts.Theme != nil {
		th = resolveTheme(opts.Theme)
	} else {
		th = DefaultTheme()
		th.Font.Size = core.Points(DefaultScreenFontSize)
	}
	th.TextScale = clampTextScale(th.TextScale)
	textScales := []float32{th.TextScale}
	if opts.TextScale > 0 {
		if maxScale := clampTextScale(opts.TextScale); maxScale > th.TextScale {
			textScales = append(textScales, maxScale)
		}
	}
	if th.TextMeasurer == nil {
		th.TextMeasurer = screenTextMeasurer
	}

	// Collect sorted unique locales across Locales and Plurals.
	localeSet := make(map[string]bool, len(opts.Locales)+len(opts.Plurals))
	for loc := range opts.Locales {
		localeSet[loc] = true
	}
	for loc := range opts.Plurals {
		localeSet[loc] = true
	}
	locales := slices.Sorted(maps.Keys(localeSet))

	// Flatten node tree with YAML paths, inherited device classes, and inherited opaque fill.
	var nodes []flatScreenNode
	var walk func(list []ScreenNodeDoc, prefix []string, parentDC deviceClassFilter, ancBG core.Color, hasAncBG bool, isRoot bool)
	walk = func(list []ScreenNodeDoc, prefix []string, parentDC deviceClassFilter, ancBG core.Color, hasAncBG bool, isRoot bool) {
		for i, n := range list {
			p := make([]string, 0, len(prefix)+1)
			p = append(p, prefix...)
			p = append(p, strconv.Itoa(i))

			effDC := intersectDeviceClasses(parentDC, n.DeviceClasses)
			nodes = append(nodes, flatScreenNode{
				doc:          n,
				path:         p,
				effectiveDC:  effDC,
				inheritedBG:  ancBG,
				hasAncBG:     hasAncBG,
				isRootCanvas: isRoot,
			})

			nextBG, nextHasBG := ancBG, hasAncBG
			if n.Background != "" {
				if parsed, err := core.ParseColor(n.Background); err == nil && parsed != core.Transparent {
					base := backdrop
					if hasAncBG {
						base = ancBG
					}
					if parsed&0xff < 0xff {
						parsed = parsed.Over(base)
					}
					nextBG = parsed
					nextHasBG = true
				}
			}
			if len(n.Children) > 0 {
				childPrefix := make([]string, 0, len(p)+1)
				childPrefix = append(childPrefix, p...)
				childPrefix = append(childPrefix, "children")
				walk(n.Children, childPrefix, effDC, nextBG, nextHasBG, n.Type == NodeContainer)
			}
		}
	}
	walk(doc.Nodes, []string{"nodes"}, deviceClassFilter{}, backdrop, false, true)

	var issues []ScreenIssue

	// 1. Touch area check (< 44 pt on phones and tablets).
	for _, fn := range nodes {
		n := fn.doc
		var touchDevices []string
		for _, dev := range devices {
			if dev.Touch && fn.effectiveDC.matches(dev.Class) {
				touchDevices = append(touchDevices, dev.Name)
			}
		}
		if len(touchDevices) == 0 {
			continue
		}
		isTouchWidget := n.Type == NodeTouchButton || n.Type == NodeTouchStick
		if !isTouchWidget && !(n.Type == NodeButton && n.TouchHitSize > 0) {
			continue
		}
		effectiveHit := n.TouchHitSize
		field := "touchHitSize"
		if effectiveHit <= 0 {
			switch {
			case n.Width > 0 && n.Height > 0:
				effectiveHit = min(n.Width, n.Height)
				field = "width"
			case n.Width > 0:
				effectiveHit = n.Width
				field = "width"
			case n.Height > 0:
				effectiveHit = n.Height
				field = "height"
			default:
				field = "id"
			}
		}
		if effectiveHit < MinTouchTargetSize {
			issues = append(issues, ScreenIssue{
				Code:     IssueTouchAreaTooSmall,
				NodeID:   n.ID,
				NodePath: appendPath(fn.path, field),
				Devices:  touchDevices,
				Message:  fmt.Sprintf("node %q: touch area is %s pt (minimum %s pt on %s)", n.ID, formatPoints(effectiveHit), formatPoints(MinTouchTargetSize), strings.Join(touchDevices, ", ")),
			})
		}
	}

	// 2 & 3. Missing text key and localized text overflow across reference sizes.
	if len(locales) > 0 {
		// Pre-resolve text lookup per locale and lay out the screen per (locale, device).
		for _, loc := range locales {
			textMap := opts.Locales[loc]
			pluralMap := opts.Plurals[loc]
			lookup := func(key string) (string, bool) {
				if v, ok := textMap[key]; ok {
					return v, true
				}
				if pf, ok := pluralMap[key]; ok {
					return widestPluralForm(pf, DefaultScreenFontSize), true
				}
				return "", false
			}

			// Report missing keys for this locale first.
			present := make(map[string]string)
			for _, fn := range nodes {
				if fn.doc.TextKey == "" {
					continue
				}
				txt, ok := lookup(fn.doc.TextKey)
				if !ok {
					issues = append(issues, ScreenIssue{
						Code:     IssueMissingTextKey,
						NodeID:   fn.doc.ID,
						NodePath: appendPath(fn.path, "textKey"),
						Locale:   loc,
						Message:  fmt.Sprintf("node %q (locale %q): textKey %q is missing from locale %q", fn.doc.ID, loc, fn.doc.TextKey, loc),
					})
					continue
				}
				present[fn.doc.ID] = txt
			}

			// Lay out the screen at every reference device size for this locale to check box and safe-area overflows.
			type overflowRec struct {
				scale   float32
				devices []string
				maxOver float32
				boxW    float32
				neededW float32
			}
			overflows := make(map[string]*overflowRec)

			for _, scale := range textScales {
				scaled := th
				scaled.TextScale = scale
				for _, dev := range devices {
					built, err := BuildScreen(doc, ScreenBuildOptions{
						SafeArea:       dev.SafeArea,
						DeviceClass:    dev.Class,
						ViewportWidth:  dev.Width,
						ViewportHeight: dev.Height,
						Text: func(key string) string {
							if txt, ok := lookup(key); ok {
								return txt
							}
							return key
						},
						Theme: &scaled,
					})
					if err != nil {
						continue
					}
					built.Layout(core.Bounds{
						Size: core.Size{Width: core.Points(dev.Width), Height: core.Points(dev.Height)},
					})
					safeW := max(float32(0), dev.Width-float32(dev.SafeArea.Left+dev.SafeArea.Right))

					for _, fn := range nodes {
						txt, ok := present[fn.doc.ID]
						if !ok || !fn.effectiveDC.matches(dev.Class) {
							continue
						}
						// A node that already overflows at a smaller scale is reported once, at that scale.
						if rec := overflows[fn.doc.ID]; rec != nil && rec.scale < scale {
							continue
						}
						w := built.Widget(fn.doc.ID)
						if w == nil || w.spec().Hidden {
							continue
						}
						fontSize := fn.doc.FontSize
						if fontSize <= 0 {
							fontSize = float32(th.Font.Size)
						}
						tw, _ := MeasureScreenText(txt, fontSize*scale)
						var padX float32
						if fn.doc.Padding != nil {
							padX = fn.doc.Padding.Left + fn.doc.Padding.Right
						} else if fn.doc.Type == NodeButton && fn.doc.Width == 0 {
							padX = 2 * max(th.Padding, 4)
						}
						neededW := tw + padX
						boxW := float32(w.Bounds().Size.Width)

						var availW float32
						if fn.doc.SizeMode == SizeFitText {
							availW = safeW
							if !fn.doc.KeepInSafeArea() {
								availW = dev.Width
							}
						} else if boxW > 0 {
							availW = boxW
						} else if fn.doc.Width > 0 {
							availW = fn.doc.Width
						}
						if availW <= 0 {
							continue
						}
						if over := neededW - availW; over > 0.5 {
							rec := overflows[fn.doc.ID]
							if rec == nil {
								rec = &overflowRec{scale: scale, boxW: availW, neededW: neededW, maxOver: over}
								overflows[fn.doc.ID] = rec
							}
							rec.devices = append(rec.devices, dev.Name)
							if over > rec.maxOver {
								rec.maxOver = over
								rec.boxW = availW
								rec.neededW = neededW
							}
						}
					}
				}
			}

			for _, fn := range nodes {
				rec := overflows[fn.doc.ID]
				if rec == nil {
					continue
				}
				field := "textKey"
				if fn.doc.SizeMode != SizeFitText && fn.doc.Width > 0 {
					field = "width"
				}
				var atScale string
				if rec.scale != 1 {
					atScale = fmt.Sprintf(" at text scale %d%%", int(math.Round(float64(rec.scale)*100)))
				}
				issues = append(issues, ScreenIssue{
					Code:     IssueTextOverflow,
					NodeID:   fn.doc.ID,
					NodePath: appendPath(fn.path, field),
					Locale:   loc,
					Devices:  rec.devices,
					Message: fmt.Sprintf(
						"node %q (locale %q): text for key %q overflows %s pt box by %s pt%s (needs %s pt; %s)",
						fn.doc.ID, loc, fn.doc.TextKey,
						formatPoints(rec.boxW), formatPoints(rec.maxOver), atScale, formatPoints(rec.neededW),
						strings.Join(rec.devices, ", "),
					),
				})
			}
		}
	}

	// 4. Text contrast check (< 4.5:1 against its own fill).
	for _, fn := range nodes {
		n := fn.doc
		if n.TextKey == "" {
			continue
		}
		var bg core.Color
		hasBG := false
		if n.Background != "" {
			if parsed, err := core.ParseColor(n.Background); err == nil && parsed != core.Transparent {
				base := backdrop
				if fn.hasAncBG {
					base = fn.inheritedBG
				}
				if parsed&0xff < 0xff {
					parsed = parsed.Over(base)
				}
				bg = parsed
				hasBG = true
			}
		} else if n.Type == NodeButton {
			bg = th.Background
			hasBG = true
		} else if fn.hasAncBG {
			bg = fn.inheritedBG
			hasBG = true
		}
		if !hasBG {
			continue
		}

		fg := th.Text
		if n.TextColor != "" {
			if parsed, err := core.ParseColor(n.TextColor); err == nil {
				fg = parsed
			}
		}
		ratio := a11y.ContrastRatio(fg, bg)
		if ratio < MinTextContrastRatio {
			field := "id"
			switch {
			case n.TextColor != "":
				field = "textColor"
			case n.Background != "":
				field = "background"
			}
			issues = append(issues, ScreenIssue{
				Code:     IssueLowContrast,
				NodeID:   n.ID,
				NodePath: appendPath(fn.path, field),
				Message:  fmt.Sprintf("node %q: text contrast %.2f:1 against fill is under the %.1f:1 minimum", n.ID, ratio, MinTextContrastRatio),
			})
		}
	}

	return issues
}

func widestPluralForm(pf l10n.PluralForms, fontSize float32) string {
	best := ""
	var bestW float32 = -1
	for _, s := range []string{pf.Other, pf.Many, pf.Few, pf.Two, pf.One, pf.Zero} {
		if s == "" {
			continue
		}
		w, _ := MeasureScreenText(s, fontSize)
		if w > bestW {
			bestW = w
			best = s
		}
	}
	return best
}

func appendPath(base []string, extra string) []string {
	out := make([]string, 0, len(base)+1)
	out = append(out, base...)
	out = append(out, extra)
	return out
}

func formatPoints(v float32) string {
	rounded := math.Round(float64(v)*10) / 10
	if rounded == math.Trunc(rounded) {
		return strconv.Itoa(int(rounded))
	}
	return strconv.FormatFloat(rounded, 'f', 1, 64)
}
