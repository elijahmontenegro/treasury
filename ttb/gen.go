package ttb

import (
	"fmt"
	"image"
	"math"
	"math/rand"
	"strings"

	"treasury/internal/render"
	"treasury/internal/synth"
)

// Printed is what actually went onto a generated label, against the
// application in Expected. Under an error the two differ.
type Printed struct {
	Family    string   `json:"family"` // spirits, wine, beer
	Brand     string   `json:"brand"`
	Class     string   `json:"class"`
	Producer  []string `json:"producer"`
	Origin    string   `json:"origin"`
	ABV       float64  `json:"abv"`
	ABVText   string   `json:"abv_text"`
	NetML     float64  `json:"net_ml"`
	NetText   string   `json:"net_text"`
	Error     string   `json:"error"` // "", wrong_abv, wrong_net, wrong_brand, wrong_class, title_header, regular_header, wording, missing_abv, missing_net, missing_class
	Wording   string   `json:"wording,omitempty"`
	BodyFace  string   `json:"body_face"`
	HeavyFace string   `json:"heavy_face"`
	BrandFace string   `json:"brand_face"`

	// What the label actually prints for each claim, transcribed by eye
	// (amendment step 12a). "=" is the filed value printed as filed, ""
	// is not on the label at all, and anything else is what the label
	// prints where the filed string does not match it. A set without it
	// is scored against its filed values as before.
	Carried map[string]string `json:"carried,omitempty"`

	// Which statements this label sets across two lines, so the detector
	// returns them in pieces (amendment step 27d). Recorded so the corpus
	// can be measured back against the population on this, as step 10b's
	// conventions are.
	Split map[string]bool `json:"split,omitempty"`

	// The conventions real labels showed (amendment step 5a), at the
	// frequencies seen in ten registry labels.
	ClaimFaces  map[string]string `json:"claim_faces,omitempty"` // face per claim; a claim set in the body face shares the warning's alphabet
	WarningCaps bool              `json:"warning_caps"`          // the warning in capitals (5 of 10)
	Inverted    bool              `json:"inverted"`              // light type on a dark ground (2 of 10)
	Vertical    bool              `json:"vertical_warning"`      // the warning along a side (2 of 10)
	Crowded     bool              `json:"crowded"`               // rows of other text in the warning's size against it (2 of 10)
}

// FacePool is the type available to the generator: families with both a
// regular and a bold face carry body text; any face may set the brand.
type FacePool struct {
	Body    []*render.Face // regular faces whose family also has a bold
	Heavy   map[string]*render.Face
	Display []*render.Face // every text-capable face
}

// NewFacePool sorts faces into the pool, dropping faces that cannot render
// the characters a label needs or whose x-height is not that of a text
// face.
func NewFacePool(faces []*render.Face) FacePool {
	p := FacePool{Heavy: map[string]*render.Face{}}
	usable := map[string]bool{}
	for _, f := range faces {
		if textCapable(f) {
			usable[f.Name] = true
			p.Display = append(p.Display, f)
		}
	}
	for _, f := range faces {
		if f.Weight == "bold" && usable[f.Name] {
			p.Heavy[f.Family] = f
		}
	}
	for _, f := range faces {
		if f.Weight == "regular" && usable[f.Name] && p.Heavy[f.Family] != nil {
			p.Body = append(p.Body, f)
		}
	}
	return p
}

// textCapable requires every character a label prints and an x-height
// between 0.35 and 0.7 of the size, which excludes symbol and all-caps faces.
func textCapable(f *render.Face) bool {
	for _, r := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789%.,():/" {
		if _, err := f.Glyph(r, 14, false); err != nil {
			return false
		}
	}
	face, err := f.At(100)
	if err != nil {
		return false
	}
	xh := float64(face.Metrics().XHeight) / 64 / 100
	return xh >= 0.35 && xh <= 0.7
}

var (
	brandWords = []string{"Silver", "Fox", "Reserve", "Old", "Tom", "Copper", "Creek", "Highland", "Valley", "Ridge", "Black", "Oak", "Harbor", "Stone", "Meadow", "Crown", "North", "Star", "Golden", "Barrel", "River", "Mill", "King's", "Saint", "Fork", "Iron", "Gate", "Wild", "Heron", "Anchor"}
	classes    = map[string][]string{
		"spirits": {"Kentucky Straight Bourbon Whiskey", "London Dry Gin", "Straight Rye Whiskey", "Vodka", "Silver Rum", "Blended Scotch Whisky", "Brandy"},
		"wine":    {"Cabernet Sauvignon", "Red Table Wine", "Chardonnay", "Sparkling Wine", "Pinot Noir", "Rose Wine"},
		"beer":    {"India Pale Ale", "Lager", "Stout", "Pilsner", "Wheat Beer", "Amber Ale"},
	}
	producers = map[string][]string{
		"spirits": {"Distilled and Bottled by", "Produced and Bottled by", "Bottled by"},
		"wine":    {"Produced and Bottled by", "Vinted and Bottled by", "Cellared and Bottled by"},
		"beer":    {"Brewed and Bottled by", "Brewed and Canned by", "Brewed by"},
	}
	cities  = []string{"Louisville, Kentucky 40202", "Napa, California 94558", "Portland, Oregon 97209", "Austin, Texas 78701", "Brooklyn, New York 11201", "Denver, Colorado 80202", "Asheville, North Carolina 28801"}
	origins = []string{"Product of USA", "Product of USA", "Product of USA", "Product of Scotland", "Product of France", "Product of Mexico", "Product of Ireland"}
	nets    = map[string][]float64{
		"spirits": {750, 750, 1000, 375, 1750, 50},
		"wine":    {750, 750, 375, 187, 1000},
		"beer":    {750, 500, 1000, 375},
	}
	wordingErrors = []struct{ from, to string }{
		{"and may cause health problems", "and might cause health problems"},
		{"women should not drink", "women should never drink"},
		{"because of the risk of birth defects", "because of the chance of birth defects"},
		{"impairs your ability", "impairs the ability"},
		{"to drive a car or operate machinery", "to drive a car or use machinery"},
	}
)

// Generate draws one label: the application, what was printed, and the
// document to render. With probability errorRate the label carries one of
// the deliberate errors.
func Generate(rng *rand.Rand, pool FacePool, errorRate float64) (synth.Document, Expected, Printed) {
	family := []string{"spirits", "wine", "beer"}[rng.Intn(3)]
	exp := Expected{Beverage: family}
	n := 2 + rng.Intn(2)
	var words []string
	for len(words) < n {
		w := brandWords[rng.Intn(len(brandWords))]
		dup := false
		for _, x := range words {
			dup = dup || x == w
		}
		if !dup {
			words = append(words, w)
		}
	}
	exp.Brand = strings.Join(words, " ")
	if rng.Intn(2) == 0 {
		exp.Brand = strings.ToUpper(exp.Brand)
	}
	exp.Class = classes[family][rng.Intn(len(classes[family]))]
	exp.Producer = []string{producers[family][rng.Intn(len(producers[family]))] + " " + titleWords(words) + " Company", cities[rng.Intn(len(cities))]}
	exp.Origin = origins[rng.Intn(len(origins))]
	switch family {
	case "spirits":
		exp.ABV = 35 + float64(rng.Intn(31))*0.5
	case "wine":
		exp.ABV = 11 + float64(rng.Intn(9))*0.5
	default:
		exp.ABV = 4 + float64(rng.Intn(11))*0.5
	}
	exp.NetML = nets[family][rng.Intn(len(nets[family]))]

	body := pool.Body[rng.Intn(len(pool.Body))]
	heavy := pool.Heavy[body.Family]
	brandFace := heavy
	if rng.Intn(2) == 0 && len(pool.Display) > 0 {
		brandFace = pool.Display[rng.Intn(len(pool.Display))]
	}
	// Three of four claims are set in a face other than the warning's,
	// as on real labels, where only the warning is in the warning's face.
	claimFaces := map[string]string{}
	for _, c := range []string{"class", "producer", "origin", "abv", "net"} {
		face := body
		if rng.Float64() < 0.75 {
			face = otherBody(rng, pool, body)
		}
		claimFaces[c] = face.Name
	}
	caps := rng.Float64() < 0.5
	inverted := rng.Float64() < 0.2
	vertical := rng.Float64() < 0.2
	crowded := rng.Float64() < 0.2
	pr := Printed{
		Family: family, Brand: exp.Brand, Class: exp.Class, Producer: exp.Producer, Origin: exp.Origin,
		ABV: exp.ABV, NetML: exp.NetML, BodyFace: body.Name, HeavyFace: heavy.Name, BrandFace: brandFace.Name,
		ClaimFaces: claimFaces, WarningCaps: caps, Inverted: inverted, Vertical: vertical, Crowded: crowded,
	}
	// Which statements the detector will be made to return in pieces.
	// The rate is the population's, measured at 27d from the fifty's own
	// verdicts, and it is drawn per claim because that is the figure that
	// differs: 0.45 of the claims the fifty verify are matched across a
	// chain against 0.25 on the corpus, while the share of labels
	// carrying at least one was already the same.
	split := map[string]bool{}
	for _, c := range []string{"brand", "class", "producer", "origin", "abv", "net"} {
		if rng.Float64() < Measured().SplitStatement {
			split[c] = true
		}
	}
	pr.Split = split
	v := Variant{BodyFace: body.Name, HeavyFace: heavy.Name, BrandFace: brandFace.Name,
		ClaimFaces: claimFaces, WarningCaps: caps, Vertical: vertical, Crowded: crowded,
		Split: split}
	if rng.Float64() < errorRate {
		switch pr.Error = []string{"wrong_abv", "wrong_net", "title_header", "regular_header", "wording", "missing_abv", "missing_net", "missing_class", "wrong_brand", "wrong_class"}[rng.Intn(10)]; pr.Error {
		case "wrong_brand":
			for pr.Brand == exp.Brand {
				var other []string
				for len(other) < n {
					w := brandWords[rng.Intn(len(brandWords))]
					dup := false
					for _, x := range other {
						dup = dup || x == w
					}
					if !dup {
						other = append(other, w)
					}
				}
				pr.Brand = strings.Join(other, " ")
				if strings.ToUpper(exp.Brand) == exp.Brand {
					pr.Brand = strings.ToUpper(pr.Brand)
				}
			}
		case "wrong_class":
			for pr.Class == exp.Class {
				pr.Class = classes[family][rng.Intn(len(classes[family]))]
			}
		case "wrong_abv":
			for pr.ABV == exp.ABV {
				pr.ABV = exp.ABV + float64(rng.Intn(11)-5)*0.5
				if pr.ABV < 0.5 {
					pr.ABV = exp.ABV + 0.5
				}
			}
		case "wrong_net":
			for pr.NetML == exp.NetML {
				pr.NetML = nets[family][rng.Intn(len(nets[family]))]
			}
		case "title_header":
			v.HeaderTitleCase = true
		case "regular_header":
			v.HeaderRegular = true
			pr.HeavyFace = body.Name
		case "wording":
			we := wordingErrors[rng.Intn(len(wordingErrors))]
			pr.Wording = strings.Replace(Statute, we.from, we.to, 1)
			v.Wording = pr.Wording
		case "missing_abv":
			pr.ABV = 0
		case "missing_net":
			pr.NetML = 0
		case "missing_class":
			pr.Class = ""
		}
	}
	pr.ABVText = abvForms(pr.ABV)[rng.Intn(6)]
	pr.NetText = netForms(pr.NetML)[rng.Intn(4)]
	return layout(rng, pr, v), exp, pr
}

// otherBody picks a body face from another family than body, or body when
// the pool has no other.
func otherBody(rng *rand.Rand, pool FacePool, body *render.Face) *render.Face {
	var others []*render.Face
	for _, f := range pool.Body {
		if f.Family != body.Family {
			others = append(others, f)
		}
	}
	if len(others) == 0 {
		return body
	}
	return others[rng.Intn(len(others))]
}

func titleWords(words []string) string {
	out := make([]string, len(words))
	for i, w := range words {
		r := []rune(strings.ToLower(w))
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		out[i] = string(r)
	}
	return strings.Join(out, " ")
}

func abvForms(v float64) []string {
	if v == 0 {
		return []string{"", "", "", "", "", ""}
	}
	n := Num(v)
	return []string{n + "% Alc./Vol.", n + "% ALC./VOL.", n + "% ABV", "ALC. " + n + "% BY VOL.", n + "% alc/vol", "(" + Num(2*v) + " Proof)"}
}

func netForms(ml float64) []string {
	if ml == 0 {
		return []string{"", "", "", ""}
	}
	if ml >= 1000 {
		l := Num(ml / 1000)
		return []string{l + " L", l + "L", l + " Liter", l + " L"}
	}
	m := Num(ml)
	return []string{m + " mL", m + " ml", m + "mL", m + " ML"}
}

// layout places the printed label over artwork, at the population's own
// sizes and type scale.
//
// A generated label is what the fifty real ones are: a ground with panels,
// rules, a pattern and a barcode, and text composited over it at the
// contrast the fifty keep. Every number it draws from is in population.go
// with the measurement it came from.
func layout(rng *rand.Rand, pr Printed, v Variant) synth.Document {
	pop := Measured()
	w, h := Size(rng.Intn(len(Sizes)))
	long := max(w, h)
	doc := synth.Document{W: w, H: h}

	// Type is sized from the population: the median glyph stood 0.0059 of
	// the label's longer side, and everything else is set relative to the
	// warning, which is the smallest type a label carries.
	glyph := (pop.GlyphFracLow + rng.Float64()*(pop.GlyphFracHigh-pop.GlyphFracLow)) * float64(long)
	wpx := math.Max(5, glyph/0.80) // the rendered glyph stands taller than the fraction asks; measured back against the population
	size := func(mult float64) float64 { return wpx * mult }

	// The ground, and one panel of the other polarity. A third of the marks
	// on a real label are light on dark, and they are light because they
	// sit on a band or a block, not because the label is inverted.
	art := &synth.Art{Ground: pop.DarkGround, Angle: rng.Float64() * 3.14159}
	if rng.Float64() < pop.Gradient {
		art.Gradient = 0.10 + rng.Float64()*0.25
	}
	if rng.Float64() < pop.Texture {
		art.Texture = 0.006 + rng.Float64()*0.014
	}
	if rng.Float64() < pop.Pattern {
		cell := max(6, long/120)
		art.Pattern = &synth.Pattern{Cell: cell, Radius: max(1, cell/4), Grey: shift(art.Ground, -10-rng.Intn(22)), Rect: image.Rect(0, 0, w, h)}
	}
	if rng.Float64() < pop.Rules {
		t := max(2, long/400)
		pad := long / 40
		art.Rules = append(art.Rules,
			synth.Panel{Rect: image.Rect(pad, pad, w-pad, pad+t), Grey: shift(art.Ground, -120)},
			synth.Panel{Rect: image.Rect(pad, h-pad-t, w-pad, h-pad), Grey: shift(art.Ground, -120)},
			synth.Panel{Rect: image.Rect(pad, pad, pad+t, h-pad), Grey: shift(art.Ground, -120)},
			synth.Panel{Rect: image.Rect(w-pad-t, pad, w-pad, h-pad), Grey: shift(art.Ground, -120)})
	}
	if rng.Float64() < pop.Barcode {
		bw, bh := long/6, long/12
		art.Barcode = &synth.Barcode{Rect: image.Rect(w-bw-long/30, h-bh-long/30, w-long/30, h-long/30), Grey: 20}
	}
	// The panel the warning may sit on, and the ink for each polarity at
	// the contrast the population keeps.
	dark := pop.ContrastLow + rng.Float64()*(pop.ContrastHigh-pop.ContrastLow)
	// The contrast measured on the population is what survives the
	// engine's resize, not what was printed: small type blends with its
	// ground. Ink is drawn well clear of it and the corpus is measured
	// back against the fifty.
	inkOnLight := shift(art.Ground, -int(150+dark*220))
	inkOnDark := shift(pop.LightGround, +int(150+dark*220))
	// The panel covers a band that text actually falls in, since that is
	// how a third of a real label's marks come to be light on dark.
	// The panel is decided here and placed once the text's own extent is
	// known, since a third of a real label's marks are light on dark and
	// that only happens where the panel is under the words.
	panel := image.Rectangle{}
	onPanel := rng.Float64() < pop.LightOnDark
	// Ornament the text may overlap: a seal, a crest, a wash.
	for range rng.Intn(3) {
		ow := long / (6 + rng.Intn(6))
		x0, y0 := rng.Intn(max(1, w-ow)), rng.Intn(max(1, h-ow))
		art.Ornaments = append(art.Ornaments, synth.Ornament{
			Rect: image.Rect(x0, y0, x0+ow, y0+ow), Grey: shift(art.Ground, -30-rng.Intn(40)), Round: rng.Intn(2) == 0,
		})
	}
	doc.Art = art

	inkAt := func(y int) uint8 {
		if onPanel && y >= panel.Min.Y && y < panel.Max.Y {
			return inkOnDark
		}
		return inkOnLight
	}

	cx := w / 2
	if v.Vertical {
		cx = (w + long/6) / 2 // the warning takes the left side
	}
	face := func(claim string) string {
		if f, ok := v.ClaimFaces[claim]; ok && f != "" {
			return f
		}
		return v.BodyFace
	}
	// A statement the population sets across two lines is emitted as two
	// items on consecutive lines, cut at the word boundary nearest the
	// middle, so the detector returns it in pieces and the run builder
	// has to chain them before a claim can be compared (step 27d). The
	// second line is indented a little, as a wrapped line on a real label
	// is, which also gives the adjacency test something to be tested by:
	// the halves still overlap in x by far more than half their width.
	//
	// It returns the height it used, since a split statement takes two
	// lines where the caller budgeted one.
	item := func(text, face string, px float64, y int, claim string) int {
		if text == "" {
			return 0
		}
		ink := inkAt(y)
		cut := -1
		if v.Split[claim] {
			mid := len(text) / 2
			for off := 0; off < len(text)/2; off++ {
				if mid-off > 0 && text[mid-off] == ' ' {
					cut = mid - off
					break
				}
				if mid+off < len(text) && text[mid+off] == ' ' {
					cut = mid + off
					break
				}
			}
		}
		if cut <= 0 {
			doc.Items = append(doc.Items, synth.Item{Text: text, Face: face, Px: px, X: cx, Y: y, Center: true, Claim: claim, Ink: ink})
			return 0
		}
		drop := int(px * 1.15)
		doc.Items = append(doc.Items,
			synth.Item{Text: text[:cut], Face: face, Px: px, X: cx, Y: y, Center: true, Claim: claim, Ink: ink},
			synth.Item{Text: text[cut+1:], Face: face, Px: px, X: cx + int(px), Y: y + drop, Center: true, Claim: claim, Ink: inkAt(y + drop)})
		return drop
	}
	step := func(mult float64) int { return int(math.Round(wpx * mult * (0.9 + rng.Float64()*0.3))) }

	y := int(float64(h)*0.06) + rng.Intn(max(1, h/20))
	y += item(pr.Brand, v.BrandFace, size(3.0+rng.Float64()), y, "brand")
	y += step(4.5)
	if rng.Intn(2) == 0 {
		item([]string{"SMALL BATCH", "ESTATE BOTTLED", "LIMITED RELEASE", "CRAFT BREWED"}[rng.Intn(4)], v.HeavyFace, size(1.5), y, "")
		y += step(3)
	}
	y += item(pr.Class, face("class"), size(1.8+rng.Float64()*0.6), y, "class")
	y += step(3.2)
	if rng.Intn(2) == 0 {
		item([]string{"AGED 8 YEARS", "Batch No. 12 - Est. 1887", "Distilled from grain", "Vintage 2019", "Unfiltered"}[rng.Intn(5)], v.BodyFace, size(1.3), y, "")
		y += step(2.8)
	}
	npx := size(1.4 + rng.Float64()*0.4)
	if pr.ABVText != "" && pr.NetText != "" && rng.Intn(2) == 0 {
		left := w / 10
		if v.Vertical {
			left = long / 5
		}
		doc.Items = append(doc.Items,
			synth.Item{Text: pr.ABVText, Face: face("abv"), Px: npx, X: left, Y: y, Claim: "abv", Ink: inkAt(y)},
			synth.Item{Text: pr.NetText, Face: face("net"), Px: npx, X: w * 3 / 4, Y: y, Claim: "net", Ink: inkAt(y)},
		)
		y += step(3.2)
	} else {
		y += item(pr.ABVText, face("abv"), npx, y, "abv")
		if pr.ABVText != "" {
			y += step(2.4)
		}
		y += item(pr.NetText, face("net"), npx, y, "net")
		if pr.NetText != "" {
			y += step(3.2)
		}
	}
	ppx := size(1.2 + rng.Float64()*0.3)
	for _, line := range pr.Producer {
		y += item(line, face("producer"), ppx, y, "producer")
		y += int(ppx * 1.4)
	}
	y += item(pr.Origin, face("origin"), ppx, y, "origin")
	y += step(4)

	text := Statute
	if v.Wording != "" {
		text = v.Wording
	}
	if v.WarningCaps {
		text = strings.ToUpper(text)
	}
	if v.HeaderTitleCase {
		text = "Government Warning:" + text[HeaderLen:]
	}
	leading := 1.25 + rng.Float64()*0.2
	pitch := int(math.Round(wpx * leading))
	crowd := []string{
		"INGREDIENTS: WATER, BARLEY MALT, HOPS, YEAST.", "Imported by Ridge Imports LLC, 8859 NW 102nd Ct, Doral, FL 33178",
		"Contains sulfites. Return for refund where applicable.", "Produced and bottled under license. Store in a cool dry place.",
	}
	if v.Vertical {
		width := int(float64(h) * (0.55 + rng.Float64()*0.1))
		side := long / 40
		ground := art.Ground
		ink := inkOnLight
		if onPanel && rng.Intn(2) == 0 {
			// The warning runs down a dark band of its own.
			art.Panels = append(art.Panels, synth.Panel{Rect: image.Rect(0, 0, long/6, h), Grey: pop.LightGround})
			ground, ink = pop.LightGround, inkOnDark
		}
		doc.Blocks = append(doc.Blocks, synth.Block{
			Text: text, Heavy: []synth.Span{{Start: 0, End: HeaderLen}}, Face: v.BodyFace, HeavyFace: v.HeavyFace,
			Px: wpx, X: side, Y: (h - width) / 2, Width: width, Leading: leading, Claim: "reference", Quarters: 3,
			Ink: ink, Ground: ground,
		})
		if y+int(wpx*4) < h {
			item([]string{"Please drink responsibly.", "Enjoy responsibly.", "Keep refrigerated."}[rng.Intn(3)], v.BodyFace, size(1.1), y+int(wpx*2), "")
		}
		return doc
	}
	width := int(float64(w) * (0.7 + rng.Float64()*0.15))
	left := (w - width) / 2
	if onPanel {
		// Over the warning and what follows it, which is where a real
		// label puts its dark band.
		panel = image.Rect(0, y-pitch, w, min(h, y+pitch*9))
		art.Panels = append(art.Panels, synth.Panel{Rect: panel, Grey: pop.LightGround})
	}
	if v.Crowded {
		for range 1 + rng.Intn(2) {
			doc.Items = append(doc.Items, synth.Item{Text: crowd[rng.Intn(len(crowd))], Face: v.BodyFace, Px: wpx, X: left, Y: y, Claim: "", Ink: inkAt(y)})
			y += pitch
		}
	}
	doc.Blocks = append(doc.Blocks, synth.Block{
		Text:      text,
		Heavy:     []synth.Span{{Start: 0, End: HeaderLen}},
		Face:      v.BodyFace,
		HeavyFace: v.HeavyFace,
		Px:        wpx,
		X:         left,
		Y:         y,
		Width:     width,
		Leading:   leading,
		Claim:     "reference",
		Ink:       inkAt(y),
	})
	y += pitch * 6
	if v.Crowded {
		doc.Items = append(doc.Items, synth.Item{Text: crowd[rng.Intn(len(crowd))], Face: v.BodyFace, Px: wpx, X: left, Y: y, Claim: "", Ink: inkAt(y)})
		y += pitch
	}
	// The rest of what a label carries. A real label of this size holds
	// about twice the text the old corpus drew, so the filler is the
	// difference between a mock-up and a label.
	// A serving-facts line states the container's own fill, as a real
	// label's does. Stating a different one made the label say two fills
	// and the engine was right to call the difference: the corpus was
	// wrong, not the reading.
	serving := "Serving Facts   Servings per container 1"
	if pr.NetText != "" {
		serving = "Serving Facts   Serving size " + pr.NetText + "   Servings per container 1"
	}
	filler := []string{
		"Please drink responsibly.", "Enjoy responsibly.", "Keep refrigerated.",
		"CONTAINS SULFITES", "ME-MA-VT-CT-NY-DE-LA-OR-COL 5c, MI 10c REFUND",
		"INGREDIENTS: WATER, MALTED BARLEY, HOPS, YEAST.",
		serving,
		"Bottled under license. Store in a cool dry place, away from sunlight.",
		"www.example-brand.com", "Certified sustainable. Recycle where facilities exist.",
	}
	for y+int(wpx*3) < h {
		doc.Items = append(doc.Items, synth.Item{Text: filler[rng.Intn(len(filler))], Face: v.BodyFace, Px: size(0.9 + rng.Float64()*0.5), X: left, Y: y + int(wpx*2), Claim: "", Ink: inkAt(y + int(wpx*2))})
		y += int(wpx * (2.2 + rng.Float64()))
	}
	return doc
}

// shift moves a grey by d, clamped.
func shift(g uint8, d int) uint8 {
	v := int(g) + d
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(v)
}

// String renders the error for tables.
func (p Printed) String() string {
	if p.Error == "" {
		return fmt.Sprintf("%s compliant", p.Family)
	}
	return fmt.Sprintf("%s %s", p.Family, p.Error)
}
