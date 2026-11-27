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
	"sync"
	"time"
)

const (
	maxCodeReviewCheckOutputBytes = 512 * 1024
	maxCodeReviewStderrBytes      = 256 * 1024
	maxCodeReviewTestLinesBytes   = 64 * 1024
)

type codeReviewBoundedBuf struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *codeReviewBoundedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 {
		return len(p), nil
	}
	remain := b.limit - b.buf.Len()
	if remain > 0 {
		if len(p) <= remain {
			_, _ = b.buf.Write(p)
		} else {
			_, _ = b.buf.Write(p[:remain])
			b.truncated = true
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *codeReviewBoundedBuf) WriteString(s string) {
	_, _ = b.Write([]byte(s))
}

func (b *codeReviewBoundedBuf) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *codeReviewBoundedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.buf.String()
	if b.truncated {
		return s + "\n... (output truncated)"
	}
	return s
}

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

	// GOFLAGS splits tokens on whitespace, so prefer a space-free temp dir.
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
		if _, err := resolveCodeReviewAbsPath(projectDir, clean); err != nil {
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
	if overlayPath == "" || strings.ContainsAny(overlayPath, "\r\n\x00") {
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
	tokens := strings.Fields(existing)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "-overlay" || tok == "--overlay" {
			i++ // skip separate flag argument too
			continue
		}
		if strings.HasPrefix(tok, "-overlay=") || strings.HasPrefix(tok, "--overlay=") {
			continue
		}
		kept = append(kept, tok)
	}
	flag := "-overlay=" + overlayPath
	if strings.ContainsAny(overlayPath, " \t") && !strings.ContainsRune(overlayPath, '\'') {
		// Go's GOFLAGS parser (internal/quoted.Split) supports single-quoted tokens containing spaces.
		flag = "'" + flag + "'"
	}
	kept = append(kept, flag)
	out = append(out, "GOFLAGS="+strings.Join(kept, " "))
	return out
}

func validateCodeReviewPackage(pkg string) error {
	if pkg == "." {
		return nil
	}
	if !strings.HasPrefix(pkg, "./") || strings.ContainsAny(pkg, "\r\n\x00") {
		return fmt.Errorf("editor: invalid Go package path %q", pkg)
	}
	cleaned := path.Clean(strings.TrimPrefix(pkg, "./"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "-") {
		return fmt.Errorf("editor: invalid Go package path %q", pkg)
	}
	return nil
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
	for _, pkg := range pkgs {
		if err := validateCodeReviewPackage(pkg); err != nil {
			return CodeReviewCheckResult{
				Phase:      CodeReviewChecksFailed,
				CheckError: err.Error(),
				Duration:   time.Since(started),
			}, err
		}
	}
	res := CodeReviewCheckResult{
		Phase:         CodeReviewChecksRunning,
		Packages:      slices.Clone(pkgs),
		ExpectedTotal: req.PreviousTotal,
	}

	env := withOverlayGOFLAGS(os.Environ(), req.OverlayPath)

	// 1. Run `go tool nsgo check <pkg>` for each affected package using the Go build overlay.
	checkOutBuf := &codeReviewBoundedBuf{limit: maxCodeReviewCheckOutputBytes}
	res.CheckPassed = true
	for _, pkg := range pkgs {
		if err := ctx.Err(); err != nil {
			res.CheckPassed = false
			res.CheckError = err.Error()
			res.Duration = time.Since(started)
			res.Phase = CodeReviewChecksFailed
			return res, err
		}
		cmd := exec.CommandContext(ctx, "go", "tool", "nsgo", "check", pkg)
		cmd.Dir = req.ProjectDir
		cmd.Env = env
		configureCodeReviewCmd(cmd)
		cmdBuf := &codeReviewBoundedBuf{limit: maxCodeReviewCheckOutputBytes}
		cmd.Stdout = cmdBuf
		cmd.Stderr = cmdBuf
		err := cmd.Run()
		if cmdBuf.Len() > 0 {
			if checkOutBuf.Len() > 0 {
				checkOutBuf.WriteString("\n")
			}
			checkOutBuf.WriteString(cmdBuf.String())
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			res.CheckPassed = false
			res.CheckOutput = strings.TrimSpace(checkOutBuf.String())
			res.CheckError = ctxErr.Error()
			res.Duration = time.Since(started)
			res.Phase = CodeReviewChecksFailed
			return res, ctxErr
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
	testOutBuf := &codeReviewBoundedBuf{limit: maxCodeReviewCheckOutputBytes}
	res.TestPassed = true
	for _, pkg := range pkgs {
		if err := ctx.Err(); err != nil {
			res.TestPassed = false
			res.CheckError = err.Error()
			res.Duration = time.Since(started)
			res.Phase = CodeReviewChecksFailed
			return res, err
		}
		cmd := exec.CommandContext(ctx, "go", "tool", "nsgo", "test", "-json", pkg, "-count=1")
		cmd.Dir = req.ProjectDir
		cmd.Env = env
		configureCodeReviewCmd(cmd)
		stderr := &codeReviewBoundedBuf{limit: maxCodeReviewStderrBytes}
		cmd.Stderr = stderr
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
		testLineBytes := make(map[string]int)
		appendTestLine := func(name, line string) {
			if testLineBytes[name]+len(line) > maxCodeReviewTestLinesBytes {
				return
			}
			testLines[name] = append(testLines[name], line)
			testLineBytes[name] += len(line)
		}

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
					appendTestLine(ev.Test, ev.Output)
					// Also retain subtest failure output under its top-level test so subtest file:line failures surface clearly.
					if root, _, isSub := strings.Cut(ev.Test, "/"); isSub && root != "" {
						appendTestLine(root, ev.Output)
					}
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
		scanErr := scanner.Err()
		waitErr := cmd.Wait()
		if ctxErr := ctx.Err(); ctxErr != nil {
			res.TestPassed = false
			res.CheckError = ctxErr.Error()
			res.Duration = time.Since(started)
			res.Phase = CodeReviewChecksFailed
			return res, ctxErr
		}
		if waitErr != nil || scanErr != nil {
			res.TestPassed = false
			if len(res.Failures) == 0 {
				combined := strings.TrimSpace(testOutBuf.String() + "\n" + stderr.String())
				fallback := ""
				if waitErr != nil {
					fallback = waitErr.Error()
				} else if scanErr != nil {
					fallback = scanErr.Error()
				}
				res.CheckError = firstNonEmptyLine(combined, fallback)
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
		// Use rundll32 directly rather than cmd /c start so path metacharacters cannot inject shell commands.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", absPath)
	default:
		cmd = exec.Command("xdg-open", absPath)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
