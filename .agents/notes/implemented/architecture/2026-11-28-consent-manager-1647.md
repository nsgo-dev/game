# Agent Note: purpose-based consent records

Status: implemented

## Problem

Analytics had one bare consent string, with no purpose separation, policy version or timestamp. Crash and personalization features had no shared gate.

## Decision

Add `liveops.Consent` with four purposes, versioned JSON records, explicit source and timestamp, `OptIn` and `OptOut` regimes, and policy-version prompting. Unknown or malformed records are ignored and fail closed under opt-in.

## Consequences

Liveops features can share one storage-backed consent manager while retaining independent choices. Existing Analytics remains compatible. Consent UI and wiring CrashReporter, personalization and marketing are follow-up integration work.

## Alternatives considered

One global boolean was rejected because players may allow diagnostics while refusing analytics. Storing a date of birth or free-form policy text was rejected because the manager needs only a policy version and purpose choice.
