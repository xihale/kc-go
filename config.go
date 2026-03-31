package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigDir     = "/etc/kc-go"
	DefaultConfigPath    = "/etc/kc-go/config.yaml"
	DefaultLogPath       = "/var/log/kc-go.log"
	DefaultBinaryPath    = "/usr/bin/kc-go"
	DefaultInitPath      = "/etc/init.d/kc-go"
	DefaultPIDPath       = "/var/run/kc-go.pid"
	DefaultPortalBaseURL = "http://10.0.3.2:801"
	DefaultPortalACIP    = "172.16.254.2"
	ServiceName          = "kc-go"
)

type Config struct {
	Service struct {
		LogFile string `yaml:"log_file"`
	} `yaml:"service"`
	Check struct {
		URL      string `yaml:"url"`
		Interval int    `yaml:"interval"`
	} `yaml:"check"`
	Account struct {
		User     string `yaml:"user"`
		Password string `yaml:"password"`
	} `yaml:"account"`
	Portal struct {
		BaseURL string `yaml:"base_url"`
		ACIP    string `yaml:"ac_ip"`
	} `yaml:"portal"`
	Cloudflare struct {
		Token   string `yaml:"token"`
		ZoneID  string `yaml:"zone_id"`
		Domains []struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		} `yaml:"domains"`
	} `yaml:"cloudflare"`
}

func LoadConfig(path string) (*Config, error) {
	var cfg Config

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", path, err)
	}
	defer file.Close()

	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config %s: %w", path, err)
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Service.LogFile == "" {
		c.Service.LogFile = DefaultLogPath
	}
	if c.Check.URL == "" {
		c.Check.URL = "http://connect.rom.miui.com/generate_204"
	}
	if c.Check.Interval <= 0 {
		c.Check.Interval = 1
	}
	if c.Check.Interval > 300 {
		return fmt.Errorf("check interval %d is too large (max 300)", c.Check.Interval)
	}
	if c.Portal.BaseURL == "" {
		c.Portal.BaseURL = DefaultPortalBaseURL
	}
	if c.Portal.ACIP == "" {
		c.Portal.ACIP = DefaultPortalACIP
	}
	return nil
}

func ResolveConfigPath(explicitPath string) string {
	if explicitPath != "" {
		return explicitPath
	}
	if _, err := os.Stat(DefaultConfigPath); err == nil {
		return DefaultConfigPath
	}
	return "config.yaml"
}

func ResolveLogPathFromConfig(path string) string {
	cfg, err := LoadConfig(path)
	if err != nil || cfg.Service.LogFile == "" {
		return DefaultLogPath
	}
	return cfg.Service.LogFile
}

func DefaultConfigTemplate() string {
	return fmt.Sprintf(`service:
  log_file: %q

check:
  url: %q
  interval: 1

account:
  user: %q
  password: %q

portal:
  base_url: %q
  ac_ip: %q

cloudflare:
  token: %q
  zone_id: %q
  domains:
    - name: %q
      type: %q
`, DefaultLogPath, "http://connect.rom.miui.com/generate_204", "YOUR_ACCOUNT", "YOUR_PASSWORD",
		DefaultPortalBaseURL, DefaultPortalACIP, "", "", "example.com", "A")
}
