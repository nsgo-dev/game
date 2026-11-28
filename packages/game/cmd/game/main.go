// Command game was the game layer's command-line tool in this module. The module moved to github.com/nsgo-dev/game in 0.2.0, and this version of the command only says where it went: the code it used to run here has known security vulnerabilities, fixed in github.com/nsgo-dev/game v0.2.1 (see https://github.com/nsgo-dev/game/security/advisories).
package main

import (
	"fmt"
	"os"
)

const moved = `github.com/nsgo-dev/game/packages/game is deprecated: the game layer moved to github.com/nsgo-dev/game in 0.2.0, and the old module has known security vulnerabilities (https://github.com/nsgo-dev/game/security/advisories).

Run the command-line tool from its new path instead:

    go run github.com/nsgo-dev/game/cmd/game@latest %s

In a project, add it as a tool and run it with go tool:

    go get -tool github.com/nsgo-dev/game/cmd/game@latest
    go tool game %s

A project that imports github.com/nsgo-dev/game/packages/game/... should require github.com/nsgo-dev/game v0.2.1 or later and drop the /packages/game segment from its import paths.
`

func main() {
	args := "<command>"
	if len(os.Args) > 1 {
		args = fmt.Sprint(os.Args[1:])
		args = args[1 : len(args)-1]
	}
	fmt.Fprintf(os.Stderr, moved, args, args)
	os.Exit(2)
}
