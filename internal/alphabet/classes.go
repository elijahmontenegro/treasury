package alphabet

import (
	"image"
	"math"
	"unicode"
)

// feat is a coarse shape description: does the glyph rise above the
// x-height, drop below the baseline, or stand shorter than half an x-height.
// 0 means no, 1 yes, 2 either.
type feat struct{ tall, desc, small int8 }

// prior is what a character is expected to look like: its features and its
// ink width in x-heights.
type prior struct {
	f     feat
	width float64
}

// charPrior derives the prior from the character alone, for any script that
// has a baseline and an x-height. Unknown characters accept anything.
func charPrior(r rune) prior {
	switch {
	case unicode.IsUpper(r):
		p := prior{f: feat{tall: 1}, width: 1.2}
		switch r {
		case 'I':
			p.width = 0.35
		case 'J':
			p.width, p.f.desc = 0.8, 2
		case 'Q':
			p.f.desc = 2
		case 'M', 'W':
			p.width = 1.7
		}
		return p
	case unicode.IsLower(r):
		p := prior{width: 1.0}
		switch r {
		case 'b', 'd', 'f', 'h', 'k', 'l', 't':
			p.f.tall = 1
		case 'g', 'p', 'q', 'y':
			p.f.desc = 1
		case 'j':
			p.f.tall, p.f.desc = 2, 1
		case 'i':
			p.f.tall = 2
		}
		switch r {
		case 'i', 'l':
			p.width = 0.3
		case 'j':
			p.width = 0.45
		case 't', 'f', 'r':
			p.width = 0.6
		case 'm':
			p.width = 1.6
		case 'w':
			p.width = 1.5
		}
		return p
	case unicode.IsDigit(r):
		if r == '1' {
			return prior{f: feat{tall: 1}, width: 0.5}
		}
		return prior{f: feat{tall: 1}, width: 1.0}
	}
	switch r {
	case '.':
		return prior{f: feat{small: 1}, width: 0.3}
	case ',':
		return prior{f: feat{small: 2, desc: 2}, width: 0.3}
	case ':':
		return prior{f: feat{small: 2}, width: 0.3}
	case ';':
		return prior{f: feat{small: 2, desc: 2}, width: 0.3}
	case '(', ')', '[', ']', '{', '}':
		return prior{f: feat{tall: 1, desc: 1}, width: 0.5}
	case '-', '–', '—':
		return prior{f: feat{small: 1}, width: 0.8}
	case '\'', '"', '’', '“', '”':
		return prior{f: feat{small: 1, tall: 1}, width: 0.3}
	case '%':
		return prior{f: feat{tall: 1}, width: 1.5}
	case '/':
		return prior{f: feat{tall: 1, desc: 2}, width: 0.6}
	case '&':
		return prior{f: feat{tall: 1}, width: 1.3}
	case '!':
		return prior{f: feat{tall: 1}, width: 0.3}
	case '?':
		return prior{f: feat{tall: 1}, width: 0.8}
	}
	return prior{f: feat{tall: 2, desc: 2, small: 2}}
}

// measured reads the features and width of a box relative to its row.
func measured(box image.Rectangle, baseline int, xh float64) (feat, float64) {
	above := float64(baseline-box.Min.Y) / xh
	below := float64(box.Max.Y-baseline) / xh
	h := float64(box.Dy()) / xh
	// Each feature has a dead band where a pixel of blur or baseline error
	// at a small x-height could put it on either side; there it is "either"
	// and neither confirms nor contradicts a character.
	return feat{
		tall:  grade(above, 1.12, 1.3),
		desc:  grade(below, 0.15, 0.35),
		small: grade(-h, -0.6, -0.4),
	}, float64(box.Dx()) / xh
}

// grade is 0 below lo, 1 above hi, and 2 (either) in between.
func grade(v, lo, hi float64) int8 {
	switch {
	case v < lo:
		return 0
	case v > hi:
		return 1
	}
	return 2
}

// priorCost scores a measured glyph against a character prior: one per
// contradicted feature, plus up to half a point for width, where a factor of
// two counts fully.
func priorCost(m feat, w float64, p prior) float64 {
	c := mismatch(m.tall, p.f.tall) + mismatch(m.desc, p.f.desc) + mismatch(m.small, p.f.small)
	// Half a unit per doubling of the width miss, up to a full unit at
	// four times: a glyph a quarter as wide as its characters contradicts
	// them as surely as a missing ascender does.
	if p.width > 0 && w > 0 {
		c += 0.5 * math.Min(2, math.Abs(math.Log(w/p.width))/math.Ln2)
	}
	return c
}

func mismatch(m, p int8) float64 {
	switch {
	case p == 2 || m == p:
		return 0
	case m == 2:
		return 0.3
	}
	return 1
}

// mergedPrior describes two characters printed as one touching component.
func mergedPrior(a, b prior) prior {
	either := func(x, y int8) int8 {
		if x == 1 || y == 1 {
			return 1
		}
		if x == 2 || y == 2 {
			return 2
		}
		return 0
	}
	return prior{
		f:     feat{tall: either(a.f.tall, b.f.tall), desc: either(a.f.desc, b.f.desc), small: 0},
		width: a.width + b.width,
	}
}
