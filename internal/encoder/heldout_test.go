package encoder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/nn"
)

// glyphSet loads the glyph-frame data cmd/glyphs wrote, when present:
// coverage frames as bytes with a class label each.
type glyphSet struct {
	side    int
	classes int
	frames  [][]byte
	labels  []int
}

func loadGlyphs(t *testing.T, name string) glyphSet {
	t.Helper()
	dir := filepath.Join("..", "..", "python", "encoder", "data")
	if d := os.Getenv("GLYPH_DATA"); d != "" {
		dir = d
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Skip("no glyph data:", err)
	}
	var man struct {
		Side    int    `json:"side"`
		Classes string `json:"classes"`
	}
	if err := json.Unmarshal(b, &man); err != nil {
		t.Fatal(err)
	}
	pix, err := os.ReadFile(filepath.Join(dir, name+".u8"))
	if err != nil {
		t.Skip("no glyph data:", err)
	}
	lbl, err := os.ReadFile(filepath.Join(dir, name+".lbl"))
	if err != nil {
		t.Skip("no glyph labels:", err)
	}
	gs := glyphSet{side: man.Side, classes: len(man.Classes)}
	n := man.Side * man.Side
	for i := range lbl {
		gs.frames = append(gs.frames, pix[i*n:(i+1)*n])
		gs.labels = append(gs.labels, int(lbl[i]))
	}
	return gs
}

// patchOf turns a coverage frame back into a binary patch at the encoder's
// input, ink where coverage is at least half.
func patchOf(frame []byte, side int) Patch {
	b := bitmap.New(side, side)
	for i, v := range frame {
		if v >= 128 {
			b.Pix[i] = 1
		}
	}
	return Patch{Bin: b}
}

// crossFaceScore is the number the engine relies on, for any encoder: on
// held-out families, the nearest character by Hamming distance to
// per-character prototypes built from training families, and the
// same-character and nearest-other-character distance distributions.
func crossFaceScore(t *testing.T, enc Encoder, maxTrain, maxHeld int) {
	t.Helper()
	train := loadGlyphs(t, "train")
	held := loadGlyphs(t, "heldout")
	bits := enc.Bits()
	step := max(1, len(train.frames)/maxTrain)
	byClass := make([][]bitcode.Code, train.classes)
	for i := 0; i < len(train.frames); i += step {
		c := train.labels[i]
		byClass[c] = append(byClass[c], enc.Encode(patchOf(train.frames[i], train.side)))
	}
	protos := make([]bitcode.Code, train.classes)
	for c := range protos {
		if len(byClass[c]) > 0 {
			protos[c] = bitcode.Majority(byClass[c], bits)
		}
	}
	step = max(1, len(held.frames)/maxHeld)
	correct, n := 0, 0
	var same, other []float64
	for i := 0; i < len(held.frames); i += step {
		code := enc.Encode(patchOf(held.frames[i], held.side))
		best, bestD, own, otherD := -1, 0.0, 0.0, 1.0
		for c, p := range protos {
			if p == nil {
				continue
			}
			d := float64(bitcode.Distance(code, p)) / float64(bits)
			if best < 0 || d < bestD {
				best, bestD = c, d
			}
			if c == held.labels[i] {
				own = d
			} else if d < otherD {
				otherD = d
			}
		}
		n++
		if best == held.labels[i] {
			correct++
		}
		same = append(same, own)
		other = append(other, otherD)
	}
	sort.Float64s(same)
	sort.Float64s(other)
	q := func(v []float64, f float64) float64 { return v[int(f*float64(len(v)-1))] }
	t.Logf("%s: held-out nearest-prototype accuracy %.4f (%d frames); same-character distance p50 %.3f p90 %.3f; nearest other p10 %.3f p50 %.3f",
		enc.Name(), float64(correct)/float64(n), n, q(same, 0.5), q(same, 0.9), q(other, 0.1), q(other, 0.5))
}

// TestCrossFaceDual scores the dual hash the alphabet uses.
func TestCrossFaceDual(t *testing.T) {
	crossFaceScore(t, Glyph(), 40000, 20000)
}

// TestCrossFaceLearned scores the embedded contrastive encoder the same way.
func TestCrossFaceLearned(t *testing.T) {
	enc, err := NewLearned()
	if err != nil {
		t.Skip(err)
	}
	crossFaceScore(t, enc, 40000, 20000)
}

// TestCrossFaceLearnedAlt scores a learned encoder read from the directory
// LEARNED_ALT names (learned.bin and learned.json), for comparing models.
func TestCrossFaceLearnedAlt(t *testing.T) {
	dir := os.Getenv("LEARNED_ALT")
	if dir == "" {
		t.Skip("set LEARNED_ALT=dir")
	}
	bin, err := os.ReadFile(filepath.Join(dir, "learned.bin"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile(filepath.Join(dir, "learned.json"))
	if err != nil {
		t.Fatal(err)
	}
	net, err := nn.Decode(bin, js)
	if err != nil {
		t.Fatal(err)
	}
	crossFaceScore(t, &Learned{net: net}, 40000, 20000)
}
