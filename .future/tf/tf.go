// Package tf implements a simple transform using a repeating XOR key.
package tf

// Transform applies a simple XOR transform to data using the provided key.
// The same function is used for both transformation and its inverse.
func Transform(data, key []byte) []byte {
	out := make([]byte, len(data))
	if len(key) == 0 {
		copy(out, data)
		return out
	}
	for i, b := range data {
		out[i] = b ^ key[i%len(key)]
	}
	return out
}
