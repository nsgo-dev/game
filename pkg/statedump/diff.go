package statedump

import (
	"bytes"
	"fmt"
	"strings"
)

// diffContext is the number of unchanged lines Diff prints around each change.
const diffContext = 3

// maxDiffCells bounds the table Diff uses to find the fewest changed lines; a larger changed region is printed as all old lines removed and all new lines added.
const maxDiffCells = 1 << 24

type diffOp struct {
	kind byte // ' ', '-' or '+'
	line string
}

// Diff returns a unified line diff from a to b, labelled "a" and "b", or "" when they are equal.
//
// It is meant for dumps that Encode wrote, where one changed field is one changed line, and prints each change with three unchanged lines around it.
func Diff(a, b []byte) string {
	if bytes.Equal(a, b) {
		return ""
	}
	al, bl := splitLines(a), splitLines(b)
	ops := diffLines(al, bl)

	// aPos[k] and bPos[k] count the lines of a and b that come before ops[k].
	aPos := make([]int, len(ops)+1)
	bPos := make([]int, len(ops)+1)
	for k, op := range ops {
		aPos[k+1], bPos[k+1] = aPos[k], bPos[k]
		if op.kind != '+' {
			aPos[k+1]++
		}
		if op.kind != '-' {
			bPos[k+1]++
		}
	}

	var out strings.Builder
	out.WriteString("--- a\n+++ b\n")
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// A hunk runs from diffContext lines before its first change to diffContext lines after its last, taking in later changes whose context would overlap.
		start := max(i-diffContext, 0)
		end := i
		for {
			for end < len(ops) && ops[end].kind != ' ' {
				end++
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run < len(ops) && run-end <= 2*diffContext {
				end = run
				continue
			}
			end = min(end+diffContext, len(ops))
			break
		}
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", hunkRange(aPos[start]+1, aPos[end]-aPos[start]), hunkRange(bPos[start]+1, bPos[end]-bPos[start]))
		for _, op := range ops[start:end] {
			out.WriteByte(op.kind)
			out.WriteString(op.line)
			out.WriteByte('\n')
		}
		i = end
	}
	if out.Len() == len("--- a\n+++ b\n") {
		// The lines match, so a and b differ only in whether they end with a newline.
		out.WriteString("\\ the inputs differ only in the newline at the end\n")
	}
	return out.String()
}

func hunkRange(line, count int) string {
	if count == 0 {
		return fmt.Sprintf("%d,0", line-1)
	}
	if count == 1 {
		return fmt.Sprintf("%d", line)
	}
	return fmt.Sprintf("%d,%d", line, count)
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := strings.TrimSuffix(string(data), "\n")
	return strings.Split(s, "\n")
}

// diffLines returns the edit script from a to b: the common prefix and suffix as they are, and the fewest removed and added lines between them.
func diffLines(a, b []string) []diffOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ops := make([]diffOp, 0, len(a)+len(b))
	for _, l := range a[:pre] {
		ops = append(ops, diffOp{' ', l})
	}
	am, bm := a[pre:len(a)-suf], b[pre:len(b)-suf]
	n, m := len(am), len(bm)
	if n == 0 || m == 0 || (n+1)*(m+1) > maxDiffCells {
		for _, l := range am {
			ops = append(ops, diffOp{'-', l})
		}
		for _, l := range bm {
			ops = append(ops, diffOp{'+', l})
		}
	} else {
		// lcs[i*(m+1)+j] is the longest common subsequence of am[i:] and bm[j:].
		w := m + 1
		lcs := make([]int32, (n+1)*w)
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if am[i] == bm[j] {
					lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
				} else {
					lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
				}
			}
		}
		i, j := 0, 0
		for i < n && j < m {
			switch {
			case am[i] == bm[j]:
				ops = append(ops, diffOp{' ', am[i]})
				i++
				j++
			case lcs[(i+1)*w+j] >= lcs[i*w+j+1]:
				ops = append(ops, diffOp{'-', am[i]})
				i++
			default:
				ops = append(ops, diffOp{'+', bm[j]})
				j++
			}
		}
		for ; i < n; i++ {
			ops = append(ops, diffOp{'-', am[i]})
		}
		for ; j < m; j++ {
			ops = append(ops, diffOp{'+', bm[j]})
		}
	}
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', l})
	}
	return ops
}
