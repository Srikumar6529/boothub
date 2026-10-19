package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// modulePath is the root import path of this tool. `go install <modulePath>@latest`
// rebuilds and installs the newest version, which is what `boothub upgrade` does.
const modulePath = "github.com/Srikumar6529/boothub"

func cmdUpgrade(args []string) error {
	fs := newFlagSet("upgrade")
	check := fs.Bool("check", false, "show the current and latest version without installing")
	to := fs.String("version", "latest", "version to install (e.g. latest or v1.2.3)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *check {
		fmt.Printf("current: %s\n", buildVersion())
		latest, err := latestVersion()
		if err != nil {
			return err
		}
		fmt.Printf("latest:  %s\n", latest)
		return nil
	}

	if !commandExists("go") {
		return fmt.Errorf("Go is required to upgrade; install it from https://go.dev/dl/ (or reinstall with install.sh)")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the running binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	binDir := filepath.Dir(exe)

	target := modulePath + "@" + strings.TrimSpace(*to)
	fmt.Printf("Installing %s into %s ...\n", target, binDir)

	cmd := exec.Command("go", "install", target)
	cmd.Env = append(os.Environ(), "GOBIN="+binDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed (is %s writable? try running with the right permissions): %w", binDir, err)
	}

	fmt.Println("Upgrade complete.")
	return nil
}

// latestVersion asks the module proxy for the newest version of this module.
func latestVersion() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}", modulePath+"@latest").Output()
	if err != nil {
		return "", fmt.Errorf("could not determine latest version (is Go installed?): %w", err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("module proxy returned an empty version")
	}
	return v, nil
}
