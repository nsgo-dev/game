//go:build ios || android || (!darwin && !linux && !windows)

package steamworks

func openPlatformDriver(Options) (Driver, error) {
	return nil, ErrUnavailable
}
