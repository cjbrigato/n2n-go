// Package random provides a pseudo-random number generator based on XORSHIFT128+,
// with a period of 2^128. It attempts to use crypto/rand for seeding.
package random

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	mathrand "math/rand"
	"time"
)

// XORShift128Plus implements the XORSHIFT128+ algorithm.
type XORShift128Plus struct {
	state [2]uint64
}

// Rand returns a 64-bit random number and updates the internal state.
func (x *XORShift128Plus) Rand() uint64 {
	s1 := x.state[0]
	s0 := x.state[1]
	result := s0 + s1

	x.state[0] = s0
	s1 ^= s1 << 23
	x.state[1] = s1 ^ s0 ^ (s1 >> 17) ^ (s0 >> 26)
	return result
}

// RandSqr returns the square (mod 2^64) of a random number.
func (x *XORShift128Plus) RandSqr() uint64 {
	r := x.Rand()
	return r * r
}

var defaultGen XORShift128Plus

// init seeds the default generator.
func init() {
	err := SeedDefault()
	if err != nil {
		// If seeding fails, panic – secure randomness is critical.
		panic(err)
	}
}

// SeedDefault seeds the default XORShift128Plus generator using crypto/rand.
// If crypto/rand fails, it falls back to using math/rand seeded by current time.
func SeedDefault() error {
	var seedBytes [16]byte
	_, err := rand.Read(seedBytes[:])
	if err != nil {
		// Fall back to math/rand seeding if crypto/rand fails.
		mathrand.Seed(time.Now().UnixNano())
		for i := 0; i < 16; i++ {
			seedBytes[i] = byte(mathrand.Intn(256))
		}
	}
	defaultGen.state[0] = binary.LittleEndian.Uint64(seedBytes[0:8])
	defaultGen.state[1] = binary.LittleEndian.Uint64(seedBytes[8:16])
	// Ensure the state is non-zero.
	if defaultGen.state[0] == 0 && defaultGen.state[1] == 0 {
		return errors.New("random: invalid seed (all zero state)")
	}
	return nil
}

// Rand returns a 64-bit random number from the default generator.
func Rand() uint64 {
	return defaultGen.Rand()
}

// RandSqr returns the square (mod 2^64) of a random number from the default generator.
func RandSqr() uint64 {
	return defaultGen.RandSqr()
}
