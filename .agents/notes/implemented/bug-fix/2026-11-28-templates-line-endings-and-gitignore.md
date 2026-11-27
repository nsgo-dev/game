# Agent Note: Template LF line-ending rules, .game/ gitignore, and CRLF-agnostic refs hashing

Status: implemented

## Problem

Every project template's `.gitattributes` listed only the `nsgo-scene` merge driver lines (`*.scene.yaml` and `*.scene.yml`), with no line-ending rule (`* text=auto eol=lf`). On Windows, where Git defaults to `core.autocrlf=true`, cloning a project created with `game new` checked text files out with CRLF, causing every template's byte-for-byte canonical YAML tests (`TestProjectFileIsValidAndCanonical`, `TestDataFilesAreCanonical`, etc.) to fail on unmodified files and causing `game index` (`pkg/refs`) to hash non-UID text files differently between Windows and Unix machines. In addition, template `.gitignore` files ignored `.nsgo/` and `dist/` but omitted `.game/`, so CLI-only users could commit `.game/assets-cache.yaml` (`nsgo-dev/game#1534`).

## Decision

Keep line endings consistent in Git while making the reference index resilient even if a checkout already contains CRLF:

- **Template `.gitattributes` and `game merge -setup`** (`internal/scenemerge/scenemerge.go`, `internal/gamecli/merge.go`, `template/*/template/.gitattributes`, `template/template/.gitattributes`, and their embedded copies under `internal/gamecli/_templates/projects/`):
  - Set `* text=auto eol=lf` at the top, followed by `binary` rules for every binary media extension the engine loads (images, compressed/HDR textures, audio, video, fonts, `.glb`, `.bin`), followed by `*.scene.yaml merge=nsgo-scene` and `*.scene.yml merge=nsgo-scene`.
  - `scenemerge.EnsureGitAttributes` (called by `game merge -setup`) upgrades existing projects idempotently: when `* text=auto eol=lf` is missing it prepends the wildcard rule before existing lines so project-specific overrides (such as `*.png binary` or `*.bat text eol=crlf`) retain Git's later-line precedence, and appends any missing binary media and scene merge lines.
- **Template `.gitignore`**: add `.game/` alongside `.nsgo/` and `dist/` in every template and enforce both `.gitattributes` and `.gitignore` invariants across all registered templates in `TestTemplateGitFiles` (`internal/gamecli/templates_test.go`).
- **CRLF-normalized text hashing in `pkg/refs`** (`pkg/refs/text.go`, `pkg/refs/scan.go`, `pkg/refs/plan.go`): `hashContent` normalizes `\r\n` to `\n` before SHA-256 for text files (files without a binary extension, containing no NUL byte, and valid UTF-8), while hashing binary extensions and non-UTF-8/NUL-bearing files verbatim. Both `(*Project).sha` and `(*planner).indexes` (for in-memory edited files) route through `hashContent`, so LF checkouts keep identical hashes while CRLF checkouts agree on `game.index.yaml` and `Project.Moved`.
- **Windows CRLF CI coverage** (`.github/workflows/test.yml`): `templates-windows` runs on `depot-windows-2025-4`, enables `git config --global core.autocrlf true` before checkout, commits `template/platformer2d/template` to a standalone Git repository, clones it with `git -c core.autocrlf=true clone`, and runs `go tool nsgo test ./...` with `GOWORK=off`.

## Alternatives considered

- **Relax canonical YAML tests in templates to normalize CRLF before `bytes.Equal`**: rejected because it would mask CRLF drift across every template and user project, still leave Git diffs noisy across OSes, and still break `game index` hashes. Enforcing LF via `.gitattributes` solves the root cause while keeping canonical checks strict.
- **Normalize CRLF in `pkg/refs` using an allowlist of text extensions only**: rejected because games may reference custom plain-text data or shader extensions; combining a binary-extension denylist with a NUL-free valid-UTF-8 check handles any UTF-8 text file safely while never altering binary files (including synthetic ASCII fixtures with binary extensions such as `.png` or `.bin`).

## Consequences

- **Benefits**: projects created from any template or upgraded via `game merge -setup` check out with LF line endings on Windows even under `core.autocrlf=true`, keeping canonical YAML tests green and `.game/` untracked; `game index` and `Project.Moved` stay stable across LF and CRLF checkouts without changing any existing LF hashes.
- **Costs**: `templates-windows` adds one `depot-windows-2025-4` CI job per push/PR, and `TestTemplateGitFiles` requires new templates to ship the full `.gitattributes` and `.gitignore` rules (matching `game merge -setup`).
