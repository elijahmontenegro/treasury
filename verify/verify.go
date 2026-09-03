// Package verify is the engine's public surface. Give it an image, the text
// known to appear in it, and the claims to check; it returns a verdict per
// claim with evidence.
package verify

import (
	"context"
	"errors"
	"fmt"
	"image"

	"treasury/internal/alphabet"
	"treasury/internal/encoder"
	"treasury/internal/preprocess"
	"treasury/internal/region"
	"treasury/internal/render"
	"treasury/internal/spell"
)

// Span is a half-open rune range of a reference expected in a heavier weight.
type Span struct {
	Start, End int
}

// Reference is text known to be printed in the image verbatim.
type Reference struct {
	Text     string
	Emphasis []Span
}

// Candidate is one way a claim's value may be printed. Value is what gets
// reported as observed; several candidates may share a value.
type Candidate struct {
	Text  string
	Value string
}

// Claim is something the caller expects the image to say.
type Claim struct {
	Name       string
	Expected   string      // the value the caller asserts
	Candidates []Candidate // every value the claim could decode to; free text has one
	Required   bool
	Radius     float64 // Hamming radius as a fraction of the code length; 0 uses the default
	Variants   bool    // also spell casing, quote, and weight variants of each candidate; for free text
}

// Status is a verdict.
type Status int

const (
	NotFound Status = iota
	Verified
	Review
	Mismatch
	Skipped
)

func (s Status) String() string {
	switch s {
	case Verified:
		return "VERIFIED"
	case Review:
		return "REVIEW"
	case Mismatch:
		return "MISMATCH"
	case Skipped:
		return "SKIPPED"
	}
	return "NOT_FOUND"
}

// MarshalText renders the status name in JSON.
func (s Status) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Evidence is what a verdict rests on.
type Evidence struct {
	Region         image.Rectangle `json:"region"`
	Crop           *image.Gray     `json:"-"`
	Codeword       encoder.Patch   `json:"-"`
	Text           string          `json:"text"` // the candidate text that matched
	Params         spell.Params    `json:"params"`
	D1             int             `json:"d1"`
	D2             int             `json:"d2"`
	Radius         int             `json:"radius"`
	Bits           int             `json:"bits"`
	Refined        bool            `json:"refined"`   // glyph-wise alignment scored the match
	Glyphs         int             `json:"glyphs"`    // candidate glyphs when refined
	Penalized      int             `json:"penalized"` // merge/split/insert/delete steps when refined
	Competitor     string          `json:"competitor,omitempty"`
	CompetitorText string          `json:"competitor_text,omitempty"`
	Spread         float64         `json:"spread,omitempty"`    // the alphabet's within-character spread, which sets the tie margin
	Relearned      bool            `json:"relearned,omitempty"` // decided on the second pass, after glyphs learned from verified claims
	LowConfidence  bool            `json:"low_confidence"`

	// Reference rows.
	Matched         int                `json:"matched,omitempty"`
	Unexplained     int                `json:"unexplained,omitempty"`
	Outliers        int                `json:"outliers,omitempty"`
	PriorViolations int                `json:"prior_violations,omitempty"`
	MeanDistance    float64            `json:"mean_distance,omitempty"`
	Anomalies       []alphabet.Anomaly `json:"anomalies,omitempty"`

	// Emphasis spans.
	StrokeRatio float64 `json:"stroke_ratio,omitempty"`
}

// Verdict is the outcome for one claim.
type Verdict struct {
	Claim      string    `json:"claim"`
	Status     Status    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	Expected   string    `json:"expected"`
	Observed   string    `json:"observed,omitempty"`
	Candidates []string  `json:"candidates,omitempty"`
	Evidence   *Evidence `json:"evidence,omitempty"`
}

// AlphabetReport summarizes what the reference taught.
type AlphabetReport struct {
	Block       image.Rectangle       `json:"block"`
	Glyphs      int                   `json:"glyphs"`
	Matched     int                   `json:"matched"`
	Unexplained int                   `json:"unexplained"`
	Violations  float64               `json:"violations"` // share of matched glyphs contradicting their shape class
	Recovered   int                   `json:"recovered"`
	Spread      float64               `json:"spread"`
	Characters  string                `json:"characters"`
	Face        string                `json:"face"`
	XHeight     float64               `json:"x_height"`
	CapHeight   float64               `json:"cap_height"`
	LetterGap   float64               `json:"letter_gap"`
	WordGap     float64               `json:"word_gap"`
	Rows        []alphabet.RowQuality `json:"rows"`
	Learned     string                `json:"learned,omitempty"`   // characters learned from claims that verified decisively
	Unlearned   string                `json:"unlearned,omitempty"` // characters of the reference whose samples disagreed and were dropped
}

// Result is everything Verify found.
type Result struct {
	DeskewDeg float64         `json:"deskew_deg"`
	Regions   int             `json:"regions"`
	Alphabet  *AlphabetReport `json:"alphabet,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Reference []Verdict       `json:"reference,omitempty"` // one per row of the reference block
	Emphasis  []Verdict       `json:"emphasis,omitempty"`  // one per emphasis span
	Claims    []Verdict       `json:"claims"`
}

// Options tune the engine. Zero values take the defaults.
type Options struct {
	Encoder           string  // glyph encoder: "dual" (default; "hash" is accepted as its alias), "pos16" (positional view only), "sharp24" (24×24 binary, the naive grid)
	DefaultRadius     float64 // fraction of code length; 0.15
	TieMargin         float64 // glyph-wise: fraction of a glyph code per differing glyph; 0.01 (tuned on half A of the synthetic set; the spread term usually dominates)
	LineTieMargin     float64 // line-wise fallback: fraction of the line code; 0.04
	MaxCharSpread     float64 // per-character acceptance: a character whose samples spread beyond this is unlearned; 0.12
	MaxUnexplained    float64 // alphabet acceptance; 0.10
	LineThreshold     float64 // reference row acceptance, mean normalized distance; 0.08
	ViolationFraction float64 // alphabet and reference row acceptance, share of glyphs contradicting their shape class; 0.1
	RowFailAnomalies  float64 // anomaly weight (unexplained and strong outliers one, weak outliers half) at which a reference row fails rather than reviews; 2
	HeavyFactor       float64 // stroke ratio of the heavy hypothesis to the body; 1.25
	EmphasisGate      float64 // an emphasis span must be within this fraction of a hypothesis; 0.15
	GlareFraction     float64 // region overlap with the glare mask that flags low confidence; 0.3
	MinGlyphs         int     // a reading of fewer glyphs cannot decide a claim, only ask for review; 3
}

func (o Options) withDefaults() Options {
	if o.Encoder == "" {
		o.Encoder = "dual"
	}
	if o.DefaultRadius == 0 {
		o.DefaultRadius = 0.15
	}
	if o.TieMargin == 0 {
		o.TieMargin = 0.01
	}
	if o.LineTieMargin == 0 {
		o.LineTieMargin = 0.04
	}
	if o.MaxCharSpread == 0 {
		o.MaxCharSpread = 0.12
	}
	if o.MaxUnexplained == 0 {
		o.MaxUnexplained = 0.10
	}
	if o.LineThreshold == 0 {
		o.LineThreshold = 0.08
	}
	if o.ViolationFraction == 0 {
		o.ViolationFraction = 0.1
	}
	if o.RowFailAnomalies == 0 {
		o.RowFailAnomalies = 2
	}
	if o.HeavyFactor == 0 {
		o.HeavyFactor = 1.25
	}
	if o.EmphasisGate == 0 {
		o.EmphasisGate = 0.15
	}
	if o.GlareFraction == 0 {
		o.GlareFraction = 0.3
	}
	if o.MinGlyphs == 0 {
		o.MinGlyphs = 3
	}
	return o
}

// Engine verifies claims against images.
type Engine struct {
	opt      Options
	faces    []*render.Face
	lineEnc  encoder.Encoder
	glyphEnc encoder.Encoder
}

// New builds an engine.
func New(o Options) (*Engine, error) {
	o = o.withDefaults()
	faces, err := render.Bundled()
	if err != nil {
		return nil, err
	}
	var glyph encoder.Encoder
	switch o.Encoder {
	case "hash", "dual":
		glyph = encoder.Glyph()
	case "pos16":
		glyph = encoder.Hash{W: 16, H: 16, Levels: 4, Smooth: 1}
	case "sharp24":
		glyph = encoder.Hash{W: 24, H: 24, Levels: 2}
	default:
		return nil, fmt.Errorf("verify: unknown encoder %q", o.Encoder)
	}
	return &Engine{opt: o, faces: faces, lineEnc: encoder.Line(), glyphEnc: glyph}, nil
}

// Verify decodes the image. It learns the alphabet from the first reference;
// if that fails every claim is NOT_FOUND with reason no_alphabet.
func (e *Engine) Verify(ctx context.Context, img image.Image, refs []Reference, claims []Claim) (Result, error) {
	if len(refs) == 0 {
		return Result{}, errors.New("verify: at least one reference is required")
	}
	pre, err := preprocess.Run(img, preprocess.Default())
	if err != nil {
		return Result{}, err
	}
	lines, regions := region.Propose(pre.Bin, pre.Glare, region.Default())
	res := Result{DeskewDeg: pre.AngleDeg, Regions: len(regions)}

	spans := make([]alphabet.Span, len(refs[0].Emphasis))
	for i, s := range refs[0].Emphasis {
		spans[i] = alphabet.Span{Start: s.Start, End: s.End}
	}
	opt := alphabet.DefaultOptions()
	opt.Encoder = e.glyphEnc
	opt.MaxCharSpread = e.opt.MaxCharSpread
	a, err := alphabet.Find(lines, pre.Bin, pre.Gray, refs[0].Text, spans, opt)
	if err != nil && !errors.Is(err, alphabet.ErrNoBlock) {
		return Result{}, err
	}
	if err != nil || !a.OK(e.opt.MaxUnexplained, e.opt.ViolationFraction) {
		res.Reason = "no_alphabet"
		if a != nil {
			res.Alphabet = report(a, nil) // what was rejected, and why
		}
		for _, c := range claims {
			res.Claims = append(res.Claims, Verdict{Claim: c.Name, Status: NotFound, Reason: "no_alphabet", Expected: c.Expected})
		}
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sp := spell.New(a, e.faces, e.lineEnc)
	res.Reference = e.referenceVerdicts(a)
	for i := range spans {
		res.Emphasis = append(res.Emphasis, e.emphasisVerdict(a, pre, refs[0], i))
	}
	encoded := encodeRegions(pre, regions, e.lineEnc, e.opt.GlareFraction)
	res.Claims = make([]Verdict, len(claims))
	winners := make([]*scored, len(claims))
	for i, c := range claims {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		res.Claims[i], winners[i] = e.decide(c, sp, pre, encoded)
	}
	// A claim that verified decisively is known text too: its glyphs teach
	// the characters the reference lacked, digits and capitals above all.
	// With those learned from the label's own type, the claims that were
	// undecided are read again.
	learned := e.harvest(a, pre, winners)
	if len(learned) > 0 {
		sp = spell.New(a, e.faces, e.lineEnc)
		for i, c := range claims {
			if res.Claims[i].Status == Verified {
				continue
			}
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			res.Claims[i], _ = e.decide(c, sp, pre, encoded)
			if res.Claims[i].Evidence != nil {
				res.Claims[i].Evidence.Relearned = true
			}
		}
	}
	res.Alphabet = report(a, sp)
	res.Alphabet.Learned = string(learned)
	return res, nil
}

// harvest adds, from every decisive reading, the glyphs matched to
// characters the alphabet has no sample of, rescaled to the reference's
// size. Heavy readings feed the emphasis pool. It returns the characters
// learned.
func (e *Engine) harvest(a *alphabet.Alphabet, pre *preprocess.Result, winners []*scored) []rune {
	var learned []rune
	seen := map[rune]bool{}
	for _, w := range winners {
		if w == nil || !w.refined {
			continue
		}
		span := -1
		if w.word_.Heavy && len(a.Spans) > 0 {
			span = 0
		}
		for _, st := range w.path {
			if st.Kind != alphabet.Match {
				continue
			}
			r := w.target.Text[st.Char]
			if r == ' ' || w.target.Codes[st.Char] == nil {
				continue
			}
			// Only a glyph that matched its own code closely teaches: a
			// piece of a bad cut or a blurred glyph would poison the pool.
			if encoder.NormalizedDistance(w.obs.Codes[st.Glyph], w.target.Codes[st.Char], e.glyphEnc.Bits()) > e.opt.MaxCharSpread {
				continue
			}
			if span < 0 && len(a.Samples[r]) > 0 {
				continue
			}
			if span >= 0 && len(a.Emphasis[span][r]) > 0 {
				continue
			}
			g := a.Rescaled(pre.Bin, pre.Gray, w.obs.Boxes[st.Glyph], w.obs.Baselines[st.Glyph], w.obs.XHeight)
			a.AddSample(r, span, g)
			if !seen[r] {
				seen[r] = true
				learned = append(learned, r)
			}
		}
	}
	return learned
}

func report(a *alphabet.Alphabet, sp *spell.Speller) *AlphabetReport {
	chars := make([]rune, 0, len(a.Samples))
	for r := range a.Samples {
		chars = append(chars, r)
	}
	for i := 1; i < len(chars); i++ {
		for j := i; j > 0 && chars[j] < chars[j-1]; j-- {
			chars[j], chars[j-1] = chars[j-1], chars[j]
		}
	}
	r := &AlphabetReport{
		Block: a.Block.Box, Glyphs: len(a.Block.Glyphs), Matched: a.Matched, Unexplained: a.Unexplained,
		Violations: a.ViolationFraction(),
		Recovered:  a.Recovered, Spread: a.Spread, Characters: string(chars),
		XHeight: a.XHeight, CapHeight: a.CapHeight, LetterGap: a.LetterGap, WordGap: a.WordGap, Rows: a.Rows,
	}
	if sp != nil && sp.Face != nil {
		r.Face = sp.Face.Name
	}
	var un []rune
	for c := range a.Unlearned {
		un = append(un, c)
	}
	for i := 1; i < len(un); i++ {
		for j := i; j > 0 && un[j] < un[j-1]; j-- {
			un[j], un[j-1] = un[j-1], un[j]
		}
	}
	r.Unlearned = string(un)
	return r
}
