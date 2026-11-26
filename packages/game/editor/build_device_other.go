//go:build (!darwin && !linux && !freebsd && !openbsd && !netbsd && !windows) || android || ios

package editor

import (
	"context"
	"fmt"
	"runtime"
)

func defaultOpenBrowserURL(string) error {
	return fmt.Errorf("editor: opening a browser is not supported on %s", runtime.GOOS)
}

func defaultDiscoverConnectedDevices(context.Context, string) ([]ConnectedDevice, error) {
	return nil, nil
}

func defaultDeviceCommandRunner(context.Context, DeviceCommandRequest, interface{ Write([]byte) (int, error) }, interface{ Write([]byte) (int, error) }) error {
	return fmt.Errorf("editor: USB device commands are not supported on %s", runtime.GOOS)
}
