package n2nregex

import "testing"

func TestRegexMatch(t *testing.T) {
	pattern := "^hello.*world$"
	regex, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}

	tests := []struct {
		input  string
		expect bool
	}{
		{"hello there, world", true},
		{"hello world", true},
		{"helloworld", false},
		{"hello world!", false},
	}

	for _, test := range tests {
		res := regex.Match(test.input)
		if res != test.expect {
			t.Errorf("Match(%q) = %v, expected %v", test.input, res, test.expect)
		}
	}
}

func TestRegexFind(t *testing.T) {
	pattern := "([0-9]+)"
	regex, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q) failed: %v", pattern, err)
	}
	input := "The answer is 42."
	submatches := regex.Find(input)
	if len(submatches) < 2 || submatches[1] != "42" {
		t.Errorf("Expected to find \"42\", got %v", submatches)
	}
}
