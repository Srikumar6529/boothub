package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Srikumar6529/boothub/internal/heatmap"
)

// Heatmap is the locally cached view of the Boot.dev activity heatmap. Past
// days never change, so they can be served from disk instead of re-fetching
// the whole history on every run.
type Heatmap struct {
	Handle        string         `json:"handle"`
	Timezone      string         `json:"timezone"`
	FetchedAt     time.Time      `json:"fetched_at"`
	Days          map[string]int `json:"days"`
	GitHubCommits map[string]int `json:"github_commits,omitempty"`
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// DirOverride, when set, replaces the user cache directory. Tests use it to
// avoid touching the real cache.
var DirOverride string

func safe(s string) string {
	s = unsafe.ReplaceAllString(s, "_")
	if s == "" {
		return "default"
	}
	return s
}

func Dir() (string, error) {
	if DirOverride != "" {
		return DirOverride, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "boothub"), nil
}

func Path(handle, timezone string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "heatmap-"+safe(handle)+"-"+safe(timezone)+".json"), nil
}

func Load(handle, timezone string) (Heatmap, bool, error) {
	path, err := Path(handle, timezone)
	if err != nil {
		return Heatmap{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Heatmap{}, false, nil
		}
		return Heatmap{}, false, err
	}
	var h Heatmap
	if err := json.Unmarshal(data, &h); err != nil {
		// A corrupt cache must never break a run; treat it as absent.
		return Heatmap{}, false, nil
	}
	if h.Days == nil {
		h.Days = map[string]int{}
	}
	return h, true, nil
}

func Save(h Heatmap) error {
	path, err := Path(h.Handle, h.Timezone)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h, "", "  ")
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
// the days they include; older cached days are kept so a truncated API window
// does not erase history.
func (h *Heatmap) Merge(hm heatmap.Heatmap, fetchedAt time.Time) {
	if h.Days == nil {
		h.Days = map[string]int{}
	}
	for _, d := range hm.Calendar {
		if d.Count <= 0 {
			continue
		}
		h.Days[d.Date] = d.Count
	}
	if len(hm.GitHubCommits) > 0 {
		if h.GitHubCommits == nil {
			h.GitHubCommits = map[string]int{}
		}
		for _, d := range hm.GitHubCommits {
			if d.Count <= 0 {
				continue
			}
			h.GitHubCommits[d.Date] = d.Count
		}
	}
	h.FetchedAt = fetchedAt
}

// FreshFor reports whether the cache was refreshed on the same local calendar
// day as now.
func (h Heatmap) FreshFor(now time.Time, loc *time.Location) bool {
	if h.FetchedAt.IsZero() {
		return false
	}
	return h.FetchedAt.In(loc).Format("2006-01-02") == now.In(loc).Format("2006-01-02")
}
