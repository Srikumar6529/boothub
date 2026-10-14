package heatmap

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const baseURL = "https://api.boot.dev/v1/users/public"

type CalendarDay struct {
	Date  time.Time
	Count int
}

type apiResponse struct {
	Data struct {
		Calendar []struct {
			Date  time.Time `json:"date"`
			Count int       `json:"count"`
		} `json:"calendar"`
	} `json:"data"`
	Status string `json:"status"`
}

func FetchActivityHeatmap(handle, timezone string) ([]CalendarDay, error) {
	if handle == "" {
		return nil, fmt.Errorf("boot.dev handle is required")
	}
	if timezone == "" {
		timezone = "UTC"
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/activity_heatmap", baseURL, url.PathEscape(handle)))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("timezone", timezone)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "boothub/1.0 (+https://github.com/Srikumar6529/boothub)")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("heatmap request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("boot.dev handle %q not found", handle)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("heatmap API returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse heatmap: %w", err)
	}

	days := make([]CalendarDay, 0, len(parsed.Data.Calendar))
	for _, d := range parsed.Data.Calendar {
		if d.Count <= 0 {
			continue
		}
		days = append(days, CalendarDay{Date: d.Date, Count: d.Count})
	}
	return days, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
