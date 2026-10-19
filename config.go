package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultRemote = "origin"
	defaultBranch = "main"
	defaultRepo   = "bootdev-submissions"
)

type config struct {
	RepoPath      string `yaml:"repo_path"`
	Remote        string `yaml:"remote"`
	Branch        string `yaml:"branch"`
	BootdevHandle string `yaml:"bootdev_handle"`
	Timezone      string `yaml:"timezone"`
	Enabled       bool   `yaml:"enabled"`
	BootdevBinary string `yaml:"bootdev_binary,omitempty"`
}

func defaultConfig() config {
	home, _ := os.UserHomeDir()
	return config{
		RepoPath: filepath.Join(home, defaultRepo),
		Remote:   defaultRemote,
		Branch:   defaultBranch,
		Timezone: "UTC",
		Enabled:  true,
	}
}

func configDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "boothub"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "boothub"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func loadConfig() (config, error) {
	path, err := configPath()
	if err != nil {
		return config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config{}, fmt.Errorf("config not found at %s; run `boothub setup` first", path)
		}
		return config{}, err
	}
	cfg := defaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	return cfg, nil
}

func (c *config) applyDefaults() {
	d := defaultConfig()
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

func saveConfig(cfg config) error {
	cfg.applyDefaults()
	path, err := configPath()
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

func (c config) location() (*time.Location, error) {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", c.Timezone, err)
	}
	return loc, nil
}

func (c config) validate() error {
	c.applyDefaults()
	if c.RepoPath == "" {
		return errors.New("repo_path is required")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", c.Timezone, err)
	}
	return nil
}

func cmdConfig(args []string) error {
	fs := newFlagSet("config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path, _ := configPath()
	fmt.Println("Config path:     ", path)

	cfg, err := loadConfig()
	if err != nil {
		fmt.Println("Status:           not configured (run: boothub setup)")
		return nil
	}
	fmt.Println("repo_path:       ", cfg.RepoPath)
	fmt.Println("remote:          ", cfg.Remote)
	fmt.Println("branch:          ", cfg.Branch)
	fmt.Println("bootdev_handle:  ", cfg.BootdevHandle)
	fmt.Println("timezone:        ", cfg.Timezone)
	fmt.Println("enabled:         ", cfg.Enabled)
	if cfg.BootdevBinary != "" {
		fmt.Println("bootdev_binary:  ", cfg.BootdevBinary)
	}

	r := repo{path: cfg.RepoPath}
	fmt.Println("git repo ok:     ", r.isRepo())
	fmt.Println("git clean:       ", r.isClean())
	fmt.Println("bootdev on PATH: ", commandExists("bootdev"))
	fmt.Println("gh authenticated:", ghAuthenticated())

	if cachePath, err := heatmapCachePath(cfg.BootdevHandle, cfg.Timezone); err == nil {
		if cached, ok := loadHeatmapCache(cfg.BootdevHandle, cfg.Timezone); ok {
			fmt.Printf("heatmap cache:    %s (%d days, fetched %s)\n",
				cachePath, len(cached.Days), cached.FetchedAt.Format("2006-01-02 15:04"))
		} else {
			fmt.Println("heatmap cache:    none yet")
		}
	}

	fmt.Println("validation:      ", cmpErr(cfg.validate()))
	return nil
}

func cmpErr(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}
