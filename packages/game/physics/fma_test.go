package physics_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// fusedMultiplyAdd matches the arm64 fused multiply-add family in go tool objdump's output: FMADDD, FMSUBD, FNMADDD and FNMSUBD, and their single-precision S forms.
var fusedMultiplyAdd = regexp.MustCompile(`\sFN?M(ADD|SUB)[DS]\s`)

// objdumpText matches the header go tool objdump prints before each function: its symbol and the file that declares it.
var objdumpText = regexp.MustCompile(`^TEXT (\S+)\(SB\) (\S+\.go)$`)

// objdumpLine matches one instruction line of go tool objdump, which starts with the source file and line the instruction came from. Code inlined from another function keeps that function's file and line.
var objdumpLine = regexp.MustCompile(`^\s+\S+\.go:\d+\s`)

// goldenDrivers are the test files whose code drives the cross-architecture golden trajectories, so a contraction in them would break the goldens as surely as one in the package.
var goldenDrivers = map[string]bool{"golden_test.go": true, "snapshot_test.go": true}

// TestNoFusedMultiplyAddOnArm64 builds this package's test binary, disassembles its physics code, and fails on any fused multiply-add outside the debug drawing files, and on any in the code that drives the golden trajectories. Go may contract x*y + z into one instruction on arm64 that rounds once, while amd64 rounds the product and the sum separately, so a single contraction left in a stepped path makes the two architectures drift apart. The check runs on arm64 only, where the compiler emits these instructions; it covers the package's functions with whatever is inlined into them, and internal/detmath's functions, which the step calls when they are too big to inline.
func TestNoFusedMultiplyAddOnArm64(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skipf("fused multiply-add contraction is checked on arm64, not %s", runtime.GOARCH)
	}
	goTool := testGoTool(t)
	// go test links the binary it runs without a symbol table, so build one that keeps it.
	exe := filepath.Join(t.TempDir(), "physics.test")
	build := exec.Command(goTool, "test", "-c", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go test -c: %v\n%s", err, out)
	}
	cmd := exec.Command(goTool, "tool", "objdump", "-s", `^github\.com/nsgo-dev/game/packages/game/(physics(_test)?|internal/detmath)\.`, exe)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go tool objdump: %v\n%s", err, stderr.String())
	}

	instructions := 0
	var fn string
	checked := false
	var found []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if m := objdumpText.FindStringSubmatch(line); m != nil {
			// A function is judged by the file that declares it, because the products a contraction fuses are written there even when the add comes from an inlined helper. Debug drawing is not part of the step: it turns state into lines and never feeds back into it. Tests other than the golden drivers compare against tolerances, not hashes.
			fn = m[1]
			file := filepath.Base(m[2])
			checked = !strings.HasPrefix(file, "debugdraw") && !strings.HasPrefix(file, "xpbd_debugdraw") && (!strings.HasSuffix(file, "_test.go") || goldenDrivers[file])
			continue
		}
		if !objdumpLine.MatchString(line) {
			continue
		}
		instructions++
		if checked && fusedMultiplyAdd.MatchString(line) {
			found = append(found, fn+": "+strings.Join(strings.Fields(line), " "))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading go tool objdump output: %v", err)
	}
	// A filter that matched nothing would pass vacuously, so make sure the step itself was disassembled.
	if instructions == 0 || !bytes.Contains(out, []byte("physics.(*SoftWorld).Step(SB)")) || !bytes.Contains(out, []byte("detmath.Sincos(SB)")) {
		t.Fatalf("go tool objdump listed %d instructions without both SoftWorld.Step and detmath.Sincos; the symbol filter no longer matches", instructions)
	}
	if len(found) > 0 {
		t.Fatalf("%d fused multiply-add instructions in physics code or a golden driver; wrap each product that feeds an add or subtract in an explicit float64(...) or float32(...) conversion:\n%s", len(found), strings.Join(found, "\n"))
	}
}

// testGoTool returns the go command of the toolchain that built this test, so the binary it disassembles is compiled the same way. It prefers GOROOT's go, which go test sets for the processes it runs, and falls back to PATH. The check is the arm64 acceptance gate, so a missing go command fails it instead of skipping it.
func testGoTool(t *testing.T) string {
	t.Helper()
	goTool := ""
	if root := os.Getenv("GOROOT"); root != "" {
		if p := filepath.Join(root, "bin", "go"); fileExists(p) {
			goTool = p
		}
	}
	if goTool == "" {
		p, err := exec.LookPath("go")
		if err != nil {
			t.Fatalf("go command not found, so the arm64 fused multiply-add check cannot run: %v", err)
		}
		goTool = p
	}
	out, err := exec.Command(goTool, "env", "GOVERSION").Output()
	if err != nil {
		t.Fatalf("%s env GOVERSION: %v", goTool, err)
	}
	if v := strings.TrimSpace(string(out)); !strings.HasPrefix(runtime.Version(), v) {
		t.Fatalf("%s is %s but this test was built with %s; run it with the toolchain that builds it", goTool, v, runtime.Version())
	}
	return goTool
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}
