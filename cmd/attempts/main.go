// Command attempts reports, for each label, every attempt the orientation
// ladder made and what became of it.
//
//	attempts real2/0002.png real2/0012.png
//
// A label that learns no alphabet is otherwise attributed to the last thing
// tried. This prints all of them, so the stage that failed can be named:
// the block never located, the alignment rejected for unexplained glyphs,
// the alignment rejected for characters contradicting their shape class, or
// an orientation chosen wrongly.
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
	"strings"

	"treasury/ttb"
	"treasury/verify"
)

type attempt struct {
	Orientation string  `json:"orientation"`
	Casing      string  `json:"casing"`
	Glyphs      int     `json:"glyphs"`
	Matched     int     `json:"matched"`
	Unexplained int     `json:"unexplained"`
	Violations  float64 `json:"violations"`
	Spread      float64 `json:"spread"`
	Coverage    float64 `json:"coverage"`
	Accepted    bool    `json:"accepted"`
	Located     bool    `json:"located"`
}

func main() {
	asJSON := flag.Bool("json", false, "one JSON object per label")
	flag.Parse()
	for _, path := range flag.Args() {
		if err := one(path, *asJSON); err != nil {
			fmt.Fprintln(os.Stderr, path, err)
		}
	}
}

func one(path string, asJSON bool) error {
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
	var tries []attempt
	verify.AttemptTrace = func(orientation, casing string, block image.Rectangle, glyphs, matched, unexplained int, violations, spread, coverage float64, accepted bool) {
		tries = append(tries, attempt{orientation, casing, glyphs, matched, unexplained, violations, spread, coverage, accepted, casing != ""})
	}
	defer func() { verify.AttemptTrace = nil }()
	eng, err := verify.New(verify.Options{ClaimEncoder: "learned"})
	if err != nil {
		return err
	}
	refs, claims := ttb.Inputs(exp)
	res, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		return err
	}
	name := filepath.Base(base)
	if asJSON {
		out, err := json.Marshal(map[string]any{
			"label": name, "reason": res.Reason, "orientation": res.Orientation,
			"claims_orientation": res.ClaimsOrientation, "attempts": tries,
		})
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("%s: %s, taken %s, claims in %s\n", name, reasonOf(res), res.Orientation, res.ClaimsOrientation)
	for _, t := range tries {
		if !t.Located {
			fmt.Printf("   %-16s no block located\n", t.Orientation)
			continue
		}
		fmt.Printf("   %-16s %-9s glyphs %3d matched %3d unexplained %2d violations %.2f spread %.3f %s\n",
			t.Orientation, t.Casing, t.Glyphs, t.Matched, t.Unexplained, t.Violations, t.Spread, acceptedOf(t.Accepted))
	}
	return nil
}

func reasonOf(res verify.Result) string {
	if res.Reason == "" {
		return "alphabet learned"
	}
	return res.Reason
}

func acceptedOf(ok bool) string {
	if ok {
		return "accepted"
	}
	return "rejected"
}
