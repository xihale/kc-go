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

	// 默认日志轮转：单文件 2 MiB，保留 3 份备份（含当前文件最多约 8 MiB）。
	// tmpfs 内存环境，不宜设大。
	defaultLogMaxBytes = 2 * 1024 * 1024
	defaultLogBackups  = 3
)

type Config struct {
	Service struct {
		LogFile    string `yaml:"log_file"`
		LogMaxSize int64  `yaml:"log_max_size"` // 单文件最大字节数，0 用默认 (2MiB)
		LogBackups int    `yaml:"log_backups"`  // 保留旧备份份数，0 用默认 (3)
		Timezone   string `yaml:"timezone"`     // 本地时区如 "CST-8"；空则读 /etc/TZ
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
			IPv4 bool   `yaml:"ipv4"`
			IPv6 bool   `yaml:"ipv6"`
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
	if c.Service.LogMaxSize <= 0 {
		c.Service.LogMaxSize = defaultLogMaxBytes
	}
	if c.Service.LogBackups <= 0 {
		c.Service.LogBackups = defaultLogBackups
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
	for _, d := range c.Cloudflare.Domains {
		if d.Name == "" {
			return fmt.Errorf("cloudflare domains: entry with empty name")
		}
		if !d.IPv4 && !d.IPv6 {
			return fmt.Errorf("cloudflare domain %s: enable at least one of ipv4/ipv6", d.Name)
		}
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
  # log_max_size: %d      # 单文件最大字节，超出后轮转
  # log_backups: %d       # 保留的旧备份份数
  # timezone: "CST-8"     # 本地时区；留空则读 /etc/TZ

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
  # 获取: https://dash.cloudflare.com/profile/api-tokens
  token: %q
  zone_id: %q
  domains:
    # true = 更新对应 A/AAAA 记录
    - name: %q
      ipv4: true
      ipv6: false
`, DefaultLogPath, defaultLogMaxBytes, defaultLogBackups,
		"http://connect.rom.miui.com/generate_204", "YOUR_ACCOUNT", "YOUR_PASSWORD",
		DefaultPortalBaseURL, DefaultPortalACIP, "", "", "example.com")
}
