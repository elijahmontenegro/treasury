package digits

import (
	_ "embed"
	"fmt"
	"math"
	"sync"

	"treasury/internal/nn"
)

// The trained network, exported by python/digits/export.py: batch norm
// folded into the convolutions, weights as little-endian float32 in layer
// order, and the layer list with shapes. Inference is plain Go so the
// default build classifies digits without cgo or a runtime library; the
// onnx build tag adds the same network through onnxruntime for parity.

//go:embed model.bin
var modelBin []byte

//go:embed model.json
var modelJSON []byte

// Model is the classifier: a network over a Side×Side frame.
type Model struct {
	net *nn.Model
}

var (
	loadOnce  sync.Once
	loaded    *Model
	loadError error
)

// Load returns the embedded model, decoding it once.
func Load() (*Model, error) {
	loadOnce.Do(func() { loaded, loadError = decode(modelBin, modelJSON) })
	return loaded, loadError
}

func decode(bin, js []byte) (*Model, error) {
	net, err := nn.Decode(bin, js)
	if err != nil {
		return nil, fmt.Errorf("digits: %w", err)
	}
	if net.Spec.Side != Side || net.Spec.Classes != Classes {
		return nil, fmt.Errorf("digits: model is for side %d classes %q, code has %d %q", net.Spec.Side, net.Spec.Classes, Side, Classes)
	}
	return &Model{net: net}, nil
}

// Logits runs the network on one frame (Side×Side values in [0, 1], row
// major) and returns one score per class.
func (m *Model) Logits(frame []float32) []float32 {
	return m.net.Forward(frame)
}

// Classify returns the class index of the frame and the softmax probability
// of that class.
func (m *Model) Classify(frame []float32) (int, float64) {
	logits := m.Logits(frame)
	best := 0
	for i, v := range logits {
		if v > logits[best] {
			best = i
		}
	}
	sum := 0.0
	for _, v := range logits {
		sum += math.Exp(float64(v - logits[best]))
	}
	return best, 1 / sum
}
