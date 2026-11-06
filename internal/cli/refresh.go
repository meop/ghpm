package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ghbin"
)

func newRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "refresh",
		Aliases: []string{"rf", "ref"},
		Short:   "Refresh repo sources, and the gh and wedge ghpm uses, to their latest",
		Args:    cobra.NoArgs,
		RunE:    runRefresh,
	}
}

func runRefresh(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(context.Background(), cmdOptions{Lock: true, GH: true})
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
	sep()
	ctx := context.Background()
	for _, tool := range []struct {
		name    string
		refresh func(context.Context) (string, string, error)
	}{
		{"gh", func(ctx context.Context) (string, string, error) { return ghbin.Refresh(ctx, fetchingVendored("gh")) }},
		{"wedge", refreshWedge},
	} {
		from, to, err := tool.refresh(ctx)
		switch {
		case err != nil:
			printFail(cfg, "%s %v", tool.name, err)
			hadErrors = true
		case from == to:
			printPass(cfg, "%s %s is the latest", tool.name, to)
		case from == "":
			printPass(cfg, "installed %s %s", tool.name, to)
		default:
			printPass(cfg, "updated %s %s → %s", tool.name, from, to)
			if tool.name == "wedge" {
				print("shims keep the wedge they were stamped with: `ghpm sync --reinstall` restamps them")
			}
		}
	}
	if hadErrors {
		return errSilent
	}
	return nil
}
