package gitops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultReadme seeds a brand-new submissions repo.
const DefaultReadme = "# bootdev-submissions\n\nSynced automatically by [BootHub](https://github.com/Srikumar6529/boothub).\n"

// User is the identity from `gh auth` (not the global git config).
type User struct {
	Login string
	ID    int64
	Name  string
	Email string
}

func CurrentUser() (User, error) {
	out, err := runCmd("", nil, "gh", "api", "user", "--jq", "{login:.login,id:.id,name:.name}")
	if err != nil {
		return User{}, fmt.Errorf("%s (run: gh auth login)", err)
	}
	var raw struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return User{}, fmt.Errorf("parse gh user: %w", err)
	}
	if raw.Login == "" || raw.ID == 0 {
		return User{}, fmt.Errorf("gh api user returned empty login/id")
	}
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		name = raw.Login
	}
	// Linking commits to the account without exposing a personal email.
	email := fmt.Sprintf("%d+%s@users.noreply.github.com", raw.ID, raw.Login)
	return User{Login: raw.Login, ID: raw.ID, Name: name, Email: email}, nil
}

type Repo struct{ Path string }

func (r Repo) run(env []string, args ...string) (string, error) {
	return runCmd(r.Path, env, "git", args...)
}

// runCmd runs a command and returns trimmed stdout; on failure the error
// includes stderr (or stdout) so callers never see a bare "exit status 1".
func runCmd(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r Repo) Add(paths ...string) error {
	args := append([]string{"add", "--"}, paths...)
	_, err := r.run(nil, args...)
	return err
}

// Commit creates a commit authored/committed by user. when==nil uses now.
func (r Repo) Commit(user *User, msg string, when *time.Time, allowEmpty bool) error {
	env := []string{}
	if user != nil {
		env = append(env,
			"GIT_AUTHOR_NAME="+user.Name, "GIT_AUTHOR_EMAIL="+user.Email,
			"GIT_COMMITTER_NAME="+user.Name, "GIT_COMMITTER_EMAIL="+user.Email,
		)
	}
	if when != nil {
		d := FormatGitDate(*when)
		env = append(env, "GIT_AUTHOR_DATE="+d, "GIT_COMMITTER_DATE="+d)
	}
	args := []string{"commit", "-m", msg}
	if allowEmpty {
		args = append(args, "--allow-empty")
	}
	_, err := r.run(env, args...)
	return err
}

func (r Repo) ResetHard(ref string) error {
	args := []string{"reset", "--hard"}
	if ref != "" {
		args = append(args, ref)
	}
	_, err := r.run(nil, args...)
	return err
}

func (r Repo) Head() (string, error) { return r.run(nil, "rev-parse", "HEAD") }

func (r Repo) CurrentBranch() string {
	out, err := r.run(nil, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

func (r Repo) IsRepo() bool {
	_, err := r.run(nil, "rev-parse", "--git-dir")
	return err == nil
}

func (r Repo) IsClean() bool {
	out, err := r.run(nil, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) == ""
}

func (r Repo) FileInHEAD(rel string) bool {
	_, err := r.run(nil, "cat-file", "-e", "HEAD:"+rel)
	return err == nil
}

func (r Repo) FilesInHEAD(prefix string) ([]string, error) {
	out, err := r.run(nil, "ls-tree", "-r", "--name-only", "HEAD", "--", prefix)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// StagedOutside returns staged paths that are not under prefix, so a commit
// never sweeps up unrelated files.
func (r Repo) StagedOutside(prefix string) ([]string, error) {
	out, err := r.run(nil, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, prefix) {
			bad = append(bad, line)
		}
	}
	return bad, nil
}

// MarkerCounts counts commits whose subject starts with prefix per calendar day
// in loc. This is the exact number GitHub shows for the repository.
func (r Repo) MarkerCounts(loc *time.Location, prefix string) map[string]int {
	out, err := r.run(nil, "log", "--format=%at%x09%s")
	if err != nil {
		return map[string]int{}
	}
	counts := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		ts, subj, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasPrefix(subj, prefix) {
			continue
		}
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			continue
		}
		counts[time.Unix(sec, 0).In(loc).Format("2006-01-02")]++
	}
	return counts
}

func (r Repo) AheadOfRemote(remote, branch string) bool {
	if _, err := r.run(nil, "rev-parse", "--verify", remote+"/"+branch); err != nil {
		_, headErr := r.run(nil, "rev-parse", "--verify", "HEAD")
		return headErr == nil
	}
	out, err := r.run(nil, "rev-list", "--count", remote+"/"+branch+"..HEAD")
	return err == nil && strings.TrimSpace(out) != "0"
}

func (r Repo) Push(remote, branch string) error      { return r.pushArgs(false, remote, branch) }
func (r Repo) PushForce(remote, branch string) error { return r.pushArgs(true, remote, branch) }

func (r Repo) pushArgs(force bool, remote, branch string) error {
	args := []string{"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential", "push", "-u"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, remote, branch)
	_, err := r.run(nil, args...)
	return err
}

func (r Repo) Fetch(remote string) error {
	_, err := r.run(nil, "fetch", "--quiet", remote)
	return err
}

// EnsureIdentity sets repo-local identity from gh and uses gh for push auth.
// It never touches global git config.
func (r Repo) EnsureIdentity(u User) error {
	if _, err := r.run(nil, "config", "--local", "user.name", u.Name); err != nil {
		return err
	}
	if _, err := r.run(nil, "config", "--local", "user.email", u.Email); err != nil {
		return err
	}
	return r.UseGHCredentials()
}

func (r Repo) UseGHCredentials() error {
	_, _ = r.run(nil, "config", "--local", "--unset-all", "credential.helper")
	_, _ = r.run(nil, "config", "--local", "--add", "credential.helper", "")
	if _, err := r.run(nil, "config", "--local", "--add", "credential.helper", "!gh auth git-credential"); err != nil {
		return fmt.Errorf("set local gh credential helper: %w", err)
	}
	return r.ensureHTTPSRemote("origin")
}

func (r Repo) ensureHTTPSRemote(name string) error {
	url, err := r.run(nil, "remote", "get-url", name)
	if err != nil || url == "" {
		return nil
	}
	if https := sshToHTTPS(url); https != url {
		_, err = r.run(nil, "remote", "set-url", name, https)
		return err
	}
	return nil
}

func (r Repo) RemoteURL(name string) string {
	out, _ := r.run(nil, "remote", "get-url", name)
	return strings.TrimSpace(out)
}

func (r Repo) SetRemote(name, url string) error {
	_, _ = r.run(nil, "remote", "remove", name)
	_, err := r.run(nil, "remote", "add", name, url)
	return err
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

func (r Repo) CreateBranch(name, ref string) error {
	_, err := r.run(nil, "branch", "-f", name, ref)
	return err
}

func (r Repo) DeleteBranch(name string) error {
	_, err := r.run(nil, "branch", "-D", name)
	return err
}

// ImportEntry is one commit for fast-import: a file to add at a backdated time.
type ImportEntry struct {
	Rel     string
	When    time.Time
	Content []byte
	Message string
}

// FastImport builds `entries` commits in a single git subprocess. parent is the
// commit to start from ("" builds an orphan history). readme, when set, is
// committed alongside the first entry.
func (r Repo) FastImport(branch, parent string, entries []ImportEntry, user User, readme []byte) error {
	var b bytes.Buffer
	blob := 0
	writeBlob := func(content []byte) int {
		blob++
		fmt.Fprintf(&b, "blob\nmark :%d\ndata %d\n", blob, len(content))
		b.Write(content)
		b.WriteByte('\n')
		return blob
	}

	var readmeMark int
	if len(readme) > 0 {
		readmeMark = writeBlob(readme)
	}

	for i, e := range entries {
		mark := writeBlob(e.Content)
		fmt.Fprintf(&b, "commit refs/heads/%s\n", branch)
		fmt.Fprintf(&b, "author %s <%s> %d %s\n", user.Name, user.Email, e.When.Unix(), e.When.Format("-0700"))
		fmt.Fprintf(&b, "committer %s <%s> %d %s\n", user.Name, user.Email, e.When.Unix(), e.When.Format("-0700"))
		fmt.Fprintf(&b, "data %d\n%s\n", len(e.Message), e.Message)
		if i == 0 && parent != "" {
			fmt.Fprintf(&b, "from %s\n", parent)
		}
		fmt.Fprintf(&b, "M 100644 :%d %s\n", mark, e.Rel)
		if i == 0 && readmeMark > 0 {
			fmt.Fprintf(&b, "M 100644 :%d README.md\n", readmeMark)
		}
	}

	// A stream with no commits would leave the ref untouched (and break a
	// caller that resets to it), so emit a single README commit instead.
	if len(entries) == 0 {
		if readmeMark == 0 {
			readmeMark = writeBlob([]byte("# bootdev-submissions\n"))
		}
		now := time.Now()
		msg := "Initial commit"
		fmt.Fprintf(&b, "commit refs/heads/%s\n", branch)
		fmt.Fprintf(&b, "author %s <%s> %d %s\n", user.Name, user.Email, now.Unix(), now.Format("-0700"))
		fmt.Fprintf(&b, "committer %s <%s> %d %s\n", user.Name, user.Email, now.Unix(), now.Format("-0700"))
		fmt.Fprintf(&b, "data %d\n%s\n", len(msg), msg)
		if parent != "" {
			fmt.Fprintf(&b, "from %s\n", parent)
		}
		fmt.Fprintf(&b, "M 100644 :%d README.md\n", readmeMark)
	}

	cmd := exec.Command("git", "fast-import", "--force", "--quiet")
	cmd.Dir = r.Path
	cmd.Stdin = &b
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

func sshToHTTPS(remote string) string {
	remote = strings.TrimSpace(remote)
	switch {
	case strings.HasPrefix(remote, "git@github.com:"):
		return "https://github.com/" + strings.TrimPrefix(remote, "git@github.com:")
	case strings.HasPrefix(remote, "ssh://git@github.com/"):
		return "https://github.com/" + strings.TrimPrefix(remote, "ssh://git@github.com/")
	}
	return remote
}

func FormatGitDate(t time.Time) string { return t.Format("2006-01-02T15:04:05-0700") }

func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func GitHubAuthenticated() bool {
	return exec.Command("gh", "auth", "status").Run() == nil
}

func CreateGitHubRepo(name string, private bool) (string, error) {
	args := []string{"repo", "create", name}
	if private {
		args = append(args, "--private")
	} else {
		args = append(args, "--public")
	}
	if out, err := runCmd("", nil, "gh", args...); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return "", fmt.Errorf("gh repo create: %s", out)
		}
	}
	url, err := runCmd("", nil, "gh", "repo", "view", name, "--json", "url", "-q", ".url")
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(url, ".git") {
		url += ".git"
	}
	return url, nil
}

func Clone(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	_, err := runCmd("", nil, "git", "clone", url, dest)
	return err
}
