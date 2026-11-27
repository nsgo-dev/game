package editor_test

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func collectMenuItemsByPath(menus []desktop.MenuItem) map[string]desktop.MenuItem {
	out := map[string]desktop.MenuItem{}
	var walk func(prefix string, items []desktop.MenuItem)
	walk = func(prefix string, items []desktop.MenuItem) {
		for _, it := range items {
			if it.Separator {
				continue
			}
			path := it.Label
			if prefix != "" {
				path = prefix + " > " + it.Label
			}
			if len(it.Items) > 0 {
				walk(path, it.Items)
				continue
			}
			out[path] = it
		}
	}
	walk("", menus)
	return out
}

// #1611: every control formerly stranded in dead toolbarView is registered as a command,
// works through ExecuteCommand, appears in Command Palette and desktop menus, and is localized in EN and ZH.
func TestUnreachableControlsRegisteredAsCommandsAndMenus(t *testing.T) {
	ed := newTestEditor(t)

	requiredIDs := []string{
		editor.CmdGridMapPaint,
		editor.CmdGridMapErase,
		editor.CmdGridMapPick,
		editor.CmdGridMapBoxFill,
		editor.CmdToggleLODOverlay,
		editor.CmdToggleOccluders,
		editor.CmdToggleRenderStats,
		editor.CmdTogglePlaceSnap3D,
		editor.CmdTogglePlaceAlign3D,
		editor.CmdToolPickTile,
		editor.CmdToggleSnapGrid,
		editor.CmdSnapSettings,
	}

	// Verify localization in both English and Chinese and presence in Command Palette.
	for _, lang := range []editor.Language{editor.LanguageEnglish, editor.LanguageChinese} {
		if err := ed.SetLanguage(lang); err != nil {
			t.Fatalf("SetLanguage(%v): %v", lang, err)
		}
		cmds := make(map[string]editor.EditorCommand)
		for _, c := range ed.Commands() {
			cmds[c.ID] = c
		}
		palette := make(map[string]bool)
		for _, it := range ed.PaletteItems() {
			palette[it.ID] = true
		}
		for _, id := range requiredIDs {
			c, ok := cmds[id]
			if !ok {
				t.Fatalf("lang=%v: command %q missing from Commands()", lang, id)
			}
			if c.Label == "" || strings.HasPrefix(c.Label, "cmd.") {
				t.Fatalf("lang=%v: command %q has unlocalized label %q", lang, id, c.Label)
			}
			if c.Description == "" || strings.HasPrefix(c.Description, "cmd.") {
				t.Fatalf("lang=%v: command %q has unlocalized description %q", lang, id, c.Description)
			}
			if !palette["cmd:"+id] {
				t.Fatalf("lang=%v: command %q missing from PaletteItems", lang, id)
			}
		}
	}
	_ = ed.SetLanguage(editor.LanguageEnglish)

	// Verify presence in desktop menus under View > Tools, View > Overlays, and View.
	menuByPath := collectMenuItemsByPath(ed.ApplicationMenu())
	wantMenuPaths := []string{
		"View > Tools > Pick Tile",
		"View > Tools > GridMap: Paint",
		"View > Tools > GridMap: Erase",
		"View > Tools > GridMap: Pick",
		"View > Tools > GridMap: Box Fill",
		"View > Tools > 3D Place: Snap to Grid",
		"View > Tools > 3D Place: Align to Surface",
		"View > Overlays > Show 3D LOD Overlay",
		"View > Overlays > Show 3D Occluders",
		"View > Overlays > Show 3D Render Stats",
		"View > Snap to Grid",
		"View > Snap Settings...",
	}
	for _, p := range wantMenuPaths {
		if _, ok := menuByPath[p]; !ok {
			t.Fatalf("desktop menu missing %q", p)
		}
	}

	// Drive GridMap commands via ExecuteCommand: switches to 3D, activates each tool, toggles off on second call.
	ed.SetViewMode(editor.ViewMode2D)
	for _, tc := range []struct {
		cmd  string
		want editor.GridMapTool
	}{
		{editor.CmdGridMapPaint, editor.GridMapToolPaint},
		{editor.CmdGridMapErase, editor.GridMapToolErase},
		{editor.CmdGridMapPick, editor.GridMapToolPick},
		{editor.CmdGridMapBoxFill, editor.GridMapToolBoxFill},
	} {
		if err := ed.ExecuteCommand(tc.cmd); err != nil {
			t.Fatalf("ExecuteCommand(%q): %v", tc.cmd, err)
		}
		if ed.ViewMode != editor.ViewMode3D {
			t.Fatalf("ExecuteCommand(%q) ViewMode = %v, want ViewMode3D", tc.cmd, ed.ViewMode)
		}
		if ed.GridMapTool() != tc.want {
			t.Fatalf("ExecuteCommand(%q) GridMapTool = %v, want %v", tc.cmd, ed.GridMapTool(), tc.want)
		}
		// Running the active GridMap tool command again toggles back to GridMapToolNone.
		if err := ed.ExecuteCommand(tc.cmd); err != nil {
			t.Fatalf("second ExecuteCommand(%q): %v", tc.cmd, err)
		}
		if ed.GridMapTool() != editor.GridMapToolNone {
			t.Fatalf("second ExecuteCommand(%q) GridMapTool = %v, want GridMapToolNone", tc.cmd, ed.GridMapTool())
		}
	}

	// Selecting a normal tool (even if ed.Tool is already ToolSelect) exits GridMap mode.
	if err := ed.ExecuteCommand(editor.CmdGridMapPaint); err != nil {
		t.Fatal(err)
	}
	ed.SetTool(editor.ToolSelect)
	if ed.GridMapTool() != editor.GridMapToolNone {
		t.Fatalf("SetTool(ToolSelect) left GridMapTool = %v, want GridMapToolNone", ed.GridMapTool())
	}

	// 3D overlays via ExecuteCommand.
	for _, tc := range []struct {
		cmd string
		get func() bool
	}{
		{editor.CmdToggleLODOverlay, func() bool { return ed.ShowLODOverlay }},
		{editor.CmdToggleOccluders, func() bool { return ed.ShowOccluders }},
		{editor.CmdToggleRenderStats, func() bool { return ed.ShowRenderStats }},
	} {
		before := tc.get()
		if err := ed.ExecuteCommand(tc.cmd); err != nil {
			t.Fatalf("ExecuteCommand(%q): %v", tc.cmd, err)
		}
		if tc.get() == before {
			t.Fatalf("ExecuteCommand(%q) did not toggle state", tc.cmd)
		}
		if err := ed.ExecuteCommand(tc.cmd); err != nil {
			t.Fatalf("ExecuteCommand(%q) toggle back: %v", tc.cmd, err)
		}
		if tc.get() != before {
			t.Fatalf("ExecuteCommand(%q) did not restore state", tc.cmd)
		}
	}

	// 3D Place Snap & Align via ExecuteCommand.
	for _, tc := range []struct {
		cmd string
		get func() bool
	}{
		{editor.CmdTogglePlaceSnap3D, func() bool { return ed.PlaceSnap3D }},
		{editor.CmdTogglePlaceAlign3D, func() bool { return ed.PlaceAlign3D }},
	} {
		before := tc.get()
		if err := ed.ExecuteCommand(tc.cmd); err != nil {
			t.Fatalf("ExecuteCommand(%q): %v", tc.cmd, err)
		}
		if tc.get() == before {
			t.Fatalf("ExecuteCommand(%q) did not toggle state", tc.cmd)
		}
		if err := ed.ExecuteCommand(tc.cmd); err != nil {
			t.Fatalf("ExecuteCommand(%q) toggle back: %v", tc.cmd, err)
		}
		if tc.get() != before {
			t.Fatalf("ExecuteCommand(%q) did not restore state", tc.cmd)
		}
	}

	// Pick tile tool via ExecuteCommand.
	if err := ed.ExecuteCommand(editor.CmdToolPickTile); err != nil {
		t.Fatalf("ExecuteCommand(CmdToolPickTile): %v", err)
	}
	if ed.Tool != editor.ToolPickTile {
		t.Fatalf("Tool after CmdToolPickTile = %v, want ToolPickTile", ed.Tool)
	}

	// 2D snap grid toggle and Snap Settings popover command.
	snapBefore := ed.SnapToGrid
	if err := ed.ExecuteCommand(editor.CmdToggleSnapGrid); err != nil {
		t.Fatalf("ExecuteCommand(CmdToggleSnapGrid): %v", err)
	}
	if ed.SnapToGrid == snapBefore {
		t.Fatal("CmdToggleSnapGrid did not toggle SnapToGrid")
	}
	if ed.SnapSettingsPopoverOpen() {
		t.Fatal("SnapSettingsPopoverOpen should start false")
	}
	if err := ed.ExecuteCommand(editor.CmdSnapSettings); err != nil {
		t.Fatalf("ExecuteCommand(CmdSnapSettings): %v", err)
	}
	if !ed.SnapSettingsPopoverOpen() {
		t.Fatal("CmdSnapSettings did not open SnapSettingsPopoverOpen")
	}
	ed.SetViewMode(editor.ViewMode2D)
	if ed.SnapSettingsPopoverOpen() {
		t.Fatal("SetViewMode should close SnapSettingsPopoverOpen")
	}
}

// #1611: verify 2D Snap Settings popover, Tile Palette Pick tool, 3D GridMap tools,
// 3D Overlays popover, and contextual 3D Place options bar are reachable in live EditorView UI.
func TestUnreachableControlsLiveViewportChromeUI(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetActiveLayer("world")
	app := mountEditor(t, ed)

	// 1. 2D viewport bar: Snap Settings popover opens via button and via CmdSnapSettings,
	// edits GridSize & resets with Auto, and toggles Scale snap.
	if app.Find("id:view-snap-settings") == nil {
		t.Fatal("2D view-snap-settings trigger not found")
	}
	app.Press("id:view-snap-settings")
	app.Settle()
	if !ed.SnapSettingsPopoverOpen() {
		t.Fatal("pressing view-snap-settings did not open popover")
	}
	app.Post(func() { ed.SetGridSize(32) })
	app.Settle()
	if ed.GridSize != 32 {
		t.Fatalf("GridSize = %v, want 32", ed.GridSize)
	}
	app.Press("id:snap-grid-auto")
	app.Settle()
	if ed.GridSize != 0 {
		t.Fatalf("pressing snap-grid-auto in 2D left GridSize = %v, want 0", ed.GridSize)
	}
	app.Press("id:snap-scale-toggle")
	app.Settle()
	if !ed.SnapScale {
		t.Fatal("pressing snap-scale-toggle in 2D did not enable SnapScale")
	}
	// Close popover via command.
	app.Post(func() { _ = ed.ExecuteCommand(editor.CmdSnapSettings) })
	app.Settle()
	if ed.SnapSettingsPopoverOpen() {
		t.Fatal("CmdSnapSettings did not close popover")
	}

	// 2. Tile Palette: Pick tile button activates ToolPickTile.
	app.Post(func() { ed.SetTool(editor.ToolPaintTile) })
	app.Settle()
	if app.Find("id:tool-pick") == nil {
		t.Fatal("Tile Palette tool-pick button not found")
	}
	app.Press("id:tool-pick")
	app.Settle()
	if ed.Tool != editor.ToolPickTile {
		t.Fatalf("pressing tool-pick set Tool = %v, want ToolPickTile", ed.Tool)
	}

	// 3. Switch to 3D: GridMap tool buttons on view3d-bottom-bar toggle GridMap tools,
	// and clicking header Select tool exits GridMap mode.
	app.Press("id:view-mode-3d")
	app.Settle()
	for _, tc := range []struct {
		id   string
		want editor.GridMapTool
	}{
		{"id:gridmap-tool-paint", editor.GridMapToolPaint},
		{"id:gridmap-tool-erase", editor.GridMapToolErase},
		{"id:gridmap-tool-pick", editor.GridMapToolPick},
		{"id:gridmap-tool-boxfill", editor.GridMapToolBoxFill},
	} {
		if app.Find(tc.id) == nil {
			t.Fatalf("3D GridMap button %s not found", tc.id)
		}
		app.Press(tc.id)
		app.Settle()
		if ed.GridMapTool() != tc.want {
			t.Fatalf("pressing %s set GridMapTool = %v, want %v", tc.id, ed.GridMapTool(), tc.want)
		}
	}
	app.Press("id:tool-select")
	app.Settle()
	if ed.GridMapTool() != editor.GridMapToolNone {
		t.Fatalf("pressing header tool-select left GridMapTool = %v, want GridMapToolNone", ed.GridMapTool())
	}

	// 4. 3D Overlays popover toggles LOD, Occluders, Render Stats, and 3D NavMesh off and back on.
	if app.Find("id:view3d-overlays") == nil {
		t.Fatal("3D view3d-overlays popover trigger not found")
	}
	lodBefore, occBefore, statsBefore, navBefore := ed.ShowLODOverlay, ed.ShowOccluders, ed.ShowRenderStats, ed.ShowNavMesh3D
	app.Press("id:view3d-overlays")
	app.Settle()
	app.Press("id:viewport3d-toggle-lod")
	app.Press("id:viewport3d-toggle-occluders")
	app.Press("id:viewport3d-toggle-stats")
	app.Press("id:viewport3d-toggle-navmesh")
	app.Settle()
	if ed.ShowLODOverlay == lodBefore || ed.ShowOccluders == occBefore || ed.ShowRenderStats == statsBefore || ed.ShowNavMesh3D == navBefore {
		t.Fatalf("3D overlay switches did not toggle all flags: lod=%v (was %v) occ=%v (was %v) stats=%v (was %v) nav=%v (was %v)",
			ed.ShowLODOverlay, lodBefore, ed.ShowOccluders, occBefore, ed.ShowRenderStats, statsBefore, ed.ShowNavMesh3D, navBefore)
	}
	app.Press("id:viewport3d-toggle-lod")
	app.Press("id:viewport3d-toggle-occluders")
	app.Press("id:viewport3d-toggle-stats")
	app.Press("id:viewport3d-toggle-navmesh")
	app.Settle()
	if ed.ShowLODOverlay != lodBefore || ed.ShowOccluders != occBefore || ed.ShowRenderStats != statsBefore || ed.ShowNavMesh3D != navBefore {
		t.Fatalf("3D overlay switches did not restore all flags: lod=%v occ=%v stats=%v nav=%v",
			ed.ShowLODOverlay, ed.ShowOccluders, ed.ShowRenderStats, ed.ShowNavMesh3D)
	}

	// 5. Contextual 3D Place bar appears only in ToolPlace and updates Snap and Align.
	if app.Exists("id:view3d-place-bar") {
		t.Fatal("view3d-place-bar should be hidden while ToolSelect is active")
	}
	app.Post(func() { ed.SetTool(editor.ToolPlace) })
	app.Settle()
	if app.Find("id:view3d-place-bar") == nil {
		t.Fatal("view3d-place-bar did not appear when ToolPlace became active in 3D")
	}
	if app.Find("id:place3d-height") == nil {
		t.Fatal("place3d-height input missing from view3d-place-bar")
	}
	snapBefore := ed.PlaceSnap3D
	alignBefore := ed.PlaceAlign3D
	app.Press("id:place3d-snap")
	app.Press("id:place3d-align")
	app.Settle()
	if ed.PlaceSnap3D == snapBefore || ed.PlaceAlign3D == alignBefore {
		t.Fatalf("3D Place bar toggles did not update options: snap=%v (was %v) align=%v (was %v)",
			ed.PlaceSnap3D, snapBefore, ed.PlaceAlign3D, alignBefore)
	}
}

// #1611: adding view-snap-settings to 2D bar and overlays/GridMap/Place to 3D chrome must not
// wrap the bottom pills or cause overlaps with toast notifications or the 3D orientation widget.
func TestViewportChromeNoOverlapAtCompactAndDesktopSizes(t *testing.T) {
	for _, sz := range []struct {
		name string
		w, h core.Points
	}{
		{"compact-1120x680", 1120, 680},
		{"desktop-1440x900", 1440, 900},
	} {
		t.Run(sz.name, func(t *testing.T) {
			doc := &sceneformat.SceneDoc{
				Version:       sceneformat.CurrentVersion,
				Name:          "layout_check",
				VirtualWidth:  640,
				VirtualHeight: 360,
				Layers: []sceneformat.LayerDoc{
					{Name: "world", Space: sceneformat.SpaceWorld},
				},
			}
			ed, err := editor.New(doc, nil)
			if err != nil {
				t.Fatal(err)
			}
			ed.ShowToast(editor.Toast{ID: "info", Kind: editor.ToastInfo, Message: "Toast check"})
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return editor.EditorView(cx, ed)
			}, nsgotest.Size(sz.w, sz.h), nsgotest.Environment(core.Environment{
				OS:     core.OSMacOS,
				Idiom:  core.IdiomDesktop,
				Dark:   true,
				Locale: "en-US",
			}))
			app.Settle()

			bar2D := app.Find("id:viewport-overlay-bar").Bounds()
			toast2D := app.Find("id:editor-toast").Bounds()
			if bar2D.Size.Height > 44 {
				t.Fatalf("2D viewport-overlay-bar wrapped to height %v", bar2D.Size.Height)
			}
			if toast2D.Origin.Y+toast2D.Size.Height > bar2D.Origin.Y {
				t.Fatalf("2D toast %+v overlaps viewport-overlay-bar %+v", toast2D, bar2D)
			}

			app.Post(func() { ed.SetViewMode(editor.ViewMode3D) })
			app.Settle()

			if sz.w >= 1440 {
				topBar3D := app.Find("id:view3d-bar").Bounds()
				axes3D := app.Find("id:view3d-axes-panel").Bounds()
				if topBar3D.Origin.X+topBar3D.Size.Width > axes3D.Origin.X {
					t.Fatalf("3D view3d-bar %+v overlaps view3d-axes-panel %+v", topBar3D, axes3D)
				}
			}
			vp3D := app.Find("id:editor-viewport-container").Bounds()
			bottomBar3D := app.Find("id:view3d-bottom-bar").Bounds()
			toast3D := app.Find("id:editor-toast").Bounds()
			if bottomBar3D.Origin.X < vp3D.Origin.X || bottomBar3D.Origin.X+bottomBar3D.Size.Width > vp3D.Origin.X+vp3D.Size.Width {
				t.Fatalf("3D view3d-bottom-bar %+v overflows viewport container %+v", bottomBar3D, vp3D)
			}
			if math.Abs(float64(bottomBar3D.Size.Height-38)) > 6 {
				t.Fatalf("3D view3d-bottom-bar height = %v, want single-row ~38pt", bottomBar3D.Size.Height)
			}
			if toast3D.Origin.Y+toast3D.Size.Height > bottomBar3D.Origin.Y {
				t.Fatalf("3D toast %+v overlaps view3d-bottom-bar %+v", toast3D, bottomBar3D)
			}
		})
	}
}
