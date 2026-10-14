package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/gitops"
)

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show BootHub configuration and status",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			fmt.Println("Config path:", path)

			cfg, err := config.Load()
			if err != nil {
				fmt.Println("Status: not configured (", err, ")")
				return nil
			}
			fmt.Println("repo_path:     ", cfg.RepoPath)
			fmt.Println("remote:        ", cfg.Remote)
			fmt.Println("branch:        ", cfg.Branch)
			fmt.Println("bootdev_handle:", cfg.BootdevHandle)
			fmt.Println("timezone:      ", cfg.Timezone)
			fmt.Println("enabled:       ", cfg.Enabled)
			if cfg.BootdevBinary != "" {
				fmt.Println("bootdev_binary:", cfg.BootdevBinary)
			}

			repo := gitops.Repo{Path: cfg.RepoPath}
			fmt.Println("git repo ok:   ", repo.IsGitRepo())
			fmt.Println("bootdev on PATH:", gitops.CommandExists("bootdev"))
			fmt.Println("gh authenticated:", gitops.GitHubAuthenticated())
			if err := cfg.Validate(); err != nil {
				fmt.Println("validation:    ", err)
			} else {
				fmt.Println("validation:     ok")
			}
			return nil
		},
	}
}
