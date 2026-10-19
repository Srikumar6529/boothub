package main

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

// ghUser is the identity from `gh auth` (not the global git config).
type ghUser struct {
	Login string
	ID    int64
	Name  string
	Email string
}

func currentGHUser() (ghUser, error) {
	out, err := runCmd("", nil, "gh", "api", "user", "--jq", "{login:.login,id:.id,name:.name}")
	if err != nil {
		return ghUser{}, fmt.Errorf("%s (run: gh auth login)", err)
	}
	var raw struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return ghUser{}, fmt.Errorf("parse gh user: %w", err)
	}
	if raw.Login == "" || raw.ID == 0 {
		return ghUser{}, fmt.Errorf("gh api user returned empty login/id")
	}
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		name = raw.Login
	}
	// Linking commits to the account without exposing a personal email.
	email := fmt.Sprintf("%d+%s@users.noreply.github.com", raw.ID, raw.Login)
	return ghUser{Login: raw.Login, ID: raw.ID, Name: name, Email: email}, nil
}

type repo struct{ path string }

func (r repo) run(env []string, args ...string) (string, error) {
	return runCmd(r.path, env, "git", args...)
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

func (r repo) add(paths ...string) error {
	args := append([]string{"add", "--"}, paths...)
	_, err := r.run(nil, args...)
	return err
}

// commit creates a commit authored/committed by user. when==nil uses now.
func (r repo) commit(user *ghUser, msg string, when *time.Time, allowEmpty bool) error {
	env := []string{}
	if user != nil {
		env = append(env,
			"GIT_AUTHOR_NAME="+user.Name, "GIT_AUTHOR_EMAIL="+user.Email,
			"GIT_COMMITTER_NAME="+user.Name, "GIT_COMMITTER_EMAIL="+user.Email,
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

func (r repo) resetHard(ref string) error {
	args := []string{"reset", "--hard"}
	if ref != "" {
		args = append(args, ref)
	}
	_, err := r.run(nil, args...)
	return err
}

func (r repo) head() (string, error) { return r.run(nil, "rev-parse", "HEAD") }

func (r repo) currentBranch() string {
	out, err := r.run(nil, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

func (r repo) isRepo() bool {
	_, err := r.run(nil, "rev-parse", "--git-dir")
	return err == nil
}

func (r repo) isClean() bool {
	out, err := r.run(nil, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) == ""
}

func (r repo) fileInHEAD(rel string) bool {
	_, err := r.run(nil, "cat-file", "-e", "HEAD:"+rel)
	return err == nil
}

func (r repo) filesInHEAD(prefix string) ([]string, error) {
	out, err := r.run(nil, "ls-tree", "-r", "--name-only", "HEAD", "--", prefix)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// stagedOutside returns staged paths that are not under prefix, so a commit
// never sweeps up unrelated files.
func (r repo) stagedOutside(prefix string) ([]string, error) {
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

// markerCounts counts marker commits per calendar day in loc. This is the exact
// number GitHub shows for the repository.
func (r repo) markerCounts(loc *time.Location) map[string]int {
	out, err := r.run(nil, "log", "--format=%at%x09%s")
	if err != nil {
		return map[string]int{}
	}
	counts := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		ts, subj, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasPrefix(subj, markerPrefix) {
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

func (r repo) aheadOfRemote(remote, branch string) bool {
	if _, err := r.run(nil, "rev-parse", "--verify", remote+"/"+branch); err != nil {
		_, headErr := r.run(nil, "rev-parse", "--verify", "HEAD")
		return headErr == nil
	}
	out, err := r.run(nil, "rev-list", "--count", remote+"/"+branch+"..HEAD")
	return err == nil && strings.TrimSpace(out) != "0"
}

func (r repo) push(remote, branch string) error      { return r.pushArgs(false, remote, branch) }
func (r repo) pushForce(remote, branch string) error { return r.pushArgs(true, remote, branch) }

func (r repo) pushArgs(force bool, remote, branch string) error {
	args := []string{"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential", "push", "-u"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, remote, branch)
	_, err := r.run(nil, args...)
	return err
}

func (r repo) fetch(remote string) error {
	_, err := r.run(nil, "fetch", "--quiet", remote)
	return err
}

// ensureIdentity sets repo-local identity from gh and uses gh for push auth.
// It never touches global git config.
func (r repo) ensureIdentity(u ghUser) error {
	if _, err := r.run(nil, "config", "--local", "user.name", u.Name); err != nil {
		return err
	}
	if _, err := r.run(nil, "config", "--local", "user.email", u.Email); err != nil {
		return err
	}
	return r.useGHCredentials()
}

func (r repo) useGHCredentials() error {
	_, _ = r.run(nil, "config", "--local", "--unset-all", "credential.helper")
	_, _ = r.run(nil, "config", "--local", "--add", "credential.helper", "")
	if _, err := r.run(nil, "config", "--local", "--add", "credential.helper", "!gh auth git-credential"); err != nil {
		return fmt.Errorf("set local gh credential helper: %w", err)
	}
	return r.ensureHTTPSRemote("origin")
}

func (r repo) ensureHTTPSRemote(name string) error {
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

func (r repo) remoteURL(name string) string {
	out, _ := r.run(nil, "remote", "get-url", name)
	return strings.TrimSpace(out)
}

func (r repo) setRemote(name, url string) error {
	_, _ = r.run(nil, "remote", "remove", name)
	_, err := r.run(nil, "remote", "add", name, url)
	return err
}

func (r repo) init(branch string) error {
	if err := os.MkdirAll(r.path, 0o755); err != nil {
		return err
	}
	if branch == "" {
		branch = defaultBranch
	}
	_, err := r.run(nil, "init", "-b", branch)
	return err
}

func (r repo) createBranch(name, ref string) error {
	_, err := r.run(nil, "branch", "-f", name, ref)
	return err
}

func (r repo) deleteBranch(name string) error {
	_, err := r.run(nil, "branch", "-D", name)
	return err
}

// importEntry is one commit for fast-import: a file to add at a backdated time.
type importEntry struct {
	rel     string
	when    time.Time
	content []byte
	message string
}

// fastImport builds `entries` commits in a single git subprocess. parent is the
// commit to start from ("" builds an orphan history). readme, when set, is
// committed alongside the first entry.
func (r repo) fastImport(branch, parent string, entries []importEntry, user ghUser, readme []byte) error {
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
		mark := writeBlob(e.content)
		fmt.Fprintf(&b, "commit refs/heads/%s\n", branch)
		fmt.Fprintf(&b, "author %s <%s> %d %s\n", user.Name, user.Email, e.when.Unix(), e.when.Format("-0700"))
		fmt.Fprintf(&b, "committer %s <%s> %d %s\n", user.Name, user.Email, e.when.Unix(), e.when.Format("-0700"))
		fmt.Fprintf(&b, "data %d\n%s\n", len(e.message), e.message)
		if i == 0 && parent != "" {
			fmt.Fprintf(&b, "from %s\n", parent)
		}
		fmt.Fprintf(&b, "M 100644 :%d %s\n", mark, e.rel)
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
	cmd.Dir = r.path
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

func formatGitDate(t time.Time) string { return t.Format("2006-01-02T15:04:05-0700") }

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func ghAuthenticated() bool {
	return exec.Command("gh", "auth", "status").Run() == nil
}

func createGitHubRepo(name string, private bool) (string, error) {
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

func cloneRepo(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	_, err := runCmd("", nil, "git", "clone", url, dest)
	return err
}
