package gamecli

import (
	"flag"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// completion is what a shell offers when completing a flag's value or a command's arguments: file paths, or a fixed list of words.
type completion struct {
	// files completes file and directory paths.
	files bool
	// words are the values offered when files is false.
	words []string
}

// filePaths completes file and directory paths.
var filePaths = completion{files: true}

// oneOf completes one of words.
func oneOf(words ...string) completion {
	return completion{words: words}
}

// none reports whether c completes nothing.
func (c completion) none() bool {
	return !c.files && len(c.words) == 0
}

// nsgoCommands are the words completed after nsgo: game, whose commands follow it, and nsgo's own commands, offered as plain words since nsgo has no completion of its own.
var nsgoCommands = []string{"game", "init", "dev", "mcp", "build", "test", "check", "i18n", "doctor", "keygen", "certify", "publish", "verify", "revoke", "version", "help"}

// shells are the shells game completion writes a script for, in the order its usage names them, each with what writes its script.
var shells = []struct {
	name  string
	write func(w io.Writer, specs []commandSpec)
}{
	{"zsh", writeZsh},
	{"bash", writeBash},
	{"fish", writeFish},
}

// shellNames returns the names of shells, in order.
func shellNames() []string {
	names := make([]string, len(shells))
	for i, s := range shells {
		names[i] = s.name
	}
	return names
}

// completionCommand writes a completion script for zsh, bash or fish, generated from the commands so a new subcommand or flag completes without editing the scripts.
var completionCommand = &command{
	name:    "completion",
	args:    "zsh|bash|fish",
	summary: "Write a shell completion script for zsh, bash or fish",
	define:  defineCompletion,
	complete: map[string]completion{
		"": oneOf(shellNames()...),
	},
}

// The completion command reads commands to describe every command, itself included, so it is added here: listing it in commands' initializer would make an initialization cycle.
func init() {
	commands = append(commands, completionCommand)
}

func defineCompletion(a *app, fs *flag.FlagSet) func() error {
	return func() error {
		if fs.NArg() != 1 {
			return usagef("expected one shell: %s", strings.Join(shellNames(), ", "))
		}
		for _, s := range shells {
			if s.name == fs.Arg(0) {
				s.write(a.stdout, commandSpecs())
				return nil
			}
		}
		return usagef("unknown shell %q (want %s)", fs.Arg(0), strings.Join(shellNames(), ", "))
	}
}

// commandSpec is a command as the completion scripts and the reference describe it, read from its definition.
type commandSpec struct {
	// name is the word after game that runs it.
	name string
	// usage is its usage line, such as "game tex [flags] [input.png|input.jpg]".
	usage string
	// summary is the one line game help shows for it.
	summary string
	// flags are its flags in lexical order, as game help lists them.
	flags []flagSpec
	// args is what completes its arguments.
	args completion
}

// flagSpec is one flag of a commandSpec.
type flagSpec struct {
	// name is the flag's name without its dash.
	name string
	// valueName names the value it takes, such as "int" or "string", and is empty for a boolean flag.
	valueName string
	// usage is the flag's help text.
	usage string
	// def is its default as game help shows it, empty when the default is the type's zero value.
	def string
	// takesValue reports whether the flag reads the next argument as its value: every flag but a boolean one.
	takesValue bool
	// value is what completes its value.
	value completion
}

// commandSpecs returns every command's spec in the order game help lists them, help last.
func commandSpecs() []commandSpec {
	specs := make([]commandSpec, 0, len(commands)+1)
	names := make([]string, 0, len(commands)+1)
	for _, c := range commands {
		specs = append(specs, specOf(c))
		names = append(names, c.name)
	}
	names = append(names, "help")
	return append(specs, commandSpec{
		name:    "help",
		usage:   "game help [command]",
		summary: helpSummary,
		args:    oneOf(names...),
	})
}

// specOf reads c's flags by defining them on a flag set of its own; the function define returns is never run.
func specOf(c *command) commandSpec {
	fs := flag.NewFlagSet("game "+c.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c.define(&app{stdout: io.Discard, stderr: io.Discard}, fs)
	s := commandSpec{name: c.name, usage: c.usage(), summary: c.summary, args: c.complete[""]}
	fs.VisitAll(func(f *flag.Flag) {
		valueName, usage := flag.UnquoteUsage(f)
		value := c.complete[f.Name]
		if _, ok := c.complete[f.Name]; !ok && (f.Name == "in" || f.Name == "out") {
			value = filePaths
		}
		s.flags = append(s.flags, flagSpec{
			name:       f.Name,
			valueName:  valueName,
			usage:      usage,
			def:        defaultText(f),
			takesValue: !isBoolFlag(f),
			value:      value,
		})
	})
	return s
}

// isBoolFlag reports whether f is a boolean flag, one set by its name alone.
func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// defaultText returns f's default, or "" when it is its type's zero value, which game help leaves out as flag.PrintDefaults does.
func defaultText(f *flag.Flag) string {
	t := reflect.TypeOf(f.Value)
	var zero reflect.Value
	if t.Kind() == reflect.Pointer {
		zero = reflect.New(t.Elem())
	} else {
		zero = reflect.Zero(t)
	}
	if v, ok := zero.Interface().(flag.Value); ok && v.String() == f.DefValue {
		return ""
	}
	return f.DefValue
}

// shQuote quotes s for zsh and bash as one word in single quotes.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// fishQuote quotes s for fish as one word in single quotes, where a backslash escapes a quote or another backslash.
func fishQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// quoteAll quotes each of words with quote and joins them with sep.
func quoteAll(words []string, quote func(string) string, sep string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = quote(w)
	}
	return strings.Join(q, sep)
}

// casePattern returns the alternatives of a zsh and bash case pattern, each matching "command:flag" for a flag of s that pick selects.
func casePattern(s commandSpec, pick func(flagSpec) bool) []string {
	var alts []string
	for _, f := range s.flags {
		if pick(f) {
			alts = append(alts, shQuote(s.name+":"+f.name))
		}
	}
	return alts
}

// valueCases groups the flags of specs whose values complete alike, in order of first appearance, so each completion is one case arm keyed by "command:flag".
func valueCases(specs []commandSpec) (pattern []string, value []completion) {
	for _, s := range specs {
		for _, f := range s.flags {
			if !f.takesValue || f.value.none() {
				continue
			}
			key := shQuote(s.name + ":" + f.name)
			found := false
			for i, v := range value {
				if reflect.DeepEqual(v, f.value) {
					pattern[i] += "|" + key
					found = true
					break
				}
			}
			if !found {
				pattern = append(pattern, key)
				value = append(value, f.value)
			}
		}
	}
	return pattern, value
}

// argCases groups the commands of specs whose arguments complete alike, in order of first appearance, so each completion is one case arm keyed by the command's name.
func argCases(specs []commandSpec) (pattern []string, value []completion) {
	for _, s := range specs {
		if s.args.none() {
			continue
		}
		found := false
		for i, v := range value {
			if reflect.DeepEqual(v, s.args) {
				pattern[i] += "|" + shQuote(s.name)
				found = true
				break
			}
		}
		if !found {
			pattern = append(pattern, shQuote(s.name))
			value = append(value, s.args)
		}
	}
	return pattern, value
}

// valueFlagPattern returns the case pattern matching every "command:flag" of specs whose flag takes a value, or "" when no flag does.
func valueFlagPattern(specs []commandSpec) string {
	var alts []string
	for _, s := range specs {
		alts = append(alts, casePattern(s, func(f flagSpec) bool { return f.takesValue })...)
	}
	return strings.Join(alts, "|")
}

// writeZsh writes the zsh completion script for specs: a #compdef function that works both from a directory on $fpath and sourced after compinit. It completes nsgo too, unless nsgo already has a completion: compinit registers the names after #compdef with compdef -n, which keeps an existing one, and the sourced script checks $_comps.
func writeZsh(w io.Writer, specs []commandSpec) {
	var b strings.Builder
	b.WriteString("#compdef game nsgo\n\n")
	b.WriteString("# zsh completion for game, written by `game completion zsh`, for game and for nsgo game. Load it after compinit with `source <(game completion zsh)`, or save it as _game in a directory on $fpath.\n\n")

	b.WriteString("__game_takes_value() {\n\tcase $1:$2 in\n")
	if p := valueFlagPattern(specs); p != "" {
		fmt.Fprintf(&b, "\t(%s) return 0 ;;\n", p)
	}
	b.WriteString("\tesac\n\treturn 1\n}\n\n")

	b.WriteString("__game_value() {\n\tcase $1:$2 in\n")
	patterns, values := valueCases(specs)
	for i, v := range values {
		fmt.Fprintf(&b, "\t(%s) %s ;;\n", patterns[i], zshCompletes(v))
	}
	b.WriteString("\t(*) return 1 ;;\n\tesac\n}\n\n")

	b.WriteString("__game_flags() {\n\tlocal -a flags\n\tcase $1 in\n")
	for _, s := range specs {
		if len(s.flags) == 0 {
			continue
		}
		items := make([]string, len(s.flags))
		for i, f := range s.flags {
			items[i] = shQuote("-" + strings.ReplaceAll(f.name, ":", `\:`) + ":" + f.usage)
		}
		fmt.Fprintf(&b, "\t(%s) flags=(%s) ;;\n", shQuote(s.name), strings.Join(items, " "))
	}
	b.WriteString("\tesac\n\t(( $#flags )) && _describe -t flags 'flag' flags\n}\n\n")

	b.WriteString("# __game_args completes the arguments of command $1, or its flags when it takes no arguments and $2 is 1.\n")
	b.WriteString("__game_args() {\n\tcase $1 in\n")
	patterns, values = argCases(specs)
	for i, v := range values {
		fmt.Fprintf(&b, "\t(%s) %s ;;\n", patterns[i], zshCompletes(v))
	}
	b.WriteString("\t(*) (( $2 )) && __game_flags $1 ;;\n\tesac\n}\n\n")

	b.WriteString(`# __game_nsgo completes, after nsgo game, the rest of the line as __game_command does after game. Before that it completes nsgo's commands, or hands the line to a _nsgo of nsgo's own on $fpath: compinit registers this script for nsgo only when nothing came before it on $fpath, and one that comes after it still completes nsgo.
__game_nsgo() {
	if (( CURRENT > 2 )) && [[ $words[2] == game ]]; then
		shift words
		(( CURRENT-- ))
		__game_command
		return
	fi
	local -a own
	own=(${^fpath}/_nsgo(N))
	if (( $#own )); then
		autoload -Uz _nsgo
		_nsgo "$@"
		return
	fi
	if (( CURRENT == 2 )); then
`)
	fmt.Fprintf(&b, "\t\tcompadd -- %s\n\telse\n\t\t_files\n\tfi\n}\n\n", quoteAll(nsgoCommands, shQuote, " "))

	b.WriteString("_game() {\n\tif [[ $service == nsgo ]]; then\n\t\t__game_nsgo\n\telse\n\t\t__game_command\n\tfi\n}\n\n")

	b.WriteString("__game_command() {\n\tlocal -a commands\n\tcommands=(\n")
	for _, s := range specs {
		fmt.Fprintf(&b, "\t\t%s\n", shQuote(strings.ReplaceAll(s.name, ":", `\:`)+":"+s.summary))
	}
	b.WriteString(`	)
	if (( CURRENT == 2 )); then
		_describe -t commands 'game command' commands
		return
	fi
	# Go's flag package reads flags up to the first argument that is not one, or --, so walk them to see whether the word being completed is a flag, a flag's value or an argument.
	local cmd=$words[2] word name
	integer i=3
	while (( i < CURRENT )); do
		word=$words[i]
		if [[ $word == -- || $word != -?* ]]; then
			__game_args $cmd 0
			return
		fi
		name=${word#-}
		name=${name#-}
		if [[ $word != *=* ]] && __game_takes_value $cmd $name; then
			if (( i + 1 == CURRENT )); then
				__game_value $cmd $name
				return
			fi
			(( i++ ))
		fi
		(( i++ ))
	done
	if [[ $PREFIX == -*=* ]]; then
		name=${PREFIX%%=*}
		name=${name#-}
		name=${name#-}
		compset -P '*='
		__game_value $cmd $name
	elif [[ $PREFIX == -* ]]; then
		__game_flags $cmd
	else
		__game_args $cmd 1
	fi
}

if [[ $funcstack[1] == _game ]]; then
	_game "$@"
else
	compdef _game game
	(( $+_comps[nsgo] )) || compdef _game nsgo
fi
`)
	_, _ = io.WriteString(w, b.String())
}

// zshCompletes returns the zsh command that offers c.
func zshCompletes(c completion) string {
	if c.files {
		return "_files"
	}
	return "compadd -- " + quoteAll(c.words, shQuote, " ")
}

// writeBash writes the bash completion script for specs, which works in bash 3.2 as macOS ships it, where there is no compopt and the function is registered with -o filenames instead.
func writeBash(w io.Writer, specs []commandSpec) {
	var b strings.Builder
	b.WriteString("# bash completion for game, written by `game completion bash`, for game and, unless nsgo already has a completion, for nsgo game. Save it as game and as nsgo in bash-completion's completions directory, or save it anywhere and source that file from ~/.bashrc.\n\n")
	b.WriteString(`# __game_reply adds each line compgen writes for its arguments to COMPREPLY.
__game_reply() {
	local line
	while IFS= read -r line; do
		COMPREPLY+=("$line")
	done < <(compgen "$@")
}

# __game_files completes $1 as a path. Where compopt exists (bash 4 and later) it marks just these results as file names, so bash quotes them and adds / to a directory; bash 3.2 has no compopt, so there the whole function is registered with -o filenames instead.
__game_files() {
	__game_reply -f -- "$1"
	if type compopt >/dev/null 2>&1; then
		compopt -o filenames
	fi
}

`)
	b.WriteString("__game_takes_value() {\n\tcase \"$1:$2\" in\n")
	if p := valueFlagPattern(specs); p != "" {
		fmt.Fprintf(&b, "\t%s) return 0 ;;\n", p)
	}
	b.WriteString("\tesac\n\treturn 1\n}\n\n")

	b.WriteString("# __game_value completes $3 as the value of flag $2 of command $1.\n")
	b.WriteString("__game_value() {\n\tcase \"$1:$2\" in\n")
	patterns, values := valueCases(specs)
	for i, v := range values {
		fmt.Fprintf(&b, "\t%s) %s ;;\n", patterns[i], bashCompletes(v, `"$3"`))
	}
	b.WriteString("\tesac\n}\n\n")

	b.WriteString("__game_flags() {\n\tcase \"$1\" in\n")
	for _, s := range specs {
		if len(s.flags) == 0 {
			continue
		}
		names := make([]string, len(s.flags))
		for i, f := range s.flags {
			names[i] = "-" + f.name
		}
		fmt.Fprintf(&b, "\t%s) __game_reply -W %s -- \"$2\" ;;\n", shQuote(s.name), shQuote(strings.Join(names, " ")))
	}
	b.WriteString("\tesac\n}\n\n")

	b.WriteString("# __game_args completes $2 as an argument of command $1, or as its flags when it takes no arguments and $3 is 1.\n")
	b.WriteString("__game_args() {\n\tcase \"$1\" in\n")
	patterns, values = argCases(specs)
	for i, v := range values {
		fmt.Fprintf(&b, "\t%s) %s ;;\n", patterns[i], bashCompletes(v, `"$2"`))
	}
	b.WriteString("\t*) if [ \"$3\" = 1 ]; then __game_flags \"$1\" \"$2\"; fi ;;\n\tesac\n}\n\n")

	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.name
	}
	b.WriteString("_game() {\n\tlocal cur=${COMP_WORDS[COMP_CWORD]} cmd word name i\n\tCOMPREPLY=()\n")
	fmt.Fprintf(&b, "\tif [ \"$COMP_CWORD\" -eq 1 ]; then\n\t\t__game_reply -W %s -- \"$cur\"\n\t\treturn\n\tfi\n", shQuote(strings.Join(names, " ")))
	b.WriteString(`	cmd=${COMP_WORDS[1]}
	# bash splits -flag=value into -flag, = and value, so = before the word being completed, or as it, means a flag's value.
	if [ "$cur" = = ]; then
		name=${COMP_WORDS[COMP_CWORD-1]#-}
		__game_value "$cmd" "${name#-}" ""
		return
	fi
	if [ "$COMP_CWORD" -ge 3 ] && [ "${COMP_WORDS[COMP_CWORD-1]}" = = ]; then
		name=${COMP_WORDS[COMP_CWORD-2]#-}
		__game_value "$cmd" "${name#-}" "$cur"
		return
	fi
	# Go's flag package reads flags up to the first argument that is not one, or --, so walk them to see whether the word being completed is a flag, a flag's value or an argument.
	i=2
	while [ "$i" -lt "$COMP_CWORD" ]; do
		word=${COMP_WORDS[i]}
		case $word in
		-- | - | [!-]*)
			__game_args "$cmd" "$cur" 0
			return
			;;
		esac
		if [ "${COMP_WORDS[i+1]}" = = ]; then
			i=$((i + 3))
			continue
		fi
		name=${word#-}
		name=${name#-}
		case $word in
		*=*) ;;
		*)
			if __game_takes_value "$cmd" "$name"; then
				if [ $((i + 1)) -eq "$COMP_CWORD" ]; then
					__game_value "$cmd" "$name" "$cur"
					return
				fi
				i=$((i + 1))
			fi
			;;
		esac
		i=$((i + 1))
	done
	case $cur in
	-*) __game_flags "$cmd" "$cur" ;;
	*) __game_args "$cmd" "$cur" 1 ;;
	esac
}

`)
	fmt.Fprintf(&b, "\n# __game_nsgo completes nsgo's commands after nsgo and, after nsgo game, the rest of the line as _game does after game, by dropping nsgo from COMP_WORDS.\n__game_nsgo() {\n\tlocal cur=${COMP_WORDS[COMP_CWORD]}\n\tCOMPREPLY=()\n\tif [ \"$COMP_CWORD\" -eq 1 ]; then\n\t\t__game_reply -W %s -- \"$cur\"\n\t\treturn\n\tfi\n", shQuote(strings.Join(nsgoCommands, " ")))
	b.WriteString(`	if [ "${COMP_WORDS[1]}" != game ]; then
		__game_files "$cur"
		return
	fi
	local words=("${COMP_WORDS[@]:1}") cword=$((COMP_CWORD - 1))
	local COMP_WORDS COMP_CWORD
	COMP_WORDS=("${words[@]}")
	COMP_CWORD=$cword
	_game
}

# bash-completion loads a command's completion the first time the command is completed, so before nsgo is registered here, one of nsgo's own that it has not loaded yet is loaded; __game_loading keeps this script, when it is that one, from loading itself again.
if [ -z "${__game_loading-}" ] && ! complete -p nsgo >/dev/null 2>&1; then
	__game_loading=1
	if type _comp_load >/dev/null 2>&1; then
		_comp_load nsgo >/dev/null 2>&1
	elif type __load_completion >/dev/null 2>&1; then
		__load_completion nsgo >/dev/null 2>&1
	fi
	unset __game_loading
fi

if type compopt >/dev/null 2>&1; then
	complete -F _game game
	complete -p nsgo >/dev/null 2>&1 || complete -F __game_nsgo nsgo
else
	complete -o filenames -F _game game
	complete -p nsgo >/dev/null 2>&1 || complete -o filenames -F __game_nsgo nsgo
fi
`)
	_, _ = io.WriteString(w, b.String())
}

// bashCompletes returns the bash command that completes word, a shell expression such as "$3", as c.
func bashCompletes(c completion, word string) string {
	if c.files {
		return "__game_files " + word
	}
	return "__game_reply -W " + shQuote(strings.Join(c.words, " ")) + " -- " + word
}

// writeFish writes the fish completion script for specs. It completes nsgo game too, with completions for nsgo that each hold only after nsgo game, so a completion of nsgo's own keeps working beside them.
func writeFish(w io.Writer, specs []commandSpec) {
	var b strings.Builder
	b.WriteString("# fish completion for game, written by `game completion fish`, for game and for nsgo game. Load it with `game completion fish | source`, or save it as game.fish and as nsgo.fish in ~/.config/fish/completions.\n\n")
	b.WriteString(`# __game_tokens prints the words before the cursor as game reads them: after nsgo game, nsgo is dropped.
function __game_tokens
    set -l tokens (commandline -opc)
    if test (count $tokens) -ge 2; and test "$tokens[2]" = game
        set -e tokens[1]
    end
    printf '%s\n' $tokens
end

# __game_after_nsgo succeeds when the line is nsgo game followed by what game would complete.
function __game_after_nsgo
    set -l tokens (commandline -opc)
    test (count $tokens) -ge 2; and test "$tokens[2]" = game
end

function __game_needs_command
    test (count (__game_tokens)) -eq 1
end

function __game_using
    set -l tokens (__game_tokens)
    test (count $tokens) -ge 2; and test "$tokens[2]" = $argv[1]
end

`)
	b.WriteString("function __game_takes_value\n    switch \"$argv[1]:$argv[2]\"\n")
	var valueFlags []string
	for _, s := range specs {
		for _, f := range s.flags {
			if f.takesValue {
				valueFlags = append(valueFlags, fishQuote(s.name+":"+f.name))
			}
		}
	}
	if len(valueFlags) > 0 {
		fmt.Fprintf(&b, "        case %s\n            return 0\n", strings.Join(valueFlags, " "))
	}
	b.WriteString("    end\n    return 1\nend\n\n")
	b.WriteString(`# __game_in_flags succeeds when the command is $argv[1] and the word being completed may still be one of its flags: Go's flag package reads flags up to the first argument that is not one, or --, so it walks the words after the command, skipping a flag's value.
function __game_in_flags
    set -l tokens (__game_tokens)
    test (count $tokens) -ge 2; and test "$tokens[2]" = $argv[1]; or return 1
    set -l i 3
    while test $i -le (count $tokens)
        set -l word $tokens[$i]
        if contains -- $word - --; or not string match -q -- '-*' $word
            return 1
        end
        if not string match -q -- '*=*' $word; and __game_takes_value $argv[1] (string replace -r -- '^--?' '' $word)
            set i (math $i + 1)
        end
        set i (math $i + 1)
    end
    return 0
end

complete -c nsgo -n 'test (count (commandline -opc)) -eq 1' -a game
`)
	// The game completions are written once for game and once for nsgo, where each holds only after nsgo game.
	for _, c := range []struct{ command, guard string }{{"game", ""}, {"nsgo", "__fish_seen_subcommand_from game; and __game_after_nsgo"}} {
		writeFishCommand(&b, specs, c.command, c.guard)
	}
	_, _ = io.WriteString(w, b.String())
}

// writeFishCommand writes the completions of specs for command, each holding only when guard, a fish condition, succeeds, or always when guard is empty.
func writeFishCommand(b *strings.Builder, specs []commandSpec, command, guard string) {
	when := func(cond string) string {
		if guard == "" {
			return fishQuote(cond)
		}
		if cond == "" {
			return fishQuote(guard)
		}
		return fishQuote(guard + "; and " + cond)
	}
	if guard == "" {
		fmt.Fprintf(b, "\ncomplete -c %s -f\n", command)
	} else {
		fmt.Fprintf(b, "\ncomplete -c %s -n %s -f\n", command, when(""))
	}
	for _, s := range specs {
		fmt.Fprintf(b, "complete -c %s -n %s -a %s -d %s\n", command, when("__game_needs_command"), fishQuote(s.name), fishQuote(s.summary))
	}
	for _, s := range specs {
		cond := when("__game_using " + s.name)
		flagCond := when("__game_in_flags " + s.name)
		for _, f := range s.flags {
			fmt.Fprintf(b, "complete -c %s -n %s -o %s", command, flagCond, fishQuote(f.name))
			switch {
			case !f.takesValue:
			case f.value.files:
				b.WriteString(" -r -F")
			case len(f.value.words) > 0:
				fmt.Fprintf(b, " -x -a %s", fishQuote(strings.Join(f.value.words, " ")))
			default:
				b.WriteString(" -x")
			}
			fmt.Fprintf(b, " -d %s\n", fishQuote(f.usage))
		}
		switch {
		case s.args.files:
			fmt.Fprintf(b, "complete -c %s -n %s -F\n", command, cond)
		case len(s.args.words) > 0:
			fmt.Fprintf(b, "complete -c %s -n %s -a %s\n", command, cond, fishQuote(strings.Join(s.args.words, " ")))
		}
	}
}
