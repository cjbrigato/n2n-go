// Package n2nregex provides a simple interface for compiling and matching regular expressions,
// reimplementing the functionality of n2n_regex.c/h using Go's built-in regexp package.
package n2nregex

import (
	"regexp"
)

// Regex wraps a compiled regular expression.
type Regex struct {
	re *regexp.Regexp
}

// Compile compiles the regular expression pattern and returns a Regex instance.
func Compile(pattern string) (*Regex, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &Regex{re: re}, nil
}

// MustCompile compiles the regular expression pattern and panics if the pattern is invalid.
func MustCompile(pattern string) *Regex {
	return &Regex{re: regexp.MustCompile(pattern)}
}

// Match returns true if the regular expression matches the entire input string.
func (r *Regex) Match(s string) bool {
	return r.re.MatchString(s)
}

// Find returns the first match of the regular expression in the input string,
// including any submatches. If no match is found, it returns nil.
func (r *Regex) Find(s string) []string {
	return r.re.FindStringSubmatch(s)
}
