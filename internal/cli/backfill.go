package cli

import (
	"fmt"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/sync"
)

func cmdBackfill(args []string) error {
	fs := newFlagSet("backfill")
	var (
		opts  sync.BackfillOptions
		force bool
	)
	fs.StringVar(&opts.Handle, "handle", "", "boot.dev handle (default: config)")
	fs.StringVar(&opts.Timezone, "timezone", "", "IANA timezone (default: config)")
	fs.IntVar(&opts.MaxPerDay, "max-per-day", 0, "cap commits per day (0 = exact match)")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "show what would be created without committing")
	fs.BoolVar(&opts.Refresh, "refresh", false, "force a heatmap API refresh even if the cache is fresh")
	fs.BoolVar(&opts.NoRefresh, "no-refresh", false, "never call the API; use the cached heatmap")
	fs.StringVar(&opts.Since, "since", "", "only sync days on or after YYYY-MM-DD")
	fs.StringVar(&opts.Until, "until", "", "only sync days on or before YYYY-MM-DD")
	fs.BoolVar(&opts.IncludeGitHubCommits, "include-github-commits", false, "also count Boot.dev's view of your real GitHub commits")
	fs.BoolVar(&force, "force", false, "deprecated: use `boothub repair --yes`")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if force {
		return fmt.Errorf("`backfill --force` was replaced by `boothub repair --yes`:\n" +
			"  backfill only adds missing commits; repair rewrites history to match the heatmap exactly")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !opts.DryRun {
		fmt.Println("Syncing Boot.dev activity to GitHub...")
	}
	stats, err := sync.New(cfg).Backfill(opts)
	if err != nil {
		return err
	}
	printBackfillStats(stats)
	return nil
}

func printBackfillStats(stats sync.BackfillStats) {
	prefix := "created: "
	if stats.DryRun {
		prefix = "[dry-run] would create: "
	}
	source := "heatmap cache"
	if stats.Refreshed {
		source = "heatmap API"
	}
	fmt.Printf("%s%d commits across %d days (target %d, existing %d) [source: %s]\n",
		prefix, stats.Created, stats.Days, stats.Target, stats.Existing, source)
	if stats.CappedDays > 0 {
		fmt.Printf("capped days: %d\n", stats.CappedDays)
	}
	if stats.OverCommits > 0 {
		fmt.Printf("note: %d days have %d more commits than Boot.dev shows.\n"+
			"      Run `boothub repair --yes` to rebuild history for an exact match.\n",
			stats.OverDays, stats.OverCommits)
	}
	if !stats.DryRun {
		if stats.Pushed {
			fmt.Println("pushed to GitHub")
		} else if stats.Created == 0 {
			fmt.Println("nothing to do - GitHub already matches Boot.dev")
		}
	}
}
