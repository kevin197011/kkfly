package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	// Required fields (per project request)
	User              string   `yaml:"user"`
	PrivateKeyPath    string   `yaml:"private_key_path,omitempty"`
	PrivateKeyContent string   `yaml:"private_key_content,omitempty"`
	Port              int      `yaml:"port"`
	Hosts             []string `yaml:"hosts"`
	Concurrency       int      `yaml:"concurrency"`

	// Required behavior
	Command string `yaml:"command"`
	Sudo    bool   `yaml:"sudo,omitempty"`

	// Optional security knobs
	KnownHostsPath           string `yaml:"known_hosts_path,omitempty"`
	StrictHostKeyChecking    *bool  `yaml:"strict_host_key_checking,omitempty"`
	ConnectTimeoutSeconds    int    `yaml:"connect_timeout_seconds,omitempty"`
	CommandTimeoutSeconds    int    `yaml:"command_timeout_seconds,omitempty"`
	MaxOutputBytesPerStream  int    `yaml:"max_output_bytes_per_stream,omitempty"`
	DisableStdoutStderrPrint bool   `yaml:"disable_stdout_stderr_print,omitempty"`
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}

	if err := cfg.NormalizeAndValidate(filepath.Dir(path)); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) NormalizeAndValidate(configDir string) error {
	if c.Port == 0 {
		c.Port = 22
	}
	if c.ConnectTimeoutSeconds <= 0 {
		c.ConnectTimeoutSeconds = 10
	}
	if c.CommandTimeoutSeconds <= 0 {
		c.CommandTimeoutSeconds = 15 * 60
	}
	if c.MaxOutputBytesPerStream <= 0 {
		c.MaxOutputBytesPerStream = 256 * 1024
	}

	c.PrivateKeyPath = expandUserPath(c.PrivateKeyPath)
	c.KnownHostsPath = expandUserPath(c.KnownHostsPath)

	// Allow relative key path relative to config file directory.
	if c.PrivateKeyPath != "" && !filepath.IsAbs(c.PrivateKeyPath) {
		c.PrivateKeyPath = filepath.Join(configDir, c.PrivateKeyPath)
	}
	if c.KnownHostsPath != "" && !filepath.IsAbs(c.KnownHostsPath) {
		c.KnownHostsPath = filepath.Join(configDir, c.KnownHostsPath)
	}

	var errs []error
	if c.User == "" {
		errs = append(errs, errors.New("user is required"))
	}
	hasKeyContent := strings.TrimSpace(c.PrivateKeyContent) != ""
	hasKeyPath := strings.TrimSpace(c.PrivateKeyPath) != ""
	if !hasKeyContent && !hasKeyPath {
		errs = append(errs, errors.New("either private_key_path or private_key_content is required"))
	}
	if strings.TrimSpace(c.Command) == "" {
		errs = append(errs, errors.New("command is required"))
	}
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("port must be in 1..65535 (got %d)", c.Port))
	}
	if len(c.Hosts) == 0 {
		errs = append(errs, errors.New("hosts must be non-empty"))
	}
	if c.Concurrency < 1 {
		errs = append(errs, fmt.Errorf("concurrency must be >= 1 (got %d)", c.Concurrency))
	}

	// If private_key_content is provided, it takes precedence over private_key_path.
	// In that case, do not require the file to exist.
	if !hasKeyContent && hasKeyPath {
		if _, err := os.Stat(c.PrivateKeyPath); err != nil {
			errs = append(errs, fmt.Errorf("private_key_path not accessible: %w", err))
		}
	}
	if c.KnownHostsPath != "" {
		if _, err := os.Stat(c.KnownHostsPath); err != nil {
			errs = append(errs, fmt.Errorf("known_hosts_path not accessible: %w", err))
		}
	}

	return errors.Join(errs...)
}

func expandUserPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}

	// Support "~" and "~/" as a convenience in YAML configs.
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			if p == "~" {
				return home
			}
			return filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	return p
}
