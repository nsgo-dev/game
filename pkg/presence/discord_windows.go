//go:build windows

package presence

import (
	"context"
	"fmt"
	"io"
	"os"
	"syscall"
)

func dialDiscordIPC(ctx context.Context) (io.ReadWriteCloser, error) {
	for i := range 10 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pipePath := fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
		ptr, err := syscall.UTF16PtrFromString(pipePath)
		if err != nil {
			continue
		}
		handle, err := syscall.CreateFile(
			ptr,
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			0,
			nil,
			syscall.OPEN_EXISTING,
			0,
			0,
		)
		if err == nil {
			return os.NewFile(uintptr(handle), pipePath), nil
		}
	}
	return nil, ErrUnavailable
}
