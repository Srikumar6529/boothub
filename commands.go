package main

import (
	"fmt"
	"time"
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

	a, err := newApp()
	if err != nil {
		return err
	}

	when := time.Now()
	if date != "" {
		loc, err := a.cfg.location()
		if err != nil {
			return err
		}
		parsed, err := time.ParseInLocation("2006-01-02", date, loc)
		if err != nil {
			return fmt.Errorf("invalid --date (want YYYY-MM-DD): %w", err)
		}
		when = noonLocal(parsed, loc)
	}
	if lesson == "" {
		lesson = fmt.Sprintf("manual-%d", when.Unix())
	}

	res, err := a.syncOne(syncRequest{LessonID: lesson, Trigger: triggerManual, When: when, Note: note})
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

func cmdBackfill(args []string) error {
	fs := newFlagSet("backfill")
	var (
		opts  backfillOptions
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

	a, err := newApp()
	if err != nil {
		return err
	}
	if !opts.DryRun {
		fmt.Println("Syncing Boot.dev activity to GitHub...")
	}
	stats, err := a.backfill(opts)
	if err != nil {
		return err
	}
	printBackfillStats(stats)
	return nil
}

func printBackfillStats(stats backfillStats) {
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

func cmdRepair(args []string) error {
	fs := newFlagSet("repair")
	opts := repairOptions{Push: true}
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

	a, err := newApp()
	if err != nil {
		return err
	}

	if !opts.Yes && !opts.DryRun {
		plan := opts
		plan.DryRun = true
		plan.Yes = false
		stats, err := a.repair(plan)
		if err != nil {
			return err
		}
		printRepairStats(stats, true)
		fmt.Println("\nThis rewrites history and force-pushes. Re-run with --yes to apply.")
		return nil
	}

	stats, err := a.repair(opts)
	if err != nil {
		return err
	}
	printRepairStats(stats, opts.DryRun)
	return nil
}

func printRepairStats(stats repairStats, dryRun bool) {
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
