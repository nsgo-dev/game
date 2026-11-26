//go:build !ios && !android && !js && !wasm

package editor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// BuildGoOverlay writes a temporary Go build overlay JSON file mapping each pending Go file inside projectDir
// to its staged content in a temporary directory, leaving projectDir on disk untouched.
// Both the cleaned path and EvalSymlinks-resolved path (for example /var vs /private/var on macOS) are mapped.
func BuildGoOverlay(projectDir string, pending map[string]PendingGoOverlayFile) (string, func(), error) {
	if projectDir == "" {
		return "", nil, fmt.Errorf("editor: empty project directory for Go build overlay")
	}
	absRoot, err := filepath.Abs(projectDir)
	if err != nil {
		return "", nil, err
	}
	realRoot := absRoot
	if eval, evalErr := filepath.EvalSymlinks(absRoot); evalErr == nil {
		realRoot = eval
	}

	// GOFLAGS splits tokens on whitespace, so ensure the overlay file lives in a space-free temp dir.
	baseTmp := os.TempDir()
	if strings.ContainsAny(baseTmp, " \t\r\n") && runtime.GOOS != "windows" {
		baseTmp = "/tmp"
	}
	tmpDir, err := os.MkdirTemp(baseTmp, "nsgo-review-overlay-*")
	if err != nil {
		return "", nil, fmt.Errorf("editor: create overlay temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	filesDir := filepath.Join(tmpDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("editor: create overlay files dir: %w", err)
	}

	replace := make(map[string]string, len(pending)*2)
	keys := make([]string, 0, len(pending))
	for rel := range pending {
		keys = append(keys, rel)
	}
	slices.Sort(keys)

	for idx, rel := range keys {
		clean, err := cleanCodeReviewPath(rel)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		pf := pending[rel]
		var replacement string
		if !pf.Delete {
			staged := filepath.Join(filesDir, fmt.Sprintf("%03d_%s", idx, path.Base(clean)))
			if err := os.WriteFile(staged, []byte(pf.Content), 0o600); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("editor: write overlay file %s: %w", clean, err)
			}
			replacement = staged
		}
		origAbs := filepath.Join(absRoot, filepath.FromSlash(clean))
		replace[origAbs] = replacement
		if realRoot != absRoot {
			origReal := filepath.Join(realRoot, filepath.FromSlash(clean))
			replace[origReal] = replacement
		}
	}

	payload := struct {
		Replace map[string]string `json:"Replace"`
	}{
		Replace: replace,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("editor: encode overlay json: %w", err)
	}
	overlayPath := filepath.Join(tmpDir, "overlay.json")
	if err := os.WriteFile(overlayPath, data, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("editor: write overlay json: %w", err)
	}
	return overlayPath, cleanup, nil
}

// withOverlayGOFLAGS returns a copy of env with GOFLAGS updated to include -overlay=<overlayPath>
// (replacing any pre-existing -overlay=... flag while preserving other GOFLAGS tokens).
func withOverlayGOFLAGS(env []string, overlayPath string) []string {
	if overlayPath == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	var existing string
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			existing = strings.TrimPrefix(kv, "GOFLAGS=")
			continue
		}
		out = append(out, kv)
	}
	var kept []string
	for _, tok := range strings.Fields(existing) {
		if strings.HasPrefix(tok, "-overlay=") {
			continue
		}
		kept = append(kept, tok)
	}
	kept = append(kept, "-overlay="+overlayPath)
	out = append(out, "GOFLAGS="+strings.Join(kept, " "))
	return out
}

type goTestJSONEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

func runDefaultCodeReviewChecks(ctx context.Context, req CodeReviewCheckRequest, onProgress func(CodeReviewCheckProgress)) (CodeReviewCheckResult, error) {
	started := time.Now()
	pkgs := req.Packages
	if len(pkgs) == 0 {
		pkgs = []string{"."}
	}
	res := CodeReviewCheckResult{
		Phase:         CodeReviewChecksRunning,
		Packages:      slices.Clone(pkgs),
		ExpectedTotal: req.PreviousTotal,
	}

	env := withOverlayGOFLAGS(os.Environ(), req.OverlayPath)

	// 1. Run `go tool nsgo check <pkg>` for each affected package using the Go build overlay.
	var checkOutBuf strings.Builder
	res.CheckPassed = true
	for _, pkg := range pkgs {
		cmd := exec.CommandContext(ctx, "go", "tool", "nsgo", "check", pkg)
		cmd.Dir = req.ProjectDir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if len(out) > 0 {
			if checkOutBuf.Len() > 0 {
				checkOutBuf.WriteByte('\n')
			}
			checkOutBuf.Write(out)
		}
		if err != nil {
			res.CheckPassed = false
			res.CheckOutput = strings.TrimSpace(checkOutBuf.String())
			res.CheckError = firstNonEmptyLine(res.CheckOutput, err.Error())
			res.Duration = time.Since(started)
			res.Phase = CodeReviewChecksFailed
			return res, nil
		}
	}
	res.CheckOutput = strings.TrimSpace(checkOutBuf.String())

	// 2. Run `go tool nsgo test -json <pkg> -count=1` and stream progress & failures.
	var testOutBuf strings.Builder
	res.TestPassed = true
	for _, pkg := range pkgs {
		cmd := exec.CommandContext(ctx, "go", "tool", "nsgo", "test", "-json", pkg, "-count=1")
		cmd.Dir = req.ProjectDir
		cmd.Env = env
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			res.TestPassed = false
			res.CheckError = err.Error()
			res.Duration = time.Since(started)
			res.Phase = CodeReviewChecksFailed
			return res, err
		}
		if err := cmd.Start(); err != nil {
			res.TestPassed = false
			res.CheckError = err.Error()
			res.Duration = time.Since(started)
			res.Phase = CodeReviewChecksFailed
			return res, err
		}

		testLines := make(map[string][]string)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			raw := scanner.Text()
			var ev goTestJSONEvent
			if jsonErr := json.Unmarshal([]byte(raw), &ev); jsonErr != nil {
				continue
			}
			if ev.Output != "" {
				testOutBuf.WriteString(ev.Output)
				if ev.Test != "" {
					testLines[ev.Test] = append(testLines[ev.Test], ev.Output)
				}
			}
			if ev.Test == "" || strings.Contains(ev.Test, "/") {
				continue
			}
			switch ev.Action {
			case "pass":
				res.PassedCount++
				res.Completed++
				if onProgress != nil {
					onProgress(CodeReviewCheckProgress{
						Completed:   res.Completed,
						Total:       max(res.ExpectedTotal, res.Completed),
						CurrentTest: ev.Test,
					})
				}
			case "fail":
				res.FailedCount++
				res.Completed++
				res.TestPassed = false
				file, line, msg := parseGoTestFailureOutput(testLines[ev.Test])
				res.Failures = append(res.Failures, CodeReviewTestFailure{
					Package:  pkg,
					TestName: ev.Test,
					File:     file,
					Line:     line,
					Message:  msg,
				})
				if onProgress != nil {
					onProgress(CodeReviewCheckProgress{
						Completed:   res.Completed,
						Total:       max(res.ExpectedTotal, res.Completed),
						CurrentTest: ev.Test,
					})
				}
			}
		}
		waitErr := cmd.Wait()
		if waitErr != nil {
			res.TestPassed = false
			if len(res.Failures) == 0 {
				combined := strings.TrimSpace(testOutBuf.String() + "\n" + stderr.String())
				res.CheckError = firstNonEmptyLine(combined, waitErr.Error())
			}
		}
	}

	res.TestOutput = strings.TrimSpace(testOutBuf.String())
	res.TotalCount = res.PassedCount + res.FailedCount
	res.Duration = time.Since(started)
	if res.CheckPassed && res.TestPassed && res.FailedCount == 0 && res.CheckError == "" {
		res.Phase = CodeReviewChecksPassed
	} else {
		res.Phase = CodeReviewChecksFailed
	}
	return res, nil
}

var goTestFailureLineRe = regexp.MustCompile(`^\s*([^\s:]+\.go):(\d+):\s*(.+)$`)

func parseGoTestFailureOutput(lines []string) (file string, line int, message string) {
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "=== RUN") || strings.HasPrefix(trimmed, "--- FAIL") {
			continue
		}
		if m := goTestFailureLineRe.FindStringSubmatch(trimmed); len(m) == 4 {
			ln, _ := strconv.Atoi(m[2])
			return m[1], ln, fmt.Sprintf("%s:%d: %s", m[1], ln, strings.TrimSpace(m[3]))
		}
	}
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" && !strings.HasPrefix(trimmed, "=== RUN") && !strings.HasPrefix(trimmed, "--- FAIL") {
			return "", 0, trimmed
		}
	}
	return "", 0, "test failed"
}

func firstNonEmptyLine(text, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func openExternalSourceFileOS(absPath string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", absPath)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", absPath)
	default:
		cmd = exec.Command("xdg-open", absPath)
	}
	return cmd.Start()
}
