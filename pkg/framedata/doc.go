// Package framedata gives fighting games per-frame hitboxes, hurtboxes and hit properties for their moves, authored as versioned YAML and resolved deterministically once per fixed step.
//
// # Documents
//
// A Doc lists a character's moves. Each Move lasts Length frames (one frame per fixed step, numbered from 1 like published frame data) and holds Boxes, each active on an inclusive range of frames: hurtboxes (KindHurt) where the character can be hit and hitboxes (KindHit) that strike, with damage, hitstun, blockstun, knockback and a hit Group. Cancels open windows where a move may be cut short into other moves, on any frame or only after the move hit, was blocked or made contact. Parse decodes and validates a version 1 document, refusing unknown fields; Encode writes deterministic YAML that Parse reads back unchanged.
//
// Boxes are in points relative to the fighter's origin (usually between its feet), facing right, with Y growing downward like a core.Drawing. A fighter facing left mirrors them about its origin.
//
// # Resolving hits
//
// A Fighter plays one move at a time from its Doc and returns to its Neutral move when a move that does not loop ends. A World holds the fighters of a match; World.Step advances every fighter one frame, then tests each active hitbox against every other fighter's active hurtboxes. All hits of a step are found from the positions before any of them is applied, so two fighters that strike each other on the same frame trade. Fighters are visited in slice order and boxes in document order, so the same inputs always produce the same hits in the same order.
//
// A move instance hits each opponent at most once per hit Group (a multi-hit move gives its hits different groups) and at most once per step. A hit puts the opponent in hitstun, or blockstun when it is Blocking, interrupts its move with its Neutral move, and lets the attacker's cancel windows that wait for contact open.
//
// The gamedebug package's ShapeOverlay.DrawFighterBoxes draws a fighter's active boxes as wireframes.
package framedata
