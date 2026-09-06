// Command decompose takes the best candidate of each claim apart into the
// terms the engine sums, so that a distance above the radius can be
// attributed rather than guessed at.
//
//	decompose -claims brand,producer_1 real2/0047.png
//
// It prints, per claim, the total distance against the radius, the share
// that is glyph shape, structural steps, unexplained glyphs and
// punctuation, the size the claim is set at against the size the warning
// taught, and every character with its own distance and whether it was
// spelled from the label's own type or synthesized from a bundled face.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"treasury/ttb"
	"treasury/verify"
)

func main() {
	claims := flag.String("claims", "", "comma-separated claim names; empty means all")
	asJSON := flag.Bool("json", false, "one JSON object per claim")
	flag.Parse()
	want := map[string]bool{}
	for _, c := range strings.Split(*claims, ",") {
		if c != "" {
			want[c] = true
		}
	}
	for _, path := range flag.Args() {
		if err := one(path, want, *asJSON); err != nil {
			fmt.Fprintln(os.Stderr, path, err)
		}
	}
}

func one(path string, want map[string]bool, asJSON bool) error {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	b, err := os.ReadFile(base + ".json")
	if err != nil {
		return err
	}
	var exp ttb.Expected
	if err := json.Unmarshal(b, &exp); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	var seen []verify.ClaimBreakdown
	verify.ClaimTrace = func(b verify.ClaimBreakdown) {
		if len(want) == 0 || want[b.Name] {
			seen = append(seen, b)
		}
	}
	defer func() { verify.ClaimTrace = nil }()
	eng, err := verify.New(verify.Options{ClaimEncoder: "learned"})
	if err != nil {
		return err
	}
	refs, claims := ttb.Inputs(exp)
	if _, err := eng.Verify(context.Background(), img, refs, claims); err != nil {
		return err
	}
	// One line per claim: the last breakdown is the one the verdict rests
	// on, since the second pass re-decides what the first left undecided.
	last := map[string]verify.ClaimBreakdown{}
	var order []string
	for _, b := range seen {
		if _, ok := last[b.Name]; !ok {
			order = append(order, b.Name)
		}
		last[b.Name] = b
	}
	sort.Strings(order)
	name := filepath.Base(base)
	for _, n := range order {
		b := last[n]
		if asJSON {
			b.Candidate = strings.TrimSpace(b.Candidate)
			out, err := json.Marshal(struct {
				Label string `json:"label"`
				verify.ClaimBreakdown
			}{name, b})
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			continue
		}
		fmt.Printf("%s %-11s %-34q dist %.3f radius %.3f  shape %.3f structural %.3f unexplained %.3f punctuation %.3f  x-height %.1f against the warning's %.1f\n",
			name, b.Name, trim(b.Candidate, 32), b.Dist, b.Radius, b.Shape, b.Structural, b.Unexplained, b.Punctuation,
			b.RegionXHeight, b.AlphabetXHeight)
		var learned, synth []string
		for _, c := range b.Chars {
			if c.Kind != "match" {
				continue
			}
			s := fmt.Sprintf("%s %.2f", c.Char, c.Dist)
			if c.Learned {
				learned = append(learned, s)
			} else {
				synth = append(synth, s)
			}
		}
		fmt.Printf("    from the label's own type: %s\n", strings.Join(learned, "  "))
		fmt.Printf("    synthesized:               %s\n", strings.Join(synth, "  "))
	}
	return nil
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
