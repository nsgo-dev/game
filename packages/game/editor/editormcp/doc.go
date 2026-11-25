// Package editormcp is the NSGo Game Editor's MCP server: an agent reads and edits the project open in the editor through it, with the same editor commands the inspector, scene tree and viewport use, so every edit is an undo step the person sees and can take back.
//
// A Server serves an editor.Editor. Open serves it on the project's local endpoint (Endpoint), a Unix domain socket private to the user and named after the project's path, never a network port, and follows the editor to another project's endpoint when it opens one. `go tool game mcp` runs a Proxy on standard input and output, which an agent such as Claude Code or Codex starts: it finds the editor that has the project open through that endpoint and forwards to it, and while none has it answers each call with how to open the project.
//
// The server's tools are a registry (Server.AddTool) that the editor's parts join, so that every action with a button or a shortcut has a tool. This package adds the first five, named as the editor's Settings › Tools page lists them:
//
//   - scene_read reads a scene, prefab, data or project file with line numbers, the open scene and game.yaml as the editor holds them.
//   - scene_edit adds, moves, changes and removes objects, components and properties of the open scene through the editor's methods, the edits of one call as one undo step, or none when the editor refuses one; the refusal (EditError) names the file, the edit, the object, the field and the fix.
//   - selection_get reports what the person has selected.
//   - viewport_image returns a PNG of the viewport.
//   - game_check runs the Problems panel's checks and returns its problems.
//   - game_playtest runs headless bot playtests configured by playtest.yaml with replay recordings and returns the evaluated YAML report.
//   - playtest_define proposes a new or updated metric in playtest.yaml as a pending review with a diff.
//
// Tool handlers run on the server's goroutines and reach the editor through Call.Editor, which runs a function on the editor's goroutine and waits, so a tool may wait for work the editor does elsewhere, such as a project check, and return an image: the two things an app tool registered with NSGo's automation cannot do (nsgo-dev/nsgo#2342, nsgo-dev/nsgo#2343). RegisterAutomation offers the same tools to nsgo mcp as well, for an editor running under go tool nsgo dev -automation.
//
// Resources editor://scene and editor://selection hold the open scene's YAML and the selection; a client that subscribes to them hears of every change, with _meta saying whether the agent or the person made it (MetaBy) and the editor's revision (MetaRevision).
package editormcp
