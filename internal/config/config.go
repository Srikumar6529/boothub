package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultRemote = "origin"
	DefaultBranch = "main"
	DefaultRepo   = "bootdev-submissions"
)

type Config struct {
	RepoPath      string `yaml:"repo_path"`
	Remote        string `yaml:"remote"`
	Branch        string `yaml:"branch"`
	BootdevHandle string `yaml:"bootdev_handle"`
	Timezone      string `yaml:"timezone"`
	Enabled       bool   `yaml:"enabled"`
	BootdevBinary string `yaml:"bootdev_binary,omitempty"`
}

func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		RepoPath: filepath.Join(home, DefaultRepo),
		Remote:   DefaultRemote,
		Branch:   DefaultBranch,
		Timezone: "UTC",
		Enabled:  true,
	}
}

func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "boothub"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "boothub"), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("config not found at %s; run `boothub setup` first", path)
		}
		return Config{}, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) applyDefaults() {
	d := Default()
	if c.Remote == "" {
		c.Remote = d.Remote
	}
	if c.Branch == "" {
		c.Branch = d.Branch
	}
	if c.Timezone == "" {
		c.Timezone = d.Timezone
	}
}

func Save(cfg Config) error {
	cfg.applyDefaults()
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c Config) Validate() error {
	c.applyDefaults()
	if c.RepoPath == "" {
		return errors.New("repo_path is required")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", c.Timezone, err)
	}
	return nil
}

func (c Config) Location() (*time.Location, error) {
	return time.LoadLocation(c.Timezone)
}
