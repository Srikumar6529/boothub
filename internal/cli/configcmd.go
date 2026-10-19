package cli

import (
	"fmt"

	"github.com/Srikumar6529/boothub/internal/cache"
	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/spf13/cobra"
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
			fmt.Println("Config path:     ", path)

			cfg, err := config.Load()
			if err != nil {
				fmt.Println("Status:           not configured (", err, ")")
				return nil
			}
			fmt.Println("repo_path:       ", cfg.RepoPath)
			fmt.Println("remote:          ", cfg.Remote)
			fmt.Println("branch:          ", cfg.Branch)
			fmt.Println("bootdev_handle:  ", cfg.BootdevHandle)
			fmt.Println("timezone:        ", cfg.Timezone)
			fmt.Println("enabled:         ", cfg.Enabled)
			if cfg.BootdevBinary != "" {
				fmt.Println("bootdev_binary:  ", cfg.BootdevBinary)
			}

			repo := gitops.Repo{Path: cfg.RepoPath}
			fmt.Println("git repo ok:     ", repo.IsGitRepo())
			fmt.Println("git clean:       ", repo.IsClean())
			fmt.Println("current branch:  ", repo.CurrentBranch())
			fmt.Println("bootdev on PATH: ", gitops.CommandExists("bootdev"))
			fmt.Println("gh authenticated:", gitops.GitHubAuthenticated())

			if cachePath, err := cache.Path(cfg.BootdevHandle, cfg.Timezone); err == nil {
				if cached, ok, _ := cache.Load(cfg.BootdevHandle, cfg.Timezone); ok {
					fmt.Printf("heatmap cache:    %s (%d days, fetched %s)\n",
						cachePath, len(cached.Days), cached.FetchedAt.Format("2006-01-02 15:04"))
				} else {
					fmt.Println("heatmap cache:    none yet")
				}
			}

			if err := cfg.Validate(); err != nil {
				fmt.Println("validation:      ", err)
			} else {
				fmt.Println("validation:       ok")
			}
			return nil
		},
	}
}
