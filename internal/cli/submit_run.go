package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/Srikumar6529/boothub/internal/bootdev"
	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/marker"
	"github.com/Srikumar6529/boothub/internal/sync"
)

func newSubmitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "submit [args...]",
		Short: "Proxy bootdev submit; sync to GitHub on success",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return proxyAndMaybeSync(append([]string{"submit"}, args...))
		},
	}
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [args...]",
		Short: "Proxy bootdev run; sync only with -s/--submit on success",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return proxyAndMaybeSync(append([]string{"run"}, args...))
		},
	}
}

func proxyAndMaybeSync(args []string) error {
	cfg, cfgErr := config.Load()
	// Allow running bootdev even if config missing; only sync needs config.

	result, err := bootdev.Proxy(bootdev.RunRequest{
		Binary: cfg.BootdevBinary,
		Args:   args,
	})
	if err != nil {
		return err
	}

	if !result.Submitted {
		os.Exit(result.ExitCode)
		return nil
	}

	if !bootdev.Passed(result) {
		os.Exit(result.ExitCode)
		return nil
	}

	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "boothub: bootdev passed, but sync skipped: %v\n", cfgErr)
		os.Exit(result.ExitCode)
		return nil
	}

	trigger := marker.TriggerCLISubmit
	if result.Command == "run" {
		trigger = marker.TriggerCLIRunSubmit
	}

	engine := sync.New(cfg)
	syncResult, syncErr := engine.Sync(sync.Request{
		LessonID: result.LessonID,
		Trigger:  trigger,
		When:     time.Now(),
	})
	if syncErr != nil {
		fmt.Fprintf(os.Stderr, "boothub: warning: GitHub sync failed: %v\n", syncErr)
	} else if syncResult.Skipped {
		fmt.Fprintf(os.Stderr, "boothub: %s\n", syncResult.Message)
	} else {
		fmt.Fprintf(os.Stderr, "boothub: synced %s\n", syncResult.RelPath)
	}

	os.Exit(result.ExitCode)
	return nil
}
