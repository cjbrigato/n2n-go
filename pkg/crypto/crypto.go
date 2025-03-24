package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
)

type SNSecrets struct {
	Priv *rsa.PrivateKey
	Pub  *rsa.PublicKey
	Pem  []byte
}

func GenSNSecrets() (*SNSecrets, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate key: %w", err)
	}
	publicKeyPEM := marshalPublicKey(&privateKey.PublicKey)
	return &SNSecrets{
		Priv: privateKey,
		Pub:  &privateKey.PublicKey,
		Pem:  publicKeyPEM,
	}, nil
}

func PublicKeyFromPEMData(pemdata []byte) (*rsa.PublicKey, error) {
	publicKey, err := parsePublicKey(pemdata)
	if err != nil {
		return nil, err
	}
	return publicKey, nil
}

func DecryptSequence(sequence []byte, privKey *rsa.PrivateKey) ([]byte, error) {
	decrypted, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		privKey,
		sequence,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to decrypt: %v", err)
	}
	return decrypted, nil
}

func EncryptSequence(sequence []byte, pubKey *rsa.PublicKey) ([]byte, error) {
	encrypted, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		pubKey,
		sequence,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to encrypt: %v", err)
	}
	return encrypted, nil
}

// Helper function to marshal public key to PEM format
func marshalPublicKey(publicKey *rsa.PublicKey) []byte {
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		log.Fatalf("Failed to marshal public key: %v", err)
	}

	pemBlock := &pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: publicKeyBytes,
	}

	return pem.EncodeToMemory(pemBlock)
}

// Helper function to parse public key from PEM format
func parsePublicKey(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block containing the public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	publicKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}

	return publicKey, nil
}
