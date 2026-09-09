package httpapi_test

import (
	"os"
	"testing"
)

// needReader is what a test calls when it could not build an engine.
//
// On a workstation without the ONNX Runtime library, skipping is right: a
// test that cannot read a label has nothing to say about reading labels.
// On a machine that is supposed to have it, skipping is the worst possible
// answer, because the suite goes green having tested nothing — which is
// what CI did for two days from step 19b, where the runner had no library,
// the tests in this package skipped, and the steps that call themselves
// the gate reported success.
//
// So the environment decides. CI sets TREASURY_REQUIRE_READER, and there a
// missing reader is a failure that names itself.
func needReader(t *testing.T, err error) {
	t.Helper()
	if os.Getenv("TREASURY_REQUIRE_READER") != "" {
		t.Fatalf("this machine is required to have a reader and does not: %v", err)
	}
	t.Skipf("the reader is not available here: %v", err)
}
