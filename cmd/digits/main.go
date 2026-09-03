// Command digits builds the training data for the digit classifier: the
// digit classes and other characters drawn in every available face at three
// x-heights, sent through the label generator's augmentation and the
// engine's preprocessing, and cut into glyph frames with the truth's labels.
//
//	digits gen -fontdir C:\Windows\Fonts -out python/digits/data [-seed 1] [-limit N]
//
// Families whose name hashes to a fifth of the space are held out; the
// bundled faces always train. Frames are written as raw Side×Side bytes with
// one label byte per frame and a little-endian uint16 family index per
// frame, plus a manifest and a montage for the eye.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"treasury/internal/digits"
	"treasury/internal/imgops"
	"treasury/internal/preprocess"
	"treasury/internal/render"
	"treasury/internal/synth"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "gen" {
		fmt.Fprintln(os.Stderr, "usage: digits gen -fontdir DIR -out DIR [-seed N] [-limit N] [-workers N]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	fontdir := fs.String("fontdir", "", "directory of TTF/OTF faces (held-out split is drawn from these)")
	out := fs.String("out", "python/digits/data", "output directory")
	seed := fs.Int64("seed", 1, "random seed")
	limit := fs.Int("limit", 0, "use at most this many faces (0 = all)")
	workers := fs.Int("workers", 8, "parallel faces")
	fs.Parse(os.Args[2:])
	if err := run(*fontdir, *out, *seed, *limit, *workers); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// sample is one labelled frame.
type sample struct {
	pix    []byte
	label  byte
	family uint16 // index into the manifest's families list
}

type faceSet struct {
	face    *render.Face
	heldout bool
}

func run(fontdir, out string, seed int64, limit, workers int) error {
	faces, err := render.Bundled()
	if err != nil {
		return err
	}
	var sets []faceSet
	for _, f := range faces {
		sets = append(sets, faceSet{face: f})
	}
	if fontdir != "" {
		dir, skipped, err := render.LoadDir(fontdir)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "loaded %d faces from %s (%d skipped)\n", len(dir), fontdir, len(skipped))
		for _, f := range dir {
			sets = append(sets, faceSet{face: f, heldout: heldoutFamily(f.Family)})
		}
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].face.Name < sets[j].face.Name })
	if limit > 0 && len(sets) > limit {
		sets = sets[:limit]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	// Family indices, for per-family error analysis of the trained model.
	familyIndex := map[string]uint16{}
	var familyNames []string
	for _, fs := range sets {
		if _, ok := familyIndex[fs.face.Family]; !ok {
			familyIndex[fs.face.Family] = uint16(len(familyNames))
			familyNames = append(familyNames, fs.face.Family)
		}
	}
	var mu sync.Mutex
	var train, held []sample
	families := map[bool]map[string]bool{false: {}, true: {}}
	unusable := 0
	var wg sync.WaitGroup
	jobs := make(chan int)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				fs := sets[i]
				rng := rand.New(rand.NewSource(seed*1000003 + int64(i)))
				got, err := sheets(fs.face, rng)
				mu.Lock()
				if err != nil || len(got) == 0 {
					unusable++
					if err != nil {
						fmt.Fprintf(os.Stderr, "skip %s: %v\n", fs.face.Name, err)
					}
				} else {
					for k := range got {
						got[k].family = familyIndex[fs.face.Family]
					}
					families[fs.heldout][fs.face.Family] = true
					if fs.heldout {
						held = append(held, got...)
					} else {
						train = append(train, got...)
					}
				}
				mu.Unlock()
			}
		}()
	}
	for i := range sets {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	if err := write(out, "train", train); err != nil {
		return err
	}
	if err := write(out, "heldout", held); err != nil {
		return err
	}
	if err := montage(filepath.Join(out, "montage.png"), train, 24); err != nil {
		return err
	}
	names := func(m map[string]bool) []string {
		var v []string
		for k := range m {
			v = append(v, k)
		}
		sort.Strings(v)
		return v
	}
	man := map[string]any{
		"side": digits.Side, "classes": digits.Classes,
		"train": len(train), "heldout": len(held),
		"train_families": names(families[false]), "heldout_families": names(families[true]),
		"unusable_faces": unusable, "seed": seed, "families": familyNames,
		"per_class": counts(train), "per_class_heldout": counts(held),
	}
	b, _ := json.MarshalIndent(man, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "manifest.json"), b, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "train %d frames from %d families, heldout %d frames from %d families, %d faces unusable\n",
		len(train), len(families[false]), len(held), len(families[true]), unusable)
	return nil
}

func heldoutFamily(family string) bool {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(family)))
	return h.Sum32()%5 == 0
}

func counts(s []sample) map[string]int {
	m := map[string]int{}
	for _, x := range s {
		m[string(digits.Classes[x.label])]++
	}
	return m
}

// sheets renders the face at three x-heights, clean and augmented, and
// returns the labelled frames.
func sheets(face *render.Face, rng *rand.Rand) ([]sample, error) {
	xhRatio, capRatio, err := face.Metrics()
	if err != nil || xhRatio <= 0 || capRatio <= 0 {
		return nil, fmt.Errorf("no metrics")
	}
	var all []sample
	for _, xh := range []float64{10, 14, 20} {
		px := xh / xhRatio
		doc, lineChars := document(face.Name, px, rng)
		img, truth, err := synth.Render(doc, []*render.Face{face})
		if err != nil {
			return nil, err
		}
		if !renders(truth, lineChars) {
			return nil, fmt.Errorf("face does not draw the digit classes")
		}
		if rng.Float64() < 0.8 {
			if img, truth, err = synth.Augment(img, truth, synth.Random(rng)); err != nil {
				return nil, err
			}
		}
		pre, err := preprocess.Run(img, preprocess.Default())
		if err != nil {
			return nil, err
		}
		all = append(all, frames(pre, truth, capRatio/xhRatio, rng)...)
	}
	return all, nil
}

// document lays out the lines of a sheet: fixed numeric strings in the
// forms labels use, random digit strings, and a line of other characters.
// Each line is its own claim so its glyphs can be grouped.
func document(face string, px float64, rng *rand.Rand) (synth.Document, map[string]bool) {
	lines := []string{
		"0123456789 0123456789",
		"45.0% 12.5% 40% 5.5% 95.0% 100% 0.5%",
		"750 mL 375 mL 1,000 mL 1.75 L 50 mL 187 ML 700 ml",
		"(90 Proof) 80 Proof 40% ABV 13.5% Alc./Vol. ALC. 7.2% BY VOL.",
		"abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ /-():;",
	}
	for range 4 {
		lines = append(lines, randomDigits(rng))
	}
	doc := synth.Document{W: 1600}
	y := int(math.Round(2 * px))
	chars := map[string]bool{}
	for i, text := range lines {
		doc.Items = append(doc.Items, synth.Item{Text: text, Face: face, Px: px, X: 40, Y: y, Claim: fmt.Sprintf("L%d", i)})
		for _, r := range text {
			chars[string(r)] = true
		}
		y += int(math.Round(2.6 * px))
	}
	doc.H = y
	return doc, chars
}

func randomDigits(rng *rand.Rand) string {
	const pool = "0123456789.,%.,%"
	var b strings.Builder
	for b.Len() < 40 {
		n := 1 + rng.Intn(5)
		for range n {
			b.WriteByte(pool[rng.Intn(len(pool))])
		}
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

// renders checks that every digit class the sheet asked for was drawn
// with ink; a symbol face draws nothing or boxes.
func renders(t *synth.Truth, chars map[string]bool) bool {
	seen := map[string]bool{}
	for _, g := range t.Glyphs {
		if g.Box.Dx() > 0 && g.Box.Dy() > 0 {
			seen[g.Char] = true
		}
	}
	for _, c := range digits.Classes[:digits.Other] {
		if chars[string(c)] && !seen[string(c)] {
			return false
		}
	}
	return true
}

// frames cuts one labelled frame per truth glyph, mapped through the
// preprocessing's resize and deskew, with the geometry jittered as the
// engine's own estimate will be: x-height within 15 percent, baseline
// within an eighth of an x-height, centre within a tenth; and half of them
// condensed, extended, or slanted, which the installed faces cover thinly.
func frames(pre *preprocess.Result, t *synth.Truth, capOverX float64, rng *rand.Rand) []sample {
	cx, cy := float64(pre.Gray.Rect.Dx())/2, float64(pre.Gray.Rect.Dy())/2
	mapBox := func(b image.Rectangle) image.Rectangle {
		x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, p := range []image.Point{b.Min, {b.Max.X, b.Min.Y}, {b.Min.X, b.Max.Y}, b.Max} {
			x, y := float64(p.X)*pre.Scale, float64(p.Y)*pre.Scale
			if pre.AngleDeg != 0 {
				x, y = imgops.RotatePoint(x, y, cx, cy, -pre.AngleDeg)
			}
			x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x), math.Max(y1, y)
		}
		return image.Rect(int(math.Round(x0)), int(math.Round(y0)), int(math.Round(x1)), int(math.Round(y1)))
	}
	// Per line: the digits' median height gives the x-height, their median
	// bottom the baseline.
	type lineGeom struct {
		hs, bottoms []float64
	}
	geoms := map[string]*lineGeom{}
	mapped := make([]image.Rectangle, len(t.Glyphs))
	for i, g := range t.Glyphs {
		mapped[i] = mapBox(g.Box)
		if len(g.Char) == 1 && g.Char[0] >= '0' && g.Char[0] <= '9' {
			lg := geoms[g.Claim]
			if lg == nil {
				lg = &lineGeom{}
				geoms[g.Claim] = lg
			}
			lg.hs = append(lg.hs, float64(mapped[i].Dy()))
			lg.bottoms = append(lg.bottoms, float64(mapped[i].Max.Y))
		}
	}
	var out []sample
	others := 0
	for i, g := range t.Glyphs {
		lg := geoms[g.Claim]
		if lg == nil || len(lg.hs) == 0 || len(g.Char) != 1 {
			continue
		}
		label := digits.ClassOf(rune(g.Char[0]))
		if label == digits.Other {
			// Letters shaped like a digit in many faces (O for 0, l and I
			// for 1, S for 5, B for 8, Z for 2, g and q for 9, b for 6, D
			// for 0) do not train the other class: the classifier says
			// which digit a glyph is, and whether it is a digit at all is
			// the engine's call from the run's context. Trained as other,
			// they capped digit recall at 96 percent on unseen faces.
			if g.Char == " " || others >= 40 || strings.ContainsRune("OolISBZzgqbD", rune(g.Char[0])) {
				continue
			}
			others++
		}
		xh := median(lg.hs) / capOverX
		base := median(lg.bottoms)
		xhJ := xh * (0.85 + 0.3*rng.Float64())
		baseJ := int(math.Round(base + (rng.Float64()-0.5)*0.25*xh))
		box := mapped[i]
		shift := int(math.Round((rng.Float64() - 0.5) * 0.2 * xh))
		box = box.Add(image.Pt(shift, 0))
		// Half the frames stand in for faces the set lacks: condensed or
		// extended by up to a third, slanted by up to a quarter.
		hscale, shear := 1.0, 0.0
		if rng.Float64() < 0.5 {
			hscale = 0.7 + 0.65*rng.Float64()
			shear = (rng.Float64() - 0.5) * 0.5
		}
		f := digits.FrameAt(pre.Gray, box, baseJ, xhJ, hscale, shear)
		if os.Getenv("DIGITS_DEBUG") != "" && i < 3 {
			mn, mx := 255, 0
			r := image.Rect(box.Min.X-5, baseJ-int(2*xhJ), box.Max.X+5, baseJ+5).Intersect(pre.Gray.Rect)
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for x := r.Min.X; x < r.Max.X; x++ {
					v := int(pre.Gray.GrayAt(x, y).Y)
					mn, mx = min(mn, v), max(mx, v)
				}
			}
			sum := float32(0)
			for _, v := range f {
				sum += v
			}
			fmt.Fprintf(os.Stderr, "glyph %q truth %v mapped %v scale %.3f angle %.2f xh %.1f base %d gray[min %d max %d] frame sum %.1f\n", g.Char, g.Box, box, pre.Scale, pre.AngleDeg, xhJ, baseJ, mn, mx, sum)
		}
		pix := make([]byte, len(f))
		blank := true
		for k, v := range f {
			pix[k] = byte(math.Round(float64(v) * 255))
			if pix[k] > 0 {
				blank = false
			}
		}
		if blank {
			continue
		}
		out = append(out, sample{pix: pix, label: byte(label)})
	}
	return out
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

func write(dir, name string, s []sample) error {
	pix := make([]byte, 0, len(s)*digits.Side*digits.Side)
	lbl := make([]byte, 0, len(s))
	fam := make([]byte, 0, 2*len(s))
	for _, x := range s {
		pix = append(pix, x.pix...)
		lbl = append(lbl, x.label)
		fam = append(fam, byte(x.family), byte(x.family>>8))
	}
	if err := os.WriteFile(filepath.Join(dir, name+".u8"), pix, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".lbl"), lbl, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+".fam"), fam, 0o644)
}

// montage draws n random frames of every class in a row per class.
func montage(path string, s []sample, n int) error {
	byClass := map[byte][]sample{}
	for _, x := range s {
		byClass[x.label] = append(byClass[x.label], x)
	}
	rows := len(digits.Classes)
	img := image.NewGray(image.Rect(0, 0, n*(digits.Side+2), rows*(digits.Side+2)))
	for i := range img.Pix {
		img.Pix[i] = 128
	}
	rng := rand.New(rand.NewSource(7))
	for c := range rows {
		list := byClass[byte(c)]
		for k := range n {
			if len(list) == 0 {
				break
			}
			x := list[rng.Intn(len(list))]
			for y := range digits.Side {
				for xx := range digits.Side {
					img.SetGray(k*(digits.Side+2)+xx, c*(digits.Side+2)+y, color.Gray{255 - x.pix[y*digits.Side+xx]})
				}
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
