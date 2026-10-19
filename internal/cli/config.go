package cli

import (
	"fmt"

	"github.com/Srikumar6529/boothub/internal/config"
	"github.com/Srikumar6529/boothub/internal/gitops"
	"github.com/Srikumar6529/boothub/internal/heatmap"
)

func cmdConfig(args []string) error {
	fs := newFlagSet("config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path, _ := config.Path()
	fmt.Println("Config path:     ", path)

	cfg, err := config.Load()
	if err != nil {
		fmt.Println("Status:           not configured (run: boothub setup)")
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

	r := gitops.Repo{Path: cfg.RepoPath}
	fmt.Println("git repo ok:     ", r.IsRepo())
	fmt.Println("git clean:       ", r.IsClean())
	fmt.Println("current branch:  ", r.CurrentBranch())
	fmt.Println("bootdev on PATH: ", gitops.CommandExists("bootdev"))
	fmt.Println("gh authenticated:", gitops.GitHubAuthenticated())

	if cachePath, err := heatmap.CachePath(cfg.BootdevHandle, cfg.Timezone); err == nil {
		if cached, ok := heatmap.LoadCache(cfg.BootdevHandle, cfg.Timezone); ok {
			fmt.Printf("heatmap cache:    %s (%d days, fetched %s)\n",
				cachePath, len(cached.Days), cached.FetchedAt.Format("2006-01-02 15:04"))
		} else {
			fmt.Println("heatmap cache:    none yet")
		}
	}

	fmt.Println("validation:      ", cmpErr(cfg.Validate()))
	return nil
}

func cmpErr(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}
