package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type HetznerConfig struct {
	Token             string `yaml:"token"`
	SSHKeyName        string `yaml:"ssh_key_name"`
	DefaultLocation   string `yaml:"default_location"`
	DefaultServerType string `yaml:"default_server_type"`
	FirewallID        int64  `yaml:"firewall_id,omitempty"`
}

type CloudflareConfig struct {
	Token  string `yaml:"token"`
	ZoneID string `yaml:"zone_id"`
}

type SnapshotConfig struct {
	ID       int64   `yaml:"id,omitempty"`
	Version  string  `yaml:"version,omitempty"`
	DiskSize float32 `yaml:"disk_size,omitempty"`
}

type Config struct {
	Hetzner    HetznerConfig    `yaml:"hetzner"`
	Cloudflare CloudflareConfig `yaml:"cloudflare"`
	Domain     string           `yaml:"domain"`
	Snapshot   SnapshotConfig   `yaml:"snapshot"`
}

func DefaultConfig() *Config {
	return &Config{
		Hetzner: HetznerConfig{
			DefaultLocation:   "nbg1",
			DefaultServerType: "cx33",
		},
		Domain: "specter.tools",
	}
}

func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home directory: %w", err)
	}
	return filepath.Join(home, ".specter"), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func Exists() bool {
	p, err := Path()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config found. Run `specter init` to set up")
		}
		return nil, fmt.Errorf("could not read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config file: %w", err)
	}

	return cfg, nil
}

func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("could not marshal config: %w", err)
	}

	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("could not write config: %w", err)
	}

	return nil
}

func (c *Config) Validate() error {
	if c.Hetzner.Token == "" {
		return fmt.Errorf("hetzner token is required. Run `specter init`")
	}
	if c.Cloudflare.Token == "" {
		return fmt.Errorf("cloudflare token is required. Run `specter init`")
	}
	if c.Cloudflare.ZoneID == "" {
		return fmt.Errorf("cloudflare zone ID is required. Run `specter init`")
	}
	if c.Domain == "" {
		return fmt.Errorf("domain is required. Run `specter init`")
	}
	return nil
}
