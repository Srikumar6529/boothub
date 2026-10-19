package cli

import (
	"fmt"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/sync"
	"github.com/spf13/cobra"
)

func newBackfillCmd() *cobra.Command {
	opts := sync.BackfillOptions{}
	var force bool

	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Intensity-match GitHub commits from your Boot.dev activity heatmap",
		Long: `Fetches your Boot.dev activity heatmap and makes the number of commits per
day equal the activity count Boot.dev shows for that day.

Past days are served from a local cache and only the current day is re-fetched,
so repeat runs are fast and do not re-create existing commits.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if force {
				return fmt.Errorf("`backfill --force` was replaced by `boothub repair --yes`:\n" +
					"  backfill only adds missing commits; repair rewrites history to match the heatmap exactly")
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			engine := sync.New(cfg)
			if !opts.DryRun {
				fmt.Println("Syncing Boot.dev activity to GitHub...")
			}
			stats, err := engine.Backfill(opts)
			if err != nil {
				return err
			}
			printBackfillStats(stats)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Handle, "handle", "", "Boot.dev handle (default: config)")
	cmd.Flags().StringVar(&opts.Timezone, "timezone", "", "IANA timezone (default: config)")
	cmd.Flags().IntVar(&opts.MaxPerDay, "max-per-day", 0, "Cap commits per day (0 = exact match)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be created without committing")
	cmd.Flags().BoolVar(&opts.Refresh, "refresh", false, "Force a heatmap API refresh even if the cache is fresh")
	cmd.Flags().BoolVar(&opts.NoRefresh, "no-refresh", false, "Never call the API; use the cached heatmap")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Only sync days on or after YYYY-MM-DD")
	cmd.Flags().StringVar(&opts.Until, "until", "", "Only sync days on or before YYYY-MM-DD")
	cmd.Flags().BoolVar(&opts.IncludeGitHubCommits, "include-github-commits", false, "Also count Boot.dev's view of your real GitHub commits")
	cmd.Flags().BoolVar(&force, "force", false, "Deprecated: use `boothub repair --yes`")
	_ = cmd.Flags().MarkHidden("force")
	return cmd
}

func printBackfillStats(stats sync.BackfillStats) {
	prefix := ""
	if stats.DryRun {
		prefix = "[dry-run] would create: "
	} else {
		prefix = "created: "
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
			fmt.Println("nothing to do — GitHub already matches Boot.dev")
		}
	}
}
