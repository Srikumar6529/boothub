package cli

import (
	"fmt"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/marker"
	"github.com/Srikumar6529/boothub/internal/sync"
)

func cmdSync(args []string) error {
	fs := newFlagSet("sync")
	var lesson, date, note string
	fs.StringVar(&lesson, "lesson", "", "lesson id / slug")
	fs.StringVar(&date, "date", "", "calendar date YYYY-MM-DD (backdates the commit)")
	fs.StringVar(&note, "note", "", "optional note")
	if err := fs.Parse(args); err != nil {
		return err
	}

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
	res, err := engine.Sync(sync.Request{LessonID: lesson, Trigger: marker.TriggerManual, When: when, Note: note})
	if err != nil {
		return err
	}
	if res.Skipped {
		fmt.Println(res.Message)
	} else {
		fmt.Printf("synced %s\n", res.RelPath)
	}
	return nil
}
