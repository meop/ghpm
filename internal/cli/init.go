package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/entrypoint"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "generate shell entrypoint scripts",
		Args:  cobra.NoArgs,
		RunE:  runInit,
	}
	cmd.Flags().String("shell", "", "force generation for a specific shell (sh, nu, pwsh)")
	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return errSilent
	}
	manifest, err := config.LoadManifest()
	if err != nil {
		printFail(cfg, "could not load manifest: %v", err)
		return errSilent
	}
	if err := config.EnsureDirs(); err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	forceShell, _ := cmd.Flags().GetString("shell")
	if forceShell != "" {
		generated, err := entrypoint.GenerateFor(forceShell, manifest)
		if err != nil {
			printFail(cfg, "%v", err)
			return errSilent
		}
		for _, p := range generated {
			printPass(cfg, "generated %s", p)
		}
		printShellInstructions(cfg, forceShell)
		return nil
	}

	generated, err := entrypoint.Generate(manifest)
	if err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}

	if len(generated) == 0 {
		printInfo(cfg, "no supported shells detected in PATH")
		return nil
	}

	for _, p := range generated {
		printPass(cfg, "generated %s", p)
	}

	shells := entrypoint.DetectShells()
	if shells.POSIX {
		printShellInstructions(cfg, "sh")
	}
	if shells.Nu {
		printShellInstructions(cfg, "nu")
	}
	if shells.PWSh {
		printShellInstructions(cfg, "pwsh")
	}

	return nil
}

func printShellInstructions(cfg *config.Settings, shell string) {
	home, _ := os.UserHomeDir()
	switch strings.ToLower(shell) {
	case "sh", "bash", "zsh":
		fmt.Println()
		printInfo(cfg, "add to your shell config:")
		fmt.Printf("  echo 'source %s/.ghpm/entrypoint.sh' >> ~/.bashrc\n", home)
		fmt.Printf("  echo 'source %s/.ghpm/entrypoint.sh' >> ~/.zshrc\n", home)
	case "nu", "nushell":
		fmt.Println()
		printInfo(cfg, "add to your nushell config:")
		fmt.Printf("  echo 'source %s/.ghpm/entrypoint.nu' >> $\"($env.XDG_CONFIG_HOME | default ($env.HOME | path join '.config'))/nushell/config.nu\"\n", home)
	case "pwsh", "powershell":
		fmt.Println()
		printInfo(cfg, "add to your PowerShell profile:")
		fmt.Printf("  Add-Content $PROFILE '. %s\\.ghpm\\entrypoint.ps1'\n", filepath.FromSlash(home))
	}
}
