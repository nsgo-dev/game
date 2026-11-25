//go:build (!darwin && !linux && !freebsd && !openbsd && !netbsd && !windows) || android || ios

package editor

import "io"

const canRunGameDesktop = false

func startGameProcess(RunGameSpec, io.Writer, io.Writer) (GameProcess, error) {
	return nil, ErrRunGameUnsupported
}
