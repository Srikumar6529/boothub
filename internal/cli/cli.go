package cli

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
)

const usage = `boothub - turn Boot.dev activity into GitHub contribution greens

Usage:
  boothub <command> [flags]

Core:
  setup                 one-time setup: GitHub repo, config, optional backfill
  submit [args...]      proxy bootdev submit, then sync on success
  run [args...]         proxy bootdev run (syncs with -s / --submit)
  sync                  record a marker manually (--lesson <id>)
  backfill              match GitHub commits to your Boot.dev heatmap
  repair --yes          rebuild history so the counts match exactly

Other:
  upgrade               update boothub to the latest version
  config                show configuration and status
  version               print version
  help                  show this help

Any flags after "submit" or "run" are passed straight to bootdev, e.g.
  boothub run -s <lesson-uuid>
`

// Version is set by main (which may itself be set with -ldflags) and falls back
// to the module version recorded by `go install module@version`.
var Version = "dev"

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	var err error
	switch os.Args[1] {
	case "setup", "init":
		err = cmdSetup(os.Args[2:])
	case "submit":
		err = cmdProxy("submit", os.Args[2:])
	case "run":
		err = cmdProxy("run", os.Args[2:])
	case "sync":
		err = cmdSync(os.Args[2:])
	case "backfill":
		err = cmdBackfill(os.Args[2:])
	case "repair":
		err = cmdRepair(os.Args[2:])
	case "upgrade", "update":
		err = cmdUpgrade(os.Args[2:])
	case "config", "status":
		err = cmdConfig(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("boothub " + buildVersion())
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		return 2
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

// buildVersion prefers an ldflags value, then the module version recorded by
// `go install module@version`.
func buildVersion() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}
