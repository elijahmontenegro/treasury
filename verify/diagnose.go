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
	Box     float64         `json:"box"`
	BoxText string          `json:"box_text,omitempty"`
	BoxRect image.Rectangle `json:"box_rect,omitempty"`
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
func (e *Engine) Diagnose(ctx context.Context, img image.Image, refs []Reference, claims []Claim) ([]Region, []Diagnosis, Frame, error) {
	read, err := e.reader.Read(img)
	if err != nil {
		return nil, nil, Frame{}, err
	}
	if e.opt.Turned > 0 {
		more, err := e.reader.ReadTurned(img, read)
		if err != nil {
			return nil, nil, Frame{}, err
		}
		read = append(read, more...)
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

	cc := e.opt.confusionHalfCost()
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
				if x := distance(cd.norm, rs[j].norm, cc); x < d.Run {
					d.Run, d.RunText = x, rs[j].text
				}
			}
			for j := range norms {
				x, from, to := infixSpan(cd.norm, norms[j], cc)
				if x < d.Box {
					d.Box, d.BoxText, d.BoxRect = x, texts[j], kept[order[j]].Box
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
					if x := infix(cd.norm, norms[a]+norms[b], cc); x < d.Pair {
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
					if x := infix(cd.norm, joined, cc); x < d.Seq {
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
	return regions, out, frameOf(kept, refs), nil
}

// Frame is what a claim's position is measured against: the page, the
// tallest text on it, and where the reference block sits.
type Frame struct {
	Page    image.Rectangle `json:"page"`
	Tallest int             `json:"tallest"`
	Ref     image.Rectangle `json:"ref"`
	RefN    int             `json:"ref_n"`
}

// frameOf locates the reference block in the reading. Nothing in this
// engine looks for it any more, so it is found the way anything else is:
// a detection whose text is a piece of the known reference is part of it.
func frameOf(kept []Region, refs []Reference) Frame {
	f := Frame{Tallest: 0}
	for _, r := range kept {
		f.Page = f.Page.Union(r.Box)
		if r.Box.Dy() > f.Tallest {
			f.Tallest = r.Box.Dy()
		}
	}
	for _, ref := range refs {
		want := normalize(ref.Text)
		if want == "" {
			continue
		}
		for _, r := range kept {
			n := normalize(r.Text)
			if len(n) < 8 {
				continue
			}
			// The block is located by matching the statute, where a
			// shape confusion is neither here nor there; charge in full.
			if infix(n, want, 2) <= 0.25 {
				f.Ref = f.Ref.Union(r.Box)
				f.RefN++
			}
		}
	}
	return f
}

// infix is the edit distance from claim to the nearest substring of read,
// as a share of the claim's length: text before and after the match costs
// nothing. It answers "is the claim's text in there somewhere", which is a
// different question from the one the engine asks, and is used only to
// diagnose.
func infix(claim, read string, cc int) float64 {
	d, _, _ := infixSpan(claim, read, cc)
	return d
}

func isLetter(r rune) bool { return r >= 'A' && r <= 'Z' }

// infixSpan is infix with the span it matched, so a caller can ask what
// sits on either side of it.
//
// cc is the confusion cost in halves of an edit, as in distance: the two
// have to charge the same, or the whole-run path and the inside-a-
// detection path would disagree about how far apart the same two strings
// are, and which one a claim went down would change its distance.
func infixSpan(claim, read string, cc int) (float64, int, int) {
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
		cur[0], cfrom[0] = 2*i, 0
		x := a[i-1]
		for j := 1; j <= len(b); j++ {
			c, f := prev[j-1], pfrom[j-1]
			if y := b[j-1]; x != y {
				if x < 128 && y < 128 && confuseTable[x][y] {
					c += cc
				} else {
					c += 2
				}
			}
			if v := prev[j] + 2; v < c {
				c, f = v, pfrom[j]
			}
			if v := cur[j-1] + 2; v < c {
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
	return float64(best) / float64(2*len(a)), pfrom[to], to
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
			// Reach asks how near the claim's text comes to being read
			// at all, which is a question about the reader; a confusion
			// discount would answer a different one, so charge in full.
			d, from, to := infixSpan(cd.norm, rs[i].norm, 2)
			if d < best {
				best, where = d, rs[i].quote(from, to)
			}
		}
	}
	return best, where
}
