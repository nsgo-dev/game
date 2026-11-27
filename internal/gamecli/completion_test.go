package gamecli

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

	"github.com/nsgo-dev/game/pkg/ktx2"
	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
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

// gameCompletionCases run in a directory holding a.png, scene.yaml and the directory sub.
var gameCompletionCases = []completionCase{
	{"game ", []string{"assets", "audio", "bench", "check", "completion", "credits", "dev", "editor", "generate", "help", "index", "library", "manifest", "mcp", "merge", "mv", "navmesh", "net", "new", "pack", "pixelfx", "playtest", "release", "replay", "schema", "tex"}},
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
	{"game generate ", []string{"component", "editor", "prefab", "scene", "system"}},
	// The kinds complete at the name too, since a command's arguments complete alike at every position.
	{"game generate scene ", []string{"component", "editor", "prefab", "scene", "system"}},
	{"game generate -in ", []string{"a.png", "scene.yaml", "sub/"}},
	{"game generate -main s", []string{"scene", "system"}},
	{"game library ", []string{"digest", "install", "list", "remove"}},
	{"game library -kind ", []string{"asset", "plugin", "template"}},
	{"game library -project ", []string{"a.png", "scene.yaml", "sub/"}},
	{"game pixelfx explosions/sm", []string{"explosions/small-explosion", "explosions/smoke-bomb"}},
	{"game pixelfx -f", []string{"-flip", "-fps"}},
	{"game merge -", []string{"-conflict-out", "-out", "-setup"}},
	{"game help t", []string{"tex"}},
	{"game new -", []string{"-list", "-module", "-name", "-template", "-version"}},
}

// nsgoCompletionCases complete nsgo's own words, and after nsgo game every line of gameCompletionCases as game does.
func nsgoCompletionCases() []completionCase {
	cases := []completionCase{
		{"nsgo ", []string{"build", "certify", "check", "dev", "doctor", "game", "help", "i18n", "init", "keygen", "mcp", "publish", "revoke", "test", "verify", "version"}},
		{"nsgo g", []string{"game"}},
		{"nsgo dev ", []string{"a.png", "scene.yaml", "sub/"}},
	}
	for _, tc := range gameCompletionCases {
		cases = append(cases, completionCase{"nsgo " + tc.line, tc.want})
	}
	return cases
}

// completionCases returns the cases shell runs: game's and nsgo's, but in fish not nsgo alone, since the fish script adds to a completion of nsgo rather than replacing it, so there fish offers file paths beside game.
func completionCases(shell string) []completionCase {
	var cases []completionCase
	for _, tc := range append(slices.Clone(gameCompletionCases), nsgoCompletionCases()...) {
		if shell != "fish" || tc.line != "nsgo " {
			cases = append(cases, tc)
		}
	}
	return cases
}

// completionLines returns the lines of cases.
func completionLines(cases []completionCase) []string {
	lines := make([]string, len(cases))
	for i, tc := range cases {
		lines[i] = tc.line
	}
	return lines
}

// completionDir returns a directory holding the files the completion cases expect.
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

// checkCompletions compares out, each case's sorted words under a "## line" header, with cases.
func checkCompletions(t *testing.T, shell, out string, cases []completionCase) {
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
	for _, tc := range cases {
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

// zshCapture drives an interactive zsh in a pseudo-terminal for each line it is given, presses Tab and prints the matches the completion system adds, under a "## line" header, finding each match by the byte \x01 it is printed after, which the echo of what was typed does not hold. It loads the script either from $fpath (mode fpath, followed by directories to put after it on $fpath), the way a saved _game file is found, or with source after compinit, as `source <(game completion zsh)` does. What it types into the terminal is indented with spaces, since a tab there would complete.
const zshCapture = `
zmodload zsh/zpty || exit 3
script=$1 mode=$2
shift 2
for line in "$@"; do
	zpty z zsh -f -i
	zpty -w z 'PROMPT= RPROMPT= ; unsetopt zle_bracketed_paste 2>/dev/null; bindkey "^I" complete-word'
	if [[ $mode == fpath* ]]; then
		zpty -w z "fpath=(${script:h} \$fpath ${mode#fpath}); autoload -Uz compinit; compinit -D -u"
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
	cases := completionCases("zsh")
	for _, mode := range []string{"source", "fpath"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			out := runShell(t, dir, "zsh", append([]string{"-f", "-c", zshCapture, "zsh", script, mode}, completionLines(cases)...)...)
			checkCompletions(t, "zsh "+mode, out, cases)
		})
	}
}

// TestZshCompletionKeepsOwnNsgo checks that the script leaves a completion of nsgo's own in place: one registered before it is sourced, one compinit registers from earlier on $fpath, and one later on $fpath, which completes nsgo's words while nsgo game still completes game's commands.
func TestZshCompletionKeepsOwnNsgo(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	dir := completionDir(t)
	scriptDir, ownDir := t.TempDir(), t.TempDir()
	script := filepath.Join(scriptDir, "_game")
	if err := os.WriteFile(script, []byte(completionScript(t, "zsh")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ownDir, "_nsgo"), []byte("#compdef nsgo\ncompadd -- own\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, setup, want string }{
		{"sourced", "autoload -Uz compinit; compinit -D -u; _mine() { }; compdef _mine nsgo; source " + script, "_mine _game"},
		{"fpath before", "fpath=(" + ownDir + " " + scriptDir + " $fpath); autoload -Uz compinit; compinit -D -u", "_nsgo _game"},
	} {
		out := runShell(t, dir, "zsh", "-f", "-c", tc.setup+"; print -r -- $_comps[nsgo] $_comps[game]")
		if got := strings.TrimSpace(out); got != tc.want {
			t.Errorf("%s: nsgo and game complete with %q, want %q", tc.name, got, tc.want)
		}
	}
	cases := []completionCase{{"nsgo ", []string{"own"}}, {"nsgo game pa", []string{"pack"}}}
	out := runShell(t, dir, "zsh", append([]string{"-f", "-c", zshCapture, "zsh", script, "fpath " + ownDir}, completionLines(cases)...)...)
	checkCompletions(t, "zsh fpath after", out, cases)
}

// bashCapture sources the script and, for each line, sets COMP_WORDS and COMP_CWORD as bash does when Tab is pressed, splitting = into a word of its own, calls the function registered for the line's command and prints COMPREPLY under a "## line" header. A directory gets the / that bash adds only when the script marked the results as file names, either with compopt -o filenames (which works only inside real completion, so a function stands in for it and records the call) or, in bash 3.2 where there is no compopt, by registering the function with -o filenames.
const bashCapture = `
source "$1" || exit 3
shift
if type compopt >/dev/null 2>&1; then
	compopt() {
		case " $* " in
		*' -o filenames '*) marked=1 ;;
		esac
	}
fi
for line in "$@"; do
	split=${line//=/ = }
	read -r -a COMP_WORDS <<<"$split"
	case $line in
	*' ') COMP_WORDS+=("") ;;
	esac
	COMP_CWORD=$((${#COMP_WORDS[@]} - 1))
	COMPREPLY=()
	spec=$(complete -p "${COMP_WORDS[0]}")
	case $spec in
	*'-o filenames'*) marked=1 ;;
	*) marked= ;;
	esac
	fn=${spec##*-F }
	${fn%% *}
	echo "## $line"
	for w in "${COMPREPLY[@]}"; do
		if [ -n "$marked" ] && [ -d "$w" ]; then w="$w/"; fi
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
	cases := completionCases("bash")
	args := append([]string{"--norc", "--noprofile", "-c", bashCapture, "bash", script}, completionLines(cases)...)
	// macOS ships bash 3.2 as /bin/bash, which has no compopt, while bash on $PATH may be a newer one, so both run where they differ.
	shells := []string{"bash"}
	if path, err := exec.LookPath("bash"); err == nil {
		onPath, err1 := filepath.EvalSymlinks(path)
		system, err2 := filepath.EvalSymlinks("/bin/bash")
		if err1 == nil && err2 == nil && onPath != system {
			shells = append(shells, "/bin/bash")
		}
	}
	for _, shell := range shells {
		checkCompletions(t, shell, runShell(t, dir, shell, args...), cases)
	}
}

// TestBashCompletionKeepsOwnNsgo checks that the script leaves a completion of nsgo's own in place: one registered before it is sourced, and one bash-completion has not loaded yet, which a stand-in for bash-completion's loader registers. It also checks that the loader does not load the script again when the script is that completion.
func TestBashCompletionKeepsOwnNsgo(t *testing.T) {
	dir := completionDir(t)
	script := filepath.Join(t.TempDir(), "game.bash")
	if err := os.WriteFile(script, []byte(completionScript(t, "bash")), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, setup, want string }{
		{"registered", "complete -W own nsgo", "complete -W 'own' nsgo"},
		{"not loaded", "_comp_load() { complete -W own \"$1\"; }", "complete -W 'own' nsgo"},
		{"loaded as nsgo", "_comp_load() { loads=$((loads + 1)); source " + shQuote(script) + "; }", "1 complete -F __game_nsgo nsgo"},
	} {
		out := runShell(t, dir, "bash", "--norc", "--noprofile", "-c", tc.setup+"; source "+shQuote(script)+"; echo ${loads-} $(complete -p nsgo)")
		// bash 3.2 registers the script's function with -o filenames as well.
		if got := strings.Replace(strings.TrimSpace(out), "-o filenames ", "", 1); got != tc.want {
			t.Errorf("%s: nsgo completes with %q, want %q", tc.name, got, tc.want)
		}
	}
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
	cases := completionCases("fish")
	for _, tc := range cases {
		prog.WriteString("echo " + fishQuote("## "+tc.line) + "\n")
		// string replace fails when it replaces nothing, as for words without a description, and fish -c exits with the last command's status, so its status is dropped.
		prog.WriteString("complete -C " + fishQuote(tc.line) + " | string replace -r '\\t.*' ''; or true\n")
	}
	checkCompletions(t, "fish", runShell(t, dir, "fish", "--no-config", "-c", prog.String()), cases)
}
