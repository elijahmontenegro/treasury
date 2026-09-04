package verify

import (
	"image"
	"testing"
)

func TestParseNumber(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"45", 45, true}, {"45.0", 45, true}, {"12.5", 12.5, true}, {"1,000", 1000, true},
		{"1.75", 1.75, true}, {"12,5", 12.5, true}, {"1,000.5", 1000.5, true},
		{"1,00", 1, true}, {"0.5", 0.5, true}, {"01", 0, false}, {"007", 0, false}, {"", 0, false}, {".5", 0, false}, {"5.", 0, false}, {"1.2.3", 0, false}, {"1,0000.5", 0, false},
	}
	for _, c := range cases {
		got, ok := parseNumber(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseNumber(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	if n, pct := splitPercent("45.0%"); n != "45.0" || !pct {
		t.Errorf("splitPercent: %q %v", n, pct)
	}
}

func TestRegionXHeight(t *testing.T) {
	// Digits 14 px tall with a period: the x-height is the digits' height
	// over 1.42, and the period does not drag it down.
	comps := []image.Rectangle{image.Rect(0, 0, 8, 14), image.Rect(10, 0, 18, 14), image.Rect(20, 11, 23, 14), image.Rect(25, 0, 33, 14)}
	if xh := regionXHeight(comps); xh < 9.5 || xh > 10.2 {
		t.Errorf("x-height %.2f, want about 9.86", xh)
	}
}
