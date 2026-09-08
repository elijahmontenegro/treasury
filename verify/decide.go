package verify

import (
	"image"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// The decision layer, rebuilt over recognised text (step 19c). Its rules
// are the ones this build has always used and are the part of it that has
// always worked: a claim is compared to what was read, a distance decides
// whether the claim is present at all, a margin decides which value is
// present when several are close, and the engine reports absence as
// absence rather than guessing. Only the representation changed. Where the
// retired engine compared bit codes of spelled glyphs, this compares text
// to text.

// maxRun is how many detections may be joined into one candidate region.
// Step 19b found a producer's name arriving as three detections of one
// printed line, so a claim cannot be compared to a single detection.
const maxRun = 4

// candidate is one accepted spelling of a claim and the value it stands
// for. For free text every spelling stands for the same value, so nothing
// competes; for a number each value the field may legally take is a
// candidate of its own, which is what the margin separates.
type candidate struct {
	text    string // as printed
	norm    string // as compared
	value   float64
	name    string // the value as it will be reported
	claimed bool   // stands for the value the application filed
	form    int    // which printed form it was spelled in; -1 for free text
	num     string // the number as printed, for a numeric candidate
}

// run is a candidate region: one detection, or several joined.
type run struct {
	box  image.Rectangle
	text string
	norm string
	nums []string // the numbers in it, as printed
	conf float64
}

// fold maps a letter carrying a diacritic to the letter. A label sets
// "VIÑEDOS" where the application files "VINEDOS"; they name the same
// producer, and step 12b adopted that equivalence on merit.
var fold = map[rune]rune{
	0xC0: 'A', 0xC1: 'A', 0xC2: 'A', 0xC3: 'A', 0xC4: 'A', 0xC5: 'A', 0xC7: 'C',
	0xC8: 'E', 0xC9: 'E', 0xCA: 'E', 0xCB: 'E', 0xCC: 'I', 0xCD: 'I', 0xCE: 'I',
	0xCF: 'I', 0xD1: 'N', 0xD2: 'O', 0xD3: 'O', 0xD4: 'O', 0xD5: 'O', 0xD6: 'O',
	0xD8: 'O', 0xD9: 'U', 0xDA: 'U', 0xDB: 'U', 0xDC: 'U', 0xDD: 'Y', 0xDF: 'S',
	0xC6: 'A', 0x152: 'O', 0x160: 'S', 0x17D: 'Z',
}

// normalize reduces text to what a name is made of. Case, accents,
// punctuation and spacing are set aside: step 12b adopted each of those as
// an equivalence on merit, and here they cost nothing, because two strings
// are compared rather than a spelled codeword aligned to ink.
func normalize(s string) string {
	rs := []rune(strings.ToUpper(s))
	var b strings.Builder
	for i, r := range rs {
		if f, ok := fold[r]; ok {
			r = f
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '.' || r == ',':
			// A separator between two digits is the number, not
			// punctuation: dropping it makes 4.5 percent and 45 percent
			// the same string, which is a wrong value asserted rather
			// than a spelling set aside. The comma is the same
			// separator, which is the equivalence 12b adopted for
			// labels that print "14,5%".
			if i > 0 && i+1 < len(rs) && unicode.IsDigit(rs[i-1]) && unicode.IsDigit(rs[i+1]) {
				b.WriteRune('.')
			}
		}
	}
	return b.String()
}

// distance is the edit distance between two normalized strings as a share
// of the claim's own length, so a wrong character costs the same fraction
// of a short name as of a long one.
func distance(claim, read string) float64 {
	if claim == "" {
		return 1
	}
	a, b := []rune(claim), []rune(read)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := prev[j-1]
			if a[i-1] != b[j-1] {
				c++
			}
			if v := prev[j] + 1; v < c {
				c = v
			}
			if v := cur[j-1] + 1; v < c {
				c = v
			}
			cur[j] = c
		}
		prev, cur = cur, prev
	}
	return float64(prev[len(b)]) / float64(len(a))
}

// numbers is every maximal run of digits, with its decimal separator, in
// normalized text. A numeric claim is a claim about a number, so the
// number itself has to be read exactly; only the words around it are
// allowed the radius. Without this a claim of 5.1 percent verifies against
// a label printing 4.1, because one wrong digit in "ALC. 4.1% BY VOL." is
// a twelfth of the string and the radius admits it.
func numbers(norm string) []string {
	var out []string
	start := -1
	for i, r := range norm {
		digit := (r >= '0' && r <= '9') || r == '.'
		if digit && start < 0 {
			start = i
		} else if !digit && start >= 0 {
			out = append(out, norm[start:i])
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, norm[start:])
	}
	return out
}

// buildRuns joins the reader's detections into the regions a claim can be
// compared to. A detection the recogniser was not confident of is not
// evidence for anything and is dropped before any joining, so a garbage
// reading cannot be half of a match.
func buildRuns(regions []Region, minConf float64) []run {
	var keep []Region
	for _, r := range regions {
		if r.Confidence >= minConf && strings.TrimSpace(r.Text) != "" {
			keep = append(keep, r)
		}
	}
	order := readingOrder(keep)
	var out []run
	for i := range order {
		cur := keep[order[i]]
		box, text, conf := cur.Box, cur.Text, cur.Confidence
		out = append(out, newRun(box, text, conf))
		for n := 1; n < maxRun && i+n < len(order); n++ {
			prev, next := keep[order[i+n-1]], keep[order[i+n]]
			if !adjacent(prev.Box, next.Box) {
				break
			}
			box = box.Union(next.Box)
			text += " " + next.Text
			if next.Confidence < conf {
				conf = next.Confidence
			}
			out = append(out, newRun(box, text, conf))
		}
	}
	return out
}

func newRun(box image.Rectangle, text string, conf float64) run {
	n := normalize(text)
	return run{box: box, text: text, norm: n, nums: numbers(n), conf: conf}
}

// adjacent says whether two detections are close enough to be one piece of
// printed text: side by side on a line with a gap no wider than a
// character, or one line under the other in the same column. Joining
// detections that are not adjacent would invent text the label does not
// print, which is the way this stage could assert something false.
func adjacent(a, b image.Rectangle) bool {
	h := min(a.Dy(), b.Dy())
	if h <= 0 {
		return false
	}
	// Side by side: the two share a band, and the gap between them is no
	// wider than the type is tall.
	overlapY := min(a.Max.Y, b.Max.Y) - max(a.Min.Y, b.Min.Y)
	if overlapY > h/2 {
		gap := b.Min.X - a.Max.X
		if gap < 0 {
			gap = -gap
		}
		return gap <= h
	}
	// Stacked: the next line sits under this one, overlapping it across
	// more than half its width, within a line's leading.
	overlapX := min(a.Max.X, b.Max.X) - max(a.Min.X, b.Min.X)
	if overlapX*2 < min(a.Dx(), b.Dx()) {
		return false
	}
	gap := b.Min.Y - a.Max.Y
	return gap >= -h/2 && gap <= h
}

// readingOrder sorts detections into lines and each line left to right,
// which is the order a run may join them in.
func readingOrder(rs []Region) []int {
	idx := make([]int, len(rs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		a, b := rs[idx[i]].Box, rs[idx[j]].Box
		if a.Min.Y != b.Min.Y {
			return a.Min.Y < b.Min.Y
		}
		return a.Min.X < b.Min.X
	})
	var lines [][]int
	for _, i := range idx {
		placed := false
		for l := range lines {
			last := rs[lines[l][len(lines[l])-1]].Box
			cur := rs[i].Box
			h := min(last.Dy(), cur.Dy())
			if h > 0 && min(last.Max.Y, cur.Max.Y)-max(last.Min.Y, cur.Min.Y) > h/2 {
				lines[l] = append(lines[l], i)
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, []int{i})
		}
	}
	out := make([]int, 0, len(rs))
	for _, l := range lines {
		sort.SliceStable(l, func(i, j int) bool { return rs[l[i]].Box.Min.X < rs[l[j]].Box.Min.X })
		out = append(out, l...)
	}
	return out
}

// spellings is every accepted spelling of a claim, with the value each
// stands for.
func spellings(c Claim) []candidate {
	seen := map[string]bool{}
	var out []candidate
	add := func(text, num string, value float64, name string, claimed bool, form int) {
		n := normalize(text)
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, candidate{text: text, norm: n, value: value, name: name, claimed: claimed, form: form, num: num})
	}
	if c.Numeric == nil {
		add(c.Expected, "", 0, c.Expected, true, -1)
		for _, cand := range c.Candidates {
			add(cand.Text, "", 0, cand.Value, true, -1)
		}
		return out
	}
	// A number is not matched as a string but chosen among the values the
	// field may legally hold, each in every form the regulation allows it
	// to be printed in. The winner names a value, so the margin separates
	// 14.5 from 14.0 rather than separating spellings.
	want, _ := strconv.ParseFloat(c.Expected, 64)
	for _, v := range c.Numeric.Valid {
		name := trimNum(v)
		claimed := math.Abs(v-want) <= c.Numeric.Tolerance
		for fi, f := range c.Numeric.Formats {
			printed := v
			if f.Scale != 0 {
				printed = v / f.Scale
			}
			for _, s := range printings(printed) {
				add(strings.Replace(f.Template, "{n}", s, 1), normalize(s), v, name, claimed, fi)
			}
		}
	}
	return out
}

// printings are the ways a number appears on a label: with as many
// decimals as it needs, and to one and two places, which is not the same
// thing. A label setting "Alc. 13.0% by Vol." prints a trailing zero the
// value does not have, and without that spelling the nearest candidate to
// it is 13.5, which is a wrong value asserted at a smaller distance than
// the right one. A fluid-ounce figure needs the rounding for the opposite
// reason: 750 mL is printed as 25.4 FL OZ.
func printings(v float64) []string {
	out := []string{trimNum(v)}
	seen := map[string]bool{out[0]: true}
	for _, d := range []int{1, 2} {
		s := strconv.FormatFloat(v, 'f', d, 64)
		t := s
		if strings.Contains(t, ".") {
			t = strings.TrimRight(strings.TrimRight(t, "0"), ".")
		}
		for _, x := range []string{s, t} {
			if x != "" && !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	return out
}

func trimNum(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// scored is one candidate matched against one run.
type scored struct {
	cand candidate
	run  run
	dist float64
}

// nearest finds the closest run to any candidate the filter admits.
func nearest(cands []candidate, rs []run, radius float64, keep func(candidate) bool) *scored {
	var best *scored
	for i := range cands {
		cd := &cands[i]
		if keep != nil && !keep(*cd) {
			continue
		}
		n := float64(len(cd.norm))
		lo, hi := n*(1-radius), n*(1+radius)
		for j := range rs {
			r := &rs[j]
			// An alignment cannot be inside the radius when the two
			// strings differ in length by more than the radius allows.
			if l := float64(len(r.norm)); l < lo || l > hi {
				continue
			}
			if cd.num != "" && !holds(r.nums, cd.num) {
				continue // the number itself has to be read exactly
			}
			d := distance(cd.norm, r.norm)
			if d > radius {
				continue
			}
			// Between two spellings that fit equally, the longer one is
			// the better explanation, because it accounts for more of
			// what the label prints and the shorter is usually a piece of
			// it: a detection that clipped the seven off "750 ML" reads
			// "50ML", which is exactly a smaller standard of fill.
			if best != nil {
				if d > best.dist+1e-9 {
					continue
				}
				if d > best.dist-1e-9 && len(cd.norm) <= len(best.cand.norm) {
					continue
				}
			}
			best = &scored{cand: *cd, run: *r, dist: d}
		}
	}
	return best
}

// decide judges one claim against what the reader read. It is the spec's
// own rule 7.3, over recognised text instead of bit codes: the claim's own
// value is measured against the reading, every other value the field may
// hold is measured against the same reading, and the two distances decide.
//
// The margin is not symmetric, and the reason is not that the two errors
// differ in gravity - both are false assertions - but that the application
// is prior information. When a reading fits the filed value and fits
// another legal value nearly as well, the filed value is the better
// hypothesis and the evidence has not overturned it. Contradicting the
// application therefore has to be won by the margin; agreeing with it does
// not. Step 19c measured what happens without this: a recogniser that
// dropped the point in "8.5% alc/vol" reads a legal 85%, and an engine
// with a symmetric rule asserts that the label carries 85 percent alcohol.
func (e *Engine) decide(c Claim, rs []run) Verdict {
	v := Verdict{Claim: c.Name, Status: NotFound, Expected: c.Expected}
	if !c.Required {
		v.Status = Skipped
	}
	cands := spellings(c)
	radius := e.opt.Radius
	if c.Numeric != nil {
		radius = e.opt.NumericRadius
	}
	if c.Radius > 0 {
		radius = c.Radius
	}
	// The claim's own value plays two parts, and they need different
	// searches. To verify, its number has to be read exactly and its
	// spelling has to be inside the radius. To be protected by the
	// margin, it only has to be visible: a reading a margin beyond the
	// radius, with whatever the recogniser made of the digits, is still
	// evidence that the reading is ambiguous rather than decisive. Keeping
	// the two apart is what stops "(10 Proof)" read as "(101Proof)" from
	// being asserted as a hundred and one proof.
	exact := nearest(cands, rs, radius, func(cd candidate) bool { return cd.claimed })
	own := nearest(loosen(cands), rs, radius+e.opt.TieMargin, func(cd candidate) bool { return cd.claimed })
	other := nearest(cands, rs, radius, func(cd candidate) bool { return !cd.claimed })
	if own == nil && exact == nil && other == nil {
		v.Reason = "not_found"
		return v
	}
	verified := exact != nil && (other == nil || exact.dist <= other.dist+e.opt.TieMargin)
	mismatch := !verified && other != nil && (own == nil || other.dist+e.opt.TieMargin < own.dist)
	win, lose := exact, other
	if !verified {
		win, lose = other, own
	}
	if win == nil {
		v.Reason = "not_found"
		return v
	}
	ev := &Evidence{
		Region: win.run.box, Read: win.run.text, Matched: win.cand.text,
		Distance: win.dist, Radius: radius, Confidence: win.run.conf,
	}
	if lose != nil {
		ev.Competitor = lose.cand.text
		ev.CompDist = lose.dist
	}
	if c.Numeric != nil {
		ev.Reading = win.cand.name
	}
	v.Evidence = ev
	v.Observed = win.cand.name
	switch {
	case verified:
		v.Status = Verified
	case mismatch && own != nil && shorterThanClaimed(cands, win):
		// The reading is shorter than the claim's own value set in the
		// same printed form, so what separates them could be ink the
		// recogniser missed rather than ink the label does not carry.
		// Step 7a made this rule for the retired reader, where a "12%"
		// read as "2%" had to review rather than contradict; a detector
		// and a recogniser drop characters too. Measured: it refuses
		// every wrong value the two corpora produced this way and keeps
		// the wrong values they print on purpose, which are substituted
		// digits rather than missing ones.
		v.Status = Review
		v.Reason = "reading_short"
	case mismatch:
		v.Status = Mismatch
		v.Reason = "read " + win.cand.name
	default:
		// The filed value is too far from the reading to verify and the
		// other is not far enough ahead to name: the engine says it
		// cannot tell rather than asserting either.
		v.Status = Review
		v.Reason = "between:" + own.cand.name + "," + other.cand.name
	}
	return v
}

// shorterThanClaimed says whether the reading the winner was matched
// against is shorter than the claim's own value would be in the same
// printed form. A verdict that contradicts the application on a reading
// with characters missing rests on absence of evidence. It is asked only
// when the claim's own value is near the reading in the first place: a
// value that is plainly not the filed one is named however long it is.
func shorterThanClaimed(cands []candidate, win *scored) bool {
	best := -1
	for i := range cands {
		cd := &cands[i]
		if !cd.claimed || cd.form != win.cand.form {
			continue
		}
		if best < 0 || len(cd.norm) < best {
			best = len(cd.norm)
		}
	}
	return best > 0 && len(win.run.norm) < best
}

func holds(nums []string, want string) bool {
	for _, n := range nums {
		if n == want {
			return true
		}
	}
	return false
}

// loosen drops the requirement that a candidate's number be read exactly.
// It is used only to find the claim's own value for the margin to protect,
// never to verify or to name one.
func loosen(cands []candidate) []candidate {
	out := make([]candidate, len(cands))
	copy(out, cands)
	for i := range out {
		out[i].num = ""
	}
	return out
}
