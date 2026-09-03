package alphabet

import "math"

// Kind is the type of one alignment step.
type Kind uint8

const (
	_      Kind = iota
	Match       // one glyph, one character
	Merge       // one glyph, two characters printed touching
	Split       // two glyphs, one character printed broken
	Split3      // three glyphs, one character (a percent sign, a broken glyph with a dot)
	Insert      // a glyph the reference does not explain
	Delete      // a character with no glyph
	Space       // a space character consumed at a gap
)

func (k Kind) String() string {
	switch k {
	case Match:
		return "match"
	case Merge:
		return "merge"
	case Split:
		return "split"
	case Split3:
		return "split3"
	case Insert:
		return "insert"
	case Delete:
		return "delete"
	case Space:
		return "space"
	}
	return "?"
}

// Step is one alignment decision. Glyph is the first glyph consumed (or the
// next glyph for Delete and Space), Char the first character consumed (or the
// next character for Insert). Cost is the shape cost for Match/Merge/Split
// and the penalty otherwise.
type Step struct {
	Kind  Kind
	Glyph int
	Char  int
	Cost  float64
}

// Path is a complete alignment.
type Path []Step

// Penalties are the transition costs of the alignment, on the same scale as
// the shape cost (one point per contradicted feature).
type Penalties struct {
	Merge   float64
	Split   float64
	Insert  float64
	Delete  float64
	Break   float64 // consecutive letters across a row break
	WordGap float64 // gap, in x-heights, that counts fully as a word space
}

// DefaultPenalties are the starting values; step 6 tunes them.
func DefaultPenalties() Penalties {
	return Penalties{Merge: 1.5, Split: 1.5, Insert: 2.5, Delete: 2.5, Break: 1.5, WordGap: 0.5}
}

// problem is one alignment instance with its cost functions.
type problem struct {
	chars  []rune
	m      int       // glyph count
	gap    []float64 // gap before glyph g, x-heights; +Inf at row starts
	spaces []int     // spaces[c] = spaces among chars[:c]
	shape  func(g, c int) float64 // glyph g as char c
	pair   func(g, c int) float64 // glyph g as chars c and c+1 touching
	union  func(g, c int) float64 // glyphs g and g+1 as char c; NaN when not allowed
	union3 func(g, c int) float64 // glyphs g, g+1, g+2 as char c; NaN when not allowed; may be nil
	pen    Penalties
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// spaceCost charges a space character placed before glyph g: free at a word
// gap or row break, one point at a letter gap.
func (p *problem) spaceCost(g int) float64 {
	if g <= 0 || g >= p.m || math.IsInf(p.gap[g], 1) {
		return 0
	}
	return clamp01(1 - p.gap[g]/p.pen.WordGap)
}

// joinCost charges consuming glyph g right after the previous glyph with no
// space character between: free at a letter gap, up to one point at a word
// gap, Break across rows.
func (p *problem) joinCost(c, g int) float64 {
	if g <= 0 || (c > 0 && p.chars[c-1] == ' ') {
		return 0
	}
	if math.IsInf(p.gap[g], 1) {
		return p.pen.Break
	}
	return clamp01((p.gap[g] - p.pen.WordGap) / p.pen.WordGap)
}

// align runs the banded dynamic programme. touched reports that the best path
// ran along the band edge or the end state fell outside it, so the caller
// should widen the band and retry.
func (p *problem) align(band int) (path Path, cost float64, touched bool) {
	n, m := len(p.chars), p.m
	width := 2*band + 1
	exp := func(c int) int { return c - p.spaces[c] }
	lo := func(c int) int { return max(0, exp(c)-band) }
	hi := func(c int) int { return min(m, exp(c)+band) }
	valid := func(c, g int) bool { return g >= lo(c) && g <= hi(c) }
	idx := func(c, g int) int { return c*width + (g - lo(c)) }
	inf := math.Inf(1)
	dp := make([]float64, (n+1)*width)
	for i := range dp {
		dp[i] = inf
	}
	bp := make([]Kind, (n+1)*width)
	stepCost := make([]float64, (n+1)*width)
	dp[idx(0, 0)] = 0
	relax := func(c, g int, from, cost, extra float64, k Kind) {
		if !valid(c, g) {
			return
		}
		if v := from + cost + extra; v < dp[idx(c, g)] {
			dp[idx(c, g)] = v
			bp[idx(c, g)] = k
			stepCost[idx(c, g)] = cost
		}
	}
	for c := 0; c <= n; c++ {
		for g := lo(c); g <= hi(c); g++ {
			cur := dp[idx(c, g)]
			if math.IsInf(cur, 1) {
				continue
			}
			if g < m {
				relax(c, g+1, cur, p.pen.Insert, 0, Insert)
			}
			if c == n {
				continue
			}
			if p.chars[c] == ' ' {
				relax(c+1, g, cur, p.spaceCost(g), 0, Space)
				continue
			}
			relax(c+1, g, cur, p.pen.Delete, 0, Delete)
			if g >= m {
				continue
			}
			join := p.joinCost(c, g)
			relax(c+1, g+1, cur, p.shape(g, c), join, Match)
			if c+1 < n && p.chars[c+1] != ' ' {
				relax(c+2, g+1, cur, p.pen.Merge+p.pair(g, c), join, Merge)
			}
			if g+1 < m {
				if u := p.union(g, c); !math.IsNaN(u) {
					relax(c+1, g+2, cur, p.pen.Split+u, join, Split)
				}
			}
			if g+2 < m && p.union3 != nil {
				if u := p.union3(g, c); !math.IsNaN(u) {
					relax(c+1, g+3, cur, p.pen.Split+0.5+u, join, Split3)
				}
			}
		}
	}
	if !valid(n, m) || math.IsInf(dp[idx(n, m)], 1) {
		return nil, inf, true
	}
	cost = dp[idx(n, m)]
	c, g := n, m
	for c > 0 || g > 0 {
		if (g == lo(c) && lo(c) > 0) || (g == hi(c) && hi(c) < m) {
			touched = true
		}
		k := bp[idx(c, g)]
		sc := stepCost[idx(c, g)]
		switch k {
		case Match:
			c, g = c-1, g-1
			path = append(path, Step{Match, g, c, sc})
		case Merge:
			c, g = c-2, g-1
			path = append(path, Step{Merge, g, c, sc})
		case Split:
			c, g = c-1, g-2
			path = append(path, Step{Split, g, c, sc})
		case Split3:
			c, g = c-1, g-3
			path = append(path, Step{Split3, g, c, sc})
		case Insert:
			g--
			path = append(path, Step{Insert, g, c, sc})
		case Delete:
			c--
			path = append(path, Step{Delete, g, c, sc})
		case Space:
			c--
			path = append(path, Step{Space, g, c, sc})
		default:
			return nil, inf, true
		}
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, cost, touched
}
