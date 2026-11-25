package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// TestLevelsFileIsCanonical checks that levels.yaml is in the form EncodeLevels writes, so loading and saving it changes nothing and a review sees only real edits.
func TestLevelsFileIsCanonical(t *testing.T) {
	out, err := EncodeLevels(Levels)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, levelsYAML) {
		t.Fatalf("levels.yaml is not canonical; EncodeLevels writes:\n%s", out)
	}
}

// TestLevelsAreSolvableAtPar checks that every level can be won, that its three-star count is the fewest moves that win it, and that the moves allowed leave room to recover from a mistake.
func TestLevelsAreSolvableAtPar(t *testing.T) {
	for _, l := range Levels.Levels {
		best, path := solve(NewBoard(&l))
		if best < 0 {
			t.Errorf("%s cannot be won", l.ID)
			continue
		}
		if best != l.Stars.Three {
			t.Errorf("%s: the fewest moves that win it are %d (%v), but stars.three is %d", l.ID, best, path, l.Stars.Three)
		}
		if l.Moves < best+2 {
			t.Errorf("%s: moves %d leaves no room for a mistake over the best %d", l.ID, l.Moves, best)
		}
	}
}

func TestParseLevelsRefusesBadFiles(t *testing.T) {
	valid := "version: 1\nlevels:\n  - id: a\n    name: A\n    moves: 3\n    stars:\n      three: 1\n      two: 2\n    rows:\n      - '#So#'\n"
	if _, err := ParseLevels([]byte(valid)); err != nil {
		t.Fatalf("the valid file was refused: %v", err)
	}
	for _, tc := range []struct{ name, src, want string }{
		{"empty", "", "empty"},
		{"version", strings.Replace(valid, "version: 1", "version: 2", 1), "version 2"},
		{"unknown field", strings.Replace(valid, "    moves: 3\n", "    moves: 3\n    timer: 9\n", 1), "timer"},
		{"no levels", "version: 1\nlevels: []\n", "no levels"},
		{"bad id", strings.Replace(valid, "id: a", "id: A", 1), `id "A"`},
		{"no name", strings.Replace(valid, "name: A", "name: ' '", 1), "no name"},
		{"stars out of order", strings.Replace(valid, "two: 2", "two: 4", 1), "stars.two (4)"},
		{"ragged", strings.Replace(valid, "      - '#So#'\n", "      - '#So#'\n      - '##'\n", 1), "row 2"},
		{"unknown tile", strings.Replace(valid, "'#So#'", "'#So?'", 1), "unknown tile"},
		{"two starts", strings.Replace(valid, "'#So#'", "'SSo#'", 1), "2 starts"},
		{"no lamp", strings.Replace(valid, "'#So#'", "'#S.#'", 1), "no lamps"},
		{"two documents", valid + "---\nversion: 1\n", "more than one"},
		{"duplicate id", valid + "  - id: a\n    name: B\n    moves: 3\n    stars:\n      three: 1\n      two: 2\n    rows:\n      - '#So#'\n", "used twice"},
	} {
		if _, err := ParseLevels([]byte(tc.src)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", tc.name, err, tc.want)
		}
	}
}

func TestStarsFor(t *testing.T) {
	l := Level{Moves: 8, Stars: Stars{Three: 3, Two: 5}}
	for moves, want := range map[int]int{1: 3, 3: 3, 4: 2, 5: 2, 6: 1, 8: 1} {
		if got := l.StarsFor(moves); got != want {
			t.Errorf("StarsFor(%d) = %d, want %d", moves, got, want)
		}
	}
}

// FuzzParseLevels checks that any file ParseLevels accepts encodes to a canonical form that parses back to the same levels and encodes to itself.
func FuzzParseLevels(f *testing.F) {
	f.Add(levelsYAML)
	f.Add([]byte("version: 1\nlevels:\n  - id: a\n    name: A\n    moves: 3\n    stars: {three: 1, two: 2}\n    rows: ['#So#', 'x..#']\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		lf, err := ParseLevels(data)
		if err != nil {
			return
		}
		out, err := EncodeLevels(lf)
		if err != nil {
			t.Fatalf("EncodeLevels of a parsed file: %v", err)
		}
		again, err := ParseLevels(out)
		if err != nil {
			t.Fatalf("the canonical form does not parse: %v\n%s", err, out)
		}
		if !slices.EqualFunc(lf.Levels, again.Levels, levelsEqual) {
			t.Fatalf("the round trip changed the levels:\n%s", out)
		}
		if out2, _ := EncodeLevels(again); !bytes.Equal(out, out2) {
			t.Fatalf("the canonical form is not stable:\n%s\n---\n%s", out, out2)
		}
	})
}

func levelsEqual(a, b Level) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Moves == b.Moves && a.Stars == b.Stars && slices.Equal(a.Rows, b.Rows)
}

// solve returns the fewest moves that win b and one such sequence, or -1 when it cannot be won within its moves, by breadth-first search over the spark's cell and the lamps lit.
func solve(b *Board) (int, []Dir) {
	type state struct {
		spark Cell
		lit   string
	}
	key := func(b *Board) state {
		var lit strings.Builder
		for y := range b.Height {
			for x := range b.Width {
				if b.Lit(Cell{x, y}) {
					lit.WriteByte('1')
				} else {
					lit.WriteByte('0')
				}
			}
		}
		return state{b.Spark, lit.String()}
	}
	type node struct {
		moves []Dir
	}
	replay := func(moves []Dir) *Board {
		nb := NewBoard(b.Level)
		for _, d := range moves {
			(&slideMove{Dir: d}).Apply(nb)
		}
		return nb
	}
	seen := map[state]bool{key(b): true}
	queue := []node{{}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		cur := replay(n.moves)
		for _, d := range []Dir{DirLeft, DirRight, DirUp, DirDown} {
			if len(cur.Path(d)) == 0 {
				continue
			}
			moves := append(slices.Clip(n.moves), d)
			next := replay(moves)
			switch next.Status() {
			case Won:
				return len(moves), moves
			case Lost:
				continue
			}
			if k := key(next); !seen[k] {
				seen[k] = true
				queue = append(queue, node{moves})
			}
		}
	}
	return -1, nil
}
