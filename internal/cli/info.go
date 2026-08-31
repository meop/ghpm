package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
	"github.com/meop/ghpm/internal/parallel"
)

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "info <name> [name...]",
		Aliases: []string{"i", "show"},
		Short:   "Show releases and available assets for packages",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runInfo,
	}
}

// infoJob is one arg that resolved locally and needs a network fetch.
type infoJob struct {
	idx         int
	ver         string
	owner, repo string
}

// infoFetch is the network-phase result for one job.
type infoFetch struct {
	singleRel gh.Release
	hasSingle bool
	releases  []gh.Release
	latestRel gh.Release
	hasLatest bool
}

// infoOutcome accumulates everything needed to print one arg's block, whether
// it failed locally, failed to fetch, or succeeded.
type infoOutcome struct {
	arg      string
	pkgName  string
	source   string
	localErr error
	fetch    infoFetch
	fetchErr error
}

func runInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	ci, err := initCommand(ctx, cmdOptions{Manifest: true, GH: true, Repos: true})
	if err != nil {
		return err
	}
	cfg := ci.cfg
	manifest := ci.manifest
	repos := ci.repos
	ghClient := ci.gh

	// Phase 1: local resolution only (no network) — errors here are recorded,
	// not printed yet, so per-arg output still lands in original argument order
	// once the parallel fetch phase (below) finishes out of order.
	outcomes := make([]infoOutcome, len(args))
	var jobs []infoJob
	for i, arg := range args {
		pkgName, ver, _ := config.ParseVersionSuffix(arg)
		outcomes[i] = infoOutcome{arg: arg, pkgName: pkgName}
		if err := config.ValidateName(pkgName); err != nil {
			outcomes[i].localErr = err
			continue
		}
		source, err := config.ResolveSource(pkgName, ver, manifest, repos)
		if err != nil {
			outcomes[i].localErr = err
			continue
		}
		owner, repo, err := gh.SplitSource(source)
		if err != nil {
			outcomes[i].localErr = err
			continue
		}
		outcomes[i].source = source
		jobs = append(jobs, infoJob{idx: i, ver: ver, owner: owner, repo: repo})
	}

	// Phase 2: fetch every resolved arg's release info through one bounded
	// pool, instead of one network round trip per argument in sequence.
	tasks := make([]parallel.Task[infoFetch], len(jobs))
	for i, job := range jobs {
		tasks[i] = parallel.Task[infoFetch]{
			Name: outcomes[job.idx].pkgName,
			Run: func() (infoFetch, error) {
				if job.ver != "" {
					rel, err := ghClient.GetReleaseByTag(ctx, job.owner, job.repo, job.ver)
					if err != nil {
						return infoFetch{}, err
					}
					return infoFetch{singleRel: rel, hasSingle: true}, nil
				}
				releases, err := ghClient.ListReleases(ctx, job.owner, job.repo)
				if err != nil {
					return infoFetch{}, err
				}
				fetch := infoFetch{releases: releases}
				if len(releases) > 0 {
					if rel, err := ghClient.GetLatestRelease(ctx, job.owner, job.repo); err == nil {
						fetch.latestRel = rel
						fetch.hasLatest = true
					}
				}
				return fetch, nil
			},
		}
	}
	for i, res := range parallel.Run(ctx, tasks, cfg.NumParallel) {
		idx := jobs[i].idx
		outcomes[idx].fetch = res.Value
		outcomes[idx].fetchErr = res.Err
	}

	// Phase 3: print everything in original argument order.
	var hadErrors bool
	for _, o := range outcomes {
		sep()
		print("info: %s", o.pkgName)
		if o.localErr != nil {
			printFail(cfg, "%s: %v", o.arg, o.localErr)
			hadErrors = true
			continue
		}

		sep()
		print("%s (%s)", o.arg, o.source)
		if descr := repos[o.pkgName].Descr; descr != "" {
			print("%s", descr)
		}
		print("%s", strings.Repeat("─", 60))

		if o.fetchErr != nil {
			printFail(cfg, "%v", o.fetchErr)
			hadErrors = true
			continue
		}

		if o.fetch.hasSingle {
			printReleaseInfo(o.fetch.singleRel)
			continue
		}
		limit := min(len(o.fetch.releases), 10)
		print("  recent releases (%d shown):", limit)
		for _, r := range o.fetch.releases[:limit] {
			print("    %s", config.NormalizeVersion(r.TagName))
		}
		if o.fetch.hasLatest {
			sep()
			printReleaseInfo(o.fetch.latestRel)
		}
	}
	if hadErrors {
		return errSilent
	}
	return nil
}

func printReleaseInfo(rel gh.Release) {
	print("  tag: %s", config.NormalizeVersion(rel.TagName))
	print("  assets:")
	for _, a := range rel.Assets {
		print("    %-60s %d bytes", a.Name, a.Size)
	}
}
