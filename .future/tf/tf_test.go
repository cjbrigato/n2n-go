package tf

import (
	"bytes"
	"testing"
)

func TestTFTransform(t *testing.T) {
	data := []byte("This is some test data for TF transform.")
	key := []byte("secret")
	transformed := Transform(data, key)
	inversed := Transform(transformed, key)
	if !bytes.Equal(data, inversed) {
		t.Errorf("TF inverse transform failed: expected %q, got %q", data, inversed)
	}
}
