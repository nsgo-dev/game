### Security

- `cmd/game`: projects that `game new` creates, and the template modules under `template/`, now require `github.com/nsgo-dev/game v0.2.1` instead of `v0.1.0`, so a new project starts with the 0.2.1 security fixes. Existing projects should run `go get github.com/nsgo-dev/game@v0.2.1`.
