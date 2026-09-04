package ttb

import (
	"fmt"
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
	v := Variant{BodyFace: body.Name, HeavyFace: heavy.Name, BrandFace: brandFace.Name,
		ClaimFaces: claimFaces, WarningCaps: caps, Vertical: vertical, Crowded: crowded}
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

// layout places the printed label with modest randomness in size and position.
func layout(rng *rand.Rand, pr Printed, v Variant) synth.Document {
	w := 1000 + rng.Intn(400)
	h := 1400 + rng.Intn(400)
	cx := w / 2
	if v.Vertical {
		cx = (w + 260) / 2 // the warning takes the left side
	}
	doc := synth.Document{W: w, H: h}
	face := func(claim string) string {
		if f, ok := v.ClaimFaces[claim]; ok && f != "" {
			return f
		}
		return v.BodyFace
	}
	item := func(text, face string, px float64, y int, claim string) {
		if text == "" {
			return
		}
		doc.Items = append(doc.Items, synth.Item{Text: text, Face: face, Px: px, X: cx, Y: y, Center: true, Claim: claim})
	}
	y := 120 + rng.Intn(80)
	item(pr.Brand, v.BrandFace, 60+float64(rng.Intn(30)), y, "brand")
	y += 90 + rng.Intn(40)
	if rng.Intn(2) == 0 {
		item([]string{"SMALL BATCH", "ESTATE BOTTLED", "LIMITED RELEASE", "CRAFT BREWED"}[rng.Intn(4)], v.HeavyFace, 30+float64(rng.Intn(10)), y, "")
		y += 70 + rng.Intn(30)
	}
	item(pr.Class, face("class"), 36+float64(rng.Intn(12)), y, "class")
	y += 70 + rng.Intn(30)
	if rng.Intn(2) == 0 {
		item([]string{"AGED 8 YEARS", "Batch No. 12 - Est. 1887", "Distilled from grain", "Vintage 2019", "Unfiltered"}[rng.Intn(5)], v.BodyFace, 26+float64(rng.Intn(8)), y, "")
		y += 60 + rng.Intn(30)
	}
	px := 28 + float64(rng.Intn(10))
	if pr.ABVText != "" && pr.NetText != "" && rng.Intn(2) == 0 {
		left := w / 10
		if v.Vertical {
			// Right of the warning along the left side, which the row's
			// old start overprinted: a "4.5%" lost its "4." under it.
			left = 300
		}
		doc.Items = append(doc.Items,
			synth.Item{Text: pr.ABVText, Face: face("abv"), Px: px, X: left, Y: y, Claim: "abv"},
			synth.Item{Text: pr.NetText, Face: face("net"), Px: px, X: w * 3 / 4, Y: y, Claim: "net"},
		)
		y += 70 + rng.Intn(30)
	} else {
		item(pr.ABVText, face("abv"), px, y, "abv")
		if pr.ABVText != "" {
			y += 50 + rng.Intn(20)
		}
		item(pr.NetText, face("net"), px, y, "net")
		if pr.NetText != "" {
			y += 70 + rng.Intn(30)
		}
	}
	ppx := 24 + float64(rng.Intn(8))
	for _, line := range pr.Producer {
		item(line, face("producer"), ppx, y, "producer")
		y += int(ppx * 1.4)
	}
	item(pr.Origin, face("origin"), ppx, y, "origin")
	y += 90 + rng.Intn(60)
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
	wpx := 20 + float64(rng.Intn(7))
	leading := 1.25 + rng.Float64()*0.2
	pitch := int(math.Round(wpx * leading))
	crowd := []string{
		"INGREDIENTS: WATER, BARLEY MALT, HOPS, YEAST.", "Imported by Ridge Imports LLC, 8859 NW 102nd Ct, Doral, FL 33178",
		"Contains sulfites. Return for refund where applicable.", "Produced and bottled under license. Store in a cool dry place.",
	}
	if v.Vertical {
		// Along the left side, reading upward, as labels set it.
		width := int(float64(h) * (0.55 + rng.Float64()*0.1))
		doc.Blocks = append(doc.Blocks, synth.Block{
			Text: text, Heavy: []synth.Span{{Start: 0, End: HeaderLen}}, Face: v.BodyFace, HeavyFace: v.HeavyFace,
			Px: wpx, X: 40, Y: (h - width) / 2, Width: width, Leading: leading, Claim: "reference", Quarters: 3,
		})
		if y+80 < h {
			item([]string{"Please drink responsibly.", "Enjoy responsibly.", "Keep refrigerated."}[rng.Intn(3)], v.BodyFace, 22+float64(rng.Intn(6)), y+40, "")
		}
		return doc
	}
	width := int(float64(w) * (0.7 + rng.Float64()*0.15))
	left := (w - width) / 2
	if v.Crowded {
		for range 1 + rng.Intn(2) {
			doc.Items = append(doc.Items, synth.Item{Text: crowd[rng.Intn(len(crowd))], Face: v.BodyFace, Px: wpx, X: left, Y: y, Claim: ""})
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
	})
	y += pitch * 6
	if v.Crowded {
		doc.Items = append(doc.Items, synth.Item{Text: crowd[rng.Intn(len(crowd))], Face: v.BodyFace, Px: wpx, X: left, Y: y, Claim: ""})
		y += pitch
	}
	if y+80 < h {
		item([]string{"Please drink responsibly.", "Enjoy responsibly.", "Keep refrigerated."}[rng.Intn(3)], v.BodyFace, 22+float64(rng.Intn(6)), y+40, "")
	}
	return doc
}

// String renders the error for tables.
func (p Printed) String() string {
	if p.Error == "" {
		return fmt.Sprintf("%s compliant", p.Family)
	}
	return fmt.Sprintf("%s %s", p.Family, p.Error)
}
