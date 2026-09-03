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
	Region        image.Rectangle `json:"region"`
	Crop          *image.Gray     `json:"-"`
	Codeword      encoder.Patch   `json:"-"`
	Text          string          `json:"text"` // the candidate text that matched
	Params        spell.Params    `json:"params"`
	D1            int             `json:"d1"`
	D2            int             `json:"d2"`
	Radius        int             `json:"radius"`
	Bits          int             `json:"bits"`
	Refined       bool            `json:"refined"`   // glyph-wise alignment scored the match
	Glyphs        int             `json:"glyphs"`    // candidate glyphs when refined
	Penalized     int             `json:"penalized"` // merge/split/insert/delete steps when refined
	Competitor    string          `json:"competitor,omitempty"`
	LowConfidence bool            `json:"low_confidence"`
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
	Recovered   int                   `json:"recovered"`
	Spread      float64               `json:"spread"`
	Characters  string                `json:"characters"`
	Face        string                `json:"face"`
	XHeight     float64               `json:"x_height"`
	CapHeight   float64               `json:"cap_height"`
	LetterGap   float64               `json:"letter_gap"`
	WordGap     float64               `json:"word_gap"`
	Rows        []alphabet.RowQuality `json:"rows"`
}

// Result is everything Verify found.
type Result struct {
	DeskewDeg float64         `json:"deskew_deg"`
	Regions   int             `json:"regions"`
	Alphabet  *AlphabetReport `json:"alphabet,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Claims    []Verdict       `json:"claims"`
}

// Options tune the engine. Zero values take the defaults.
type Options struct {
	Encoder        string  // "hash" (default) or "hash-nodhash"
	DefaultRadius  float64 // fraction of code length; 0.15
	TieMargin      float64 // glyph-wise: fraction of a glyph code per differing glyph; 0.02
	LineTieMargin  float64 // line-wise fallback: fraction of the line code; 0.04
	MaxSpread      float64 // alphabet acceptance; 0.12
	MaxUnexplained float64 // alphabet acceptance; 0.05
	GlareFraction  float64 // region overlap with the glare mask that flags low confidence; 0.3
}

func (o Options) withDefaults() Options {
	if o.Encoder == "" {
		o.Encoder = "hash"
	}
	if o.DefaultRadius == 0 {
		o.DefaultRadius = 0.15
	}
	if o.TieMargin == 0 {
		o.TieMargin = 0.02
	}
	if o.LineTieMargin == 0 {
		o.LineTieMargin = 0.04
	}
	if o.MaxSpread == 0 {
		o.MaxSpread = 0.12
	}
	if o.MaxUnexplained == 0 {
		o.MaxUnexplained = 0.05
	}
	if o.GlareFraction == 0 {
		o.GlareFraction = 0.3
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
	var line encoder.Encoder
	switch o.Encoder {
	case "hash":
		line = encoder.Line()
	case "hash-nodhash":
		line = encoder.Hash{W: 64, H: 16}
	default:
		return nil, fmt.Errorf("verify: unknown encoder %q", o.Encoder)
	}
	return &Engine{opt: o, faces: faces, lineEnc: line, glyphEnc: encoder.Glyph()}, nil
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
	a, err := alphabet.Find(lines, pre.Bin, pre.Gray, refs[0].Text, spans, opt)
	if err != nil && !errors.Is(err, alphabet.ErrNoBlock) {
		return Result{}, err
	}
	if err != nil || !a.OK(e.opt.MaxSpread, e.opt.MaxUnexplained) {
		res.Reason = "no_alphabet"
		for _, c := range claims {
			res.Claims = append(res.Claims, Verdict{Claim: c.Name, Status: NotFound, Reason: "no_alphabet", Expected: c.Expected})
		}
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sp := spell.New(a, e.faces, e.lineEnc)
	res.Alphabet = report(a, sp)
	encoded := encodeRegions(pre, regions, e.lineEnc, e.opt.GlareFraction)
	for _, c := range claims {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		res.Claims = append(res.Claims, e.decide(c, sp, pre, encoded))
	}
	return res, nil
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
		Recovered: a.Recovered, Spread: a.Spread, Characters: string(chars),
		XHeight: a.XHeight, CapHeight: a.CapHeight, LetterGap: a.LetterGap, WordGap: a.WordGap, Rows: a.Rows,
	}
	if sp.Face != nil {
		r.Face = sp.Face.Name
	}
	return r
}
