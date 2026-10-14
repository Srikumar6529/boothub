package gitops

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Repo struct {
	Path string
}

type CommitDates struct {
	Author    time.Time
	Committer time.Time
}

func (r Repo) run(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Path
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r Repo) Add(paths ...string) error {
	args := append([]string{"add", "--"}, paths...)
	_, err := r.run(nil, args...)
	return err
}

func (r Repo) Commit(msg string, dates *CommitDates) error {
	env := []string{}
	if dates != nil {
		env = append(env,
			"GIT_AUTHOR_DATE="+formatGitDate(dates.Author),
			"GIT_COMMITTER_DATE="+formatGitDate(dates.Committer),
		)
	}
	_, err := r.run(env, "commit", "-m", msg)
	return err
}

func (r Repo) Push(remote, branch string) error {
	_, err := r.run(nil, "push", "-u", remote, branch)
	return err
}

func (r Repo) HasFile(rel string) bool {
	_, err := os.Stat(filepath.Join(r.Path, rel))
	return err == nil
}

// FileInHEAD reports whether rel is present in the current HEAD commit.
func (r Repo) FileInHEAD(rel string) bool {
	_, err := r.run(nil, "cat-file", "-e", "HEAD:"+filepath.ToSlash(rel))
	return err == nil
}

// AheadOfRemote is true when local branch has commits not yet on remote.
func (r Repo) AheadOfRemote(remote, branch string) bool {
	// Ensure we have a remote-tracking ref; if missing, treat as ahead when we have commits.
	if _, err := r.run(nil, "rev-parse", "--verify", remote+"/"+branch); err != nil {
		_, err2 := r.run(nil, "rev-parse", "--verify", "HEAD")
		return err2 == nil
	}
	out, err := r.run(nil, "rev-list", "--count", remote+"/"+branch+"..HEAD")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != "0"
}

// EnsureIdentity sets a local user.name/email if git cannot commit yet.
func (r Repo) EnsureIdentity() error {
	if _, err := r.run(nil, "config", "--get", "user.email"); err == nil {
		return nil
	}
	if _, err := r.run(nil, "config", "user.email", "boothub@users.noreply.github.com"); err != nil {
		return err
	}
	_, err := r.run(nil, "config", "user.name", "boothub")
	return err
}

func (r Repo) IsGitRepo() bool {
	info, err := os.Stat(filepath.Join(r.Path, ".git"))
	return err == nil && info.IsDir()
}

func (r Repo) Init() error {
	if err := os.MkdirAll(r.Path, 0o755); err != nil {
		return err
	}
	_, err := r.run(nil, "init", "-b", "main")
	return err
}

func (r Repo) SetRemote(name, url string) error {
	// Remove existing remote if present, then add.
	_, _ = r.run(nil, "remote", "remove", name)
	_, err := r.run(nil, "remote", "add", name, url)
	return err
}

func formatGitDate(t time.Time) string {
	return t.Format("2006-01-02T15:04:05-0700")
}

func CreateGitHubRepo(name string, private bool) (string, error) {
	args := []string{"repo", "create", name}
	if private {
		args = append(args, "--private")
	} else {
		args = append(args, "--public")
	}

	cmd := exec.Command("gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if !strings.Contains(strings.ToLower(msg), "already exists") {
			return "", fmt.Errorf("gh repo create: %s", msg)
		}
	}

	urlCmd := exec.Command("gh", "repo", "view", name, "--json", "url", "-q", ".url")
	var urlOut, urlErr bytes.Buffer
	urlCmd.Stdout = &urlOut
	urlCmd.Stderr = &urlErr
	if err := urlCmd.Run(); err != nil {
		return "", fmt.Errorf("gh repo view: %s", strings.TrimSpace(urlErr.String()))
	}
	url := strings.TrimSpace(urlOut.String())
	if !strings.HasSuffix(url, ".git") {
		url += ".git"
	}
	return url, nil
}

func Clone(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("git", "clone", url, dest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func GitHubAuthenticated() bool {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run() == nil
}
