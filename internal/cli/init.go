package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [shell]",
		Short: "Output shell hook for eval (sources path script + defines ghpm wrapper with reload)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runInit,
	}
}

func runInit(cmd *cobra.Command, args []string) error {
	if _, err := config.LoadSettings(); err != nil {
		printFail(nil, "could not load settings: %v", err)
		return errSilent
	}

	shell := ""
	if len(args) > 0 {
		shell = args[0]
	}
	fmt.Print(envHook(shell))
	return nil
}

func envHook(shell string) string {
	switch strings.ToLower(shell) {
	case "nu", "nushell":
		return nuHook()
	case "pwsh", "powershell":
		return ps1Hook()
	default:
		return shHook()
	}
}

func shHook() string {
	return `ghpm() {
  case "$1" in
    reload) [ -f "$HOME/.ghpm/scripts/paths.sh" ] && source "$HOME/.ghpm/scripts/paths.sh" ;;
    *) command ghpm "$@" ;;
  esac
}
ghpm reload
`
}

func nuHook() string {
	return `def --env --wrapped ghpm [...args] {
  if ($args | length) > 0 and $args.0 == "reload" {
    if ("~/.ghpm/scripts/paths.nu" | path expand | path exists) { source-env ("~/.ghpm/scripts/paths.nu" | path expand) }
  } else {
    ^ghpm ...$args
  }
}
ghpm reload
`
}

func ps1Hook() string {
	return `function ghpm {
  if ($args.Count -gt 0 -and $args[0] -eq "reload") {
    if (Test-Path "${env:HOME}/.ghpm/scripts/paths.ps1") { . "${env:HOME}/.ghpm/scripts/paths.ps1" }
  } else {
    & (Get-Command ghpm -CommandType Application).Source @args
  }
}
ghpm reload
`
}
