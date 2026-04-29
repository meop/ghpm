package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check system health and configuration",
		Args:  cobra.NoArgs,
		RunE:  runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
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
	fmt.Println(repeatStr("─", 50))

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

	ghpmDir := mustGhpmDir()

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

	manifest, manifestErr := config.LoadManifest()
	pkgsDir, pkgsErr := store.ExtractsDir()
	if manifestErr == nil && manifest != nil && pkgsErr == nil {
		var stale []string
		for key := range manifest.Extracts {
			if _, serr := os.Stat(filepath.Join(pkgsDir, key)); os.IsNotExist(serr) {
				stale = append(stale, key)
			}
		}
		if len(stale) == 0 {
			pass("installed packages", "present")
		} else {
			for _, key := range stale {
				warn("installed packages", key+" — not present")
			}
		}
	}

	if releaseDir, err := store.ReleaseBaseDir(); err == nil {
		pass("cache", humanBytes(dirSize(releaseDir))+" — "+releaseDir)
	}

	return nil
}

func mustGhpmDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ghpm")
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
