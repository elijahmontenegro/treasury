// Package spell composes codewords for expected text out of the learned
// alphabet, synthesizing from the nearest bundled face what the reference
// did not teach.
package spell

import (
	"fmt"
	"image"
	"math"
	"sort"
	"unicode"

	"treasury/internal/alphabet"
	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/encoder"
	"treasury/internal/render"
)

// Params records how a codeword was spelled, for evidence.
type Params struct {
	Casing      string `json:"casing,omitempty"`      // as_given, upper, title
	Weight      string `json:"weight,omitempty"`      // regular, heavy
	Synthesized string `json:"synthesized,omitempty"` // characters synthesized from Face
	Emphasis    string `json:"emphasis,omitempty"`    // characters taken from an emphasis pool
	Face        string `json:"face,omitempty"`
}

// Codeword is spelled text with its code.
type Codeword struct {
	Text   string
	Value  string
	Heavy  bool
	Code   bitcode.Code
	Params Params
	Patch  encoder.Patch
}

// piece is one glyph ready to place: Box relative to the pen origin on the
// baseline, and its frame code for glyph-wise decoding.
type piece struct {
	bin     *bitmap.Bitmap
	gray    *image.Gray
	box     image.Rectangle
	code    bitcode.Code
	source  byte // 'l' learned, 'e' emphasis, 's' synthesized
	derived bool // learned from a claim or cut from a merged pair rather than a reference component
}

// Speller spells with one alphabet.
type Speller struct {
	A         *alphabet.Alphabet
	Face      *render.Face // bundled face nearest the body samples
	HeavyFace *render.Face // bundled face nearest the emphasis samples, or the body face's bold sibling
	Enc       encoder.Encoder
	GlyphEnc  encoder.Encoder
	Spread    float64 // the alphabet's within-character spread in GlyphEnc's code
	cache     *Cache

	letterGap, wordGap int
	bodyStroke         float64 // learned body stroke width in px; synthesized glyphs are brought to it
	heavyStroke        float64 // learned emphasis stroke width, or 1.25 times the body's
	medoids            map[alphabet.Key]alphabet.Glyph
	synth              map[rune]piece
	heavySynth         map[rune]piece
	altFaces           []*render.Face // the next-nearest faces; their glyphs stand as alternatives for synthesized characters
	altCodes           map[rune][]bitcode.Code
}

// New picks the nearest faces and prepares the medoid sample per character.
func New(a *alphabet.Alphabet, faces []*render.Face, enc encoder.Encoder) *Speller {
	return NewWith(a, faces, enc, nil)
}

// NewWith is New with the glyph encoder claims are decoded with. When it
// differs from the alphabet's own, every sample is recoded under it from
// its crop, so medoids, targets, and observed frames all live in the same
// code, and the spread is measured in that code too. The alphabet itself,
// and the reference verdicts drawn from it, keep the encoder it was
// learned with.
func NewWith(a *alphabet.Alphabet, faces []*render.Face, enc, glyphEnc encoder.Encoder) *Speller {
	return NewCached(a, faces, enc, glyphEnc, nil)
}

// Cache holds codes that do not change between the spellers of one
// verification: samples recoded under the claim encoder, and glyphs
// synthesized in a face. A learned encoder costs milliseconds a frame,
// and the second pass would otherwise pay for every frame again.
type Cache struct {
	samples map[sampleKey]bitcode.Code
	synth   map[synthKey]bitcode.Code
	runs    map[string]bitcode.Code
}

type sampleKey struct {
	box      image.Rectangle
	baseline int
}

type synthKey struct {
	face   string
	r      rune
	xh     int // target x-height, quarter pixels
	stroke int // normalized stroke, tenths
}

// NewCache returns an empty cache for one verification.
func NewCache() *Cache {
	return &Cache{samples: map[sampleKey]bitcode.Code{}, synth: map[synthKey]bitcode.Code{}, runs: map[string]bitcode.Code{}}
}

// NextPass keeps the sample and synthesized codes for another speller and
// drops the composed runs, whose pieces may change when the alphabet has
// learned from the first pass.
func (c *Cache) NextPass() *Cache {
	return &Cache{samples: c.samples, synth: c.synth, runs: map[string]bitcode.Code{}}
}

// NewCached is NewWith with a cache shared across the verification's spellers.
func NewCached(a *alphabet.Alphabet, faces []*render.Face, enc, glyphEnc encoder.Encoder, cache *Cache) *Speller {
	if cache == nil {
		cache = NewCache()
	}
	own := a.Block.Encoder()
	if glyphEnc == nil {
		glyphEnc = own
	}
	if glyphEnc != own {
		a = recoded(a, glyphEnc, cache)
	}
	s := &Speller{A: a, Enc: enc, GlyphEnc: glyphEnc, Spread: a.Spread, cache: cache, medoids: map[alphabet.Key]alphabet.Glyph{}, synth: map[rune]piece{}, heavySynth: map[rune]piece{}, altCodes: map[rune][]bitcode.Code{}}
	s.bodyStroke = a.BodyStroke()
	s.heavyStroke = 1.25 * s.bodyStroke
	if len(a.Emphasis) > 0 {
		if w := a.SpanStroke(0); w > 0 {
			s.heavyStroke = w
		}
	}
	// Faces are scored in the alphabet's own code, not the claim code: a
	// code trained to ignore the face cannot tell which face is nearest.
	scores := FaceScores(a, faces, own, -1)
	sort.Slice(scores, func(i, j int) bool { return scores[i].Score < scores[j].Score })
	if len(scores) > 0 {
		s.Face = scores[0].Face
		for _, fs := range scores[1:min(len(scores), 4)] {
			s.altFaces = append(s.altFaces, fs.Face)
		}
	} else if len(faces) > 0 {
		s.Face = faces[0]
	}
	if len(a.Emphasis) > 0 {
		s.HeavyFace, _ = nearestFace(a, faces, glyphEnc, 0)
	}
	if s.HeavyFace == nil {
		if b, ok := render.Sibling(faces, s.Face, "bold"); ok {
			s.HeavyFace = b
		} else {
			s.HeavyFace = s.Face
		}
	}
	s.letterGap = int(math.Round(a.LetterGap))
	s.wordGap = int(math.Round(a.WordGap))
	if s.wordGap <= s.letterGap {
		s.wordGap = s.letterGap + int(math.Round(0.4*a.XHeight))
	}
	for r, samples := range a.Samples {
		s.medoids[alphabet.Key{R: r, Span: -1}] = medoid(samples)
	}
	for span, m := range a.Emphasis {
		for r, samples := range m {
			s.medoids[alphabet.Key{R: r, Span: span}] = medoid(samples)
		}
	}
	return s
}

// HasEmphasis reports whether the alphabet learned a heavier pool, so that
// heavy spellings mean something.
func (s *Speller) HasEmphasis() bool { return len(s.A.Emphasis) > 0 }

// recoded is a copy of a whose samples carry enc's codes, with the
// within-character spread measured in that code.
func recoded(a *alphabet.Alphabet, enc encoder.Encoder, cache *Cache) *alphabet.Alphabet {
	c := *a
	c.Samples = map[rune][]alphabet.Glyph{}
	c.Emphasis = map[int]map[rune][]alphabet.Glyph{}
	bits := enc.Bits()
	var spreads []float64
	pool := func(samples []alphabet.Glyph) []alphabet.Glyph {
		out := make([]alphabet.Glyph, len(samples))
		for i, g := range samples {
			k := sampleKey{g.Box, g.Baseline}
			code, ok := cache.samples[k]
			if !ok {
				code = a.Recode(g, enc)
				cache.samples[k] = code
			}
			g.Code = code
			out[i] = g
		}
		if len(out) >= 2 {
			cen := bitcode.Majority(codesOf(out), bits)
			sum := 0.0
			for _, g := range out {
				sum += encoder.NormalizedDistance(g.Code, cen, bits)
			}
			spreads = append(spreads, sum/float64(len(out)))
		}
		return out
	}
	for r, samples := range a.Samples {
		c.Samples[r] = pool(samples)
	}
	for span, m := range a.Emphasis {
		c.Emphasis[span] = map[rune][]alphabet.Glyph{}
		for r, samples := range m {
			c.Emphasis[span][r] = pool(samples)
		}
	}
	if len(spreads) > 0 {
		sum := 0.0
		for _, v := range spreads {
			sum += v
		}
		c.Spread = sum / float64(len(spreads))
	}
	return &c
}

func codesOf(gs []alphabet.Glyph) []bitcode.Code {
	out := make([]bitcode.Code, len(gs))
	for i, g := range gs {
		out[i] = g.Code
	}
	return out
}

// synthCode is the frame code of a synthesized glyph, cached across the
// verification's spellers by face, character, target size, and stroke.
func (s *Speller) synthCode(face *render.Face, r rune, sg render.Synth, stroke float64) bitcode.Code {
	k := synthKey{face: face.Name, r: r, xh: int(math.Round(4 * s.A.XHeight)), stroke: int(math.Round(10 * stroke))}
	if code, ok := s.cache.synth[k]; ok {
		return code
	}
	code := frameCode(sg, s.A.XHeight, s.GlyphEnc)
	s.cache.synth[k] = code
	return code
}

// medoid is the sample nearest all the others by frame code; samples that
// are components of their own are preferred over ones cut from merged pairs.
func medoid(samples []alphabet.Glyph) alphabet.Glyph {
	pool := samples[:0:0]
	for _, g := range samples {
		if !g.Derived {
			pool = append(pool, g)
		}
	}
	if len(pool) == 0 {
		pool = samples
	}
	best, bestSum := pool[0], math.MaxInt
	for i, g := range pool {
		sum := 0
		for j, h := range pool {
			if i != j {
				sum += bitcode.Distance(g.Code, h.Code)
			}
		}
		if sum < bestSum {
			best, bestSum = g, sum
		}
	}
	return best
}

// NearestFace scores each face by synthesizing the learned lowercase letters
// at the alphabet's x-height and measuring their frame codes against the
// learned centroids.
func NearestFace(a *alphabet.Alphabet, faces []*render.Face, glyphEnc encoder.Encoder) (*render.Face, float64) {
	return nearestFace(a, faces, glyphEnc, -1)
}

// FaceScore is how well one bundled face reproduces a pool of the alphabet.
type FaceScore struct {
	Face     *render.Face
	Score    float64 // mean normalized distance of synthesized glyphs to the pool's centroids
	Compared int
}

// FaceScores scores every face against the centroids of one pool: the body
// (span -1) by its lowercase letters, an emphasis span by its letters. Each
// synthesized glyph is first brought to the pool's stroke width, so the
// score compares letterforms alone: the image's threshold thickens strokes
// relative to a synthesized rendering, and left uncorrected that bias made
// every bold face outscore its regular sibling against a regular body.
func FaceScores(a *alphabet.Alphabet, faces []*render.Face, glyphEnc encoder.Encoder, span int) []FaceScore {
	stroke := a.BodyStroke()
	if span >= 0 {
		stroke = a.SpanStroke(span)
	}
	var out []FaceScore
	for _, f := range faces {
		sum, n := 0.0, 0
		for key, cen := range a.Centroid {
			if key.Span != span || !unicode.IsLetter(key.R) || (span < 0 && !unicode.IsLower(key.R)) {
				continue
			}
			byCap := unicode.IsUpper(key.R) && a.CapHeight > 0
			target := a.XHeight
			if byCap {
				target = a.CapHeight
			}
			sg, err := f.Glyph(key.R, target, byCap)
			if err != nil {
				continue
			}
			if stroke > 0 {
				sg.Bin = sg.Bin.WithStroke(stroke)
			}
			sum += encoder.NormalizedDistance(frameCode(sg, a.XHeight, glyphEnc), cen, glyphEnc.Bits())
			n++
		}
		if n > 0 {
			out = append(out, FaceScore{Face: f, Score: sum / float64(n), Compared: n})
		}
	}
	return out
}

func nearestFace(a *alphabet.Alphabet, faces []*render.Face, glyphEnc encoder.Encoder, span int) (*render.Face, float64) {
	var best *render.Face
	bestD := math.Inf(1)
	for _, fs := range FaceScores(a, faces, a.Block.Encoder(), span) {
		if fs.Score < bestD {
			best, bestD = fs.Face, fs.Score
		}
	}
	if best == nil && len(faces) > 0 {
		best = faces[0]
	}
	return best, bestD
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s[len(s)/2]
}

// frameCode encodes a synthesized glyph the way the alphabet encodes its own.
func frameCode(sg render.Synth, xh float64, enc encoder.Encoder) bitcode.Code {
	side := int(math.Ceil(2.2 * xh))
	canvas := bitmap.New(sg.Box.Dx()+2*side, 4*side)
	baseline := 2 * side
	box := sg.Box.Add(image.Pt(side, baseline))
	blit(canvas, sg.Bin, box.Min)
	return enc.Encode(alphabet.Frame(canvas, box, baseline, xh))
}

func blit(dst *bitmap.Bitmap, src *bitmap.Bitmap, at image.Point) {
	for y := range src.H {
		for x := range src.W {
			if src.Pix[y*src.W+x] != 0 {
				dst.Set(at.X+x, at.Y+y, 1)
			}
		}
	}
}

// piece returns the glyph to place for r. Regular spellings take the body
// medoid, then an emphasis medoid, then a glyph synthesized from the body
// face; heavy spellings take an emphasis medoid, then a glyph synthesized
// from the heavy face, then the body medoid.
func (s *Speller) piece(r rune, heavy bool) (piece, error) {
	body, hasBody := s.medoids[alphabet.Key{R: r, Span: -1}]
	var emph alphabet.Glyph
	hasEmph := false
	for span := range s.A.Emphasis {
		if g, ok := s.medoids[alphabet.Key{R: r, Span: span}]; ok {
			emph, hasEmph = g, true
			break
		}
	}
	if heavy {
		if hasEmph {
			return fromGlyph(emph, 'e'), nil
		}
		if p, err := s.synthesize(r, s.HeavyFace, s.heavySynth); err == nil {
			return p, nil
		}
		if hasBody {
			return fromGlyph(body, 'l'), nil
		}
		return piece{}, fmt.Errorf("spell: no glyph for %q", r)
	}
	if hasBody {
		return fromGlyph(body, 'l'), nil
	}
	if hasEmph {
		return fromGlyph(emph, 'e'), nil
	}
	return s.synthesize(r, s.Face, s.synth)
}

func (s *Speller) synthesize(r rune, face *render.Face, cache map[rune]piece) (piece, error) {
	if p, ok := cache[r]; ok {
		return p, nil
	}
	if face == nil {
		return piece{}, fmt.Errorf("spell: no face to synthesize %q", r)
	}
	byCap := (unicode.IsUpper(r) || unicode.IsDigit(r)) && s.A.CapHeight > 0
	target := s.A.XHeight
	if byCap {
		target = s.A.CapHeight
	}
	sg, err := face.Glyph(r, target, byCap)
	if err != nil {
		return piece{}, err
	}
	// Bring the stroke to the learned weight so a synthesized glyph sits
	// beside learned ones as if printed by the same press.
	stroke := s.bodyStroke
	if face == s.HeavyFace && s.heavyStroke > 0 {
		stroke = s.heavyStroke
	}
	if stroke > 0 {
		sg.Bin = sg.Bin.WithStroke(stroke)
	}
	p := piece{bin: sg.Bin, gray: sg.Gray, box: sg.Box, code: s.synthCode(face, r, sg, stroke), source: 's'}
	cache[r] = p
	return p, nil
}

// alternatives returns r synthesized in the next-nearest faces, stroke
// normalized, for characters the reference did not teach. No single
// bundled face reproduces a held-out font's digits; the nearest of a few
// usually comes close enough while a wrong digit stays far in all of them.
func (s *Speller) alternatives(r rune) []bitcode.Code {
	if codes, ok := s.altCodes[r]; ok {
		return codes
	}
	var codes []bitcode.Code
	byCap := (unicode.IsUpper(r) || unicode.IsDigit(r)) && s.A.CapHeight > 0
	target := s.A.XHeight
	if byCap {
		target = s.A.CapHeight
	}
	for _, f := range s.altFaces {
		sg, err := f.Glyph(r, target, byCap)
		if err != nil {
			continue
		}
		if s.bodyStroke > 0 {
			sg.Bin = sg.Bin.WithStroke(s.bodyStroke)
		}
		codes = append(codes, s.synthCode(f, r, sg, s.bodyStroke))
	}
	s.altCodes[r] = codes
	return codes
}

func fromGlyph(g alphabet.Glyph, source byte) piece {
	return piece{bin: g.Bin, gray: g.Gray, box: g.Box.Sub(image.Pt(g.Box.Min.X, g.Baseline)), code: g.Code, source: source, derived: g.Derived}
}

// Target returns the glyph-wise codeword for text: a frame code per
// non-space character, composed pair and triple codes for touching runs,
// plus the vertical ink extent of the composite relative to its baseline.
func (s *Speller) Target(text string, heavy bool) (t alphabet.Target, top, bottom int, err error) {
	t.Text = []rune(text)
	t.Codes = make([]bitcode.Code, len(t.Text))
	top, bottom = math.MaxInt, math.MinInt
	for i, r := range t.Text {
		if r == ' ' {
			continue
		}
		p, err := s.piece(r, heavy)
		if err != nil {
			return alphabet.Target{}, 0, 0, err
		}
		t.Codes[i] = p.code
		top = min(top, p.box.Min.Y)
		bottom = max(bottom, p.box.Max.Y)
		// Synthesized characters carry the next-nearest faces as
		// alternatives. A character learned from a claim rather than the
		// reference carries the synthesized glyphs too, so learning it can
		// only add evidence, never replace a good synthesis with a worse
		// sample.
		if !heavy && (p.source == 's' || p.derived) {
			alts := s.alternatives(r)
			if p.derived {
				if sp, err := s.synthesize(r, s.Face, s.synth); err == nil {
					alts = append([]bitcode.Code{sp.code}, alts...)
				}
			}
			if len(alts) > 0 {
				if t.Alt == nil {
					t.Alt = map[int][]bitcode.Code{}
				}
				t.Alt[i] = alts
			}
		}
	}
	// Composed run codes are memoized per target: an alignment asks for
	// them once per region, and a candidate is aligned to many regions.
	memo := map[int]bitcode.Code{}
	run := func(c, n int) bitcode.Code {
		key := c*4 + n
		if code, ok := memo[key]; ok {
			return code
		}
		// The same run of characters composes to the same code in every
		// candidate of this speller; the cache is keyed by the text.
		var rkey string
		if c+n <= len(t.Text) {
			rkey = string(t.Text[c:c+n]) + "|" + map[bool]string{false: "r", true: "h"}[heavy]
			if code, ok := s.cache.runs[rkey]; ok {
				memo[key] = code
				return code
			}
		}
		var code bitcode.Code
		if c+n <= len(t.Text) {
			ps := make([]piece, 0, n)
			for i := c; i < c+n; i++ {
				if t.Text[i] == ' ' {
					ps = nil
					break
				}
				p, err := s.piece(t.Text[i], heavy)
				if err != nil {
					ps = nil
					break
				}
				ps = append(ps, p)
			}
			if len(ps) == n {
				code = s.runCode(ps)
			}
		}
		memo[key] = code
		if rkey != "" {
			s.cache.runs[rkey] = code
		}
		return code
	}
	t.Pair = func(c int) bitcode.Code { return run(c, 2) }
	t.Triple = func(c int) bitcode.Code { return run(c, 3) }
	// A three-way merge is rare and already costs two structural units;
	// under a costly encoder its composed code is not worth computing.
	if c, ok := s.GlyphEnc.(encoder.Costly); ok && c.Costly() {
		t.Triple = nil
	}
	return t, top, bottom, nil
}

// runCode frames pieces set side by side at the label's letter gap, as a
// touching run in the image would be framed.
func (s *Speller) runCode(ps []piece) bitcode.Code {
	xh := s.A.XHeight
	side := int(math.Ceil(2.2 * xh))
	w := 0
	for i, p := range ps {
		if i > 0 {
			w += s.letterGap
		}
		w += p.box.Dx()
	}
	canvas := bitmap.New(w+2*side, 4*side)
	baseline := 2 * side
	x := side
	box := image.Rectangle{}
	for i, p := range ps {
		at := image.Pt(x, baseline+p.box.Min.Y)
		blit(canvas, p.bin, at)
		r := image.Rect(at.X, at.Y, at.X+p.box.Dx(), at.Y+p.box.Dy())
		if i == 0 {
			box = r
		} else {
			box = box.Union(r)
		}
		x += p.box.Dx() + s.letterGap
	}
	return s.GlyphEnc.Encode(alphabet.Frame(canvas, box, baseline, xh))
}

// Spell composes text on one baseline with the label's own letter and word
// gaps, tight to the ink like an encoded region, and encodes it.
func (s *Speller) Spell(text, value string) (Codeword, error) {
	return s.spell(text, value, false, float64(s.letterGap), float64(s.wordGap))
}

// SpellAs is Spell with a choice of weight.
func (s *Speller) SpellAs(text, value string, heavy bool) (Codeword, error) {
	return s.spell(text, value, heavy, float64(s.letterGap), float64(s.wordGap))
}

// SpellFit composes text so that its aspect matches a region of width×height
// px: the composite lives at the reference's glyph size, so the region's
// width is first brought to that scale through the ratio of ink heights,
// then the label's gap proportions are scaled to fill it.
func (s *Speller) SpellFit(text, value string, heavy bool, width, height int) (Codeword, error) {
	letters, words, glyphs := 0, 0, 0
	top, bottom := math.MaxInt, math.MinInt
	prev := rune(-1)
	for _, r := range text {
		if r == ' ' {
			words++
		} else {
			p, err := s.piece(r, heavy)
			if err != nil {
				return Codeword{}, err
			}
			glyphs += p.box.Dx()
			top = min(top, p.box.Min.Y)
			bottom = max(bottom, p.box.Max.Y)
			if prev >= 0 && prev != ' ' {
				letters++
			}
		}
		prev = r
	}
	if glyphs == 0 || height <= 0 || bottom <= top {
		return s.spell(text, value, heavy, 0, 0)
	}
	scale := float64(bottom-top) / float64(height)
	budget := float64(width)*scale - float64(glyphs)
	if budget <= 0 || letters+words == 0 {
		return s.spell(text, value, heavy, 0, 0)
	}
	ratio := 3.0
	if s.letterGap > 0 {
		ratio = float64(s.wordGap) / float64(s.letterGap)
	}
	unit := budget / (float64(letters) + ratio*float64(words))
	return s.spell(text, value, heavy, unit, unit*ratio)
}

func (s *Speller) spell(text, value string, heavy bool, letterGap, wordGap float64) (Codeword, error) {
	type placed struct {
		p piece
		x int
	}
	var ps []placed
	pen := 0.0
	prev := rune(-1)
	var synthesized, emphasis []rune
	for _, r := range text {
		if r == ' ' {
			pen += wordGap
			prev = r
			continue
		}
		p, err := s.piece(r, heavy)
		if err != nil {
			return Codeword{}, err
		}
		switch p.source {
		case 's':
			synthesized = append(synthesized, r)
		case 'e':
			emphasis = append(emphasis, r)
		}
		if prev >= 0 && prev != ' ' {
			pen += letterGap
		}
		ps = append(ps, placed{p, int(math.Round(pen))})
		pen += float64(p.box.Dx())
		prev = r
	}
	if len(ps) == 0 {
		return Codeword{}, fmt.Errorf("spell: nothing to spell in %q", text)
	}
	top, bottom := math.MaxInt, math.MinInt
	for _, pl := range ps {
		top = min(top, pl.p.box.Min.Y)
		bottom = max(bottom, pl.p.box.Max.Y)
	}
	last := ps[len(ps)-1]
	w, h := last.x+last.p.box.Dx(), bottom-top
	bin := bitmap.New(w, h)
	gray := image.NewGray(image.Rect(0, 0, w, h))
	for i := range gray.Pix {
		gray.Pix[i] = 255
	}
	baseline := -top
	for _, pl := range ps {
		at := image.Pt(pl.x, baseline+pl.p.box.Min.Y)
		blit(bin, pl.p.bin, at)
		for y := range pl.p.bin.H {
			for x := range pl.p.bin.W {
				gx, gy := at.X+x, at.Y+y
				if gx < 0 || gy < 0 || gx >= w || gy >= h {
					continue
				}
				v := pl.p.gray.Pix[pl.p.gray.PixOffset(pl.p.gray.Rect.Min.X+x, pl.p.gray.Rect.Min.Y+y)]
				if v < gray.Pix[gy*w+gx] {
					gray.Pix[gy*w+gx] = v
				}
			}
		}
	}
	patch := encoder.Patch{Bin: bin, Gray: gray}
	cw := Codeword{Text: text, Value: value, Heavy: heavy, Code: s.Enc.Encode(patch), Patch: patch}
	cw.Params.Synthesized = string(synthesized)
	cw.Params.Emphasis = string(emphasis)
	cw.Params.Weight = "regular"
	if heavy {
		cw.Params.Weight = "heavy"
	}
	if len(synthesized) > 0 {
		face := s.Face
		if heavy {
			face = s.HeavyFace
		}
		if face != nil {
			cw.Params.Face = face.Name
		}
	}
	return cw, nil
}
