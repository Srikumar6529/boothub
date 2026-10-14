package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func Execute() int {
	root := &cobra.Command{
		Use:   "boothub",
		Short: "Sync Boot.dev submissions to your GitHub contribution graph",
		Long: `BootHub proxies bootdev submit/run and writes marker commits to a
GitHub repo you own so your contribution graph turns green.

Also backfills past Boot.dev activity from your public profile heatmap.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newSetupCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newSubmitCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newBackfillCmd())
	root.AddCommand(newConfigCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}
