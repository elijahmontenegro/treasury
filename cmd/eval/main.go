// Command eval runs the engine over a generated set and reports the table
// of the approach doc: per claim, precision and recall of VERIFIED against
// what was printed, the share of REVIEW, error detection, and latency.
//
//	eval -set synth [-encoders dual,pos16] [-workers 8] [-allow-leak] [-tune]
//
// A set drawn from the bundled faces would match synthesized glyphs for the
// wrong reason; eval refuses such a set unless -allow-leak is given, and
// then stamps the table LEAKED. Records of every verdict's evidence are
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
	"strings"
	"sync"
	"time"

	"treasury/internal/alphabet"
	"treasury/internal/render"
	"treasury/ttb"
	"treasury/verify"
)

func main() {
	set := flag.String("set", "synth", "directory written by gen set")
	encoders := flag.String("encoders", "dual", "comma-separated glyph encoders to compare")
	workers := flag.Int("workers", runtime.NumCPU(), "parallel verifications")
	allowLeak := flag.Bool("allow-leak", false, "evaluate a set drawn from the bundled faces anyway")
	tune := flag.Bool("tune", false, "sweep radii and tie margin on half A of the records, report on half B")
	limit := flag.Int("n", 0, "evaluate only the first n labels")
	half := flag.String("half", "", "evaluate only half A (even labels) or B (odd labels)")
	flag.Parse()
	if err := run(*set, strings.Split(*encoders, ","), *workers, *allowLeak, *tune, *limit, *half); err != nil {
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
	Label     string                 `json:"label"`
	Encoder   string                 `json:"encoder"`
	Printed   ttb.Printed            `json:"printed"`
	Augmented bool                   `json:"augmented"`
	Latency   time.Duration          `json:"latency"`
	Reason    string                 `json:"reason,omitempty"`
	Orient    string                 `json:"orientation,omitempty"`
	Casing    string                 `json:"reference_casing,omitempty"`
	Alphabet  *verify.AlphabetReport `json:"alphabet,omitempty"`
	Claims    []verify.Verdict       `json:"claims"`
	Reference []verify.Verdict       `json:"reference"`
	Emphasis  []verify.Verdict       `json:"emphasis"`
}

func run(dir string, encoders []string, workers int, allowLeak, tune bool, limit int, half string) error {
	leaked, err := checkLeak(dir)
	if err != nil {
		return err
	}
	if leaked != "" && !allowLeak {
		return fmt.Errorf("set %s draws on bundled faces (%s); pass -allow-leak to evaluate it anyway", dir, leaked)
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
	var records []Record
	for _, enc := range encoders {
		eng, err := verify.New(verify.Options{Encoder: enc})
		if err != nil {
			return err
		}
		recs, err := evaluate(eng, enc, labels, workers)
		if err != nil {
			return err
		}
		records = append(records, recs...)
	}
	if err := writeJSON(filepath.Join(dir, "records.json"), records); err != nil {
		return err
	}
	var out strings.Builder
	if leaked != "" {
		out.WriteString("# LEAKED: this set draws on bundled faces (" + leaked + "); the digits are matched by the same face that synthesized them\n\n")
	}
	for _, enc := range encoders {
		var recs []Record
		for _, r := range records {
			if r.Encoder == enc {
				recs = append(recs, r)
			}
		}
		out.WriteString(table(enc, recs, nil))
	}
	if tune {
		out.WriteString(tuned(records, encoders[0]))
	}
	fmt.Print(out.String())
	return os.WriteFile(filepath.Join(dir, "table.md"), []byte(out.String()), 0o644)
}

// checkLeak returns the families a set shares with the bundled faces.
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
	faces, err := render.Bundled()
	if err != nil {
		return "", err
	}
	bundled := map[string]bool{}
	for _, f := range faces {
		bundled[f.Family] = true
	}
	var shared []string
	for _, f := range m.Families {
		if bundled[f] {
			shared = append(shared, f)
		}
	}
	return strings.Join(shared, ", "), nil
}

func evaluate(eng *verify.Engine, enc string, labels []string, workers int) ([]Record, error) {
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
				results[j.i], errs[j.i] = one(eng, enc, j.path)
			}
		}()
	}
	start := time.Now()
	for i, p := range labels {
		jobs <- job{i, p}
		if (i+1)%25 == 0 {
			fmt.Fprintf(os.Stderr, "%s: %d/%d labels (%.0fs)\n", enc, i+1, len(labels), time.Since(start).Seconds())
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

func one(eng *verify.Engine, enc, truthPath string) (Record, error) {
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
		Label: filepath.Base(base), Encoder: enc, Printed: t.Printed, Augmented: len(t.Aug) > 0,
		Latency: time.Since(start), Reason: res.Reason, Orient: res.Orientation, Casing: res.ReferenceCasing, Alphabet: res.Alphabet, Claims: res.Claims, Reference: res.Reference, Emphasis: res.Emphasis,
	}
	if rec.Alphabet != nil {
		rec.Alphabet.Rows = nil
	}
	for i := range rec.Claims {
		if rec.Claims[i].Evidence != nil {
			rec.Claims[i].Evidence.Crop = nil
		}
	}
	for i := range rec.Emphasis {
		if rec.Emphasis[i].Evidence != nil {
			rec.Emphasis[i].Evidence.Crop = nil
		}
	}
	return rec, nil
}

// want is what the printed label implies for a claim: "correct" (the
// application's value is on the label), "wrong" (another value is), or
// "missing" (the claim is not printed).
func want(p ttb.Printed, claim string) string {
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
		if w == "wrong" {
			t.mismatchHit++
		} else if w == "correct" {
			t.fn++
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

// override lets tune re-derive a claim verdict from its evidence.
type override struct {
	freeRadius, enumRadius, tie float64
}

// rederive applies radius and tie margin to a claim's recorded evidence,
// approximating the engine (which also weighs agreement across regions).
func rederive(v verify.Verdict, o override) verify.Status {
	ev := v.Evidence
	if ev == nil || ev.Bits == 0 {
		return v.Status
	}
	radius := o.freeRadius
	if v.Claim == "abv" || v.Claim == "net" {
		radius = o.enumRadius
	}
	d1 := float64(ev.D1) / float64(ev.Bits)
	if d1 > radius {
		if v.Status == verify.Skipped {
			return verify.Skipped
		}
		return verify.NotFound
	}
	if ev.D2 >= 0 && ev.Refined {
		d2 := float64(ev.D2) / float64(ev.Bits)
		k := float64(max(1, differing(ev.Text, ev.CompetitorText)))
		margin := math.Max(o.tie, 1.5*ev.Spread) * k / float64(max(1, ev.Glyphs))
		if d2-d1 < margin {
			return verify.Review
		}
	}
	observed := v.Observed
	if observed == "" {
		observed = valueOf(v)
	}
	if observed == v.Expected {
		return verify.Verified
	}
	return verify.Mismatch
}

// valueOf recovers the decoded value of a review verdict from its candidates.
func valueOf(v verify.Verdict) string {
	if len(v.Candidates) > 0 {
		return v.Candidates[0]
	}
	return v.Observed
}

func differing(a, b string) int {
	strip := func(s string) []rune {
		var out []rune
		for _, r := range s {
			if r != ' ' {
				out = append(out, r)
			}
		}
		return out
	}
	x, y := strip(a), strip(b)
	n := 0
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			n++
		}
	}
	if len(x) > len(y) {
		n += len(x) - len(y)
	} else {
		n += len(y) - len(x)
	}
	return n
}

// table renders the report for one encoder; with o set, claim verdicts are
// re-derived from evidence under those thresholds.
func table(enc string, recs []Record, o *override) string {
	claims := []string{"brand", "class", "producer_1", "producer_2", "origin", "abv", "net"}
	tallies := map[string]*tally{}
	for _, c := range claims {
		tallies[c] = &tally{}
	}
	var latencies []float64
	noAlphabet := 0
	rowErr := tally{}       // wording and title-case errors: any row MISMATCH
	rowClean := tally{}     // compliant labels: every row VERIFIED
	emph := tally{}         // regular-header errors detected; compliant headers verified
	brandDisplay := tally{} // brand set in a face outside the body family
	for _, r := range recs {
		latencies = append(latencies, r.Latency.Seconds())
		if r.Reason == "no_alphabet" {
			noAlphabet++
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
			if o != nil {
				status = rederive(v, *o)
			}
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
	title := "## Encoder " + enc
	if o != nil {
		title += fmt.Sprintf(" (tuned: free radius %.2f, enum radius %.2f, tie %.3f; half B)", o.freeRadius, o.enumRadius, o.tie)
	}
	fmt.Fprintf(&b, "%s\n\n%d labels, %d without an alphabet, latency median %.1fs p95 %.1fs\n\n", title, len(recs), noAlphabet, pct(0.5), pct(0.95))
	b.WriteString("| claim | n | precision | recall | review | mismatch found | not found on missing |\n|---|---|---|---|---|---|---|\n")
	for _, c := range claims {
		t := tallies[c]
		fmt.Fprintf(&b, "| %s | %d | %.2f | %.2f | %.2f | %d/%d | %d |\n", c, t.n, t.precision(), t.recall(), float64(t.review)/math.Max(1, float64(t.n)), t.mismatchHit, t.mismatchHit+t.mismatchMiss, t.notFoundHit)
	}
	fmt.Fprintf(&b, "| brand (display face) | %d | %.2f | %.2f | %.2f | | |\n", brandDisplay.n, brandDisplay.precision(), brandDisplay.recall(), float64(brandDisplay.review)/math.Max(1, float64(brandDisplay.n)))
	fmt.Fprintf(&b, "\nReference rows: compliant labels with every row verified %d/%d (%d reviewed, %d failed); wording and title-case errors caught %d/%d.\n", rowClean.tp, rowClean.n, rowClean.review, rowClean.fp, rowErr.tp, rowErr.n)
	fmt.Fprintf(&b, "Emphasis: correct on %d/%d labels (compliant headers verified and regular-weight headers caught).\n\n", emph.tp, emph.n)
	return b.String()
}

// rowWeight recomputes a reference row's anomaly weight from its evidence:
// unexplained glyphs and strong outliers one, weak outliers half.
func rowWeight(v verify.Verdict) float64 {
	if v.Evidence == nil {
		return 0
	}
	w := 0.0
	for _, a := range v.Evidence.Anomalies {
		if a.Strong || a.Kind == alphabet.Insert || a.Kind == alphabet.Delete {
			w++
		} else {
			w += 0.5
		}
	}
	return w
}

// rowStatus re-derives a row verdict under a fail threshold; the shape-class
// and distance rules are left as the engine decided them.
func rowStatus(v verify.Verdict, fail float64) verify.Status {
	if v.Status == verify.Mismatch && v.Reason != "anomalies" {
		return v.Status
	}
	if v.Status == verify.NotFound {
		return v.Status
	}
	w := rowWeight(v)
	switch {
	case w >= fail:
		return verify.Mismatch
	case w > 0:
		return verify.Review
	}
	return verify.Verified
}

// rowScore is the share of wording and title-case errors caught minus the
// share of compliant labels with a failed row, under a fail threshold.
func rowScore(recs []Record, fail float64) (score float64, caught, errs, falseFails, compliant int) {
	for _, r := range recs {
		anyFail := false
		for _, v := range r.Reference {
			if rowStatus(v, fail) == verify.Mismatch {
				anyFail = true
			}
		}
		switch r.Printed.Error {
		case "wording", "title_header":
			errs++
			if anyFail {
				caught++
			}
		case "":
			if len(r.Reference) > 0 {
				compliant++
				if anyFail {
					falseFails++
				}
			}
		}
	}
	if errs > 0 {
		score += float64(caught) / float64(errs)
	}
	if compliant > 0 {
		score -= float64(falseFails) / float64(compliant)
	}
	return
}

// tuned sweeps thresholds on half A and reports half B.
func tuned(records []Record, enc string) string {
	var a, bHalf []Record
	for _, r := range records {
		if r.Encoder != enc {
			continue
		}
		var n int
		fmt.Sscanf(r.Label, "%d", &n)
		if n%2 == 0 {
			a = append(a, r)
		} else {
			bHalf = append(bHalf, r)
		}
	}
	best, bestF := override{0.12, 0.15, 0.02}, -1.0
	for _, free := range []float64{0.08, 0.10, 0.12, 0.15, 0.18} {
		for _, enum := range []float64{0.10, 0.12, 0.15, 0.18, 0.20, 0.22} {
			for _, tie := range []float64{0.01, 0.02, 0.03, 0.05} {
				o := override{free, enum, tie}
				f := f1(a, o)
				if f > bestF {
					best, bestF = o, f
				}
			}
		}
	}
	var rows strings.Builder
	fmt.Fprintf(&rows, "Reference row fail threshold (anomaly weight), half A → half B:\n\n| threshold | errors caught (A) | compliant false fails (A) | errors caught (B) | compliant false fails (B) |\n|---|---|---|---|---|\n")
	for _, fail := range []float64{1, 1.5, 2, 3, 4} {
		_, ca, ea, fa, na := rowScore(a, fail)
		_, cb, eb, fb, nb := rowScore(bHalf, fail)
		fmt.Fprintf(&rows, "| %.1f | %d/%d | %d/%d | %d/%d | %d/%d |\n", fail, ca, ea, fa, na, cb, eb, fb, nb)
	}
	return fmt.Sprintf("## Tuning on half A (%d labels): mean F1 %.3f\n\n", len(a), bestF) + table(enc, bHalf, &best) + rows.String() + "\n"
}

// f1 is the mean F1 of VERIFIED over the claims, with a penalty for wrong
// values that go unflagged.
func f1(recs []Record, o override) float64 {
	tallies := map[string]*tally{}
	for _, r := range recs {
		for _, v := range r.Claims {
			t := tallies[v.Claim]
			if t == nil {
				t = &tally{}
				tallies[v.Claim] = t
			}
			t.add(rederive(v, o), want(r.Printed, v.Claim))
		}
	}
	sum, n := 0.0, 0
	for _, t := range tallies {
		p, r := t.precision(), t.recall()
		if p+r > 0 {
			sum += 2 * p * r / (p + r)
		}
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

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
