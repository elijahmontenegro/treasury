package verify

import (
	"context"
	"image"
	"sort"
	"strings"

	"treasury/internal/ocr"
)

// Diagnosis reports, for one claim, the measurements that separate the
// reasons a claim can go unverified (step 20a). Nothing here applies a
// radius or a rule: it says what was available to the decision, so that a
// claim lost because the text was never read can be told apart from one
// lost because the boxes did not line up with it, or because a rule
// refused what the engine had.
type Diagnosis struct {
	Claim   string  `json:"claim"`
	Status  string  `json:"status"`
	Reason  string  `json:"reason,omitempty"`
	Radius  float64 `json:"radius"`
	Numeric bool    `json:"numeric"`

	// Run is the smallest distance from an accepted spelling of the claim
	// to a run the engine actually built, with no radius applied. This is
	// what the decision saw.
	Run     float64 `json:"run"`
	RunText string  `json:"run_text,omitempty"`

	// Box is the smallest distance to the claim's text found *inside* a
	// single detection, allowing text either side of it to be free. A
	// small Box with a large Run means the detection holds the claim and
	// more, so the whole-run comparison could not match it.
	Box     float64 `json:"box"`
	BoxText string  `json:"box_text,omitempty"`
	// BoxLetter says a letter sits immediately before or after the span
	// the claim matched inside that detection. A claim bounded by letters
	// is a piece of a longer piece of the same kind of text - "ALE"
	// inside "INDIA PALE ALE" - which step 12b refused on merit; a claim
	// bounded by a digit or by the edge of the detection is a statement
	// of its own printed beside another.
	BoxLetter bool `json:"box_letter,omitempty"`

	// Seq is the same, over any contiguous sequence of detections in
	// reading order however long and however far apart. A small Seq with
	// a large Box means the claim's text is split across detections that
	// the run builder would not join.
	Seq     float64 `json:"seq"`
	SeqText string  `json:"seq_text,omitempty"`
	SeqLen  int     `json:"seq_len,omitempty"`

	// Pair is the best distance over two detections joined in either
	// order, whether or not the run builder would join them and whether
	// or not they are next to each other in reading order. A small Pair
	// with a large Run and a large Box is a claim whose words are all
	// read and never compared to it together, which is the shape a
	// logotype makes: one word set vertically, one horizontally.
	Pair     float64         `json:"pair"`
	PairA    string          `json:"pair_a,omitempty"`
	PairB    string          `json:"pair_b,omitempty"`
	PairBoxA image.Rectangle `json:"pair_box_a,omitempty"`
	PairBoxB image.Rectangle `json:"pair_box_b,omitempty"`
	PairStep int             `json:"pair_step,omitempty"` // how far apart in reading order

	// Figure says, for a numeric claim, whether the value the application
	// filed appears anywhere as a run of digits. A read figure with no
	// spelling inside the radius is a printed form the enumeration lacks,
	// which is the engine having no candidate rather than not reading.
	Figure bool `json:"figure,omitempty"`
}

// Diagnose reads the image once and reports, for every claim, both the
// verdict and the probes above.
func (e *Engine) Diagnose(ctx context.Context, img image.Image, claims []Claim) ([]Region, []Diagnosis, error) {
	read, err := e.reader.Read(img)
	if err != nil {
		return nil, nil, err
	}
	regions := make([]Region, 0, len(read))
	for _, r := range read {
		regions = append(regions, Region{Box: r.Box, Text: r.Text, Confidence: r.Confidence, Rotated: r.Rotated})
	}
	rs := buildRuns(regions, e.opt.MinConfidence)

	// The detections in reading order, so a sequence is contiguous in the
	// order a person would read them.
	kept := make([]Region, 0, len(regions))
	for _, r := range regions {
		if r.Confidence >= e.opt.MinConfidence && strings.TrimSpace(r.Text) != "" {
			kept = append(kept, r)
		}
	}
	order := readingOrder(kept)
	texts := make([]string, len(order))
	norms := make([]string, len(order))
	for i, ix := range order {
		texts[i] = kept[ix].Text
		norms[i] = normalize(kept[ix].Text)
	}

	var out []Diagnosis
	for _, c := range claims {
		d := Diagnosis{Claim: c.Name, Numeric: c.Numeric != nil, Run: 1, Box: 1, Seq: 1, Pair: 1}
		v := e.decide(c, rs)
		d.Status, d.Reason = v.Status.String(), v.Reason
		d.Radius = e.opt.Radius
		if c.Numeric != nil {
			d.Radius = e.opt.NumericRadius
		}
		cands := spellings(c)
		for i := range cands {
			cd := &cands[i]
			if !cd.claimed {
				continue
			}
			for j := range rs {
				if x := distance(cd.norm, rs[j].norm); x < d.Run {
					d.Run, d.RunText = x, rs[j].text
				}
			}
			for j := range norms {
				x, from, to := infixSpan(cd.norm, norms[j])
				if x < d.Box {
					d.Box, d.BoxText = x, texts[j]
					d.BoxLetter = (from > 0 && isLetter(rune(norms[j][from-1]))) ||
						(to < len(norms[j]) && isLetter(rune(norms[j][to])))
				}
			}
			// Any two detections, in either order, however far apart.
			for a := range norms {
				for b := range norms {
					if a == b {
						continue
					}
					if x := infix(cd.norm, norms[a]+norms[b]); x < d.Pair {
						d.Pair, d.PairA, d.PairB = x, texts[a], texts[b]
						d.PairBoxA, d.PairBoxB = kept[order[a]].Box, kept[order[b]].Box
						d.PairStep = b - a
					}
				}
			}
			// Any contiguous sequence of detections, joined, with the
			// claim allowed to sit anywhere inside it. The join is
			// bounded to twice the claim's own length: a long enough
			// concatenation contains a short claim by accident, which
			// would report a split where there is none.
			limit := 2*len(cd.norm) + 8
			for a := range norms {
				joined := ""
				for b := a; b < len(norms) && len(joined) < limit; b++ {
					joined += norms[b]
					if x := infix(cd.norm, joined); x < d.Seq {
						d.Seq, d.SeqLen = x, b-a+1
						d.SeqText = strings.Join(texts[a:b+1], " ")
					}
				}
			}
		}
		if c.Numeric != nil {
			for i := range cands {
				if !cands[i].claimed || cands[i].num == "" {
					continue
				}
				for j := range rs {
					if holds(rs[j].nums, cands[i].num) {
						d.Figure = true
					}
				}
			}
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Claim < out[j].Claim })
	return regions, out, nil
}

// infix is the edit distance from claim to the nearest substring of read,
// as a share of the claim's length: text before and after the match costs
// nothing. It answers "is the claim's text in there somewhere", which is a
// different question from the one the engine asks, and is used only to
// diagnose.
func infix(claim, read string) float64 {
	d, _, _ := infixSpan(claim, read)
	return d
}

func isLetter(r rune) bool { return r >= 'A' && r <= 'Z' }

// infixSpan is infix with the span it matched, so a caller can ask what
// sits on either side of it.
func infixSpan(claim, read string) (float64, int, int) {
	if claim == "" {
		return 1, 0, 0
	}
	a, b := []rune(claim), []rune(read)
	prev := make([]int, len(b)+1) // a free start anywhere in read
	cur := make([]int, len(b)+1)
	pfrom := make([]int, len(b)+1)
	cfrom := make([]int, len(b)+1)
	for j := range prev {
		pfrom[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0], cfrom[0] = i, 0
		for j := 1; j <= len(b); j++ {
			c, f := prev[j-1], pfrom[j-1]
			if a[i-1] != b[j-1] {
				c++
			}
			if v := prev[j] + 1; v < c {
				c, f = v, pfrom[j]
			}
			if v := cur[j-1] + 1; v < c {
				c, f = v, cfrom[j-1]
			}
			cur[j], cfrom[j] = c, f
		}
		prev, cur = cur, prev
		pfrom, cfrom = cfrom, pfrom
	}
	best, to := prev[0], 0
	for j, v := range prev { // a free end anywhere in read
		if v < best {
			best, to = v, j
		}
	}
	return float64(best) / float64(len(a)), pfrom[to], to
}

// Reach reports how near a claim's own text comes to being read at all:
// the smallest distance from an accepted spelling to any part of any
// detection, with whatever surrounds it free. It applies no radius, no
// margin and no rule, so it answers a question about the reader rather
// than about a verdict, which is what step 21c asks of a second detector
// or a second pass.
func Reach(c Claim, read []ocr.Region) (float64, string) {
	best, where := 1.0, ""
	regions := make([]Region, 0, len(read))
	for _, r := range read {
		regions = append(regions, Region{Box: r.Box, Text: r.Text, Confidence: r.Confidence})
	}
	rs := buildRuns(regions, 0)
	for _, cd := range spellings(c) {
		if !cd.claimed {
			continue
		}
		for i := range rs {
			d, from, to := infixSpan(cd.norm, rs[i].norm)
			if d < best {
				best, where = d, rs[i].quote(from, to)
			}
		}
	}
	return best, where
}
