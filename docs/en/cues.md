# cues

`github.com/nsgo-dev/game/pkg/cues` keeps gameplay presentation mappings in a versioned YAML file. The package owns deterministic parsing and cue lifecycle bookkeeping. A game supplies `cues.Hooks` to connect the mapping to its particle, audio, camera and sprite systems.

```yaml
version: 1
cues:
  - tag: cue.hit.fire
    once: true
    particles:
      file: fx/hit.yaml
      attach: chest
    sound:
      file: sfx/hit.ogg
    shake:
      amplitude: 0.5
      duration: 100ms
  - tag: cue.status.burning
    whileActive: true
    animation: burning
```

Each cue has a dotted `tag` and sets `once`, `whileActive`, or both. A one-shot is keyed by tag, target, instance and simulation step, so dispatching the same event again while replaying a rollback does not play it twice. A while-active cue remains active until an `EventRemoved` arrives.

`NewPlayer` validates the set and builds a client-side dispatcher. `Dispatch` starts handlers and tracks active cues. `Reconcile` accepts the while-active tags from restored gameplay state, stopping missing handlers and starting newly present ones in deterministic order. `Reset` stops all active cues and clears replay deduplication. The package has no sound, rendering or scene dependency and can be loaded by headless code without constructing a player.

`Hooks` callbacks receive the parsed handler and the original event. `Stop` is called once when an active cue is removed or disappears during reconciliation. Resource loading, pooling and target attachment stay in the game layer.
