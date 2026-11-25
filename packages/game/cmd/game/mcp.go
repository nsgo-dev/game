package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/nsgo/core"
)

// mcpCommand serves the editor's MCP tools on standard input and output for an agent that starts it, forwarding to the editor that has the project open.
var mcpCommand = &command{
	name:    "mcp",
	summary: "Serve the editor's MCP tools on standard input and output for an agent, forwarding each call to the editor that has the project open",
	define:  defineMCP,
	complete: map[string]completion{
		"project": filePaths,
	},
}

func defineMCP(a *app, fs *flag.FlagSet) func() error {
	dir := fs.String("project", ".", "the project: its directory, a directory inside it, or its game.yaml")
	return func() error {
		if fs.NArg() > 0 {
			return usagef("expected no arguments, got %d", fs.NArg())
		}
		root, err := editormcp.ProjectRoot(*dir)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return editormcp.NewProxy(root).Run(ctx, &mcp.IOTransport{Reader: io.NopCloser(a.stdin), Writer: nopWriteCloser{a.stdout}})
	}
}

// nopWriteCloser is a writer the MCP transport may close without closing what it writes to, standard output.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// serveEditor serves ed's MCP server on the endpoint of the project ed has open, and of each project it opens later, until cx is cleaned up, and offers its tools to nsgo mcp under go tool nsgo dev -automation. An endpoint it cannot open, as when another editor has the project open, is reported on stderr; the editor works without it.
func serveEditor(cx *core.Scope, ed *editor.Editor, stderr io.Writer) *editormcp.Server {
	s := editormcp.New(ed, editormcp.Options{Run: cx.Post})
	if err := s.Open(); err != nil {
		fmt.Fprintf(stderr, "game editor: agents cannot reach this editor: %v\n", err)
	}
	if err := s.RegisterAutomation(cx); err != nil {
		fmt.Fprintf(stderr, "game editor: %v\n", err)
	}
	cx.Cleanup(func() { _ = s.Close() })
	return s
}
