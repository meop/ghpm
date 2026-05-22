package cli

import (
	"runtime"

	"github.com/spf13/cobra"
)

const (
	binGhpm   = "ghpm"
	binGh     = "gh"
	binSheesh = "sheesh"
)

var (
	version   = "dev"
	dryRun    bool
	noVerify  bool
	onlyNames bool
	yes       bool
)

func SetVersion(v string) { version = v }

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ghpm [flags] [command]",
		Short:         "GitHub Package Manager — Extract Releases / Tags / Assets",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return nil
		},
	}

	root.SetUsageTemplate(`Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if and (not .Runnable) .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`)

	root.Version = version
	root.SetVersionTemplate("{{.Version}}\n")
	root.Flags().Bool("version", false, "Print ghpm version")

	root.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "Print execution only without running")
	root.PersistentFlags().BoolVarP(&yes, "yes", "y", false, "Yes for confirmation prompts")

	root.SetHelpCommand(&cobra.Command{Hidden: true})

	root.AddCommand(
		newTidyCmd(),
		newDoctorCmd(),
		newDownloadCmd(),
		newFindCmd(),
		newInfoCmd(),
		newAddCmd(),
		newListCmd(),
		newRefreshCmd(),
		newOutdatedCmd(),
		newSyncCmd(),
		newRemoveCmd(),
		newUpgradeCmd(),
	)

	return root
}
