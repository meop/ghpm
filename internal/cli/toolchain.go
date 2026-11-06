package cli

import (
	"context"

	"github.com/meop/golie/vendored"
	"github.com/meop/golie/wedge"

	"github.com/meop/ghpm/internal/store"
)

// syncWedge makes sure ghpm has its vendored mallet and shim templates: what
// is there is used, with no network, and only where there are none is wedge's
// latest fetched. It is the counterpart to ghbin.Ensure; `refresh` is what
// moves either to a newer release.
func syncWedge(ctx context.Context) error {
	shimDir, r, err := wedgeRelease()
	if err != nil {
		return err
	}
	// A dry run has nothing to preview here: the vendored toolchain isn't the
	// change the user asked about, and fetching it anyway would write to disk.
	if dryRun && r.Installed(shimDir) == "" {
		return nil
	}
	_, err = r.Ensure(ctx, shimDir, vendored.Options{Fetching: fetchingVendored("wedge")})
	return err
}

// refreshWedge moves the vendored wedge to its latest release. See
// vendored.Release.Refresh for what it returns.
func refreshWedge(ctx context.Context) (from, to string, err error) {
	shimDir, r, err := wedgeRelease()
	if err != nil {
		return "", "", err
	}
	return r.Refresh(ctx, shimDir, vendored.Options{Fetching: fetchingVendored("wedge")})
}

func wedgeRelease() (string, vendored.Release, error) {
	shimDir, err := store.ShimDir()
	if err != nil {
		return "", vendored.Release{}, err
	}
	r, err := wedge.Here()
	return shimDir, r, err
}

// fetchingVendored says a vendored tool's archive is being fetched, as any
// other asset fetch is said
func fetchingVendored(name string) func(archive string) {
	return func(archive string) { print("%s: asset fetching [%s]...", name, archive) }
}
