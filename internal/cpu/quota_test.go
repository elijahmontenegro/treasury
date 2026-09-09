package cpu

import (
	"runtime"
	"testing"
)

// TestRatioReadsAQuota pins the arithmetic, which is the only part of this
// package that can be wrong without a container to run in: a quota is a
// share of a period, an unlimited one is not a number, and a fraction of a
// core still needs somewhere to run.
func TestRatioReadsAQuota(t *testing.T) {
	for _, c := range []struct {
		max, period string
		want        int
		ok          bool
	}{
		{"200000", "100000", 2, true}, // two vCPU, the shape this deploys at
		{"100000", "100000", 1, true}, // one
		{"400000", "100000", 4, true}, // four
		{"50000", "100000", 1, true},  // half a core is still a core to schedule on
		{"250000", "100000", 3, true}, // two and a half rounds up
		{"0", "100000", 0, false},     // nonsense
		{"200000", "0", 0, false},     // nonsense
		{"nonsense", "100000", 0, false},
	} {
		got, ok := ratio(c.max, c.period)
		if ok != c.ok || got != c.want {
			t.Errorf("ratio(%q, %q) = %d, %v; want %d, %v",
				c.max, c.period, got, ok, c.want, c.ok)
		}
	}
}

// TestAvailableIsSane covers what can be checked anywhere: whatever this
// machine is, the answer is at least one core and never more than the
// machine has. On a workstation there is no cgroup and it is NumCPU; the
// container case is measured in step 30f rather than mocked here, since a
// fake /sys would test the fake.
func TestAvailableIsSane(t *testing.T) {
	n := Available()
	if n < 1 {
		t.Errorf("Available() = %d, want at least one", n)
	}
	if n > runtime.NumCPU() {
		t.Errorf("Available() = %d, more than the %d this machine has", n, runtime.NumCPU())
	}
}
