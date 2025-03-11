package util

import "testing"

func TestMinMax(t *testing.T) {
	if Min(3, 5) != 3 {
		t.Errorf("Min(3, 5) expected 3")
	}
	if Max(3, 5) != 5 {
		t.Errorf("Max(3, 5) expected 5")
	}
	if Min(10, -5) != -5 {
		t.Errorf("Min(10, -5) expected -5")
	}
	if Max(10, -5) != 10 {
		t.Errorf("Max(10, -5) expected 10")
	}
}
