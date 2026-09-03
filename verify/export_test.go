package verify

import (
	"fmt"
	"image"
	"sort"

	"treasury/internal/alphabet"
	"treasury/internal/bitcode"
)

func boxOf(o alphabet.Observed, g int) image.Rectangle {
	if g < 0 || g >= len(o.Boxes) {
		return image.Rectangle{}
	}
	return o.Boxes[g]
}

// SetDebugScored installs a test hook that receives, per claim, a
// description of the refined pairs, nearest first.
func SetDebugScored(fn func(claim string, lines []string)) {
	if fn == nil {
		debugScored = nil
		return
	}
	debugScored = func(claim string, regions []encodedRegion, pairs []scored) {
		sort.Slice(pairs, func(a, b int) bool { return pairs[a].dist < pairs[b].dist })
		var out []string
		for i, p := range pairs {
			if i >= 8 {
				break
			}
			r := regions[p.region]
			out = append(out, fmt.Sprintf("%-18q dist=%.3f raw=%d bits=%d refined=%v pen=%d region=%v comps=%d %s",
				p.word_.Text, p.dist, p.raw, p.bits, p.refined, p.penalized, r.ink, len(r.comps), p.word_.Params.Face))
			if i == 0 && p.path != nil {
				out = append(out, "  xh="+fmt.Sprintf("%.1f baseline=%d", p.obs.XHeight, p.obs.Baseline))
				for _, st := range p.path {
					var ch string
					if st.Char >= 0 && st.Char < len(p.target.Text) {
						ch = string(p.target.Text[st.Char])
					}
					h := -1
					if st.Kind == alphabet.Match && p.target.Codes[st.Char] != nil {
						h = bitcode.Distance(p.obs.Codes[st.Glyph], p.target.Codes[st.Char])
					}
					out = append(out, fmt.Sprintf("  %-6s glyph=%d char=%d %q ham=%d box=%v", st.Kind, st.Glyph, st.Char, ch, h, boxOf(p.obs, st.Glyph)))
				}
			}
		}
		fn(claim, out)
	}
}

// DigitDistances, for the best refined pair of the claim, lists for every
// matched glyph whose target is a digit the distances to all ten digit
// codes. It exercises the synthesis path against observed digits.
func DigitDistances(fn func(line string)) {
	digitProbe = fn
}


// SetProbe makes decide report, through the SetDebugScored hook, every
// region overlapping box against the candidate whose text is text: its
// components, aspect, filter decisions, and coarse and refined distances.
func SetProbe(box image.Rectangle, text string) {
	debugProbe = &probe{box: box, text: text}
}
