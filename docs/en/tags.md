# tags

`github.com/nsgo-dev/game/pkg/tags` provides a project-wide vocabulary of dotted gameplay tags. A registry is a versioned YAML file with deterministic encoding:

```yaml
version: 1
tags:
  - name: status
    description: broad state
  - name: status.debuff.stun
  - name: weapon.fire
```

Parents are implied. Declaring `status.debuff.stun` makes `status` and `status.debuff` resolvable for matching, while only declared names are written to the file. Names use ASCII letters, digits, `_`, `-` and dots and must be unique.

`Registry.Resolve` interns a name to a `Tag`. `Container` stores reference-counted grants, so two systems can grant the same tag and remove their grants independently. `HasExact` checks only the granted tag. `Has` also matches descendants, so a `status.debuff.stun` grant satisfies a `status` query.

`Query` evaluates `All`, `Any` and `None` predicates and can compose nested `AllOf`, `AnyOf` and `NoneOf` predicates. `Registry.QueryNames` resolves the name form used by data loaders and refuses undeclared names. Matching reads existing container state without allocating.
