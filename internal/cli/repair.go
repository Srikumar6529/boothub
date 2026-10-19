package cli

import (
	"fmt"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/sync"
)

func cmdRepair(args []string) error {
	fs := newFlagSet("repair")
	opts := sync.RepairOptions{Push: true}
	fs.StringVar(&opts.Handle, "handle", "", "boot.dev handle (default: config)")
	fs.StringVar(&opts.Timezone, "timezone", "", "IANA timezone (default: config)")
	fs.BoolVar(&opts.Yes, "yes", false, "confirm the rewrite and force-push")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "show what would be rebuilt without changing anything")
	fs.BoolVar(&opts.Refresh, "refresh", false, "force a heatmap API refresh even if the cache is fresh")
	fs.BoolVar(&opts.NoRefresh, "no-refresh", false, "never call the API; use the cached heatmap")
	fs.StringVar(&opts.Since, "since", "", "only rebuild days on or after YYYY-MM-DD")
	fs.StringVar(&opts.Until, "until", "", "only rebuild days on or before YYYY-MM-DD")
	fs.IntVar(&opts.MaxPerDay, "max-per-day", 0, "cap commits per day (0 = exact match)")
	fs.BoolVar(&opts.IncludeGitHubCommits, "include-github-commits", false, "also count Boot.dev's view of your real GitHub commits")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	engine := sync.New(cfg)

	if !opts.Yes && !opts.DryRun {
		plan := opts
		plan.DryRun = true
		plan.Yes = false
		stats, err := engine.Repair(plan)
		if err != nil {
			return err
		}
		printRepairStats(stats, true)
		fmt.Println("\nThis rewrites history and force-pushes. Re-run with --yes to apply.")
		return nil
	}

	stats, err := engine.Repair(opts)
	if err != nil {
		return err
	}
	printRepairStats(stats, opts.DryRun)
	return nil
}

func printRepairStats(stats sync.RepairStats, dryRun bool) {
	source := "heatmap cache"
	if stats.Refreshed {
		source = "heatmap API"
	}
	mode := "rebuilt"
	if dryRun {
		mode = "[dry-run] would rebuild"
	}
	fmt.Printf("%s %d commits across %d days [source: %s]\n", mode, stats.Commits, stats.Days, source)
	if stats.Preserved > 0 {
		fmt.Printf("preserved %d real submission markers\n", stats.Preserved)
	}
	if stats.Dropped > 0 {
		fmt.Printf("dropped %d surplus markers (days or counts Boot.dev no longer shows)\n", stats.Dropped)
	}
	if stats.OldCommits > 0 {
		fmt.Printf("previous history: %d marker commits\n", stats.OldCommits)
	}
	if stats.Backup != "" {
		fmt.Printf("backup branch: %s\n", stats.Backup)
	}
	if stats.Pushed {
		fmt.Println("force-pushed to GitHub")
	}
}
