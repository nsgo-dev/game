//go:build !(js && wasm)

package game

import "github.com/nsgo-dev/nsgo/core"

func (l *Loop) attachWebBridge(*core.Scope) func() {
	return func() {}
}
