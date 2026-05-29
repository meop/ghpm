package cli

import (
	"slices"
	"strings"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/gh"
)

var noVerify bool

type cmdInit struct {
	cfg      *config.Settings
	manifest *config.Manifest
	repos    map[string]string
	unlock   func()
}

type cmdOptions struct {
	Lock     bool
	Manifest bool
	GH       bool
	Dirs     bool
	Repos    bool
	NoVerify bool
}

func initCommand(opts cmdOptions) (*cmdInit, error) {
	ci := &cmdInit{}

	if opts.Lock {
		unlock, err := config.AcquireLock()
		if err != nil {
			printFail(nil, "%v", err)
			return nil, errSilent
		}
		ci.unlock = unlock
	}

	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return nil, ci.fail()
	}
	ci.cfg = cfg
	if opts.NoVerify && cfg.NoVerify {
		noVerify = true
	}

	if opts.Manifest {
		manifest, err := config.LoadManifest()
		if err != nil {
			printFail(cfg, "could not load manifest: %v", err)
			return nil, ci.fail()
		}
		ci.manifest = manifest
	}

	if opts.Dirs {
		if err := config.EnsureDirs(); err != nil {
			printFail(cfg, "%v", err)
			return nil, ci.fail()
		}
	}

	if opts.GH {
		if err := gh.CheckInstalled(); err != nil {
			printFail(cfg, "%v", err)
			return nil, ci.fail()
		}
	}

	if opts.Repos {
		repos, repoErr := config.LoadRepos()
		if repoErr != nil {
			printInfo(cfg, "could not load repos: %v", repoErr)
		}
		ci.repos = repos
	}

	return ci, nil
}

func (ci *cmdInit) fail() error {
	if ci.unlock != nil {
		ci.unlock()
	}
	return errSilent
}

func (ci *cmdInit) close() {
	if ci.unlock != nil {
		ci.unlock()
	}
}

func pkgType(p config.PackageEntry) string {
	if len(p.AllFonts()) > 0 {
		return "font"
	}
	return "bin"
}

func pkgAsset(p config.PackageEntry) string {
	if assetName := p.BinAssetName(); assetName != "" {
		return assetName
	}
	names := make([]string, 0, len(p.Asset))
	for assetName := range p.Asset {
		names = append(names, assetName)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
