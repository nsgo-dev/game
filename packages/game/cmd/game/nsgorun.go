package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// runGoToolNSGo runs `go tool nsgo` with args in the current directory, the NSGo command of the module game is run from, so tests and benchmarks build as nsgo test builds them, with the app's linker flags and NSGo's check tags.
func runGoToolNSGo(args []string, stdout, stderr io.Writer) error {
	return runGoToolNSGoEnv(nil, args, stdout, stderr)
}

// runGoToolNSGoEnv runs `go tool nsgo` with extra KEY=VALUE environment entries appended to the current environment.
func runGoToolNSGoEnv(env []string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command("go", append([]string{"tool", "nsgo"}, args...)...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go tool nsgo %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// nsgoOutput collects what nsgo prints on each stream while passing it on to the app's own streams. Each stream has a buffer of its own because os/exec copies the two streams in separate goroutines when they are different writers, so one shared buffer would be written concurrently.
type nsgoOutput struct {
	stdout, stderr bytes.Buffer
}

// run runs nsgo with args through a, teeing its output into o.
func (o *nsgoOutput) run(a *app, args []string) error {
	return a.nsgo(args, io.MultiWriter(a.stdout, &o.stdout), io.MultiWriter(a.stderr, &o.stderr))
}

// runEnv runs nsgo with extra environment entries through a, teeing its output into o.
func (o *nsgoOutput) runEnv(a *app, env []string, args []string) error {
	if a.nsgoEnv != nil {
		return a.nsgoEnv(env, args, io.MultiWriter(a.stdout, &o.stdout), io.MultiWriter(a.stderr, &o.stderr))
	}
	return o.run(a, args)
}

// bytes returns everything collected, standard output first, then standard error.
func (o *nsgoOutput) bytes() []byte {
	return append(bytes.Clone(o.stdout.Bytes()), o.stderr.Bytes()...)
}

// tallyLine matches the line nsgo test ends a run with, "N passed, N failed, N skipped" (testRun.finish in NSGo v0.3.0's cmd/nsgo/test.go).
var tallyLine = regexp.MustCompile(`(?m)^\s*(\d+) passed, \d+ failed, \d+ skipped\s*$`)

// passedTests returns the number of passed tests in the last tally line of nsgo test's output out, and false when out holds no tally line.
func passedTests(out []byte) (int, bool) {
	m := tallyLine.FindAllSubmatch(out, -1)
	if len(m) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(string(m[len(m)-1][1]))
	return n, err == nil
}
