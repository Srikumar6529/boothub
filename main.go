package main

import (
	"fmt"
	"os"
)

// version is set at build time:
//
//	go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

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

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
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
		fmt.Println("boothub " + version)
		return
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
