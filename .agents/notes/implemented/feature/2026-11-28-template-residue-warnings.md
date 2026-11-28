# Agent Note: Template residue warnings

Status: implemented

## Problem

Projects created from templates could keep the starter rules, data, name, and README unchanged without any signal during `game check`.

## Decision

Projects may record an `origin` with a template name and optional `keep` categories. `game check` compares exact bytes against the embedded template and emits advisory `Warning` problems for unchanged rules/data, name, and README content. Warnings are printed with file locations but do not make the command fail. Unknown origin kinds and keep categories remain schema errors.

## Alternatives considered

A changed-line threshold would be tolerant of formatting but could miss an unchanged starter file, so exact byte comparison is used. Blocking the check would punish deliberate template copies, so residue findings remain warnings with explicit opt-outs.

## Consequences

The CLI now surfaces starter residue to editor and agent consumers that read check output. Projects without an origin remain unchanged, and the comparison only uses templates embedded in the current CLI version.
