//go:build !ios && !android && !js

package editor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

func runFirstRunPlaytestCommand(ctx context.Context, projectDir string) (*playtest.Report, error) {
	outPath := filepath.Join(projectDir, playtest.ReportFileName)
	cmd := exec.CommandContext(ctx, "go", "tool", "game", "playtest",
		"-config", playtest.FileName,
		"-out", playtest.ReportFileName,
	)
	cmd.Dir = projectDir
	cmd.Env = enrichedAgentEnv(os.Environ(), "")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	rep, loadErr := playtest.LoadReportFile(outPath)
	if loadErr == nil && rep != nil {
		return rep, runErr
	}
	if runErr != nil {
		detail := strings.TrimSpace(out.String())
		if detail != "" {
			return nil, fmt.Errorf("game playtest failed: %s", detail)
		}
		return nil, fmt.Errorf("game playtest failed: %w", runErr)
	}
	return nil, fmt.Errorf("game playtest wrote no valid report: %w", loadErr)
}
