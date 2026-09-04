// Package nn runs the small convolutional networks the engine embeds, in
// plain Go: the digit classifier and the glyph encoder. A model is a
// sequence of layers exported by the Python side with batch norm folded:
// 3×3 same-padded convolutions with bias, ReLU, 2×2 max pooling, global
// average pooling, linear layers, and tanh. Weights are little-endian
// float32 in layer order; the layer list with shapes is JSON.
package nn

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

// LayerSpec is one layer of the exported model.
type LayerSpec struct {
	Kind    string `json:"kind"`
	In      int    `json:"in"`
	Out     int    `json:"out"`
	K       int    `json:"k"`
	Weights int    `json:"weights"`
	Bias    int    `json:"bias"`
}

// Spec is the exported model's manifest.
type Spec struct {
	Side    int         `json:"side"`
	Classes string      `json:"classes,omitempty"`
	Dim     int         `json:"dim,omitempty"`
	Layers  []LayerSpec `json:"layers"`
	Floats  int         `json:"floats"`
}

type layer struct {
	spec    LayerSpec
	weights []float32
	bias    []float32
}

// Model is a decoded network.
type Model struct {
	Spec   Spec
	layers []layer
}

// Decode reads a model from its weight blob and manifest.
func Decode(bin, js []byte) (*Model, error) {
	var spec Spec
	if err := json.Unmarshal(js, &spec); err != nil {
		return nil, fmt.Errorf("nn: manifest: %w", err)
	}
	if len(bin) != 4*spec.Floats {
		return nil, fmt.Errorf("nn: weights have %d bytes, manifest says %d floats", len(bin), spec.Floats)
	}
	floats := make([]float32, spec.Floats)
	for i := range floats {
		floats[i] = math.Float32frombits(binary.LittleEndian.Uint32(bin[4*i:]))
	}
	m := &Model{Spec: spec}
	pos := 0
	take := func(n int) []float32 {
		v := floats[pos : pos+n]
		pos += n
		return v
	}
	for _, ls := range spec.Layers {
		l := layer{spec: ls}
		switch ls.Kind {
		case "conv":
			if ls.K != 3 || ls.Weights != ls.Out*ls.In*9 || ls.Bias != ls.Out {
				return nil, fmt.Errorf("nn: bad conv layer %+v", ls)
			}
			l.weights, l.bias = take(ls.Weights), take(ls.Bias)
		case "linear":
			if ls.Weights != ls.Out*ls.In || ls.Bias != ls.Out {
				return nil, fmt.Errorf("nn: bad linear layer %+v", ls)
			}
			l.weights, l.bias = take(ls.Weights), take(ls.Bias)
		case "relu", "gap", "tanh":
		case "maxpool":
			if ls.K != 2 {
				return nil, fmt.Errorf("nn: bad maxpool layer %+v", ls)
			}
		default:
			return nil, fmt.Errorf("nn: unknown layer kind %q", ls.Kind)
		}
		m.layers = append(m.layers, l)
	}
	if pos != len(floats) {
		return nil, fmt.Errorf("nn: %d floats unused", len(floats)-pos)
	}
	return m, nil
}

// Forward runs the network on one single-channel Side×Side input, row
// major, and returns the output vector.
func (m *Model) Forward(frame []float32) []float32 {
	side := m.Spec.Side
	if len(frame) != side*side {
		panic("nn: input must be Side×Side")
	}
	act := append([]float32(nil), frame...)
	c, h, w := 1, side, side
	for _, l := range m.layers {
		switch l.spec.Kind {
		case "conv":
			act = conv3x3(act, c, h, w, l.weights, l.bias, l.spec.Out)
			c = l.spec.Out
		case "relu":
			for i, v := range act {
				if v < 0 {
					act[i] = 0
				}
			}
		case "tanh":
			for i, v := range act {
				act[i] = float32(math.Tanh(float64(v)))
			}
		case "maxpool":
			act, h, w = maxpool2(act, c, h, w)
		case "gap":
			pooled := make([]float32, c)
			n := float32(h * w)
			for ch := range c {
				sum := float32(0)
				for _, v := range act[ch*h*w : (ch+1)*h*w] {
					sum += v
				}
				pooled[ch] = sum / n
			}
			act, h, w = pooled, 1, 1
		case "linear":
			out := make([]float32, l.spec.Out)
			for o := range l.spec.Out {
				sum := l.bias[o]
				row := l.weights[o*l.spec.In : (o+1)*l.spec.In]
				for i, v := range act {
					sum += row[i] * v
				}
				out[o] = sum
			}
			act = out
		}
	}
	return act
}

// conv3x3 is a same-padded 3×3 convolution over c×h×w activations with
// weights laid out [out][in][3][3]. Each input plane is copied into a
// zero-padded buffer once so the nine taps run over whole rows without
// bounds tests; that is most of the network's time.
func conv3x3(in []float32, c, h, w int, weights, bias []float32, out int) []float32 {
	pw, ph := w+2, h+2
	padded := make([]float32, c*ph*pw)
	for i := range c {
		for y := range h {
			copy(padded[i*ph*pw+(y+1)*pw+1:], in[i*h*w+y*w:i*h*w+(y+1)*w])
		}
	}
	res := make([]float32, out*h*w)
	for o := range out {
		plane := res[o*h*w : (o+1)*h*w]
		for i := range plane {
			plane[i] = bias[o]
		}
		for i := range c {
			k := weights[(o*c+i)*9 : (o*c+i+1)*9]
			w00, w01, w02 := k[0], k[1], k[2]
			w10, w11, w12 := k[3], k[4], k[5]
			w20, w21, w22 := k[6], k[7], k[8]
			src := padded[i*ph*pw : (i+1)*ph*pw]
			for y := range h {
				dst := plane[y*w : (y+1)*w]
				r0 := src[y*pw : y*pw+w+2]
				r1 := src[(y+1)*pw : (y+1)*pw+w+2]
				r2 := src[(y+2)*pw : (y+2)*pw+w+2]
				r0, r1, r2 = r0[:len(dst)+2], r1[:len(dst)+2], r2[:len(dst)+2]
				for x := range dst {
					dst[x] += w00*r0[x] + w01*r0[x+1] + w02*r0[x+2] +
						w10*r1[x] + w11*r1[x+1] + w12*r1[x+2] +
						w20*r2[x] + w21*r2[x+1] + w22*r2[x+2]
				}
			}
		}
	}
	return res
}

// maxpool2 halves height and width by the maximum of each 2×2 block.
func maxpool2(in []float32, c, h, w int) ([]float32, int, int) {
	oh, ow := h/2, w/2
	res := make([]float32, c*oh*ow)
	for ch := range c {
		src := in[ch*h*w:]
		dst := res[ch*oh*ow:]
		for y := range oh {
			for x := range ow {
				a := src[(2*y)*w+2*x]
				b := src[(2*y)*w+2*x+1]
				d := src[(2*y+1)*w+2*x]
				e := src[(2*y+1)*w+2*x+1]
				dst[y*ow+x] = max(a, b, d, e)
			}
		}
	}
	return res, oh, ow
}
