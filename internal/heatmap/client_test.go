package heatmap

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseDayKeyVariants(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"2026-10-01T00:00:00Z"`, "2026-10-01"},
		{`"2026-10-01"`, "2026-10-01"},
		{`"2026-10-01T00:00:00-05:00"`, "2026-10-01"},
	}
	for _, tc := range cases {
		got, err := parseDayKey([]byte(tc.in))
		if err != nil {
			t.Fatalf("parseDayKey(%s): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseDayKey(%s)=%s want %s", tc.in, got, tc.want)
		}
	}
}

func TestParseDayKeyRejectsGarbage(t *testing.T) {
	if _, err := parseDayKey([]byte(`"not-a-date"`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchParsesAndFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("timezone"); got != "America/Chicago" {
			t.Errorf("timezone=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"calendar": [
					{"date": "2026-10-01T00:00:00Z", "count": 3},
					{"date": "2026-10-02", "count": 4},
					{"date": "2026-10-03T00:00:00Z", "count": 0}
				],
				"githubCommits": [{"date": "2026-10-01T00:00:00Z", "count": 9}]
			},
			"status": "success"
		}`))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	hm, err := c.Fetch("davinci2003", "America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	if len(hm.Calendar) != 3 {
		t.Fatalf("calendar len=%d", len(hm.Calendar))
	}
	if hm.Calendar[0].Date != "2026-10-01" || hm.Calendar[1].Date != "2026-10-02" {
		t.Fatalf("bad dates: %+v", hm.Calendar)
	}
	if len(hm.GitHubCommits) != 1 || hm.GitHubCommits[0].Count != 9 {
		t.Fatalf("github commits=%+v", hm.GitHubCommits)
	}
}

func TestFetchRetriesThenSucceeds(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"calendar":[]},"status":"success"}`))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	c.Sleep = func(time.Duration) {}
	if _, err := c.Fetch("x", "UTC"); err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d", attempts)
	}
}
