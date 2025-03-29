package supernode

import (
	"crypto/rand"
	"encoding/hex"
	"n2n-go/pkg/crypto"
)

func GenerateSecureToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
func (s *Supernode) DecryptMachineID(encMachineID []byte) ([]byte, error) {
	machineID, err := crypto.DecryptSequence(encMachineID, s.SNSecrets.Priv)
	if err != nil {
		return nil, err
	}
	return machineID, nil
}
