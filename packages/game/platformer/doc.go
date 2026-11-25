// Package platformer provides a platformer character controller on top of physics.CharacterBody: coyote time, jump buffering, variable jump height, wall slides and wall jumps, each with its own tunable timing.
//
// A Controller wraps a CharacterBody and is stepped once per fixed game.Loop step with that step's Input. It reads the contacts the body's previous Move reported (Grounded, OnLeftWall, OnRightWall), sets the body's velocity and moves it with CharacterBody.Move (or CharacterBody.MoveWorld via StepWorld to collide against a physics.World's bodies, tiles and static rigid bodies alongside slopes and moving platforms). Coordinates follow package physics: X increases to the right and Y increases downward.
//
// Timings are measured in step time. An event (being on the ground, pressing jump, touching a wall, wall jumping) opens a window that stays open on later steps while less than its duration has passed since the event's step, so the same Config plays alike at any fixed rate and a test can count the steps a window lasts. Each step is allowed 1ns of slack because game.Loop's dt is truncated to whole nanoseconds, so 100ms at 60 Hz is 6 steps, not 7.
//
// Config holds the tunables and is a versioned YAML document read with ParseConfig and written with EncodeConfig: unknown fields are refused, durations are Go duration strings such as "100ms", and encoding is deterministic: EncodeConfig writes a canonical form, every field in a fixed order, and parsing and encoding that form again gives the same bytes.
package platformer
