# Agent Note: Escape glob metacharacters and validate names in manifest -embed

Status: implemented

## Problem

`pkg/tooling/manifest` emits one `//go:embed` directive per asset in `Options.Embed` mode (`game manifest -embed`). Previously, `formatEmbedPattern` only quoted paths containing whitespace, quotes, backticks, or backslashes, leaving `path.Match` glob metacharacters (`[`, `*`, `?`) unescaped. Because Go's `//go:embed` always interprets each pattern as a `path.Match` glob (even when quoted), bracketed asset names such as `hero[1].txt` silently matched a sibling `hero1.txt` and embedded the wrong bytes, or failed at `go build` time when no plain sibling existed. In addition, `Scan` accepted filenames that Go's `//go:embed` (`cmd/go/internal/load.resolveEmbed` and `isBadEmbedName`) unconditionally rejects, deferring errors until compilation.

## Decision

`pkg/tooling/manifest` escapes glob metacharacters in `formatEmbedPattern` and validates asset paths and `Options.EmbedDir` in `Embed` mode during `Scan` / `normalizeOptions`:

- `escapeEmbedGlob` replaces `[`, `*`, and `?` with single-character classes `[[]`, `[*]`, and `[?]` before applying directive quoting in `formatEmbedPattern`. Outside a character class, `]` is already literal in `path.Match` and `filepath.Match`.
- In `Embed` mode, `validateEmbedPath` checks every scanned asset path and any non-empty `Options.EmbedDir` against Go's `//go:embed` rules: `fs.ValidPath` (excluding `"."`), valid UTF-8, no all-dot or trailing-dot path element, no reserved VCS directory (`.bzr`, `.hg`, `.git`, `.svn`), no Windows reserved device name (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`), and every rune accepted by Go's `module.CheckFilePath` (`[0-9A-Za-z]`, ASCII space, `!#$%&()+,-.=@[]^_{}~`, or non-ASCII `unicode.IsLetter`).
- URI mode (`Embed: false`) does not apply `//go:embed` name restrictions because URI entries are ordinary Go string constants.

## Alternatives considered

- **Backslash-escaping metacharacters (`\[`, `\*`, `\?`) as `cmd/go`'s `str.QuoteGlob` does**: simpler visually, but `filepath.Match` disables backslash escaping on Windows and treats `\` as a path separator, breaking cross-platform manifests. Single-character classes (`[[]`, `[*]`, `[?]`) work identically in `path.Match` and `filepath.Match` on Unix and Windows.
- **Importing `golang.org/x/mod/module` for `CheckFilePath`**: would avoid reimplementing the character allowlist, but `golang.org/x/mod` is not a dependency of `github.com/nsgo-dev/game` and `isBadEmbedName`'s rules (about 40 lines of pure Go) are stable and self-contained.
- **Allowing `*` and `?` through `Scan` in `Embed` mode relying on `[*]` / `[?]`**: rejected because `cmd/go/internal/load.resolveEmbed` still runs `isBadEmbedName` on the matched filename after glob expansion and fails the build with `cannot embed file ...: invalid name`. Keeping `escapeEmbedGlob` defensive for `*` and `?` while rejecting them in `validateEmbedPath` prevents unbuildable output today and remains safe if Go ever relaxes `CheckFilePath`.

## Consequences

- **Benefits**: Bracketed asset names (`hero[1].png`, `tile[a-z].txt`, `dir[0]/frame[1].txt`, `space [1].txt`) compile and embed their own exact bytes on every OS, and unembeddable asset names fail immediately in `Scan` / `game manifest -embed` with an error naming the offending asset.
- **Costs and known limits**: `validateEmbedPath` mirrors Go's current `module.CheckFilePath` / `isBadEmbedName` rules; if a future Go release changes the allowed character set, `embedFileRuneOK` will need a matching update. Filesystem-level embed restrictions (such as a nested `go.mod` boundary or irregular files) are still enforced by the Go compiler.
