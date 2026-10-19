package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=boothub",
		"GIT_AUTHOR_EMAIL=boothub@example.com",
		"GIT_COMMITTER_NAME=boothub",
		"GIT_COMMITTER_EMAIL=boothub@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGitT(t, dir, "init", "-b", "main")
	runGitT(t, dir, "config", "user.email", "boothub@example.com")
	runGitT(t, dir, "config", "user.name", "boothub")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# bootdev-submissions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, dir, "add", ".")
	runGitT(t, dir, "commit", "-m", "Initial commit")
	return dir
}

func newTestApp(t *testing.T, dir string, days map[string]int) app {
	t.Helper()
	cacheDirOverride = t.TempDir()
	t.Cleanup(func() { cacheDirOverride = "" })

	a := app{cfg: config{
		RepoPath:      dir,
		Remote:        "origin",
		Branch:        "main",
		Timezone:      "America/Chicago",
		BootdevHandle: "testhandle",
		Enabled:       true,
	}}
	a.user = &ghUser{Login: "tester", Name: "Test User", Email: "tester@example.com", ID: 42}

	if days != nil {
		if err := saveHeatmapCache(heatmapCache{
			Handle:    "testhandle",
			Timezone:  "America/Chicago",
			FetchedAt: time.Now(),
			Days:      days,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func countsOn(t *testing.T, dir string, loc *time.Location) map[string]int {
	t.Helper()
	return repo{path: dir}.markerCounts(loc)
}

func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestSyncBackdatedCommitAndIdempotency(t *testing.T) {
	dir := newTestRepo(t)
	a := newTestApp(t, dir, nil)
	when := time.Date(2026, 5, 1, 12, 0, 1, 0, time.UTC)

	res, err := a.syncOne(syncRequest{LessonID: "abc-123", Trigger: triggerManual, When: when, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("should not skip")
	}
	if !markerExists(dir, res.RelPath) {
		t.Fatal("marker missing")
	}
	if got := runGitT(t, dir, "log", "-1", "--format=%cI"); got[:10] != "2026-05-01" {
		t.Fatalf("committer date %q", got)
	}

	res2, err := a.syncOne(syncRequest{LessonID: "abc-123", Trigger: triggerManual, When: when, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Skipped {
		t.Fatal("expected skip on identical marker")
	}
}

// Regression: --force used to fail on the second run with "nothing to commit".
func TestForceIsRepeatable(t *testing.T) {
	dir := newTestRepo(t)
	a := newTestApp(t, dir, nil)
	when := time.Date(2026, 5, 1, 12, 0, 1, 0, time.UTC)

	for i := 0; i < 3; i++ {
		res, err := a.syncOne(syncRequest{LessonID: "backfill-1", Trigger: triggerBackfill, When: when, SkipPush: true, Force: true})
		if err != nil {
			t.Fatalf("force run %d: %v", i+1, err)
		}
		if res.Skipped {
			t.Fatalf("force run %d unexpectedly skipped", i+1)
		}
	}
	if got := countsOn(t, dir, chicago(t))["2026-05-01"]; got != 3 {
		t.Fatalf("commits on day = %d, want 3", got)
	}
}

func TestBackfillExactCountsAndIncremental(t *testing.T) {
	dir := newTestRepo(t)
	days := map[string]int{"2026-05-01": 3, "2026-05-02": 1, "2026-05-04": 5}
	a := newTestApp(t, dir, days)
	loc := chicago(t)

	stats, err := a.backfill(backfillOptions{NoRefresh: true, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 9 {
		t.Fatalf("created=%d want 9", stats.Created)
	}
	got := countsOn(t, dir, loc)
	for day, want := range days {
		if got[day] != want {
			t.Fatalf("day %s: got %d want %d (all=%v)", day, got[day], want, got)
		}
	}

	// Second run must be a no-op.
	stats2, err := a.backfill(backfillOptions{NoRefresh: true, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Created != 0 {
		t.Fatalf("second run created %d commits, want 0", stats2.Created)
	}
}

func TestBackfillSubtractsExistingSubmissions(t *testing.T) {
	dir := newTestRepo(t)
	a := newTestApp(t, dir, map[string]int{"2026-05-03": 3})
	loc := chicago(t)

	// A real submit already produced one marker on that day.
	if _, err := a.syncOne(syncRequest{
		LessonID: "lesson-xyz", Trigger: triggerCLISubmit,
		When: time.Date(2026, 5, 3, 18, 0, 0, 0, time.UTC), SkipPush: true,
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := a.backfill(backfillOptions{NoRefresh: true, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 2 {
		t.Fatalf("created=%d want 2 (one submit + two backfills = 3)", stats.Created)
	}
	if got := countsOn(t, dir, loc)["2026-05-03"]; got != 3 {
		t.Fatalf("commits on day = %d, want 3", got)
	}
}

func TestBackfillDoesNotAddWhenAhead(t *testing.T) {
	dir := newTestRepo(t)
	a := newTestApp(t, dir, map[string]int{"2026-05-05": 1})

	// Simulate two shifted/extra commits already present on the day.
	for i := 0; i < 2; i++ {
		if _, err := a.syncOne(syncRequest{
			LessonID: "extra-" + string(rune('a'+i)), Trigger: triggerBackfill,
			When: time.Date(2026, 5, 5, 12, 0, i, 0, time.UTC), SkipPush: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := a.backfill(backfillOptions{NoRefresh: true, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.OverDays != 1 || stats.OverCommits != 1 {
		t.Fatalf("overage detection: days=%d commits=%d", stats.OverDays, stats.OverCommits)
	}
	if stats.Created != 0 {
		t.Fatalf("created=%d want 0", stats.Created)
	}
}

func TestRepairRebuildsExactHistory(t *testing.T) {
	dir := newTestRepo(t)
	days := map[string]int{"2026-05-01": 2, "2026-05-02": 3}
	a := newTestApp(t, dir, days)
	loc := chicago(t)

	// Mess up the history: duplicates on the wrong day.
	for i := 0; i < 4; i++ {
		if _, err := a.syncOne(syncRequest{
			LessonID: "junk", Trigger: triggerBackfill,
			When: time.Date(2026, 4, 30, 12, 0, i, 0, time.UTC), SkipPush: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// And one real submission we want preserved.
	if _, err := a.syncOne(syncRequest{
		LessonID: "real-lesson", Trigger: triggerCLISubmit,
		When: time.Date(2026, 5, 2, 15, 0, 0, 0, time.UTC), SkipPush: true,
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := a.repair(repairOptions{NoRefresh: true, Yes: true, Push: false})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Commits != 5 {
		t.Fatalf("rebuilt commits=%d want 5", stats.Commits)
	}
	got := countsOn(t, dir, loc)
	if got["2026-05-01"] != 2 || got["2026-05-02"] != 3 {
		t.Fatalf("post-repair counts=%v want 2 and 3", got)
	}
	if got["2026-04-30"] != 0 {
		t.Fatalf("stale day not removed: %v", got)
	}
	if stats.Backup == "" {
		t.Fatal("expected a backup branch")
	}
	if out := runGitT(t, dir, "branch", "--list", stats.Backup); !strings.Contains(out, stats.Backup) {
		t.Fatalf("backup branch missing: %q", out)
	}
	if stats.Preserved != 1 {
		t.Fatalf("preserved=%d want 1", stats.Preserved)
	}
}

func TestSyncRejectsPathTraversal(t *testing.T) {
	dir := newTestRepo(t)
	a := newTestApp(t, dir, nil)
	res, err := a.syncOne(syncRequest{LessonID: "../../../../tmp/pwn", Trigger: triggerManual, SkipPush: true})
	if err == nil {
		t.Fatalf("expected error, got %+v", res)
	}
	if _, statErr := os.Stat("/tmp/pwn.json"); statErr == nil {
		t.Fatal("path traversal wrote outside the repo")
	}
}

func TestBackfillAddsOnlyTheDeltaWhenCountGrows(t *testing.T) {
	dir := newTestRepo(t)
	a := newTestApp(t, dir, map[string]int{"2026-05-06": 2})
	loc := chicago(t)

	if _, err := a.backfill(backfillOptions{NoRefresh: true, SkipPush: true}); err != nil {
		t.Fatal(err)
	}
	// Boot.dev reports more units later the same day.
	if err := saveHeatmapCache(heatmapCache{
		Handle: "testhandle", Timezone: "America/Chicago",
		FetchedAt: time.Now(), Days: map[string]int{"2026-05-06": 5},
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := a.backfill(backfillOptions{NoRefresh: true, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 3 {
		t.Fatalf("created=%d want 3", stats.Created)
	}
	if got := countsOn(t, dir, loc)["2026-05-06"]; got != 5 {
		t.Fatalf("commits=%d want 5", got)
	}
}

// The whole point of the cache: a fresh cache must not trigger an API call.
func TestBackfillSkipsAPICallWhenCacheIsFresh(t *testing.T) {
	dir := newTestRepo(t)
	a := newTestApp(t, dir, map[string]int{"2026-05-07": 1})

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":{"calendar":[]},"status":"success"}`))
	}))
	defer srv.Close()
	old := heatmapBaseURL
	heatmapBaseURL = srv.URL
	t.Cleanup(func() { heatmapBaseURL = old })

	if _, err := a.backfill(backfillOptions{SkipPush: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("API calls=%d, want 0 (cache is fresh)", calls)
	}

	// --refresh must force a call.
	if _, err := a.backfill(backfillOptions{Refresh: true, SkipPush: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("API calls=%d, want 1 after --refresh", calls)
	}
}

func TestSanitizeLessonID(t *testing.T) {
	ok := map[string]string{
		"abc-123":    "abc-123",
		"@abc-123":   "abc-123",
		"lesson_1.2": "lesson_1.2",
		"A":          "A",
		"a/b":        "a-b", // slashes are normalized, never kept
		"a\\b":       "a-b",
	}
	for in, want := range ok {
		got, err := sanitizeLessonID(in)
		if err != nil {
			t.Fatalf("sanitize(%q) errored: %v", in, err)
		}
		if got != want {
			t.Fatalf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	bad := []string{"", "..", ".hidden", "a b"}
	for _, id := range bad {
		if got, err := sanitizeLessonID(id); err == nil {
			t.Fatalf("sanitize(%q) = %q, want error", id, got)
		}
	}
}

func TestParseHeatmapDay(t *testing.T) {
	cases := map[string]string{
		`"2026-05-01"`:                "2026-05-01",
		`"2026-05-01T00:00:00Z"`:      "2026-05-01",
		`"2026-05-01T05:00:00-05:00"`: "2026-05-01",
	}
	for in, want := range cases {
		got, err := parseHeatmapDay([]byte(in))
		if err != nil {
			t.Fatalf("parse(%s): %v", in, err)
		}
		if got != want {
			t.Fatalf("parse(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestProxyPassed(t *testing.T) {
	if !proxyPassed(proxyResult{ExitCode: 0, Stdout: "Lesson complete!"}) {
		t.Fatal("clean output should pass")
	}
	if proxyPassed(proxyResult{ExitCode: 1, Stdout: "Lesson complete!"}) {
		t.Fatal("nonzero exit must fail")
	}
	if proxyPassed(proxyResult{ExitCode: 0, Stdout: "Tests failed, try again"}) {
		t.Fatal("failure text must fail even with exit 0")
	}
	if !proxyPassed(proxyResult{ExitCode: 0, Stdout: "\x1b[32mSUCCESS\x1b[0m"}) {
		t.Fatal("ANSI-decorated success should pass")
	}
}

func TestIsSubmitInvocation(t *testing.T) {
	yes := [][]string{
		{"submit"},
		{"submit", "abc"},
		{"run", "-s"},
		{"run", "--submit", "abc"},
	}
	for _, args := range yes {
		if !isSubmitInvocation(args) {
			t.Fatalf("isSubmitInvocation(%v) = false, want true", args)
		}
	}
	no := [][]string{
		{},
		{"run"},
		{"submit", "--help"},
		{"submit", "-h"},
		{"run", "--version"},
		{"run", "--help", "-s"},
	}
	for _, args := range no {
		if isSubmitInvocation(args) {
			t.Fatalf("isSubmitInvocation(%v) = true, want false", args)
		}
	}
}

func TestLessonIDFromOutput(t *testing.T) {
	r := proxyResult{Stdout: "\x1b[33mSubmitting lesson 0d5e6f7a-1234-4abc-9def-0123456789ab\x1b[0m"}
	if got := lessonIDFromOutput(r); got != "0d5e6f7a-1234-4abc-9def-0123456789ab" {
		t.Fatalf("got %q", got)
	}
	if got := lessonIDFromOutput(proxyResult{Stdout: "no id here"}); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
