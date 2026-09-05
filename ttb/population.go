package ttb

import "image"

// What the population is made of.
//
// Every number here was measured on the fifty real label approvals in
// real2/, not chosen. The generator draws from these rather than from
// assumption, and each field records what measured it. The measurement is
// `separate -stats` over the fifty, aggregated in out/population.jsonl;
// the conventions are the transcription recorded per label in the truth
// files.
//
// The old corpus differed from this population in ways nobody had checked:
// its labels were about a thousand pixels across where these are two
// thousand and more, so its type was two to three times larger relative to
// the label; all of its text was dark on white where a third of the marks
// here are light on dark; its ground was blank where forty-nine of fifty
// real labels put text over something structured; and none of it had a
// barcode, where half of these do.

// Sizes are the fifty labels' own pixel dimensions, as served by the
// registry. A generated label takes one of them, so the corpus has the
// population's resolutions and aspect ratios rather than a guess at them.
var Sizes = [][2]int{
	{1934, 3217}, {2504, 2345}, {3707, 2371}, {1566, 3371}, {1562, 3389},
	{2227, 1856}, {1730, 976}, {759, 2362}, {698, 2101}, {698, 2101},
	{698, 2437}, {1280, 786}, {2227, 1856}, {2227, 1856}, {1330, 769},
	{1630, 934}, {1602, 934}, {2568, 2192}, {2227, 1856}, {2227, 1856},
	{2227, 1856}, {1707, 1405}, {1701, 1407}, {1711, 1299}, {1711, 1401},
	{307, 1653}, {2201, 5093}, {1680, 2520}, {582, 1668}, {555, 1696},
	{554, 1668}, {552, 1667}, {1355, 2333}, {2480, 1205}, {1280, 2055},
	{1430, 1863}, {880, 1387}, {1537, 2811}, {2561, 5391}, {1734, 1883},
	{1166, 1707}, {1122, 2584}, {1748, 2371}, {830, 680}, {1483, 2440},
	{1350, 2188}, {1608, 2492}, {1513, 2305}, {1513, 2623}, {1949, 2485},
}

// Population is the rate or the range of each thing the fifty showed.
type Population struct {
	// Conventions, from the per-label transcription.
	Capitals float64 // 34 of 50 set the warning in capitals
	// 45 of 50 labels carry text that is light on a dark ground, measured
	// as a share of kept marks above 0.15; the transcription counted 23,
	// which was the front panel only. The measurement drives the corpus.
	LightOnDark float64
	Crowded     float64 // 33 of 50 crowd the warning with other text of its size
	Vertical    float64 // 14 of 50 set the warning along a side
	Upsidedown  float64 // 1 of 50 is printed upside down
	DisplayFace float64 // 41 of 50 set the brand in a display face

	// The artwork, from `separate -stats`.
	Barcode  float64 // 24 of 50 carry a barcode
	Rules    float64 // 21 of 50 have rules or borders separation rejected
	Pattern  float64 // 49 of 50 put text over something structured, ring variation above 0.06
	Gradient float64
	Texture  float64

	// Type size as a fraction of the label's longer side: the median glyph
	// stood 0.0059 of it, a tenth of the labels at 0.0038 and a tenth at
	// 0.0106. The old corpus drew type at two to three times that.
	GlyphFracLow, GlyphFracHigh float64

	// The ground under text, and the contrast the text keeps against it.
	// Dark ink sits on a ground of 209 in the median label, light ink on
	// one of 106; the ink's own grey stands 0.35 of the range from its
	// ring, a tenth of the labels at 0.22 and a tenth at 0.50, measured
	// after the engine's resize.
	DarkGround, LightGround   uint8
	ContrastLow, ContrastHigh float64

	// A third of the marks on a label are light on dark.
	LightShare float64
}

// Measured is the population as measured on the fifty.
func Measured() Population {
	return Population{
		Capitals: 34.0 / 50, LightOnDark: 45.0 / 50, Crowded: 33.0 / 50,
		Vertical: 14.0 / 50, Upsidedown: 1.0 / 50, DisplayFace: 41.0 / 50,
		Barcode: 24.0 / 50, Rules: 21.0 / 50, Pattern: 49.0 / 50,
		Gradient: 0.35, Texture: 0.5,
		GlyphFracLow: 0.0038, GlyphFracHigh: 0.0106,
		DarkGround: 209, LightGround: 106,
		ContrastLow: 0.22, ContrastHigh: 0.50,
		LightShare: 0.34,
	}
}

// Size returns one of the population's own label sizes.
func Size(i int) (w, h int) {
	s := Sizes[i%len(Sizes)]
	return s[0], s[1]
}

// Rect is a convenience for the artwork's rectangles.
func Rect(x0, y0, x1, y1 int) image.Rectangle { return image.Rect(x0, y0, x1, y1) }
