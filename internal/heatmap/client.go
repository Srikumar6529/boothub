package heatmap

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.boot.dev/v1/users/public"
	// maxBody caps how much of a response we are willing to buffer.
	maxBody = 8 << 20
)

// Day is one calendar day of activity. Date is always YYYY-MM-DD in the
// timezone that was requested from the API.
type Day struct {
	Date  string
	Count int
}

type Heatmap struct {
	Calendar      []Day
	GitHubCommits []Day
	Status        string
}

type Client struct {
	BaseURL   string
	HTTP      *http.Client
	UserAgent string
	// Retries is the number of additional attempts on 429/5xx. Defaults to 2.
	Retries int
	// Sleep is used between retries (overridable in tests).
	Sleep func(time.Duration)
}

func NewClient() *Client {
	return &Client{
		BaseURL:   DefaultBaseURL,
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: "boothub/1.0 (+https://github.com/Srikumar6529/boothub)",
		Retries:   2,
		Sleep:     time.Sleep,
	}
}

type rawDay struct {
	Date  json.RawMessage `json:"date"`
	Count int             `json:"count"`
}

type rawResponse struct {
	Data struct {
		Calendar      []rawDay `json:"calendar"`
		GitHubCommits []rawDay `json:"githubCommits"`
	} `json:"data"`
	Status string `json:"status"`
}

// parseDayKey extracts a YYYY-MM-DD label. The API encodes a local calendar
// day as midnight UTC, so the UTC date component is the correct key even when
// a different timezone was requested. Date-only strings are accepted too.
func parseDayKey(raw json.RawMessage) (string, error) {
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

var dateOnlyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func toDays(raw []rawDay) ([]Day, error) {
	out := make([]Day, 0, len(raw))
	for _, d := range raw {
		key, err := parseDayKey(d.Date)
		if err != nil {
			return nil, err
		}
		out = append(out, Day{Date: key, Count: d.Count})
	}
	return out, nil
}

// Fetch retrieves the public activity heatmap for handle, bucketed by timezone.
func (c *Client) Fetch(handle, timezone string) (Heatmap, error) {
	if handle == "" {
		return Heatmap{}, fmt.Errorf("boot.dev handle is required")
	}
	if timezone == "" {
		timezone = "UTC"
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/activity_heatmap", strings.TrimRight(c.BaseURL, "/"), url.PathEscape(handle)))
	if err != nil {
		return Heatmap{}, err
	}
	q := u.Query()
	q.Set("timezone", timezone)
	u.RawQuery = q.Encode()

	body, err := c.get(u.String())
	if err != nil {
		return Heatmap{}, err
	}

	var parsed rawResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Heatmap{}, fmt.Errorf("parse heatmap: %w", err)
	}
	if s := strings.ToLower(parsed.Status); s != "" && s != "success" {
		return Heatmap{}, fmt.Errorf("heatmap API returned status %q: %s", parsed.Status, truncate(string(body), 200))
	}

	cal, err := toDays(parsed.Data.Calendar)
	if err != nil {
		return Heatmap{}, err
	}
	gh, err := toDays(parsed.Data.GitHubCommits)
	if err != nil {
		return Heatmap{}, err
	}
	return Heatmap{Calendar: cal, GitHubCommits: gh, Status: parsed.Status}, nil
}

func (c *Client) get(url string) ([]byte, error) {
	retries := c.Retries
	if retries < 0 {
		retries = 0
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			sleep(time.Duration(attempt) * 700 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := httpc.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("heatmap request failed: %w", err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("boot.dev handle %q not found", handleFromURL(url))
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("heatmap API returned %d: %s", resp.StatusCode, truncate(string(body), 200))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("heatmap API returned %d: %s", resp.StatusCode, truncate(string(body), 200))
		}
		if readErr != nil {
			return nil, readErr
		}
		return body, nil
	}
	return nil, lastErr
}

func handleFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-2]
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
