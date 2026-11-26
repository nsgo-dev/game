package editor

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/math3d"
	gamemcp "github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// AskInPlaceSuggestion is one contextual suggestion chip shown under the ⌘L in-place composer.
type AskInPlaceSuggestion struct {
	ID     string
	Label  string
	Prompt string
}

// AskInPlacePlacement holds the computed viewport-local coordinates of the anchored ⌘L composer card
// and the selection bounds it anchors to, clamped to stay on screen and clear of the right panel.
type AskInPlacePlacement struct {
	Left               float32
	Top                float32
	Width              float32
	Height             float32
	AnchorX            float32
	AnchorY            float32
	SelectionBounds    [4]float32 // minX, minY, maxX, maxY in viewport points
	HasSelectionBounds bool
}

// AskInPlaceSnapshot is a read-only view of the in-place ⌘L composer state for UI and tests.
type AskInPlaceSnapshot struct {
	Open               bool
	Draft              string
	TargetIDs          []string
	TargetNames        []string
	SceneLabel         string
	ChipSummary        string
	ChipAnnouncement   string
	InputLabel         string
	Placeholder        string
	Suggestions        []AskInPlaceSuggestion
	SelectedSuggestion int
	Placement          AskInPlacePlacement
}

type askInPlaceState struct {
	open               bool
	seq                uint64
	draft              string
	targetIDs          []string
	targetNames        []string
	suggestions        []AskInPlaceSuggestion
	selectedSuggestion int
	returnFocus        *core.Ref
	measuredW          float32
	measuredH          float32
}

const (
	askInPlaceDefaultWidth  float32 = 420
	askInPlaceMinWidth      float32 = 220
	askInPlaceDefaultHeight float32 = 152
	askInPlaceMargin        float32 = 12
	askInPlaceAnchorGap     float32 = 14
	maxViewportPNGDim               = 1280
)

func (e *Editor) askInPlaceBlocked() bool {
	return e == nil || e.Doc == nil || e.playing || e.shortcutCaptureActive ||
		e.AgentMode() == AgentModeNone ||
		e.StartScreenOpen() || e.ReviewOpen() || e.MergeConflictOpen() || e.PlaytestReportOpen() ||
		e.UsagesScreenOpen() || e.ChangesOpen() || e.HUDLayoutOpen() ||
		e.SettingsOpen() || e.ShortcutsSheetOpen() || e.BuildReleaseScreenOpen() ||
		e.ConnectDialogOpen() || e.AssetGeneratorOpen() || e.CutsceneOpen()
}

func (e *Editor) askInPlaceKeyBlocked() bool {
	return e == nil || e.playing || e.shortcutCaptureActive ||
		e.StartScreenOpen() || e.ReviewOpen() || e.MergeConflictOpen() || e.PlaytestReportOpen() ||
		e.UsagesScreenOpen() || e.ChangesOpen() || e.HUDLayoutOpen() ||
		e.SettingsOpen() || e.ShortcutsSheetOpen() || e.BuildReleaseScreenOpen() ||
		e.ConnectDialogOpen() || e.AssetGeneratorOpen() || e.CommandPaletteOpen()
}

// AskInPlaceOpen reports whether the anchored ⌘L in-place agent composer is currently open.
func (e *Editor) AskInPlaceOpen() bool {
	return e != nil && e.askInPlace.open
}

// OpenAskInPlace opens the anchored in-place composer targeting the current object selection,
// or targeting the open scene when nothing is selected.
func (e *Editor) OpenAskInPlace() error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no scene open")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.askInPlaceBlocked() {
		return nil
	}
	ids := e.SelectedIDs()
	return e.openAskInPlaceForTargets(ids)
}

// OpenAskInPlaceFor opens the anchored in-place composer targeting ids (or the open scene when ids is empty).
func (e *Editor) OpenAskInPlaceFor(ids ...string) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no scene open")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.askInPlaceBlocked() {
		return nil
	}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || slices.Contains(filtered, id) {
			continue
		}
		if obj, _, _, _ := e.resolveContextObject(id); obj != nil {
			filtered = append(filtered, id)
		}
	}
	return e.openAskInPlaceForTargets(filtered)
}

func (e *Editor) openAskInPlaceForTargets(ids []string) error {
	if e.contextMenu.open {
		e.contextMenu.open = false
		e.contextMenu.targetID = ""
	}
	if e.paletteOpen {
		e.paletteOpen = false
	}
	// Capture current focus before opening so Esc returns focus right where it was.
	if !e.askInPlace.open && e.scope != nil {
		e.askInPlace.returnFocus = core.FocusedRef(e.scope)
	}
	names := make([]string, 0, len(ids))
	cleanIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		obj, _, _, _ := e.resolveContextObject(id)
		if obj == nil {
			continue
		}
		cleanIDs = append(cleanIDs, obj.ID)
		names = append(names, contextObjectDisplayName(obj))
	}
	e.askInPlace.open = true
	e.askInPlace.seq++
	e.askInPlace.draft = ""
	e.askInPlace.targetIDs = cleanIDs
	e.askInPlace.targetNames = names
	e.askInPlace.suggestions = e.buildAskInPlaceSuggestions(cleanIDs)
	e.askInPlace.selectedSuggestion = -1
	e.askInPlace.measuredW = 0
	e.askInPlace.measuredH = 0

	if e.scope != nil {
		snap := e.AskInPlaceSnapshot()
		e.scope.Announce(snap.ChipAnnouncement)
	}
	e.notify()
	return nil
}

// CloseAskInPlace closes the anchored ⌘L in-place composer without sending and restores prior focus.
func (e *Editor) CloseAskInPlace() {
	if e == nil || !e.askInPlace.open {
		return
	}
	ret := e.askInPlace.returnFocus
	e.askInPlace.open = false
	e.askInPlace.draft = ""
	e.askInPlace.selectedSuggestion = -1
	e.askInPlace.returnFocus = nil
	e.askInPlace.seq++
	e.notify()
	if ret != nil && refMounted(ret) && !e.IsDragging() {
		ret.Focus()
	}
}

// SetAskInPlaceDraft updates the multi-line text draft of the open in-place composer
// and clears any highlighted suggestion so free typing is never blocked.
func (e *Editor) SetAskInPlaceDraft(text string) {
	if e == nil || !e.askInPlace.open {
		return
	}
	if e.askInPlace.draft == text && e.askInPlace.selectedSuggestion == -1 {
		return
	}
	e.askInPlace.draft = text
	e.askInPlace.selectedSuggestion = -1
	e.notify()
}

// InsertAskInPlaceNewline inserts a newline into the open in-place composer draft (⇧↵).
func (e *Editor) InsertAskInPlaceNewline() {
	if e == nil || !e.askInPlace.open {
		return
	}
	e.askInPlace.draft += "\n"
	e.askInPlace.selectedSuggestion = -1
	e.notify()
}

// AskInPlaceSelectedSuggestion returns the currently keyboard-highlighted suggestion index, or -1 when none is highlighted.
func (e *Editor) AskInPlaceSelectedSuggestion() int {
	if e == nil || !e.askInPlace.open {
		return -1
	}
	return e.askInPlace.selectedSuggestion
}

// StepAskInPlaceSuggestion moves the highlighted contextual suggestion by delta (+1 for Down, -1 for Up),
// keeping -1 ("free typing") in the cycle so arrows never trap or block typing.
func (e *Editor) StepAskInPlaceSuggestion(delta int) {
	if e == nil || !e.askInPlace.open || len(e.askInPlace.suggestions) == 0 || delta == 0 {
		return
	}
	n := len(e.askInPlace.suggestions)
	cur := e.askInPlace.selectedSuggestion
	var next int
	if delta > 0 {
		if cur < 0 {
			next = 0
		} else if cur+1 < n {
			next = cur + 1
		} else {
			next = -1
		}
	} else {
		if cur < 0 {
			next = n - 1
		} else if cur-1 >= 0 {
			next = cur - 1
		} else {
			next = -1
		}
	}
	if next == cur {
		return
	}
	e.askInPlace.selectedSuggestion = next
	e.notify()
}

// PickAskInPlaceSuggestion selects suggestion index and immediately starts an in-place agent turn with it.
func (e *Editor) PickAskInPlaceSuggestion(index int) (AgentTurn, error) {
	if e == nil || !e.askInPlace.open {
		return AgentTurn{}, errors.New("editor: ask-in-place composer is not open")
	}
	if index < 0 || index >= len(e.askInPlace.suggestions) {
		return AgentTurn{}, fmt.Errorf("editor: suggestion index %d out of range", index)
	}
	sug := e.askInPlace.suggestions[index]
	prompt := strings.TrimSpace(sug.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(sug.Label)
	}
	e.askInPlace.draft = prompt
	e.askInPlace.selectedSuggestion = -1
	return e.submitAgentTurn(prompt, "in_place", true, e.askInPlace.targetIDs)
}

// SubmitAskInPlace sends the active in-place composer request (or the currently arrow-highlighted suggestion),
// attaching selection IDs, open scene, and rendered viewport PNG context, flagging SkipPlan=true per Plan.dc.html,
// recording the turn in the Agent panel, and closing the in-place composer.
func (e *Editor) SubmitAskInPlace() (AgentTurn, error) {
	if e == nil || !e.askInPlace.open {
		return AgentTurn{}, errors.New("editor: ask-in-place composer is not open")
	}
	if idx := e.askInPlace.selectedSuggestion; idx >= 0 && idx < len(e.askInPlace.suggestions) {
		return e.PickAskInPlaceSuggestion(idx)
	}
	prompt := strings.TrimSpace(e.askInPlace.draft)
	if prompt == "" {
		return AgentTurn{}, errors.New("editor: enter a request for the agent")
	}
	return e.submitAgentTurn(prompt, "in_place", true, e.askInPlace.targetIDs)
}

func (e *Editor) submitAgentTurn(prompt, source string, skipPlan bool, targetIDs []string) (AgentTurn, error) {
	if e == nil || e.Doc == nil {
		return AgentTurn{}, errors.New("editor: no scene open")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return AgentTurn{}, errors.New("editor: prompt must not be empty")
	}

	selIDs := make([]string, 0, len(targetIDs))
	selNames := make([]string, 0, len(targetIDs))
	layerName := e.ActiveLayer
	for i, id := range targetIDs {
		obj, lname, _, _ := e.resolveContextObject(id)
		if obj == nil {
			continue
		}
		selIDs = append(selIDs, obj.ID)
		selNames = append(selNames, contextObjectDisplayName(obj))
		if i == 0 && lname != "" {
			layerName = lname
		}
	}

	scenePath := e.effectiveScenePath()
	sceneName := e.Doc.Name
	if sceneName == "" {
		sceneName = e.T("scene.untitledScene")
	}
	var sceneYAML string
	if data, err := e.SaveYAML(); err == nil {
		sceneYAML = string(data)
	}

	pngBytes, pw, ph, _ := e.RenderViewportPNG(1)

	now := e.nowTime()
	e.agent.turnSeq++
	e.agent.turnChangeSeq++
	turn := AgentTurn{
		ID:        fmt.Sprintf("turn_%d", e.agent.turnSeq),
		Prompt:    prompt,
		Source:    source,
		SkipPlan:  skipPlan,
		Status:    "pending",
		CreatedAt: now,
		StartedAt: now,
		Context: AgentTurnContext{
			ScenePath:      scenePath,
			SceneName:      sceneName,
			SceneYAML:      sceneYAML,
			SelectionIDs:   selIDs,
			SelectionNames: selNames,
			Layer:          layerName,
			ViewportPNG:    pngBytes,
			ViewportWidth:  pw,
			ViewportHeight: ph,
			ViewMode:       e.ViewMode.String(),
		},
	}

	e.agent.turns = append(e.agent.turns, cloneAgentTurn(turn))
	e.trimAgentTurnAttachments()
	e.askInPlace.open = false
	e.askInPlace.draft = ""
	e.askInPlace.selectedSuggestion = -1
	e.askInPlace.returnFocus = nil
	e.askInPlace.seq++
	if e.agent.mode != AgentModeNone {
		e.agent.rightTab = RightTabAgent
	}
	e.Status = e.T("askInPlace.statusSent", l10n.Param("prompt", prompt))
	if e.OnAgentTurn != nil {
		e.OnAgentTurn(cloneAgentTurn(turn))
	}
	e.notify()
	return cloneAgentTurn(turn), nil
}

func (e *Editor) effectiveScenePath() string {
	if e == nil {
		return ""
	}
	if e.ScenePath != "" {
		return e.ScenePath
	}
	if e.FilePath != "" {
		if e.ProjectPath != "" {
			root := filepath.Dir(e.ProjectPath)
			if rel, err := filepath.Rel(root, e.FilePath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(e.FilePath)
	}
	return ""
}

// RenderViewportPNG renders the editor's current viewport (2D or 3D) into a PNG image at scale pixels per point,
// clamping non-finite scales and oversized pixel dimensions (max 1280 px on the longest edge),
// and returning the encoded PNG bytes and its pixel dimensions.
func (e *Editor) RenderViewportPNG(scale float64) ([]byte, int, int, error) {
	if e == nil || e.Doc == nil {
		return nil, 0, 0, errors.New("editor: no scene open")
	}
	if math.IsNaN(scale) || math.IsInf(scale, 0) || scale <= 0 {
		scale = 1
	}
	if scale > 4 {
		scale = 4
	}
	w, h := e.CanvasSize()
	if !isFiniteFloat32(w) || !isFiniteFloat32(h) || w <= 0 || h <= 0 {
		w, h = e.ViewportSize()
	}
	if !isFiniteFloat32(w) || !isFiniteFloat32(h) || w <= 0 || h <= 0 {
		w, h = 640, 360
	}
	effScale := scale
	maxDim := max(float64(w)*effScale, float64(h)*effScale)
	if maxDim > maxViewportPNGDim {
		effScale *= float64(maxViewportPNGDim) / maxDim
	}
	var d core.Drawing
	d.Scale(float32(effScale), float32(effScale))
	e.DrawViewport(&d, core.Size{Width: core.Points(w), Height: core.Points(h)})
	pw := max(1, min(maxViewportPNGDim, int(math.Round(float64(w)*effScale))))
	ph := max(1, min(maxViewportPNGDim, int(math.Round(float64(h)*effScale))))
	r, err := gamemcp.RenderPNG(d.Ops, pw, ph, 0)
	if err != nil {
		return nil, 0, 0, err
	}
	return r.PNG, r.Width, r.Height, nil
}

func (e *Editor) buildAskInPlaceSuggestions(ids []string) []AskInPlaceSuggestion {
	if len(ids) == 0 {
		return []AskInPlaceSuggestion{
			{
				ID:     "scene-check-layout",
				Label:  e.T("askInPlace.suggest.sceneLayout"),
				Prompt: e.T("askInPlace.suggest.sceneLayout"),
			},
			{
				ID:     "scene-populate",
				Label:  e.T("askInPlace.suggest.scenePopulate"),
				Prompt: e.T("askInPlace.suggest.scenePopulate"),
			},
		}
	}
	if len(ids) > 1 {
		return []AskInPlaceSuggestion{
			{
				ID:     "multi-prefab-variant",
				Label:  e.T("askInPlace.suggest.prefabVariant"),
				Prompt: e.T("askInPlace.suggest.prefabVariant"),
			},
			{
				ID:     "multi-space-evenly",
				Label:  e.T("askInPlace.suggest.spaceEvenly"),
				Prompt: e.T("askInPlace.suggest.spaceEvenly"),
			},
		}
	}
	obj, _, _, _ := e.resolveContextObject(ids[0])
	if obj != nil {
		if obj.Light != nil || (obj.Components != nil && obj.Components["light2d"] != nil) {
			return []AskInPlaceSuggestion{
				{
					ID:     "light-soften",
					Label:  e.T("askInPlace.suggest.softenLight"),
					Prompt: e.T("askInPlace.suggest.softenLight"),
				},
				{
					ID:     "light-flicker",
					Label:  e.T("askInPlace.suggest.flickerLight"),
					Prompt: e.T("askInPlace.suggest.flickerLight"),
				},
			}
		}
		if obj.Camera != nil {
			return []AskInPlaceSuggestion{
				{
					ID:     "camera-fit-bounds",
					Label:  e.T("askInPlace.suggest.fitCamera"),
					Prompt: e.T("askInPlace.suggest.fitCamera"),
				},
				{
					ID:     "camera-smooth-follow",
					Label:  e.T("askInPlace.suggest.smoothFollow"),
					Prompt: e.T("askInPlace.suggest.smoothFollow"),
				},
			}
		}
	}
	return []AskInPlaceSuggestion{
		{
			ID:     "single-patrol",
			Label:  e.T("askInPlace.suggest.addPatrol"),
			Prompt: e.T("askInPlace.suggest.addPatrol"),
		},
		{
			ID:     "single-prefab-variant",
			Label:  e.T("askInPlace.suggest.singlePrefabVariant"),
			Prompt: e.T("askInPlace.suggest.singlePrefabVariant"),
		},
	}
}

// SelectionViewportBounds computes the union axis-aligned bounding box [minX, minY, maxX, maxY]
// in viewport canvas points for ids (defaulting to the current selection).
func (e *Editor) SelectionViewportBounds(ids ...string) ([4]float32, bool) {
	if e == nil {
		return [4]float32{}, false
	}
	if len(ids) == 0 {
		ids = e.SelectedIDs()
	}
	if len(ids) == 0 {
		return [4]float32{}, false
	}
	if e.ViewMode == ViewMode3D {
		cw, ch := e.CanvasSize()
		if cw <= 0 || ch <= 0 {
			cw, ch = e.ViewportSize()
		}
		proj := e.Projection3D(cw, ch)
		hasAny := false
		var minX, minY, maxX, maxY float32
		for _, id := range ids {
			obj, _, _, _ := e.resolveContextObject(id)
			if obj == nil {
				continue
			}
			sx, sy, _, vis := proj.Project(math3d.Vec3{X: obj.X, Y: obj.Y, Z: obj.PosZ})
			if !vis || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
				continue
			}
			const pad float32 = 24
			if !hasAny {
				minX, maxX = sx-pad, sx+pad
				minY, maxY = sy-pad, sy+pad
				hasAny = true
			} else {
				minX, maxX = min(minX, sx-pad), max(maxX, sx+pad)
				minY, maxY = min(minY, sy-pad), max(maxY, sy+pad)
			}
		}
		return [4]float32{minX, minY, maxX, maxY}, hasAny
	}

	scene := e.ActiveScene()
	if scene == nil {
		return [4]float32{}, false
	}
	hasAny := false
	var minX, minY, maxX, maxY float32
	for _, id := range ids {
		bo := scene.Object(id)
		if bo == nil || bo.Doc == nil {
			continue
		}
		space := sceneformat.SpaceWorld
		var px, py float32 = 1, 1
		if layer := scene.Layer(bo.Layer); layer != nil {
			if !layer.Visible {
				continue
			}
			space = layer.Space
			px, py = layer.ParallaxX, layer.ParallaxY
		}
		m := e.layerAffineFor(space, px, py).Mul(bo.World)
		rx, ry, rw, rh := objectOutline(bo)
		corners := [4]math2d.Vec2{
			m.TransformPoint(math2d.Vec2{X: rx, Y: ry}),
			m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry}),
			m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry + rh}),
			m.TransformPoint(math2d.Vec2{X: rx, Y: ry + rh}),
		}
		for _, c := range corners {
			if !isFiniteFloat32(c.X) || !isFiniteFloat32(c.Y) {
				continue
			}
			if !hasAny {
				minX, maxX = c.X, c.X
				minY, maxY = c.Y, c.Y
				hasAny = true
			} else {
				minX, maxX = min(minX, c.X), max(maxX, c.X)
				minY, maxY = min(minY, c.Y), max(maxY, c.Y)
			}
		}
	}
	return [4]float32{minX, minY, maxX, maxY}, hasAny
}

// ComputeAskInPlacePlacement anchors a cardW x cardH composer card to selBounds inside a vw x vh viewport,
// flipping above the selection if there is not enough room below, and clamping strictly inside
// [margin, vw-margin] x [margin, vh-margin] so it stays on screen and never overlaps the right panel.
func ComputeAskInPlacePlacement(vw, vh float32, selBounds [4]float32, hasSel bool, cardW, cardH float32) AskInPlacePlacement {
	if !isFiniteFloat32(vw) || vw <= 0 {
		vw = 640
	}
	if !isFiniteFloat32(vh) || vh <= 0 {
		vh = 360
	}
	if hasSel {
		for _, v := range selBounds {
			if !isFiniteFloat32(v) {
				hasSel = false
				selBounds = [4]float32{}
				break
			}
		}
	}
	margin := askInPlaceMargin
	if vw < 2*margin+askInPlaceMinWidth {
		margin = max(4, (vw-askInPlaceMinWidth)/2)
	}
	if !isFiniteFloat32(cardW) || cardW <= 0 {
		cardW = askInPlaceDefaultWidth
	}
	maxW := max(120, vw-2*margin)
	if cardW > maxW {
		cardW = maxW
	}
	if !isFiniteFloat32(cardH) || cardH <= 0 {
		cardH = askInPlaceDefaultHeight
	}
	// Reserve room at the bottom for the viewport overlay bar and hint pill when possible.
	bottomLimit := vh - margin - 86
	if bottomLimit < margin+cardH {
		bottomLimit = vh - margin
	}

	var left, top, anchorX, anchorY float32
	if hasSel {
		minX, minY, _, maxY := selBounds[0], selBounds[1], selBounds[2], selBounds[3]
		anchorX = minX
		anchorY = maxY
		left = minX
		// Prefer placing directly below the selection (matching AskInPlace.dc.html); flip above if clipped.
		top = maxY + askInPlaceAnchorGap
		if top+cardH > bottomLimit {
			above := minY - cardH - askInPlaceAnchorGap - 22
			if above >= margin {
				top = above
			} else {
				top = bottomLimit - cardH
			}
		}
	} else {
		// No selection: anchor centered in the upper viewport to ask about the open scene.
		anchorX = vw / 2
		anchorY = max(margin, vh*0.24)
		left = (vw - cardW) / 2
		top = anchorY
	}

	maxLeft := max(margin, vw-cardW-margin)
	if left < margin {
		left = margin
	}
	if left > maxLeft {
		left = maxLeft
	}
	maxTop := max(margin, bottomLimit-cardH)
	if top < margin {
		top = margin
	}
	if top > maxTop {
		top = maxTop
	}

	return AskInPlacePlacement{
		Left:               float32(math.Round(float64(left))),
		Top:                float32(math.Round(float64(top))),
		Width:              float32(math.Round(float64(cardW))),
		Height:             float32(math.Round(float64(cardH))),
		AnchorX:            anchorX,
		AnchorY:            anchorY,
		SelectionBounds:    selBounds,
		HasSelectionBounds: hasSel,
	}
}

// AskInPlacePlacement computes the current clamped placement of the in-place composer inside the viewport.
func (e *Editor) AskInPlacePlacement() AskInPlacePlacement {
	if e == nil {
		return AskInPlacePlacement{}
	}
	vw, vh := e.CanvasSize()
	if vw <= 0 || vh <= 0 {
		vw, vh = e.ViewportSize()
	}
	ids := e.askInPlace.targetIDs
	if !e.askInPlace.open {
		ids = e.SelectedIDs()
	}
	bounds, hasSel := e.SelectionViewportBounds(ids...)
	cardW := askInPlaceDefaultWidth
	if e.askInPlace.measuredW > 0 {
		cardW = e.askInPlace.measuredW
	}
	cardH := askInPlaceDefaultHeight
	if e.askInPlace.measuredH > 0 {
		cardH = e.askInPlace.measuredH
	}
	return ComputeAskInPlacePlacement(vw, vh, bounds, hasSel, cardW, cardH)
}

func (e *Editor) setAskInPlaceMeasuredSize(w, h float32) {
	if e == nil || !e.askInPlace.open || w <= 0 || h <= 0 {
		return
	}
	if math.Abs(float64(e.askInPlace.measuredW-w)) < 1 && math.Abs(float64(e.askInPlace.measuredH-h)) < 1 {
		return
	}
	e.askInPlace.measuredW = w
	e.askInPlace.measuredH = h
	e.notify()
}

// AskInPlaceSnapshot returns the full view-model snapshot for the anchored ⌘L in-place composer.
func (e *Editor) AskInPlaceSnapshot() AskInPlaceSnapshot {
	if e == nil {
		return AskInPlaceSnapshot{}
	}
	sceneLabel := e.T("scene.untitledScene")
	if e.Doc != nil && e.Doc.Name != "" {
		sceneLabel = e.Doc.Name
	} else if sp := e.effectiveScenePath(); sp != "" {
		sceneLabel = filepath.Base(sp)
	}
	ids := slices.Clone(e.askInPlace.targetIDs)
	names := slices.Clone(e.askInPlace.targetNames)

	var chipSummary, chipAnnouncement, inputLabel, placeholder string
	switch len(ids) {
	case 0:
		chipSummary = e.T("askInPlace.sceneChip", l10n.Param("scene", sceneLabel))
		chipAnnouncement = e.T("askInPlace.announceScene", l10n.Param("scene", sceneLabel))
		inputLabel = e.T("askInPlace.inputLabelScene", l10n.Param("scene", sceneLabel))
		placeholder = e.T("askInPlace.placeholderScene", l10n.Param("scene", sceneLabel))
	case 1:
		chipSummary = e.T("askInPlace.selectedOneChip", l10n.Param("name", names[0]))
		chipAnnouncement = e.T("askInPlace.announceSelection", l10n.Param("count", "1"), l10n.Param("names", names[0]))
		inputLabel = e.T("askInPlace.inputLabelSelection")
		placeholder = e.T("askInPlace.placeholderSelection")
	default:
		joined := strings.Join(names, ", ")
		chipSummary = e.T("askInPlace.selectedManyChip", l10n.Param("count", fmt.Sprintf("%d", len(ids))), l10n.Param("names", joined))
		chipAnnouncement = e.T("askInPlace.announceSelection", l10n.Param("count", fmt.Sprintf("%d", len(ids))), l10n.Param("names", joined))
		inputLabel = e.T("askInPlace.inputLabelSelection")
		placeholder = e.T("askInPlace.placeholderSelection")
	}

	return AskInPlaceSnapshot{
		Open:               e.askInPlace.open,
		Draft:              e.askInPlace.draft,
		TargetIDs:          ids,
		TargetNames:        names,
		SceneLabel:         sceneLabel,
		ChipSummary:        chipSummary,
		ChipAnnouncement:   chipAnnouncement,
		InputLabel:         inputLabel,
		Placeholder:        placeholder,
		Suggestions:        slices.Clone(e.askInPlace.suggestions),
		SelectedSuggestion: e.askInPlace.selectedSuggestion,
		Placement:          e.AskInPlacePlacement(),
	}
}
