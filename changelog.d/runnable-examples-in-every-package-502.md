### Added

- `game` and every library package under it: runnable `Example` functions in `example_test.go`, shown with each package's Go doc and checked against their `// Output:` by `go tool nsgo test`, except examples that need an outside service or a running test, which are only compiled; commands (`package main`, such as `cmd/*` and `server/cmd/dedicatedserver`) and internal packages have none (#502).
