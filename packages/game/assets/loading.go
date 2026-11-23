package assets

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// LoadingScreenProps configures LoadingScreen and LoadingView.
type LoadingScreenProps struct {
	// Manager holds the asset group to load.
	Manager *Manager
	// Group is the name of the asset group to load on mount.
	Group string
	// Title is shown above the progress bar in the default view; "Loading" when
	// empty.
	Title string
	// OnReady runs on the engine goroutine once the group finishes loading
	// without error.
	OnReady func()
	// OnError runs on the engine goroutine if loading the group fails.
	OnError func(error)
	// Render replaces the default loading view when non-nil. It re-renders
	// reactively as the group's progress or error changes.
	Render func(progress float64, err error) core.Node
	// Style merges onto the default outer container view.
	Style core.Style
	// TestID sets the default outer container's TestID.
	TestID string
}

// LoadingScreen is a core.Node component that starts loading a named asset
// group on mount, shows reactive loading progress, and calls OnReady or OnError
// once loading finishes. Set View to replace the default progress UI.
type LoadingScreen struct {
	Manager *Manager
	Group   string
	Title   string
	OnReady func()
	OnError func(error)
	// View replaces the default loading view when non-nil, re-rendering
	// reactively as progress or error changes.
	View   func(progress float64, err error) core.Node
	Style  core.Style
	TestID string
}

// Render implements core.Node.
func (s LoadingScreen) Render(cx *core.Scope) core.Node {
	return LoadingView(cx, LoadingScreenProps{
		Manager: s.Manager,
		Group:   s.Group,
		Title:   s.Title,
		OnReady: s.OnReady,
		OnError: s.OnError,
		Render:  s.View,
		Style:   s.Style,
		TestID:  s.TestID,
	})
}

// LoadingView mounts a loading screen for props.Group on cx. It starts loading
// the named group on mount, displays reactive progress (0%..100% with a
// progress bar, or props.Render when set), and invokes props.OnReady or
// props.OnError once loading finishes while cx is still mounted.
//
// The once is per mount: a Render runs once under its scope, so OnReady fires again only when the view is mounted again, for example by a parent Dynamic that rebuilds it, and then immediately if the group is already loaded. Mounting the view for a group whose last load failed retries that load. Keep the view out of reactive branches that rebuild it, or make OnReady idempotent.
func LoadingView(cx *core.Scope, props LoadingScreenProps) core.Node {
	if props.Manager == nil {
		return core.View{TestID: props.TestID, Style: props.Style}
	}
	g := props.Manager.Group(props.Group)

	mounted := true
	cx.Cleanup(func() { mounted = false })
	notified := false
	inRender := true
	defer func() { inRender = false }()

	finish := func(err error) {
		if !mounted || notified {
			return
		}
		notified = true
		if err != nil {
			if props.OnError != nil {
				props.OnError(err)
			}
			return
		}
		if props.OnReady != nil {
			props.OnReady()
		}
	}

	g.Load(func(err error) {
		if inRender {
			cx.Post(func() { finish(err) })
			return
		}
		finish(err)
	})

	if props.Render != nil {
		return core.Dynamic(func(_ *core.Scope) core.Node {
			return props.Render(g.Progress(), g.Err())
		})
	}

	title := props.Title
	if title == "" {
		title = "Loading"
	}
	containerStyle := core.Style{
		FlexGrow:       1,
		Width:          core.Pct(100),
		Height:         core.Pct(100),
		JustifyContent: core.JustifyCenter,
		AlignItems:     core.AlignCenter,
		Gap:            12,
		Padding:        core.All(24),
	}.Merge(props.Style)

	return core.View{
		TestID: props.TestID,
		Style:  containerStyle,
		Children: []core.Node{
			core.Text{
				Content: title,
				Style:   core.Style{FontSize: 18},
			},
			ui.Progress{
				Value: func() float32 { return float32(g.Progress() * 100) },
				Label: title,
				Style: core.Style{Width: core.Pt(240)},
			},
			core.Text{
				Role: core.RoleStatus,
				ContentFunc: func() string {
					if err := g.Err(); err != nil {
						return err.Error()
					}
					pct := int(math.Round(g.Progress() * 100))
					return fmt.Sprintf("%d%%", min(max(pct, 0), 100))
				},
				Style: core.Style{FontSize: 14},
			},
		},
	}
}
