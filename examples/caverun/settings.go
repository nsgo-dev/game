package main

import (
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/quality"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
)

func caveRunControlActions() []gameui.SettingsAction {
	return []gameui.SettingsAction{
		{Name: actionLeft, Label: "Move Left", Defaults: []string{"key:a", "key:left", "gamepad:dpad_left"}},
		{Name: actionRight, Label: "Move Right", Defaults: []string{"key:d", "key:right", "gamepad:dpad_right"}},
		{Name: actionJump, Label: "Jump", Defaults: []string{"key:space", "key:w", "key:up", "key:z", "gamepad:south"}},
		{Name: actionPause, Label: "Pause", Defaults: []string{"key:escape", "key:p", "gamepad:start"}},
		{Name: actionRestart, Label: "Restart", Defaults: []string{"key:r", "gamepad:back"}},
		{Name: actionSwarm, Label: "Fireflies", Defaults: []string{"key:f", "gamepad:north"}},
	}
}

func caveRunDefaultSettings() save.Settings {
	s := save.DefaultSettings()
	s.Display.ScaleMode = gameViewport.Mode.String()
	s.Display.IntegerScale = gameViewport.IntegerScale
	s.Bindings = make(map[string][]string)
	for _, act := range caveRunControlActions() {
		s.Bindings[act.Name] = slices.Clone(act.Defaults)
	}
	gameui.SetSettingsResolution(&s, "960x540")
	return s
}

func newCaveRunCatalog() *l10n.Catalog {
	cat := l10n.NewCatalog("en")
	cat.Set("en", "settings.title", "Settings")
	cat.Set("en", "settings.tab.audio", "Audio")
	cat.Set("en", "settings.tab.video", "Video")
	cat.Set("en", "settings.tab.controls", "Controls")
	cat.Set("en", "settings.tab.language", "Language")
	cat.Set("en", "settings.tab.accessibility", "Accessibility")
	cat.Set("en", "settings.apply", "Apply")
	cat.Set("en", "settings.save", "Save")
	cat.Set("en", "settings.reset", "Reset")
	cat.Set("en", "settings.back", "Back")

	cat.Set("zh-Hans", "settings.title", "设置")
	cat.Set("zh-Hans", "settings.tab.audio", "音频")
	cat.Set("zh-Hans", "settings.tab.video", "视频")
	cat.Set("zh-Hans", "settings.tab.controls", "控制")
	cat.Set("zh-Hans", "settings.tab.language", "语言")
	cat.Set("zh-Hans", "settings.tab.accessibility", "无障碍")
	cat.Set("zh-Hans", "settings.apply", "应用")
	cat.Set("zh-Hans", "settings.save", "保存")
	cat.Set("zh-Hans", "settings.reset", "重置")
	cat.Set("zh-Hans", "settings.back", "返回")
	return cat
}

func (g *caveRun) settingsTextFor(locale string) gameui.SettingsText {
	loc := strings.TrimSpace(locale)
	if loc == "" {
		loc = "en"
	}
	t := gameui.DefaultSettingsText()
	t.Title = g.catalog.T(loc, "settings.title")
	t.TabAudio = g.catalog.T(loc, "settings.tab.audio")
	t.TabVideo = g.catalog.T(loc, "settings.tab.video")
	t.TabControls = g.catalog.T(loc, "settings.tab.controls")
	t.TabLanguage = g.catalog.T(loc, "settings.tab.language")
	t.TabAccessibility = g.catalog.T(loc, "settings.tab.accessibility")
	t.Apply = g.catalog.T(loc, "settings.apply")
	t.Save = g.catalog.T(loc, "settings.save")
	t.Reset = g.catalog.T(loc, "settings.reset")
	t.Back = g.catalog.T(loc, "settings.back")
	return t
}

func parseScaleMode(name string) screen.Mode {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "fill":
		return screen.ModeFill
	case "stretch":
		return screen.ModeStretch
	case "expand":
		return screen.ModeExpand
	default:
		return screen.ModeFit
	}
}

func (g *caveRun) settingsTargets() save.SettingsTargets {
	return save.SettingsTargets{
		Sound: g.mixer,
		ApplyDisplay: func(d save.DisplaySettings) error {
			if g.video != nil {
				_ = g.video.Apply(d)
			}
			if g.quality != nil {
				_ = g.quality.SetSetting(d.Quality)
			}
			g.viewport.Mode = parseScaleMode(d.ScaleMode)
			g.viewport.IntegerScale = d.IntegerScale
			return nil
		},
		ApplyBindings: func(b map[string][]string) error {
			gameui.ApplyBindingsToActions(g.actions, b, true)
			return nil
		},
		ApplyLocale: func(loc string) error {
			g.locale.Set(loc)
			if g.settingsMenu != nil {
				g.settingsMenu.SetText(g.settingsTextFor(loc))
			}
			return nil
		},
		ApplyAccessibility: func(a save.AccessibilitySettings) error {
			g.a11y = a
			g.reduceFlashes = a.ReduceFlashes
			g.syncLevelAccessibility()
			return nil
		},
	}
}

func (g *caveRun) syncLevelAccessibility() {
	if g.level == nil {
		return
	}
	if g.a11y.ReduceMotion {
		g.level.shakeScale = 0
	} else {
		g.level.shakeScale = float32(g.a11y.ScreenShake)
	}
}

func caveRunResolutions() []gameui.SettingsResolution {
	return []gameui.SettingsResolution{
		{ID: "960x540", Label: "960x540", Width: 960, Height: 540},
		{ID: "1280x720", Label: "1280x720", Width: 1280, Height: 720},
		{ID: "1920x1080", Label: "1920x1080", Width: 1920, Height: 1080},
		{ID: "native", Label: "Native"},
	}
}

func (g *caveRun) initSettings(cx *core.Scope) {
	g.catalog = newCaveRunCatalog()
	g.settingsStore = save.NewStorageSettingsStore(cx.Storage(), "caverun")
	winW, winH := gameProject.WindowSize()
	g.video = &screen.Video{
		Loop:             &g.loop,
		NativeWindowSize: core.Size{Width: winW, Height: winH},
		OnWindowChanged: func(d save.DisplaySettings) {
			if g.settingsMenu == nil {
				return
			}
			cur := g.settingsMenu.Settings()
			cur.Display.Fullscreen = d.Fullscreen
			g.settingsMenu.SetSettings(cur)
			g.bumpSettingsTick()
		},
	}
	g.video.Run(cx)

	sel, err := quality.NewSelector(quality.DefaultConfig(), quality.DeviceFromEnv(cx.Env()))
	if err == nil {
		g.quality = sel
	}

	defaults := caveRunDefaultSettings()
	initial := defaults
	if g.settingsStore.Exists() {
		if loaded, err := g.settingsStore.Load(); err == nil {
			initial = loaded
		}
	}
	g.a11y = initial.Accessibility
	g.reduceFlashes = initial.Accessibility.ReduceFlashes
	g.resolution = gameui.SettingsResolutionFrom(initial)

	resolutions := caveRunResolutions()
	g.settingsMenu = gameui.NewSettingsMenu(gameui.SettingsMenuConfig{
		Store:    g.settingsStore,
		Targets:  g.settingsTargets(),
		Video:    g.video,
		Env:      cx.Env(),
		Initial:  &initial,
		Defaults: &defaults,
		AudioBuses: []gameui.SettingsAudioBus{
			{Name: soundGroupMusic, Label: "Music"},
			{Name: soundGroupSFX, Label: "SFX"},
			{Name: "ui", Label: "UI"},
		},
		Resolutions: resolutions,
		Actions:     caveRunControlActions(),
		Locales: []gameui.SettingsLocale{
			{Tag: "", Label: "System Default"},
			{Tag: "en", Label: "English"},
			{Tag: "zh-Hans", Label: "简体中文"},
		},
		Text: g.settingsTextFor(initial.Locale),
		OnApply: func(s save.Settings) error {
			g.resolution = gameui.SettingsResolutionFrom(s)
			g.reduceFlashes = s.Accessibility.ReduceFlashes
			return nil
		},
		OnBack: func() {
			g.closeSettings()
		},
	})

	if g.settingsStore.Exists() {
		_ = save.Apply(initial, g.settingsTargets())
		gameui.ApplySettingsResolution(g.video, initial, resolutions...)
		g.resolution = gameui.SettingsResolutionFrom(initial)
		g.reduceFlashes = initial.Accessibility.ReduceFlashes
	}
}

func (g *caveRun) openSettings() {
	if g.settingsOpen.Get() {
		return
	}
	g.clearHeldInputs()
	if g.settingsMenu != nil {
		g.settingsMenu.SyncGamepad(g.actions.Gamepads())
	}
	g.settingsOpen.Set(true)
	g.bumpSettingsTick()
	g.settingsRef.Focus()
}

func (g *caveRun) closeSettings() {
	if !g.settingsOpen.Get() {
		return
	}
	g.settingsOpen.Set(false)
	g.clearHeldInputs()
	g.gameRef.Focus()
}

func (g *caveRun) bumpSettingsTick() {
	g.settingsTick.Update(func(n int) int { return n + 1 })
}

func (g *caveRun) renderSettingsOverlay(cx *core.Scope) core.Node {
	return core.Show{
		When: g.settingsOpen.Get,
		Then: func(cx *core.Scope) core.Node {
			focusedOnce := false
			return core.View{
				StyleFunc: func() core.Style {
					return core.Style{
						Position:       core.Absolute,
						Top:            core.Pt(0),
						Left:           core.Pt(0),
						Right:          core.Pt(0),
						Bottom:         core.Pt(0),
						Padding:        g.safeInsets(),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Background:     core.RGBA(0x060812, 0.85),
					}
				},
				Children: []core.Node{
					core.View{
						TestID:    "settings-menu",
						Ref:       &g.settingsRef,
						Focusable: func() bool { return true },
						Style: core.Style{
							Width:  core.Pct(100),
							Height: core.Pct(100),
						},
						OnLayout: func(core.Bounds) {
							if !focusedOnce {
								focusedOnce = true
								cx.Post(func() { g.settingsRef.Focus() })
							}
						},
						OnMouseMove: func(ev core.MouseEvent) {
							if g.settingsMenu != nil && g.settingsMenu.OnPointerMove(ev.Position) {
								g.bumpSettingsTick()
							}
						},
						OnPressDown: func(ev core.PressEvent) {
							if g.settingsMenu != nil && g.settingsMenu.OnPointerDown(ev.Position) {
								g.bumpSettingsTick()
							}
						},
						OnPressUp: func(ev core.PressEvent) {
							if g.settingsMenu != nil && g.settingsMenu.OnPointerUp(ev.Position) {
								g.bumpSettingsTick()
							}
						},
						OnTouch: func(ev core.TouchEvent) {
							if g.settingsMenu != nil && g.settingsMenu.OnTouch(ev) {
								g.bumpSettingsTick()
							}
							ev.StopPropagation()
						},
						OnKeyDown: func(ev core.KeyEvent) bool {
							if g.settingsMenu != nil && g.settingsMenu.OnKeyDown(ev) {
								g.bumpSettingsTick()
								return true
							}
							return false
						},
						Children: []core.Node{
							core.Canvas{
								TestID: "settings-canvas",
								Label:  "Settings menu",
								Style: core.Style{
									Width:  core.Pct(100),
									Height: core.Pct(100),
								},
								Draw: func(d *core.Drawing, size core.Size) {
									_ = g.frameTick.Get()
									_ = g.settingsTick.Get()
									if g.settingsMenu != nil {
										g.settingsMenu.Draw(d, core.Bounds{Size: size})
									}
								},
							},
						},
					},
				},
			}
		},
	}
}
