# Agent Note: strict benchmark baseline checks

Status: implemented

## Problem

Benchmark evaluation listed unbaselined measurements as informational output. A newly added benchmark could therefore bypass the quality gate until someone manually edited the baseline.

## Decision

Add `EvaluateStrict` and the `game bench -strict` flag. Strict evaluation promotes every measured name absent from the baseline into a failed result while preserving the existing permissive mode. The baseline YAML contract remains unchanged for compatibility.

## Consequences

CI and local users can opt into requiring a baseline entry for every measured benchmark. Existing jobs stay permissive until their baseline covers the broader benchmark set.

## Alternatives considered

Changing default evaluation to strict would make current partial baselines fail immediately. The opt-in flag allows the baseline expansion to be staged safely.
