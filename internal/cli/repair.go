package cli

import (
	"fmt"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/sync"
	"github.com/spf13/cobra"
)

func newRepairCmd() *cobra.Command {
	opts := sync.RepairOptions{Push: true}

	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Rebuild the submissions history to exactly match the heatmap",
		Long: `Rewrites the submissions repository so that each day has exactly as many
commits as your Boot.dev heatmap shows. Use this once to clean up surplus or
shifted commits from older versions of BootHub; afterwards plain ` + "`boothub backfill`" + `
stays in sync incrementally.

The old history is kept on a boothub-backup-* branch (locally and on GitHub)
before the rewrite, and the new history is force-pushed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			engine := sync.New(cfg)

			if !opts.Yes && !opts.DryRun {
				// Show the plan first, then ask the user to confirm.
				planOpts := opts
				planOpts.DryRun = true
				planOpts.Yes = false
				stats, err := engine.Repair(planOpts)
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
		},
	}

	cmd.Flags().StringVar(&opts.Handle, "handle", "", "Boot.dev handle (default: config)")
	cmd.Flags().StringVar(&opts.Timezone, "timezone", "", "IANA timezone (default: config)")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Confirm the rewrite and force-push")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be rebuilt without changing anything")
	cmd.Flags().BoolVar(&opts.Refresh, "refresh", false, "Force a heatmap API refresh even if the cache is fresh")
	cmd.Flags().BoolVar(&opts.NoRefresh, "no-refresh", false, "Never call the API; use the cached heatmap")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Only rebuild days on or after YYYY-MM-DD")
	cmd.Flags().StringVar(&opts.Until, "until", "", "Only rebuild days on or before YYYY-MM-DD")
	cmd.Flags().IntVar(&opts.MaxPerDay, "max-per-day", 0, "Cap commits per day (0 = exact match)")
	cmd.Flags().BoolVar(&opts.IncludeGitHubCommits, "include-github-commits", false, "Also count Boot.dev's view of your real GitHub commits")
	return cmd
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
