package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/sync"
)

func newBackfillCmd() *cobra.Command {
	var (
		handle    string
		timezone  string
		maxPerDay int
	)
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Intensity-match GitHub commits from your Boot.dev activity heatmap",
		Long: `Fetches your public Boot.dev activity heatmap and creates one backdated
git commit per activity unit (count commits for a day with count N).

Already-synced backfill-N.json markers are skipped (idempotent).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			engine := sync.New(cfg)
			fmt.Println("Fetching Boot.dev activity heatmap...")
			stats, err := engine.Backfill(sync.BackfillOptions{
				Handle:    handle,
				Timezone:  timezone,
				MaxPerDay: maxPerDay,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Days: %d | Planned units: %d | Created: %d | Skipped: %d",
				stats.Days, stats.TotalPlanned, stats.Created, stats.Skipped)
			if stats.CappedDays > 0 {
				fmt.Printf(" | Capped days: %d", stats.CappedDays)
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().StringVar(&handle, "handle", "", "Boot.dev handle (default: config)")
	cmd.Flags().StringVar(&timezone, "timezone", "", "IANA timezone (default: config)")
	cmd.Flags().IntVar(&maxPerDay, "max-per-day", 0, "Cap commits per day (0 = uncapped intensity match)")
	return cmd
}
