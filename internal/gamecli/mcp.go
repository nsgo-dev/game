package gamecli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

// mcpCommand serves the editor's MCP tools on standard input and output for an agent that starts it, forwarding to the editor that has the project open, or writes project-level MCP configurations for Claude Code and Codex with `mcp install`.
var mcpCommand = &command{
	name:    "mcp",
	args:    "[install]",
	summary: "Serve the editor's MCP tools on stdio, or run `mcp install` to configure Claude Code and Codex for this project",
	define:  defineMCP,
	complete: map[string]completion{
		"project": filePaths,
		"":        oneOf("install"),
	},
}

func defineMCP(a *app, fs *flag.FlagSet) func() error {
	dir := fs.String("project", ".", "the project: its directory, a directory inside it, or its game.yaml")
	return func() error {
		if fs.NArg() > 0 {
			if fs.Arg(0) == "install" {
				if err := fs.Parse(fs.Args()[1:]); err != nil {
					if errors.Is(err, flag.ErrHelp) {
						return flag.ErrHelp
					}
					return errUsageReported
				}
				if fs.NArg() > 0 {
					return usagef("expected no arguments after install, got %d", fs.NArg())
				}
				return runMCPInstall(a, *dir)
			}
			return usagef("unknown subcommand %q (expected none or install)", fs.Arg(0))
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

func runMCPInstall(a *app, dir string) error {
	var extra []editor.AgentMCPServer
	if settingsPath, perr := editor.DefaultSettingsPath(); perr == nil {
		if s, serr := editor.LoadEditorSettings(settingsPath); serr == nil && s.Agent != nil && len(s.Agent.MCPServers) > 0 {
			extra = s.Agent.MCPServers
		}
	}
	res, err := editormcp.InstallWithServers(dir, extra)
	if err != nil {
		return err
	}
	for _, f := range res.Files {
		verb := "Wrote"
		if !f.Changed {
			verb = "Unchanged"
		}
		fmt.Fprintf(a.stdout, "%s %s (%s)\n", verb, f.RelPath, f.Client)
	}
	return nil
}

// nopWriteCloser is a writer the MCP transport may close without closing what it writes to, standard output.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
