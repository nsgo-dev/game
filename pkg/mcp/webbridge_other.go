//go:build !(js && wasm)

package mcp

import "github.com/nsgo-dev/nsgo/core"

func (b *Bridge) attachWebBridge(*core.Scope) func() {
	return func() {}
}
