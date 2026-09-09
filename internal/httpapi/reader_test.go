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
	if required() {
		t.Fatalf("this machine is required to have a reader and does not: %v", err)
	}
	t.Skipf("the reader is not available here: %v", err)
}

// needData is the same argument about the other input. The service gate
// and the three-hundred-label gate both read the fifty out of the tree,
// and both skipped when it was not there - so a checkout that had lost
// eval/real50, or a test run from somewhere the relative path did not
// reach, would have gone green having verified nothing. That is the
// library defect exactly, with data in place of the library.
func needData(t *testing.T, why string) {
	t.Helper()
	if required() {
		t.Fatalf("this machine is required to have the evaluation labels and does not: %s", why)
	}
	t.Skip(why)
}

// required is the single switch. CI sets it, and where it is set a
// missing input is a failure rather than a quiet pass.
func required() bool { return os.Getenv("TREASURY_REQUIRE_READER") != "" }
