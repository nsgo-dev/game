//go:build !(windows || (darwin && !ios) || (linux && !android) || freebsd || openbsd || netbsd)

package presence

import (
	"context"
	"io"
)

func dialDiscordIPC(context.Context) (io.ReadWriteCloser, error) {
	return nil, ErrUnavailable
}
