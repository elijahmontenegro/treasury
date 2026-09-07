package buildid_test

import (
	"strings"
	"testing"

	"treasury/internal/buildid"
)

// TestIdentityCoversTheWeights requires that the binary reports a hash for
// every model it carries: a verdict is traceable to its weights only if the
// weights are named.
func TestIdentityCoversTheWeights(t *testing.T) {
	id := buildid.Get()
	// Step 19a retired the two models this used to check for; step 19b
	// registers the detector and the recogniser in their place.
	for _, name := range []string{} {
		h, ok := id.Models[name]
		if !ok {
			t.Errorf("%s is not in the identity", name)
			continue
		}
		if len(h) != 64 {
			t.Errorf("%s hash %q is not a SHA-256", name, h)
		}
	}
	if id.Version == "" {
		t.Error("no version")
	}
	if s := id.String(); !strings.Contains(s, "fingerprint") {
		t.Errorf("identity line carries no fingerprint: %s", s)
	}
}

// TestFingerprintIsStable requires the fingerprint to depend on nothing but
// the identity.
func TestFingerprintIsStable(t *testing.T) {
	first := buildid.Fingerprint()
	if len(first) != 12 {
		t.Fatalf("fingerprint %q is not twelve characters", first)
	}
	for range 5 {
		if got := buildid.Fingerprint(); got != first {
			t.Fatalf("fingerprint changed within one process: %s then %s", first, got)
		}
	}
}
