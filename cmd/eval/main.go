// Command eval runs the engine over a generated set and reports the table
// of the approach doc: per claim, precision and recall of VERIFIED against
// what was printed, the share of REVIEW, error detection, and latency.
//
//	eval -set synth [-encoders dual,pos16] [-workers 8] [-tune]
//
// A set drawn from a face the models learned from reports their training
// data back as accuracy, and one drawn from a bundled face matches
// synthesized glyphs by the face that synthesized them; a set naming any
// family outside the evaluation partition is refused rather than evaluated.
// Records of every verdict's evidence are
// written to records.json so that -tune can sweep radii and the tie margin
// without re-running the engine: half A (even labels) picks the values,
// half B (odd labels) is reported.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"treasury/internal/fontset"
	"treasury/ttb"
	"treasury/verify"
)

var tuneSet = flag.String("tune-set", "", "override constants by name, as name=value pairs separated by commas")

func main() {
	set := flag.String("set", "synth", "directory written by gen set")
	encoders := flag.String("encoders", "dual", "comma-separated glyph encoders to compare")
	workers := flag.Int("workers", runtime.NumCPU(), "parallel verifications")
	tune := flag.Bool("tune", false, "sweep radii and tie margin on half A of the records, report on half B")
	limit := flag.Int("n", 0, "evaluate only the first n labels")
	half := flag.String("half", "", "evaluate only half A (even labels) or B (odd labels)")
	flag.Parse()
	if err := run(*set, strings.Split(*encoders, ","), *workers, *tune, *limit, *half); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

// Truth mirrors gen's per-label truth file.
type Truth struct {
	Printed ttb.Printed     `json:"printed"`
	Aug     json.RawMessage `json:"aug,omitempty"`
}

// Record is one label's outcome under one encoder.
type Record struct {
	Label     string           `json:"label"`
	Printed   ttb.Printed      `json:"printed"`
	Augmented bool             `json:"augmented"`
	Latency   time.Duration    `json:"latency"`
	Reason    string           `json:"reason,omitempty"`
	Claims    []verify.Verdict `json:"claims"`
	Reference []verify.Verdict `json:"reference"`
	Emphasis  []verify.Verdict `json:"emphasis"`
}

func run(dir string, encoders []string, workers int, tune bool, limit int, half string) error {
	leaked, err := checkLeak(dir)
	if err != nil {
		return err
	}
	if leaked != "" {
		return fmt.Errorf("set %s draws on faces the models trained on (%s); it cannot be evaluated", dir, leaked)
	}
	labels, err := filepath.Glob(filepath.Join(dir, "*.truth.json"))
	if err != nil {
		return err
	}
	sort.Strings(labels)
	if half != "" {
		var keep []string
		for _, l := range labels {
			var n int
			fmt.Sscanf(filepath.Base(l), "%d", &n)
			if (n%2 == 0) == (strings.EqualFold(half, "A")) {
				keep = append(keep, l)
			}
		}
		labels = keep
	}
	if limit > 0 && limit < len(labels) {
		labels = labels[:limit]
	}
	eng, err := verify.New(verify.Options{Tune: tuneMap(*tuneSet)})
	if err != nil {
		return err
	}
	records, err := evaluate(eng, labels, workers)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "records.json"), records); err != nil {
		return err
	}
	var out strings.Builder
	out.WriteString(table(records))
	fmt.Print(out.String())
	return os.WriteFile(filepath.Join(dir, "table.md"), []byte(out.String()), 0o644)
}

// checkLeak returns the families of a set that may not appear in an
// evaluated image: everything outside the evaluation partition, which is
// every face the models may have learned from and every bundled face that
// synthesizes the characters a label never taught.
func checkLeak(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", fmt.Errorf("no manifest in %s: %w", dir, err)
	}
	var m struct {
		Families []string `json:"families"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return "", err
	}
	return strings.Join(fontset.Leaked(m.Families), ", "), nil
}

func evaluate(eng *verify.Engine, labels []string, workers int) ([]Record, error) {
	type job struct {
		i    int
		path string
	}
	jobs := make(chan job)
	results := make([]Record, len(labels))
	errs := make([]error, len(labels))
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results[j.i], errs[j.i] = one(eng, j.path)
			}
		}()
	}
	start := time.Now()
	for i, p := range labels {
		jobs <- job{i, p}
		if (i+1)%25 == 0 {
			fmt.Fprintf(os.Stderr, "%d/%d labels (%.0fs)\n", i+1, len(labels), time.Since(start).Seconds())
		}
	}
	close(jobs)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("%s: %w", labels[i], err)
		}
	}
	return results, nil
}

func one(eng *verify.Engine, truthPath string) (Record, error) {
	base := strings.TrimSuffix(truthPath, ".truth.json")
	var t Truth
	if err := readJSON(truthPath, &t); err != nil {
		return Record{}, err
	}
	var exp ttb.Expected
	if err := readJSON(base+".json", &exp); err != nil {
		return Record{}, err
	}
	f, err := os.Open(base + ".png")
	if err != nil {
		return Record{}, err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return Record{}, err
	}
	refs, claims := ttb.Inputs(exp)
	start := time.Now()
	res, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		return Record{}, err
	}
	rec := Record{
		Label: filepath.Base(base), Printed: t.Printed, Augmented: len(t.Aug) > 0,
		Latency: time.Since(start), Reason: res.Reason, Claims: res.Claims, Reference: res.Reference, Emphasis: res.Emphasis,
	}
	return rec, nil
}

// want is what the printed label implies for a claim: "correct" (the
// application's value is on the label), "wrong" (another value is), or
// "missing" (the claim is not printed).
func want(p ttb.Printed, claim string) string {
	// Step 12a: where the label's own text has been transcribed, that is
	// the truth. A claim the label does not carry in the filed form is
	// missing from the label whatever the registry filed, so a NOT_FOUND
	// on it is a correct absence report and a VERIFIED is a false
	// assertion.
	if p.Carried != nil {
		if v, ok := p.Carried[claim]; ok && v != "=" {
			return "missing"
		}
	}
	switch claim {
	case "abv":
		switch {
		case p.ABV == 0:
			return "missing"
		case p.Error == "wrong_abv":
			return "wrong"
		}
	case "net":
		switch {
		case p.NetML == 0:
			return "missing"
		case p.Error == "wrong_net":
			return "wrong"
		}
	case "class":
		switch {
		case p.Class == "":
			return "missing"
		case p.Error == "wrong_class":
			return "wrong"
		}
	case "brand":
		if p.Error == "wrong_brand" {
			return "wrong"
		}
	}
	return "correct"
}

// tally accumulates verdicts against truth for one claim name.
type tally struct {
	n, tp, fp, fn, review, mismatchHit, mismatchMiss, notFoundHit int
}

func (t *tally) add(status verify.Status, w string) {
	t.n++
	switch status {
	case verify.Verified:
		if w == "correct" {
			t.tp++
		} else {
			t.fp++
		}
	case verify.Review:
		t.review++
		if w == "correct" {
			t.fn++
		}
	case verify.Mismatch:
		// A mismatch on a correct label asserts a wrong value: a false
		// positive, not a miss. Counting it as a miss let a tuner widen the
		// numeric radius into false mismatches without a penalty.
		if w == "wrong" {
			t.mismatchHit++
		} else if w == "correct" {
			t.fp++
		}
	default:
		if w == "correct" {
			t.fn++
		} else if w == "missing" {
			t.notFoundHit++
		}
	}
	if w == "wrong" && status != verify.Mismatch {
		t.mismatchMiss++
	}
}

func (t tally) precision() float64 {
	if t.tp+t.fp == 0 {
		return 0
	}
	return float64(t.tp) / float64(t.tp+t.fp)
}

func (t tally) recall() float64 {
	if t.tp+t.fn == 0 {
		return 0
	}
	return float64(t.tp) / float64(t.tp+t.fn)
}

// table renders the report for one encoder; with o set, claim verdicts are
// re-derived from evidence under those thresholds.
// crossFaceAndConventions reports recall split by whether a claim's face is
// the warning's (the cross-face gap) and, per convention the real labels
// showed, how many labels carry it and how they fare.
func crossFaceAndConventions(recs []Record) string {
	type split struct{ same, cross tally }
	splits := map[string]*split{}
	order := []string{"class", "producer_1", "producer_2", "origin", "abv", "net"}
	for _, name := range order {
		splits[name] = &split{}
	}
	anyFaces := false
	for _, r := range recs {
		if r.Printed.ClaimFaces == nil {
			continue
		}
		anyFaces = true
		for _, v := range r.Claims {
			sp, ok := splits[v.Claim]
			if !ok {
				continue
			}
			w := want(r.Printed, v.Claim)
			if w != "correct" {
				continue
			}
			st := v.Status
			base := strings.TrimSuffix(strings.TrimSuffix(v.Claim, "_1"), "_2")
			if r.Printed.ClaimFaces[base] == r.Printed.BodyFace {
				sp.same.add(st, w)
			} else {
				sp.cross.add(st, w)
			}
		}
	}
	var b strings.Builder
	if anyFaces {
		fmt.Fprintf(&b, "Cross-face gap (correct claims only; same-face = set in the warning's face):\n\n| claim | same-face n | same-face recall | cross-face n | cross-face recall |\n|---|---|---|---|---|\n")
		for _, name := range order {
			sp := splits[name]
			fmt.Fprintf(&b, "| %s | %d | %.2f | %d | %.2f |\n", name, sp.same.n, sp.same.recall(), sp.cross.n, sp.cross.recall())
		}
		b.WriteString("\n")
	}
	type conv struct {
		name string
		has  func(ttb.Printed) bool
	}
	convs := []conv{
		{"warning in capitals", func(p ttb.Printed) bool { return p.WarningCaps }},
		{"light on dark", func(p ttb.Printed) bool { return p.Inverted }},
		{"vertical warning", func(p ttb.Printed) bool { return p.Vertical }},
		{"crowded warning", func(p ttb.Printed) bool { return p.Crowded }},
		{"none of these", func(p ttb.Printed) bool { return !p.WarningCaps && !p.Inverted && !p.Vertical && !p.Crowded }},
	}
	fmt.Fprintf(&b, "Convention coverage (free-text recall over brand, class, producer, origin):\n\n| convention | labels | nothing read | free-text recall |\n|---|---|---|---|\n")
	for _, c := range convs {
		n, noAlpha := 0, 0
		var t tally
		for _, r := range recs {
			if !c.has(r.Printed) {
				continue
			}
			n++
			if r.Reason == "no_alphabet" {
				noAlpha++
			}
			for _, v := range r.Claims {
				switch v.Claim {
				case "brand", "class", "producer_1", "producer_2", "origin":
					if w := want(r.Printed, v.Claim); w == "correct" {
						st := v.Status
						t.add(st, w)
					}
				}
			}
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %.2f |\n", c.name, n, noAlpha, t.recall())
	}
	b.WriteString("\n")
	return b.String()
}

func table(recs []Record) string {
	claims := []string{"brand", "class", "producer_1", "producer_2", "origin", "abv", "net"}
	tallies := map[string]*tally{}
	for _, c := range claims {
		tallies[c] = &tally{}
	}
	var latencies []float64
	noRead := 0
	rowErr := tally{}       // wording and title-case errors: any row MISMATCH
	rowClean := tally{}     // compliant labels: every row VERIFIED
	emph := tally{}         // regular-header errors detected; compliant headers verified
	brandDisplay := tally{} // brand set in a face outside the body family
	for _, r := range recs {
		latencies = append(latencies, r.Latency.Seconds())
		if r.Reason == "no_alphabet" {
			noRead++
		}
		bodyFamily := strings.Fields(r.Printed.BodyFace)
		brandFamily := strings.Fields(r.Printed.BrandFace)
		displayBrand := len(bodyFamily) > 0 && len(brandFamily) > 0 && bodyFamily[0] != brandFamily[0]
		for _, v := range r.Claims {
			t, ok := tallies[v.Claim]
			if !ok {
				continue
			}
			status := v.Status
			w := want(r.Printed, v.Claim)
			if v.Claim == "brand" && displayBrand {
				brandDisplay.add(status, w)
				continue
			}
			t.add(status, w)
		}
		anyRowFail := false
		allRowsOK := len(r.Reference) > 0
		for _, v := range r.Reference {
			if v.Status == verify.Mismatch {
				anyRowFail = true
			}
			if v.Status != verify.Verified {
				allRowsOK = false
			}
		}
		switch r.Printed.Error {
		case "wording", "title_header":
			rowErr.n++
			if anyRowFail {
				rowErr.tp++
			} else {
				rowErr.fn++
			}
		case "":
			rowClean.n++
			if allRowsOK {
				rowClean.tp++
			} else if anyRowFail {
				rowClean.fp++
			} else {
				rowClean.review++
			}
		}
		if len(r.Emphasis) > 0 {
			s := r.Emphasis[0].Status
			switch r.Printed.Error {
			case "regular_header":
				emph.n++
				if s == verify.Mismatch {
					emph.tp++
				} else {
					emph.fn++
				}
			case "":
				emph.n++
				if s == verify.Verified {
					emph.tp++
				} else {
					emph.fp++
				}
			}
		}
	}
	sort.Float64s(latencies)
	pct := func(p float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[min(len(latencies)-1, int(p*float64(len(latencies))))]
	}
	var b strings.Builder
	title := "## The set"
	fmt.Fprintf(&b, "%s\n\n%d labels, %d the reader found nothing on, latency median %.1fs p95 %.1fs\n\n", title, len(recs), noRead, pct(0.5), pct(0.95))
	b.WriteString("| claim | n | precision | recall | review | mismatch found | not found on missing |\n|---|---|---|---|---|---|---|\n")
	for _, c := range claims {
		t := tallies[c]
		fmt.Fprintf(&b, "| %s | %d | %.2f | %.2f | %.2f | %d/%d | %d |\n", c, t.n, t.precision(), t.recall(), float64(t.review)/math.Max(1, float64(t.n)), t.mismatchHit, t.mismatchHit+t.mismatchMiss, t.notFoundHit)
	}
	fmt.Fprintf(&b, "| brand (display face) | %d | %.2f | %.2f | %.2f | | |\n", brandDisplay.n, brandDisplay.precision(), brandDisplay.recall(), float64(brandDisplay.review)/math.Max(1, float64(brandDisplay.n)))
	fmt.Fprintf(&b, "\nReference rows: compliant labels with every row verified %d/%d (%d reviewed, %d failed); wording and title-case errors caught %d/%d.\n", rowClean.tp, rowClean.n, rowClean.review, rowClean.fp, rowErr.tp, rowErr.n)
	fmt.Fprintf(&b, "Emphasis: correct on %d/%d labels (compliant headers verified and regular-weight headers caught).\n\n", emph.tp, emph.n)
	b.WriteString(crossFaceAndConventions(recs))
	return b.String()
}

// f1 is the mean F1 of VERIFIED over the claims, with a penalty for wrong
// values that go unflagged.
// falseAssertions counts the verdicts that name something untrue under o:
// a claim verified on a label that prints another value, or a mismatch on a
// label that prints the right one.
// FalseAssertionCost is how many claims left unverified one false verdict
// is worth. The engine's verdicts carry its authority: a wrong value named
// on a correct label is a rejection the reader will act on, where a miss is
// work the reader was doing anyway.
const FalseAssertionCost = 10

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// tuneMap parses -tune-set.
func tuneMap(s string) map[string]float64 {
	if s == "" {
		return nil
	}
	out := map[string]float64{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			continue
		}
		out[strings.TrimSpace(k)] = f
	}
	return out
}
