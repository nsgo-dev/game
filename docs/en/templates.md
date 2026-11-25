# Templates

A new project starts from a template: a small, complete game that builds, runs and passes its tests before you change a line. `game new` writes one into a new directory, and NSGo's `nsgo init -template` writes the same files from the template's Go module.

## The templates

| Template | What it is | `nsgo init -template` module |
| --- | --- | --- |
| `starter` | A tap-to-score 2D game: an animated sprite on a fixed-step loop, a native HUD and headless tests, the smallest project to grow from. The default. | `github.com/nsgo-dev/game/template` |

`go tool game new -list` prints the same list from the command itself, the default first.

## Create a project from a template

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -list
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template starter mygame
```

Without `-template`, `game new` uses `starter`. A name that is not in the list is refused with the names that are. Shell completion for `go tool game` ([Command Line](cli.md#completion)) completes `-template` with the same names. [Command Line](cli.md#new) describes the other flags.

NSGo's own command writes a template from its module without the `game` tool, with an NSGo command that supports [Go module templates](https://nsgo.dev/docs/getting-started/#templates-from-go-modules), such as `go install github.com/nsgo-dev/nsgo/cmd/nsgo@v0.3.0`:

```sh
nsgo init -template github.com/nsgo-dev/game/template -module example.com/mygame mygame
```

Use the module the table names for each template, and add `@<version>` to pin one. The project's `game.yaml` and `nsgo.yaml` keep the template's own game name and identifier until you edit them, where `game new` sets them from the directory or `-name`.

## How the templates are laid out

Each template is a Go module of its own in this repository, because `nsgo init -template` writes the `template` directory at the root of the module it is given. `starter` is the `template/` module itself; every other template is the module `github.com/nsgo-dev/game/template/<name>` in `template/<name>/`:

```text
template/
├─ go.mod                     module github.com/nsgo-dev/game/template
├─ template/                  starter
└─ <name>/
   ├─ go.mod                  module github.com/nsgo-dev/game/template/<name>
   └─ template/               the <name> template
      ├─ go.mod.tmpl
      ├─ go.sum
      ├─ game.yaml
      ├─ nsgo.yaml
      ├─ main.go
      ├─ …_test.go
      ├─ README.md
      └─ .gitignore
packages/game/cmd/game/
├─ templates.go               the registry: one entry per template
└─ _templates/projects/
   ├─ starter/                a copy of template/template
   └─ <name>/                 a copy of template/<name>/template
```

`game new` embeds the copies under `_templates/projects`, so it works from the module cache without this repository. The repository's `go.work` uses every template module, so from a checkout `go tool nsgo dev ./template/<name>/template` runs a template and `go tool nsgo test ./template/<name>/template` tests it against the local game layer.

## Add a template

A template is added in one pull request of its own:

1. Create the module: `template/<name>/go.mod` declaring `module github.com/nsgo-dev/game/template/<name>` with the `go` line of `template/go.mod`, and the project in `template/<name>/template/`. Its name is lower-case letters, digits and `-`, starting with a letter, and not `template`.
2. Add `./template/<name>` to the `use` list of `go.work`.
3. Copy the project into the `game` command, from `packages/game/cmd/game`: `cp -R ../../../../template/<name>/template _templates/projects/<name>`.
4. Add one entry to `projectTemplates` in `packages/game/cmd/game/templates.go`: `newProjectTemplate("<name>", templateModuleRoot+"/<name>", "<one line saying what the game is>")`.
5. Add a row for the template to the table above, in `docs/en/templates.md` and `docs/zh/templates.md`, and a fragment under `changelog.d/`.

The project in `template/<name>/template/` follows these rules, which the tests check where they can:

- `go.mod.tmpl` starts with `module {{.Module}}`, requires a published version of `github.com/nsgo-dev/game/packages/game` that has every package the template uses, and has `tool github.com/nsgo-dev/nsgo/cmd/nsgo`. It is the only file with a template action: `game new` and `nsgo init` render `nsgo.yaml`, `main.go` and any other `.tmpl` file too, but CI renders only `go.mod.tmpl` and the template must also build as it is in the workspace, so each of those must render to itself, and `main.go` cannot hold `{{`, as a composite literal such as `[]T{{...}}` does; put such code in another Go file. Other files are copied as they are and may hold any bytes. A file named `go.mod` would make the directory a module of its own, which neither `go:embed` nor the workspace can include.
- `go.sum` holds the sums of the pinned modules. Write it by turning `go.mod.tmpl` into a `go.mod` in a copy of the template outside the repository and running `GOWORK=off go mod tidy` there.
- `game.yaml` is canonical, as `go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check` reports, and `nsgo.yaml` agrees with it; `game new` replaces the `name` and `id` of both.
- `main.go`, `README.md`, `.gitignore` and tests are there, and `go run ./cmd/game index ../../template` from `packages/game` has nothing left to do.
- Every file the template's author writes is text, as for any project in this repository.

The tests of `packages/game/cmd/game` fail when any of these steps is missing: when the copy under `_templates/projects` differs from the module's `template` directory by a byte, when a `template/*/template` directory has no registry entry, when a registry entry has no copy, module or `go.work` entry, and when this page or its Chinese counterpart has no row naming a template and its module. They also create a project from every template, build it against the local game layer and run its tests.

CI's `templates` job needs no edit for a new template: it lists `template/template` and every `template/*/template` directory and tests each template the way a developer's project gets it: it copies the template out of the workspace, renders `go.mod` for `example.com/<name>game`, and runs `go tool nsgo test ./...` with `GOWORK=off`, so the project builds against the game layer version `go.mod.tmpl` pins rather than this checkout. A template that needs a package not yet published bumps that pin once the package is on `main`.
