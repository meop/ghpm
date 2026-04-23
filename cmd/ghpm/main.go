package main

import (
	"os"

	"github.com/meop/ghpm/internal/cli"
)

var version = "dev"

func main() {
	cli.SetVersion(version)
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
