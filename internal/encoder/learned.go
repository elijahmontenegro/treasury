package encoder

import (
	_ "embed"
	"fmt"
	"sync"

	"treasury/internal/bitcode"
	"treasury/internal/buildid"
	"treasury/internal/nn"
)

// The contrastive glyph encoder, trained by python/encoder on every claim
// character in every installed face through the label channel, with the
// same character in different faces as positives: a 256-bit code whose
// Hamming distance is small across faces for the same character and large
// between characters. It replaces the dual hash for decoding claims, where
// the glyphs are in faces the reference never showed; the alphabet itself
// is still learned with the hash.

//go:embed learned.bin
var learnedBin []byte

//go:embed learned.json
var learnedJSON []byte

func init() {
	buildid.Register("encoder.bin", learnedBin)
	buildid.Register("encoder.json", learnedJSON)
}

// Learned is the embedded contrastive encoder.
type Learned struct {
	net *nn.Model
}

var (
	learnedOnce sync.Once
	learnedEnc  *Learned
	learnedErr  error
)

// NewLearned returns the embedded contrastive glyph encoder, decoding it
// once.
func NewLearned() (Encoder, error) {
	learnedOnce.Do(func() {
		net, err := nn.Decode(learnedBin, learnedJSON)
		if err != nil {
			learnedErr = fmt.Errorf("encoder: learned: %w", err)
			return
		}
		if net.Spec.Dim <= 0 {
			learnedErr = fmt.Errorf("encoder: learned model has no output dimension")
			return
		}
		learnedEnc = &Learned{net: net}
	})
	if learnedErr != nil {
		return nil, learnedErr
	}
	return learnedEnc, nil
}

// Bits is the code length.
func (l *Learned) Bits() int { return l.net.Spec.Dim }

// Name identifies the encoder in evidence.
func (l *Learned) Name() string { return fmt.Sprintf("learned%d", l.net.Spec.Dim) }

// Encode area-averages the patch onto the network's input grid, runs it,
// and takes the sign of every coordinate as a bit.
func (l *Learned) Encode(p Patch) bitcode.Code {
	code := bitcode.New(l.Bits())
	if p.Bin == nil {
		return code
	}
	side := l.net.Spec.Side
	cov := Coverage(p.Bin, side, side)
	in := make([]float32, len(cov))
	for i, v := range cov {
		in[i] = float32(v)
	}
	z := l.net.Forward(in)
	for i, v := range z {
		if v > 0 {
			code.Set(i)
		}
	}
	return code
}

// Costly reports that Encode runs a network.
func (l *Learned) Costly() bool { return true }
