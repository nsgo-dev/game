# Agent Note: Check relative Markdown fragments with the website's own slugger

Status: implemented

## Problem

Guide links to `editor.md` retained heading fragments from before issue numbers were added to the headings, so they opened the page at the top instead of the intended section. Existing repository checks validated documents and file links but not fragments between Markdown pages, allowing a heading edit to break every inbound link without failing CI (#1616).

## Decision

- `apps/website/scripts/check-guide-links.ts` parses Markdown with the website's existing `unified`, `remark-parse`, `remark-gfm`, `remark-rehype` and `rehype-slug` pipeline. Target heading IDs therefore come from `rehype-slug` itself, including punctuation removal, non-Latin text and duplicate-heading suffixes, rather than a second implementation of GitHub's slug rules.
- `bun run test` runs focused fixture tests and a repository-wide test. The checked source files are every Markdown file under `docs/en`, `docs/zh` and `skills`, each direct `examples/*/README.md`, and every `template/**/README.md`.
- Every relative link whose target path ends in `.md` and has a non-empty fragment must resolve to an existing file containing that heading ID. External URLs, absolute paths, same-page fragments and links without fragments are outside this cross-page check. Percent-encoded paths and fragments are decoded before resolution, matching the URLs emitted by the Markdown pipeline.
- Failures identify the source file and line, original target and missing heading or file. Parsing and heading results are cached so repeated links do not parse the same target repeatedly.

## Alternatives considered

- **Add a Go check under `internal/repocheck` using regular expressions**: would place the check beside other repository documentation tests, but Markdown links and headings have nested and reference forms that a hand-written parser would handle inconsistently. Rejected in favor of the website's existing parser and slugger.
- **Reimplement GitHub slug generation from heading text**: avoids running `rehype-slug`, but duplicates punctuation, Unicode and duplicate-heading behavior and can drift independently from the website. Rejected because using the same plugin makes the rendering pipeline the single source of truth.
- **Remove issue numbers from headings or introduce explicit stable IDs**: would make these particular fragments less likely to change, but requires a new documentation convention or rendering support and still leaves other renamed headings unchecked. The issue makes this optional; the general fragment check addresses the underlying regression without changing heading text.

## Consequences

- **Benefits**: the broken English and Chinese editor links resolve again, additional occurrences of the same stale fragments are repaired, and future heading renames fail the existing website test job when they break any relative Markdown fragment in the specified documentation surfaces.
- **Costs**: the repository-wide fixture adds a full documentation parse to `bun run test` and has a 60-second timeout to accommodate cold runs. Same-page fragments and non-Markdown destinations remain outside this check's scope.
- **Verification**: `bun test scripts/check-guide-links.spec.ts` covers accepted GitHub/rehype-slug IDs, an unknown fragment, every required source scope and the real repository; the repository test failed with all 21 stale links before the fixes and passes afterward. The final change also passes `bun test --timeout 60000` (39 tests), `bun run build` (including TypeScript, API anchors and Pagefind), `ubuild mise exec go@1.27.3 -- go tool nsgo check`, `go run ./tools/changelog check`, `bun run verify-notes` and `git diff --check`.
