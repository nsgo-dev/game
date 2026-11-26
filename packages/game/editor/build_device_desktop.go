//go:build (darwin || linux || freebsd || openbsd || netbsd || windows) && !android && !ios

package editor

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func defaultOpenBrowserURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("editor: invalid browser URL %q", rawURL)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u.String())
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u.String())
	case "linux", "freebsd", "openbsd", "netbsd":
		cmd = exec.Command("xdg-open", u.String())
	default:
		return fmt.Errorf("editor: opening a browser is not supported on %s", runtime.GOOS)
	}
	return cmd.Start()
}

func defaultDiscoverConnectedDevices(ctx context.Context, _ string) ([]ConnectedDevice, error) {
	adbPath, err := exec.LookPath("adb")
	if err != nil {
		return nil, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, adbPath, "devices", "-l").Output()
	if err != nil {
		return nil, nil
	}
	return parseADBDevicesOutput(string(out)), nil
}

func defaultDeviceCommandRunner(ctx context.Context, req DeviceCommandRequest, stdout, stderr interface{ Write([]byte) (int, error) }) error {
	cmd := exec.CommandContext(ctx, "go", req.Args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
