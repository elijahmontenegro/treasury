// Package verify is the engine's public surface. Give it an image, the text
// known to appear in it, and the claims to check; it returns a verdict per
// claim with evidence.
package verify

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"treasury/internal/imgops"

	"unicode"

	"treasury/internal/alphabet"
	"treasury/internal/bitmap"
	"treasury/internal/buildid"
	"treasury/internal/digits"
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
	Radius     float64  // Hamming radius as a fraction of the code length; 0 uses the default
	Variants   bool     // also spell casing, quote, and weight variants of each candidate; for free text
	Numeric    *Numeric // when set, the claim is read as a number and Candidates is ignored

	numericInner bool // an instantiated numeric claim, decided by the free-text path
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

	// Numeric claims.
	Reading         string  `json:"reading,omitempty"`          // the digit run as the classifier read it
	DigitConfidence float64 `json:"digit_confidence,omitempty"` // the least certain digit's probability

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
	Claim string `json:"claim"`
	// Engine is the fingerprint of the build and the weights that produced
	// this verdict, so that a verdict separated from its result is still
	// traceable to them.
	Engine     string    `json:"engine,omitempty"`
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
	// Engine is what produced this result: the version, the commit, and
	// the hash of every model file the binary carries.
	Engine            buildid.Identity `json:"engine"`
	Orientation       string           `json:"orientation,omitempty"`        // how the image was taken: as_is, inverted, rot90, rot270, or both
	ReferenceCasing   string           `json:"reference_casing,omitempty"`   // as_given or upper: how the reference was printed
	ClaimsOrientation string           `json:"claims_orientation,omitempty"` // the image the claims were searched in
	DeskewDeg         float64          `json:"deskew_deg"`
	Regions           int              `json:"regions"`
	Alphabet          *AlphabetReport  `json:"alphabet,omitempty"`
	Reason            string           `json:"reason,omitempty"`
	Reference         []Verdict        `json:"reference,omitempty"` // one per row of the reference block
	Emphasis          []Verdict        `json:"emphasis,omitempty"`  // one per emphasis span
	Claims            []Verdict        `json:"claims"`
}

// Options tune the engine. Zero values take the defaults.
// DefaultNumericRadius is the radius a numeric claim is decided at under
// the learned code. Chosen by running half A at 0.10, 0.15 and 0.22: 262,
// 299 and 300 correct verifications against 1, 1 and 7 false assertions.
const DefaultNumericRadius = 0.15

// How a numeric field's value is obtained.
const (
	// DigitsClassifier reads the digits with the embedded classifier and
	// instantiates the printed formats with the reading.
	DigitsClassifier = ""
	// DigitsImage uses no classifier: the field's enumeration is decided
	// as an ordinary claim, and the winner's digit glyphs teach the
	// alphabet the label's own digits for the claims read afterwards.
	DigitsImage = "image"
	// DigitsImageEnum decodes every numeric field by its enumeration, not
	// only the fields whose vocabulary is small: the alcohol content's
	// 1,520 spellings as well as the fill's. Kept so the cost of deleting
	// the alcohol enumeration can be measured against it.
	DigitsImageEnum = "image-enum"
	// DigitsSynthetic is DigitsImage with the harvest forbidden to teach
	// digits, so every digit compared is one synthesized from a bundled
	// face: the engine as it stood before the classifier.
	DigitsSynthetic = "synthetic"
)

type Options struct {
	// Digits is how numeric fields are read: DigitsClassifier (default),
	// DigitsImage, or DigitsSynthetic.
	Digits string
	// Separate turns text separation on or off; nil means on. Off is the
	// pipeline as it stood through step 9, where ink is whatever is dark.
	Separate *bool
	// Without names rules to remove, so that the cost of deleting one can
	// be measured rather than argued: "invalid-tie" sends a tie between
	// values a field cannot hold to review instead of returning nothing,
	// "fill-enumeration" stops the fill being decoded by its vocabulary.
	Without []string

	Encoder              string  // glyph encoder: "dual" (default; "hash" is accepted as its alias), "pos16" (positional view only), "sharp24" (24×24 binary, the naive grid)
	ClaimEncoder         string  // the code claims are decoded in: "" or "same" for the glyph encoder, "learned" for the embedded contrastive encoder
	LearnedRadius        float64 // free-text radius when claims are decoded with the learned encoder; 0 keeps DefaultRadius
	LearnedNumericRadius float64 // numeric radius under the learned encoder; 0 keeps the claim's own
	LearnedTie           float64 // tie margin per differing glyph under the learned encoder; tuned 0.02 on half A
	DefaultRadius        float64 // fraction of code length; 0.15
	TieMargin            float64 // glyph-wise: fraction of a glyph code per differing glyph; 0.01 (tuned on half A of the synthetic set; the spread term usually dominates)
	LineTieMargin        float64 // line-wise fallback: fraction of the line code; 0.04
	MaxCharSpread        float64 // per-character acceptance: a character whose samples spread beyond this is unlearned; 0.12
	MaxUnexplained       float64 // alphabet acceptance; 0.10
	LineThreshold        float64 // reference row acceptance, mean normalized distance; 0.08
	ViolationFraction    float64 // alphabet and reference row acceptance, share of glyphs contradicting their shape class; 0.1
	RowFailAnomalies     float64 // anomaly weight (unexplained and strong outliers one, weak outliers half) at which a reference row fails rather than reviews; 2
	HeavyFactor          float64 // stroke ratio of the heavy hypothesis to the body; 1.25
	EmphasisGate         float64 // an emphasis span must be within this fraction of a hypothesis; 0.15
	GlareFraction        float64 // region overlap with the glare mask that flags low confidence; 0.3
	MinGlyphs            int     // a reading of fewer glyphs cannot decide a claim, only ask for review; 3
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
	if o.LearnedTie == 0 {
		o.LearnedTie = 0.01 // tuned on half A (7a): with partial reads undecided at the source, the tuner no longer trades decisions for reviews
	}
	if o.LearnedNumericRadius == 0 {
		o.LearnedNumericRadius = DefaultNumericRadius // chosen by running half A at 0.10, 0.15, 0.22 (8a): 262, 299, 300 correct numeric verifications against 1, 1, 7 false assertions
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
	claimEnc encoder.Encoder // the code claims are decoded in; nil means glyphEnc
	digits   *digits.Model   // the embedded digit classifier
}

// New builds an engine.
func New(o Options) (*Engine, error) {
	o = o.withDefaults()
	faces, err := render.Bundled()
	if err != nil {
		return nil, err
	}
	model, err := digits.Load()
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
	var claim encoder.Encoder
	switch o.ClaimEncoder {
	case "", "same":
	case "learned":
		if claim, err = encoder.NewLearned(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("verify: unknown claim encoder %q", o.ClaimEncoder)
	}
	return &Engine{opt: o, faces: faces, lineEnc: encoder.Line(), glyphEnc: glyph, claimEnc: claim, digits: model}, nil
}

// Verify decodes the image. It learns the alphabet from the first reference;
// if that fails every claim is NOT_FOUND with reason no_alphabet.
func (e *Engine) Verify(ctx context.Context, img image.Image, refs []Reference, claims []Claim) (Result, error) {
	if len(refs) == 0 {
		return Result{}, errors.New("verify: at least one reference is required")
	}
	spans := make([]alphabet.Span, len(refs[0].Emphasis))
	for i, s := range refs[0].Emphasis {
		spans[i] = alphabet.Span{Start: s.Start, End: s.End}
	}
	opt := alphabet.DefaultOptions()
	opt.Encoder = e.glyphEnc
	opt.MaxCharSpread = e.opt.MaxCharSpread

	// The image is taken as it comes first; when no alphabet can be
	// learned from it, it is tried inverted (light type on a dark ground)
	// and in the other three orientations, since labels print the
	// reference vertically and in reverse. The reference is tried as
	// given and in capitals, since labels set it in either.
	gray := imgops.ToGray(img)
	type attempt struct {
		name     string
		quarters int
		invert   bool
	}
	// Detection before trial. The median gray says whether the label is
	// light on dark; the anisotropy of the binarized image's projection
	// profiles says whether its text runs horizontal or vertical (rows of
	// text make the row sums vary far more than the column sums). The
	// detected orientation and polarity are tried first; the rest of the
	// ladder stays as the fallback, since a warning alone may run
	// vertically on a horizontal label, and a light label with a boxed
	// warning has been found only inverted.
	invertFirst := medianGray(gray) < 128
	type prepared struct {
		pre     *preprocess.Result
		lines   []region.Line
		regions []region.Region
	}
	done := map[attempt]prepared{}
	prepare := func(at attempt) (prepared, error) {
		if p, ok := done[at]; ok {
			return p, nil
		}
		g := imgops.Rotate90(gray, at.quarters)
		if at.invert {
			g = imgops.Invert(g)
		}
		pp := preprocess.Default()
		if e.opt.Separate != nil {
			pp.Separate = *e.opt.Separate
		}
		pre, err := preprocess.Run(g, pp)
		if err != nil {
			return prepared{}, err
		}
		lines, regions := region.Propose(pre.Bin, pre.Glare, region.Default())
		p := prepared{pre, lines, regions}
		done[at] = p
		return p, nil
	}
	first, err := prepare(attempt{"as_is", 0, invertFirst})
	if err != nil {
		return Result{}, err
	}
	verticalText := anisotropy(first.pre.Bin) < 1
	var order []int
	if verticalText {
		order = []int{1, 3, 0, 2}
	} else {
		order = []int{0, 1, 3, 2}
	}
	var attempts []attempt
	for _, inv := range []bool{invertFirst, !invertFirst} {
		for _, q := range order {
			name := map[int]string{0: "as_is", 1: "rot90", 2: "rot180", 3: "rot270"}[q]
			if inv {
				name = "inverted_" + name
				if q == 0 {
					name = "inverted"
				}
			}
			attempts = append(attempts, attempt{name, q, inv})
		}
	}
	var pre *preprocess.Result
	var lines []region.Line
	var a *alphabet.Alphabet
	var found attempt
	casing := ""
	for _, at := range attempts {
		p, err := prepare(at)
		if err != nil {
			return Result{}, err
		}
		pre, lines = p.pre, p.lines
		var candidates []*alphabet.Alphabet
		for _, c := range []struct{ name, text string }{{"as_given", refs[0].Text}, {"upper", strings.ToUpper(refs[0].Text)}} {
			b, ferr := alphabet.Find(lines, pre.Bin, pre.Gray, c.text, spans, opt)
			if ferr != nil && !errors.Is(ferr, alphabet.ErrNoBlock) {
				return Result{}, ferr
			}
			if b == nil {
				continue
			}
			candidates = append(candidates, b)
			// Between the two casings, the one whose glyphs contradict
			// their characters least is the one the label printed; matched
			// counts alone favoured capitals on mixed-case warnings.
			// When both casings align, the block's own glyph heights say
			// which the label printed: capitals stand uniformly tall.
			better := func(x, y *alphabet.Alphabet, xName string) bool {
				if y == nil || !y.OK(e.opt.MaxUnexplained, e.opt.ViolationFraction) {
					return true
				}
				uniform := x.HeightUniformity() >= 0.7
				return (xName == "upper") == uniform
			}
			if b.OK(e.opt.MaxUnexplained, e.opt.ViolationFraction) && better(b, a, c.name) {
				a, casing = b, c.name
			}
		}
		if a != nil && a.OK(e.opt.MaxUnexplained, e.opt.ViolationFraction) {
			found = at
			break
		}
		// Keep the best rejected alignment of the first attempt for the report.
		if a == nil && len(candidates) > 0 {
			a = candidates[0]
		}
		if at == attempts[0] {
			found = at
		}
	}
	if a == nil {
		err = alphabet.ErrNoBlock
	} else {
		err = nil
	}
	// Claims are searched in the image whose text is upright by
	// detection, at the alphabet's polarity: a label that sets its warning
	// vertically sets its claims upright, and the rotated image's regions
	// are that upright text on its side, a second region set that once
	// doubled the decoding of such a label.
	// Which image that is: the anisotropy is decided by the largest text
	// mass, and a vertical warning is often that mass on a label whose
	// other text is upright. So the alphabet's image and the detected one
	// are compared by the glyphs in runs of four or more outside the
	// warning block, and the claims are searched where there are more.
	claimsAt := found
	if a != nil && err == nil && found.quarters != 0 {
		asIs := attempt{"as_is", 0, found.invert}
		if found.invert {
			asIs.name = "inverted"
		}
		alt, err2 := prepare(asIs)
		if err2 != nil {
			return Result{}, err2
		}
		block := a.Block.Box
		own := done[found]
		if textMass(alt.regions, rotateBack(block, found.quarters, own.pre.Gray.Rect.Dx(), own.pre.Gray.Rect.Dy())) > textMass(own.regions, block) {
			claimsAt = asIs
		}
	}
	claims_, err2 := prepare(claimsAt)
	if err2 != nil {
		return Result{}, err2
	}
	claimsPre, claimsRegions := claims_.pre, claims_.regions
	// The reference block's glyphs are never claims: regions inside it
	// are left out, in the alphabet's image or mapped into the other.
	// In the alphabet's own image they stay: a block aligned across a
	// two-column label holds the claims' words too, and dropping them
	// dropped THE AUSTIN WINERY's verified fields.
	if a != nil && err == nil && claimsAt != found {
		kept := claimsRegions[:0:0]
		{
			own := done[found]
			block := rotateBack(a.Block.Box, found.quarters, own.pre.Gray.Rect.Dx(), own.pre.Gray.Rect.Dy())
			for _, r := range claimsRegions {
				if inter := r.Box.Intersect(block); inter.Dx()*inter.Dy()*10 >= r.Box.Dx()*r.Box.Dy()*8 {
					continue
				}
				kept = append(kept, r)
			}
		}
		claimsRegions = kept
	}
	res := Result{Engine: buildid.Get(), DeskewDeg: pre.AngleDeg, Regions: len(claimsRegions), Orientation: found.name, ClaimsOrientation: claimsAt.name, ReferenceCasing: casing}
	if err != nil || !a.OK(e.opt.MaxUnexplained, e.opt.ViolationFraction) {
		res.Reason = "no_alphabet"
		if a != nil {
			res.Alphabet = report(a, nil) // what was rejected, and why
		}
		for _, c := range claims {
			res.Claims = append(res.Claims, Verdict{Claim: c.Name, Status: NotFound, Reason: "no_alphabet", Expected: c.Expected})
		}
		stamp(&res)
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	spellCache := spell.NewCache()
	sp := spell.NewCached(a, e.faces, e.lineEnc, e.claimEnc, spellCache)
	res.Reference = e.referenceVerdicts(a)
	for i := range spans {
		res.Emphasis = append(res.Emphasis, e.emphasisVerdict(a, pre, refs[0], i))
	}
	encoded := encodeRegions(claimsPre, claimsRegions, e.lineEnc, e.opt.GlareFraction)
	res.Claims = make([]Verdict, len(claims))
	cache := newCallCache()
	winners := make([]*scored, len(claims))
	for i, c := range claims {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		res.Claims[i], winners[i] = e.decideClaim(c, sp, pre, encoded, cache)
	}
	// A claim that verified decisively is known text too: its glyphs teach
	// the characters the reference lacked, digits and capitals above all.
	// With those learned from the label's own type, the claims that were
	// undecided are read again.
	learned := e.harvest(a, encoded, winners)
	if len(learned) > 0 {
		sp = spell.NewCached(a, e.faces, e.lineEnc, e.claimEnc, spellCache.NextPass())
		for i, c := range claims {
			if res.Claims[i].Status == Verified {
				continue
			}
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			res.Claims[i], _ = e.decideClaim(c, sp, pre, encoded, cache)
			if res.Claims[i].Evidence != nil {
				res.Claims[i].Evidence.Relearned = true
			}
		}
	}
	res.Alphabet = report(a, sp)
	res.Alphabet.Learned = string(learned)
	stamp(&res)
	return res, nil
}

// harvest adds, from every decisive reading, the glyphs matched to
// characters the alphabet has no sample of, rescaled to the reference's
// size. Heavy readings feed the emphasis pool. It returns the characters
// learned.
func (e *Engine) harvest(a *alphabet.Alphabet, regions []encodedRegion, winners []*scored) []rune {
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
			if e.opt.Digits == DigitsSynthetic && unicode.IsDigit(r) {
				continue
			}
			// Only a glyph that matched its own code closely teaches: a
			// piece of a bad cut or a blurred glyph would poison the pool.
			if encoder.NormalizedDistance(w.obs.Codes[st.Glyph], w.target.Codes[st.Char], w.obs.Enc.Bits()) > e.opt.MaxCharSpread {
				continue
			}
			if span < 0 && len(a.Samples[r]) > 0 {
				continue
			}
			if span >= 0 && len(a.Emphasis[span][r]) > 0 {
				continue
			}
			pre := regions[w.region].pre
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

// stamp writes the build fingerprint onto every verdict a result carries.
func stamp(res *Result) {
	f := buildid.Fingerprint()
	res.Engine = buildid.Get()
	for i := range res.Claims {
		res.Claims[i].Engine = f
	}
	for i := range res.Reference {
		res.Reference[i].Engine = f
	}
	for i := range res.Emphasis {
		res.Emphasis[i].Engine = f
	}
}

// without reports whether a named rule has been removed for measurement.
func (e *Engine) without(rule string) bool {
	for _, r := range e.opt.Without {
		if r == rule {
			return true
		}
	}
	return false
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

// medianGray is the median pixel of g, sampled on a coarse grid.
func medianGray(g *image.Gray) int {
	var hist [256]int
	n := 0
	for y := g.Rect.Min.Y; y < g.Rect.Max.Y; y += 4 {
		for x := g.Rect.Min.X; x < g.Rect.Max.X; x += 4 {
			hist[g.GrayAt(x, y).Y]++
			n++
		}
	}
	acc := 0
	for v, c := range hist {
		acc += c
		if 2*acc >= n {
			return v
		}
	}
	return 255
}

// anisotropy is the variation of the binarized image's row ink counts
// against its column ink counts, each as a squared coefficient of
// variation. Rows of horizontal text alternate between ink and leading,
// so their sums vary far more than the columns'; text set vertically
// reverses it. Above 1 the text runs horizontal.
func anisotropy(b *bitmap.Bitmap) float64 {
	rows := make([]float64, b.H)
	cols := make([]float64, b.W)
	for y := range b.H {
		for x := range b.W {
			if b.Pix[y*b.W+x] != 0 {
				rows[y]++
				cols[x]++
			}
		}
	}
	cv2 := func(v []float64) float64 {
		n := float64(len(v))
		if n == 0 {
			return 0
		}
		mean := 0.0
		for _, x := range v {
			mean += x
		}
		mean /= n
		if mean == 0 {
			return 0
		}
		s := 0.0
		for _, x := range v {
			s += (x - mean) * (x - mean)
		}
		return s / n / (mean * mean)
	}
	c := cv2(cols)
	if c == 0 {
		return 2
	}
	return cv2(rows) / c
}

// textMass counts the glyphs of line regions holding four or more, outside
// the excluded box: the upright running text of an image.
func textMass(regions []region.Region, exclude image.Rectangle) int {
	n := 0
	for _, r := range regions {
		if r.Kind != region.KindLine || len(r.Comps) < 4 || r.Box.Overlaps(exclude) {
			continue
		}
		n += len(r.Comps)
	}
	return n
}

// rotateBack maps a box in an image rotated by quarters clockwise (of
// size w×h after rotation) into the unrotated image, padded a little for
// the deskew that differs between the two.
func rotateBack(r image.Rectangle, quarters, w, h int) image.Rectangle {
	var out image.Rectangle
	switch ((quarters % 4) + 4) % 4 {
	case 1:
		out = image.Rect(r.Min.Y, h-r.Max.X, r.Max.Y, h-r.Min.X)
	case 2:
		out = image.Rect(w-r.Max.X, h-r.Max.Y, w-r.Min.X, h-r.Min.Y)
	case 3:
		out = image.Rect(w-r.Max.Y, r.Min.X, w-r.Min.Y, r.Max.X)
	default:
		out = r
	}
	return out.Inset(-8)
}
