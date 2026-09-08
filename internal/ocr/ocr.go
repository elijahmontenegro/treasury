// Package ocr reads the text in an image the way a scene-text system does:
// a detector proposes regions and a recogniser reads each one whole.
//
// Nothing here binarises, labels components or cuts glyphs. Step 18a
// measured what that cost: the stage that cut glyphs was right on 0.39 of
// characters and on 0.25 once the image had been through a camera channel,
// and every threshold above it had been fitted to pieces that are wrong
// most of the time.
//
// The two models are PaddleOCR's PP-OCRv4 detection and recognition
// networks, exported to ONNX, carried in the binary and run through ONNX
// Runtime in this process on the CPU. The detector emits a probability map
// of where text is; the recogniser emits a distribution over characters per
// frame, which a CTC decode collapses into a string.
package ocr

import (
	_ "embed"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"

	"treasury/internal/buildid"
)

func init() {
	// A verdict has to be traceable to the weights that produced it, so
	// the two models are hashed into the build identity as the retired
	// ones were.
	buildid.Register("ocr-detector.onnx", detModel)
	buildid.Register("ocr-recogniser.onnx", recModel)
}

//go:embed models/det.onnx
var detModel []byte

//go:embed models/rec.onnx
var recModel []byte

//go:embed models/keys.txt
var keysFile string

// Params are the reader's settings. They live with the reader rather than
// in the engine's options: they belong to the models, not to the decision.
type Params struct {
	MaxSide     int     // the longer side the detector sees; the image is scaled to it
	BoxThresh   float64 // the probability above which a pixel is text
	ScoreThresh float64 // the mean probability a region must reach to be kept
	Unclip      float64 // how far a region is grown, as PaddleOCR's unclip ratio
	MinSide     int     // regions thinner than this, in the detector's own scale, are dropped
	RecHeight   int     // the height every crop is resized to before recognition
	MaxRecWidth int     // the widest crop the recogniser is given
	// Det and Rec replace the embedded models, with the names of the
	// tensors they read and write. They exist so a step can measure a
	// second detector or a second recogniser against the ones shipped
	// (step 21c); left empty, the embedded models are used.
	Det, Rec             []byte
	DetOutput, RecOutput string
}

// Default is what the models were trained to see.
func Default() Params {
	return Params{MaxSide: 960, BoxThresh: 0.3, ScoreThresh: 0.5, Unclip: 1.6,
		MinSide: 3, RecHeight: 48, MaxRecWidth: 640}
}

// Region is one piece of text: where it is in the image as given, what it
// says, and how sure the recogniser was.
type Region struct {
	Box        image.Rectangle
	Text       string
	Confidence float64
	Rotated    bool
}

// Reader holds the two sessions. It is safe for concurrent use.
type Reader struct {
	p        Params
	det, rec *ort.DynamicAdvancedSession
	chars    []string
	mu       sync.Mutex // ONNX Runtime sessions are not safe to run concurrently
}

var (
	initOnce sync.Once
	initErr  error
)

// LibraryPath is where ONNX Runtime is loaded from. The environment
// variable wins, so a machine that keeps the library elsewhere needs no
// rebuild; otherwise the usual places are tried.
func LibraryPath() string {
	if p := os.Getenv("TREASURY_ORT_LIB"); p != "" {
		return p
	}
	name := map[string]string{"windows": "onnxruntime.dll", "darwin": "libonnxruntime.dylib"}[runtime.GOOS]
	if name == "" {
		name = "libonnxruntime.so"
	}
	for _, dir := range []string{"third_party/onnxruntime", "../third_party/onnxruntime", "/usr/lib", "/usr/local/lib"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return name
}

func initialize() error {
	initOnce.Do(func() {
		ort.SetSharedLibraryPath(LibraryPath())
		initErr = ort.InitializeEnvironment()
	})
	return initErr
}

// New loads the two models.
func New(p Params) (*Reader, error) { return newReader(p, true) }

// NewRecogniser builds a reader that can only read boxes someone else
// chose. It leaves the detector unloaded, which matters: a second reader
// in the process is a second set of sessions competing for the same cores,
// and step 25b measured a detector it never called costing more than the
// reading it was there to do.
func NewRecogniser(p Params) (*Reader, error) { return newReader(p, false) }

func newReader(p Params, withDetector bool) (*Reader, error) {
	if p.MaxSide == 0 {
		p = Default()
	}
	if err := initialize(); err != nil {
		return nil, fmt.Errorf("onnx runtime at %s: %w", LibraryPath(), err)
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Destroy()
	if err := opts.SetIntraOpNumThreads(runtime.NumCPU()); err != nil {
		return nil, err
	}
	detBytes, detOut := detModel, "sigmoid_0.tmp_0"
	if len(p.Det) > 0 {
		detBytes, detOut = p.Det, p.DetOutput
	}
	recBytes, recOut := recModel, "softmax_11.tmp_0"
	if len(p.Rec) > 0 {
		recBytes, recOut = p.Rec, p.RecOutput
	}
	var det *ort.DynamicAdvancedSession
	if withDetector {
		det, err = ort.NewDynamicAdvancedSessionWithONNXData(detBytes, []string{"x"}, []string{detOut}, opts)
		if err != nil {
			return nil, fmt.Errorf("detector: %w", err)
		}
	}
	rec, err := ort.NewDynamicAdvancedSessionWithONNXData(recBytes, []string{"x"}, []string{recOut}, opts)
	if err != nil {
		if det != nil {
			det.Destroy()
		}
		return nil, fmt.Errorf("recogniser: %w", err)
	}
	// The charset is a blank for the CTC decode, then the dictionary, then
	// a space: 6625 classes for 6623 lines.
	chars := []string{""}
	for _, line := range strings.Split(strings.ReplaceAll(keysFile, "\r\n", "\n"), "\n") {
		chars = append(chars, line)
	}
	if n := len(chars); n > 0 && chars[n-1] == "" {
		chars = chars[:n-1] // the file's trailing newline
	}
	chars = append(chars, " ")
	return &Reader{p: p, det: det, rec: rec, chars: chars}, nil
}

// Close frees the sessions.
func (r *Reader) Close() {
	if r.det != nil {
		r.det.Destroy()
	}
	if r.rec != nil {
		r.rec.Destroy()
	}
}

// Read finds the text in an image and reads it.
func (r *Reader) Read(img image.Image) ([]Region, error) {
	boxes, err := r.detect(img)
	if err != nil {
		return nil, err
	}
	return r.readBoxes(img, boxes)
}

// ReadTurned offers the page to the detector turned a quarter, which is
// how a word set bottom to top reaches a detector trained on horizontal
// lines, and returns only what the first pass did not already find. It is
// separate from Read so that a caller can spend it on the labels that need
// it: step 24a runs it only where the upright pass left a required claim
// unread.
func (r *Reader) ReadTurned(img image.Image, have []Region) ([]Region, error) {
	turned, err := r.detect(turn90(img))
	if err != nil {
		return nil, err
	}
	seen := make([]image.Rectangle, 0, len(have))
	for _, x := range have {
		seen = append(seen, x.Box)
	}
	h := img.Bounds().Dy()
	var boxes []image.Rectangle
	for _, t := range turned {
		b := image.Rect(t.Min.Y, h-t.Max.X, t.Max.Y, h-t.Min.X).Add(img.Bounds().Min)
		if covered(b, seen) {
			continue
		}
		boxes = append(boxes, b)
		seen = append(seen, b)
	}
	return r.readBoxes(img, boxes)
}

// ReadBoxes recognises boxes someone else chose, which is how a second
// opinion is taken on one reading without detecting the page again.
func (r *Reader) ReadBoxes(img image.Image, boxes []image.Rectangle) ([]Region, error) {
	return r.readBoxes(img, boxes)
}

// readBoxes recognises every box and returns them in reading order.
func (r *Reader) readBoxes(img image.Image, boxes []image.Rectangle) ([]Region, error) {
	out := make([]Region, 0, len(boxes))
	for _, b := range boxes {
		text, conf, rot, err := r.recognise(img, b)
		if err != nil {
			return nil, err
		}
		if text == "" {
			continue
		}
		out = append(out, Region{Box: b, Text: text, Confidence: conf, Rotated: rot})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Box.Min.Y != out[j].Box.Min.Y {
			return out[i].Box.Min.Y < out[j].Box.Min.Y
		}
		return out[i].Box.Min.X < out[j].Box.Min.X
	})
	return out, nil
}

// detect runs the detector and turns its probability map into boxes in the
// coordinates of the image as given.
func (r *Reader) detect(img image.Image) ([]image.Rectangle, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, nil
	}
	scale := float64(r.p.MaxSide) / float64(max(w, h))
	dw, dh := roundTo32(float64(w)*scale), roundTo32(float64(h)*scale)
	small := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(small, small.Bounds(), img, b, draw.Src, nil)
	mean := [3]float32{0.485, 0.456, 0.406}
	std := [3]float32{0.229, 0.224, 0.225}
	data := make([]float32, 3*dh*dw)
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			i := small.PixOffset(x, y)
			for c := 0; c < 3; c++ {
				v := float32(small.Pix[i+c]) / 255
				data[c*dh*dw+y*dw+x] = (v - mean[c]) / std[c]
			}
		}
	}
	in, err := ort.NewTensor(ort.NewShape(1, 3, int64(dh), int64(dw)), data)
	if err != nil {
		return nil, err
	}
	defer in.Destroy()
	outputs := []ort.Value{nil}
	r.mu.Lock()
	err = r.det.Run([]ort.Value{in}, outputs)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("detector returned %T", outputs[0])
	}
	defer out.Destroy()
	prob := out.GetData()
	shape := out.GetShape()
	ph, pw := int(shape[2]), int(shape[3])
	return r.boxes(prob, pw, ph, float64(w)/float64(pw), float64(h)/float64(ph), w, h), nil
}

// boxes turns the probability map into rectangles, grown as PaddleOCR grows
// them so that a box drawn on the map's shrunken text covers the glyphs.
func (r *Reader) boxes(prob []float32, pw, ph int, sx, sy float64, w, h int) []image.Rectangle {
	seen := make([]int32, pw*ph)
	var out []image.Rectangle
	stack := make([]int, 0, 1024)
	label := int32(0)
	for i := range seen {
		if seen[i] != 0 || float64(prob[i]) < r.p.BoxThresh {
			continue
		}
		label++
		minX, minY, maxX, maxY := pw, ph, -1, -1
		var sum float64
		var n int
		stack = append(stack[:0], i)
		seen[i] = label
		for len(stack) > 0 {
			k := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := k%pw, k/pw
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
			sum += float64(prob[k])
			n++
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= pw || ny >= ph {
						continue
					}
					j := ny*pw + nx
					if seen[j] != 0 || float64(prob[j]) < r.p.BoxThresh {
						continue
					}
					seen[j] = label
					stack = append(stack, j)
				}
			}
		}
		if n == 0 || sum/float64(n) < r.p.ScoreThresh {
			continue
		}
		bw, bh := maxX-minX+1, maxY-minY+1
		if bw < r.p.MinSide || bh < r.p.MinSide {
			continue
		}
		// Grow the box: the map marks a shrunken core, and the offset that
		// restores it is the region's area over its perimeter, times the
		// unclip ratio.
		d := float64(bw*bh) * r.p.Unclip / float64(2*(bw+bh))
		x0 := int(math.Round((float64(minX) - d) * sx))
		y0 := int(math.Round((float64(minY) - d) * sy))
		x1 := int(math.Round((float64(maxX+1) + d) * sx))
		y1 := int(math.Round((float64(maxY+1) + d) * sy))
		box := image.Rect(max(0, x0), max(0, y0), min(w, x1), min(h, y1))
		if box.Dx() > 0 && box.Dy() > 0 {
			out = append(out, box)
		}
	}
	return out
}

// recognise reads one region. A region taller than it is wide is text set
// on its side, so it is read both ways and the surer reading is kept.
func (r *Reader) recognise(img image.Image, box image.Rectangle) (string, float64, bool, error) {
	text, conf, err := r.readCrop(img, box, 0)
	if err != nil {
		return "", 0, false, err
	}
	if box.Dy() <= box.Dx()*3/2 {
		return text, conf, false, nil
	}
	// A tall box holds text running one of two ways, and which one is not
	// knowable from the box. Both are read and the surer kept. Reading
	// only one of them was a defect step 21d found: the detector was
	// proposing the vertical brands and the recogniser was turning them
	// the wrong way, so they came back as nonsense.
	turned := false
	for _, dir := range []int{1, -1} {
		rot, rconf, err := r.readCrop(img, box, dir)
		if err != nil {
			return "", 0, false, err
		}
		if rconf > conf {
			text, conf, turned = rot, rconf, true
		}
	}
	return text, conf, turned, nil
}

// readCrop resizes a crop to the recogniser's height and decodes what it
// returns.
func (r *Reader) readCrop(img image.Image, box image.Rectangle, turn int) (string, float64, error) {
	src := image.NewRGBA(image.Rect(0, 0, box.Dx(), box.Dy()))
	draw.Copy(src, image.Point{}, img, box, draw.Src, nil)
	crop := image.Image(src)
	switch {
	case turn > 0:
		crop = rotate90(src)
	case turn < 0:
		crop = rotate270(src)
	}
	cb := crop.Bounds()
	if cb.Dx() == 0 || cb.Dy() == 0 {
		return "", 0, nil
	}
	rh := r.p.RecHeight
	rw := int(math.Round(float64(cb.Dx()) * float64(rh) / float64(cb.Dy())))
	rw = min(max(rw, 8), r.p.MaxRecWidth)
	small := image.NewRGBA(image.Rect(0, 0, rw, rh))
	draw.CatmullRom.Scale(small, small.Bounds(), crop, cb, draw.Src, nil)
	data := make([]float32, 3*rh*rw)
	for y := 0; y < rh; y++ {
		for x := 0; x < rw; x++ {
			i := small.PixOffset(x, y)
			for c := 0; c < 3; c++ {
				v := float32(small.Pix[i+c]) / 255
				data[c*rh*rw+y*rw+x] = (v - 0.5) / 0.5
			}
		}
	}
	in, err := ort.NewTensor(ort.NewShape(1, 3, int64(rh), int64(rw)), data)
	if err != nil {
		return "", 0, err
	}
	defer in.Destroy()
	outputs := []ort.Value{nil}
	r.mu.Lock()
	err = r.rec.Run([]ort.Value{in}, outputs)
	r.mu.Unlock()
	if err != nil {
		return "", 0, err
	}
	out, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return "", 0, fmt.Errorf("recogniser returned %T", outputs[0])
	}
	defer out.Destroy()
	shape := out.GetShape()
	return r.decode(out.GetData(), int(shape[1]), int(shape[2]))
}

// decode is the greedy CTC decode: the likeliest class per frame, repeats
// collapsed, blanks dropped. The confidence is the mean of the
// probabilities of the frames that emitted a character.
func (r *Reader) decode(p []float32, frames, classes int) (string, float64, error) {
	var b strings.Builder
	var sum float64
	var n int
	last := -1
	for t := 0; t < frames; t++ {
		best, bestP := 0, float32(-1)
		for c := 0; c < classes; c++ {
			if v := p[t*classes+c]; v > bestP {
				best, bestP = c, v
			}
		}
		if best != 0 && best != last {
			if best < len(r.chars) {
				b.WriteString(r.chars[best])
			}
			sum += float64(bestP)
			n++
		}
		last = best
	}
	if n == 0 {
		return "", 0, nil
	}
	return strings.TrimSpace(b.String()), sum / float64(n), nil
}

func rotate90(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(y, w-1-x, src.At(x, y))
		}
	}
	return out
}

// rotate270 turns a crop the other way, for text that runs top to bottom
// where rotate90 suits text that runs bottom to top.
func rotate270(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(h-1-y, x, src.At(x, y))
		}
	}
	return out
}

func roundTo32(v float64) int {
	n := int(math.Round(v/32)) * 32
	if n < 32 {
		n = 32
	}
	return n
}

// turn90 rotates the page a quarter turn clockwise.
func turn90(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.Y-1-y, x-b.Min.X, src.At(x, y))
		}
	}
	return dst
}

// covered says whether more than half of b already lies inside one of the
// boxes given.
func covered(b image.Rectangle, boxes []image.Rectangle) bool {
	area := b.Dx() * b.Dy()
	if area <= 0 {
		return true
	}
	for _, o := range boxes {
		in := b.Intersect(o)
		if in.Dx()*in.Dy()*2 > area {
			return true
		}
	}
	return false
}
