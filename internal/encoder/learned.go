package encoder

import "errors"

// NewLearned returns the embedded contrastive glyph encoder. Until the
// encoder is trained and exported (python/encoder), there is none.
func NewLearned() (Encoder, error) {
	return nil, errors.New("encoder: no learned encoder is built into this binary")
}
