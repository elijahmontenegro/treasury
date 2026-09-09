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
	box    image.Rectangle
	text   string
	norm   string
	at     []int    // where each character of norm came from in text
	nums   []string // the numbers in it, as printed
	parts  int      // how many detections were joined to make it
	widest int      // the longest single member, in normalized characters
	conf   float64
	// hist counts the reading's characters by confusable class, so a
	// candidate that cannot possibly be within the radius is dropped
	// before any distance is computed (step 29b).
	hist [36]uint16
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

// confusable says whether two characters are ones a reader confuses
// because their printed forms are near-identical.
//
// The set is a property of the alphabet and not of any label, which is
// the whole of its justification. Step 27c derived the residual damage
// from the engine's own evidence and found two of these in it - an O
// returned as a 0 in "12 FL. 0Z.", a G as a C in "CRaPEViNE" - and also
// found three that are NOT of this kind: an E read as an A, a Y as a P,
// an L as an S, all from stylised display type on two labels. Those are
// a recogniser failing, not two shapes that look alike, and they are
// deliberately absent: a set fitted to the labels it is scored on is
// fitted to the report set, which is the objection step 23c raised
// against choosing a radius between 0.071 and 0.077.
//
// Both orders are covered by the caller, which tries the pair each way.
func confusable(a, b rune) bool {
	switch {
	case a == 'O' && b == '0', a == 'D' && b == '0', a == 'Q' && b == '0':
		return true
	case a == 'I' && b == '1', a == 'L' && b == '1':
		return true
	case a == 'G' && b == 'C':
		return true
	case a == 'S' && b == '5':
		return true
	case a == 'B' && b == '8':
		return true
	case a == 'Z' && b == '2':
		return true
	case a == 'U' && b == 'V':
		return true
	}
	return false
}

// symIdx maps a normalized character to 0..35. Normalization leaves only
// digits and capitals, so nothing else can appear.
func symIdx(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'A' && r <= 'Z':
		return int(r-'A') + 10
	}
	return -1
}

// canonIdx folds each confusable pair onto one class, so the histogram
// below cannot rule out a reading that says 0regon where the claim says
// Oregon (step 29b).
var canonIdx [36]uint8

func init() {
	for i := range canonIdx {
		canonIdx[i] = uint8(i)
	}
	for _, f := range [][2]rune{{'0', 'O'}, {'0', 'D'}, {'0', 'Q'}, {'1', 'I'}, {'1', 'L'},
		{'C', 'G'}, {'5', 'S'}, {'8', 'B'}, {'2', 'Z'}, {'U', 'V'}} {
		canonIdx[symIdx(f[1])] = canonIdx[symIdx(f[0])]
	}
}

// histogram counts the characters of a normalized string by class.
func histogram(s string) [36]uint16 {
	var h [36]uint16
	for _, r := range s {
		if i := symIdx(r); i >= 0 {
			h[canonIdx[i]]++
		}
	}
	return h
}

// beyond says the claim cannot be within budget half-edits of the reading,
// whatever alignment is tried, because the reading has too few characters
// of some class for the claim to draw on (step 29b).
//
// It is a lower bound and not a guess: a character of the claim that no
// character of the reading can supply has to be deleted or substituted
// against a different class, and either costs a full edit - two
// half-edits - so twice the shortfall can never exceed the true distance.
// A candidate it rules out could not have been inside the radius, so no
// verdict can move. It holds for the whole-run comparison and for a span
// inside a longer reading alike, since a span draws its characters from
// the same reading.
func beyond(claim, read *[36]uint16, budget int) bool {
	deficit := 0
	for i := range claim {
		if claim[i] > read[i] {
			deficit += int(claim[i] - read[i])
			if 2*deficit > budget {
				return true
			}
		}
	}
	return false
}

// confuseTable answers confusable in one indexed load. Deciding is the
// engine's slowest stage (step 26a) and this sits in the innermost loop
// of its edit distance, where a call and a switch cost real time: the
// first version of step 28c took the fifty's median from 3.4 s to 5.3 s
// and its p95 from 6.6 s to 18.0 s, all of it here.
var confuseTable [128][128]bool

func init() {
	for a := rune(0); a < 128; a++ {
		for b := rune(0); b < 128; b++ {
			confuseTable[a][b] = confusable(a, b) || confusable(b, a)
		}
	}
}

// subCost is what replacing a with b costs, in halves of an edit, so that
// the arithmetic stays in integers. A confusion costs `cc`, anything else
// a full 2. Callers in a hot loop inline this rather than call it.
func subCost(a, b rune, cc int) int {
	if a == b {
		return 0
	}
	if a < 128 && b < 128 && confuseTable[a][b] {
		return cc
	}
	return 2
}

// distance is the edit distance between two normalized strings as a share
// of the claim's own length, so a wrong character costs the same fraction
// of a short name as of a long one.
//
// cc is what a confusable substitution costs in halves of an edit: 2 is
// no discount at all, 1 is the half step 27c measured and step 28c
// adopted.
func distance(claim, read string, cc int) float64 { return bounded(claim, read, cc, -1) }

// bounded is distance with a ceiling: once every cell of a row is past
// `budget` half-edits, the answer cannot come back under it, because no
// step of the rest of an alignment costs less than nothing. The work
// stops there and a value above the budget is returned. A negative
// budget means no ceiling.
//
// Step 29b added it. The caller's budget is what the radius allows, and
// a distance already past the radius is one the decision discards, so
// the exact value was never wanted - which is why this cannot move a
// verdict.
func bounded(claim, read string, cc, budget int) float64 {
	if claim == "" {
		return 1
	}
	a, b := []rune(claim), []rune(read)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = 2 * j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = 2 * i
		x := a[i-1]
		lowest := prev[0]
		for j := 1; j <= len(b); j++ {
			c := prev[j-1]
			if y := b[j-1]; x != y {
				if x < 128 && y < 128 && confuseTable[x][y] {
					c += cc
				} else {
					c += 2
				}
			}
			if v := prev[j] + 2; v < c {
				c = v
			}
			if v := cur[j-1] + 2; v < c {
				c = v
			}
			cur[j] = c
			if c < lowest {
				lowest = c
			}
		}
		prev, cur = cur, prev
		if budget >= 0 && lowest > budget {
			return float64(budget+1) / float64(2*len(a))
		}
	}
	return float64(prev[len(b)]) / float64(2*len(a))
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
	var walk func(at int, box image.Rectangle, text string, conf float64, n, widest int)
	walk = func(at int, box image.Rectangle, text string, conf float64, n, widest int) {
		out = append(out, newRun(box, text, conf, n, widest))
		if n == maxRun {
			return
		}
		for _, j := range next[at] {
			r := keep[order[j]]
			c := conf
			if r.Confidence < c {
				c = r.Confidence
			}
			w := widest
			if l := len(normalize(r.Text)); l > w {
				w = l
			}
			walk(j, box.Union(r.Box), text+" "+r.Text, c, n+1, w)
		}
	}
	for i := range order {
		r := keep[order[i]]
		walk(i, r.Box, r.Text, r.Confidence, 1, len(normalize(r.Text)))
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

func newRun(box image.Rectangle, text string, conf float64, parts, widest int) run {
	n, at := normalizeIdx(text)
	return run{box: box, text: text, norm: n, at: at, nums: numbers(n),
		parts: parts, widest: widest, conf: conf, hist: histogram(n)}
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

// ends says whether the claim begins and finishes where the span does.
//
// A delimiter says where a statement ends on the label; it does not say
// that the claim ends there too. Step 28d searched chained runs for the
// first time and found the difference at once: label 0028 prints
// "BOTTLED BY APONA VINEYARDS, VENETA, OR" and the application files
// "Apona Vineyards, LLC", so the span ending at the comma after
// VINEYARDS is delimited, is inside the radius, and is missing the
// claim's last three characters. The engine asserted a company form the
// label does not print, which is step 12b's first refusal - "a legal
// suffix the label does not print" - arriving from the other direction.
//
// This is the rule step 7a made for numbers and step 20b remade as "a
// value that is the claim's own figure with digits missing from an end
// may not be named", stated for names: a name taken from inside a longer
// reading has to be the whole name. Characters may be wrong within it,
// which is what the radius is for; they may not be absent from its ends,
// because then the delimiter is marking the end of something else.
func ends(claim, span string, cc int) bool {
	a, b := []rune(claim), []rune(span)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return subCost(a[0], b[0], cc) < 2 && subCost(a[len(a)-1], b[len(b)-1], cc) < 2
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
func nearest(cands []candidate, rs []run, radius float64, cc int, keep func(candidate) bool) *scored {
	var best *scored
	for i := range cands {
		cd := &cands[i]
		if keep != nil && !keep(*cd) {
			continue
		}
		n := float64(len(cd.norm))
		lo, hi := n*(1-radius), n*(1+radius)
		hi = math.Inf(1) // the reading may hold another statement too
		// What the radius allows, in the half-edits the distance counts.
		budget := int(radius * 2 * n)
		ch := histogram(cd.norm)
		for j := range rs {
			r := &rs[j]
			// An alignment cannot be inside the radius when the two
			// strings differ in length by more than the radius allows.
			if l := float64(len(r.norm)); l < lo || l > hi {
				continue
			}
			// Nor when the reading has too few characters of some class
			// for the claim to draw on (step 29b).
			if beyond(&ch, &r.hist, budget) {
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
			d, from, to := bounded(cd.norm, r.norm, cc, budget), 0, len(r.norm)
			switch {
			case cd.form >= 0:
				// A number, exact or loosened for the margin, is looked
				// for inside the reading: its unit delimits it. The
				// figure test above is what the loosened form drops, and
				// nothing else.
				d, from, to = infixSpan(cd.norm, r.norm, cc)
			case len(r.norm) > len(cd.norm) && len(r.norm) <= 3*len(cd.norm)+24 &&
				(r.parts == 1 || len(cd.norm) > r.widest):
				// A chain is searched only where the claim is longer
				// than any one of its members, so the chain is
				// genuinely needed to hold it. Where a claim fits
				// inside a single detection, the single-detection
				// search above already finds it, and searching every
				// chain as well multiplies the work by the number of
				// runs a dense label builds - step 26a measured 2,590
				// on one of the fifty.
				// A name may be taken from inside a reading when
				// punctuation, a digit or the reading's own edge
				// delimits it at both ends.
				//
				// Step 21b allowed this inside a single detection only,
				// on the reasoning that "a join is a construction of
				// this engine rather than a line the label printed".
				// That was true when it was written and stopped being
				// true at step 23b, which made a run a chain of
				// detections each adjacent to the one before it - so a
				// chain is a printed line the detector cut up, not an
				// arbitrary pairing, and the reasoning no longer
				// applies to it. Step 27a found two claims of exactly
				// that shape: 0050's permittee across two detections
				// stacked ONE pixel apart at a type height of 27, and
				// 0033's brand across two stacked eleven apart at 52.
				//
				// What does not change is the delimiter test, and that
				// is what keeps step 16a's shape refused. Chains are
				// joined with a space, and a space has never been a
				// delimiter, so extending the search cannot by itself
				// admit anything: a span still has to end at a mark, a
				// digit or an edge. TestTheGuard is what says so.
				if id, ifrom, ito := infixSpan(cd.norm, r.norm, cc); id < d &&
					r.bounded(ifrom, ito) && ends(cd.norm, r.norm[ifrom:ito], cc) {
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
	cc := e.opt.confusionHalfCost()
	exact := nearest(cands, rs, radius, cc, func(cd candidate) bool { return cd.claimed })
	own := nearest(loosen(cands), rs, radius+e.opt.TieMargin, cc, func(cd candidate) bool { return cd.claimed })
	other := nearest(cands, rs, radius, cc, func(cd candidate) bool { return !cd.claimed })
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
			near = nearest(cands, rs, 2*radius, cc, func(cd candidate) bool { return cd.claimed })
		}
		if near != nil && near.dist <= 2*radius {
			v.Evidence = &Evidence{
				Region: near.run.box, Read: near.run.quote(near.from, near.to),
				Matched: near.cand.text, Distance: near.dist, Radius: radius,
				Confidence: near.run.conf, Parts: near.run.parts,
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
		Parts: win.run.parts,
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
