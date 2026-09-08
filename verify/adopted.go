package verify

import "fmt"

// The apparatus, not the engine, has been where the defects were: a metric
// that priced a false assertion as a miss, an evaluation set sharing font
// families with the models' training, a sweep whose overrides never reached
// the options they named, and constants recorded as adopted that were never
// written into the binary. The last went unnoticed from step 10c to step
// 14d, four amendments during which the doc described an engine that was
// not running.
//
// Adopted is the one list of what this build has adopted. A test reads the
// live value out of the engine and requires each to equal what is written
// here, so a value recorded and not shipped fails the build rather than the
// reader.
//
// Step 19a emptied most of this list along with the mechanism it belonged
// to: the separation bounds, the region grouping, the shape classes, the
// coverage bound, the two models' framings and every radius fitted to bit
// codes went with the glyph-cutting engine. What remains belongs to the
// decision layer, and it is refitted over recognised text in step 19c.

// Constant is one adopted value: what it is called in the doc and in the
// sweep, what it was set to, which step set it, and where it lives.
type Constant struct {
	Name  string  // the sweep's name for it
	Value float64 // the value the engine must run at
	Step  string  // the step that adopted it
	Where string  // the field that carries it
}

// Adopted is every constant the build has adopted, with the step that
// adopted it. A constant present here and different in the code is a defect.
var Adopted = []Constant{
	{"radius", 0.14, "25a", "Options.Radius"},
	{"numeric_radius", 0.12, "19c", "Options.NumericRadius"},
	{"tie", 0.15, "19c", "Options.TieMargin"},
	{"min_confidence", 0.5, "19c, measured insensitive", "Options.MinConfidence"},
	{"read_max_side", 1600, "20c", "Options.MaxSide"},
	{"read_box_thresh", 0.3, "20c", "Options.BoxThresh"},
	{"read_unclip", 1.6, "20c", "Options.Unclip"},
	{"read_turned", 1, "21d, made conditional at 24a", "Options.Turned"},
}

// TuneNames is every constant a sweep may set.
var TuneNames = []string{"radius", "numeric_radius", "tie", "min_confidence", "read_max_side", "read_box_thresh", "read_unclip", "read_turned"}

// applyOptions writes the overrides a sweep asked for.
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
	set("radius", func(v float64) { o.Radius = v })
	set("numeric_radius", func(v float64) { o.NumericRadius = v })
	set("tie", func(v float64) { o.TieMargin = v })
	set("min_confidence", func(v float64) { o.MinConfidence = v })
	set("read_max_side", func(v float64) { o.MaxSide = v })
	set("read_box_thresh", func(v float64) { o.BoxThresh = v })
	set("read_unclip", func(v float64) { o.Unclip = v })
	set("read_turned", func(v float64) { o.Turned = v })
	return o
}

// Live reads the value the engine actually runs at for one adopted
// constant. The lookup is by the field it names, so a constant renamed in
// the code and not here fails rather than passing quietly.
func Live(c Constant) (float64, error) { return liveIn(c, (Options{}).withDefaults()) }

// liveIn reads an adopted constant out of the options given, which is how
// the override test reads the value a sweep set.
func liveIn(c Constant, o Options) (float64, error) {
	switch c.Where {
	case "Options.Radius":
		return o.Radius, nil
	case "Options.NumericRadius":
		return o.NumericRadius, nil
	case "Options.TieMargin":
		return o.TieMargin, nil
	case "Options.MinConfidence":
		return o.MinConfidence, nil
	case "Options.MaxSide":
		return o.MaxSide, nil
	case "Options.BoxThresh":
		return o.BoxThresh, nil
	case "Options.Unclip":
		return o.Unclip, nil
	case "Options.Turned":
		return o.Turned, nil
	}
	return 0, fmt.Errorf("no live value known for %q", c.Where)
}
