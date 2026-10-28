package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/golie/upgrade"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
)

// self is ghpm as its own release page publishes it. Everything upgrade asks
// of GitHub is a plain page or release download — not the API and not gh —
// so ghpm can upgrade itself before, or without, its vendored gh being
// authenticated. See golie's upgrade package.
var self = upgrade.Self{Name: binGhpm, ReleasesURL: "https://github.com/meop/ghpm/releases/"}

// installedSelf is self held to the one place ghpm upgrades itself from:
// ~/.ghpm/bin, where its installer puts it. A ghpm that runs from anywhere
// else — a package manager's, go install's, a build — is upgraded by what put
// it there.
func installedSelf() (upgrade.Self, error) {
	binDir, err := store.BinDir()
	if err != nil {
		return upgrade.Self{}, err
	}
	s := self
	s.Dir = binDir
	return s, nil
}

func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "upgrade",
		Aliases: []string{"ug", "upg"},
		Short:   "Upgrade ghpm to its latest release",
		Args:    cobra.NoArgs,
		RunE:    runUpgrade,
	}
}

// runUpgrade upgrades ghpm and nothing else. The tools ghpm vendors for its
// own use (gh, wedge's mallet) are deliberately not components here: they are
// pinned and synced to that pin at runtime, so "upgrade" means exactly what a
// user reading it expects — upgrade ghpm — rather than asking about internals
// they never chose to install. The release's archive is always checked
// against the release's own SHA256SUMS before the running binary is replaced.
func runUpgrade(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	ci, err := initCommand(ctx, cmdOptions{Lock: true})
	if err != nil {
		return err
	}
	defer ci.close()
	cfg := ci.cfg

	failed := func(err error) error {
		printFail(cfg, "%s: %v", binGhpm, err)
		printFailedTable(cfg, "component", []failedItem{{name: binGhpm, reason: err.Error()}})
		return errSilent
	}

	// asked first, and of nothing but the disk: a ghpm that will not be
	// replaced has no use for which release is newest
	self, err := installedSelf()
	if err != nil {
		return failed(err)
	}
	if err := self.Check(); err != nil {
		if elsewhere, ok := errors.AsType[*upgrade.NotSelfInstalledError](err); ok {
			printFail(cfg, msgCannotUpgradeItself, binGhpm, elsewhere.Running)
			return errSilent
		}
		return failed(err)
	}

	tag, err := self.LatestTag(ctx)
	if err != nil {
		return failed(err)
	}
	if strings.TrimPrefix(tag, "v") == strings.TrimPrefix(version, "v") {
		print(msgGhpmUpToDate)
		return nil
	}
	latest := config.NormalizeVersion(tag)

	if !gate(
		[]string{"name", "version", "update"},
		[][]string{{binGhpm, version, latest}},
		[]func(string) string{nil, colorfn(cfg, "old"), colorfn(cfg, "new")},
		fmt.Sprintf("upgrade %s to %s", binGhpm, latest),
	) {
		return nil
	}

	fetching := func(asset string) { print("%s: asset fetching [%s]...", binGhpm, asset) }
	if err := self.Install(ctx, tag, fetching); err != nil {
		return failed(err)
	}
	printPass(cfg, "upgraded %s to %s", binGhpm, latest)
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
