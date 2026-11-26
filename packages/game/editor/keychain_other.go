//go:build ios || android || (!darwin && !linux && !windows)

package editor

type unavailableKeychain struct{}

func platformKeychain() Keychain {
	return unavailableKeychain{}
}

func (unavailableKeychain) Set(string, string, string) error   { return ErrKeychainUnavailable }
func (unavailableKeychain) Get(string, string) (string, error) { return "", ErrKeychainUnavailable }
func (unavailableKeychain) Delete(string, string) error        { return ErrKeychainUnavailable }
