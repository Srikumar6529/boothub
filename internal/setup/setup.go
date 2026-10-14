package setup

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/detect"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/sync"
)

type Options struct {
	NonInteractive bool
	Handle         string
	Timezone       string
	RepoName       string
	RepoPath       string
	Private        bool
	SkipBackfill   bool
	MaxPerDay      int
}

func Run(opts Options) error {
	fmt.Println("BootHub setup")
	fmt.Println("-------------")
	fmt.Println("Press Enter to accept detected defaults.")
	fmt.Println()

	if err := checkTools(); err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)
	cfg := config.Default()

	// Handle: flag → bootdev CLI session → prompt
	handle := strings.TrimPrefix(strings.TrimSpace(opts.Handle), "@")
	if handle == "" {
		fmt.Println("Detecting Boot.dev handle from bootdev login…")
		if detected, err := detect.BootdevHandle(); err == nil && detected != "" {
			handle = detected
			fmt.Printf("  found @%s via bootdev CLI session\n", handle)
		} else if err != nil {
			fmt.Printf("  could not auto-detect (%v)\n", err)
		}
	}
	if !opts.NonInteractive {
		handle = prompt(reader, "Boot.dev handle", handle)
	}
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if handle == "" {
		return fmt.Errorf("boot.dev handle is required (run `bootdev login`, or pass --handle)")
	}
	cfg.BootdevHandle = handle

	// Timezone: flag → OS (/etc/localtime, TZ) → prompt
	tz := strings.TrimSpace(opts.Timezone)
	if tz == "" {
		tz = detect.Timezone()
		fmt.Printf("Detected timezone: %s\n", tz)
	}
	if !opts.NonInteractive {
		tz = prompt(reader, "Timezone (IANA, e.g. America/Chicago)", tz)
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	cfg.Timezone = tz

	repoName := opts.RepoName
	if repoName == "" {
		repoName = config.DefaultRepo
	}
	if !opts.NonInteractive {
		repoName = prompt(reader, "GitHub repo name", repoName)
	}

	repoPath := opts.RepoPath
	if repoPath == "" {
		home, _ := os.UserHomeDir()
		repoPath = filepath.Join(home, repoName)
	}
	if !opts.NonInteractive {
		repoPath = prompt(reader, "Local clone path", repoPath)
	}
	cfg.RepoPath = repoPath

	private := true
	if !opts.NonInteractive {
		ans := prompt(reader, "Private GitHub repo? [Y/n]", "Y")
		private = !strings.EqualFold(strings.TrimSpace(ans), "n")
	} else {
		private = opts.Private
	}

	if err := provisionRepo(repoName, repoPath, private); err != nil {
		return err
	}

	cfg.Enabled = true
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	cfgPath, _ := config.Path()
	fmt.Printf("\nSaved config to %s\n", cfgPath)
	fmt.Printf("Submissions repo: %s\n", cfg.RepoPath)

	doBackfill := !opts.SkipBackfill
	if !opts.NonInteractive && !opts.SkipBackfill {
		ans := prompt(reader, "Backfill past Boot.dev activity from your heatmap? [Y/n]", "Y")
		doBackfill = !strings.EqualFold(strings.TrimSpace(ans), "n")
	}
	if doBackfill {
		fmt.Println("\nRunning intensity-matched backfill (this may take a while)...")
		engine := sync.New(cfg)
		stats, err := engine.Backfill(sync.BackfillOptions{
			Handle:    cfg.BootdevHandle,
			Timezone:  cfg.Timezone,
			MaxPerDay: opts.MaxPerDay,
		})
		if err != nil {
			return fmt.Errorf("backfill failed: %w", err)
		}
		fmt.Printf("Backfill done: %d days, %d commits created, %d skipped", stats.Days, stats.Created, stats.Skipped)
		if stats.CappedDays > 0 {
			fmt.Printf(" (%d days capped)", stats.CappedDays)
		}
		fmt.Println()
	}

	fmt.Println("\nYou're set. Daily usage:")
	fmt.Println("  boothub submit [UUID]")
	fmt.Println("  boothub run -s [UUID]")
	fmt.Println("  boothub sync --lesson <id>")
	fmt.Println("  boothub backfill")
	return nil
}

func checkTools() error {
	missing := []string{}
	for _, t := range []string{"git", "gh"} {
		if !gitops.CommandExists(t) {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tools: %s", strings.Join(missing, ", "))
	}
	if !gitops.GitHubAuthenticated() {
		return fmt.Errorf("GitHub CLI not authenticated; run: gh auth login")
	}
	if !gitops.CommandExists("bootdev") {
		fmt.Println("Warning: bootdev not found on PATH.")
		fmt.Println("Install with: go install github.com/bootdotdev/bootdev@latest")
		fmt.Println("Then run: bootdev login")
	}
	return nil
}

func provisionRepo(name, dest string, private bool) error {
	repo := gitops.Repo{Path: dest}
	if repo.IsGitRepo() {
		fmt.Printf("Using existing git repo at %s\n", dest)
		_ = ensureGitIdentity(dest)
		return nil
	}
	if _, err := os.Stat(dest); err == nil {
		// Directory exists but is not a git repo.
		entries, _ := os.ReadDir(dest)
		if len(entries) > 0 {
			return fmt.Errorf("path %s exists and is not an empty git repo", dest)
		}
	}

	fmt.Printf("Creating GitHub repo %s...\n", name)
	cloneURL, err := gitops.CreateGitHubRepo(name, private)
	if err != nil {
		return err
	}

	// Prefer clone; if remote is empty, init locally and set remote.
	if err := gitops.Clone(cloneURL, dest); err != nil {
		fmt.Println("Clone of empty repo failed (normal for brand-new repos); initializing locally...")
		if err := repo.Init(); err != nil {
			return err
		}
		if err := ensureGitIdentity(dest); err != nil {
			return err
		}
		if err := repo.SetRemote("origin", cloneURL); err != nil {
			return err
		}
		// Seed README so first push has a branch.
		readme := filepath.Join(dest, "README.md")
		content := "# bootdev-submissions\n\nSynced automatically by [BootHub](https://github.com/Srikumar6529/boothub).\n"
		if err := os.WriteFile(readme, []byte(content), 0o644); err != nil {
			return err
		}
		if err := repo.Add("README.md"); err != nil {
			return err
		}
		if err := repo.Commit("Initial commit", nil); err != nil {
			return err
		}
		if err := repo.Push("origin", "main"); err != nil {
			return fmt.Errorf("initial push failed: %w", err)
		}
	}
	_ = ensureGitIdentity(dest)
	fmt.Printf("Repo ready at %s\n", dest)
	return nil
}

func ensureGitIdentity(repoPath string) error {
	// Prefer existing global identity; only set local fallback if missing.
	check := exec.Command("git", "config", "--get", "user.email")
	check.Dir = repoPath
	if out, err := check.Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		return nil
	}
	cmds := [][]string{
		{"config", "user.email", "boothub@users.noreply.github.com"},
		{"config", "user.name", "boothub"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

func prompt(r *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

