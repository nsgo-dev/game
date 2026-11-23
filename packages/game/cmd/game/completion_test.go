package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
)

// completionScript returns what game completion writes for shell, failing the test unless it succeeds.
func completionScript(t *testing.T, shell string) string {
	t.Helper()
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"completion", shell}, a); code != 0 {
		t.Fatalf("completion %s exit %d (stderr: %s)", shell, code, stderr)
	}
	if stderr.Len() != 0 {
		t.Fatalf("completion %s wrote to stderr: %q", shell, stderr)
	}
	return stdout.String()
}

func TestCompletionCoversEveryCommandAndFlag(t *testing.T) {
	specs := commandSpecs()
	if specs[len(specs)-1].name != "help" || lookup("completion") == nil {
		t.Fatalf("specs do not end with help or completion is not a command: %v", specs)
	}
	for _, shell := range shellNames() {
		script := completionScript(t, shell)
		if again := completionScript(t, shell); again != script {
			t.Errorf("%s script differs between runs", shell)
		}
		for _, s := range specs {
			if !strings.Contains(script, s.name) {
				t.Errorf("%s script lacks command %s", shell, s.name)
			}
			// bash offers words without descriptions; the scripts quote an apostrophe, so only the text before one is looked for.
			if shell != "bash" && !strings.Contains(script, strings.Split(s.summary, "'")[0]) {
				t.Errorf("%s script lacks the summary of %s", shell, s.name)
			}
			for _, f := range s.flags {
				if !strings.Contains(script, f.name) {
					t.Errorf("%s script lacks %s -%s", shell, s.name, f.name)
				}
			}
		}
	}
}

func TestCompletionUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"completion"}, {"completion", "tcsh"}, {"completion", "zsh", "bash"}} {
		a, stdout, stderr := newTestApp(t)
		if code := run(args, a); code != 2 {
			t.Errorf("%q exit %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "usage: game completion [flags] zsh|bash|fish") {
			t.Errorf("%q stderr = %q, want the usage", args, stderr)
		}
		if stdout.Len() != 0 {
			t.Errorf("%q wrote to stdout: %q", args, stdout)
		}
	}
}

// TestCompletionEntriesNameValueFlags keeps each command's complete map in step with its flags: a key names a flag that takes a value, and offered words are values the command accepts.
func TestCompletionEntriesNameValueFlags(t *testing.T) {
	for _, c := range commands {
		s := specOf(c)
		for key, comp := range c.complete {
			if comp.none() {
				t.Errorf("%s: complete[%q] completes nothing", c.name, key)
			}
			if key == "" {
				continue
			}
			i := slices.IndexFunc(s.flags, func(f flagSpec) bool { return f.name == key })
			if i < 0 || !s.flags[i].takesValue {
				t.Errorf("%s: complete[%q] is not a flag that takes a value", c.name, key)
			}
		}
	}
	for _, w := range audioCommand.complete["format"].words {
		if _, err := audioconv.ParseFormat(w); err != nil {
			t.Errorf("audio -format %s: %v", w, err)
		}
	}
	for _, w := range texCommand.complete["format"].words {
		if _, err := ktx2.ParseFormat(w); err != nil {
			t.Errorf("tex -format %s: %v", w, err)
		}
	}
}

func TestDefaultText(t *testing.T) {
	spec := specOf(lookup("pack"))
	want := map[string]string{"in": "", "out": ".", "name": "atlas", "trim": "true", "extrude": ""}
	for _, f := range spec.flags {
		if w, ok := want[f.name]; ok && f.def != w {
			t.Errorf("pack -%s default %q, want %q", f.name, f.def, w)
		}
	}
}

// completionCase is a command line being completed and the words a shell should offer for its last word.
type completionCase struct {
	// line is the command line up to the cursor, its words separated by spaces; a trailing space starts an empty word.
	line string
	want []string
}

// completionCases run in a directory holding a.png, scene.yaml and the directory sub.
var completionCases = []completionCase{
	{"game ", []string{"audio", "bench", "check", "completion", "dev", "editor", "help", "manifest", "new", "pack", "release", "replay", "tex"}},
	{"game pa", []string{"pack"}},
	{"game pack -", []string{"-extrude", "-in", "-max-height", "-max-width", "-name", "-out", "-padding", "-scale", "-trim"}},
	{"game pack -m", []string{"-max-height", "-max-width"}},
	{"game pack -in ", []string{"a.png", "scene.yaml", "sub/"}},
	{"game pack -padding 2 -o", []string{"-out"}},
	{"game pack -padding ", nil},
	{"game tex -format ", []string{"bc1", "bc3", "etc2", "rgba8"}},
	{"game tex -format=b", []string{"-format=bc1", "-format=bc3"}},
	{"game audio -format m", []string{"mp3"}},
	{"game tex -mipmaps ", []string{"a.png", "scene.yaml", "sub/"}},
	{"game tex -mipmaps sc", []string{"scene.yaml"}},
	{"game tex a.png -", nil},
	{"game editor ", []string{"a.png", "scene.yaml", "sub/"}},
	{"game completion ", []string{"bash", "fish", "zsh"}},
	{"game release -channel st", []string{"steam", "store"}},
	{"game dev -assets ", []string{"a.png", "scene.yaml", "sub/"}},
	{"game check ", []string{"a.png", "scene.yaml", "sub/"}},
	{"game help t", []string{"tex"}},
}

// completionDir returns a directory holding the files completionCases expect.
func completionDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"a.png", "scene.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runShell runs a shell program in dir and returns its standard output, failing the test if it fails or runs for longer than a minute.
func runShell(t *testing.T, dir, shell string, args ...string) string {
	t.Helper()
	path, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s is not installed", shell)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+dir, "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\nstdout:\n%s\nstderr:\n%s", shell, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// checkCompletions compares out, each case's sorted words under a "## line" header, with completionCases.
func checkCompletions(t *testing.T, shell, out string) {
	t.Helper()
	got := map[string][]string{}
	var line string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "## "):
			line = strings.TrimPrefix(l, "## ")
			got[line] = nil
		case l != "":
			got[line] = append(got[line], l)
		}
	}
	for _, tc := range completionCases {
		words, ok := got[tc.line]
		if !ok {
			t.Errorf("%s: %q was not completed:\n%s", shell, tc.line, out)
			continue
		}
		slices.Sort(words)
		words = slices.Compact(words)
		if !slices.Equal(words, tc.want) {
			t.Errorf("%s: %q completes %q, want %q", shell, tc.line, words, tc.want)
		}
	}
}

// zshCapture drives an interactive zsh in a pseudo-terminal for each line it is given, presses Tab and prints the matches the completion system adds, under a "## line" header, finding each match by the byte \x01 it is printed after, which the echo of what was typed does not hold. It loads the script either from $fpath, the way a saved _game file is found, or with source after compinit, as `source <(game completion zsh)` does. What it types into the terminal is indented with spaces, since a tab there would complete.
const zshCapture = `
zmodload zsh/zpty || exit 3
script=$1 mode=$2
shift 2
for line in "$@"; do
	zpty z zsh -f -i
	zpty -w z 'PROMPT= RPROMPT= ; unsetopt zle_bracketed_paste 2>/dev/null; bindkey "^I" complete-word'
	if [[ $mode == fpath ]]; then
		zpty -w z "fpath=(${script:h} \$fpath); autoload -Uz compinit; compinit -D -u"
	else
		zpty -w z "autoload -Uz compinit; compinit -D -u; source $script"
	fi
	zpty -w z 'zstyle ":completion:*" list-grouped false; zstyle ":completion:*" insert-tab false'
	zpty -w z '__capture_null() { print -rn -- $'"'"'\0'"'"' }; compprefuncs=(__capture_null); comppostfuncs=(__capture_null exit)'
	zpty -w z '
compadd() {
  if [[ ${@[1,(i)(-|--)]} == *-(O|A|D)\ * ]]; then
    builtin compadd "$@"
    return
  fi
  local -a __hits
  local -A __opts
  builtin compadd -A __hits "$@"
  zparseopts -A __opts -E P: p: S: s:
  local __h
  for __h in $__hits; do
    [[ -d $__h ]] && __h+=/
    print -r -- $'"'"'\1'"'"'"$IPREFIX${__opts[-P]}${__opts[-p]}$__h"
  done
  builtin compadd "$@"
}'
	zpty -w -n z "$line"$'\t'
	zpty -r z chunk '*'$'\0''*'$'\0'
	zpty -d z
	print -r -- "## $line"
	for l in ${(f)chunk//$'\r'/}; do
		if [[ $l == *$'\1'* ]]; then
			print -r -- ${l#*$'\1'}
		fi
	done
done
`

func TestZshCompletion(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	dir := completionDir(t)
	scriptDir := t.TempDir()
	script := filepath.Join(scriptDir, "_game")
	if err := os.WriteFile(script, []byte(completionScript(t, "zsh")), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runShell(t, dir, "zsh", "-f", "-n", script); out != "" {
		t.Fatalf("zsh -n: %s", out)
	}
	lines := make([]string, len(completionCases))
	for i, tc := range completionCases {
		lines[i] = tc.line
	}
	for _, mode := range []string{"source", "fpath"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			out := runShell(t, dir, "zsh", append([]string{"-f", "-c", zshCapture, "zsh", script, mode}, lines...)...)
			checkCompletions(t, "zsh "+mode, out)
		})
	}
}

// bashCapture sources the script and, for each line, sets COMP_WORDS and COMP_CWORD as bash does when Tab is pressed, splitting = into a word of its own, calls _game and prints COMPREPLY under a "## line" header.
const bashCapture = `
source "$1" || exit 3
shift
for line in "$@"; do
	split=${line//=/ = }
	read -r -a COMP_WORDS <<<"$split"
	case $line in
	*' ') COMP_WORDS+=("") ;;
	esac
	COMP_CWORD=$((${#COMP_WORDS[@]} - 1))
	COMPREPLY=()
	_game
	echo "## $line"
	for w in "${COMPREPLY[@]}"; do
		if [ -d "$w" ]; then w="$w/"; fi
		case ${COMP_WORDS[COMP_CWORD-1]} in
		=) w="${COMP_WORDS[COMP_CWORD-2]}=$w" ;;
		esac
		echo "$w"
	done
done
`

func TestBashCompletion(t *testing.T) {
	dir := completionDir(t)
	script := filepath.Join(t.TempDir(), "game.bash")
	if err := os.WriteFile(script, []byte(completionScript(t, "bash")), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runShell(t, dir, "bash", "-n", script); out != "" {
		t.Fatalf("bash -n: %s", out)
	}
	args := []string{"--norc", "--noprofile", "-c", bashCapture, "bash", script}
	for _, tc := range completionCases {
		args = append(args, tc.line)
	}
	checkCompletions(t, "bash", runShell(t, dir, "bash", args...))
}

func TestFishCompletion(t *testing.T) {
	if _, err := exec.LookPath("fish"); err != nil {
		t.Skip("fish is not installed")
	}
	dir := completionDir(t)
	script := filepath.Join(t.TempDir(), "game.fish")
	if err := os.WriteFile(script, []byte(completionScript(t, "fish")), 0o644); err != nil {
		t.Fatal(err)
	}
	var prog strings.Builder
	prog.WriteString("source " + fishQuote(script) + "\n")
	for _, tc := range completionCases {
		prog.WriteString("echo " + fishQuote("## "+tc.line) + "\n")
		prog.WriteString("complete -C " + fishQuote(tc.line) + " | string replace -r '\\t.*' ''\n")
	}
	checkCompletions(t, "fish", runShell(t, dir, "fish", "--no-config", "-c", prog.String()))
}
