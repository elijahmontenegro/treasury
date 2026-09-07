package verify

import (
	"treasury/internal/alphabet"
	"treasury/internal/digits"
	"treasury/internal/preprocess"
	"treasury/internal/region"
)

// Tuning by name.
//
// Every threshold in this engine was fitted to a corpus that did not model
// the population, and step 10c retunes them on one that does. A constant is
// swept by name rather than by editing it, so a sweep is reproducible and
// so the doc can report, for each, what it was, what it became, and what
// the change bought.
//
// The names below are the whole list of what step 10b recorded as carried
// over unexamined. A name absent from a run's Tune map keeps the value the
// engine ships.
const (
	TuneFreeRadius     = "free_radius"      // Options.LearnedRadius, 0.12
	TuneNumericRadius  = "numeric_radius"   // Options.LearnedNumericRadius, 0.15
	TuneTie            = "tie"              // Options.LearnedTie, 0.01
	TuneCharSpread     = "char_spread"      // Options.MaxCharSpread, 0.12
	TuneUnexplained    = "unexplained"      // Options.MaxUnexplained, 0.10
	TuneViolation      = "violation"        // Options.ViolationFraction, 0.10
	TuneLineThreshold  = "line_threshold"   // Options.LineThreshold, 0.08
	TuneEmphasisGate   = "emphasis_gate"    // Options.EmphasisGate, 0.15
	TuneHeavyFactor    = "heavy_factor"     // Options.HeavyFactor, 1.25
	TuneMinGlyphs      = "min_glyphs"       // Options.MinGlyphs, 3
	TuneUnitBound      = "unit_bound"       // the per-letter unit bound, 1.6 times the radius
	TuneBlockFloor     = "block_floor"      // alphabet.Options.MinGlyphs, 0.4 of the reference's characters
	TuneUnionGap       = "union_gap"        // alphabet's union gap, 0.15 x-heights
	TuneClassBands     = "class_bands"      // a scale on the shape classes' dead bands, 1.0
	TuneDigitFrameW    = "digit_frame_w"    // digits.FrameW, 1.6
	TuneDigitFrameH    = "digit_frame_h"    // digits.FrameH, 2.2
	TuneRegionHGap     = "region_hgap"      // region.Params.HGapFrac, 1.5
	TuneRegionVCenter  = "region_vcenter"   // region.Params.VCenterFrac, 0.6
	TuneRegionWords    = "region_words"     // region.Params.MaxWords, 5
	TuneRegionFused    = "region_fused"     // region.Params.FusedFrac, 1.6
	TuneRegionMinArea  = "region_min_area"  // region.Params.MinArea, 4
	TuneSepMaxRatio    = "sep_max_ratio"    // 0.55
	TuneSepMaxSpread   = "sep_max_spread"   // 0.62
	TuneSepMinContrast = "sep_min_contrast" // 0.10
	TuneSepSolid       = "sep_solid"        // 1.6
	TuneSepMinRun      = "sep_min_run"      // 3
	TuneSepRunHeight   = "sep_run_height"   // 0.45
	TuneSepDarkGround  = "sep_dark_ground"  // 90
	TuneSepLightGround = "sep_light_ground" // 165
	TuneSepBarField    = "sep_bar_field"    // 8
	TuneSepMinHeight   = "sep_min_height"   // 2
	TuneSepMaxHeight   = "sep_max_height"   // 0.25
	TuneSepMaxWidth    = "sep_max_width"    // 0.6
	TuneSepMinArea     = "sep_min_area"     // 4
	TuneMinCoverage    = "min_coverage"     // 0.30, added in 12c
)

// TuneNames is every constant a sweep may set, in the order the doc reports
// them.
var TuneNames = []string{
	TuneFreeRadius, TuneNumericRadius, TuneTie, TuneCharSpread, TuneUnexplained,
	TuneViolation, TuneLineThreshold, TuneEmphasisGate, TuneHeavyFactor, TuneMinGlyphs,
	TuneUnitBound, TuneBlockFloor, TuneUnionGap, TuneClassBands, TuneDigitFrameW, TuneDigitFrameH,
	TuneRegionHGap, TuneRegionVCenter, TuneRegionWords, TuneRegionFused, TuneRegionMinArea,
	TuneSepMaxRatio, TuneSepMaxSpread, TuneSepMinContrast, TuneSepSolid, TuneSepMinRun,
	TuneSepRunHeight, TuneSepDarkGround, TuneSepLightGround, TuneSepBarField,
	TuneSepMinHeight, TuneSepMaxHeight, TuneSepMaxWidth, TuneSepMinArea, TuneMinCoverage,
}

// value returns the override for a name, and whether it was given.
func (e *Engine) tuned(name string) (float64, bool) {
	if e.opt.Tune == nil {
		return 0, false
	}
	v, ok := e.opt.Tune[name]
	return v, ok
}

// applyTune writes the overrides that live outside the engine's own
// options: the region proposer, the separation, the alphabet and the digit
// frame. It returns the parameters the engine should use.
func applyTune(t map[string]float64, pp preprocess.Params, rp region.Params, ao alphabet.Options) (preprocess.Params, region.Params, alphabet.Options) {
	if t == nil {
		return pp, rp, ao
	}
	set := func(name string, f func(v float64)) {
		if v, ok := t[name]; ok {
			f(v)
		}
	}
	set(TuneRegionHGap, func(v float64) { rp.HGapFrac = v })
	set(TuneRegionVCenter, func(v float64) { rp.VCenterFrac = v })
	set(TuneRegionWords, func(v float64) { rp.MaxWords = int(v) })
	set(TuneRegionFused, func(v float64) { rp.FusedFrac = v })
	set(TuneRegionMinArea, func(v float64) { rp.MinArea = int(v) })
	set(TuneSepMaxRatio, func(v float64) { pp.Sep.MaxRatio = v })
	set(TuneSepMaxSpread, func(v float64) { pp.Sep.MaxSpread = v })
	set(TuneSepMinContrast, func(v float64) { pp.Sep.MinContrast = v })
	set(TuneSepSolid, func(v float64) { pp.Sep.SolidHeight = v })
	set(TuneSepMinRun, func(v float64) { pp.Sep.MinRun = int(v) })
	set(TuneSepRunHeight, func(v float64) { pp.Sep.RunHeightFr = v })
	set(TuneSepDarkGround, func(v float64) { pp.Sep.DarkGround = v })
	set(TuneSepLightGround, func(v float64) { pp.Sep.LightGround = v })
	set(TuneSepBarField, func(v float64) { pp.Sep.BarField = int(v) })
	set(TuneSepMinHeight, func(v float64) { pp.Sep.MinHeight = int(v) })
	set(TuneSepMaxHeight, func(v float64) { pp.Sep.MaxHeightFr = v })
	set(TuneSepMaxWidth, func(v float64) { pp.Sep.MaxWidthFr = v })
	set(TuneSepMinArea, func(v float64) { pp.Sep.MinArea = int(v) })
	set(TuneBlockFloor, func(v float64) { ao.MinGlyphs = v })
	set(TuneCharSpread, func(v float64) { ao.MaxCharSpread = v })
	// These live as package variables because they are read where no
	// options struct reaches; a sweep sets them for the process.
	set(TuneUnionGap, func(v float64) { alphabet.UnionGap = v })
	set(TuneClassBands, func(v float64) { alphabet.ClassBands = v })
	set(TuneDigitFrameW, func(v float64) { digits.FrameW = v })
	set(TuneDigitFrameH, func(v float64) { digits.FrameH = v })
	return pp, rp, ao
}

// applyOptions writes the overrides that live in the engine's own options.
func applyOptions(o Options) Options {
	t := o.Tune
	if t == nil {
		return o
	}
	set := func(name string, f func(v float64)) {
		if v, ok := t[name]; ok {
			f(v)
		}
	}
	set(TuneFreeRadius, func(v float64) { o.LearnedRadius = v })
	set(TuneNumericRadius, func(v float64) { o.LearnedNumericRadius = v })
	set(TuneTie, func(v float64) { o.LearnedTie = v })
	set(TuneCharSpread, func(v float64) { o.MaxCharSpread = v })
	set(TuneUnexplained, func(v float64) { o.MaxUnexplained = v })
	set(TuneViolation, func(v float64) { o.ViolationFraction = v })
	set(TuneLineThreshold, func(v float64) { o.LineThreshold = v })
	set(TuneEmphasisGate, func(v float64) { o.EmphasisGate = v })
	set(TuneHeavyFactor, func(v float64) { o.HeavyFactor = v })
	set(TuneMinGlyphs, func(v float64) { o.MinGlyphs = int(v) })
	set(TuneUnitBound, func(v float64) { o.UnitBound = v })
	set(TuneMinCoverage, func(v float64) { o.MinCoverage = v })
	return o
}
