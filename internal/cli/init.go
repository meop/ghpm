package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/entrypoint"
	"github.com/meop/ghpm/internal/store"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "generate shell env scripts",
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
		printShellHints(cfg)
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

	printShellHints(cfg)
	return nil
}

func printShellHints(cfg *config.Settings) {
	scriptsDir, _ := store.ScriptsDir()
	shells := entrypoint.DetectShells()

	fmt.Println()
	printInfo(cfg, "add to your shell config:")

	if shells.POSIX {
		fmt.Printf("  zsh/bash:  echo 'source %s/env.sh' >> ~/.zshrc\n", scriptsDir)
	}
	if shells.Nu {
		fmt.Printf("  nushell:   source '%s/env.nu' from your config.nu\n", scriptsDir)
	}
	if shells.PWSh {
		fmt.Printf("  pwsh:      Add-Content $PROFILE '. %s\\env.ps1'\n", filepath.FromSlash(scriptsDir))
	}
}
