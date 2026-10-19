package cli

import (
	"fmt"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/marker"
	"github.com/Srikumar6529/boothub/internal/sync"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var (
		lesson string
		date   string
		note   string
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Manually write a marker commit (for web lessons until the extension)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			when := time.Now()
			if date != "" {
				loc, err := cfg.Location()
				if err != nil {
					return err
				}
				parsed, err := time.ParseInLocation("2006-01-02", date, loc)
				if err != nil {
					return fmt.Errorf("invalid --date (want YYYY-MM-DD): %w", err)
				}
				when = sync.NoonLocal(parsed, loc)
			}
			if lesson == "" {
				lesson = fmt.Sprintf("manual-%d", when.Unix())
			}
			engine := sync.New(cfg)
			res, err := engine.Sync(sync.Request{
				LessonID: lesson,
				Trigger:  marker.TriggerManual,
				When:     when,
				Note:     note,
				Backdate: date != "",
			})
			if err != nil {
				return err
			}
			if res.Skipped {
				fmt.Println(res.Message)
			} else {
				fmt.Printf("synced %s\n", res.RelPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&lesson, "lesson", "", "Lesson id / slug")
	cmd.Flags().StringVar(&date, "date", "", "Calendar date YYYY-MM-DD (backdates commit)")
	cmd.Flags().StringVar(&note, "note", "", "Optional note")
	return cmd
}
