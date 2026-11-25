# NSGo game template module

The `github.com/nsgo-dev/game/template` module ships the starter project, the default template, in its [`template/` directory](template/). Every other template is a module of its own in `template/<name>/`, `github.com/nsgo-dev/game/template/<name>`, with its project in `template/<name>/template/`. [Templates](../docs/en/templates.md) lists them and describes how to add one. Use the [getting-started guide](../docs/en/getting-started.md) to create a game with `game new`, which embeds a copy of each template, or with `nsgo init -template`. After changing a template, copy it to `packages/game/cmd/game/_templates/projects/<name>`, whose tests fail while the two differ.

The repository workspace links every template to the local game library. From the repository root, run its tests and open it on the desktop with:

```sh
go tool nsgo test ./template/template
go tool nsgo dev ./template/template
```

Another template runs the same way from `./template/<name>/template`.
