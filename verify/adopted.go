package verify

import (
	"fmt"

	"treasury/internal/alphabet"
	"treasury/internal/digits"
	"treasury/internal/preprocess"
	"treasury/internal/region"
)

// The apparatus, not the engine, has been where the defects were: a metric
// that priced a false assertion as a miss, an evaluation set sharing font
// families with the models' training, a sweep whose overrides never reached
// the options they named, and constants recorded as adopted that were never
// written into the binary. The last of those went unnoticed from step 10c to
// step 14d, four amendments during which the doc described an engine that
// was not running.
//
// Adopted is the one list of what this build has adopted. A test reads the
// live value out of the engine and out of the packages the constants live in
// and requires each to equal what is written here, so a value recorded and
// not shipped fails the build rather than the reader.

// Constant is one adopted value: what it is called in the doc and in the
// sweep, what it was set to, which step set it, and where it lives.
type Constant struct {
	Name  string  // the sweep's name for it, where it has one
	Value float64 // the value the engine must run at
	Step  string  // the step that adopted it
	Where string  // the field or variable that carries it
}

// Adopted is every constant the build has adopted, with the step that
// adopted it. A constant absent from this list is one nothing has ever
// tuned; a constant present here and different in the code is a defect.
var Adopted = []Constant{
	// The engine's own options.
	{"free_radius", 0.12, "10c, re-measured and kept at 14d and 16a", "Options.LearnedRadius"},
	{"numeric_radius", 0.15, "10c, kept at 14d", "Options.LearnedNumericRadius"},
	{"tie", 0.01, "7a", "Options.LearnedTie"},
	{"char_spread", 0.18, "10c", "Options.MaxCharSpread"},
	{"unexplained", 0.10, "6", "Options.MaxUnexplained"},
	{"violation", 0.15, "10c", "Options.ViolationFraction"},
	{"line_threshold", 0.08, "4", "Options.LineThreshold"},
	{"emphasis_gate", 0.15, "4", "Options.EmphasisGate"},
	{"heavy_factor", 1.25, "4", "Options.HeavyFactor"},
	{"min_glyphs", 3, "6", "Options.MinGlyphs"},
	{"min_coverage", 0.30, "12c", "Options.MinCoverage"},
	{"unit_bound", 2.2, "10c, shipped at 14d", "Engine.unitBound"},

	// Packages the claims path reaches into.
	{"union_gap", 0.15, "5b, kept at 14d", "alphabet.UnionGap"},
	{"class_bands", 1.0, "1", "alphabet.ClassBands"},
	{"block_floor", 0.4, "1", "alphabet.Options.MinGlyphs"},
	{"digit_frame_w", 2.0, "14d", "digits.FrameW"},
	{"digit_frame_h", 1.8, "14d", "digits.FrameH"},

	// The region proposer and the separation. Step 10c recorded five of
	// these at other values and never wrote them into the code; step 14d
	// re-swept them and left them where the code has been running.
	{"region_hgap", 1.5, "1, re-swept at 14d", "region.Params.HGapFrac"},
	{"region_vcenter", 0.6, "1, re-swept at 14d", "region.Params.VCenterFrac"},
	{"region_words", 5, "1", "region.Params.MaxWords"},
	{"region_fused", 1.6, "1", "region.Params.FusedFrac"},
	{"region_min_area", 4, "1, re-swept at 14d", "region.Params.MinArea"},
	{"sep_max_ratio", 0.55, "10a", "preprocess.SepParams.MaxRatio"},
	{"sep_max_spread", 0.62, "10a, re-swept at 14d", "preprocess.SepParams.MaxSpread"},
	{"sep_min_contrast", 0.10, "10a, re-swept at 14d", "preprocess.SepParams.MinContrast"},
	{"sep_solid", 1.6, "10a", "preprocess.SepParams.SolidHeight"},
	{"sep_min_run", 3, "10a", "preprocess.SepParams.MinRun"},
	{"sep_run_height", 0.45, "10a", "preprocess.SepParams.RunHeightFr"},
	{"sep_dark_ground", 90, "10a, re-swept at 14d", "preprocess.SepParams.DarkGround"},
	{"sep_light_ground", 165, "10a, re-swept at 14d", "preprocess.SepParams.LightGround"},
	{"sep_bar_field", 8, "10a", "preprocess.SepParams.BarField"},
	{"sep_min_height", 2, "10a", "preprocess.SepParams.MinHeight"},
	{"sep_max_height", 0.25, "10a", "preprocess.SepParams.MaxHeightFr"},
	{"sep_max_width", 0.6, "10a, re-swept at 14d", "preprocess.SepParams.MaxWidthFr"},
	{"sep_min_area", 4, "10a, re-swept at 14d", "preprocess.SepParams.MinArea"},
}

// Live reads the value the engine actually runs at for one adopted
// constant. The lookup is by the field it names, so a constant renamed in
// the code and not here fails rather than passing quietly.
func Live(c Constant) (float64, error) {
	return liveIn(c, (Options{}).withDefaults(), preprocess.Default(), region.Default(), alphabet.DefaultOptions())
}

// liveIn reads an adopted constant out of the parameters given, which is
// how the override test reads the value a sweep set.
func liveIn(c Constant, o Options, pp preprocess.Params, rp region.Params, ao alphabet.Options) (float64, error) {
	sp := pp.Sep
	switch c.Where {
	case "Options.LearnedRadius":
		return o.LearnedRadius, nil
	case "Options.LearnedNumericRadius":
		return o.LearnedNumericRadius, nil
	case "Options.LearnedTie":
		return o.LearnedTie, nil
	case "Options.MaxCharSpread":
		return o.MaxCharSpread, nil
	case "Options.MaxUnexplained":
		return o.MaxUnexplained, nil
	case "Options.ViolationFraction":
		return o.ViolationFraction, nil
	case "Options.LineThreshold":
		return o.LineThreshold, nil
	case "Options.EmphasisGate":
		return o.EmphasisGate, nil
	case "Options.HeavyFactor":
		return o.HeavyFactor, nil
	case "Options.MinGlyphs":
		return float64(o.MinGlyphs), nil
	case "Options.MinCoverage":
		return o.MinCoverage, nil
	case "Engine.unitBound":
		return (&Engine{opt: o}).unitBound(), nil
	case "alphabet.UnionGap":
		return alphabet.UnionGap, nil
	case "alphabet.ClassBands":
		return alphabet.ClassBands, nil
	case "alphabet.Options.MinGlyphs":
		return ao.MinGlyphs, nil
	case "digits.FrameW":
		return digits.FrameW, nil
	case "digits.FrameH":
		return digits.FrameH, nil
	case "region.Params.HGapFrac":
		return rp.HGapFrac, nil
	case "region.Params.VCenterFrac":
		return rp.VCenterFrac, nil
	case "region.Params.MaxWords":
		return float64(rp.MaxWords), nil
	case "region.Params.FusedFrac":
		return rp.FusedFrac, nil
	case "region.Params.MinArea":
		return float64(rp.MinArea), nil
	case "preprocess.SepParams.MaxRatio":
		return sp.MaxRatio, nil
	case "preprocess.SepParams.MaxSpread":
		return sp.MaxSpread, nil
	case "preprocess.SepParams.MinContrast":
		return sp.MinContrast, nil
	case "preprocess.SepParams.SolidHeight":
		return sp.SolidHeight, nil
	case "preprocess.SepParams.MinRun":
		return float64(sp.MinRun), nil
	case "preprocess.SepParams.RunHeightFr":
		return sp.RunHeightFr, nil
	case "preprocess.SepParams.DarkGround":
		return sp.DarkGround, nil
	case "preprocess.SepParams.LightGround":
		return sp.LightGround, nil
	case "preprocess.SepParams.BarField":
		return float64(sp.BarField), nil
	case "preprocess.SepParams.MinHeight":
		return float64(sp.MinHeight), nil
	case "preprocess.SepParams.MaxHeightFr":
		return sp.MaxHeightFr, nil
	case "preprocess.SepParams.MaxWidthFr":
		return sp.MaxWidthFr, nil
	case "preprocess.SepParams.MinArea":
		return float64(sp.MinArea), nil
	}
	return 0, fmt.Errorf("no live value known for %q", c.Where)
}

// restoreDigits puts the digit frame back to its adopted size after a sweep
// has written to it; it is a package variable because the frame is cut
// where no options struct reaches.
func restoreDigits() {
	for _, c := range Adopted {
		switch c.Where {
		case "digits.FrameW":
			digits.FrameW = c.Value
		case "digits.FrameH":
			digits.FrameH = c.Value
		}
	}
}
