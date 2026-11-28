# Agent Note: game new source copying

Status: implemented

## Problem

`game new` could only copy the embedded template registry. Developers and agents could not start from an example or an existing project without manually copying files, and copied machine-local state could leak into the new project.

## Decision

`game new -from <directory> <destination>` walks an arbitrary source directory, renders the same project identity files as templates, excludes VCS, editor, environment, workspace, dependency-cache, and temporary files, and records an `origin` object in the destination's canonical `game.yaml`. Existing `-template` creation records the embedded template module and name in the same field.

## Alternatives considered

Adding every example and pack to the template registry would keep the copy logic narrow, but it would make the registry grow with project content and still require a separate path for local directories. Copying with an external shell command would vary by platform and could follow machine-local files, so the CLI owns deterministic filtering.

## Consequences

Projects preserve their source context for later tooling while remaining portable. The source path is recorded as supplied, so callers should avoid placing secrets in command arguments. Directory copying currently treats packs as source directories and preserves their files without invoking pack installation semantics.
