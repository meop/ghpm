package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ghbin"
	"github.com/meop/ghpm/internal/store"
	"github.com/meop/golie/doctor"
	"github.com/meop/golie/ui"
	"github.com/meop/golie/vendored"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "doctor",
		Aliases: []string{"doc", "check"},
		Short:   "Check system health and configuration",
		Args:    cobra.NoArgs,
		RunE:    runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
	// doctor is a diagnostic command: it must inspect configuration even when
	// things are broken, so it loads settings directly instead of going through
	// initCommand, which would fail fast and acquire locks.
	cfg, err := config.LoadSettings()
	if err != nil {
		printFail(nil, "could not load settings: %v", err)
		return errSilent
	}

	ui.SetColorResolver(func(role string) func(string) string { return colorfn(cfg, role) })
	pass, warn, fail := doctor.Pass, doctor.Warn, doctor.Fail
	doctor.Start("ghpm")

	if err := config.CheckLock(); err != nil {
		warn("lock", err.Error())
	} else {
		pass("lock", "no concurrent process detected")
	}

	ghPath, ghErr := exec.LookPath("gh")
	if ghErr != nil {
		fail("gh", "not found")
	} else {
		pass("gh", "present — "+ghPath)
		out, authErr := exec.Command("gh", "auth", "status").CombinedOutput()
		if authErr == nil {
			pass("gh", "authenticated")
		} else {
			fail("gh", authStatusFailureDetail(out))
		}
	}

	// ghpm never runs the system gh above — only its own vendored copy (see
	// ghbin.Find) — so that copy's presence and auth are what actually
	// determines whether commands needing gh will work.
	if dir, r, err := ghbin.Release(); err != nil {
		fail(vendored.Label("gh"), err.Error())
	} else {
		doctor.Vendored("gh", r, dir, "ghpm refresh")
		if r.Installed(dir) != "" {
			vendoredGh, _ := ghbin.VendorPath()
			cfgDir, cfgErr := ghbin.ConfigDir()
			if cfgErr != nil {
				fail(vendored.Label("gh"), cfgErr.Error())
			} else {
				cmd := exec.Command(vendoredGh, "auth", "status")
				cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+cfgDir)
				out, authErr := cmd.CombinedOutput()
				if authErr == nil {
					pass(vendored.Label("gh"), "authenticated")
				} else {
					fail(vendored.Label("gh"), authStatusFailureDetail(out))
				}
			}
		}
	}
	if dir, r, err := wedgeRelease(); err != nil {
		fail(vendored.Label("wedge"), err.Error())
	} else {
		doctor.Vendored("wedge", r, dir, "ghpm refresh")
	}

	ghpmDir, err := store.Dir()
	if err != nil {
		printFail(nil, "could not determine ghpm directory: %v", err)
		return errSilent
	}

	manifestPath := filepath.Join(ghpmDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		warn("manifest.json", "not present — "+manifestPath)
	} else if err != nil {
		fail("manifest.json", err.Error())
	} else {
		var v any
		if jsonErr := json.Unmarshal(data, &v); jsonErr != nil {
			fail("manifest.json", "invalid JSON")
		} else {
			pass("manifest.json", "present — "+manifestPath)
		}
	}

	configPath := filepath.Join(ghpmDir, "config.toml")
	data, err = os.ReadFile(configPath)
	if os.IsNotExist(err) {
		warn("config.toml", "not present (using defaults) — "+configPath)
	} else if err != nil {
		fail("config.toml", err.Error())
	} else {
		var v map[string]any
		if tomlErr := toml.Unmarshal(data, &v); tomlErr != nil {
			fail("config.toml", "invalid TOML")
		} else {
			pass("config.toml", "present — "+configPath)
		}
	}

	for _, source := range cfg.Sources() {
		if v, file, err := config.RepoVersion(source); err != nil {
			fail(config.RepoLabel(source), err.Error())
		} else if v == "" {
			warn(config.RepoLabel(source), "not present — `ghpm refresh` installs it")
		} else {
			pass(config.RepoLabel(source), doctor.Present(v, file))
		}
	}

	repos, repoErr := config.LoadRepos()
	if repoErr != nil {
		fail("repos", repoErr.Error())
	} else {
		pass("repos", fmt.Sprintf("%d mappings", len(repos)))
	}

	manifest, manifestErr := config.LoadManifest()
	pkgsDir, pkgsErr := store.ExtractsDir()
	binDir, binDirErr := store.BinDir()

	if manifestErr == nil && manifest != nil && pkgsErr == nil {
		var staleExtracts, missingShims, missingFonts []string
		for key, pkg := range manifest.Extracts {
			if _, serr := os.Stat(filepath.Join(pkgsDir, key)); os.IsNotExist(serr) {
				staleExtracts = append(staleExtracts, key)
			}
			if binDirErr == nil {
				for shimName := range pkg.AllBins() {
					if _, serr := os.Lstat(filepath.Join(binDir, shimName)); os.IsNotExist(serr) {
						missingShims = append(missingShims, shimName)
					}
				}
			}
		}
		if fontsDir, ferr := userFontDir(); ferr == nil {
			for key, pkg := range manifest.Extracts {
				for fontName, fontPath := range pkg.AllFonts() {
					if !fontInstalled(fontPath, fontsDir) {
						missingFonts = append(missingFonts, key+"/"+fontName)
					}
				}
			}
		}
		if len(staleExtracts) == 0 && len(missingShims) == 0 && len(missingFonts) == 0 {
			pass("installed packages", fmt.Sprintf("%d installed", len(manifest.Extracts)))
		} else {
			for _, key := range staleExtracts {
				warn("installed packages", key+" — extract dir missing")
			}
			for _, shimName := range missingShims {
				warn("installed packages", shimName+" — shim missing")
			}
			for _, name := range missingFonts {
				warn("installed packages", name+" — font missing")
			}
		}
	}

	if binDirErr == nil {
		if slices.Contains(filepath.SplitList(os.Getenv("PATH")), binDir) {
			pass("PATH", binDir+" is on PATH")
		} else {
			fail("PATH", binDir+" is not on PATH — shims won't be found")
		}
	}

	if releaseDir, err := store.ReleaseBaseDir(); err == nil {
		pass("cache", doctor.Size(releaseDir)+" — "+releaseDir)
	}

	return nil
}

func authStatusFailureDetail(out []byte) string {
	if lines := strings.Split(string(out), "\n"); len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
		return strings.TrimSpace(lines[0])
	}
	return "not authenticated"
}
