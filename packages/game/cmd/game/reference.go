package main

import (
	"fmt"
	"io"
	"strings"
)

// referenceText is the wording of the command reference page in one language; what it says about each command comes from the command's definition, in the English game help prints.
type referenceText struct {
	// lang is the docs directory the page is written to, such as "en".
	lang string
	// title is the page's heading.
	title string
	// intro is the paragraph under the heading, which the website shows as the page's description.
	intro string
	// flag, def and description head the columns of a command's flag table.
	flag, def, description string
	// noFlags stands in for the table of a command without flags.
	noFlags string
}

// referenceTexts are the languages the reference is written in, one page each under docs/<lang>/cli-reference.md.
var referenceTexts = []referenceText{
	{
		lang:        "en",
		title:       "Command Reference",
		intro:       "Every subcommand of `go tool game`, with its usage line and flags, generated from the commands' definitions so it always matches `game help`. [Command Line](cli.md) shows how to install the command, what each subcommand is for and how to set up shell completion.",
		flag:        "Flag",
		def:         "Default",
		description: "Description",
		noFlags:     "No flags.",
	},
	{
		lang:        "zh",
		title:       "命令参考",
		intro:       "`go tool game` 的每个子命令及其用法行和参数，由命令定义生成，因此始终与 `game help` 的输出一致；说明文字取自命令自身的英文帮助。如何安装命令、各子命令的用途以及如何设置 shell 补全，见[命令行](cli.md)。",
		flag:        "参数",
		def:         "默认值",
		description: "说明",
		noFlags:     "没有参数。",
	},
}

// referenceHeader follows the title of every generated reference page, telling a reader of the Markdown not to edit it by hand. It comes after the title because the website reads a page's first line as its title.
const referenceHeader = "<!-- Code generated from the game command's definitions by `go tool nsgo test ./cmd/game -run TestReference -update` in packages/game. DO NOT EDIT. -->\n"

// writeReference writes the Markdown command reference for specs in the wording of t.
func writeReference(w io.Writer, t referenceText, specs []commandSpec) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n%s\n", t.title, referenceHeader, t.intro)
	for _, s := range specs {
		fmt.Fprintf(&b, "\n## game %s\n\n%s.\n\n```sh\n%s\n```\n\n", s.name, mdEscape(s.summary), s.usage)
		if len(s.flags) == 0 {
			fmt.Fprintf(&b, "%s\n", t.noFlags)
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n| --- | --- | --- |\n", t.flag, t.def, t.description)
		for _, f := range s.flags {
			name := "-" + f.name
			if f.valueName != "" {
				name += " " + f.valueName
			}
			def := ""
			if f.def != "" {
				def = codeSpan(f.def)
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", codeSpan(name), def, mdEscape(f.usage))
		}
	}
	_, _ = io.WriteString(w, b.String())
}

// mdEscape escapes the characters of s that Markdown, or a GFM table cell, would otherwise read as markup.
func mdEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "|", `\|`, "`", "\\`", "*", `\*`, "_", `\_`, "<", `\<`, "[", `\[`, "]", `\]`).Replace(s)
}

// codeSpan returns s as a Markdown code span, fenced with more backticks than s holds in a row and padded when s starts or ends with one, and with any | escaped for a table cell.
func codeSpan(s string) string {
	run, longest := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || strings.TrimSpace(s) == "" {
		s = " " + s + " "
	}
	return fence + strings.ReplaceAll(s, "|", `\|`) + fence
}
