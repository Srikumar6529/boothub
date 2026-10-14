package cli

import (
	"github.com/spf13/cobra"
	"github.com/Srikumar6529/boothub/internal/setup"
)

func newSetupCmd() *cobra.Command {
	opts := setup.Options{Private: true}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-time setup: GitHub repo, config, optional backfill",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Run(opts)
		},
	}
	cmd.Flags().StringVar(&opts.Handle, "handle", "", "Boot.dev handle")
	cmd.Flags().StringVar(&opts.Timezone, "timezone", "", "IANA timezone")
	cmd.Flags().StringVar(&opts.RepoName, "repo-name", "", "GitHub repository name")
	cmd.Flags().StringVar(&opts.RepoPath, "repo-path", "", "Local clone path")
	cmd.Flags().BoolVar(&opts.Private, "private", true, "Create a private GitHub repo")
	cmd.Flags().BoolVar(&opts.SkipBackfill, "skip-backfill", false, "Skip heatmap backfill")
	cmd.Flags().IntVar(&opts.MaxPerDay, "max-per-day", 0, "Cap commits per day during backfill (0 = uncapped)")
	cmd.Flags().BoolVar(&opts.NonInteractive, "yes", false, "Non-interactive mode (auto-detects handle from bootdev login if omitted)")
	return cmd
}

func newInitCmd() *cobra.Command {
	cmd := newSetupCmd()
	cmd.Use = "init"
	cmd.Short = "Alias for setup"
	return cmd
}
