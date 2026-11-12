package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/meop/golie/vendored"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ghapi"
)

func newPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "pull",
		Aliases: []string{"p", "refresh", "rf"},
		Short:   "Pull repo sources, and the wedge ghpm uses, to their latest",
		Args:    cobra.NoArgs,
		RunE:    runPull,
	}
}

func runPull(cmd *cobra.Command, args []string) error {
	ci, err := initCommand(context.Background(), cmdOptions{Lock: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg

	var hadErrors bool
	ctx := context.Background()
	for _, tool := range []struct {
		name    string
		refresh func(context.Context) (string, string, error)
	}{
		{"wedge", refreshWedge},
	} {
		from, to, err := tool.refresh(ctx)
		if err != nil {
			printFail(cfg, "%s: %v", vendored.Label(tool.name), err)
			hadErrors = true
			continue
		}
		printPass(cfg, "%s", vendored.Said(vendored.Label(tool.name), from, to))
		if tool.name == "wedge" && from != "" && from != to {
			print("%s: shims keep the wedge that stamped them until [ghpm renew]", vendored.Label("wedge"))
		}
	}

	if err := ghapi.EnsureToken(); err != nil {
		printFail(cfg, "%v", err)
		return errSilent
	}
	syncResults, err := config.RefreshRepos(func(source, path string) { fetching(config.RepoLabel(source))(path) })
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
	for _, r := range syncResults {
		if r.Err != nil {
			printFail(cfg, "%s: %v", config.RepoLabel(r.Source), r.Err)
			hadErrors = true
		} else {
			printPass(cfg, "%s", vendored.Said(config.RepoLabel(r.Source), r.From, r.To))
		}
	}
	if hadErrors {
		return errSilent
	}
	return nil
}
