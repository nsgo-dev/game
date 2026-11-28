# Agent Note: Conditions on scene connections

Status: implemented

## Problem

A scene connection always ran its handler when its event fired, so "open the door only if the player holds the key" needed a Go handler that checked the condition itself (#1753). The issue asks for a condition next to the connection, written in the scene file, with built-in actions to follow; this change covers only the condition.

## Decision

`ConnectionDoc.When` (`pkg/sceneformat/format.go`) holds an optional condition, written as `when:` and left out when empty, so existing scene files load and save byte for byte as before. The syntax (`pkg/sceneformat/rules.go`) is deliberately small: `true`, `false`, a bare name read as a bool, or `name op value` separated by spaces, where a number takes `<`, `<=`, `>`, `>=`, `==` and `!=`, and a string or bool takes `==` and `!=`.

- Validate parses every `when` and refuses one it cannot parse, so a typo in the operator or the shape fails when the scene loads.
- Connect stores the parsed expression on the binding. The values come from `RuleContext`, maps of numbers, strings and bools a game hands to `Signals.SetRuleContext`; Signals keeps the maps, not copies, and dispatch evaluates the condition each time the event fires, so the game updates a value by writing its map.
- A false condition skips the handler, and a once connection is marked done only when its handler runs, so it stays connected until it fires with the condition holding.
- A name the context has no value of, or a value of the wrong type, skips that handler and is returned from Emit or Flush, joined, after the rest of the event's handlers and listeners ran. Dispatch now returns that error instead of nothing.
- Scene merge treats `when` as a scalar field of the connection (`mergeConnections`, `connectionEqual`), and the editor keeps it through its connection form, equality check and conflict resolution, since the form has no input for it yet.

## Alternatives considered

### Wait for the shared expression evaluator (#1364)

Strongest argument: the issue names #1364 as the evaluator, and one language for dialogue, the console and scene files avoids two syntaxes to learn.

Rejected for now because #1364 is open and without any evaluator the field would be stored but ignored, which silently runs a conditional rule unconditionally. The small syntax is a subset of what an expression language accepts, so switching `parseWhen` to #1364 later keeps every file that parses today.

### Ignore an unevaluable condition, or treat it as false, without an error

Strongest argument: dispatch had no error path, and handlers nest, so returning errors touches Emit and Flush.

Rejected because a name typed wrong in the file would make a rule never fire with nothing to say why. Load-time checking of names needs declared game variables (#1754), so until then the error surfaces at the first event.

### A separate rules file

Strongest argument: event sheets in Construct 3 keep conditions and actions apart from the layout.

Rejected because it splits one rule across two files and two merge paths; keeping the condition on the connection keeps scene data deterministic and the editor's connection list the single place to edit it.

## Consequences

- Not delivered from #1753: the built-in `scene` and `game` handlers, expression arguments, load-time checking of names, the editor input for `when`, and the example collect-the-coins scene.
- A game that writes `when` must call `SetRuleContext`; otherwise every condition other than `true` and `false` fails and Emit reports it.
- The scene JSON schemas under `website/public/schemas` gain the `when` property through `tools/gameschema`.
