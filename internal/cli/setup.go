package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/sync"
)

type setupOptions struct {
	NonInteractive bool
	Handle         string
	Timezone       string
	RepoName       string
	RepoPath       string
	Private        bool
	Public         bool
	SkipBackfill   bool
	MaxPerDay      int
}

func cmdSetup(args []string) error {
	fs := newFlagSet("setup")
	var o setupOptions
	fs.StringVar(&o.Handle, "handle", "", "boot.dev handle")
	fs.StringVar(&o.Timezone, "timezone", "", "IANA timezone")
	fs.StringVar(&o.RepoName, "repo-name", "", "GitHub repository name")
	fs.StringVar(&o.RepoPath, "repo-path", "", "local clone path")
	fs.BoolVar(&o.Private, "private", true, "create a private GitHub repo")
	fs.BoolVar(&o.Public, "public", false, "create a public GitHub repo (overrides --private)")
	fs.BoolVar(&o.SkipBackfill, "skip-backfill", false, "skip heatmap backfill")
	fs.IntVar(&o.MaxPerDay, "max-per-day", 0, "cap commits per day during backfill (0 = exact)")
	fs.BoolVar(&o.NonInteractive, "yes", false, "non-interactive (accept detected defaults)")
	fs.BoolVar(&o.NonInteractive, "non-interactive", false, "alias for --yes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runSetup(o)
}

func runSetup(opts setupOptions) error {
	fmt.Println("BootHub setup")
	fmt.Println("-------------")
	fmt.Println("Press Enter to accept detected defaults.")
	fmt.Println()

	if err := checkTools(); err != nil {
		return err
	}

	reader := promptReader()
	cfg := config.Default()

	// Handle: flag -> bootdev CLI session -> prompt.
	handle := strings.TrimPrefix(strings.TrimSpace(opts.Handle), "@")
	if handle == "" {
		fmt.Println("Detecting Boot.dev handle from bootdev login...")
		if detected, err := config.DetectBootdevHandle(); err == nil && detected != "" {
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

	// Timezone: flag -> OS (/etc/localtime, TZ) -> prompt.
	tz := strings.TrimSpace(opts.Timezone)
	if tz == "" {
		tz = config.DetectTimezone()
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
		repoPath = filepath.Join(home, filepath.Base(repoName))
	}
	if !opts.NonInteractive {
		repoPath = prompt(reader, "Local clone path", repoPath)
	}
	cfg.RepoPath = repoPath

	private := opts.Private && !opts.Public
	if !opts.NonInteractive {
		def := "Y"
		if !private {
			def = "N"
		}
		ans := prompt(reader, "Private GitHub repo? [Y/n]", def)
		private = !strings.EqualFold(strings.TrimSpace(ans), "n")
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
		stats, err := sync.New(cfg).Backfill(sync.BackfillOptions{
			Handle:    cfg.BootdevHandle,
			Timezone:  cfg.Timezone,
			MaxPerDay: opts.MaxPerDay,
		})
		if err != nil {
			return fmt.Errorf("backfill failed: %w", err)
		}
		fmt.Printf("Backfill done: %d commits across %d days (skipped %d days already matching)", stats.Created, stats.Days, stats.Skipped)
		if stats.CappedDays > 0 {
			fmt.Printf(" (%d days capped)", stats.CappedDays)
		}
		fmt.Println()
	}

	fmt.Println("\nYou're set. Daily usage:")
	fmt.Println("  boothub submit [UUID]")
	fmt.Println("  boothub run -s [UUID]")
	fmt.Println("  boothub sync --lesson <id>")
	fmt.Println("  boothub backfill            # incremental, cached")
	fmt.Println("  boothub repair --yes        # rebuild history for an exact match")
	fmt.Println()
	fmt.Println("Note: for your private submissions repo to count, enable")
	fmt.Println("GitHub -> Settings -> Profile -> \"Include private contributions on my profile\".")
	return nil
}

func checkTools() error {
	var missing []string
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
	r := gitops.Repo{Path: dest}
	user, userErr := gitops.CurrentUser()

	if r.IsRepo() {
		fmt.Printf("Using existing git repo at %s\n", dest)
		if userErr != nil {
			return fmt.Errorf("cannot determine GitHub identity: %w", userErr)
		}
		if err := r.EnsureIdentity(user); err != nil {
			return err
		}
		fmt.Printf("  commits will use GitHub user @%s (%s)\n", user.Login, user.Email)
		return nil
	}
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		entries, _ := os.ReadDir(dest)
		if len(entries) > 0 {
			return fmt.Errorf("path %s exists and is not an empty git repo", dest)
		}
	}
	if userErr != nil {
		return fmt.Errorf("cannot determine GitHub identity: %w", userErr)
	}

	fmt.Printf("Creating GitHub repo %s...\n", name)
	cloneURL, err := gitops.CreateGitHubRepo(name, private)
	if err != nil {
		return err
	}

	if err := gitops.Clone(cloneURL, dest); err != nil {
		fmt.Println("Clone failed; initializing locally...")
		if err := r.Init("main"); err != nil {
			return err
		}
		if err := r.SetRemote("origin", cloneURL); err != nil {
			return err
		}
	}
	if err := r.EnsureIdentity(user); err != nil {
		return err
	}
	if err := seedIfEmpty(r, user); err != nil {
		return err
	}

	fmt.Printf("  commits will use GitHub user @%s (%s)\n", user.Login, user.Email)
	fmt.Printf("Repo ready at %s\n", dest)
	return nil
}

// seedIfEmpty creates the first commit (and branch) when the repo has no
// commits, which is the normal state of a brand-new GitHub repo.
func seedIfEmpty(r gitops.Repo, user gitops.User) error {
	if head, err := r.Head(); err == nil && head != "" {
		return nil
	}
	readme := filepath.Join(r.Path, "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		if err := os.WriteFile(readme, []byte(gitops.DefaultReadme), 0o644); err != nil {
			return err
		}
	}
	if err := r.Add("README.md"); err != nil {
		return err
	}
	if err := r.Commit(&user, "Initial commit", nil, false); err != nil {
		return err
	}
	if url := r.RemoteURL("origin"); url != "" {
		if err := r.Push("origin", config.DefaultBranch); err != nil {
			return fmt.Errorf("initial push failed: %w", err)
		}
	}
	return nil
}

// promptReader returns a reader for interactive prompts. When stdin is not a
// terminal (e.g. `curl | bash`), it reads from /dev/tty so the user can still
// answer instead of silently accepting defaults.
func promptReader() *bufio.Reader {
	if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) != 0 {
		return bufio.NewReader(os.Stdin)
	}
	if tty, err := os.Open("/dev/tty"); err == nil {
		return bufio.NewReader(tty)
	}
	return bufio.NewReader(os.Stdin)
}

func prompt(r *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return def
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}
