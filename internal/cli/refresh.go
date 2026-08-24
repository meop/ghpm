package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
)

func newRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "refresh",
		Aliases: []string{"rf", "ref"},
		Short:   "Refresh repo sources to latest versions",
		Args:    cobra.NoArgs,
		RunE:    runRefresh,
	}
}

func runRefresh(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(context.Background(), cmdOptions{Lock: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg

	syncResults, err := config.RefreshRepos()
	if syncResults == nil && err != nil {
		// RefreshRepos returns (nil, err) only when it can't even load settings
		// to find the configured sources — nothing to range over below, so
		// without this check the command would silently exit 0 with no output.
		// In every other case each source's own result carries its own error,
		// which the loop below already surfaces per-source; this top-level err
		// is otherwise redundant with those.
		printFail(cfg, "%v", err)
		return errSilent
	}
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
