package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// --- timezone ---------------------------------------------------------------

// detectTimezone returns the best-effort IANA timezone for this machine.
func detectTimezone() string {
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" && tz != ":" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if tz := timezoneFromLocaltime(); tz != "" {
		return tz
	}
	if name := time.Local.String(); name != "" && name != "Local" {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	return "UTC"
}

func timezoneFromLocaltime() string {
	target, err := filepath.EvalSymlinks("/etc/localtime")
	if err != nil {
		return ""
	}
	target = filepath.ToSlash(target)
	for _, marker := range []string{"/zoneinfo/", "/tzdata/", "zoneinfo/"} {
		if i := strings.Index(target, marker); i >= 0 {
			tz := target[i+len(marker):]
			if _, err := time.LoadLocation(tz); err == nil {
				return tz
			}
		}
	}
	return ""
}

// --- boot.dev handle --------------------------------------------------------

type bootdevConfig struct {
	APIURL       string `yaml:"api_url"`
	AccessToken  string `yaml:"access_token"`
	RefreshToken string `yaml:"refresh_token"`
	LastRefresh  int64  `yaml:"last_refresh"`
}

// detectBootdevHandle resolves the user's Boot.dev handle from the official
// CLI's stored credentials (~/.bootdev.yaml). The CLI never stores the handle,
// so we call GET /v1/users with the access token.
func detectBootdevHandle() (string, error) {
	cfg, path, err := loadBootdevConfig()
	if err != nil {
		return "", err
	}
	if cfg.AccessToken == "" {
		return "", fmt.Errorf("bootdev is not logged in (no access_token in %s); run: bootdev login", path)
	}

	api := cfg.APIURL
	if api == "" {
		api = "https://api.boot.dev"
	}

	token := cfg.AccessToken
	// Refresh if the token looks stale (>55 min), matching the CLI's behavior.
	if cfg.RefreshToken != "" && time.Now().Add(-55*time.Minute).Unix() > cfg.LastRefresh {
		if refreshed, err := refreshAccessToken(api, cfg.RefreshToken); err == nil && refreshed != "" {
			token = refreshed
		}
	}

	handle, err := fetchHandle(api, token)
	if err != nil && cfg.RefreshToken != "" {
		if refreshed, rerr := refreshAccessToken(api, cfg.RefreshToken); rerr == nil && refreshed != "" {
			return fetchHandle(api, refreshed)
		}
	}
	return handle, err
}

func loadBootdevConfig() (bootdevConfig, string, error) {
	var paths []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "bootdev", "config.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "bootdev", "config.yaml"),
			filepath.Join(home, ".bootdev.yaml"),
		)
	}
	var lastErr error
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			lastErr = err
			continue
		}
		var cfg bootdevConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return bootdevConfig{}, p, fmt.Errorf("parse %s: %w", p, err)
		}
		return cfg, p, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("bootdev config not found")
	}
	return bootdevConfig{}, "", lastErr
}

func refreshAccessToken(apiURL, refreshToken string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiURL, "/")+"/v1/auth/refresh", bytes.NewReader(nil))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Refresh-Token", refreshToken)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("refresh failed: %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("refresh returned empty access_token")
	}
	return out.AccessToken, nil
}

func fetchHandle(apiURL, accessToken string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(apiURL, "/")+"/v1/users", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /v1/users returned %d", resp.StatusCode)
	}

	var parsed struct {
		Data struct {
			Handle string `json:"handle"`
		} `json:"data"`
		Handle string `json:"handle"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse /v1/users: %w", err)
	}
	handle := strings.TrimSpace(parsed.Data.Handle)
	if handle == "" {
		handle = strings.TrimSpace(parsed.Handle)
	}
	if handle == "" {
		var raw map[string]any
		if json.Unmarshal(body, &raw) == nil {
			handle = findHandle(raw)
		}
	}
	if handle == "" {
		return "", fmt.Errorf("could not find handle in /v1/users response")
	}
	return strings.TrimPrefix(handle, "@"), nil
}

func findHandle(v any) string {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"handle", "Handle"} {
			if s, ok := t[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		for _, child := range t {
			if s := findHandle(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range t {
			if s := findHandle(child); s != "" {
				return s
			}
		}
	}
	return ""
}
