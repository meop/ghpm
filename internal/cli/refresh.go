package cli

import (
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
	ci, err := initCommand(cmdOptions{Lock: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg

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
