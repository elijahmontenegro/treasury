package verify

import (
	"math"
	"testing"
)

// TestAdoptedValuesAreLive is the check step 10c needed and did not have.
// Eight of the ten constants that step adopted live outside the engine's
// options, were only ever applied through the sweep's override map, and were
// never written into the code: from 10c to 14d the doc described an engine
// that was not running. This test reads the live value of every adopted
// constant and requires it to be the value recorded.
//
// It fails if a constant is put back to what the code carried before 14d:
// set unitBound's fallback to 1.6, or digits.FrameW to 1.6, and this test
// says so.
func TestAdoptedValuesAreLive(t *testing.T) {
	if len(Adopted) == 0 {
		t.Fatal("nothing recorded as adopted")
	}
	seen := map[string]bool{}
	for _, c := range Adopted {
		if seen[c.Where] {
			t.Errorf("%s is recorded twice", c.Where)
		}
		seen[c.Where] = true
		got, err := Live(c)
		if err != nil {
			t.Errorf("%s (%s): %v", c.Name, c.Where, err)
			continue
		}
		if math.Abs(got-c.Value) > 1e-9 {
			t.Errorf("%s: the doc records %s adopted at %g in step %s, the engine runs at %g",
				c.Name, c.Where, c.Value, c.Step, got)
		}
	}
}

// TestEveryTunableIsRecorded keeps the two lists together: a constant the
// sweep can set is one the build can adopt, so it belongs in Adopted with
// whatever value it is at.
func TestEveryTunableIsRecorded(t *testing.T) {
	recorded := map[string]bool{}
	for _, c := range Adopted {
		recorded[c.Name] = true
	}
	for _, name := range TuneNames {
		if !recorded[name] {
			t.Errorf("%q can be swept and is not recorded in Adopted", name)
		}
	}
}

// TestOverrideReachesTheCode is the check the 10c sweep needed. Its first
// pass reported all thirty-three constants perfectly insensitive, because
// the overrides that live in the engine's own options were never applied;
// a sweep that reports no effect anywhere is more likely broken than
// informative. Every name is set to a probe value through the same two
// functions the engine uses, and the destination must carry it.
func TestOverrideReachesTheCode(t *testing.T) {
	// The sweep writes package variables; put them back whatever happens.
	// A value no default is, so that "it reached the code" and "it was
	// already that" cannot be confused. Constants that are counts take a
	// whole number, since a fraction truncates to zero and zero is how the
	// engine says "not set".
	for _, c := range Adopted {
		if c.Name == "" {
			continue
		}
		probe := 0.4242
		if c.Value == math.Trunc(c.Value) {
			probe = 7
		}
		// A probe has to be a setting the engine can actually run at, and
		// second_opinion is the one constant where an arbitrary whole
		// number is not. Mode 2 asks for the 90 MB server recogniser,
		// which is fetched rather than committed, and asking for it
		// without having it is now an error rather than a silent fall
		// back to the shipped model - which is the point of that change,
		// and which turned this probe into a test that passed only on a
		// machine that happened to have the file. CI does not, and said
		// so. Mode 1 is a real setting, is not the default, and needs no
		// file.
		if c.Name == "second_opinion" {
			probe = 1
		}
		tune := map[string]float64{c.Name: probe}
		// For the constants that live in the engine's options the path
		// under test is the engine's own: New applies the overrides, and
		// 10c's defect was that it did not. For the rest, which the
		// engine applies inside a verification, the same function is
		// called directly, which is as far as a unit test reaches.
		eng, err := New(Options{Tune: tune})
		if err != nil {
			t.Fatal(err)
		}
		got, err := liveIn(c, eng.opt)
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if math.Abs(got-probe) > 1e-9 {
			t.Errorf("%s: an override of %g did not reach %s, which reads %g", c.Name, probe, c.Where, got)
		}
	}
}
