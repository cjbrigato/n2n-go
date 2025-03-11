// Package pearson implements Pearson hashing as described in
// Peter K. Pearson’s 1990 paper “Fast Hashing of Variable-Length Data”.
// The default algorithm produces an 8-bit hash. For applications that require
// a larger hash, Hash64 runs eight independent Pearson hashes (with different seeds)
// and concatenates the results into a 64-bit value.
package pearson

// Default permutation table.
// This table is a fixed permutation of the numbers 0..255.
// (Many implementations use Pearson's original table; you may change this
// table if desired as long as it contains all numbers 0..255 exactly once.)
var table = [256]uint8{
	98,   6,  85, 150, 36,  23, 112, 164,
	135, 212, 179, 37,  25,  10,  72,  35,
	8,    81, 95,  21,  50,  63,  29,  89,
	7,    113, 87,  62,  101, 45,  33,  2,
	17,   59,  23,  73,  100, 65,  111, 53,
	51,   60,  35,  45,  63,  12,  4,   8,
	75,   99,  76,  33,  45,  90,  10,  37,
	14,   22,  21,  74,  50,  60,  73,  29,
	68,   53,  80,  13,  99,  77,  43,  8,
	2,    105, 14,  97,  21,  66,  41,  72,
	90,   15,  63,  20,  11,  81,  42,  33,
	55,   44,  13,  78,  62,  91,  70,  25,
	43,   19,  45,  67,  71,  83,  19,  66,
	12,   39,  88,  26,  58,  72,  23,  9,
	37,   89,  90,  43,  55,  66,  78,  12,
	14,   92,  83,  40,  29,  10,  5,   73,
	94,   92,  30,  22,  18,  49,  15,  80,
	56,   41,  73,  99,  68,  77,  16,  44,
	65,   51,  36,  17,  78,  24,  86,  11,
	95,   42,  53,  30,  89,  14,  29,  75,
	47,   82,  59,  28,  61,  33,  10,  72,
	56,   85,  90,  16,  52,  7,   93,  25,
	8,    99,  70,  31,  45,  68,  40,  19,
	77,   22,  58,  86,  34,  81,  12,  69,
	24,   50,  16,  41,  83,  95,  27,  72,
	60,   39,  37,  18,  47,  85,  31,  90,
	13,   76,  22,  61,  40,  58,  27,  92,
	14,   69,  35,  73,  11,  87,  18,  49,
	67,   90,  30,  24,  71,  15,  80,  55,
	38,   93,  21,  66,  29,  84,  12,  45,
	59,   37,  82,  17,  94,  23,  71,  9,
	53,   48,  16,  62,  34,  77,  25,  90,
}

// Hash computes the 8-bit Pearson hash of the input data.
// If data is empty, it returns 0.
func Hash(data []byte) uint8 {
	if len(data) == 0 {
		return 0
	}
	h := table[data[0]]
	for i := 1; i < len(data); i++ {
		h = table[h^data[i]]
	}
	return h
}

// Hash64 computes a 64-bit hash by performing the Pearson hash algorithm
// eight times with different initial seeds (0,1,...,7) and concatenating the results.
func Hash64(data []byte) uint64 {
	var h uint64
	// For each seed from 0 to 7, compute an 8-bit hash and append it.
	for seed := 0; seed < 8; seed++ {
		var hash uint8 = table[uint8(seed)^data[0]]
		for i := 1; i < len(data); i++ {
			hash = table[hash^data[i]]
		}
		h = (h << 8) | uint64(hash)
	}
	return h
}
