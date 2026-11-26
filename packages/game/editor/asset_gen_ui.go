package editor

import (
	"fmt"
	"image"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/credits"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived alpha tints strictly from docs/design/editor/Spec.dc.html color tokens (never introducing off-spec hex values).
var (
	specAssetGenSelectedBg  = core.RGBA(0x3B7BFF, 0.14)
	specAssetGenPillBg      = core.RGBA(0x9B7BFF, 0.14)
	specAssetGenPillBorder  = core.RGBA(0x9B7BFF, 0.42)
	specAssetGenBtnSec      = core.RGBA(0xD9DDE5, 0.08)
	specAssetGenCheckerTile = core.RGBA(0xD9DDE5, 0.05)
	specAssetGenFloorTile   = core.RGBA(0xD9DDE5, 0.09)
	specAssetGenTorchGlow   = core.RGBA(0xE9A84A, 0.18)
	specAssetGenWavePass    = core.RGBA(0x3B7BFF, 0.72)
	specAssetGenWaveFail    = core.RGBA(0xFF6B6B, 0.85)
	specAssetGenApprovedRow = core.RGBA(0x4FD38A, 0.10)
	specAssetGenProblemText = core.RGB(0xFF6B6B)
)

// AssetGeneratorView renders the full-window Generated Assets screen matching docs/design/editor/Assets.dc.html
// Screen 2 ("generate, pick, check style and license") using strictly Spec.dc.html color, spacing, radius, and type tokens.
func AssetGeneratorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.AssetGeneratorOpen() {
			ed.CloseAssetGenerator()
		}
	})

	genRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.assetGenRev()
	})
	activeImgReqID := core.NewMemo(cx, func() string {
		_ = genRev.Get()
		snap := ed.snapshotAssetGenView()
		if snap.activeImageReq != nil {
			return snap.activeImageReq.ID
		}
		return ""
	})
	hasImgReq := core.NewMemo(cx, func() bool {
		return activeImgReqID.Get() != ""
	})
	licEditPath := core.NewMemo(cx, func() string {
		_ = genRev.Get()
		p, _, _ := ed.assetGenLicenseEditState()
		return p
	})
	hasLicEdit := core.NewMemo(cx, func() bool {
		return licEditPath.Get() != ""
	})

	refineDraftSig := core.NewSignal(cx, ed.assetGenRefineDraft())
	_, initLicDraft, initLicComm := ed.assetGenLicenseEditState()
	licDraftSig := core.NewSignal(cx, initLicDraft)
	licCommSig := core.NewSignal(cx, initLicComm)

	onBeginEditLicense := func(assetPath string) {
		ed.BeginEditGeneratedLicense(assetPath)
		_, draft, comm := ed.assetGenLicenseEditState()
		licDraftSig.Set(draft)
		licCommSig.Set(comm)
	}

	return core.View{
		TestID: "asset-gen-screen",
		Role:   core.RoleRegion,
		Label:  ed.T("assetGen.title"),
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = genRev.Get()
				return assetGenHeaderView(ed, ed.snapshotAssetGenView())
			}),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					MinHeight: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = genRev.Get()
						return assetGenLeftRailView(ed, ed.snapshotAssetGenView())
					}),
					// Center column: ScrollView stays mounted across genRev updates; Refine input bar sits outside Dynamic so typing never drops focus.
					core.View{
						Style: core.Style{
							FlexGrow:   1,
							FlexBasis:  core.Pt(0),
							MinWidth:   core.Pt(0),
							MinHeight:  core.Pt(0),
							Background: specBg,
							Overflow:   core.Hidden,
						},
						Children: []core.Node{
							ui.ScrollView{
								TestID: "asset-gen-center",
								Style: core.Style{
									FlexGrow:   1,
									FlexBasis:  core.Pt(0),
									MinWidth:   core.Pt(0),
									MinHeight:  core.Pt(0),
									Background: specBg,
								},
								ContentStyle: core.Style{
									Padding: core.All(20),
									Gap:     16,
								},
								Children: []core.Node{
									core.Dynamic(func(cx *core.Scope) core.Node {
										_ = genRev.Get()
										return assetGenCenterContentView(ed, ed.snapshotAssetGenView())
									}),
								},
							},
							core.Show{
								When: hasImgReq.Get,
								Then: func(cx *core.Scope) core.Node {
									return assetGenRefineBarView(ed, genRev, activeImgReqID, refineDraftSig)
								},
							},
						},
					},
					// Right inspector column: ScrollView stays mounted; inline license editor bar sits outside Dynamic so typing never drops focus.
					core.View{
						TestID: "asset-gen-inspector",
						Style: core.Style{
							Width:       core.Pt(380),
							FlexShrink:  0,
							Background:  specPanel,
							Borders:     core.Edges{Left: 1},
							BorderColor: specLine,
							MinHeight:   core.Pt(0),
							Overflow:    core.Hidden,
						},
						Children: []core.Node{
							core.Dynamic(func(cx *core.Scope) core.Node {
								_ = genRev.Get()
								return assetGenRightInspectorHeader(ed.snapshotAssetGenView())
							}),
							ui.ScrollView{
								Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinHeight: core.Pt(0)},
								ContentStyle: core.Style{
									Padding: core.All(16),
									Gap:     16,
								},
								Children: []core.Node{
									core.Dynamic(func(cx *core.Scope) core.Node {
										_ = genRev.Get()
										return assetGenRightInspectorBody(ed, ed.snapshotAssetGenView(), onBeginEditLicense)
									}),
								},
							},
							core.Show{
								When: hasLicEdit.Get,
								Then: func(cx *core.Scope) core.Node {
									return assetGenLicenseEditorBar(ed, licEditPath, licDraftSig, licCommSig)
								},
							},
						},
					},
				},
			},
		},
	}
}

type assetGenViewSnapshot struct {
	projectName     string
	servers         []AssetGeneratorServer
	requests        []AssetGenerationRequest
	activeImageReq  *AssetGenerationRequest
	activeSoundReq  *AssetGenerationRequest
	selectedCand    *GeneratedCandidate
	selectedReq     *AssetGenerationRequest
	brief           AssetStyleBrief
	bg              AssetPreviewBackground
	licenses        []credits.GeneratedAssetLicense
	licenseScanErr  string
	playingReqID    string
	playingCandID   string
	refineDraft     string
	licenseEditPath string
	licenseDraft    string
	approvalAuthor  string
	busy            bool
	busyLabel       string
	playingGame     bool
	mergeConflict   bool
}

func (e *Editor) snapshotAssetGenView() assetGenViewSnapshot {
	projName := "project"
	if e.Project != nil && strings.TrimSpace(e.Project.Name) != "" {
		projName = e.Project.Name
	} else if e.ProjectPath != "" {
		projName = filepathBase(filepathDir(e.ProjectPath))
	}
	servers := e.AssetGenerators()
	author := e.AssetApprovalAuthor()

	e.assetGen.mu.Lock()
	e.ensureAssetGenLocked()
	reqs := make([]AssetGenerationRequest, len(e.assetGen.requests))
	for i, r := range e.assetGen.requests {
		reqs[i] = cloneAssetGenRequest(r)
	}
	activeImgID := e.assetGen.activeRequestID
	activeSndID := e.assetGen.activeSoundReqID
	brief := e.assetGen.brief
	brief.Backgrounds = slicesCopy(brief.Backgrounds)
	bg := currentPreviewBGLocked(&e.assetGen)
	licenses := slicesCopy(e.assetGen.licenses)
	licScanErr := e.assetGen.licenseScanErr
	playReq := e.assetGen.playingReqID
	playCand := e.assetGen.playingCandID
	refineDraft := e.assetGen.refineDraft
	licEditPath := e.assetGen.licenseEditPath
	licDraft := e.assetGen.licenseDraft
	busy := e.assetGen.busy
	busyLabel := e.assetGen.busyLabel
	e.assetGen.mu.Unlock()

	var imgReq, sndReq *AssetGenerationRequest
	for i := range reqs {
		r := &reqs[i]
		if r.ID == activeImgID && r.Kind == AssetGenImages {
			imgReq = r
		}
		if r.ID == activeSndID && r.Kind == AssetGenSound {
			sndReq = r
		}
	}
	for i := range reqs {
		r := &reqs[i]
		if imgReq == nil && r.Kind == AssetGenImages {
			imgReq = r
		}
		if sndReq == nil && r.Kind == AssetGenSound {
			sndReq = r
		}
	}

	selectedReq := imgReq
	if selectedReq == nil {
		selectedReq = sndReq
	}
	var selectedCand *GeneratedCandidate
	if selectedReq != nil && len(selectedReq.Candidates) > 0 {
		for i := range selectedReq.Candidates {
			if selectedReq.Candidates[i].ID == selectedReq.SelectedCandidateID {
				selectedCand = &selectedReq.Candidates[i]
				break
			}
		}
		if selectedCand == nil {
			selectedCand = &selectedReq.Candidates[0]
		}
	}

	return assetGenViewSnapshot{
		projectName:     projName,
		servers:         servers,
		requests:        reqs,
		activeImageReq:  imgReq,
		activeSoundReq:  sndReq,
		selectedCand:    selectedCand,
		selectedReq:     selectedReq,
		brief:           brief,
		bg:              bg,
		licenses:        licenses,
		licenseScanErr:  licScanErr,
		playingReqID:    playReq,
		playingCandID:   playCand,
		refineDraft:     refineDraft,
		licenseEditPath: licEditPath,
		licenseDraft:    licDraft,
		approvalAuthor:  author,
		busy:            busy,
		busyLabel:       busyLabel,
		playingGame:     e.Playing(),
		mergeConflict:   e.MergeConflictOpen(),
	}
}

func slicesCopy[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	out := make([]T, len(s))
	copy(out, s)
	return out
}

func filepathDir(p string) string {
	idx := strings.LastIndexAny(p, `/\`)
	if idx <= 0 {
		return p
	}
	return p[:idx]
}

func filepathBase(p string) string {
	idx := strings.LastIndexAny(p, `/\`)
	if idx < 0 || idx+1 >= len(p) {
		return p
	}
	return p[idx+1:]
}

func assetGenHeaderView(ed *Editor, snap assetGenViewSnapshot) core.Node {
	pillDot := specAgent
	pillBg := specAssetGenPillBg
	pillBorder := specAssetGenPillBorder
	pillTextColor := specText
	pillText := "Connect an MCP generator server in Settings › Tools to generate candidates"

	if snap.busy && snap.busyLabel != "" {
		pillText = snap.busyLabel
	} else if r := snap.selectedReq; r != nil {
		if r.PickedCandidateID != "" {
			pillDot = specAdded
			pillBg = specRaised
			pillBorder = specLineRaised
			pillTextColor = specTextSecondary
			pillText = fmt.Sprintf("Picked %s · %s and its source file written · ⌘Z undoes", r.PickedCandidateID, path.Base(r.TargetPath))
		} else {
			pillText = fmt.Sprintf("%s returned %d candidates · waiting for your pick", r.Generator, len(r.Candidates))
		}
	}

	return core.View{
		TestID: "asset-gen-header",
		Role:   core.RoleRegion,
		Label:  "Generated assets header",
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Height:      core.Pt(44),
			FlexShrink:  0,
			Padding:     core.Symmetric(0, 12),
			Gap:         12,
			Background:  specBg,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			ui.Button{
				TestID:  "asset-gen-back",
				Icon:    lucide.ArrowLeft,
				Label:   ed.T("assetGen.back"),
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Color:   func() core.Color { return specRaised },
				Tint:    func() core.Color { return specTextSecondary },
				Style: core.Style{
					Width:        core.Pt(28),
					Height:       core.Pt(28),
					CornerRadius: 6,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					ed.CloseAssetGenerator()
				},
			},
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(20),
							Height:         core.Pt(20),
							CornerRadius:   6,
							Background:     specAccent,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Icon{SVG: lucide.Sparkles(), Style: core.Style{FontSize: 12, Color: specTextStrong}},
						},
					},
					core.Text{Content: snap.projectName, Style: core.Style{FontSize: 12.5, FontWeight: 600, Color: specTextStrong}},
					core.Text{Content: "/", Style: core.Style{FontSize: 12.5, Color: specTextMuted}},
					core.Text{Content: "Assets", Style: core.Style{FontSize: 12.5, Color: specTextSecondary}},
					core.Text{Content: "/", Style: core.Style{FontSize: 12.5, Color: specTextMuted}},
					core.Text{Content: "Generated", Style: core.Style{FontSize: 12.5, Color: specTextSecondary}},
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			core.View{
				TestID: "asset-gen-pill",
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(28),
					Padding:      core.Symmetric(0, 12),
					Gap:          8,
					CornerRadius: 14,
					Background:   pillBg,
					BorderWidth:  1,
					BorderColor:  pillBorder,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(7),
							Height:       core.Pt(7),
							CornerRadius: 4,
							Background:   pillDot,
						},
					},
					core.Text{
						TestID:  "asset-gen-pill-text",
						Content: pillText,
						Style:   core.Style{FontSize: 12, Color: pillTextColor},
					},
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			ui.Button{
				TestID:  "asset-gen-play",
				Text:    ed.T("assetGen.play"),
				Icon:    lucide.Play,
				Variant: ui.ButtonGhost,
				Size:    ui.SizeSmall,
				Color:   func() core.Color { return specTextStrong },
				Tint:    func() core.Color { return specBg },
				TextStyle: core.Style{
					FontSize:   12.5,
					FontWeight: 600,
					Color:      specBg,
				},
				Style: core.Style{
					Height:       core.Pt(30),
					CornerRadius: 8,
					Padding:      core.Symmetric(0, 14),
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					ed.CloseAssetGenerator()
					if !ed.Playing() {
						ed.report(ed.Play())
					}
				},
			},
		},
	}
}

func assetGenLeftRailView(ed *Editor, snap assetGenViewSnapshot) core.Node {
	// Combine active generation requests with on-disk generated assets (from *.source.yaml) that don't have an active request row.
	type railRow struct {
		id         string
		targetPath string
		name       string
		statusText string
		statusCol  core.Color
		selected   bool
		isImage    bool
		pixels     *image.RGBA
		waveform   []float32
		clickable  bool
	}
	seenPaths := make(map[string]bool, len(snap.requests))
	var rows []railRow
	for i := range snap.requests {
		r := &snap.requests[i]
		seenPaths[r.TargetPath] = true
		stText, stCol := assetGenStatusDisplay(r)
		var pix *image.RGBA
		var wave []float32
		if len(r.Candidates) > 0 {
			pickIdx := 0
			for ci := range r.Candidates {
				if r.Candidates[ci].ID == r.PickedCandidateID || r.Candidates[ci].ID == r.SelectedCandidateID {
					pickIdx = ci
					break
				}
			}
			pix = r.Candidates[pickIdx].Pixels
			wave = r.Candidates[pickIdx].Waveform
		}
		sel := (snap.selectedReq != nil && snap.selectedReq.ID == r.ID)
		rows = append(rows, railRow{
			id:         r.ID,
			targetPath: r.TargetPath,
			name:       path.Base(r.TargetPath),
			statusText: stText,
			statusCol:  stCol,
			selected:   sel,
			isImage:    r.Kind == AssetGenImages,
			pixels:     pix,
			waveform:   wave,
			clickable:  true,
		})
	}
	for _, lic := range snap.licenses {
		if seenPaths[lic.Asset] {
			continue
		}
		stText := "In use"
		stCol := specTextMuted
		if lic.UnknownLicense {
			stText = "License unknown"
			stCol = specAssetGenProblemText
		}
		isImg := inferAssetGenKind(lic.Asset, "") == AssetGenImages
		rows = append(rows, railRow{
			id:         "lic-" + lic.Asset,
			targetPath: lic.Asset,
			name:       path.Base(lic.Asset),
			statusText: stText,
			statusCol:  stCol,
			isImage:    isImg,
		})
	}

	rowNodes := make([]core.Node, 0, len(rows))
	for _, row := range rows {
		r := row
		var bg core.Color
		nameCol := specText
		if r.selected {
			bg = specAssetGenSelectedBg
			nameCol = specTextStrong
		}
		thumb := core.Canvas{
			Style: core.Style{
				Width:        core.Pt(32),
				Height:       core.Pt(32),
				FlexShrink:   0,
				CornerRadius: 6,
				Background:   specRaised,
			},
			Draw: func(d *core.Drawing, sz core.Size) {
				drawCheckerBackdrop(d, sz, 6)
				if r.isImage && r.pixels != nil {
					drawCrispPixels(d, r.pixels, 4, 4, float32(sz.Width)-8, float32(sz.Height)-8)
				} else {
					drawWaveformBars(d, r.waveform, 4, 6, float32(sz.Width)-8, float32(sz.Height)-12, specAssetGenWavePass, -1)
				}
			},
		}
		item := core.View{
			TestID: "asset-gen-item-" + sanitizeAssetGenTestID(r.name),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Height:       core.Pt(44),
				Padding:      core.Symmetric(0, 8),
				Gap:          8,
				CornerRadius: 8,
				Background:   bg,
			},
			Children: []core.Node{
				thumb,
				core.View{
					Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
					Children: []core.Node{
						core.Text{
							Content:  r.name,
							MaxLines: 1,
							Style:    core.Style{FontSize: 12.5, Color: nameCol},
						},
						core.Text{
							Content:  r.statusText,
							MaxLines: 1,
							Style:    core.Style{FontSize: 11.5, Color: r.statusCol},
						},
					},
				},
			},
		}
		if r.clickable {
			reqID := r.id
			item.OnPress = func(core.PressEvent) {
				ed.SelectGeneratedRequest(reqID)
			}
		}
		rowNodes = append(rowNodes, item)
	}

	serverNodes := make([]core.Node, 0, max(1, len(snap.servers)))
	if len(snap.servers) == 0 {
		serverNodes = append(serverNodes, core.Text{
			Content: ed.T("assetGen.noServers"),
			Style:   core.Style{FontSize: 12, Color: specTextMuted},
		})
	} else {
		for _, srv := range snap.servers {
			dotCol := specTextMuted
			if srv.Connected {
				dotCol = specAdded
			} else if srv.Error != "" {
				dotCol = specProblem
			}
			serverNodes = append(serverNodes, core.View{
				TestID: "asset-gen-server-" + sanitizeAssetGenTestID(srv.Config.Name),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: dotCol}},
					core.Text{Content: srv.Config.Name, Style: core.Style{FlexGrow: 1, FontSize: 12.5, Color: specText}},
					core.Text{Content: string(srv.Config.Kind), Style: core.Style{FontSize: 12, Color: specTextMuted}},
				},
			})
		}
	}

	return core.View{
		TestID: "asset-gen-left-rail",
		Style: core.Style{
			Width:       core.Pt(232),
			FlexShrink:  0,
			Background:  specBg,
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
			MinHeight:   core.Pt(0),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Height:         core.Pt(36),
					Padding:        core.Symmetric(0, 14),
				},
				Children: []core.Node{
					core.Text{Content: "GENERATED", Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted}},
					core.Text{Content: fmt.Sprintf("%d assets", len(rows)), Style: core.Style{FontSize: 11, FontWeight: 500, Color: specTextMuted}},
				},
			},
			core.View{
				Style:    core.Style{Padding: core.Symmetric(0, 6), Gap: 2},
				Children: rowNodes,
			},
			core.View{
				Style: core.Style{Padding: core.Edges{Top: 16, Left: 14, Right: 14, Bottom: 6}},
				Children: []core.Node{
					core.Text{Content: "GENERATORS · MCP", Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted}},
				},
			},
			core.View{
				TestID:   "asset-gen-servers",
				Style:    core.Style{Padding: core.Symmetric(0, 14), Gap: 8},
				Children: serverNodes,
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			core.View{
				Style: core.Style{
					Padding:     core.Symmetric(12, 14),
					Gap:         6,
					Borders:     core.Edges{Top: 1},
					BorderColor: specLine,
				},
				Children: []core.Node{
					core.Text{
						Content: "Each pick writes the file and its .source.yaml.",
						Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
					},
					ui.Button{
						TestID:  "asset-gen-settings-tools",
						Text:    ed.T("assetGen.settingsTools"),
						Variant: ui.ButtonGhost,
						Size:    ui.SizeSmall,
						TextStyle: core.Style{
							FontSize: 11.5,
							Color:    specAccent,
						},
						Style: core.Style{
							AlignSelf:    core.AlignStart,
							Height:       core.Pt(24),
							Padding:      core.Symmetric(0, 6),
							CornerRadius: 6,
							FocusVisible: specFocusRingStyle,
						},
						OnPress: func() {
							ed.SetEditorSettingsOpen(true)
						},
					},
				},
			},
		},
	}
}

func assetGenStatusDisplay(r *AssetGenerationRequest) (string, core.Color) {
	switch r.Status {
	case AssetGenLicenseUnknown:
		return "License unknown", specAssetGenProblemText
	case AssetGenInUse:
		if r.UsageCount > 0 {
			return fmt.Sprintf("In use · %d refs", r.UsageCount), specTextMuted
		}
		return "In use", specTextMuted
	case AssetGenPicked:
		if r.PickedCandidateID != "" {
			return fmt.Sprintf("Picked %s · written", r.PickedCandidateID), specAdded
		}
		return "Picked · written", specAdded
	default:
		return fmt.Sprintf("Pick 1 of %d", len(r.Candidates)), specAgent
	}
}

func assetGenCenterContentView(ed *Editor, snap assetGenViewSnapshot) core.Node {
	var sections []core.Node

	if snap.activeImageReq != nil {
		sections = append(sections, assetGenImageRequestSection(ed, snap.activeImageReq, snap))
	}
	if snap.activeSoundReq != nil {
		sections = append(sections, assetGenSoundRequestSection(ed, snap.activeSoundReq, snap))
	}
	if len(sections) == 0 {
		sections = append(sections, core.View{
			TestID: "asset-gen-empty",
			Style: core.Style{
				FlexGrow:       1,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Gap:            10,
				Padding:        core.All(24),
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("assetGen.emptyTitle"),
					Style:   core.Style{FontSize: 15, FontWeight: 600, Color: specTextStrong},
				},
				core.Text{
					Content: ed.T("assetGen.emptyBody"),
					Style:   core.Style{FontSize: 12.5, Color: specTextMuted},
				},
			},
		})
	}

	return core.View{
		Style:    core.Style{Gap: 16},
		Children: sections,
	}
}

// assetGenRefineBarView mounts outside the coarse assetGenRev Dynamic so typing in the Refine input
// never remounts or drops keyboard focus during background updates or candidate picks.
func assetGenRefineBarView(ed *Editor, genRev *core.Memo[uint64], activeImgReqID *core.Memo[string], refineDraftSig *core.Signal[string]) core.Node {
	return core.View{
		TestID: "asset-gen-refine-bar",
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         8,
			Padding:     core.Symmetric(10, 20),
			Background:  specBg,
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
			FlexShrink:  0,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					FlexGrow:     1,
					Height:       core.Pt(32),
					Padding:      core.Symmetric(0, 10),
					Gap:          8,
					CornerRadius: 8,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					core.Icon{SVG: lucide.Sparkles(), Style: core.Style{FontSize: 14, Color: specAgent}},
					ui.Input{
						TestID:      "asset-gen-refine-input",
						Label:       ed.T("assetGen.refine"),
						Value:       refineDraftSig.Get,
						Placeholder: ed.T("assetGen.refinePlaceholder"),
						OnChange: func(v string) {
							refineDraftSig.Set(v)
							ed.setAssetGenRefineDraft(v)
						},
						OnSubmit: func(v string) {
							reqID := activeImgReqID.Get()
							if reqID != "" && strings.TrimSpace(v) != "" {
								ed.report(ed.startRefineGeneratedAssetAsync(reqID, v))
							}
						},
						Style: core.Style{
							FlexGrow:         1,
							MinWidth:         core.Pt(0),
							Height:           core.Pt(28),
							Background:       core.Transparent,
							BorderWidth:      0,
							FontSize:         12,
							Color:            specText,
							PlaceholderColor: specTextMuted,
						},
					},
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = genRev.Get()
				snap := ed.snapshotAssetGenView()
				reqID := ""
				candCount := 4
				if snap.activeImageReq != nil {
					reqID = snap.activeImageReq.ID
					candCount = max(1, len(snap.activeImageReq.Candidates))
				}
				disabled := reqID == "" || snap.busy || snap.playingGame || snap.mergeConflict
				return core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						ui.Button{
							TestID:   "asset-gen-refine-btn",
							Text:     ed.T("assetGen.refine"),
							Icon:     lucide.Pencil,
							Variant:  ui.ButtonGhost,
							Size:     ui.SizeSmall,
							Disabled: func() bool { return disabled },
							Color:    func() core.Color { return specAssetGenBtnSec },
							Tint:     func() core.Color { return specText },
							TextStyle: core.Style{
								FontSize: 12,
								Color:    specText,
							},
							Style: core.Style{
								Height:       core.Pt(32),
								Padding:      core.Symmetric(0, 12),
								CornerRadius: 8,
								FocusVisible: specFocusRingStyle,
							},
							OnPress: func() {
								draft := ed.assetGenRefineDraft()
								if strings.TrimSpace(draft) == "" || reqID == "" {
									return
								}
								ed.report(ed.startRefineGeneratedAssetAsync(reqID, draft))
							},
						},
						ui.Button{
							TestID:   "asset-gen-regen-btn",
							Text:     fmt.Sprintf("Regenerate %d", candCount),
							Icon:     lucide.RefreshCw,
							Variant:  ui.ButtonGhost,
							Size:     ui.SizeSmall,
							Disabled: func() bool { return disabled },
							Color:    func() core.Color { return specAssetGenBtnSec },
							Tint:     func() core.Color { return specText },
							TextStyle: core.Style{
								FontSize: 12,
								Color:    specText,
							},
							Style: core.Style{
								Height:       core.Pt(32),
								Padding:      core.Symmetric(0, 12),
								CornerRadius: 8,
								FocusVisible: specFocusRingStyle,
							},
							OnPress: func() {
								if reqID == "" {
									return
								}
								ed.report(ed.startRegenerateAssetAsync(reqID))
							},
						},
					},
				}
			}),
		},
	}
}

func assetGenImageRequestSection(ed *Editor, req *AssetGenerationRequest, snap assetGenViewSnapshot) core.Node {
	// Background switcher ("PREVIEW ON")
	bgButtons := make([]core.Node, 0, len(snap.brief.Backgrounds))
	for _, b := range snap.brief.Backgrounds {
		bg := b
		on := snap.bg.ID == bg.ID
		var btnBg core.Color
		txtCol := specTextMuted
		if on {
			btnBg = specLineRaised
			txtCol = specTextStrong
		}
		bgButtons = append(bgButtons, ui.Button{
			TestID:  "asset-gen-bg-" + sanitizeAssetGenTestID(bg.ID),
			Text:    bg.Label,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color:   func() core.Color { return btnBg },
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: 500,
				Color:      txtCol,
			},
			Style: core.Style{
				Height:       core.Pt(26),
				Padding:      core.Symmetric(0, 10),
				CornerRadius: 6,
				FocusVisible: specFocusRingStyle,
			},
			OnPress: func() {
				ed.SetAssetGenPreviewBackground(bg.ID)
			},
		})
	}

	// Metadata line: generator · model · seeds ... · elapsed
	metaLine := formatRequestMetaLine(req)

	// Candidate cards row (up to 4 columns)
	candCards := make([]core.Node, 0, len(req.Candidates))
	for i := range req.Candidates {
		cand := req.Candidates[i]
		candCards = append(candCards, assetGenCandidateCard(ed, req, cand, snap))
	}

	children := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignStart,
				Gap:        16,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0), Gap: 4},
					Children: []core.Node{
						core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
							Children: []core.Node{
								core.Text{
									TestID:  "asset-gen-target-name",
									Content: path.Base(req.TargetPath),
									Style:   core.Style{FontSize: 14, FontWeight: 600, Color: specTextStrong},
								},
								core.Text{
									Content: fmt.Sprintf("· %d candidates", len(req.Candidates)),
									Style:   core.Style{FontSize: 12.5, Color: specTextMuted},
								},
							},
						},
						core.Text{
							Content: "“" + firstNonEmpty(req.UserPrompt, req.Prompt) + "”",
							Style:   core.Style{FontSize: 12.5, Color: specTextSecondary},
						},
						core.Text{
							TestID:  "asset-gen-meta",
							Content: metaLine,
							Style:   core.Style{FontSize: 11.5, FontFamily: core.FontMonospace, Color: specTextMuted},
						},
					},
				},
				core.View{
					Style: core.Style{AlignItems: core.AlignEnd, Gap: 4, FlexShrink: 0},
					Children: []core.Node{
						core.Text{Content: ed.T("assetGen.previewOn"), Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted}},
						core.View{
							Style: core.Style{
								Direction:    core.Row,
								Gap:          2,
								Padding:      core.All(3),
								CornerRadius: 8,
								Background:   specRaised,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
							},
							Children: bgButtons,
						},
					},
				},
			},
		},
		core.View{
			TestID: "asset-gen-candidates",
			Style: core.Style{
				Direction: core.Row,
				Gap:       12,
			},
			Children: candCards,
		},
	}

	if req.LastNote != "" {
		children = append(children, core.Text{
			TestID:  "asset-gen-note",
			Content: req.LastNote,
			Style:   core.Style{FontSize: 11.5, Color: specAgent},
		})
	}

	return core.View{
		Style:    core.Style{Gap: 14},
		Children: children,
	}
}

func assetGenCandidateCard(ed *Editor, req *AssetGenerationRequest, cand GeneratedCandidate, snap assetGenViewSnapshot) core.Node {
	bg := snap.bg
	selected := req.SelectedCandidateID == cand.ID
	picked := req.PickedCandidateID == cand.ID
	cardBg := specRaised
	cardBorder := specLineRaised
	borderW := core.Points(1)
	if selected {
		cardBg = specAssetGenSelectedBg
		cardBorder = specAccent
		borderW = core.Points(1.5)
	}
	var opacity core.Opacity
	if req.PickedCandidateID != "" && !picked {
		opacity = core.Alpha(0.60)
	}

	// Stage preview canvas: selected floor/checker background + reference sprite + candidate sprite + letter badge + picked badge.
	refPix := req.RefPixels
	candPix := cand.Pixels
	refLabel := path.Base(req.ReferenceAsset)
	stageChildren := []core.Node{
		core.Canvas{
			Style: core.Style{
				Width:  core.Pct(100),
				Height: core.Pt(168),
			},
			Draw: func(d *core.Drawing, sz core.Size) {
				drawCandidateStage(d, sz, bg, refPix, candPix)
			},
		},
		core.View{
			Style: core.Style{
				Position:       core.Absolute,
				Top:            core.Pt(8),
				Left:           core.Pt(8),
				Width:          core.Pt(22),
				Height:         core.Pt(22),
				CornerRadius:   6,
				Background:     specBgOverlay,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{Content: cand.ID, Style: core.Style{FontSize: 12, FontWeight: 600, Color: specTextStrong}},
			},
		},
	}
	if picked {
		stageChildren = append(stageChildren, core.View{
			Style: core.Style{
				Position:       core.Absolute,
				Top:            core.Pt(8),
				Right:          core.Pt(8),
				Height:         core.Pt(20),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				Background:     specAccent,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.Text{Content: ed.T("assetGen.picked"), Style: core.Style{FontSize: 11, FontWeight: 600, Color: specBg}},
			},
		})
	}
	if refLabel != "" && refLabel != "." {
		stageChildren = append(stageChildren, core.Text{
			Content: refLabel,
			Style: core.Style{
				Position: core.Absolute,
				Left:     core.Pt(10),
				Bottom:   core.Pt(6),
				FontSize: 11,
				Color:    specTextMuted,
			},
		})
	}

	// Up to 3 compact check lines on card (Palette, Pixel grid, Size)
	checkNodes := make([]core.Node, 0, 3)
	for i := 0; i < len(cand.Checks) && i < 3; i++ {
		chk := cand.Checks[i]
		sym := lucide.Check
		iconCol := specAdded
		txtCol := specTextSecondary
		if !chk.Pass {
			sym = lucide.X
			iconCol = specProblem
			txtCol = specAssetGenProblemText
		}
		checkNodes = append(checkNodes, core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, Height: core.Pt(20)},
			Children: []core.Node{
				core.Icon{SVG: sym(), Style: core.Style{FontSize: 13, Color: iconCol}},
				core.Text{
					Content:  chk.Summary,
					MaxLines: 1,
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, FontSize: 11.5, Color: txtCol},
				},
			},
		})
	}

	pickText := ed.T("assetGen.pick") + " " + cand.ID
	if picked {
		pickText = ed.T("assetGen.undoPick")
	}
	pickBg := specAssetGenBtnSec
	pickTxtCol := specText
	if selected && !picked {
		pickBg = specAccent
		pickTxtCol = specBg
	}

	reqID := req.ID
	candID := cand.ID
	actionDisabled := snap.playingGame || snap.mergeConflict
	return core.View{
		TestID: "asset-gen-cand-" + cand.ID,
		Style: core.Style{
			FlexGrow:     1,
			FlexBasis:    core.Pt(0),
			MinWidth:     core.Pt(0),
			Padding:      core.Edges{Top: 8, Left: 8, Right: 8, Bottom: 10},
			Gap:          10,
			CornerRadius: 12,
			Background:   cardBg,
			BorderWidth:  borderW,
			BorderColor:  cardBorder,
			Opacity:      opacity,
		},
		Children: []core.Node{
			core.View{
				TestID: "asset-gen-cand-stage-" + cand.ID,
				Style: core.Style{
					Height:       core.Pt(168),
					CornerRadius: 8,
					Overflow:     core.Hidden,
				},
				OnPress: func(core.PressEvent) {
					ed.SelectGeneratedCandidate(reqID, candID)
				},
				Children: stageChildren,
			},
			core.View{
				Style:    core.Style{Padding: core.Symmetric(0, 4), Gap: 4},
				Children: checkNodes,
			},
			core.View{
				Style: core.Style{Direction: core.Row, Gap: 8},
				Children: []core.Node{
					ui.Button{
						TestID:   "asset-gen-pick-" + cand.ID,
						Text:     pickText,
						Variant:  ui.ButtonGhost,
						Size:     ui.SizeSmall,
						Disabled: func() bool { return actionDisabled },
						Color:    func() core.Color { return pickBg },
						TextStyle: core.Style{
							FontSize:   12,
							FontWeight: 600,
							Color:      pickTxtCol,
						},
						Style: core.Style{
							FlexGrow:     1,
							Height:       core.Pt(30),
							CornerRadius: 8,
							FocusVisible: specFocusRingStyle,
						},
						OnPress: func() {
							if picked {
								ed.report(ed.UndoPickedCandidate(reqID, candID))
							} else {
								ed.report(ed.PickGeneratedCandidate(reqID, candID))
							}
						},
					},
					ui.Button{
						TestID:   "asset-gen-vary-" + cand.ID,
						Text:     ed.T("assetGen.varyThis"),
						Variant:  ui.ButtonGhost,
						Size:     ui.SizeSmall,
						Disabled: func() bool { return actionDisabled || snap.busy },
						Color:    func() core.Color { return specAssetGenBtnSec },
						TextStyle: core.Style{
							FontSize: 12,
							Color:    specText,
						},
						Style: core.Style{
							Height:       core.Pt(30),
							Padding:      core.Symmetric(0, 10),
							CornerRadius: 8,
							FocusVisible: specFocusRingStyle,
						},
						OnPress: func() {
							ed.report(ed.startVaryGeneratedCandidateAsync(reqID, candID))
						},
					},
				},
			},
		},
	}
}

func assetGenSoundRequestSection(ed *Editor, req *AssetGenerationRequest, snap assetGenViewSnapshot) core.Node {
	actionDisabled := snap.playingGame || snap.mergeConflict
	rows := make([]core.Node, 0, len(req.Candidates))
	for i := range req.Candidates {
		cand := req.Candidates[i]
		playing := snap.playingReqID == req.ID && snap.playingCandID == cand.ID
		picked := req.PickedCandidateID == cand.ID
		rowBg := specRaised
		rowBorder := specLineRaised
		rowBorderW := core.Points(1)
		if picked {
			rowBg = specAssetGenSelectedBg
			rowBorder = specAccent
			rowBorderW = core.Points(1.5)
		}
		playBg := specAssetGenBtnSec
		playTint := specTextStrong
		playSym := lucide.Play
		if playing {
			playBg = specTextStrong
			playTint = specBg
			playSym = lucide.X
		}
		checkCol := specTextMuted
		waveCol := specAssetGenWavePass
		if !cand.AudioCheckPass {
			checkCol = specAssetGenProblemText
			waveCol = specAssetGenWaveFail
		}
		pickBg := specAssetGenBtnSec
		pickTxtCol := specText
		pickTxt := ed.T("assetGen.pick")
		if picked {
			pickBg = specAccent
			pickTxtCol = specBg
			pickTxt = ed.T("assetGen.picked")
		}
		reqID := req.ID
		candID := cand.ID
		wave := cand.Waveform
		headFrac := float32(-1)
		if playing {
			headFrac = 0.38
		}
		rows = append(rows, core.View{
			TestID: "asset-gen-sound-" + cand.ID,
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Height:       core.Pt(44),
				Padding:      core.Symmetric(0, 8),
				Gap:          12,
				CornerRadius: 8,
				Background:   rowBg,
				BorderWidth:  rowBorderW,
				BorderColor:  rowBorder,
			},
			Children: []core.Node{
				ui.Button{
					TestID:   "asset-gen-sound-play-" + cand.ID,
					Icon:     playSym,
					Label:    "Play candidate " + cand.ID,
					Variant:  ui.ButtonGhost,
					Size:     ui.SizeSmall,
					Disabled: func() bool { return actionDisabled },
					Color:    func() core.Color { return playBg },
					Tint:     func() core.Color { return playTint },
					Style: core.Style{
						Width:        core.Pt(30),
						Height:       core.Pt(30),
						CornerRadius: 8,
						FocusVisible: specFocusRingStyle,
					},
					OnPress: func() {
						ed.report(ed.PlayGeneratedSoundCandidate(reqID, candID))
					},
				},
				core.Text{
					Content: cand.ID,
					Style:   core.Style{Width: core.Pt(16), FontSize: 12.5, FontWeight: 600, Color: specTextStrong},
				},
				core.Canvas{
					Style: core.Style{
						FlexGrow:  1,
						FlexBasis: core.Pt(0),
						Height:    core.Pt(28),
					},
					Draw: func(d *core.Drawing, sz core.Size) {
						drawWaveformBars(d, wave, 0, 2, float32(sz.Width), float32(sz.Height)-4, waveCol, headFrac)
					},
				},
				core.Text{
					Content: fmt.Sprintf("%.2f s", cand.Duration.Seconds()),
					Style:   core.Style{Width: core.Pt(48), FontSize: 11.5, FontFamily: core.FontMonospace, Color: specTextSecondary},
				},
				core.Text{
					Content: formatLoudnessLabel(cand.LoudnessDBFS),
					Style:   core.Style{Width: core.Pt(88), FontSize: 11.5, FontFamily: core.FontMonospace, Color: specTextSecondary},
				},
				core.Text{
					Content:  cand.AudioCheckText,
					MaxLines: 1,
					Style:    core.Style{Width: core.Pt(160), FontSize: 11.5, Color: checkCol},
				},
				ui.Button{
					TestID:   "asset-gen-sound-pick-" + cand.ID,
					Text:     pickTxt,
					Variant:  ui.ButtonGhost,
					Size:     ui.SizeSmall,
					Disabled: func() bool { return actionDisabled },
					Color:    func() core.Color { return pickBg },
					TextStyle: core.Style{
						FontSize:   12,
						FontWeight: 600,
						Color:      pickTxtCol,
					},
					Style: core.Style{
						Height:       core.Pt(28),
						Padding:      core.Symmetric(0, 12),
						CornerRadius: 8,
						FocusVisible: specFocusRingStyle,
					},
					OnPress: func() {
						if picked {
							ed.report(ed.UndoPickedCandidate(reqID, candID))
						} else {
							ed.report(ed.PickGeneratedCandidate(reqID, candID))
						}
					},
				},
			},
		})
	}

	return core.View{
		TestID: "asset-gen-sound-section",
		Style: core.Style{
			Padding:     core.Edges{Top: 12},
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
			Gap:         8,
		},
		Children: append([]core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.Text{
						Content: path.Base(req.TargetPath),
						Style:   core.Style{FontSize: 14, FontWeight: 600, Color: specTextStrong},
					},
					core.Text{
						Content:  fmt.Sprintf("· %d candidates · “%s”", len(req.Candidates), firstNonEmpty(req.UserPrompt, req.Prompt)),
						MaxLines: 1,
						Style:    core.Style{FlexGrow: 1, FlexShrink: 1, FontSize: 12, Color: specTextMuted},
					},
					core.Text{
						Content: formatSeedRangeMeta(req),
						Style:   core.Style{FontSize: 11.5, FontFamily: core.FontMonospace, Color: specTextMuted},
					},
				},
			},
		}, rows...),
	}
}

func assetGenRightInspectorHeader(snap assetGenViewSnapshot) core.Node {
	headerTitle := "Candidate"
	headerSub := ""
	if snap.selectedCand != nil {
		headerTitle = "Candidate " + snap.selectedCand.ID
		if snap.selectedCand.Seed != nil {
			headerSub = fmt.Sprintf("· seed %d", *snap.selectedCand.Seed)
		}
	}
	return core.View{
		Style: core.Style{
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Height:      core.Pt(44),
			FlexShrink:  0,
			Padding:     core.Symmetric(0, 16),
			Gap:         8,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			core.Text{Content: headerTitle, Style: core.Style{FontSize: 14, FontWeight: 600, Color: specTextStrong}},
			core.Text{Content: headerSub, Style: core.Style{FontSize: 12.5, Color: specTextMuted}},
		},
	}
}

func assetGenRightInspectorBody(ed *Editor, snap assetGenViewSnapshot, onBeginEditLicense func(assetPath string)) core.Node {
	// 1. Style against brief.yaml
	var styleNodes []core.Node
	styleNodes = append(styleNodes,
		core.View{
			Style: core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, AlignItems: core.AlignCenter},
			Children: []core.Node{
				core.Text{Content: "STYLE AGAINST THE BRIEF", Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted}},
				core.Text{Content: "brief.yaml", Style: core.Style{FontSize: 11, FontFamily: core.FontMonospace, Color: specTextMuted}},
			},
		},
		core.View{
			Style: core.Style{
				Padding:     core.Symmetric(8, 12),
				Borders:     core.Edges{Left: 2},
				BorderColor: specLineRaised,
			},
			Children: []core.Node{
				core.Text{Content: snap.brief.Summary, Style: core.Style{FontSize: 12, Color: specTextSecondary}},
			},
		},
	)
	if snap.selectedCand != nil {
		for _, sc := range snap.selectedCand.Checks {
			sym := lucide.Check
			iconCol := specAdded
			detCol := specTextMuted
			if !sc.Pass {
				sym = lucide.X
				iconCol = specProblem
				detCol = specAssetGenProblemText
			}
			styleNodes = append(styleNodes, core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignStart, Gap: 8, Padding: core.Symmetric(2, 0)},
				Children: []core.Node{
					core.View{
						Style:    core.Style{Padding: core.Edges{Top: 2}},
						Children: []core.Node{core.Icon{SVG: sym(), Style: core.Style{FontSize: 13, Color: iconCol}}},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, Gap: 2},
						Children: []core.Node{
							core.Text{Content: sc.Title, Style: core.Style{FontSize: 12.5, Color: specTextStrong}},
							core.Text{Content: sc.Detail, Style: core.Style{FontSize: 11.5, Color: detCol}},
						},
					},
				},
			})
		}
	}

	// 2. SOURCE (<file>.source.yaml preview)
	sourceFileName := "asset.source.yaml"
	var sourceLineNodes []core.Node
	if snap.selectedReq != nil && snap.selectedCand != nil {
		sourceFileName = path.Base(importsettings.AssetSourcePath(snap.selectedReq.TargetPath))
		picked := snap.selectedReq.PickedCandidateID == snap.selectedCand.ID
		approvedVal := "null"
		if picked {
			approvedVal = snap.approvalAuthor
		}
		seedVal := "null"
		if snap.selectedCand.Seed != nil {
			seedVal = strconv.FormatInt(*snap.selectedCand.Seed, 10)
		}
		refsVal := "[" + strings.Join(snap.selectedReq.References, ", ") + "]"
		licVal := firstNonEmpty(snap.selectedCand.License, snap.selectedReq.License, "unknown")
		commVal := strconv.FormatBool(snap.selectedCand.CommercialUse)

		kvLines := [][2]string{
			{"version:", strconv.Itoa(importsettings.AssetSourceVersion)},
			{"generator:", snap.selectedReq.Generator},
			{"model:", firstNonEmpty(snap.selectedReq.Model, "default")},
			{"prompt:", strconv.Quote(snap.selectedReq.Prompt)},
			{"seed:", seedVal},
			{"references:", refsVal},
			{"license:", licVal},
			{"commercialUse:", commVal},
			{"approvedBy:", approvedVal},
		}
		for _, kv := range kvLines {
			var rowBg core.Color
			valCol := specText
			if kv[0] == "approvedBy:" && picked {
				rowBg = specAssetGenApprovedRow
				valCol = specAdded
			}
			sourceLineNodes = append(sourceLineNodes, core.View{
				Style: core.Style{
					Direction:  core.Row,
					Padding:    core.Symmetric(1, 12),
					Gap:        6,
					Background: rowBg,
				},
				Children: []core.Node{
					core.Text{Content: kv[0], Style: core.Style{FontSize: 11.5, FontFamily: core.FontMonospace, Color: specTextMuted}},
					core.Text{Content: kv[1], MaxLines: 1, Style: core.Style{FlexGrow: 1, FlexShrink: 1, FontSize: 11.5, FontFamily: core.FontMonospace, Color: valCol}},
				},
			})
		}
	}

	// 3. LICENSES · CHECKED AT EXPORT
	licenseRows := make([]core.Node, 0, len(snap.licenses)+1)
	if snap.licenseScanErr != "" {
		licenseRows = append(licenseRows, core.View{
			TestID: "asset-gen-license-error",
			Style: core.Style{
				Padding:      core.Symmetric(8, 10),
				CornerRadius: 8,
				Background:   specRaised,
				BorderWidth:  1.5,
				BorderColor:  specProblem,
			},
			Children: []core.Node{
				core.Text{
					Content: snap.licenseScanErr,
					Style:   core.Style{FontSize: 11.5, Color: specAssetGenProblemText},
				},
			},
		})
	}
	actionDisabled := snap.playingGame || snap.mergeConflict
	for i, lic := range snap.licenses {
		lc := lic
		var rowBg core.Color
		var rowBorder core.Color
		rowBorderW := core.Points(0)
		noteText := lc.License
		if lc.CommercialUse {
			noteText += " · commercial use allowed"
		}
		noteCol := specTextMuted
		if lc.UnknownLicense {
			rowBg = specRaised
			rowBorder = specProblem
			rowBorderW = core.Points(1.5)
			noteText = "License unknown · blocks export"
			noteCol = specAssetGenProblemText
		}
		rowChildren := []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0), Gap: 2},
				Children: []core.Node{
					core.Text{Content: path.Base(lc.Asset), Style: core.Style{FontSize: 11.5, FontFamily: core.FontMonospace, Color: specTextStrong}},
					core.Text{Content: noteText, Style: core.Style{FontSize: 11.5, Color: noteCol}},
				},
			},
		}
		if lc.UnknownLicense {
			assetPath := lc.Asset
			rowChildren = append(rowChildren, ui.Button{
				TestID:   fmt.Sprintf("asset-gen-set-license-%d", i),
				Text:     ed.T("assetGen.setLicense"),
				Variant:  ui.ButtonGhost,
				Size:     ui.SizeSmall,
				Disabled: func() bool { return actionDisabled },
				Color:    func() core.Color { return specAssetGenBtnSec },
				TextStyle: core.Style{
					FontSize: 11.5,
					Color:    specText,
				},
				Style: core.Style{
					Height:       core.Pt(28),
					Padding:      core.Symmetric(0, 10),
					CornerRadius: 6,
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func() {
					onBeginEditLicense(assetPath)
				},
			})
		}
		licenseRows = append(licenseRows, core.View{
			TestID: "asset-gen-license-" + sanitizeAssetGenTestID(path.Base(lc.Asset)),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Padding:      core.Symmetric(8, 10),
				Gap:          8,
				CornerRadius: 8,
				Background:   rowBg,
				BorderWidth:  rowBorderW,
				BorderColor:  rowBorder,
			},
			Children: rowChildren,
		})
	}
	if len(licenseRows) == 0 {
		licenseRows = append(licenseRows, core.Text{
			Content: ed.T("assetGen.noLicenses"),
			Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
		})
	}

	return core.View{
		Style: core.Style{Gap: 16},
		Children: []core.Node{
			core.View{Style: core.Style{Gap: 8}, Children: styleNodes},
			core.View{
				Style: core.Style{Gap: 8},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, AlignItems: core.AlignCenter},
						Children: []core.Node{
							core.Text{Content: "SOURCE", Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted}},
							core.Text{Content: sourceFileName, Style: core.Style{FontSize: 11, FontFamily: core.FontMonospace, Color: specTextMuted}},
						},
					},
					core.View{
						TestID: "asset-gen-source-preview",
						Style: core.Style{
							Padding:      core.Symmetric(8, 0),
							CornerRadius: 12,
							Background:   specRaised,
							BorderWidth:  1,
							BorderColor:  specLineRaised,
							Gap:          2,
						},
						Children: sourceLineNodes,
					},
					core.Text{
						TestID:  "asset-gen-untrusted-notice",
						Content: ed.T("assetGen.untrustedNotice"),
						Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
					},
				},
			},
			core.View{
				TestID: "asset-gen-licenses",
				Style:  core.Style{Gap: 6},
				Children: append([]core.Node{
					core.Text{Content: ed.T("assetGen.licensesHeader"), Style: core.Style{FontSize: 11, FontWeight: 600, Color: specTextMuted}},
				}, licenseRows...),
			},
		},
	}
}

// assetGenLicenseEditorBar mounts outside the coarse assetGenRev Dynamic so typing a license string
// never remounts or loses focus, and never silently fabricates a license identifier.
func assetGenLicenseEditorBar(ed *Editor, licEditPath *core.Memo[string], licDraftSig *core.Signal[string], licCommSig *core.Signal[bool]) core.Node {
	return core.View{
		TestID: "asset-gen-license-editor",
		Style: core.Style{
			Padding:     core.All(12),
			Gap:         8,
			Background:  specRaised,
			Borders:     core.Edges{Top: 1},
			BorderColor: specLineRaised,
			FlexShrink:  0,
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				target := path.Base(licEditPath.Get())
				return core.Text{
					Content: fmt.Sprintf("%s · %s", ed.T("assetGen.licenseEditTitle"), target),
					Style:   core.Style{FontSize: 11.5, FontWeight: 600, Color: specTextStrong},
				}
			}),
			ui.Input{
				TestID:      "asset-gen-license-input",
				Label:       ed.T("assetGen.licenseEditTitle"),
				Value:       licDraftSig.Get,
				Placeholder: ed.T("assetGen.licensePlaceholder"),
				OnChange: func(v string) {
					licDraftSig.Set(v)
					ed.SetGeneratedLicenseDraft(v)
				},
				OnSubmit: func(v string) {
					ed.SetGeneratedLicenseDraft(v)
					ed.report(ed.SaveEditedGeneratedLicense())
				},
				Style: core.Style{
					Height:           core.Pt(30),
					Padding:          core.Symmetric(0, 8),
					CornerRadius:     6,
					Background:       specBg,
					BorderWidth:      1,
					BorderColor:      specLineRaised,
					FontSize:         12,
					Color:            specText,
					PlaceholderColor: specTextMuted,
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				comm := licCommSig.Get()
				boxBg := specBg
				boxBorder := specLineRaised
				iconCol := core.Transparent
				if comm {
					boxBg = specAccent
					boxBorder = specAccent
					iconCol = specBg
				}
				return core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
					Children: []core.Node{
						ui.Button{
							TestID:  "asset-gen-license-commercial",
							Text:    ed.T("assetGen.commercialUse"),
							Icon:    lucide.Check,
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return boxBg },
							Tint:    func() core.Color { return iconCol },
							TextStyle: core.Style{
								FontSize: 11.5,
								Color:    specText,
							},
							Style: core.Style{
								Height:       core.Pt(26),
								Padding:      core.Symmetric(0, 8),
								CornerRadius: 6,
								BorderWidth:  1,
								BorderColor:  boxBorder,
								FocusVisible: specFocusRingStyle,
							},
							OnPress: func() {
								next := !licCommSig.Get()
								licCommSig.Set(next)
								ed.SetGeneratedLicenseCommercial(next)
							},
						},
						core.View{Style: core.Style{FlexGrow: 1}},
						ui.Button{
							TestID:  "asset-gen-license-cancel",
							Text:    ed.T("assetGen.cancel"),
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return specAssetGenBtnSec },
							TextStyle: core.Style{
								FontSize: 11.5,
								Color:    specText,
							},
							Style: core.Style{
								Height:       core.Pt(26),
								Padding:      core.Symmetric(0, 10),
								CornerRadius: 6,
								FocusVisible: specFocusRingStyle,
							},
							OnPress: func() {
								ed.CancelEditGeneratedLicense()
							},
						},
						ui.Button{
							TestID:  "asset-gen-license-save",
							Text:    ed.T("assetGen.saveLicense"),
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Color:   func() core.Color { return specAccent },
							TextStyle: core.Style{
								FontSize:   11.5,
								FontWeight: 600,
								Color:      specBg,
							},
							Style: core.Style{
								Height:       core.Pt(26),
								Padding:      core.Symmetric(0, 10),
								CornerRadius: 6,
								FocusVisible: specFocusRingStyle,
							},
							OnPress: func() {
								ed.report(ed.SaveEditedGeneratedLicense())
							},
						},
					},
				}
			}),
		},
	}
}

func (e *Editor) assetGenRefineDraft() string {
	e.assetGen.mu.Lock()
	defer e.assetGen.mu.Unlock()
	return e.assetGen.refineDraft
}

func (e *Editor) setAssetGenRefineDraft(v string) {
	e.assetGen.mu.Lock()
	e.assetGen.refineDraft = v
	e.assetGen.mu.Unlock()
}

func formatRequestMetaLine(req *AssetGenerationRequest) string {
	var parts []string
	if req.Generator != "" {
		parts = append(parts, req.Generator)
	}
	if req.Model != "" {
		parts = append(parts, req.Model)
	}
	if sr := formatSeedsOnly(req); sr != "" {
		parts = append(parts, sr)
	}
	if req.Elapsed > 0 {
		secs := int(math.Round(req.Elapsed.Seconds()))
		if secs < 1 {
			parts = append(parts, fmt.Sprintf("%d ms", req.Elapsed.Milliseconds()))
		} else {
			parts = append(parts, fmt.Sprintf("%d s", secs))
		}
	}
	return strings.Join(parts, " · ")
}

func formatSeedRangeMeta(req *AssetGenerationRequest) string {
	var parts []string
	if req.Generator != "" {
		parts = append(parts, req.Generator)
	}
	if sr := formatSeedsOnly(req); sr != "" {
		parts = append(parts, sr)
	}
	return strings.Join(parts, " · ")
}

func formatSeedsOnly(req *AssetGenerationRequest) string {
	var first, last *int64
	for _, c := range req.Candidates {
		if c.Seed != nil {
			if first == nil {
				first = c.Seed
			}
			last = c.Seed
		}
	}
	if first == nil {
		return ""
	}
	if *first == *last {
		return fmt.Sprintf("seed %d", *first)
	}
	return fmt.Sprintf("seeds %d–%d", *first, *last)
}

func formatLoudnessLabel(dbfs float64) string {
	if math.IsNaN(dbfs) {
		return "— dBFS"
	}
	if math.IsInf(dbfs, -1) {
		return "−∞ dBFS"
	}
	return fmt.Sprintf("%.1f dBFS", dbfs)
}

func sanitizeAssetGenTestID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// --- Canvas drawing helpers (strictly Spec-derived colors, crisp pixel rendering) ---

func drawCheckerBackdrop(d *core.Drawing, sz core.Size, tile float32) {
	w, h := float32(sz.Width), float32(sz.Height)
	d.SetFillColor(specRaised).FillRect(0, 0, w, h)
	if tile <= 0 {
		tile = 12
	}
	d.SetFillColor(specAssetGenCheckerTile)
	for y := float32(0); y < h; y += tile {
		row := int(y / tile)
		for x := float32(0); x < w; x += tile {
			col := int(x / tile)
			if (row+col)%2 == 0 {
				d.FillRect(x, y, min(tile, w-x), min(tile, h-y))
			}
		}
	}
}

func drawCandidateStage(d *core.Drawing, sz core.Size, bg AssetPreviewBackground, refPix, candPix *image.RGBA) {
	w, h := float32(sz.Width), float32(sz.Height)
	if bg.Checker {
		drawCheckerBackdrop(d, sz, 12)
	} else {
		d.SetFillColor(bg.Color).FillRect(0, 0, w, h)
		// Subtle warm torch glow in upper right using Spec changed-amber alpha.
		d.BeginPath().Arc(w-12, 16, 48, 0, 2*math.Pi, false).SetFillColor(specAssetGenTorchGlow).Fill()
		// Floor tiles strip along bottom.
		floorH := float32(34)
		floorY := h - floorH
		d.SetFillColor(specLineRaised).FillRect(0, floorY, w, floorH)
		d.SetFillColor(specLine)
		for x := float32(0); x < w; x += 32 {
			d.FillRect(x, floorY, 1.5, floorH)
		}
		d.FillRect(0, floorY, w, 2)
		d.SetFillColor(specAssetGenFloorTile).FillRect(0, floorY+16, w, 1.5)
	}

	groundY := h - 34
	if bg.Checker {
		groundY = h - 22
	}
	// Draw reference sprite on left when available, and candidate sprite on right at matching pixel scale.
	if refPix != nil && candPix != nil {
		maxDim := max(max(refPix.Bounds().Dx(), refPix.Bounds().Dy()), max(candPix.Bounds().Dx(), candPix.Bounds().Dy()))
		pxScale := float32(4)
		if maxDim > 0 {
			fit := float32(math.Floor(float64(min(w*0.36, groundY-24)) / float64(maxDim)))
			pxScale = max(1, min(6, fit))
		}
		rw, rh := float32(refPix.Bounds().Dx())*pxScale, float32(refPix.Bounds().Dy())*pxScale
		cw, ch := float32(candPix.Bounds().Dx())*pxScale, float32(candPix.Bounds().Dy())*pxScale
		refX := max(float32(12), w*0.24-rw/2)
		candX := min(w-cw-12, w*0.64-cw/2)
		drawCrispPixels(d, refPix, refX, groundY-rh, rw, rh)
		drawCrispPixels(d, candPix, candX, groundY-ch, cw, ch)
	} else if candPix != nil {
		drawCrispPixels(d, candPix, 16, 16, w-32, groundY-24)
	}
}

func drawCrispPixels(d *core.Drawing, rgba *image.RGBA, x, y, maxW, maxH float32) {
	if rgba == nil || maxW <= 0 || maxH <= 0 {
		return
	}
	b := rgba.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw <= 0 || ih <= 0 {
		return
	}
	scale := min(maxW/float32(iw), maxH/float32(ih))
	if scale >= 1 {
		scale = float32(math.Floor(float64(scale)))
		if scale < 1 {
			scale = 1
		}
	}
	dw := float32(iw) * scale
	dh := float32(ih) * scale
	ox := x + (maxW-dw)/2
	oy := y + (maxH-dh)/2

	if iw <= 64 && ih <= 64 {
		for py := 0; py < ih; py++ {
			for px := 0; px < iw; px++ {
				c := rgba.RGBAAt(b.Min.X+px, b.Min.Y+py)
				if c.A == 0 {
					continue
				}
				rgb := uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
				col := core.RGBA(rgb, float32(c.A)/255.0)
				d.SetFillColor(col).FillRect(ox+float32(px)*scale, oy+float32(py)*scale, scale, scale)
			}
		}
		return
	}
	d.DrawImageFrom(core.ImageSource{Pixels: rgba}, 0, 0, float32(iw), float32(ih), ox, oy, dw, dh)
}

func drawWaveformBars(d *core.Drawing, bars []float32, x, y, w, h float32, col core.Color, playheadFrac float32) {
	if w <= 0 || h <= 0 {
		return
	}
	n := len(bars)
	if n == 0 {
		// Flat muted baseline when no samples decoded.
		d.SetFillColor(specLineRaised).FillRect(x, y+h/2-1, w, 2)
		return
	}
	step := w / float32(n)
	barW := max(1.5, min(2.5, step*0.6))
	midY := y + h/2
	d.SetFillColor(col)
	for i, amp := range bars {
		bh := max(2, amp*h)
		bx := x + float32(i)*step
		d.FillRect(bx, midY-bh/2, barW, bh)
	}
	if playheadFrac >= 0 && playheadFrac <= 1 {
		px := x + w*playheadFrac
		d.SetFillColor(specTextStrong).FillRect(px, y, 1.5, h)
	}
}
