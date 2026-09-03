//go:build onnx

package digits

import (
	"math"
	"testing"
)

// TestONNXParity checks that the pure-Go forward pass and onnxruntime agree
// on heldout frames: same class everywhere, logits within a small tolerance.
func TestONNXParity(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewONNXSession()
	if err != nil {
		t.Skip("onnxruntime unavailable:", err)
	}
	defer s.Close()
	frames, _ := heldout(t)
	n := min(2000, len(frames))
	worst := 0.0
	disagree := 0
	for i := range n {
		a := m.Logits(frames[i])
		b, err := s.Logits(frames[i])
		if err != nil {
			t.Fatal(err)
		}
		ca, cb := 0, 0
		for k := range a {
			worst = math.Max(worst, math.Abs(float64(a[k]-b[k])))
			if a[k] > a[ca] {
				ca = k
			}
			if b[k] > b[cb] {
				cb = k
			}
		}
		if ca != cb {
			disagree++
		}
	}
	t.Logf("%d frames: max |Δlogit| %.2e, %d class disagreements", n, worst, disagree)
	if worst > 1e-3 || disagree > 0 {
		t.Errorf("pure-Go and onnxruntime disagree: max |Δlogit| %.2e, %d disagreements", worst, disagree)
	}
}
