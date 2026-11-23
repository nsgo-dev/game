# NSGo game template module

The `github.com/nsgo-dev/game/template` module ships the starter project in its [`template/` directory](template/). Use the [getting-started guide](../docs/en/getting-started.md) to create a game with `game new`, which embeds a copy of this starter, or with `nsgo init -template`. After changing the starter, copy it to `packages/game/cmd/game/_templates/starter`, whose tests fail while the two differ.

The repository workspace links the starter to the local game library. From the repository root, run its tests and open it on the desktop with:

```sh
go tool nsgo test ./template/template
go tool nsgo dev ./template/template
```
