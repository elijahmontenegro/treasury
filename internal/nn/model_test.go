package nn

import (
	"math"
	"math/rand"
	"testing"
)

// conv3x3Ref is the plain definition the fast convolution must match.
func conv3x3Ref(in []float32, c, h, w int, weights, bias []float32, out int) []float32 {
	res := make([]float32, out*h*w)
	for o := range out {
		for y := range h {
			for x := range w {
				sum := bias[o]
				for i := range c {
					for ky := range 3 {
						for kx := range 3 {
							sy, sx := y+ky-1, x+kx-1
							if sy < 0 || sy >= h || sx < 0 || sx >= w {
								continue
							}
							sum += weights[(o*c+i)*9+ky*3+kx] * in[i*h*w+sy*w+sx]
						}
					}
				}
				res[o*h*w+y*w+x] = sum
			}
		}
	}
	return res
}

func TestConvMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	c, h, w, out := 3, 7, 9, 4
	in := make([]float32, c*h*w)
	for i := range in {
		in[i] = rng.Float32()
	}
	weights := make([]float32, out*c*9)
	for i := range weights {
		weights[i] = rng.Float32() - 0.5
	}
	bias := []float32{0.1, -0.2, 0.3, 0}
	a := conv3x3(in, c, h, w, weights, bias, out)
	b := conv3x3Ref(in, c, h, w, weights, bias, out)
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-5 {
			t.Fatalf("index %d: fast %v ref %v", i, a[i], b[i])
		}
	}
}
