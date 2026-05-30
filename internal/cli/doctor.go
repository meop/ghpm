package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
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

	passLabel := "PASS"
	failLabel := "FAIL"
	warnLabel := "WARN"
	if fn := colorfn(cfg, "pass"); fn != nil {
		passLabel = fn("PASS")
	}
	if fn := colorfn(cfg, "fail"); fn != nil {
		failLabel = fn("FAIL")
	}
	if fn := colorfn(cfg, "warn"); fn != nil {
		warnLabel = fn("WARN")
	}

	line := func(status, label, detail string) {
		if detail != "" {
			fmt.Printf("  [%s] %s — %s\n", status, label, detail)
		} else {
			fmt.Printf("  [%s] %s\n", status, label)
		}
	}
	pass := func(label, detail string) { line(passLabel, label, detail) }
	fail := func(label, detail string) { line(failLabel, label, detail) }
	warn := func(label, detail string) { line(warnLabel, label, detail) }

	fmt.Println("ghpm doctor")
	fmt.Println(strings.Repeat("─", 50))

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
			msg := "not authenticated"
			if lines := strings.Split(string(out), "\n"); len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
				msg = strings.TrimSpace(lines[0])
			}
			fail("gh", msg)
		}
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

	settingsPath := filepath.Join(ghpmDir, "settings.json")
	data, err = os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		warn("settings.json", "not present (using defaults) — "+settingsPath)
	} else if err != nil {
		fail("settings.json", err.Error())
	} else {
		var v any
		if jsonErr := json.Unmarshal(data, &v); jsonErr != nil {
			fail("settings.json", "invalid JSON")
		} else {
			pass("settings.json", "present — "+settingsPath)
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
			if _, serr := os.Stat(filepath.Join(pkgsDir, key, pkg.Version)); os.IsNotExist(serr) {
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

	if shimDir, err := store.ShimDir(); err == nil {
		kebabPath := filepath.Join(shimDir, exeName("kebab"))
		if _, err := os.Stat(kebabPath); err != nil {
			warn("kebab", "not found — run 'ghpm upgrade' to install sheesh")
		} else {
			pass("kebab", "present — "+kebabPath)
		}
	}

	if releaseDir, err := store.ReleaseBaseDir(); err == nil {
		pass("cache", humanBytes(dirSize(releaseDir))+" — "+releaseDir)
	}

	return nil
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
