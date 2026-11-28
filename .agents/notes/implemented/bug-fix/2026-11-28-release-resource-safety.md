# Agent Note: Release resource safety checks

Status: implemented

## Problem

Every file under `resources/` is copied into release packages, so a project could accidentally publish credentials or follow a symbolic link outside the project.

## Decision

`releaseplan.CheckResources` walks `resources/` with `WalkDir` and refuses symlinks, dotfiles, known credential names, private-key PEM headers, and service-account JSON. `export.allowResources` permits an intentional dot path while retaining a warning. The CLI checks before dry-run or `nsgo build`, and editor readiness uses the same function.

## Alternatives considered

Silently filtering unsafe files in the packager would avoid a failed build but could produce an incomplete game and would not protect projects pinned to older NSGo versions. Scanning only filenames would miss private keys hidden in generic text files.

## Consequences

Release builds fail before package creation and identify only the offending path and rule. Projects with intentional dotfiles need an explicit allow list and a person review. The same readiness items are available to the editor and CLI.
