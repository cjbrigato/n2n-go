package random

import (
	"testing"
)

func TestRandNotZero(t *testing.T) {
	r := Rand()
	if r == 0 {
		t.Errorf("Expected non-zero random number, got 0")
	}
}

func TestRandSqr(t *testing.T) {
	//r1 := Rand()
	//sqr := r1 * r1 // manual calculation for comparison
	rsqr := RandSqr()
	// Note: Because the generator advances state, we can't compare rsqr with r1*r1 directly.
	// Instead, we can just check that RandSqr returns a number (and not panic).
	if rsqr == 0 {
		t.Errorf("Expected non-zero RandSqr output, got 0")
	}
}

func TestSequence(t *testing.T) {
	// Generate a sequence of random numbers and ensure they're not all equal.
	const count = 1000
	var first uint64 = Rand()
	same := true
	for i := 1; i < count; i++ {
		if Rand() != first {
			same = false
			break
		}
	}
	if same {
		t.Errorf("All random numbers in the sequence are equal (unexpected)")
	}
}
