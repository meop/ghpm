package main

import (
	"os"

	"github.com/meop/ghpm/internal/cli"
)

var version = "dev"

func main() {
	cli.SetVersion(version)
	root := cli.NewRootCmd()
	cmd, err := root.ExecuteC()
	if err != nil {
		if err.Error() != "" {
			_ = cmd.Help()
		}
		os.Exit(1)
	}
}
