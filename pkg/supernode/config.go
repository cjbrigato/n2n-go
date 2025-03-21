// pkg/supernode/config.go (Modified)
package supernode

import (
	"time"

	"flag"

	"github.com/spf13/viper"
)

type Config struct {
	Debug               bool          `mapstructure:"debug"`
	CommunitySubnet     string        `mapstructure:"community_subnet"`
	CommunitySubnetCIDR int           `mapstructure:"community_subnet_cidr"`
	ExpiryDuration      time.Duration `mapstructure:"expiry_duration"`
	CleanupInterval     time.Duration `mapstructure:"cleanup_interval"`
	UDPBufferSize       int           `mapstructure:"udp_buffer_size"`
	StrictHashChecking  bool          `mapstructure:"strict_hash_checking"`
	EnableVFuze         bool          `mapstructure:"enable_vfuze"`
	ListenAddr          string        `mapstructure:"listen_address"` // Listen address
	ConfigFile          string        `mapstructure:"config_file"`    //Path to config
}

func DefaultConfig() *Config {
	return &Config{
		Debug:               false,
		CommunitySubnet:     "10.128.0.0",
		CommunitySubnetCIDR: 24,
		ExpiryDuration:      10 * time.Minute,
		CleanupInterval:     5 * time.Minute,
		UDPBufferSize:       2048 * 2048, // was 1024 * 1024
		StrictHashChecking:  true,
		EnableVFuze:         true,
		ListenAddr:          ":7777", // Default listen address
		ConfigFile:          "supernode.yaml",
	}
}

// LoadConfig loads configuration from file, environment, and flags.
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
	// Bind command-line flags to Viper.  VERY IMPORTANT!
	flag.StringVar(&cfg.ConfigFile, "config", cfg.ConfigFile, "Path to the configuration file")
	flag.StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "UDP listen address")
	flag.DurationVar(&cfg.CleanupInterval, "cleanup", cfg.CleanupInterval, "Cleanup interval for stale edges")
	flag.DurationVar(&cfg.ExpiryDuration, "expiry", cfg.ExpiryDuration, "Edge expiry duration")
	flag.BoolVar(&cfg.Debug, "debug", cfg.Debug, "Enable debug logging")
	flag.BoolVar(&cfg.EnableVFuze, "enableFuze", cfg.EnableVFuze, "enable fuze fastpath")
	flag.StringVar(&cfg.CommunitySubnet, "subnet", cfg.CommunitySubnet, "Base subnet for communities")
	flag.IntVar(&cfg.CommunitySubnetCIDR, "subnetcidr", cfg.CommunitySubnetCIDR, "CIDR prefix length for community subnets")

	flag.Parse() // Parse the flags!

	// Unmarshal the config into our struct.
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
