# Agent Note: shell completion for nsgo game

Status: implemented

## Problem

`nsgo game` is the documented form of the game command ([nsgo game as the documented form](../process/2026-11-25-nsgo-game-main-form.md)), but the scripts `game completion zsh|bash|fish` writes completed only a command named `game`. Typed as `nsgo game`, the shell completed `nsgo`, which has no completion of its own, so nothing after `nsgo game` completed unless the user defined a `game` wrapper function.

## Decision

The scripts (`packages/game/cmd/game/completion.go`) complete `nsgo` too. After `nsgo` they offer `game` and `nsgo`'s built-in command names (`nsgoCommands`) as plain words, after another `nsgo` command file paths (what the shell offers without a completion), and after `nsgo game` they hand the line to the existing game logic with `nsgo` dropped: zsh shifts `words` and decrements `CURRENT`, bash runs `_game` with local copies of `COMP_WORDS` and `COMP_CWORD` that omit `nsgo`, and fish's helpers read the words through `__game_tokens`, which drops `nsgo` when the second word is `game`. `game …` behaves as before, and `go tool game` stays uncompleted, since there the shell completes `go`.

The scripts never replace a completion of `nsgo`'s own:

- zsh: the sourced script runs `compdef _game nsgo` only when `$_comps[nsgo]` is unset. From `$fpath`, `#compdef game nsgo` makes compinit register it with `compdef -n`, which keeps a `_nsgo` found earlier; when a `_nsgo` sits later on `$fpath`, `__game_nsgo` hands every line but `nsgo game …` to it.
- bash: it registers for `nsgo` only when `complete -p nsgo` fails, and first asks bash-completion's loader (`_comp_load`, or `__load_completion` before 2.12) for a completion of `nsgo` it has not loaded yet. `__game_loading` keeps the script from loading itself again when it is that completion, saved as `nsgo`.
- fish: completions add up rather than replace, so every entry for `nsgo` is guarded by `__fish_seen_subcommand_from game; and __game_after_nsgo`, and the one at the first word offers only `game`, without `-f`, so a completion of `nsgo`'s own is untouched.

## Testing

`completion_test.go` runs every game case again prefixed with `nsgo `, plus `nsgo `, `nsgo g` and `nsgo dev `, in real zsh (sourced and from `$fpath`), bash (on `PATH` and macOS's `/bin/bash` 3.2) and fish. `TestZshCompletionKeepsOwnNsgo` and `TestBashCompletionKeepsOwnNsgo` check that an earlier registration, an earlier or later `_nsgo` on `$fpath`, and a completion bash-completion would load all keep completing `nsgo`.

## Alternatives considered

- **A wrapper `game` function in the docs, as before** — needs no change to the scripts, but every user writes it, and `nsgo game <TAB>`, the form the docs show, still completes nothing.
- **Registering for `nsgo` unconditionally** — simpler, but silently replaces a completion NSGo or the user may add later; NSGo has none today, so the guard costs little.
- **Completing `nsgo`'s own commands' flags too** — would duplicate `nsgo`'s flag definitions in the game layer and go stale with each NSGo release; plain command words are enough until NSGo writes its own completion.

## Consequences

- **Benefits**: `nsgo game <TAB>` and everything after it complete like `game <TAB>` in all three shells, with nothing beyond saving the script, plus a second file name for bash-completion and fish.
- **Costs and known limits**: bash-completion and fish load a completion from the file named after the command, so the docs link the script as `nsgo` too; a zsh `.zcompdump` written for an older `#compdef game` script must be deleted before compinit registers `nsgo`. When NSGo ships a completion of its own, it will need its own delegation to `nsgo game`, and this script's `nsgo` part becomes the fallback.
