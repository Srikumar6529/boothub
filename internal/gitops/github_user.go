package gitops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GitHubUser is the account from `gh auth` (not global git config).
type GitHubUser struct {
	Login string
	ID    int64
	Name  string
	Email string // noreply address that attributes to this GitHub account
}

func CurrentGitHubUser() (GitHubUser, error) {
	cmd := exec.Command("gh", "api", "user", "--jq", "{login:.login,id:.id,name:.name}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return GitHubUser{}, fmt.Errorf("gh api user: %s (run: gh auth login)", msg)
	}

	var raw struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return GitHubUser{}, fmt.Errorf("parse gh user: %w", err)
	}
	if raw.Login == "" || raw.ID == 0 {
		return GitHubUser{}, fmt.Errorf("gh api user returned empty login/id")
	}
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		name = raw.Login
	}
	// GitHub documents this form as linking commits to the account without
	// exposing a personal email — works even when "Keep my email private" is on.
	email := fmt.Sprintf("%d+%s@users.noreply.github.com", raw.ID, raw.Login)
	return GitHubUser{Login: raw.Login, ID: raw.ID, Name: name, Email: email}, nil
}
