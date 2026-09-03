package bitcode

import "testing"

func TestDistance(t *testing.T) {
	a, b := New(130), New(130)
	for _, i := range []int{0, 5, 63, 64, 129} {
		a.Set(i)
	}
	for _, i := range []int{0, 6, 64, 128} {
		b.Set(i)
	}
	if !a.Get(129) || a.Get(128) || a.Popcount() != 5 {
		t.Fatalf("Set/Get/Popcount broken")
	}
	// Symmetric difference: a has 5, 63, 129 alone; b has 6, 128 alone.
	if d := Distance(a, b); d != 5 {
		t.Errorf("Distance = %d, want 5", d)
	}
	if d := Distance(a, a); d != 0 {
		t.Errorf("self distance = %d", d)
	}
}

func TestMajority(t *testing.T) {
	mk := func(bits ...int) Code {
		c := New(8)
		for _, b := range bits {
			c.Set(b)
		}
		return c
	}
	m := Majority([]Code{mk(0, 1), mk(0, 2), mk(0, 3)}, 8)
	if !m.Get(0) || m.Get(1) || m.Get(2) || m.Get(3) {
		t.Errorf("majority of 3 wrong: %v", m)
	}
	tie := Majority([]Code{mk(4), mk(5)}, 8)
	if !tie.Get(4) || !tie.Get(5) {
		t.Errorf("ties must be set: %v", tie)
	}
}
