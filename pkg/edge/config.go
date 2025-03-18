package edge

import (
	"n2n-go/pkg/protocol"
	"os"
	"time"
)

// Config holds the edge node configuration
type Config struct {
	UDPBufferSize     int
	EdgeID            string
	Community         string
	TapName           string
	LocalPort         int
	SupernodeAddr     string
	HeartbeatInterval time.Duration
	ProtocolVersion   uint8 // Protocol version to use
	VerifyHash        bool  // Whether to verify community hash
	EnableVFuze       bool  // if enabled, experimental fastpath when known peer
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		HeartbeatInterval: 30 * time.Second,
		ProtocolVersion:   protocol.VersionV,
		VerifyHash:        true, // Verify community hash by default
		EnableVFuze:       true,
		UDPBufferSize:     8192 * 8192,
	}
}

func (c *Config) Defaults() error {
	if c.EdgeID == "" {
		h, err := os.Hostname()
		if err != nil {
			return err
		}
		c.EdgeID = h
	}

	// Set default values if not provided
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 30 * time.Second
	}
	return nil
}
