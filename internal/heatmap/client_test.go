package heatmap

import (
	"encoding/json"
	"testing"
)

func TestParseCalendarIgnoresZeroAndGithubCommits(t *testing.T) {
	body := []byte(`{
		"data": {
			"calendar": [
				{"date": "2026-10-01T00:00:00Z", "count": 3},
				{"date": "2026-10-02T00:00:00Z", "count": 0},
				{"date": "2026-10-03T00:00:00Z", "count": 1}
			],
			"githubCommits": [{"date": "2026-10-01T00:00:00Z", "count": 9}]
		},
		"status": "success"
	}`)
	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	active := 0
	total := 0
	for _, d := range parsed.Data.Calendar {
		if d.Count > 0 {
			active++
			total += d.Count
		}
	}
	if active != 2 || total != 4 {
		t.Fatalf("active=%d total=%d", active, total)
	}
}
