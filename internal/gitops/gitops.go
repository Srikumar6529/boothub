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
			// git writes things like "nothing to commit" to stdout.
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r Repo) Add(paths ...string) error {
	if len(paths) == 0 {
		_, err := r.run(nil, "add", "-A", "--", ".")
		return err
	}
	args := append([]string{"add", "--"}, paths...)
	_, err := r.run(nil, args...)
	return err
}

// CommitAs creates a commit authored and committed by user. When when is nil
// the current time is used. allowEmpty permits commits with no file changes
// (used to inflate a day's contribution count).
func (r Repo) CommitAs(user *GitHubUser, msg string, when *time.Time, allowEmpty bool) error {
	env := []string{}
	if user != nil {
		env = append(env,
			"GIT_AUTHOR_NAME="+user.Name,
			"GIT_AUTHOR_EMAIL="+user.Email,
			"GIT_COMMITTER_NAME="+user.Name,
			"GIT_COMMITTER_EMAIL="+user.Email,
		)
	}
	if when != nil {
		d := formatGitDate(*when)
		env = append(env, "GIT_AUTHOR_DATE="+d, "GIT_COMMITTER_DATE="+d)
	}
	args := []string{"commit", "-m", msg}
	if allowEmpty {
		args = append(args, "--allow-empty")
	}
	_, err := r.run(env, args...)
	return err
}

// StagedOutside returns staged paths that do not live under prefix. BootHub
// commits must never sweep up unrelated staged files.
func (r Repo) StagedOutside(prefix string) ([]string, error) {
	out, err := r.run(nil, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, prefix) {
			bad = append(bad, line)
		}
	}
	return bad, nil
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

func (r Repo) PushForce(remote, branch string) error {
	args := []string{
		"-c", "credential.helper=",
		"-c", "credential.helper=!gh auth git-credential",
		"push", "--force", "-u", remote, branch,
	}
	_, err := r.run(nil, args...)
	return err
}

func (r Repo) PushBranch(remote, branch string) error {
	args := []string{
		"-c", "credential.helper=",
		"-c", "credential.helper=!gh auth git-credential",
		"push", "-u", remote, branch,
	}
	_, err := r.run(nil, args...)
	return err
}

// Fetch updates remote-tracking refs so ahead/behind checks are not stale.
func (r Repo) Fetch(remote string) error {
	_, err := r.run(nil, "fetch", "--quiet", remote)
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

// FilesInHEAD lists every tracked file under prefix (slash-separated). Returns
// an empty slice when HEAD does not exist yet.
func (r Repo) FilesInHEAD(prefix string) ([]string, error) {
	out, err := r.run(nil, "ls-tree", "-r", "--name-only", "HEAD", "--", prefix)
	if err != nil {
		// No commits yet (or not a git repo): treat as empty.
		return nil, nil
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// MarkerCommitsPerDay counts commits whose subject starts with markerPrefix,
// grouped by the author date in loc. This is the exact number GitHub will show
// for this repository.
func (r Repo) MarkerCommitsPerDay(loc *time.Location, markerPrefix string) (map[string]int, error) {
	tz := "UTC"
	if loc != nil {
		tz = loc.String()
	}
	out, err := r.run([]string{"TZ=" + tz}, "log", "--date=format-local:%Y-%m-%d", "--format=%ad%x09%s")
	if err != nil {
		// Repo with no commits.
		return map[string]int{}, nil
	}
	counts := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if !strings.HasPrefix(parts[1], markerPrefix) {
			continue
		}
		counts[parts[0]]++
	}
	return counts, nil
}

// AheadOfRemote is true when local branch has commits not yet on remote.
func (r Repo) AheadOfRemote(remote, branch string) bool {
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

// EnsureIdentity sets LOCAL (repo-only) author identity from `gh auth` and
// points the repo at gh's credential helper. It never touches global config.
func (r Repo) EnsureIdentity(user GitHubUser) error {
	if _, err := r.run(nil, "config", "--local", "user.name", user.Name); err != nil {
		return err
	}
	if _, err := r.run(nil, "config", "--local", "user.email", user.Email); err != nil {
		return err
	}
	return r.UseGHCredentials()
}

func (r Repo) UseGHCredentials() error {
	_, _ = r.run(nil, "config", "--local", "--unset-all", "credential.helper")
	if _, err := r.run(nil, "config", "--local", "credential.helper", ""); err != nil {
		_ = err
	}
	if _, err := r.run(nil, "config", "--local", "--add", "credential.helper", "!gh auth git-credential"); err != nil {
		return fmt.Errorf("set local gh credential helper: %w", err)
	}
	return r.ensureHTTPSRemote("origin")
}

func (r Repo) HTTPSRemote(name string) error { return r.ensureHTTPSRemote(name) }

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

func (r Repo) RemoteURL(name string) string {
	url, _ := r.run(nil, "remote", "get-url", name)
	return strings.TrimSpace(url)
}

func sshToHTTPS(remote string) string {
	remote = strings.TrimSpace(remote)
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

// IsGitRepo works for normal clones, worktrees and submodules (where .git is a
// file rather than a directory).
func (r Repo) IsGitRepo() bool {
	_, err := r.run(nil, "rev-parse", "--git-dir")
	return err == nil
}

func (r Repo) CurrentBranch() string {
	out, err := r.run(nil, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// IsClean reports whether there are no staged or unstaged changes.
func (r Repo) IsClean() bool {
	out, err := r.run(nil, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) == ""
}

func (r Repo) Init(branch string) error {
	if err := os.MkdirAll(r.Path, 0o755); err != nil {
		return err
	}
	if branch == "" {
		branch = "main"
	}
	_, err := r.run(nil, "init", "-b", branch)
	return err
}

func (r Repo) SetRemote(name, url string) error {
	_, _ = r.run(nil, "remote", "remove", name)
	_, err := r.run(nil, "remote", "add", name, url)
	return err
}

// FastImport feeds a git fast-import stream to the repo. Used to build large
// backfilled histories in a single subprocess instead of thousands of commits.
func (r Repo) FastImport(stream []byte) error {
	cmd := exec.Command("git", "fast-import", "--force", "--quiet")
	cmd.Dir = r.Path
	cmd.Stdin = bytes.NewReader(stream)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git fast-import: %s", msg)
	}
	return nil
}

func (r Repo) ResetHard(ref string) error {
	_, err := r.run(nil, "reset", "--hard", ref)
	return err
}

func (r Repo) CreateBranch(name, ref string) error {
	_, err := r.run(nil, "branch", "-f", name, ref)
	return err
}

func (r Repo) DeleteBranch(name string) error {
	_, err := r.run(nil, "branch", "-D", name)
	return err
}

func (r Repo) Head() (string, error) {
	return r.run(nil, "rev-parse", "HEAD")
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
