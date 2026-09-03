package region

import (
	"image"
	"math"
	"sort"
)

// FitBaseline fits y = intercept + slope·x through the bottoms of boxes on
// one row, robustly: the slope is the median of pairwise slopes (Theil–Sen),
// the intercept the mode of the residuals within a pixel, so descenders and
// punctuation do not pull it. A residual skew of a degree after deskew moves
// a long line's bottom by more than an x-height from end to end, which a
// single baseline per row cannot follow.
func FitBaseline(boxes []image.Rectangle) (slope, intercept float64) {
	n := len(boxes)
	if n == 0 {
		return 0, 0
	}
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i, b := range boxes {
		xs[i] = float64(b.Min.X+b.Max.X) / 2
		ys[i] = float64(b.Max.Y)
	}
	if n >= 3 {
		var slopes []float64
		for i := range n {
			for j := i + 1; j < n; j++ {
				if dx := xs[j] - xs[i]; math.Abs(dx) >= 4 {
					slopes = append(slopes, (ys[j]-ys[i])/dx)
				}
			}
		}
		if len(slopes) > 0 {
			sort.Float64s(slopes)
			slope = slopes[len(slopes)/2]
			// Text is deskewed first; a slope beyond a few degrees is a
			// misfit on a short or descender-heavy row.
			if math.Abs(slope) > 0.06 {
				slope = 0
			}
		}
	}
	res := make([]int, n)
	for i := range n {
		res[i] = int(math.Round(ys[i] - slope*xs[i]))
	}
	return slope, float64(modeInt(res))
}

// BaselineAt evaluates a fitted baseline at x.
func BaselineAt(slope, intercept float64, x int) int {
	return int(math.Round(intercept + slope*float64(x)))
}

// modeInt returns the value with the most support within ±1.
func modeInt(v []int) int {
	if len(v) == 0 {
		return 0
	}
	counts := map[int]int{}
	for _, x := range v {
		counts[x]++
	}
	best, bestN := v[0], -1
	for x := range counts {
		n := counts[x-1] + counts[x] + counts[x+1]
		if n > bestN || (n == bestN && x > best) {
			best, bestN = x, n
		}
	}
	return best
}
