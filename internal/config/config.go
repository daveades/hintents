// Copyright 2025 Erst Users
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dotandev/hintents/internal/errors"
)

type Network string

const (
	NetworkPublic     Network = "public"
	NetworkTestnet    Network = "testnet"
	NetworkFuturenet  Network = "futurenet"
	NetworkStandalone Network = "standalone"
)

var validNetworks = map[string]bool{
	string(NetworkPublic):     true,
	string(NetworkTestnet):    true,
	string(NetworkFuturenet):  true,
	string(NetworkStandalone): true,
}

type Config struct {
	RpcUrl            string   `json:"rpc_url,omitempty"`
	RpcUrls           []string `json:"rpc_urls,omitempty"`
	Network           Network  `json:"network,omitempty"`
	NetworkPassphrase string   `json:"network_passphrase,omitempty"`
	SimulatorPath     string   `json:"simulator_path,omitempty"`
	LogLevel          string   `json:"log_level,omitempty"`
	CachePath         string   `json:"cache_path,omitempty"`
	RPCToken          string   `json:"rpc_token,omitempty"`
	CrashReporting    bool     `json:"crash_reporting,omitempty"`
	CrashEndpoint     string   `json:"crash_endpoint,omitempty"`
	CrashSentryDSN    string   `json:"crash_sentry_dsn,omitempty"`
	RequestTimeout    int      `json:"request_timeout,omitempty"`
}

func GetGeneralConfigPath() (string, error) {
	configDir, err := GetConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.json"), nil
}

func LoadConfig() (*Config, error) {
	configPath, err := GetGeneralConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errors.WrapConfigError("failed to read config file", err)
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, errors.WrapConfigError("failed to parse config file", err)
	}

	return config, nil
}

// Load loads the configuration from environment variables and TOML files.
// The lifecycle follows three distinct phases: load, merge defaults, validate.
func Load() (*Config, error) {
	cfg := &Config{}

	// Phase 1: Load from environment variables and config files.
	if err := loadFromEnv(cfg); err != nil {
		return nil, err
	}
	if err := cfg.loadFromFile(); err != nil {
		return nil, err
	}

	// Phase 2: Merge defaults for any fields still unset.
	cfg.MergeDefaults()

	// Phase 3: Validate.
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// SaveConfig saves the configuration to disk (JSON format)
func SaveConfig(config *Config) error {
	configPath, err := GetGeneralConfigPath()
	if err != nil {
		return err
	}

	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return errors.WrapConfigError("failed to create config directory", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return errors.WrapConfigError("failed to marshal config", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return errors.WrapConfigError("failed to write config file", err)
	}

	return nil
}

// MergeDefaults fills in any unset fields with their default values.
func (c *Config) MergeDefaults() {
	if c.RpcUrl == "" {
		c.RpcUrl = defaultConfig.RpcUrl
	}
	if c.Network == "" {
		c.Network = defaultConfig.Network
	}
	if c.SimulatorPath == "" {
		c.SimulatorPath = defaultConfig.SimulatorPath
	}
	if c.LogLevel == "" {
		c.LogLevel = defaultConfig.LogLevel
	}
	if c.CachePath == "" {
		c.CachePath = defaultConfig.CachePath
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = defaultRequestTimeout
	}
}

func (c *Config) Validate() error {
	return RunValidators(c, DefaultValidators())
}

func (c *Config) NetworkURL() string {
	switch c.Network {
	case NetworkPublic:
		return "https://soroban.stellar.org"
	case NetworkTestnet:
		return "https://soroban-testnet.stellar.org"
	case NetworkFuturenet:
		return "https://soroban-futurenet.stellar.org"
	case NetworkStandalone:
		return "http://localhost:8000"
	default:
		return c.RpcUrl
	}
}

func (c *Config) String() string {
	return fmt.Sprintf(
		"Config{RPC: %s, Network: %s, LogLevel: %s, CachePath: %s}",
		c.RpcUrl, c.Network, c.LogLevel, c.CachePath,
	)
}

// RequiredFieldsValidator checks that required fields are present.
type RequiredFieldsValidator struct{}

func (RequiredFieldsValidator) Validate(cfg *Config) error {
	if cfg.RpcUrl == "" {
		return errors.WrapValidationError("rpc_url cannot be empty")
	}
	return nil
}

// RequestTimeoutValidator checks that request_timeout is within bounds.
type RequestTimeoutValidator struct{}

func (RequestTimeoutValidator) Validate(cfg *Config) error {
	if cfg.RequestTimeout == 0 {
		return nil
	}
	if cfg.RequestTimeout < 1 || cfg.RequestTimeout > maxRequestTimeout {
		return errors.WrapValidationError(fmt.Sprintf("request_timeout must be between 1 and %d", maxRequestTimeout))
	}
	return nil
}
