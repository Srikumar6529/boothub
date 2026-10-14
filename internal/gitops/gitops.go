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
	// Prefer gh identity for author/committer so global git user.* is ignored.
	if user, err := CurrentGitHubUser(); err == nil {
		env = append(env,
			"GIT_AUTHOR_NAME="+user.Name,
			"GIT_AUTHOR_EMAIL="+user.Email,
			"GIT_COMMITTER_NAME="+user.Name,
			"GIT_COMMITTER_EMAIL="+user.Email,
		)
	}
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
	// Use gh credentials for this push only (-c overrides), so a different
	// global git credential / SSH key does not hijack the upload.
	args := []string{
		"-c", "credential.helper=",
		"-c", "credential.helper=!gh auth git-credential",
		"push", "-u", remote, branch,
	}
	_, err := r.run(nil, args...)
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

// EnsureIdentity sets LOCAL (repo-only) author identity from `gh auth`.
// Does not change the user's global git config.
func (r Repo) EnsureIdentity() error {
	user, err := CurrentGitHubUser()
	if err != nil {
		return err
	}
	if _, err := r.run(nil, "config", "--local", "user.name", user.Name); err != nil {
		return err
	}
	if _, err := r.run(nil, "config", "--local", "user.email", user.Email); err != nil {
		return err
	}
	return r.UseGHCredentials()
}

// UseGHCredentials configures this repo only to authenticate git over HTTPS via gh.
func (r Repo) UseGHCredentials() error {
	// Clear then set local helper so global helpers (osxkeychain, etc.) don't win.
	_, _ = r.run(nil, "config", "--local", "--unset-all", "credential.helper")
	if _, err := r.run(nil, "config", "--local", "credential.helper", ""); err != nil {
		// Some git versions reject empty helper; continue to set gh helper.
		_ = err
	}
	if _, err := r.run(nil, "config", "--local", "--add", "credential.helper", "!gh auth git-credential"); err != nil {
		return fmt.Errorf("set local gh credential helper: %w", err)
	}
	// Prefer HTTPS remote so gh credentials apply (SSH would use a different key).
	return r.ensureHTTPSRemote("origin")
}

func (r Repo) ensureHTTPSRemote(name string) error {
	url, err := r.run(nil, "remote", "get-url", name)
	if err != nil || url == "" {
		return nil
	}
	httpsURL := sshToHTTPS(url)
	if httpsURL == url {
		return nil
	}
	_, err = r.run(nil, "remote", "set-url", name, httpsURL)
	return err
}

func sshToHTTPS(remote string) string {
	remote = strings.TrimSpace(remote)
	// git@github.com:Owner/repo.git → https://github.com/Owner/repo.git
	if strings.HasPrefix(remote, "git@github.com:") {
		path := strings.TrimPrefix(remote, "git@github.com:")
		return "https://github.com/" + path
	}
	if strings.HasPrefix(remote, "ssh://git@github.com/") {
		path := strings.TrimPrefix(remote, "ssh://git@github.com/")
		return "https://github.com/" + path
	}
	return remote
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
