package cli

import (
	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
)

func newRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Download latest repo sources",
		Args:  cobra.NoArgs,
		RunE:  runRefresh,
	}
}

func runRefresh(cmd *cobra.Command, args []string) error {
	unlock, err := config.AcquireLock()
	if err != nil {
		printFail(nil, "%v", err)
		return errSilent
	}
	defer unlock()

	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return errSilent
	}

	syncResults, _ := config.RefreshRepos()
	var hadErrors bool
	for _, r := range syncResults {
		if r.Err != nil {
			printFail(cfg, "%s %v", r.Source, r.Err)
			hadErrors = true
		} else {
			printPass(cfg, "synced %s (%d entries)", r.Source, r.Count)
		}
	}
	if hadErrors {
		return errSilent
	}
	return nil
}
