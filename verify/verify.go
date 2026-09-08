// Package verify decides whether an image carries the text it was said to
// carry.
//
// The engine reads the image and then judges what it read. Reading is a
// scene-text problem: a detector proposes regions and a recogniser reads
// each one whole (step 19b). Judging is the part of this build that has
// always worked and is unchanged in principle: a claim is compared to what
// was read, a distance decides, a margin separates candidates, and the
// engine refuses rather than asserts.
//
// What was here before, from step 1 to step 18, read the image by cutting
// it into glyphs and learning the label's own alphabet from a known block
// of text. Step 18a measured the stage that cut the glyphs: it was right on
// 0.39 of characters, and on 0.25 once the image had been through a camera
// channel, with fusion the dominant error. Every mechanism above it had
// been fitted to pieces that are wrong most of the time. It is retired in
// step 19a, and docs/approach.md keeps its tables in the order they were
// measured.
package verify

import (
	"context"
	"image"

	"treasury/internal/buildid"
	"treasury/internal/ocr"
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

// Candidate is one accepted spelling of a claim and the value it stands for.
// A claim may have several: the application names a permittee twice, and
// alcohol content is printed in any of the forms the regulation allows.
type Candidate struct {
	Text  string
	Value string
}

// Claim is a value the image is said to carry.
type Claim struct {
	Name       string
	Expected   string
	Candidates []Candidate
	Required   bool
	Radius     float64
	Variants   bool
	Numeric    *Numeric
}

// Numeric describes a claim that is a number in a unit rather than a
// string: the forms it may be printed in, the values it may take, and how
// close a reading has to be.
type Numeric struct {
	Formats   []NumericFormat
	Valid     []float64
	Tolerance float64
	// Enumerable says the field's whole vocabulary is known, as the net
	// contents statement's is: every value it may take can be spelled out
	// and looked for, where alcohol content is read as a number.
	Enumerable bool
}

// NumericFormat is a printed form of a number, with the scale that takes the
// printed figure to the claim's own unit: proof is half a percent, a litre
// is a thousand millilitres.
type NumericFormat struct {
	Template string // with {n} where the number goes
	Scale    float64
}

// Status is a verdict's outcome.
type Status int

const (
	Verified Status = iota
	Review
	Mismatch
	NotFound
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
	case NotFound:
		return "NOT_FOUND"
	case Skipped:
		return "SKIPPED"
	}
	return "UNKNOWN"
}

func (s Status) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Region is one piece of text the reader found: where it is, what it says,
// and how sure the recogniser was.
type Region struct {
	Box        image.Rectangle `json:"box"`
	Text       string          `json:"text"`
	Confidence float64         `json:"confidence"`
	Rotated    bool            `json:"rotated,omitempty"` // read after turning the crop upright
}

// Evidence is what a verdict rests on.
type Evidence struct {
	Region     image.Rectangle `json:"region"`
	Read       string          `json:"read"`       // the text the recogniser read there
	Matched    string          `json:"matched"`    // the accepted spelling it was compared to
	Distance   float64         `json:"distance"`   // normalized distance between them
	Radius     float64         `json:"radius"`     // what it had to be inside
	Confidence float64         `json:"confidence"` // the recogniser's own
	Competitor string          `json:"competitor,omitempty"`
	CompDist   float64         `json:"competitor_distance,omitempty"`
	Reading    string          `json:"reading,omitempty"` // the number parsed, for a numeric claim
}

// Verdict is the answer for one claim.
type Verdict struct {
	Claim      string    `json:"claim"`
	Engine     string    `json:"engine,omitempty"` // the build that decided it
	Status     Status    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	Expected   string    `json:"expected"`
	Observed   string    `json:"observed,omitempty"`
	Candidates []string  `json:"candidates,omitempty"`
	Evidence   *Evidence `json:"evidence,omitempty"`
}

// Result is everything one verification produced.
type Result struct {
	Engine    buildid.Identity `json:"engine"`
	Reason    string           `json:"reason,omitempty"` // why nothing could be decided
	Regions   []Region         `json:"regions,omitempty"`
	Claims    []Verdict        `json:"claims"`
	Reference []Verdict        `json:"reference,omitempty"`
	Emphasis  []Verdict        `json:"emphasis,omitempty"`
}

// Options are the engine's settings. What remains of them after step 19a is
// the decision layer's: the reading stage carries its thresholds inside the
// models it runs.
type Options struct {
	// Radius is how far a claim's text may be from what was read and still
	// be that claim, as a share of the claim's length.
	Radius float64
	// TieMargin is how much closer the winner must be than the nearest
	// candidate of a different value.
	TieMargin float64
	// MinConfidence is the recogniser's own confidence below which a
	// region is not evidence for anything.
	MinConfidence float64
	// Tune overrides an adopted constant by name, for a sweep.
	Tune map[string]float64
}

func (o Options) withDefaults() Options {
	if o.Radius == 0 {
		o.Radius = 0.15
	}
	if o.TieMargin == 0 {
		o.TieMargin = 0.05
	}
	if o.MinConfidence == 0 {
		o.MinConfidence = 0.5
	}
	return o
}

// Engine verifies claims against images.
type Engine struct {
	opt    Options
	reader *ocr.Reader
}

// New builds an engine and loads the reader's models.
func New(o Options) (*Engine, error) {
	o = applyOptions(o)
	o = o.withDefaults()
	r, err := ocr.New(ocr.Default())
	if err != nil {
		return nil, err
	}
	return &Engine{opt: o, reader: r}, nil
}

// Close frees the reader's sessions.
func (e *Engine) Close() {
	if e.reader != nil {
		e.reader.Close()
	}
}

// Verify reads the image and judges what it read against the claims.
func (e *Engine) Verify(ctx context.Context, img image.Image, refs []Reference, claims []Claim) (Result, error) {
	read, err := e.reader.Read(img)
	if err != nil {
		return Result{}, err
	}
	var res Result
	for _, r := range read {
		res.Regions = append(res.Regions, Region{Box: r.Box, Text: r.Text, Confidence: r.Confidence, Rotated: r.Rotated})
	}
	if len(res.Regions) == 0 {
		res.Reason = "no_text"
	}
	for _, c := range claims {
		res.Claims = append(res.Claims, e.decide(c, res.Regions))
	}
	stamp(&res)
	return res, nil
}

// decide judges one claim against what was read. Step 19c gives it the
// distance, the margin and the numeric reading; until then it reports the
// claim as not found so that the engine never asserts what it has not
// judged.
func (e *Engine) decide(c Claim, regions []Region) Verdict {
	status := NotFound
	if !c.Required {
		status = Skipped
	}
	return Verdict{Claim: c.Name, Status: status, Reason: "no_decision", Expected: c.Expected}
}

// stamp writes the build identity onto the result and its fingerprint onto
// every verdict, so that a verdict separated from its result is still
// traceable to the weights and the commit that produced it.
func stamp(res *Result) {
	res.Engine = buildid.Get()
	f := buildid.Fingerprint()
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
