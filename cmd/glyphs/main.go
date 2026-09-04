// Command glyphs builds the training data for the glyph encoder: every
// character a claim can contain, drawn in every available face at three
// x-heights, sent through the label generator's augmentation and the
// engine's preprocessing, and cut into the alphabet's own glyph frames
// (binary, a square of 2.2 x-heights on the row's baseline) with the
// character and the face family as labels.
//
//	glyphs gen -fontdir C:\Windows\Fonts -out python/encoder/data [-seed 1] [-limit N]
//
// Families whose name hashes to a fifth of the space are held out, the
// same split as the digit classifier's; the bundled faces always train.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"image"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"treasury/internal/alphabet"
	"treasury/internal/encoder"
	"treasury/internal/imgops"
	"treasury/internal/preprocess"
	"treasury/internal/render"
	"treasury/internal/synth"
)

// Charset is the label space, in index order.
const Charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.,:;'()-/%&"

// Side is the frame's side as the encoder sees it.
const Side = 32

func main() {
	if len(os.Args) < 2 || os.Args[1] != "gen" {
		fmt.Fprintln(os.Stderr, "usage: glyphs gen -fontdir DIR -out DIR [-seed N] [-limit N] [-workers N]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	fontdir := fs.String("fontdir", "", "directory of TTF/OTF faces")
	out := fs.String("out", "python/encoder/data", "output directory")
	seed := fs.Int64("seed", 1, "random seed")
	limit := fs.Int("limit", 0, "use at most this many faces (0 = all)")
	workers := fs.Int("workers", 8, "parallel faces")
	fs.Parse(os.Args[2:])
	if err := run(*fontdir, *out, *seed, *limit, *workers); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type sample struct {
	pix    []byte
	label  byte
	family uint16
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
	names := func(m map[string]bool) []string {
		var v []string
		for k := range m {
			v = append(v, k)
		}
		sort.Strings(v)
		return v
	}
	man := map[string]any{
		"side": Side, "classes": Charset, "families": familyNames,
		"train": len(train), "heldout": len(held),
		"train_families": names(families[false]), "heldout_families": names(families[true]),
		"unusable_faces": unusable, "seed": seed,
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

// sheets renders the face at three x-heights, clean and augmented.
func sheets(face *render.Face, rng *rand.Rand) ([]sample, error) {
	xhRatio, capRatio, err := face.Metrics()
	if err != nil || xhRatio <= 0 || capRatio <= 0 {
		return nil, fmt.Errorf("no metrics")
	}
	var all []sample
	for _, xh := range []float64{10, 14, 20} {
		px := xh / xhRatio
		doc := document(face.Name, px, rng)
		img, truth, err := synth.Render(doc, []*render.Face{face})
		if err != nil {
			return nil, err
		}
		if !renders(truth) {
			return nil, fmt.Errorf("face does not draw the character set")
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
		all = append(all, frames(pre, truth, rng)...)
	}
	return all, nil
}

// document lays out the character set in random order, in words, plus a
// few lines of running text for realistic spacing and casing.
func document(face string, px float64, rng *rand.Rand) synth.Document {
	runes := []rune(Charset)
	var lines []string
	for range 2 {
		rng.Shuffle(len(runes), func(i, j int) { runes[i], runes[j] = runes[j], runes[i] })
		var b strings.Builder
		for i, r := range runes {
			b.WriteRune(r)
			if (i+1)%(3+rng.Intn(4)) == 0 {
				b.WriteByte(' ')
			}
			if b.Len() >= 34 && i < len(runes)-1 {
				lines = append(lines, strings.TrimSpace(b.String()))
				b.Reset()
			}
		}
		if b.Len() > 0 {
			lines = append(lines, strings.TrimSpace(b.String()))
		}
	}
	lines = append(lines,
		"Distilled and Bottled by Old Tom Distillery",
		"KENTUCKY STRAIGHT BOURBON WHISKEY 45% Alc./Vol.",
		"Product of USA, 750 mL (25.4 fl. oz.)",
		"according to the surgeon general, women should not",
	)
	doc := synth.Document{W: 1600}
	y := int(math.Round(2 * px))
	for i, text := range lines {
		doc.Items = append(doc.Items, synth.Item{Text: text, Face: face, Px: px, X: 40, Y: y, Claim: fmt.Sprintf("L%d", i)})
		y += int(math.Round(2.6 * px))
	}
	doc.H = y
	return doc
}

// renders checks that the face drew most of the character set with ink.
func renders(t *synth.Truth) bool {
	seen := map[string]bool{}
	for _, g := range t.Glyphs {
		if g.Box.Dx() > 0 && g.Box.Dy() > 0 {
			seen[g.Char] = true
		}
	}
	missing := 0
	for _, c := range Charset {
		if !seen[string(c)] {
			missing++
		}
	}
	return missing <= 4
}

// frames cuts one frame per truth glyph of the character set, mapped
// through the preprocessing's resize and deskew, at the alphabet's frame
// geometry with the same jitter the engine's own estimates carry.
func frames(pre *preprocess.Result, t *synth.Truth, rng *rand.Rand) []sample {
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
	// Per line: the x-height is the most common height among lowercase
	// letters without ascenders or descenders; the baseline their bottom.
	type lineGeom struct{ hs, bottoms []float64 }
	geoms := map[string]*lineGeom{}
	mapped := make([]image.Rectangle, len(t.Glyphs))
	for i, g := range t.Glyphs {
		mapped[i] = mapBox(g.Box)
		if len(g.Char) == 1 && strings.ContainsRune("acemnorsuvwxz", rune(g.Char[0])) {
			lg := geoms[g.Claim]
			if lg == nil {
				lg = &lineGeom{}
				geoms[g.Claim] = lg
			}
			lg.hs = append(lg.hs, float64(mapped[i].Dy()))
			lg.bottoms = append(lg.bottoms, float64(mapped[i].Max.Y))
		}
	}
	index := map[rune]int{}
	for i, r := range Charset {
		index[r] = i
	}
	var out []sample
	for i, g := range t.Glyphs {
		lg := geoms[g.Claim]
		if lg == nil || len(lg.hs) < 3 || len(g.Char) != 1 {
			continue
		}
		label, ok := index[rune(g.Char[0])]
		if !ok {
			continue
		}
		xh := median(lg.hs)
		base := median(lg.bottoms)
		xhJ := xh * (0.85 + 0.3*rng.Float64())
		baseJ := int(math.Round(base + (rng.Float64()-0.5)*0.25*xh))
		box := mapped[i].Add(image.Pt(int(math.Round((rng.Float64()-0.5)*0.2*xh)), 0))
		patch := alphabet.Frame(pre.Bin, box, baseJ, xhJ)
		cov := encoder.Coverage(patch.Bin, Side, Side)
		pix := make([]byte, len(cov))
		ink := 0
		for k, v := range cov {
			pix[k] = byte(math.Round(v * 255))
			if pix[k] > 0 {
				ink++
			}
		}
		if ink < 4 {
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
	pix := make([]byte, 0, len(s)*Side*Side)
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
