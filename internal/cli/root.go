package cli

import (
	"github.com/spf13/cobra"
)

var (
	version  = "dev"
	dryRun   bool
	noVerify bool
	yes      bool
)

func SetVersion(v string) { version = v }

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ghpm",
		Short: "GitHub Package Manager — install binaries from GitHub Releases",
		SilenceUsage: true,
	}

	root.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Print what would be done without executing")
	root.PersistentFlags().BoolVar(&noVerify, "no-verify", false, "Skip SHA256 verification")
	root.PersistentFlags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	root.Flags().Bool("version", false, "Print ghpm version")
	root.RunE = func(cmd *cobra.Command, args []string) error {
		v, _ := cmd.Flags().GetBool("version")
		if v {
			cmd.Printf("ghpm %s\n", version)
			return nil
		}
		return cmd.Help()
	}

	root.AddCommand(
		newInstallCmd(),
		newListCmd(),
		newSearchCmd(),
		newInfoCmd(),
		newDownloadCmd(),
		newOutdatedCmd(),
		newUpdateCmd(),
		newUninstallCmd(),
		newCleanCmd(),
		newUpgradeCmd(),
		newDoctorCmd(),
	)

	return root
}
