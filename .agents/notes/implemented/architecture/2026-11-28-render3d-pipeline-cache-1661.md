# Agent Note: render3d pipeline cache attachment

Status: implemented

## Problem

Renderers created a fresh GPU device without attaching NSGo's persistent pipeline cache, so shader artifacts were compiled on every launch.

## Decision

Expose `render3d.AttachPipelineCache`, which stores native artifacts in `<Scope.CacheDir>/pipelines` and uses an in-memory cache on targets with no cache directory. The helper attaches the cache to the device and returns it for stats inspection.

## Consequences

Applications can persist pipeline artifacts across launches and inspect cache hits and compilations. Renderer warm-up and asynchronous custom-material compilation remain caller-controlled follow-up work.

## Alternatives considered

Attaching a package-global cache was rejected because games can host multiple app scopes and tests need isolated cache directories. The helper keeps ownership explicit at device setup.
