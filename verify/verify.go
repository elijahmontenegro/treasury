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
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

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
	// How many detections were joined to make the reading. A verdict
	// resting on a chain of four is different evidence from one resting
	// on a single detection, and the doc has described runs as chains
	// since step 23b without the evidence saying which a verdict used.
	Parts int `json:"parts,omitempty"`
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

// Stages is where a verification spent its time. Step 26a needed it: the
// build had measured whole verifications for twenty-six steps and never
// the parts of one.
type Stages struct {
	Detect      time.Duration `json:"detect"`
	Recognise   time.Duration `json:"recognise"`
	TurnedPass  time.Duration `json:"turned_pass,omitempty"`
	SecondPass  time.Duration `json:"second_pass,omitempty"`
	SecondBoxes int           `json:"second_boxes,omitempty"`
	Runs        time.Duration `json:"runs"`
	Decide      time.Duration `json:"decide"`
	Boxes       int           `json:"boxes"`
	TurnedBoxes int           `json:"turned_boxes,omitempty"`
	RunCount    int           `json:"run_count"`
}

// Result is everything one verification produced.
type Result struct {
	Engine    buildid.Identity `json:"engine"`
	Reason    string           `json:"reason,omitempty"` // why nothing could be decided
	Regions   []Region         `json:"regions,omitempty"`
	Claims    []Verdict        `json:"claims"`
	Stages    Stages           `json:"stages"`
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
	// NumericRadius is the same bound for a claim that is a number in a
	// unit. The two have been measured separately since step 16b, where
	// the free-text distributions and the numeric ones behaved
	// differently, and they are fitted separately here.
	NumericRadius float64
	// TieMargin is how much closer the winner must be than the nearest
	// candidate of a different value.
	TieMargin float64
	// MinConfidence is the recogniser's own confidence below which a
	// region is not evidence for anything.
	MinConfidence float64
	// MaxSide is the longer side the detector sees. The recogniser always
	// crops from the image as given, so this bounds where text is found
	// and not how well it is read.
	MaxSide float64
	// BoxThresh is the probability above which the detector calls a pixel
	// text, and Unclip is how far a proposed region is grown.
	BoxThresh float64
	Unclip    float64
	// Turned reads the page a second time turned a quarter, so that text
	// set vertically reaches the detector the way it was trained to see
	// it. Nonzero is on.
	Turned float64
	// SecondOpinion reads one box again, taller and with a larger
	// recogniser, where a claim's nearest candidate fell just outside the
	// radius. Nonzero is on.
	SecondOpinion float64
	// ConfusionCost is what a substitution between two characters a
	// reader confuses by shape costs, as a share of an ordinary edit
	// (step 28c). 1 charges it in full, which is the behaviour before
	// this step; 0.5 is the value step 27c measured.
	ConfusionCost float64
	// Tune overrides an adopted constant by name, for a sweep.
	Tune map[string]float64
}

// confusionHalfCost is ConfusionCost in halves of an edit, so the
// distance can stay in integer arithmetic on what step 26a measured as
// the engine's slowest stage.
func (o Options) confusionHalfCost() int {
	c := int(math.Round(o.ConfusionCost * 2))
	if c < 0 {
		c = 0
	}
	if c > 2 {
		c = 2
	}
	return c
}

func (o Options) withDefaults() Options {
	if o.Radius == 0 {
		// 25a: the ceiling was 0.077, held there by label 0036 printing
		// BOURBON WHISKEY against a filed BOURBON WHISKY. 27 CFR 5.143
		// makes those one word, so the ceiling is now 0.158, set by the
		// nearest claim the fifty do not carry. Nothing carried sits
		// between 0.136 and 0.160, so 0.14 takes every claim 0.15 would
		// and leaves twice the margin. Both corpus halves admit nothing
		// uncarried at any radius up to 0.25.
		o.Radius = 0.14
	}
	if o.NumericRadius == 0 {
		o.NumericRadius = 0.12
	}
	if o.TieMargin == 0 {
		o.TieMargin = 0.15
	}
	if o.MinConfidence == 0 {
		o.MinConfidence = 0.5
	}
	if o.ConfusionCost == 0 {
		// 27c measured the shape-confusable set at half cost: five
		// carried claims admitted across the fifty and both halves and
		// not one the labels do not carry, with the measurement complete
		// at that cost by arithmetic - the evidence window is twice the
		// radius, and a half-cost re-score is at least half the
		// original, so nothing outside the window could have been
		// admitted. Zero would break that argument and is also the wrong
		// rule, since it says O and 0 are the same character; it is
		// unreachable through the sweep for that reason, zero meaning
		// unset here as it does for every other option. Setting the
		// tunable to 1 charges a confusion in full, which is the
		// behaviour before this step and is how it was measured.
		o.ConfusionCost = 0.5
	}
	if o.BoxThresh == 0 {
		o.BoxThresh = 0.3
	}
	if o.Unclip == 0 {
		o.Unclip = 1.6
	}
	if o.Turned == 0 {
		o.Turned = 1 // 21d: recovers seventeen of the twenty-six 21c measured
	}
	// 25b measured this and it is off: with the shipped recogniser a
	// second reading of the one box gained nothing at all, and with the
	// 90 MB server recogniser it gained two claims and put the p95 on the
	// fifty at 7.8 s against a 5 s requirement. 1 is the shipped model, 2
	// the server one; the numbers for both are in the doc.
	if o.SecondOpinion < 0 {
		o.SecondOpinion = 0
	}
	if o.MaxSide == 0 {
		// 20c: chosen on half A, where 960 verifies 1048 claims, 1280
		// verifies 1079, 1600 verifies 1115 and 2048 verifies 1108 and
		// costs half a second a label more.
		o.MaxSide = 1600
	}
	return o
}

// Engine verifies claims against images.
type Engine struct {
	opt    Options
	reader *ocr.Reader
	// second reads one box again where the first reading fell just
	// outside the radius: taller, and with the larger recogniser if the
	// build has it. It is nil when neither is available.
	second *ocr.Reader
}

// New builds an engine and loads the reader's models.
func New(o Options) (*Engine, error) {
	o = applyOptions(o)
	o = o.withDefaults()
	rp := ocr.Default()
	rp.MaxSide = int(o.MaxSide)
	rp.BoxThresh = o.BoxThresh
	rp.Unclip = o.Unclip
	r, err := ocr.New(rp)
	if err != nil {
		return nil, err
	}
	e := &Engine{opt: o, reader: r}
	if o.SecondOpinion > 0 {
		sp := rp
		sp.RecHeight = 64
		// The 90 MB server recogniser was measured here and is not used:
		// it read one Grapevine label's producer correctly where the
		// shipped model read CRAPEVINE, and took the fifty's median from
		// 2.7 s to 7.8 s and the p95 from 5.0 s to 17.6 s. Setting
		// second_opinion to 2 loads it, for anyone who wants that trade.
		if o.SecondOpinion >= 2 {
			if b, err := os.ReadFile(secondRecogniser); err == nil {
				sp.Rec, sp.RecOutput = b, "softmax_2.tmp_0"
			}
		}
		if s, err := ocr.NewRecogniser(sp); err == nil {
			e.second = s
		}
	}
	return e, nil
}

// secondRecogniser is where the larger recogniser is looked for. It is
// fetched rather than committed, like the runtime library, and its absence
// is not an error: without it the second opinion is the same model reading
// a taller crop, which is most of what it buys.
var secondRecogniser = filepath.Join("third_party", "models", "rec_server.onnx")

// Close frees the reader's sessions.
func (e *Engine) Close() {
	if e.reader != nil {
		e.reader.Close()
	}
	if e.second != nil {
		e.second.Close()
	}
}

// Verify reads the image and judges what it read against the claims.
func (e *Engine) Verify(ctx context.Context, img image.Image, refs []Reference, claims []Claim) (Result, error) {
	read, t, err := e.reader.ReadTimed(img)
	if err != nil {
		return Result{}, err
	}
	var res Result
	res.Stages.Detect, res.Stages.Recognise, res.Stages.Boxes = t.Detect, t.Recognise, t.Boxes
	add := func(rs []ocr.Region) {
		for _, r := range rs {
			res.Regions = append(res.Regions, Region{Box: r.Box, Text: r.Text, Confidence: r.Confidence, Rotated: r.Rotated})
		}
	}
	add(read)
	decide := func() []Verdict {
		start := time.Now()
		rs := buildRuns(res.Regions, e.opt.MinConfidence)
		res.Stages.Runs += time.Since(start)
		res.Stages.RunCount = len(rs)
		start = time.Now()
		out := make([]Verdict, 0, len(claims))
		for _, c := range claims {
			out = append(out, e.decide(c, rs))
		}
		res.Stages.Decide += time.Since(start)
		return out
	}
	res.Claims = decide()
	// The page is offered to the detector turned a quarter only where the
	// upright pass left a required claim unread (step 24a). Reading every
	// label twice cost a second a label for the labels that needed it and
	// for the labels that did not.
	if e.opt.Turned > 0 && worthTurning(read, res.Claims, e.opt.Turned) {
		start := time.Now()
		more, err := e.reader.ReadTurned(img, read)
		res.Stages.TurnedPass = time.Since(start)
		res.Stages.TurnedBoxes = len(more)
		if err != nil {
			return Result{}, err
		}
		if len(more) > 0 {
			add(more)
			sortRegions(res.Regions)
			res.Claims = decide()
		}
	}
	// A claim whose nearest reading fell just outside the radius gets one
	// box read again, taller and with the larger recogniser (step 25b).
	// Only the reading changes; every rule applies to the second as to
	// the first, and a claim is verified only if the new reading brings
	// it inside the radius on its own merits.
	if e.second != nil {
		if boxes := nearMisses(res.Claims); len(boxes) > 0 {
			start := time.Now()
			again, err := e.second.ReadBoxes(img, boxes)
			res.Stages.SecondPass = time.Since(start)
			res.Stages.SecondBoxes = len(boxes)
			if err != nil {
				return Result{}, err
			}
			if len(again) > 0 {
				add(again)
				sortRegions(res.Regions)
				res.Claims = decide()
			}
		}
	}
	if len(res.Regions) == 0 {
		res.Reason = "no_text"
	}
	stamp(&res)
	return res, nil
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

// worthTurning decides whether to spend the second detection pass.
//
// Step 24a asked for it where the upright pass leaves a required claim
// unread, and that is what a setting of 2 does. It is not the default,
// because it makes the reading depend on which claims were asked and so
// lets one claim's verdict turn on another claim being in the call - the
// coupling step 13a removed and TestClaimSetIndependence exists to catch.
// Run as asked, that test fails, naming label 1's net contents changing
// when the brand is dropped from the call.
//
// The default asks the reading instead of the claims: a page whose
// upright pass returned a detection taller than it is wide has text the
// detector saw side-on, and is worth offering turned. That is the same
// saving without the coupling, and step 24a reports both.
func worthTurning(read []ocr.Region, vs []Verdict, mode float64) bool {
	if mode >= 2 {
		for _, v := range vs {
			if v.Status == NotFound || v.Status == Review {
				return true
			}
		}
		return false
	}
	for _, r := range read {
		if r.Box.Dy() > r.Box.Dx() {
			return true
		}
	}
	return false
}

// sortRegions puts the regions back in reading order after a second pass
// has added to them, so that everything downstream sees one order.
func sortRegions(rs []Region) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Box.Min.Y != rs[j].Box.Min.Y {
			return rs[i].Box.Min.Y < rs[j].Box.Min.Y
		}
		return rs[i].Box.Min.X < rs[j].Box.Min.X
	})
}

// nearMisses gathers the one box behind each claim whose nearest reading
// sat between the radius and twice it, without repeating a box.
func nearMisses(vs []Verdict) []image.Rectangle {
	var out []image.Rectangle
	seen := map[image.Rectangle]bool{}
	for _, v := range vs {
		if v.Status == Verified || v.Status == Mismatch || v.Evidence == nil {
			continue
		}
		e := v.Evidence
		if e.Distance <= e.Radius || e.Distance > 2*e.Radius || seen[e.Region] {
			continue
		}
		seen[e.Region] = true
		out = append(out, e.Region)
	}
	return out
}
