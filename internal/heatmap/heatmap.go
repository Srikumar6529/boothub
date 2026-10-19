package heatmap

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	userAgent    = "boothub (+https://github.com/Srikumar6529/boothub)"
	heatmapLimit = 8 << 20
)

// BaseURL is a var so tests can point it at a local server.
var BaseURL = "https://api.boot.dev/v1/users/public"

var httpClient = &http.Client{Timeout: 30 * time.Second}

type Day struct {
	Date  string
	Count int
}

type Data struct {
	Calendar      []Day
	GitHubCommits []Day
	Status        string
}

var dateOnlyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// parseHeatmapDay turns a date label into YYYY-MM-DD. The API encodes a local
// calendar day as midnight UTC, so the UTC date component is the day.
func parseHeatmapDay(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	s = strings.TrimSpace(s)
	if dateOnlyRe.MatchString(s) {
		return s, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", fmt.Errorf("unsupported date %q", s)
	}
	return t.UTC().Format("2006-01-02"), nil
}

func decodeDays(raw []struct {
	Date  json.RawMessage `json:"date"`
	Count int             `json:"count"`
}) ([]Day, error) {
	out := make([]Day, 0, len(raw))
	for _, d := range raw {
		key, err := parseHeatmapDay(d.Date)
		if err != nil {
			return nil, err
		}
		out = append(out, Day{Date: key, Count: d.Count})
	}
	return out, nil
}

// Fetch retrieves the public activity heatmap bucketed by timezone.
func Fetch(handle, timezone string) (Data, error) {
	if handle == "" {
		return Data{}, fmt.Errorf("boot.dev handle is required")
	}
	if timezone == "" {
		timezone = "UTC"
	}

	u, err := url.Parse(strings.TrimRight(BaseURL, "/") + "/" + url.PathEscape(handle) + "/activity_heatmap")
	if err != nil {
		return Data{}, err
	}
	q := u.Query()
	q.Set("timezone", timezone)
	u.RawQuery = q.Encode()

	body, err := httpGet(u.String())
	if err != nil {
		return Data{}, err
	}

	var parsed struct {
		Data struct {
			Calendar []struct {
				Date  json.RawMessage `json:"date"`
				Count int             `json:"count"`
			} `json:"calendar"`
			GitHubCommits []struct {
				Date  json.RawMessage `json:"date"`
				Count int             `json:"count"`
			} `json:"githubCommits"`
		} `json:"data"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Data{}, fmt.Errorf("parse heatmap: %w", err)
	}
	if s := strings.ToLower(parsed.Status); s != "" && s != "success" {
		return Data{}, fmt.Errorf("heatmap API returned status %q", parsed.Status)
	}

	cal, err := decodeDays(parsed.Data.Calendar)
	if err != nil {
		return Data{}, err
	}
	gh, err := decodeDays(parsed.Data.GitHubCommits)
	if err != nil {
		return Data{}, err
	}
	return Data{Calendar: cal, GitHubCommits: gh, Status: parsed.Status}, nil
}

func httpGet(rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 700 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, heatmapLimit))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return nil, fmt.Errorf("boot.dev handle not found")
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("heatmap API returned %d", resp.StatusCode)
			continue
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("heatmap API returned %d", resp.StatusCode)
		case readErr != nil:
			return nil, readErr
		}
		return body, nil
	}
	return nil, lastErr
}

// --- cache ------------------------------------------------------------------

// Cache is the on-disk snapshot of the heatmap. Past days never change, so
// repeat runs reuse it and only refresh the current day.
type Cache struct {
	Handle    string         `json:"handle"`
	Timezone  string         `json:"timezone"`
	FetchedAt time.Time      `json:"fetched_at"`
	Days      map[string]int `json:"days"`
	GitHub    map[string]int `json:"github_commits,omitempty"`
}

// DirOverride is set by tests to avoid touching the real cache.
var DirOverride string

var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func cacheDir() (string, error) {
	if DirOverride != "" {
		return DirOverride, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "boothub"), nil
}

func CachePath(handle, timezone string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	name := unsafeFilename.ReplaceAllString(handle, "_") + "-" + unsafeFilename.ReplaceAllString(timezone, "_")
	return filepath.Join(dir, "heatmap-"+name+".json"), nil
}

func LoadCache(handle, timezone string) (Cache, bool) {
	path, err := CachePath(handle, timezone)
	if err != nil {
		return Cache{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Cache{}, false
	}
	var c Cache
	if json.Unmarshal(data, &c) != nil {
		return Cache{}, false
	}
	if c.Days == nil {
		c.Days = map[string]int{}
	}
	return c, true
}

func SaveCache(c Cache) error {
	path, err := CachePath(c.Handle, c.Timezone)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Merge folds a freshly fetched heatmap into the cache. Fetched values win for
// days they include; older cached days are kept.
func (c *Cache) Merge(hm Data, fetchedAt time.Time) {
	if c.Days == nil {
		c.Days = map[string]int{}
	}
	for _, d := range hm.Calendar {
		if d.Count > 0 {
			c.Days[d.Date] = d.Count
		}
	}
	if len(hm.GitHubCommits) > 0 {
		if c.GitHub == nil {
			c.GitHub = map[string]int{}
		}
		for _, d := range hm.GitHubCommits {
			if d.Count > 0 {
				c.GitHub[d.Date] = d.Count
			}
		}
	}
	c.FetchedAt = fetchedAt
}

func (c Cache) FreshFor(now time.Time, loc *time.Location) bool {
	if c.FetchedAt.IsZero() {
		return false
	}
	return c.FetchedAt.In(loc).Format("2006-01-02") == now.In(loc).Format("2006-01-02")
}
