//go:build ios || android || (!darwin && !linux && !windows)

package keychain

type unavailableKeychain struct{}

func platformKeychain() Store {
	return unavailableKeychain{}
}

func (unavailableKeychain) Set(string, string, string) error   { return ErrUnavailable }
func (unavailableKeychain) Get(string, string) (string, error) { return "", ErrUnavailable }
func (unavailableKeychain) Delete(string, string) error        { return ErrUnavailable }
