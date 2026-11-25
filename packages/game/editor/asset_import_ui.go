package editor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// assetImportInspectorView renders the Inspector panel for an asset file selected in the file browser:
// its kind-specific import settings (inheriting defaults from game.yaml's targets, with per-field Reset),
// its credit metadata, and Reimport / Build Assets actions.
func assetImportInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], relPath string, kind AssetImportKind) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	buildDisabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.AssetsBuilding()
	}

	type infoSnapshot struct {
		rev  uint64
		info AssetImportInfo
	}
	infoMemo := core.NewMemoFunc(cx, func() infoSnapshot {
		r := rev.Get()
		info, _ := ed.InspectAssetImport(relPath)
		return infoSnapshot{rev: r, info: info}
	}, func(a, b infoSnapshot) bool {
		return a.rev == b.rev
	})
	readInfo := func() AssetImportInfo {
		return infoMemo.Get().info
	}

	sectionHeader := func(title string) core.Node {
		return core.Text{
			Content: title,
			Style: core.Style{
				FontSize:   11,
				FontWeight: 700,
				Color:      colors.Muted(),
			},
		}
	}

	resetBtn := func(testID, label string, overridden func() bool, onReset func()) core.Node {
		return ui.IconButton{
			TestID: testID,
			Icon:   lucide.RotateCcw,
			Label:  label,
			Disabled: func() bool {
				return disabled() || !overridden()
			},
			OnPress: onReset,
		}
	}

	fieldRow := func(label string, tip func() string, control core.Node, reset core.Node) core.Node {
		right := control
		if reset != nil {
			right = withTrailing(control, reset)
		}
		var labelNode core.Node = core.Text{
			Content: label,
			Style: core.Style{
				Width:    core.Pt(72),
				FontSize: 12,
				Color:    colors.Text(),
			},
		}
		if tip != nil {
			// Evaluate static tooltip text untracked so InspectorView's outer core.Dynamic
			// does not subscribe to rev and remount this panel (wiping focus and drafts).
			if t := core.Untracked(cx, tip); t != "" {
				labelNode = withTooltip(labelNode, t)
			}
		}
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        6,
			},
			Children: []core.Node{
				labelNode,
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{right},
				},
			},
		}
	}

	nodes := []core.Node{
		core.View{
			Style: core.Style{Gap: 4},
			Children: []core.Node{
				core.Text{
					TestID:  "import-asset-path",
					Content: relPath,
					Style: core.Style{
						FontSize:   13,
						FontWeight: 700,
						Color:      colors.Text(),
					},
				},
				ui.HStack{
					Gap: 6,
					Children: []core.Node{
						core.View{
							TestID: "import-asset-kind",
							Children: []core.Node{
								ui.Badge{
									Text:    string(kind),
									Variant: ui.BadgeNeutral,
								},
							},
						},
						core.Text{
							TestID: "import-asset-target",
							ContentFunc: func() string {
								return readInfo().TargetSummary()
							},
							Style: core.Style{
								FontSize: 11,
								Color:    colors.Muted(),
								FlexGrow: 1,
							},
						},
					},
				},
			},
		},
		ui.HStack{
			Gap: 6,
			Children: []core.Node{
				ui.Button{
					TestID:   "import-reimport",
					Text:     "Reimport",
					Label:    "Rebuild this asset with go tool game assets and reload changed atlases and textures",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonPrimary,
					Disabled: buildDisabled,
					OnPress: func() {
						ed.report(ed.ReimportAsset(relPath))
					},
				},
				ui.Button{
					TestID:   "import-build-assets",
					Text:     "Build Assets",
					Label:    "Run go tool game assets for the whole project and reload changed atlases and textures",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: buildDisabled,
					OnPress: func() {
						ed.report(ed.BuildAssets())
					},
				},
			},
		},
		core.Show{
			When: func() bool { return readInfo().SidecarErr != nil },
			Then: func(cx *core.Scope) core.Node {
				return core.Text{
					TestID: "import-sidecar-error",
					ContentFunc: func() string {
						if err := readInfo().SidecarErr; err != nil {
							return err.Error()
						}
						return ""
					},
					Style: core.Style{FontSize: 11, Color: colors.Danger()},
				}
			},
		},
	}

	switch kind {
	case AssetImportImage:
		nodes = append(nodes, sectionHeader("IMAGE IMPORT"))
		nodes = append(nodes, imageImportFields(cx, ed, rev, relPath, readInfo, disabled, fieldRow, resetBtn)...)
	case AssetImportAudio:
		nodes = append(nodes, sectionHeader("AUDIO IMPORT"))
		nodes = append(nodes, audioImportFields(cx, ed, rev, relPath, readInfo, disabled, fieldRow, resetBtn)...)
	case AssetImportFont:
		nodes = append(nodes, sectionHeader("FONT IMPORT"))
		nodes = append(nodes, fontImportFields(cx, ed, rev, relPath, readInfo, disabled, fieldRow, resetBtn)...)
	case AssetImportModel:
		nodes = append(nodes, sectionHeader("MODEL IMPORT"))
		nodes = append(nodes, modelImportFields(cx, ed, rev, relPath, readInfo, disabled, fieldRow, resetBtn)...)
	}

	nodes = append(nodes, creditImportSection(cx, ed, rev, relPath, readInfo, disabled, colors))

	return core.View{
		TestID:   "inspector-asset-import",
		Style:    core.Style{Gap: 8},
		Children: nodes,
	}
}

func imageImportFields(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	fieldRow func(string, func() string, core.Node, core.Node) core.Node,
	resetBtn func(string, string, func() bool, func()) core.Node,
) []core.Node {
	filterOpts := make([]ui.Option[string], len(importsettings.Filters))
	for i, f := range importsettings.Filters {
		filterOpts[i] = ui.Option[string]{Value: f, Label: f}
	}
	formatOpts := make([]ui.Option[string], len(project.TextureFormats))
	for i, f := range project.TextureFormats {
		formatOpts[i] = ui.Option[string]{Value: f, Label: f}
	}

	updateSlice := func(mutate func(s *importsettings.Slice)) {
		s, ok := readInfo().EffectiveImageSlice()
		if !ok || s == nil {
			s = &importsettings.Slice{Width: 16, Height: 16}
		}
		mutate(s)
		ed.report(ed.SetImageImportSlice(relPath, s))
	}

	return []core.Node{
		fieldRow("Filter", func() string { return "Sampling filter (default: " + readInfo().DefaultImageFilter + ")" },
			ui.Select[string]{
				TestID:   "import-image-filter",
				Label:    "Image Filter",
				Options:  filterOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					v, _ := readInfo().EffectiveImageFilter()
					return v
				},
				OnChange: func(v string) {
					ed.report(ed.SetImageImportFilter(relPath, v))
				},
			},
			resetBtn("import-image-filter-reset", "Reset image filter to default", func() bool {
				_, over := readInfo().EffectiveImageFilter()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "filter"))
			}),
		),
		fieldRow("Format", func() string { return "KTX2 texture format (target default: " + readInfo().DefaultImageFormat + ")" },
			ui.Select[string]{
				TestID:   "import-image-format",
				Label:    "Image Format",
				Options:  formatOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					v, _ := readInfo().EffectiveImageFormat()
					return v
				},
				OnChange: func(v string) {
					ed.report(ed.SetImageImportFormat(relPath, v))
				},
			},
			resetBtn("import-image-format-reset", "Reset image format to target default", func() bool {
				_, over := readInfo().EffectiveImageFormat()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "format"))
			}),
		),
		fieldRow("Mipmaps", func() string {
			return fmt.Sprintf("Generate mipmaps (target default: %v)", readInfo().DefaultImageMipmaps)
		},
			ui.Checkbox{
				TestID:   "import-image-mipmaps",
				Label:    "Mipmaps",
				Disabled: disabled,
				Checked: func() bool {
					v, _ := readInfo().EffectiveImageMipmaps()
					return v
				},
				OnChange: func(on bool) {
					ed.report(ed.SetImageImportMipmaps(relPath, &on))
				},
			},
			resetBtn("import-image-mipmaps-reset", "Reset mipmaps to target default", func() bool {
				_, over := readInfo().EffectiveImageMipmaps()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "mipmaps"))
			}),
		),
		fieldRow("sRGB", func() string {
			return fmt.Sprintf("sRGB color space (target default: %v)", readInfo().DefaultImageSRGB)
		},
			ui.Checkbox{
				TestID:   "import-image-srgb",
				Label:    "sRGB",
				Disabled: disabled,
				Checked: func() bool {
					v, _ := readInfo().EffectiveImageSRGB()
					return v
				},
				OnChange: func(on bool) {
					ed.report(ed.SetImageImportSRGB(relPath, &on))
				},
			},
			resetBtn("import-image-srgb-reset", "Reset sRGB to target default", func() bool {
				_, over := readInfo().EffectiveImageSRGB()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "srgb"))
			}),
		),
		fieldRow("Slice", func() string { return "Cut sprite sheet into grid frames for an atlas" },
			ui.Checkbox{
				TestID:   "import-image-slice-enabled",
				Label:    "Slice Grid",
				Disabled: disabled,
				Checked: func() bool {
					_, over := readInfo().EffectiveImageSlice()
					return over
				},
				OnChange: func(on bool) {
					if !on {
						ed.report(ed.SetImageImportSlice(relPath, nil))
						return
					}
					ed.report(ed.SetImageImportSlice(relPath, &importsettings.Slice{Width: 16, Height: 16}))
				},
			},
			resetBtn("import-image-slice-reset", "Reset slice grid", func() bool {
				_, over := readInfo().EffectiveImageSlice()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "slice"))
			}),
		),
		core.Show{
			When: func() bool {
				_, over := readInfo().EffectiveImageSlice()
				return over
			},
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						fieldRow("Cell W", nil, ui.NumberInput{
							TestID:   "import-image-slice-width",
							Label:    "Slice Width",
							Min:      1,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Width)
								}
								return 16
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Width = roundInt(v, 1) })
							},
						}, nil),
						fieldRow("Cell H", nil, ui.NumberInput{
							TestID:   "import-image-slice-height",
							Label:    "Slice Height",
							Min:      1,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Height)
								}
								return 16
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Height = roundInt(v, 1) })
							},
						}, nil),
						fieldRow("Margin", nil, ui.NumberInput{
							TestID:   "import-image-slice-margin",
							Label:    "Slice Margin",
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Margin)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Margin = roundInt(v, 0) })
							},
						}, nil),
						fieldRow("Spacing", nil, ui.NumberInput{
							TestID:   "import-image-slice-spacing",
							Label:    "Slice Spacing",
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Spacing)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Spacing = roundInt(v, 0) })
							},
						}, nil),
						fieldRow("Count", nil, ui.NumberInput{
							TestID:   "import-image-slice-count",
							Label:    "Slice Count",
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if s, ok := readInfo().EffectiveImageSlice(); ok {
									return float64(s.Count)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateSlice(func(s *importsettings.Slice) { s.Count = roundInt(v, 0) })
							},
						}, nil),
					},
				}
			},
		},
	}
}

func audioImportFields(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	fieldRow func(string, func() string, core.Node, core.Node) core.Node,
	resetBtn func(string, string, func() bool, func()) core.Node,
) []core.Node {
	formatOpts := make([]ui.Option[string], len(project.AudioFormats))
	for i, f := range project.AudioFormats {
		formatOpts[i] = ui.Option[string]{Value: f, Label: f}
	}
	channelOpts := []ui.Option[int]{
		{Value: 0, Label: "Source"},
		{Value: 1, Label: "Mono (1)"},
		{Value: 2, Label: "Stereo (2)"},
	}
	normOpts := []ui.Option[string]{
		{Value: "none", Label: "None"},
		{Value: "peakDB", Label: "Peak (dBFS)"},
		{Value: "rmsDB", Label: "RMS (dBFS)"},
	}

	updateLoop := func(mutate func(l *importsettings.Loop)) {
		l, ok := readInfo().EffectiveAudioLoop()
		if !ok || l == nil {
			l = &importsettings.Loop{}
		}
		mutate(l)
		ed.report(ed.SetAudioImportLoop(relPath, l))
	}

	return []core.Node{
		fieldRow("Format", func() string { return "Output format (target default: " + readInfo().DefaultAudioFormat + ")" },
			ui.Select[string]{
				TestID:   "import-audio-format",
				Label:    "Audio Format",
				Options:  formatOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					v, _ := readInfo().EffectiveAudioFormat()
					return v
				},
				OnChange: func(v string) {
					ed.report(ed.SetAudioImportFormat(relPath, v))
				},
			},
			resetBtn("import-audio-format-reset", "Reset audio format to target default", func() bool {
				_, over := readInfo().EffectiveAudioFormat()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "format"))
			}),
		),
		fieldRow("Rate", func() string {
			return fmt.Sprintf("Sample rate in Hz, 0 keeps source (target default: %d)", readInfo().DefaultAudioRate)
		},
			ui.NumberInput{
				TestID:   "import-audio-rate",
				Label:    "Audio Rate",
				Min:      0,
				Step:     100,
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveAudioRate()
					return float64(v)
				},
				OnChange: func(v float64) {
					ed.report(ed.SetAudioImportRate(relPath, roundInt(v, 0)))
				},
			},
			resetBtn("import-audio-rate-reset", "Reset sample rate to target default", func() bool {
				_, over := readInfo().EffectiveAudioRate()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "rate"))
			}),
		),
		fieldRow("Channels", func() string {
			return fmt.Sprintf("Channel count (target default: %d)", readInfo().DefaultAudioChannels)
		},
			ui.Select[int]{
				TestID:   "import-audio-channels",
				Label:    "Audio Channels",
				Options:  channelOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() int {
					v, _ := readInfo().EffectiveAudioChannels()
					return v
				},
				OnChange: func(v int) {
					ed.report(ed.SetAudioImportChannels(relPath, v))
				},
			},
			resetBtn("import-audio-channels-reset", "Reset channels to target default", func() bool {
				_, over := readInfo().EffectiveAudioChannels()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "channels"))
			}),
		),
		fieldRow("Bitrate", func() string {
			return fmt.Sprintf("AAC/MP3 bitrate in kbps (target default: %d)", readInfo().DefaultAudioBitrate)
		},
			ui.NumberInput{
				TestID:   "import-audio-bitrate",
				Label:    "Audio Bitrate",
				Min:      0,
				Step:     16,
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveAudioBitrate()
					return float64(v)
				},
				OnChange: func(v float64) {
					ed.report(ed.SetAudioImportBitrate(relPath, roundInt(v, 0)))
				},
			},
			resetBtn("import-audio-bitrate-reset", "Reset bitrate to target default", func() bool {
				_, over := readInfo().EffectiveAudioBitrate()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "bitrate"))
			}),
		),
		fieldRow("Normalize", func() string { return "Peak or RMS loudness normalization in dBFS" },
			ui.Select[string]{
				TestID:   "import-audio-norm-mode",
				Label:    "Audio Normalization",
				Options:  normOpts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					mode, _, _ := readInfo().EffectiveAudioNormalization()
					return mode
				},
				OnChange: func(mode string) {
					switch mode {
					case "peakDB":
						v := -1.0
						ed.report(ed.SetAudioImportPeakDB(relPath, &v))
					case "rmsDB":
						v := -14.0
						ed.report(ed.SetAudioImportRMSDB(relPath, &v))
					default:
						ed.report(ed.ResetAssetImportField(relPath, "normalization"))
					}
				},
			},
			resetBtn("import-audio-norm-reset", "Reset normalization to target default", func() bool {
				_, _, over := readInfo().EffectiveAudioNormalization()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "normalization"))
			}),
		),
		core.Show{
			When: func() bool {
				mode, _, _ := readInfo().EffectiveAudioNormalization()
				return mode == "peakDB" || mode == "rmsDB"
			},
			Then: func(cx *core.Scope) core.Node {
				return fieldRow("Level dB", nil, ui.NumberInput{
					TestID:   "import-audio-norm-db",
					Label:    "Normalization Level (dBFS)",
					Step:     0.5,
					Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
					Disabled: disabled,
					Value: func() float64 {
						_, db, _ := readInfo().EffectiveAudioNormalization()
						return db
					},
					OnChange: func(v float64) {
						mode, _, _ := readInfo().EffectiveAudioNormalization()
						if mode == "rmsDB" {
							ed.report(ed.SetAudioImportRMSDB(relPath, &v))
						} else {
							ed.report(ed.SetAudioImportPeakDB(relPath, &v))
						}
					},
				}, nil)
			},
		},
		fieldRow("Loop", func() string { return "WAV loop points in source sample frames" },
			ui.Checkbox{
				TestID:   "import-audio-loop-enabled",
				Label:    "Loop Region",
				Disabled: disabled,
				Checked: func() bool {
					_, over := readInfo().EffectiveAudioLoop()
					return over
				},
				OnChange: func(on bool) {
					if !on {
						ed.report(ed.SetAudioImportLoop(relPath, nil))
						return
					}
					ed.report(ed.SetAudioImportLoop(relPath, &importsettings.Loop{Start: 0, End: 0}))
				},
			},
			resetBtn("import-audio-loop-reset", "Reset audio loop", func() bool {
				_, over := readInfo().EffectiveAudioLoop()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "loop"))
			}),
		),
		core.Show{
			When: func() bool {
				_, over := readInfo().EffectiveAudioLoop()
				return over
			},
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					Style: core.Style{Gap: 6},
					Children: []core.Node{
						fieldRow("Start", nil, ui.NumberInput{
							TestID:   "import-audio-loop-start",
							Label:    "Loop Start",
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if l, ok := readInfo().EffectiveAudioLoop(); ok {
									return float64(l.Start)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateLoop(func(l *importsettings.Loop) { l.Start = roundInt(v, 0) })
							},
						}, nil),
						fieldRow("End", nil, ui.NumberInput{
							TestID:   "import-audio-loop-end",
							Label:    "Loop End",
							Min:      0,
							Step:     1,
							Disabled: disabled,
							Value: func() float64 {
								if l, ok := readInfo().EffectiveAudioLoop(); ok {
									return float64(l.End)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateLoop(func(l *importsettings.Loop) { l.End = roundInt(v, 0) })
							},
						}, nil),
					},
				}
			},
		},
	}
}

func fontImportFields(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	fieldRow func(string, func() string, core.Node, core.Node) core.Node,
	resetBtn func(string, string, func() bool, func()) core.Node,
) []core.Node {
	type textDraft struct {
		rev   uint64
		dirty bool
		text  string
	}
	charsDraft := core.NewSignal(cx, textDraft{})
	charsVal := func() string {
		_ = rev.Get()
		d := charsDraft.Get()
		if d.dirty && d.rev == ed.importRev {
			return d.text
		}
		v, _ := readInfo().EffectiveFontChars()
		return v
	}
	commitChars := func(text string) {
		info := readInfo()
		if text == info.DefaultFontChars && (info.Sidecar == nil || info.Sidecar.Font == nil || info.Sidecar.Font.Chars == "") {
			charsDraft.Set(textDraft{})
			return
		}
		charsDraft.Set(textDraft{})
		ed.report(ed.SetFontImportChars(relPath, text))
	}

	return []core.Node{
		fieldRow("Size", func() string { return fmt.Sprintf("Baked pixel size (target default: %d)", readInfo().DefaultFontSize) },
			ui.NumberInput{
				TestID:   "import-font-size",
				Label:    "Font Size",
				Min:      1,
				Max:      float64(project.MaxFontSize),
				Step:     1,
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveFontSize()
					return float64(v)
				},
				OnChange: func(v float64) {
					ed.report(ed.SetFontImportSize(relPath, roundInt(v, 1)))
				},
			},
			resetBtn("import-font-size-reset", "Reset font size to target default", func() bool {
				_, over := readInfo().EffectiveFontSize()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "size"))
			}),
		),
		fieldRow("Padding", func() string {
			return fmt.Sprintf("Glyph padding in pixels (target default: %d)", readInfo().DefaultFontPadding)
		},
			ui.NumberInput{
				TestID:   "import-font-padding",
				Label:    "Font Padding",
				Min:      0,
				Step:     1,
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveFontPadding()
					return float64(v)
				},
				OnChange: func(v float64) {
					p := roundInt(v, 0)
					ed.report(ed.SetFontImportPadding(relPath, &p))
				},
			},
			resetBtn("import-font-padding-reset", "Reset font padding to target default", func() bool {
				_, over := readInfo().EffectiveFontPadding()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "padding"))
			}),
		),
		fieldRow("Chars", func() string { return "Characters to bake into bitmap font" },
			ui.Input{
				TestID:   "import-font-chars",
				Label:    "Font Characters",
				Disabled: disabled,
				Value:    charsVal,
				OnChange: func(v string) {
					charsDraft.Set(textDraft{rev: ed.importRev, dirty: true, text: v})
				},
				OnSubmit: commitChars,
				OnBlur: func() {
					d := charsDraft.Get()
					if d.dirty && d.rev == ed.importRev {
						commitChars(d.text)
					}
				},
			},
			resetBtn("import-font-chars-reset", "Reset font characters to target default", func() bool {
				_, over := readInfo().EffectiveFontChars()
				return over
			}, func() {
				charsDraft.Set(textDraft{})
				ed.report(ed.ResetAssetImportField(relPath, "chars"))
			}),
		),
	}
}

func modelImportFields(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	fieldRow func(string, func() string, core.Node, core.Node) core.Node,
	resetBtn func(string, string, func() bool, func()) core.Node,
) []core.Node {
	return []core.Node{
		fieldRow("Scale", func() string {
			return fmt.Sprintf("Model scale multiplier (target default: %g)", readInfo().DefaultModelScale)
		},
			ui.NumberInput{
				TestID:   "import-model-scale",
				Label:    "Model Scale",
				Min:      0,
				Step:     0.1,
				Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
				Disabled: disabled,
				Value: func() float64 {
					v, _ := readInfo().EffectiveModelScale()
					return v
				},
				OnChange: func(v float64) {
					ed.report(ed.SetModelImportScale(relPath, v))
				},
			},
			resetBtn("import-model-scale-reset", "Reset model scale to target default", func() bool {
				_, over := readInfo().EffectiveModelScale()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "scale"))
			}),
		),
		fieldRow("Animations", func() string {
			return fmt.Sprintf("Keep model animations (target default: %v)", readInfo().DefaultModelAnimations)
		},
			ui.Checkbox{
				TestID:   "import-model-animations",
				Label:    "Keep Animations",
				Disabled: disabled,
				Checked: func() bool {
					v, _ := readInfo().EffectiveModelAnimations()
					return v
				},
				OnChange: func(on bool) {
					ed.report(ed.SetModelImportAnimations(relPath, &on))
				},
			},
			resetBtn("import-model-animations-reset", "Reset model animations to target default", func() bool {
				_, over := readInfo().EffectiveModelAnimations()
				return over
			}, func() {
				ed.report(ed.ResetAssetImportField(relPath, "animations"))
			}),
		),
	}
}

func creditImportSection(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	readInfo func() AssetImportInfo,
	disabled func() bool,
	colors ui.Colors,
) core.Node {
	type creditDraft struct {
		rev     uint64
		dirty   bool
		author  string
		license string
		source  string
		notice  string
	}
	draft := core.NewSignal(cx, creditDraft{})
	creditErr := core.NewSignal(cx, "")

	currentCredit := func() importsettings.Credit {
		_ = rev.Get()
		d := draft.Get()
		if d.dirty && d.rev == ed.importRev {
			return importsettings.Credit{
				Author:  d.author,
				License: d.license,
				Source:  d.source,
				Notice:  d.notice,
			}
		}
		if c := readInfo().EffectiveCredit(); c != nil {
			return *c
		}
		return importsettings.Credit{}
	}

	setField := func(mutate func(*importsettings.Credit)) {
		c := currentCredit()
		mutate(&c)
		draft.Set(creditDraft{
			rev:     ed.importRev,
			dirty:   true,
			author:  c.Author,
			license: c.License,
			source:  c.Source,
			notice:  c.Notice,
		})
	}

	commit := func() {
		c := currentCredit()
		c.Author = strings.TrimSpace(c.Author)
		c.License = strings.TrimSpace(c.License)
		c.Source = strings.TrimSpace(c.Source)
		c.Notice = strings.TrimSpace(c.Notice)

		if c.Author == "" && c.License == "" && c.Source == "" && c.Notice == "" {
			creditErr.Set("")
			draft.Set(creditDraft{})
			if readInfo().EffectiveCredit() != nil {
				ed.report(ed.SetAssetImportCredit(relPath, nil))
			}
			return
		}
		if c.Author == "" || c.License == "" {
			creditErr.Set("Author and license are both required for credit")
			return
		}
		if err := ed.SetAssetImportCredit(relPath, &c); err != nil {
			creditErr.Set(err.Error())
			ed.report(err)
			return
		}
		creditErr.Set("")
		draft.Set(creditDraft{})
	}

	row := func(label string, ctrl core.Node) core.Node {
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        6,
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style: core.Style{
						Width:    core.Pt(72),
						FontSize: 12,
						Color:    colors.Text(),
					},
				},
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{ctrl},
				},
			},
		}
	}

	return core.View{
		Style: core.Style{Gap: 6},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.Text{
						Content: "CREDIT",
						Style: core.Style{
							FontSize:   11,
							FontWeight: 700,
							Color:      colors.Muted(),
							FlexGrow:   1,
						},
					},
					ui.IconButton{
						TestID: "import-credit-reset",
						Icon:   lucide.RotateCcw,
						Label:  "Clear asset credit",
						Disabled: func() bool {
							if disabled() {
								return true
							}
							d := draft.Get()
							hasDraft := d.dirty && d.rev == ed.importRev
							return !hasDraft && readInfo().EffectiveCredit() == nil
						},
						OnPress: func() {
							creditErr.Set("")
							draft.Set(creditDraft{})
							ed.report(ed.SetAssetImportCredit(relPath, nil))
						},
					},
				},
			},
			row("Author", ui.Input{
				TestID:      "import-credit-author",
				Label:       "Credit Author",
				Placeholder: "Author",
				Disabled:    disabled,
				Value:       func() string { return currentCredit().Author },
				OnChange:    func(v string) { setField(func(c *importsettings.Credit) { c.Author = v }) },
				OnSubmit:    func(string) { commit() },
				OnBlur:      commit,
			}),
			row("License", ui.Input{
				TestID:      "import-credit-license",
				Label:       "Credit License",
				Placeholder: "CC-BY-4.0",
				Disabled:    disabled,
				Value:       func() string { return currentCredit().License },
				OnChange:    func(v string) { setField(func(c *importsettings.Credit) { c.License = v }) },
				OnSubmit:    func(string) { commit() },
				OnBlur:      commit,
			}),
			row("Source", ui.Input{
				TestID:      "import-credit-source",
				Label:       "Credit Source",
				Placeholder: "Optional URL",
				Disabled:    disabled,
				Value:       func() string { return currentCredit().Source },
				OnChange:    func(v string) { setField(func(c *importsettings.Credit) { c.Source = v }) },
				OnSubmit:    func(string) { commit() },
				OnBlur:      commit,
			}),
			row("Notice", ui.Input{
				TestID:      "import-credit-notice",
				Label:       "Credit Notice",
				Placeholder: "Optional notice",
				Disabled:    disabled,
				Value:       func() string { return currentCredit().Notice },
				OnChange:    func(v string) { setField(func(c *importsettings.Credit) { c.Notice = v }) },
				OnSubmit:    func(string) { commit() },
				OnBlur:      commit,
			}),
			core.Show{
				When: func() bool { return creditErr.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID:      "import-credit-error",
						ContentFunc: creditErr.Get,
						Style:       core.Style{FontSize: 11, Color: colors.Danger()},
					}
				},
			},
		},
	}
}

// roundInt converts a finite float64 from a NumberInput into an int clamped to [minVal, math.MaxInt32], returning minVal for non-finite inputs.
func roundInt(v float64, minVal int) int {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return minVal
	}
	r := math.Round(v)
	if r <= float64(minVal) {
		return minVal
	}
	if r >= math.MaxInt32 {
		return math.MaxInt32
	}
	return int(r)
}
