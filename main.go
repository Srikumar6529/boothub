package main

import (
	"os"

	"github.com/Srikumar6529/boothub/internal/cli"
)

// version is set at build time:
//
//	go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

func main() {
	if version != "" {
		cli.Version = version
	}
	os.Exit(cli.Execute())
}
