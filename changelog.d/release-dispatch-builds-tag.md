### Fixed

- Release workflow: a `workflow_dispatch` upload builds the editor from the release tag it names, runs the full test and vet suite against that tag before uploading, and refuses a tag that does not exist or has no `cmd/editor` (#1555).
