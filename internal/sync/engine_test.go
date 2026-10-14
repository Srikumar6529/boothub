package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/marker"
)

func TestSyncBackdatedCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
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
	}
	run("init", "-b", "main")
	run("config", "user.email", "boothub@example.com")
	run("config", "user.name", "boothub")

	cfg := config.Config{
		RepoPath: dir,
		Remote:   "origin",
		Branch:   "main",
		Timezone: "UTC",
		Enabled:  true,
	}
	engine := New(cfg)
	when := time.Date(2026, 5, 1, 12, 0, 1, 0, time.UTC)
	res, err := engine.Sync(Request{
		LessonID: "backfill-1",
		Trigger:  marker.TriggerBackfill,
		When:     when,
		Backdate: true,
		SkipPush: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatal("should not skip")
	}
	if !marker.Exists(dir, res.RelPath) {
		t.Fatal("marker missing")
	}

	cmd := exec.Command("git", "log", "-1", "--format=%cI")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := string(bytesTrim(out))
	if got[:10] != "2026-05-01" {
		t.Fatalf("committer date %q", got)
	}

	// Idempotent second sync
	res2, err := engine.Sync(Request{
		LessonID: "backfill-1",
		Trigger:  marker.TriggerBackfill,
		When:     when,
		Backdate: true,
		SkipPush: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Skipped {
		t.Fatal("expected skip on identical marker")
	}
}

func bytesTrim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func TestNoonLocal(t *testing.T) {
	loc := time.UTC
	n := NoonLocal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), loc)
	if n.Hour() != 12 || n.Day() != 2 {
		t.Fatalf("got %v", n)
	}
	_ = filepath.Separator
}
