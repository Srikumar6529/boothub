package sync

import (
	"fmt"
	"os"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/marker"
)

type Request struct {
	LessonID      string
	Trigger       string
	When          time.Time
	Note          string
	ActivityCount int
	Backdate      bool
	SkipPush      bool
}

type Result struct {
	Skipped bool
	RelPath string
	Message string
}

type Engine struct {
	Cfg config.Config
}

func New(cfg config.Config) *Engine {
	return &Engine{Cfg: cfg}
}

func (e *Engine) Sync(req Request) (Result, error) {
	if !e.Cfg.Enabled {
		return Result{Skipped: true, Message: "sync disabled in config"}, nil
	}
	if req.When.IsZero() {
		req.When = time.Now()
	}
	if req.LessonID == "" {
		req.LessonID = fmt.Sprintf("unknown-%d", req.When.Unix())
	}

	loc, err := e.Cfg.Location()
	if err != nil {
		return Result{}, err
	}
	dayKey := marker.DayKey(req.When, loc)
	rel := marker.RelPath(dayKey, req.LessonID)

	m := marker.New(req.LessonID, req.Trigger, req.When)
	m.Note = req.Note
	m.ActivityCount = req.ActivityCount

	identical, err := marker.Identical(e.Cfg.RepoPath, rel, m)
	if err != nil {
		return Result{}, err
	}
	if identical {
		return Result{Skipped: true, RelPath: rel, Message: "marker already up to date"}, nil
	}

	repo := gitops.Repo{Path: e.Cfg.RepoPath}
	if !repo.IsGitRepo() {
		return Result{}, fmt.Errorf("not a git repo: %s", e.Cfg.RepoPath)
	}

	if err := marker.Write(e.Cfg.RepoPath, rel, m); err != nil {
		return Result{}, err
	}

	if err := repo.Add(rel); err != nil {
		return Result{}, err
	}

	msg := fmt.Sprintf("boot.dev: complete %s", req.LessonID)
	var dates *gitops.CommitDates
	if req.Backdate {
		local := req.When.In(loc)
		dates = &gitops.CommitDates{Author: local, Committer: local}
	}
	if err := repo.Commit(msg, dates); err != nil {
		return Result{}, err
	}

	if !req.SkipPush {
		if err := repo.Push(e.Cfg.Remote, e.Cfg.Branch); err != nil {
			return Result{}, fmt.Errorf("push failed: %w", err)
		}
	}

	return Result{RelPath: rel, Message: "synced"}, nil
}

func (e *Engine) Push() error {
	repo := gitops.Repo{Path: e.Cfg.RepoPath}
	return repo.Push(e.Cfg.Remote, e.Cfg.Branch)
}

// NoonLocal returns noon on the calendar day of t in loc (stable backfill timestamp base).
func NoonLocal(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc)
}

func EnsureRepoPath(path string) error {
	return os.MkdirAll(path, 0o755)
}
