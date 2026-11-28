# YAML map events

`flow.ParseMapEvent` validates versioned map-event YAML with ordered pages and triggers (`action`, `touch`, `autorun`, `parallel`). `EventRunner` executes `say`, `wait`, `set` and `self` commands across fixed updates, and `EventSnapshot`/`Restore` saves a mid-command event as text. Unknown fields and commands fail at load.
