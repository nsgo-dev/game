//go:build !(js && wasm)

package webmcpbridge

// Register is a no-op outside js/wasm web builds.
func Register(Config) (unregister func()) {
	return func() {}
}
