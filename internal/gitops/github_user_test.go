package gitops

import "testing"

func TestSSHToHTTPS(t *testing.T) {
	cases := map[string]string{
		"git@github.com:Owner/repo.git":           "https://github.com/Owner/repo.git",
		"ssh://git@github.com/Owner/repo.git":     "https://github.com/Owner/repo.git",
		"https://github.com/Owner/repo.git":       "https://github.com/Owner/repo.git",
		"https://github.com/Owner/repo":           "https://github.com/Owner/repo",
	}
	for in, want := range cases {
		if got := sshToHTTPS(in); got != want {
			t.Fatalf("%s → %s want %s", in, got, want)
		}
	}
}
