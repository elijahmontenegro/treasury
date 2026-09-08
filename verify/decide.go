package verify

import (
	"image"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
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
	box   image.Rectangle
	text  string
	norm  string
	at    []int    // where each character of norm came from in text
	nums  []string // the numbers in it, as printed
	parts int      // how many detections were joined to make it
	conf  float64
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
	n, _ := normalizeIdx(s)
	return n
}

// normalizeIdx is normalize with the byte offset in s of the rune that
// produced each character it kept, so a matched span can be quoted back
// as the label printed it.
func normalizeIdx(s string) (string, []int) {
	var b strings.Builder
	idx := make([]int, 0, len(s))
	rs := []rune(s)
	off := make([]int, len(rs)+1)
	n := 0
	for i, r := range rs {
		off[i] = n
		n += utf8.RuneLen(r)
	}
	off[len(rs)] = n
	for i, r := range rs {
		r = unicode.ToUpper(r)
		if f, ok := fold[r]; ok {
			r = f
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			idx = append(idx, off[i])
		case r == '.' || r == ',':
			// A separator between two digits is the number, not
			// punctuation: dropping it makes 4.5 percent and 45 percent
			// the same string, which is a wrong value asserted rather
			// than a spelling set aside. The comma is the same
			// separator, which is the equivalence 12b adopted for
			// labels that print "14,5%".
			if i > 0 && i+1 < len(rs) && unicode.IsDigit(rs[i-1]) && unicode.IsDigit(rs[i+1]) {
				b.WriteRune('.')
				idx = append(idx, off[i])
			}
		}
	}
	idx = append(idx, off[len(rs)])
	return b.String(), idx
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
//
// A run is a chain of detections each following the one before it on the
// page. Until step 23b it was a slice of the flattened reading order as
// well, and that was the wrong requirement: the order is a convenience
// for presenting detections, not a statement about the label, and it put
// other text between the two halves of one printed statement on nine of
// the twelve claims 23a found split. The geometric test is unchanged.
func buildRuns(regions []Region, minConf float64) []run {
	var keep []Region
	for _, r := range regions {
		if r.Confidence >= minConf && strings.TrimSpace(r.Text) != "" {
			keep = append(keep, r)
		}
	}
	order := readingOrder(keep)
	// Who may follow whom, in reading order so the chains are the same
	// whatever order the reader returned its detections in.
	next := make([][]int, len(order))
	for i := range order {
		for j := range order {
			if i != j && follows(keep[order[i]].Box, keep[order[j]].Box) {
				next[i] = append(next[i], j)
				if len(next[i]) == maxFollow {
					break
				}
			}
		}
	}
	var out []run
	var walk func(at int, box image.Rectangle, text string, conf float64, n int)
	walk = func(at int, box image.Rectangle, text string, conf float64, n int) {
		out = append(out, newRun(box, text, conf, n))
		if n == maxRun {
			return
		}
		for _, j := range next[at] {
			r := keep[order[j]]
			c := conf
			if r.Confidence < c {
				c = r.Confidence
			}
			walk(j, box.Union(r.Box), text+" "+r.Text, c, n+1)
		}
	}
	for i := range order {
		r := keep[order[i]]
		walk(i, r.Box, r.Text, r.Confidence, 1)
	}
	return dedupe(out)
}

// dedupe drops runs whose normalized text another run already carries. A
// dense label chains its detections into thousands of runs and most of
// them read alike; comparing every claim to each is where step 26a found
// the time going. Of two runs that compare identically, the one kept is a
// single detection over a chain and the smaller box over the larger,
// which is the preference the decision already applies.
func dedupe(rs []run) []run {
	best := make(map[string]int, len(rs))
	for i := range rs {
		j, seen := best[rs[i].norm]
		if !seen || better(rs[i], rs[j]) {
			best[rs[i].norm] = i
		}
	}
	out := rs[:0:0]
	for i := range rs {
		if best[rs[i].norm] == i {
			out = append(out, rs[i])
		}
	}
	return out
}

func better(a, b run) bool {
	if (a.parts == 1) != (b.parts == 1) {
		return a.parts == 1
	}
	return a.box.Dx()*a.box.Dy() < b.box.Dx()*b.box.Dy()
}

func newRun(box image.Rectangle, text string, conf float64, parts int) run {
	n, at := normalizeIdx(text)
	return run{box: box, text: text, norm: n, at: at, nums: numbers(n), parts: parts, conf: conf}
}

// quote gives back the part of a reading a match covered, as printed.
func (r run) quote(from, to int) string {
	if from >= to || to >= len(r.at) {
		return r.text
	}
	return strings.TrimSpace(r.text[r.at[from]:r.at[to]])
}

// bounded says whether a span of a reading is delimited at both ends by
// something that is not more of the same name: a punctuation mark, a
// digit, or the edge of the detection. Step 20b took a number from inside
// a longer reading because its unit delimits it and said a name had
// nothing playing that part; step 21a read the twenty-five claims the
// whole-run rule refuses and found that a name does have one. Where the
// filed string is printed whole with other matter around it, what abuts it
// is a comma, a dash, a colon, a digit or the end of the detection; where
// the filed string is part of a longer name, what abuts it is another
// letter of that name. "APONA VINEYARDS, VENETA, OR" gives up the brand;
// "Produced and Bottled by Valley Mill Company" does not, which is the
// false assertion of step 16a.
func (r run) bounded(from, to int) bool {
	return r.free(from, -1) && r.free(to, 1)
}

func (r run) free(at, dir int) bool {
	next := at
	if dir < 0 {
		next = at - 1
	}
	if next < 0 || next >= len(r.norm) {
		return true // the edge of the detection
	}
	if c := r.norm[next]; c >= '0' && c <= '9' {
		return true // a figure is a different statement
	}
	// Anything normalization dropped between the two characters is
	// punctuation or space, since letters and digits are kept. A mark
	// between them delimits; a bare space does not.
	lo, hi := next, at
	if dir > 0 {
		lo, hi = at-1, at
	}
	if lo < 0 || hi >= len(r.at) {
		return true
	}
	_, size := utf8.DecodeRuneInString(r.text[r.at[lo]:])
	gap := r.text[r.at[lo]+size : r.at[hi]]
	return strings.ContainsFunc(gap, func(c rune) bool { return !unicode.IsSpace(c) })
}

// maxFollow bounds how many detections one may be chained to, so a dense
// label cannot make the number of runs grow without limit.
const maxFollow = 4

// follows says whether b reads as the continuation of a: to its right on
// the same band with a gap no wider than a character, or on the line under
// it in the same column. Joining detections that do not follow one another
// would invent text the label does not print, which is the way this stage
// could assert something false.
func follows(a, b image.Rectangle) bool {
	h := min(a.Dy(), b.Dy())
	if h <= 0 {
		return false
	}
	// Side by side: the two share a band, b starts no earlier than a, and
	// it begins within a character's width of where a ends. Starting no
	// earlier is what keeps the joined text in the order the label prints
	// it; the gap may be negative, because two detections of one word
	// commonly overlap - on label 0037 the large B of "BEER" and the
	// "EER" beside it do.
	overlapY := min(a.Max.Y, b.Max.Y) - max(a.Min.Y, b.Min.Y)
	if overlapY > h/2 {
		return b.Min.X >= a.Min.X && b.Min.X-a.Max.X <= h
	}
	// Stacked: b sits under a, overlapping it across more than half the
	// narrower of the two, within a line's leading.
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

// numericCache holds the spellings of a numeric field, which depend on
// the field's own vocabulary and forms and not on the label being read.
// Building them is thousands of strings; step 25c added forms that made
// that worse and step 26a measured it.
var numericCache sync.Map // string -> []candidate

func numericKey(n *Numeric) string {
	var b strings.Builder
	for _, f := range n.Formats {
		b.WriteString(f.Template)
		b.WriteByte(0)
		b.WriteString(strconv.FormatFloat(f.Scale, 'g', -1, 64))
		b.WriteByte(0)
	}
	b.WriteByte('|')
	for _, v := range n.Valid {
		b.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
		b.WriteByte(',')
	}
	return b.String()
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
	key := numericKey(c.Numeric)
	if v, ok := numericCache.Load(key); ok {
		// Only which value the application filed differs between labels.
		cached := v.([]candidate)
		out = make([]candidate, len(cached))
		copy(out, cached)
		for i := range out {
			out[i].claimed = math.Abs(out[i].value-want) <= c.Numeric.Tolerance
		}
		return out
	}
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
	numericCache.Store(key, append([]candidate{}, out...))
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
	cand     candidate
	run      run
	dist     float64
	from, to int
	// span is how much of the reading the winner was matched against. For
	// a name it is the whole run; for a number it is the part of the run
	// the number's own statement occupies, since a label prints the
	// alcohol content and the fill on one line and a detection returns
	// them together.
	span int
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
		hi = math.Inf(1) // the reading may hold another statement too
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
			// A number is delimited by its own unit, so it can be taken
			// from inside a longer reading: "750ML" in
			// "53%ALC/VOLNET.CONT.750ML" is the fill however much else
			// the detection holds, because the figure has to be a whole
			// number of the reading and the unit has to sit against it.
			// A name has no unit to bound it, which is why "Valley Mill"
			// inside "Valley Mill Distillery" is indistinguishable from
			// the brand and is not taken (step 16a).
			d, from, to := distance(cd.norm, r.norm), 0, len(r.norm)
			switch {
			case cd.form >= 0:
				// A number, exact or loosened for the margin, is looked
				// for inside the reading: its unit delimits it. The
				// figure test above is what the loosened form drops, and
				// nothing else.
				d, from, to = infixSpan(cd.norm, r.norm)
			case r.parts == 1 && len(r.norm) > len(cd.norm) &&
				len(r.norm) <= 3*len(cd.norm)+24:
				// A name may be taken from inside one detection when
				// punctuation, a digit or the detection's own edge
				// delimits it at both ends. Joins are not searched this
				// way: every claim step 21a found printed whole inside a
				// longer reading was inside a single detection, and a
				// join is a construction of this engine rather than a
				// line the label printed.
				if id, ifrom, ito := infixSpan(cd.norm, r.norm); id < d && r.bounded(ifrom, ito) {
					d, from, to = id, ifrom, ito
				}
			}
			span := to - from
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
				if d > best.dist-1e-9 && len(cd.norm) < len(best.cand.norm) {
					continue
				}
				// Among readings that fit equally, the smallest one that
				// holds the match is the evidence: a verdict has to name
				// the line it rests on, not four lines joined.
				if d > best.dist-1e-9 && len(cd.norm) == len(best.cand.norm) && span >= best.span {
					continue
				}
			}
			best = &scored{cand: *cd, run: *r, dist: d, span: span, from: from, to: to}
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
	// nothingDecided names the nearest reading within twice the radius,
	// so that a claim the engine could not decide still says what it
	// looked at, and so that step 25b knows which one box to read again.
	nothingDecided := func() Verdict {
		v.Reason = "not_found"
		// The claim's own value has already been looked for a margin
		// beyond the radius, so where that found something there is
		// nothing more to search for.
		near := own
		if near == nil {
			near = nearest(cands, rs, 2*radius, func(cd candidate) bool { return cd.claimed })
		}
		if near != nil && near.dist <= 2*radius {
			v.Evidence = &Evidence{
				Region: near.run.box, Read: near.run.quote(near.from, near.to),
				Matched: near.cand.text, Distance: near.dist, Radius: radius,
				Confidence: near.run.conf,
			}
		}
		return v
	}
	if own == nil && exact == nil && other == nil {
		return nothingDecided()
	}
	verified := exact != nil && (other == nil || exact.dist <= other.dist+e.opt.TieMargin)
	mismatch := !verified && other != nil && (own == nil || other.dist+e.opt.TieMargin < own.dist)
	win, lose := exact, other
	if !verified {
		win, lose = other, own
	}
	if win == nil {
		return nothingDecided()
	}
	ev := &Evidence{
		Region: win.run.box, Read: win.run.quote(win.from, win.to), Matched: win.cand.text,
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
	case mismatch && digitsDropped(cands, win):
		// The value named is the claim's own figure with digits missing
		// from an end, or with digits added to one: a detection that
		// clipped the seven off "750 mL" reads a legal 50 mL. A reader
		// drops and doubles characters; it does not usually turn one
		// legal value into another, and where it could have, the engine
		// says so instead of naming one.
		v.Status = Review
		v.Reason = "figure_incomplete"
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
	return best > 0 && win.span < best
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

// digitsDropped says whether the value the winner names is the claim's own
// figure with digits missing from an end or added to one, printed in the
// same form. Two values that differ that way are not distinguishable from
// one value read badly.
func digitsDropped(cands []candidate, win *scored) bool {
	if win.cand.num == "" {
		return false
	}
	for i := range cands {
		cd := &cands[i]
		if !cd.claimed || cd.form != win.cand.form || cd.num == "" {
			continue
		}
		if strings.Contains(cd.num, win.cand.num) || strings.Contains(win.cand.num, cd.num) {
			return true
		}
	}
	return false
}
