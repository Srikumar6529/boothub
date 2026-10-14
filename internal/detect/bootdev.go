package detect

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

type bootdevConfig struct {
	APIURL       string `yaml:"api_url"`
	AccessToken  string `yaml:"access_token"`
	RefreshToken string `yaml:"refresh_token"`
	LastRefresh  int64  `yaml:"last_refresh"`
}

type userResponse struct {
	Data struct {
		Handle string `json:"handle"`
	} `json:"data"`
	Handle string `json:"handle"` // some payloads are flat
	Status string `json:"status"`
}

// BootdevHandle tries to resolve the user's Boot.dev handle using the
// official CLI's stored credentials (~/.bootdev.yaml). The CLI itself does
// not store the handle; we call GET /v1/users with the access token.
func BootdevHandle() (string, error) {
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
	// Refresh if token looks stale (>55 minutes), matching bootdev CLI behavior.
	if cfg.RefreshToken != "" && time.Now().Add(-55*time.Minute).Unix() > cfg.LastRefresh {
		if refreshed, err := refreshAccessToken(api, cfg.RefreshToken); err == nil && refreshed != "" {
			token = refreshed
		}
	}

	handle, err := fetchHandle(api, token)
	if err != nil && cfg.RefreshToken != "" {
		// One retry after forced refresh.
		if refreshed, rerr := refreshAccessToken(api, cfg.RefreshToken); rerr == nil && refreshed != "" {
			return fetchHandle(api, refreshed)
		}
	}
	return handle, err
}

func loadBootdevConfig() (bootdevConfig, string, error) {
	paths := bootdevConfigPaths()
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

func bootdevConfigPaths() []string {
	var paths []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "bootdev", "config.yaml"))
	}
	home, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "bootdev", "config.yaml"),
			filepath.Join(home, ".bootdev.yaml"),
		)
	}
	return paths
}

func refreshAccessToken(apiURL, refreshToken string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiURL, "/")+"/v1/auth/refresh", bytes.NewReader(nil))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Refresh-Token", refreshToken)
	req.Header.Set("User-Agent", "boothub/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
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
	req.Header.Set("User-Agent", "boothub/1.0")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /v1/users returned %d", resp.StatusCode)
	}

	var parsed userResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse /v1/users: %w", err)
	}
	handle := strings.TrimSpace(parsed.Data.Handle)
	if handle == "" {
		handle = strings.TrimSpace(parsed.Handle)
	}
	// Fallback: raw map walk for unexpected casing
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
