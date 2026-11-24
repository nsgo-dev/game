//go:build !(windows || (darwin && !ios) || (linux && !android))

package presence

func openSteamDriver(SteamConfig) (SteamDriver, error) {
	return nil, ErrUnavailable
}
