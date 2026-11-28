# Agent Note: Asset generator kind boundary coverage

Status: implemented

## Problem

The asset generator already recognizes the expanded texture, model, animation and tiles kinds, but only images and sound have safe in-memory validators. Without explicit acceptance coverage and documentation, a future change could accidentally send an unvalidated payload to the editor or make the settings schema promise unsupported output.

## Decision

Keep the expanded kinds visible in configuration and source metadata while `Generatable` remains restricted to images and sound. `GenerateAsset` refuses unsupported kinds before invoking the generator transport. The editor docs state this boundary and the sidecar's multi-file coverage contract.

## Alternatives considered

Removing the four kinds until validators exist would avoid an unsupported path but would break forward-compatible settings and source metadata already written by projects. Treating arbitrary bytes as candidates would violate the untrusted-payload boundary.

## Consequences

Projects can declare future generator kinds without losing schema compatibility, and unsupported output remains blocked before transport calls. Each future validator can opt a kind into `Generatable` with focused payload and pick tests.

## Verification

Existing asset generator kind tests cover all recognized kinds, refusal before transport for models, and deterministic source sidecar round trips. The editor test suite and `go tool nsgo check` remain the validation path for this boundary.
