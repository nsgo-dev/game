package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeRecording copies the replay package's golden recording to dir/name, creating dir.
func writeRecording(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "replay", "testdata", "level_clear.replay.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, filepath.Join(dir, name), string(data))
}

func writeFile(t *testing.T, path, text string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// passingTally is the tally nsgo test prints on standard error after a run in which two tests passed.
const passingTally = "  2 passed, 0 failed, 0 skipped\n"

// recordReplayNSGo replaces a's nsgo with one that records each call's arguments, prints tally on standard error as nsgo test does, and returns err.
func recordReplayNSGo(a *app, tally string, err error) *[][]string {
	var calls [][]string
	a.nsgo = func(args []string, _, stderr io.Writer) error {
		calls = append(calls, args)
		io.WriteString(stderr, tally)
		return err
	}
	return &calls
}

func TestReplayChecksRecordingsThenRunsTheirTests(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, filepath.Join(dir, "level", "testdata"), "clear.replay.yaml")
	writeRecording(t, dir, "boss.replay.yaml")

	a, stdout, stderr := newTestApp(t)
	calls := recordReplayNSGo(a, passingTally, nil)
	if code := run([]string{"replay", dir}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	want := [][]string{{"test", "-run", "Replay", dir, "-count=1"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("nsgo calls = %q, want %q", *calls, want)
	}
	if !strings.Contains(stdout.String(), `2 recordings, playing them through the tests matching "Replay"`) {
		t.Fatalf("stdout = %q", stdout)
	}

	a, _, stderr = newTestApp(t)
	calls = recordReplayNSGo(a, passingTally, nil)
	other := t.TempDir()
	writeRecording(t, other, "a.replay.yaml")
	if code := run([]string{"replay", "-run", "^TestLevel$", dir + "/...", other}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	want = [][]string{{"test", "-run", "^TestLevel$", dir + "/...", "-count=1"}, {"test", "-run", "^TestLevel$", other, "-count=1"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("nsgo calls = %q, want %q", *calls, want)
	}
}

func TestReplayFailsOnAFailedTest(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, "run.replay.yaml")
	a, _, stderr := newTestApp(t)
	recordReplayNSGo(a, "  1 passed, 1 failed, 0 skipped\n", errors.New("exit status 1"))
	if code := run([]string{"replay", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "game replay: replay tests in "+dir+" failed: exit status 1") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestReplayFailsWhenNoTestPlaysTheRecordings(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, "run.replay.yaml")
	for _, tc := range []struct {
		tally, want string
	}{
		// nsgo test exits 0 when -run matches no test, as go test does.
		{"  0 passed, 0 failed, 0 skipped\n", `no test in ` + dir + ` matching -run "NoSuchTest" passed, so no recording was played`},
		{"  0 passed, 0 failed, 2 skipped\n", `no test in ` + dir + ` matching -run "NoSuchTest" passed`},
		{"", "replay tests in " + dir + " printed no tally of passed tests"},
	} {
		a, _, stderr := newTestApp(t)
		recordReplayNSGo(a, tc.tally, nil)
		if code := run([]string{"replay", "-run", "NoSuchTest", dir}, a); code != 1 {
			t.Errorf("tally %q: exit %d, want 1", tc.tally, code)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("tally %q: stderr = %q, want %q", tc.tally, stderr, tc.want)
		}
	}
}

func TestPassedTestsReadsTheLastTally(t *testing.T) {
	for _, tc := range []struct {
		out    string
		passed int
		ok     bool
	}{
		{"  ✓ TestA (0s)\n  3 passed, 0 failed, 1 skipped\n", 3, true},
		{"  1 passed, 0 failed, 0 skipped\n  ✓ TestB (0s)\n  0 passed, 0 failed, 0 skipped\n", 0, true},
		{"ok  \tpkg\t0.1s\n", 0, false},
		{"the log said 5 passed, 0 failed, 0 skipped earlier\n", 0, false},
	} {
		passed, ok := passedTests([]byte(tc.out))
		if passed != tc.passed || ok != tc.ok {
			t.Errorf("passedTests(%q) = %d, %t, want %d, %t", tc.out, passed, ok, tc.passed, tc.ok)
		}
	}
}

func TestReplayRefusesInvalidRecordingsBeforeRunningTests(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, "good.replay.yaml")
	bad := writeFile(t, filepath.Join(dir, "bad.replay.yaml"), "version: 2\nseed: 1\nrate: 60\ntotal_steps: 3\nsteps:\n  - {step: 1, press: [jump]}\n")
	a, _, stderr := newTestApp(t)
	calls := recordReplayNSGo(a, passingTally, nil)
	if code := run([]string{"replay", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if len(*calls) != 0 {
		t.Fatalf("tests ran: %q", *calls)
	}
	for _, want := range []string{"1 of 2 recordings cannot be played", bad, `action "jump", which is not in actions`} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr does not contain %q:\n%s", want, stderr)
		}
	}
}

func TestReplayFailsWithoutRecordings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "notes.yaml"), "a: 1\n")
	file := writeRecording(t, t.TempDir(), "x.replay.yaml")
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"replay", dir}, 1, "no *.replay.yaml recordings under " + dir},
		{[]string{"replay", filepath.Join(dir, "missing")}, 1, "no such file or directory"},
		{[]string{"replay", file}, 1, file + " is not a directory"},
		{[]string{"replay", "-run", "", dir}, 2, "-run must name the tests"},
	} {
		a, _, stderr := newTestApp(t)
		if code := run(tc.args, a); code != tc.code {
			t.Errorf("%q exit %d, want %d", tc.args, code, tc.code)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%q stderr = %q, want %q", tc.args, stderr, tc.want)
		}
	}
}

func TestFindRecordingsSkipsWhatGoSkips(t *testing.T) {
	dir := t.TempDir()
	want := []string{
		writeRecording(t, dir, "a.replay.yaml"),
		writeRecording(t, filepath.Join(dir, "pkg", "testdata"), "b.replay.yaml"),
	}
	writeRecording(t, filepath.Join(dir, ".hidden"), "c.replay.yaml")
	writeRecording(t, filepath.Join(dir, "_old"), "d.replay.yaml")
	writeRecording(t, filepath.Join(dir, "nested"), "e.replay.yaml")
	writeFile(t, filepath.Join(dir, "nested", "go.mod"), "module nested\n")
	writeFile(t, filepath.Join(dir, "f.replay.json"), "{}")

	for _, arg := range []string{dir, dir + "/..."} {
		got, err := findRecordings(arg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("findRecordings(%q) = %q, want %q", arg, got, want)
		}
	}
}
