// Package bitcode is a fixed-length bit vector with Hamming distance.
package bitcode

import "math/bits"

// Code holds bits little-endian within 64-bit words.
type Code []uint64

// New returns an all-zero code with room for n bits.
func New(n int) Code { return make(Code, (n+63)/64) }

// Set turns bit i on.
func (c Code) Set(i int) { c[i>>6] |= 1 << (uint(i) & 63) }

// Get reports bit i.
func (c Code) Get(i int) bool { return c[i>>6]&(1<<(uint(i)&63)) != 0 }

// Popcount is the number of set bits.
func (c Code) Popcount() int {
	n := 0
	for _, w := range c {
		n += bits.OnesCount64(w)
	}
	return n
}

// Distance is the Hamming distance between a and b, which must have equal length.
func Distance(a, b Code) int {
	n := 0
	for i := range a {
		n += bits.OnesCount64(a[i] ^ b[i])
	}
	return n
}

// Majority returns the per-bit majority of codes over n bits; ties are set.
func Majority(codes []Code, n int) Code {
	out := New(n)
	if len(codes) == 0 {
		return out
	}
	for i := range n {
		k := 0
		for _, c := range codes {
			if c.Get(i) {
				k++
			}
		}
		if 2*k >= len(codes) {
			out.Set(i)
		}
	}
	return out
}
