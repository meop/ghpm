package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/meop/ghpm/internal/asset"
	"github.com/meop/ghpm/internal/config"
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
	quiet     bool
	yes       bool
)

func SetVersion(v string) { version = v }

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func binShimName(key, binName string) string {
	_, ver, pinned := config.ParseVersionSuffix(key)
	if !pinned {
		return binName
	}
	if strings.HasSuffix(binName, ".exe") {
		return binName[:len(binName)-4] + "@" + ver + ".exe"
	}
	return binName + "@" + ver
}

// hasReservedConflict reports whether any of the proposed shim names is already
// claimed by another package (present in reserved).
func hasReservedConflict(proposed []string, reserved map[string]string) bool {
	for _, name := range proposed {
		if _, ok := reserved[name]; ok {
			return true
		}
	}
	return false
}

// splitBinKey recovers BinDir and BinName from a Bins map value (relative binary path).
func splitBinKey(key string) (binDir, binName string) {
	i := strings.LastIndex(key, "/")
	if i < 0 {
		return "", key
	}
	return key[:i], key[i+1:]
}

// proposedShimNames returns the default shim name for each selected binary.
// When multiple binaries share the same filename, a disambiguating suffix is
// appended from the last segment of their BinDir (or a numeric index as fallback).
func proposedShimNames(manifestKey string, selected []asset.BinaryCandidate) []string {
	counts := make(map[string]int)
	for _, s := range selected {
		counts[s.BinName]++
	}
	result := make([]string, len(selected))
	for i, s := range selected {
		base := binShimName(manifestKey, s.BinName)
		if counts[s.BinName] > 1 {
			suffix := lastPathSegment(s.BinDir)
			if suffix == "" {
				suffix = fmt.Sprintf("%d", i+1)
			}
			base = shimWithSuffix(base, suffix)
		}
		result[i] = base
	}
	return result
}

func lastPathSegment(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return p
	}
	return p[i+1:]
}

func shimWithSuffix(name, suffix string) string {
	if strings.HasSuffix(name, ".exe") {
		return name[:len(name)-4] + "-" + suffix + ".exe"
	}
	return name + "-" + suffix
}

// needsShimRenamePrompt reports whether any binary's default shim name differs
// from the package name, indicating the user might want to rename it.
func needsShimRenamePrompt(pkgName string, selected []asset.BinaryCandidate) bool {
	for _, s := range selected {
		if s.BinName != pkgName {
			return true
		}
	}
	return false
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
	root.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress non-error output")
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
