package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [shell]",
		Short: "Output shell snippet to add ~/.ghpm/bin to PATH (for eval in shell config)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runInit,
	}
}

func runInit(cmd *cobra.Command, args []string) error {
	shell := ""
	if len(args) > 0 {
		shell = args[0]
	}
	fmt.Print(pathSnippet(shell))
	return nil
}

func pathSnippet(shell string) string {
	switch strings.ToLower(shell) {
	case "nu", "nushell":
		return nuSnippet()
	case "pwsh", "powershell":
		return ps1Snippet()
	default:
		return shSnippet()
	}
}

func shSnippet() string {
	return `case ":${PATH}:" in
  *":$HOME/.ghpm/bin:"*) ;;
  *) export PATH="$HOME/.ghpm/bin${PATH:+:$PATH}" ;;
esac
`
}

func nuSnippet() string {
	return "$env.PATH = ($env.PATH | prepend ($env.HOME + \"/.ghpm/bin\") | uniq)\n"
}

func ps1Snippet() string {
	return `if (-not ($env:PATH -split [IO.Path]::PathSeparator -contains "${env:HOME}/.ghpm/bin")) {
  $env:PATH = "${env:HOME}/.ghpm/bin" + [IO.Path]::PathSeparator + $env:PATH
}
`
}
