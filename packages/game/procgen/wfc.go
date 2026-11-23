package procgen

import (
	"errors"
	"fmt"
	"math/bits"

	"github.com/nsgo-dev/game/packages/game/random"
)

// ErrNoSolution is returned when WFC exhausts its attempts without a level that satisfies the rules and, unless disabled, the connectivity constraint.
var ErrNoSolution = errors.New("procgen: wave function collapse found no solution")

// WFCConfig configures WFC. Zero fields take the defaults given on each field.
type WFCConfig struct {
	// Width and Height are the level's size in cells, from 1 to MaxSide each.
	Width, Height int
	// Rules is the tile set and its adjacency rules. It must be valid.
	Rules *Rules
	// Attempts is how many times WFC restarts after a contradiction before returning ErrNoSolution. The default is 20.
	Attempts int
	// AllowDisconnected turns off the connectivity constraint, so walkable tiles may form several regions or none. By default, whenever Rules has a walkable tile, every level WFC returns has at least one walkable cell and all of its walkable cells form one 4-connected region.
	AllowDisconnected bool
}

// Directions in the order cells are visited around a cell.
const (
	dirUp = iota
	dirLeft
	dirRight
	dirDown
	dirCount
)

var dirOffset = [dirCount]Point{{0, -1}, {-1, 0}, {1, 0}, {0, 1}}

// wfcModel is a Rules set compiled into bitsets.
type wfcModel struct {
	tiles   int
	words   int
	weights []float64
	// compat[d][t] is the bitset of tiles allowed in direction d of tile t.
	compat [dirCount][][]uint64
	full   []uint64
	// walk and solid split full into walkable and non-walkable tiles.
	walk, solid []uint64
}

func compileRules(r *Rules) *wfcModel {
	n := len(r.Tiles)
	m := &wfcModel{tiles: n, words: (n + 63) / 64, weights: make([]float64, n)}
	m.full = make([]uint64, m.words)
	m.walk = make([]uint64, m.words)
	m.solid = make([]uint64, m.words)
	for t, tile := range r.Tiles {
		m.weights[t] = tile.Weight
		m.full[t/64] |= 1 << (t % 64)
		if tile.Walkable {
			m.walk[t/64] |= 1 << (t % 64)
		} else {
			m.solid[t/64] |= 1 << (t % 64)
		}
	}
	for d := range dirCount {
		m.compat[d] = make([][]uint64, n)
		for t := range n {
			m.compat[d][t] = make([]uint64, m.words)
		}
	}
	allow := func(d, from, to int) { m.compat[d][from][to/64] |= 1 << (to % 64) }
	for _, nb := range r.Neighbors {
		if nb.Left != "" {
			a, b := r.Index(nb.Left), r.Index(nb.Right)
			allow(dirRight, a, b)
			allow(dirLeft, b, a)
		} else {
			a, b := r.Index(nb.Up), r.Index(nb.Down)
			allow(dirDown, a, b)
			allow(dirUp, b, a)
		}
	}
	return m
}

// wfcState is the wave of one attempt, the set of tiles each cell may still take, with the scratch buffers of propagation and the connectivity constraint.
type wfcState struct {
	m       *wfcModel
	w, h    int
	connect bool
	wave    []uint64
	count   []int
	stack   []int
	queued  []bool
	allowed []uint64
	weights []float64
	cands   []int

	// Depth-first search state of the connectivity constraint.
	disc, low, parent, defs []int32
	dfs                     []dfsFrame
	cuts                    []cutCandidate
}

type dfsFrame struct {
	cell int32
	dir  int8
}

type cutCandidate struct {
	cell int32
	defs int32
}

// WFC fills a Width by Height grid with the simple tiled model of wave function collapse: each step collapses a cell with the fewest remaining tiles, chosen at random among ties, to one tile picked by weight, then propagates the adjacency rules to its neighbours. A contradiction restarts the solve.
//
// Unless cfg.AllowDisconnected is set, a path constraint keeps the walkable tiles connected while the wave collapses: cells that can no longer reach the walkable cells lose their walkable tiles, and a cell whose removal would split the walkable cells loses its non-walkable tiles.
//
// Each Grid cell holds the index of its tile in cfg.Rules.Tiles. All randomness comes from s, so the same stream state and cfg produce the same Grid. It returns an error wrapping ErrInvalidConfig for an unusable cfg and ErrNoSolution when every attempt fails.
func WFC(s *random.Stream, cfg WFCConfig) (*Grid, error) {
	if cfg.Attempts == 0 {
		cfg.Attempts = 20
	}
	switch {
	case cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxSide || cfg.Height > MaxSide:
		return nil, fmt.Errorf("%w: WFC size %dx%d is outside [1, MaxSide]", ErrInvalidConfig, cfg.Width, cfg.Height)
	case cfg.Attempts < 0:
		return nil, fmt.Errorf("%w: WFC Attempts %d is negative", ErrInvalidConfig, cfg.Attempts)
	case cfg.Rules == nil:
		return nil, fmt.Errorf("%w: WFC Rules is nil", ErrInvalidConfig)
	}
	if err := cfg.Rules.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	m := compileRules(cfg.Rules)
	connect := false
	if !cfg.AllowDisconnected {
		for _, w := range m.walk {
			connect = connect || w != 0
		}
	}
	cells := cfg.Width * cfg.Height
	st := &wfcState{
		m:       m,
		w:       cfg.Width,
		h:       cfg.Height,
		connect: connect,
		wave:    make([]uint64, cells*m.words),
		count:   make([]int, cells),
		queued:  make([]bool, cells),
		allowed: make([]uint64, m.words),
		weights: make([]float64, m.tiles),
	}
	if connect {
		st.disc = make([]int32, cells)
		st.low = make([]int32, cells)
		st.parent = make([]int32, cells)
		st.defs = make([]int32, cells)
	}
	for range cfg.Attempts {
		if g := st.solve(s); g != nil {
			return g, nil
		}
	}
	return nil, fmt.Errorf("%w after %d attempts", ErrNoSolution, cfg.Attempts)
}

func (st *wfcState) cell(c int) []uint64 {
	return st.wave[c*st.m.words : (c+1)*st.m.words]
}

// solve runs one attempt and returns the collapsed grid, or nil on a contradiction.
func (st *wfcState) solve(s *random.Stream) *Grid {
	m := st.m
	cells := st.w * st.h
	for c := range cells {
		copy(st.cell(c), m.full)
		st.count[c] = m.tiles
		st.queued[c] = true
	}
	// Seed propagation from every cell so tiles that cannot have a neighbour on some side are removed before the first choice.
	st.stack = st.stack[:0]
	for c := cells - 1; c >= 0; c-- {
		st.stack = append(st.stack, c)
	}
	if !st.settle() {
		return nil
	}
	for {
		st.cands = st.cands[:0]
		least := m.tiles + 1
		for c, n := range st.count {
			switch {
			case n <= 1:
			case n < least:
				least = n
				st.cands = append(st.cands[:0], c)
			case n == least:
				st.cands = append(st.cands, c)
			}
		}
		if len(st.cands) == 0 {
			break
		}
		c := st.cands[s.Intn(len(st.cands))]
		cw := st.cell(c)
		for t := range m.tiles {
			st.weights[t] = 0
			if cw[t/64]&(1<<(t%64)) != 0 {
				st.weights[t] = m.weights[t]
			}
		}
		t := s.WeightedIndex(st.weights)
		clear(cw)
		cw[t/64] = 1 << (t % 64)
		st.count[c] = 1
		st.queued[c] = true
		st.stack = append(st.stack[:0], c)
		if !st.settle() {
			return nil
		}
	}
	g := NewGrid(st.w, st.h)
	walkable := 0
	for c := range cells {
		for w, word := range st.cell(c) {
			if word != 0 {
				g.Cells[c] = uint32(w*64 + bits.TrailingZeros64(word))
				if m.walk[w]&word != 0 {
					walkable++
				}
				break
			}
		}
	}
	if st.connect && walkable == 0 {
		return nil
	}
	return g
}

// settle propagates the adjacency rules and, when enabled, the connectivity constraint until neither removes another tile, and reports false on a contradiction.
func (st *wfcState) settle() bool {
	for {
		if !st.propagate() {
			return false
		}
		if !st.connect {
			return true
		}
		changed, ok := st.enforcePath()
		if !ok {
			return false
		}
		if !changed {
			return true
		}
	}
}

// propagate removes, cell by cell from the stack, every neighbour tile no remaining tile of the cell allows, and reports false on a cell left with no tile.
func (st *wfcState) propagate() bool {
	m := st.m
	for len(st.stack) > 0 {
		c := st.stack[len(st.stack)-1]
		st.stack = st.stack[:len(st.stack)-1]
		st.queued[c] = false
		cx, cy := c%st.w, c/st.w
		cw := st.cell(c)
		for d := range dirCount {
			nx, ny := cx+dirOffset[d].X, cy+dirOffset[d].Y
			if nx < 0 || ny < 0 || nx >= st.w || ny >= st.h {
				continue
			}
			clear(st.allowed)
			for w, word := range cw {
				for word != 0 {
					t := w*64 + bits.TrailingZeros64(word)
					word &= word - 1
					for i, a := range m.compat[d][t] {
						st.allowed[i] |= a
					}
				}
			}
			if _, ok := st.restrict(ny*st.w+nx, st.allowed); !ok {
				return false
			}
		}
	}
	return true
}

// restrict keeps only the tiles of cell c that are in keep, queueing c for propagation when it loses a tile, and reports whether it changed and whether it still has a tile.
func (st *wfcState) restrict(c int, keep []uint64) (changed, ok bool) {
	cw := st.cell(c)
	total := 0
	for i := range cw {
		v := cw[i] & keep[i]
		if v != cw[i] {
			cw[i] = v
			changed = true
		}
		total += bits.OnesCount64(v)
	}
	if !changed {
		return false, true
	}
	if total == 0 {
		return true, false
	}
	st.count[c] = total
	if !st.queued[c] {
		st.queued[c] = true
		st.stack = append(st.stack, c)
	}
	return true, true
}

// mayWalk reports whether cell c still has a walkable tile.
func (st *wfcState) mayWalk(c int) bool {
	for i, w := range st.cell(c) {
		if w&st.m.walk[i] != 0 {
			return true
		}
	}
	return false
}

// mustWalk reports whether every remaining tile of cell c is walkable.
func (st *wfcState) mustWalk(c int) bool {
	for i, w := range st.cell(c) {
		if w&st.m.solid[i] != 0 {
			return false
		}
	}
	return true
}

// enforcePath applies the connectivity constraint once. Cells that must be walkable all have to lie in one component of the graph of cells that may be walkable; cells outside that component lose their walkable tiles, and articulation points of the component that separate cells that must be walkable lose their non-walkable tiles. It reports whether any tile was removed, and false for ok on a contradiction.
func (st *wfcState) enforcePath() (changed, ok bool) {
	cells := st.w * st.h
	root, required := -1, int32(0)
	for c := range cells {
		if st.mustWalk(c) {
			if root < 0 {
				root = c
			}
			required++
		}
	}
	if root < 0 {
		return false, true
	}
	clear(st.disc)
	st.cuts = st.cuts[:0]
	clock := int32(1)
	st.disc[root], st.low[root], st.parent[root], st.defs[root] = clock, clock, -1, 1
	st.dfs = append(st.dfs[:0], dfsFrame{cell: int32(root)})
	for len(st.dfs) > 0 {
		top := &st.dfs[len(st.dfs)-1]
		v := int(top.cell)
		if top.dir < dirCount {
			d := top.dir
			top.dir++
			nx, ny := v%st.w+dirOffset[d].X, v/st.w+dirOffset[d].Y
			if nx < 0 || ny < 0 || nx >= st.w || ny >= st.h {
				continue
			}
			n := ny*st.w + nx
			if !st.mayWalk(n) {
				continue
			}
			if st.disc[n] == 0 {
				clock++
				st.disc[n], st.low[n], st.parent[n] = clock, clock, int32(v)
				st.defs[n] = 0
				if st.mustWalk(n) {
					st.defs[n] = 1
				}
				st.dfs = append(st.dfs, dfsFrame{cell: int32(n)})
			} else if int32(n) != st.parent[v] {
				st.low[v] = min(st.low[v], st.disc[n])
			}
			continue
		}
		st.dfs = st.dfs[:len(st.dfs)-1]
		p := st.parent[v]
		if p < 0 {
			continue
		}
		st.low[p] = min(st.low[p], st.low[v])
		st.defs[p] += st.defs[v]
		if st.low[v] >= st.disc[p] && st.defs[v] > 0 {
			st.cuts = append(st.cuts, cutCandidate{cell: p, defs: st.defs[v]})
		}
	}
	if st.defs[root] != required {
		// Some cell that must be walkable cannot reach the others.
		return false, false
	}
	for c := range cells {
		if st.disc[c] == 0 && st.mayWalk(c) {
			ch, ok := st.restrict(c, st.m.solid)
			if !ok {
				return true, false
			}
			changed = changed || ch
		}
	}
	for _, cut := range st.cuts {
		// Removing the cell would split off cut.defs required cells from the rest, which always includes the root.
		if cut.defs < required && !st.mustWalk(int(cut.cell)) {
			ch, ok := st.restrict(int(cut.cell), st.m.walk)
			if !ok {
				return true, false
			}
			changed = changed || ch
		}
	}
	return changed, true
}
