package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/marker"
	"github.com/Srikumar6529/boothub/internal/proxy"
	"github.com/Srikumar6529/boothub/internal/sync"
)

// cmdProxy runs the bootdev CLI and syncs a marker commit when it passes.
func cmdProxy(command string, args []string) error {
	cfg, cfgErr := config.Load() // config is only needed for syncing

	result, err := proxy.Run(cfg.BootdevBinary, append([]string{command}, args...))
	if err != nil {
		return err
	}
	if !result.Submitted || !proxy.Passed(result) {
		os.Exit(result.ExitCode)
	}
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "boothub: bootdev passed, but sync skipped: %v\n", cfgErr)
		os.Exit(result.ExitCode)
	}

	trigger := marker.TriggerCLISubmit
	if result.Command == "run" {
		trigger = marker.TriggerCLIRunSubmit
	}

	lessonID := result.LessonID
	if lessonID == "" {
		lessonID = proxy.LessonIDFromOutput(result)
	}

	engine := sync.New(cfg)
	res, syncErr := engine.Sync(sync.Request{
		LessonID: lessonID,
		Trigger:  trigger,
		When:     time.Now(),
	})
	switch {
	case syncErr != nil:
		fmt.Fprintf(os.Stderr, "boothub: warning: GitHub sync failed: %v\n", syncErr)
	case res.Skipped:
		fmt.Fprintf(os.Stderr, "boothub: %s\n", res.Message)
	default:
		fmt.Fprintf(os.Stderr, "boothub: synced %s\n", res.RelPath)
	}

	os.Exit(result.ExitCode)
	return nil
}
