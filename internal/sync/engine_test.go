package sync

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Srikumar6529/boothub/internal/cache"
	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/heatmap"
	"github.com/Srikumar6529/boothub/internal/marker"
)

func runGit(t *testing.T, dir string, args ...string) string {
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
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "boothub@example.com")
	runGit(t, dir, "config", "user.name", "boothub")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# bootdev-submissions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "Initial commit")
	return dir
}

func newTestEngine(t *testing.T, dir string, days map[string]int) *Engine {
	t.Helper()
	dirOverride := t.TempDir()
	cache.DirOverride = dirOverride
	t.Cleanup(func() { cache.DirOverride = "" })

	cfg := config.Config{
		RepoPath:      dir,
		Remote:        "origin",
		Branch:        "main",
		Timezone:      "America/Chicago",
		BootdevHandle: "testhandle",
		Enabled:       true,
	}
	e := New(cfg)
	e.SetIdentity(gitops.GitHubUser{Login: "tester", Name: "Test User", Email: "tester@example.com", ID: 42})
	if days != nil {
		c := cache.Heatmap{
			Handle: "testhandle", Timezone: "America/Chicago",
			FetchedAt: time.Now(), Days: days,
		}
		if err := cache.Save(c); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func countsOn(t *testing.T, dir string, loc *time.Location) map[string]int {
	t.Helper()
	m, err := gitops.Repo{Path: dir}.MarkerCommitsPerDay(loc, marker.MessagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	return m
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
	e := newTestEngine(t, dir, nil)
	when := time.Date(2026, 5, 1, 12, 0, 1, 0, time.UTC)

	res, err := e.Sync(Request{LessonID: "abc-123", Trigger: marker.TriggerManual, When: when, Backdate: true, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("should not skip")
	}
	if !marker.Exists(dir, res.RelPath) {
		t.Fatal("marker missing")
	}
	if got := strings.TrimSpace(runGit(t, dir, "log", "-1", "--format=%cI")); got[:10] != "2026-05-01" {
		t.Fatalf("committer date %q", got)
	}

	res2, err := e.Sync(Request{LessonID: "abc-123", Trigger: marker.TriggerManual, When: when, Backdate: true, SkipPush: true})
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
	e := newTestEngine(t, dir, nil)
	when := time.Date(2026, 5, 1, 12, 0, 1, 0, time.UTC)

	for i := 0; i < 3; i++ {
		res, err := e.Sync(Request{LessonID: "backfill-1", Trigger: marker.TriggerBackfill, When: when, Backdate: true, SkipPush: true, Force: true})
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
	e := newTestEngine(t, dir, days)
	loc := chicago(t)

	stats, err := e.Backfill(BackfillOptions{NoRefresh: true, SkipPush: true})
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
	stats2, err := e.Backfill(BackfillOptions{NoRefresh: true, SkipPush: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Created != 0 {
		t.Fatalf("second run created %d commits, want 0", stats2.Created)
	}
}

func TestBackfillSubtractsExistingSubmissions(t *testing.T) {
	dir := newTestRepo(t)
	days := map[string]int{"2026-05-03": 3}
	e := newTestEngine(t, dir, days)
	loc := chicago(t)

	// A real submit already produced one marker on that day.
	if _, err := e.Sync(Request{
		LessonID: "lesson-xyz", Trigger: marker.TriggerCLISubmit,
		When: time.Date(2026, 5, 3, 18, 0, 0, 0, time.UTC), Backdate: true, SkipPush: true,
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := e.Backfill(BackfillOptions{NoRefresh: true, SkipPush: true})
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
	days := map[string]int{"2026-05-05": 1}
	e := newTestEngine(t, dir, days)

	// Simulate two shifted/extra commits already present on the day.
	for i := 0; i < 2; i++ {
		if _, err := e.Sync(Request{
			LessonID: "extra-" + string(rune('a'+i)), Trigger: marker.TriggerBackfill,
			When: time.Date(2026, 5, 5, 12, 0, i, 0, time.UTC), Backdate: true, SkipPush: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := e.Backfill(BackfillOptions{NoRefresh: true, SkipPush: true})
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
	e := newTestEngine(t, dir, days)
	loc := chicago(t)

	// Mess up the history: duplicates on the wrong day.
	for i := 0; i < 4; i++ {
		if _, err := e.Sync(Request{
			LessonID: "junk", Trigger: marker.TriggerBackfill,
			When: time.Date(2026, 4, 30, 12, 0, i, 0, time.UTC), Backdate: true, SkipPush: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// And one real submission we want preserved.
	if _, err := e.Sync(Request{
		LessonID: "real-lesson", Trigger: marker.TriggerCLISubmit,
		When: time.Date(2026, 5, 2, 15, 0, 0, 0, time.UTC), Backdate: true, SkipPush: true,
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := e.Repair(RepairOptions{NoRefresh: true, Yes: true, Push: false})
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
	if out := runGit(t, dir, "branch", "--list", stats.Backup); !strings.Contains(out, stats.Backup) {
		t.Fatalf("backup branch missing: %q", out)
	}
	if stats.Preserved != 1 {
		t.Fatalf("preserved=%d want 1", stats.Preserved)
	}
}

func TestSyncRejectsPathTraversal(t *testing.T) {
	dir := newTestRepo(t)
	e := newTestEngine(t, dir, nil)
	res, err := e.Sync(Request{LessonID: "../../../../tmp/pwn", Trigger: marker.TriggerManual, SkipPush: true})
	if err == nil {
		t.Fatalf("expected error, got %+v", res)
	}
	if _, statErr := os.Stat("/tmp/pwn.json"); statErr == nil {
		t.Fatal("path traversal wrote outside the repo")
	}
}

func TestNoonLocal(t *testing.T) {
	loc := time.UTC
	n := NoonLocal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), loc)
	if n.Hour() != 12 || n.Day() != 2 {
		t.Fatalf("got %v", n)
	}
}

func TestBackfillAddsOnlyTheDeltaWhenCountGrows(t *testing.T) {
	dir := newTestRepo(t)
	e := newTestEngine(t, dir, map[string]int{"2026-05-06": 2})
	loc := chicago(t)

	if _, err := e.Backfill(BackfillOptions{NoRefresh: true, SkipPush: true}); err != nil {
		t.Fatal(err)
	}
	// Boot.dev reports two more units later the same day.
	if err := cache.Save(cache.Heatmap{
		Handle: "testhandle", Timezone: "America/Chicago",
		FetchedAt: time.Now(), Days: map[string]int{"2026-05-06": 5},
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := e.Backfill(BackfillOptions{NoRefresh: true, SkipPush: true})
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
	e := newTestEngine(t, dir, map[string]int{"2026-05-07": 1})

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":{"calendar":[]},"status":"success"}`))
	}))
	defer srv.Close()
	c := heatmap.NewClient()
	c.BaseURL = srv.URL
	e.Client = c

	if _, err := e.Backfill(BackfillOptions{SkipPush: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("API calls=%d, want 0 (cache is fresh)", calls)
	}

	// --refresh must force a call.
	if _, err := e.Backfill(BackfillOptions{Refresh: true, SkipPush: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("API calls=%d, want 1 after --refresh", calls)
	}
}
