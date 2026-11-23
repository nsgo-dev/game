package achievements

import (
	"errors"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gameservices"
)

// ServiceFor returns the platform game service NSGo's gameservices package reaches on os: GameCenter on macOS and iOS (which includes tvOS), PlayGames on Android, and "" elsewhere.
func ServiceFor(os core.OS) Service {
	switch os {
	case core.OSMacOS, core.OSIOS:
		return GameCenter
	case core.OSAndroid:
		return PlayGames
	default:
		return ""
	}
}

// GameServices returns a Reporter that reports through gameservices.ReportAchievement on cx, so its answers arrive on cx's engine goroutine.
func GameServices(cx *core.Scope) Reporter {
	return ReporterFunc(func(id string, percent float64, done func(error)) {
		gameservices.ReportAchievement(cx, id, percent, done)
	})
}

// SignIn signs the local player into the running platform's game service with gameservices.Authenticate and, once the player is signed in, connects t to that service through GameServices(cx), which reports all pending progress. done, which may be nil, receives the player or the error on cx's engine goroutine: gameservices.ErrUnsupported where the platform has no game service, and gameservices.ErrUnauthenticated when the service answers without a signed-in player. On failure t keeps recording progress, and a later SignIn reports it.
func SignIn(cx *core.Scope, t *Tracker, done func(gameservices.Player, error)) {
	reply := func(p gameservices.Player, err error) {
		if done != nil {
			done(p, err)
		}
	}
	service := ServiceFor(core.Untracked(cx, cx.Env().OS))
	if service == "" || !core.Untracked(cx, cx.Env().CanUseGameServices) {
		cx.Post(func() { reply(gameservices.Player{}, gameservices.ErrUnsupported) })
		return
	}
	gameservices.Authenticate(cx, func(p gameservices.Player, err error) {
		if err == nil && !p.Authenticated {
			err = gameservices.ErrUnauthenticated
		}
		if err == nil {
			// Connect only fails for an unknown service or a nil Reporter, neither of which can happen here.
			_ = t.Connect(service, GameServices(cx))
		}
		reply(p, err)
	})
}

// isUnsupported reports whether err says the platform has no game service, so retrying on this connection cannot succeed.
func isUnsupported(err error) bool {
	return errors.Is(err, gameservices.ErrUnsupported)
}
