# nsgo-dev/game

A game layer for NSGo apps, written in Go: the root module `github.com/nsgo-dev/game` (`loop.go`, `cmd/`, `internal/`, and library subpackages under `pkg/`), the website under `apps/website`, examples under `examples`, docs under `docs/en` and `docs/zh`. The plan is nsgo-dev/nsgo#1047 and this repository's issues. NSGo's own rules (`AGENTS.md` in nsgo-dev/nsgo) apply here too: Go only, no hand-written C, purego or syscall for system APIs, the nsgo tool for vetting and tests.

## Every file a person writes is text

As in Godot, whose scenes and resources are text files, and unlike engines that keep scenes, prefabs and materials in binary formats, every file a game's author creates or edits is plain text that a person and an AI can read, edit and diff in review.

- Write the formats this repository defines in YAML: scenes, prefabs, materials, animations and rigs, particle and sound definitions, input bindings, localization tables, data tables, project and build settings, mod manifests, and input recordings.
- Keep them stable for review: a documented schema with a version field, unknown fields refused, deterministic output (fixed key order, no timestamps or random IDs that change on every save), and a round trip through load and save that changes nothing.
- Read other tools' exports in the form those tools write, such as Tiled's and LDtk's maps, Aseprite's and TexturePacker's atlases and glTF, and never require converting them into a binary form.
- Files a program writes for itself, such as save slots, crash reports and caches, are text too (YAML or JSON), never `encoding/gob` or a custom binary layout.
- Binary is only for media that is binary by nature (images, audio, fonts, models' buffers, compressed textures) and for data on the wire, such as multiplayer packets.

## Working here

- Each task in its own Git worktree and branch, never the main checkout; conventional commits; a PR per issue.
- Vet and test from the repository root with `go tool nsgo check` and `go tool nsgo test`, never `go vet` or `go test` directly.
- Keep `docs/en` and `docs/zh` equivalent, and update them with the code.
- Record a user-visible change in a fragment under `changelog.d/`, never in `CHANGELOG.md` directly (`changelog.d/README.md`).
- Give a non-trivial change (behavior, architecture, a cross-file contract, process and tooling, testing strategy, or an on-disk, wire or configuration format) a note under `.agents/notes`, as the write-notes-like-deepseek skill (`.agents/skills/write-notes-like-deepseek/SKILL.md`) describes, and check it with `bun run verify-notes`.
- Run every CI job on Depot's runners (`depot-ubuntu-24.04`, `depot-ubuntu-24.04-4`, `depot-macos-15`), declared in `.github/actionlint.yaml`; `check.yml` fails a workflow that uses a GitHub-hosted label.
