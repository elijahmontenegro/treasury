//go:build onnx

package digits

import (
	_ "embed"
	"fmt"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// The same network as model.bin, exported to ONNX, run through onnxruntime.
// This path exists for parity with the training side and for a runtime that
// already ships onnxruntime; the default build uses the pure-Go forward
// pass and needs neither cgo nor the library.

//go:embed model.onnx
var modelONNX []byte

// ONNXSession runs the exported network through onnxruntime.
type ONNXSession struct {
	sess *ort.DynamicAdvancedSession
}

var (
	ortOnce sync.Once
	ortErr  error
)

// initORT points onnxruntime_go at the shared library named by
// ONNXRUNTIME_DLL when set (otherwise its default name on the search path)
// and initializes the environment once.
func initORT() error {
	ortOnce.Do(func() {
		if p := os.Getenv("ONNXRUNTIME_DLL"); p != "" {
			ort.SetSharedLibraryPath(p)
		}
		if !ort.IsInitialized() {
			ortErr = ort.InitializeEnvironment()
		}
	})
	return ortErr
}

// NewONNXSession loads the embedded ONNX model.
func NewONNXSession() (*ONNXSession, error) {
	if err := initORT(); err != nil {
		return nil, fmt.Errorf("digits: onnxruntime: %w", err)
	}
	s, err := ort.NewDynamicAdvancedSessionWithONNXData(modelONNX, []string{"frame"}, []string{"logits"}, nil)
	if err != nil {
		return nil, fmt.Errorf("digits: onnx session: %w", err)
	}
	return &ONNXSession{sess: s}, nil
}

// Logits runs one frame and returns one score per class.
func (s *ONNXSession) Logits(frame []float32) ([]float32, error) {
	if len(frame) != Side*Side {
		return nil, fmt.Errorf("digits: frame must be Side×Side")
	}
	in, err := ort.NewTensor(ort.NewShape(1, 1, Side, Side), frame)
	if err != nil {
		return nil, err
	}
	defer in.Destroy()
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(len(Classes))))
	if err != nil {
		return nil, err
	}
	defer out.Destroy()
	if err := s.sess.Run([]ort.Value{in}, []ort.Value{out}); err != nil {
		return nil, err
	}
	return append([]float32(nil), out.GetData()...), nil
}

// Close releases the session.
func (s *ONNXSession) Close() error { return s.sess.Destroy() }
