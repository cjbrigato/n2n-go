// pkg/edge/config.go (Modified)
package edge

import (
	"flag"
	"n2n-go/pkg/protocol"
	"os"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	UDPBufferSize        int           `mapstructure:"udp_buffer_size"` // Use mapstructure for Viper
	EdgeID               string        `mapstructure:"edge_id"`
	Community            string        `mapstructure:"community"`
	TapName              string        `mapstructure:"tap_name"`
	LocalPort            int           `mapstructure:"local_port"`
	SupernodeAddr        string        `mapstructure:"supernode_addr"`
	HeartbeatInterval    time.Duration `mapstructure:"heartbeat_interval"`
	ProtocolVersion      uint8         `mapstructure:"protocol_version"`
	VerifyHash           bool          `mapstructure:"verify_hash"`
	EnableVFuze          bool          `mapstructure:"enable_vfuze"`
	ConfigFile           string        `mapstructure:"config_file"` // Path to the config file
	APIListenAddr        string        `mapstructure:"api_listen_address"`
	EncryptionPassphrase string        `mapstructure:"encryption_passphrase"`
	CompressPayload      bool          `mapstructure:"compress_payload"`
}

func DefaultConfig() *Config {
	return &Config{
		HeartbeatInterval: 30 * time.Second,
		ProtocolVersion:   protocol.VersionV,
		VerifyHash:        true,
		EnableVFuze:       true,
		UDPBufferSize:     8192 * 8192,
		TapName:           "n2n_tap0",
		LocalPort:         0,           // 0 means automatically assigned
		ConfigFile:        "edge.yaml", // Default config file name.
		APIListenAddr:     ":7778",
	}
}

// LoadConfig loads configuration from file, environment, and flags, in that order of precedence.
func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()

	// Use Viper to load configuration
	viper.SetConfigName(cfg.ConfigFile)  // name of config file (without extension)
	viper.SetConfigType("yaml")          // REQUIRED if the config file does not have the extension in the name
	viper.AddConfigPath(".")             // look for config in the working directory
	viper.AddConfigPath("/etc/n2n-go/")  // path to look for the config file in
	viper.AddConfigPath("$HOME/.n2n-go") // call multiple times to add many search paths
	viper.SetEnvPrefix("N2N")            // will be uppercased automatically, N2N_...
	viper.AutomaticEnv()                 // read in environment variables that match

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Config file was found but another error was produced
			return nil, err
		}
		// Config file not found; ignore error if desired
	}

	// Bind command-line flags to Viper
	// This is where we connect our flags to our config struct
	flag.StringVar(&cfg.ConfigFile, "config", cfg.ConfigFile, "Path to the configuration file")
	flag.StringVar(&cfg.EdgeID, "id", cfg.EdgeID, "Unique edge identifier (defaults to hostname if omitted)")
	flag.StringVar(&cfg.Community, "community", cfg.Community, "Community name")
	flag.StringVar(&cfg.TapName, "tap", cfg.TapName, "TAP interface name")
	flag.IntVar(&cfg.LocalPort, "port", cfg.LocalPort, "Local UDP port (0 for system-assigned)")
	flag.BoolVar(&cfg.EnableVFuze, "enableFuze", cfg.EnableVFuze, "enable fuze fastpath")
	flag.StringVar(&cfg.SupernodeAddr, "supernode", cfg.SupernodeAddr, "Supernode address (host:port)")
	flag.DurationVar(&cfg.HeartbeatInterval, "heartbeat", cfg.HeartbeatInterval, "Heartbeat interval")
	flag.IntVar(&cfg.UDPBufferSize, "udpbuffersize", cfg.UDPBufferSize, "UDP BUffer Sizes")
	flag.StringVar(&cfg.APIListenAddr, "api-listen", cfg.APIListenAddr, "API listen address")
	flag.StringVar(&cfg.EncryptionPassphrase, "encryption-passphrase", cfg.EncryptionPassphrase, "Passphrase to encryption key derivation")
	flag.BoolVar(&cfg.CompressPayload, "compress-payload", cfg.CompressPayload, "Add zstd fast compression/decompression to data packets")

	flag.Parse() // MUST call this to parse the flags

	// Unmarshal the config into our struct.
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// Handle defaults that Viper can't.
	if cfg.EdgeID == "" {
		h, err := os.Hostname()
		if err != nil {
			return nil, err // We MUST have an edge ID
		}
		cfg.EdgeID = h
	}
	//If localport is not in config use default
	if cfg.LocalPort == 0 {
		if viper.IsSet("local_port") {
			cfg.LocalPort = viper.GetInt("local_port")
		}
	}

	return cfg, nil
}
