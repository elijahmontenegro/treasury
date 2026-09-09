package verify

import (
	"image"
	"strings"
)

// This file verifies a reference: text known to be printed in the image
// verbatim, which for the TTB case is the statutory health warning of
// 27 CFR 16.21 and the header "GOVERNMENT WARNING:" at its front.
//
// Step 19a retired the mechanism that used to do this along with the
// engine it belonged to, and step 30a found the consequence: `Verify`
// had gone on taking a `[]Reference` and never reading it, so from 19a
// to here the engine verified nothing at all about the warning. It
// answered every question it was asked about the claims and no question
// about the one piece of text it is certain of.
//
// A reference is not a claim and the difference decides the mechanism.
//
//   - A claim is short, is printed once, and may be printed in any of
//     several accepted spellings. A reference is long, is quoted rather
//     than paraphrased, and wraps across as many lines as the panel
//     needs - the statute is 241 characters and comes back as five to
//     ten detections.
//   - A claim is allowed a radius because a name may be read imperfectly
//     and still be that name. A reference is allowed almost none,
//     because the regulation prescribes the words: what is being asked
//     is whether the label carries THIS text, and a label carrying
//     something else is the finding rather than a near miss.
//
// So the text is matched over a chain of detections, and the radius is
// near zero rather than the claims' 0.14.

// A reference is verified only on an EXACT match after normalisation,
// and step 30a measured why no radius would do instead.
//
// The corpus alters the statute on purpose, and one of its alterations
// is "women should not drink" to "women should NEVER drink": four
// characters in two hundred and forty-one, which measures 0.026. The
// fifty are approved labels whose warnings are compliant by
// construction, and the recogniser's own damage on them measures 0.000
// to 0.034 - `WMENSHOULNOTDINK` for `WOMEN SHOULD NOT DRINK`. The two
// ranges overlap, so there is no radius that admits a damaged reading of
// the true statute and refuses a clean reading of an altered one. A
// value between them would be fitted to the one corpus label that
// happens to sit above it.
//
// So the engine does not choose. It verifies what it read exactly,
// reviews what it read imperfectly, and says which. That is the same
// refusal the rest of this build makes: where the evidence cannot decide,
// the answer is that it cannot decide, and the reviewer is told what was
// read.
//
// referenceRadius is kept as the figure reported beside a verdict, not
// as a bound anything passes.
const referenceRadius = 0

// agreementFloor is how much of what was read must be accounted for by
// the reference before the engine will call the wording altered rather
// than badly read. Below it, the reading carries material the statute
// does not, in order, which damage does not produce.
const agreementFloor = 0.90

// maxReferenceRun is how many detections a reference may be chained
// from. A claim is capped at four (step 23b), which is right for a name
// printed on one line and cut in two; the statute needs one per line of
// the block it is set in, and the widest in the fifty takes eleven.
const maxReferenceRun = 14

// foundEnough is how much of a reference has to be read before the engine
// will say anything about its wording. Past this the warning is on the
// label and differs; short of it, too little was read to tell the
// difference between a label that alters the statute and a label whose
// statute the reader could not see.
const foundEnough = 0.5

// verifyReferences judges each reference against what was read.
func (e *Engine) verifyReferences(refs []Reference, regions []Region, img image.Image) ([]Verdict, []Verdict) {
	if len(refs) == 0 {
		return nil, nil
	}
	kept := make([]Region, 0, len(regions))
	for _, r := range regions {
		if r.Confidence >= e.opt.MinConfidence && strings.TrimSpace(r.Text) != "" {
			kept = append(kept, r)
		}
	}
	var text, emphasis []Verdict
	for i, ref := range refs {
		v, run := e.verifyReferenceText(ref, kept)
		v.Claim = referenceName(i)
		text = append(text, v)
		// The header is found at the front of the reference's own match,
		// so it is only there to be found where that match means
		// something. Where the warning was not read, the alignment puts
		// the statute's opening against whatever text happened to be
		// nearest, and judging the case or the weight of that is judging
		// a different piece of the label: it called 144 of half A's
		// compliant warnings title-case before this test was added.
		found := v.Status == Verified || v.Status == Review
		for j, span := range ref.Emphasis {
			var w Verdict
			if found {
				w = e.verifyEmphasis(ref, span, run, img)
			} else {
				w = Verdict{Status: NotFound, Reason: "warning_not_read",
					Expected: spanText(ref, span)}
			}
			w.Claim = referenceName(i) + "_emphasis"
			if len(ref.Emphasis) > 1 {
				w.Claim = referenceName(i) + "_emphasis_" + itoa(j+1)
			}
			emphasis = append(emphasis, w)
		}
	}
	return text, emphasis
}

// spanText is the text of one emphasis span of a reference.
func spanText(ref Reference, span Span) string {
	rs := []rune(ref.Text)
	if span.Start < 0 || span.End > len(rs) || span.Start >= span.End {
		return ""
	}
	return string(rs[span.Start:span.End])
}

func referenceName(i int) string {
	if i == 0 {
		return "reference"
	}
	return "reference_" + itoa(i+1)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// refRun is a chain of detections carrying a reference, and where in it
// the reference was found.
type refRun struct {
	members  []Region
	text     string // the chain's reading as printed
	norm     string
	at       []int
	from, to int
	dist     float64
}

// quote gives back the part of the chain's reading that a span covered,
// as printed rather than as normalised, so the case the recogniser read
// survives - which is what the capitals test needs.
func (r *refRun) quote(from, to int) string {
	if from < 0 || to > len(r.norm) || from >= to || to >= len(r.at) {
		return ""
	}
	return strings.TrimSpace(r.text[r.at[from]:r.at[to]])
}

// verifyReferenceText finds the best chain of detections for the
// reference and judges it.
//
// The chain is grown greedily rather than by enumerating every chain the
// way claims are: a claim is short enough that all its chains can be
// built and compared, and a reference is not - eleven detections deep,
// the enumeration is millions of runs. Greedy is sound here for a reason
// the statute supplies: its lines are consecutive, so at every step the
// continuation that best extends the text read so far is the next line
// of the block, and the alternative is text that is not the statute at
// all.
func (e *Engine) verifyReferenceText(ref Reference, kept []Region) (Verdict, *refRun) {
	v := Verdict{Status: NotFound, Reason: "not_found", Expected: ref.Text}
	want := normalize(ref.Text)
	if want == "" || len(kept) == 0 {
		return v, nil
	}
	cc := e.opt.confusionHalfCost()
	order := readingOrder(kept)
	next := make([][]int, len(order))
	for i := range order {
		for j := range order {
			if i != j && follows(kept[order[i]].Box, kept[order[j]].Box) {
				next[i] = append(next[i], j)
			}
		}
	}
	var best *refRun
	for start := range order {
		// A chain worth growing begins with text that is part of the
		// reference: without this every detection on the label starts a
		// walk and most of them go nowhere.
		first := kept[order[start]]
		if infix(normalize(first.Text), want, cc) > 0.5 {
			continue
		}
		members := []Region{first}
		text := first.Text
		at := start
		// A detection may be taken once. Without this the walk revisits
		// lines it has already used - on two of half B's labels it read
		// "OPERATE MACHINERY AND MAY CAUSE HEALTH PROBLEMS" twice - and
		// the duplicate is text the statute does not contain in that
		// order, which then reads as altered wording on a compliant
		// label.
		used := make([]bool, len(order))
		used[start] = true
		for len(members) < maxReferenceRun && len(normalize(text)) < len(want) {
			pick, pickText, pickDist := -1, "", 2.0
			for _, j := range next[at] {
				if used[j] {
					continue
				}
				cand := text + " " + kept[order[j]].Text
				if d := prefixDistance(normalize(cand), want, cc); d < pickDist {
					pick, pickText, pickDist = j, cand, d
				}
			}
			if pick < 0 {
				break
			}
			members = append(members, kept[order[pick]])
			used[pick] = true
			text, at = pickText, pick
		}
		n, idx := normalizeIdx(text)
		d, from, to := infixSpan(want, n, cc)
		if best == nil || d < best.dist {
			best = &refRun{members: members, text: text, norm: n, at: idx, from: from, to: to, dist: d}
		}
	}
	if best == nil {
		return v, nil
	}
	box := best.members[0].Box
	conf := best.members[0].Confidence
	for _, m := range best.members[1:] {
		box = box.Union(m.Box)
		if m.Confidence < conf {
			conf = m.Confidence
		}
	}
	quote := best.norm
	if best.from < best.to && best.to < len(best.at) {
		quote = best.norm[best.from:best.to]
	}
	agreement, coverage := agree(quote, want)
	_ = referenceRadius
	v.Evidence = &Evidence{
		Region: box, Read: quote, Matched: want,
		Distance: best.dist, Radius: referenceRadius, Confidence: conf,
		Parts: len(best.members), Agreement: agreement, Coverage: coverage,
	}
	switch {
	case best.dist == 0:
		v.Status, v.Reason = Verified, ""
	case coverage < foundEnough:
		v.Status, v.Reason = NotFound, "warning_not_read"
	default:
		// Not "the wording differs", and step 30a measured why twice
		// over. The corpus alters the statute by four characters in two
		// hundred and forty-one - "should not drink" to "should never
		// drink" - which measures 0.026, while the recogniser's own
		// damage on the fifty's compliant warnings measures up to 0.034.
		// No threshold separates them. And a rule that called the
		// wording altered where the reading carried material the statute
		// does not asserted it falsely on two compliant labels, because
		// what carried the foreign material was this engine's own chain
		// revisiting a line.
		//
		// So the engine reports what it read and does not say whose
		// fault the difference is. A reviewer reading "WMENSHOULNOTDINK"
		// beside the statute can see in a moment what no threshold here
		// could decide.
		v.Status, v.Reason = Review, "warning_read_imperfectly"
	}
	_ = agreementFloor
	return v, best
}

// agree measures how much of what was read is accounted for by the
// reference, and how much of the reference was read, through their
// longest common subsequence.
//
// The two answer different questions and step 30a needs both. A label
// whose warning the recogniser mangled gives a reading that is the
// statute with characters missing: everything it says appears in the
// statute in order, so agreement is near one, and only coverage falls. A
// label whose warning has been altered gives a reading that says
// something the statute does not, so agreement falls whatever the
// coverage.
//
// That is the difference between "the reader could not see it" and "the
// label does not say it", and without it the engine called ten of the
// fifty's warnings altered when all ten were the statute read badly -
// `WMENSHOULNOTDINK` for `WOMEN SHOULD NOT DRINK`. It is step 7a's rule
// again, which step 19c restated as "a verdict may not contradict on a
// reading shorter than the claim's own value": a reading with characters
// missing does not contradict.
func agree(read, want string) (agreement, coverage float64) {
	if read == "" || want == "" {
		return 0, 0
	}
	a, b := []rune(read), []rune(want)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] || confuseTable[a[i-1]&127][b[j-1]&127] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	lcs := float64(prev[len(b)])
	return lcs / float64(len(a)), lcs / float64(len(b))
}

// prefixDistance is how far `read` is from being a prefix of `want`: the
// end of `want` is free, so a chain half built is not punished for the
// lines it has not reached yet. It is what makes growing a chain greedily
// possible at all.
func prefixDistance(read, want string, cc int) float64 {
	if read == "" {
		return 1
	}
	if len(read) > len(want) {
		read = read[:len(want)]
	}
	return bounded(read, want[:min(len(want), len(read)+8)], cc, -1)
}
