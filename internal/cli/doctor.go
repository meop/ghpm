package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/entrypoint"
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

	check := func(label string, ok bool, msg string) {
		status := passLabel
		if !ok {
			status = failLabel
		}
		fmt.Printf("  [%s] %s", status, label)
		if msg != "" {
			fmt.Printf(" — %s", msg)
		}
		fmt.Println()
	}

	fmt.Println("ghpm doctor")
	fmt.Println(repeatStr("─", 50))

	ghPath, err := exec.LookPath("gh")
	check("gh installed", err == nil, ghPath)

	if err == nil {
		out, authErr := exec.Command("gh", "auth", "status").CombinedOutput()
		authed := authErr == nil
		msg := ""
		if !authed {
			lines := strings.Split(string(out), "\n")
			if len(lines) > 0 {
				msg = strings.TrimSpace(lines[0])
			}
		}
		check("gh authenticated", authed, msg)
	}

	ghpmDir := mustGhpmDir()
	scriptsDir := filepath.Join(ghpmDir, "scripts")
	shells := entrypoint.DetectShells()

	binDir, _ := store.BinDir()
	if binDir != "" {
		ghBin := filepath.Join(binDir, "gh")
		if runtime.GOOS == "windows" {
			ghBin += ".exe"
		}
		if _, err := os.Stat(ghBin); err == nil {
			check("gh managed in ~/.ghpm/bin", true, ghBin)
		}
	}

	if shells.POSIX {
		ep := filepath.Join(scriptsDir, "env.sh")
		if _, err := os.Stat(ep); os.IsNotExist(err) {
			check("scripts/env.sh exists", false, "run: ghpm init")
		} else {
			check("scripts/env.sh exists", true, ep)
			sourced := checkSourced(ep, ".bashrc", ".zshrc", ".profile")
			if !sourced {
				fmt.Printf("  [%s] scripts/env.sh sourced in shell config — run: echo 'source %s' >> ~/.bashrc\n", warnLabel, ep)
			} else {
				fmt.Printf("  [%s] scripts/env.sh sourced in shell config\n", passLabel)
			}
		}
	}

	if shells.Nu {
		ep := filepath.Join(scriptsDir, "env.nu")
		if _, err := os.Stat(ep); os.IsNotExist(err) {
			check("scripts/env.nu exists", false, "run: ghpm init")
		} else {
			check("scripts/env.nu exists", true, ep)
		}
	}

	if shells.PWSh {
		ep := filepath.Join(scriptsDir, "env.ps1")
		if _, err := os.Stat(ep); os.IsNotExist(err) {
			check("scripts/env.ps1 exists", false, "run: ghpm init")
		} else {
			check("scripts/env.ps1 exists", true, ep)
		}
	}

	manifestPath := filepath.Join(ghpmDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		check("manifest.json exists", false, manifestPath+" not found")
	} else if err != nil {
		check("manifest.json readable", false, err.Error())
	} else {
		var v any
		jsonErr := json.Unmarshal(data, &v)
		check("manifest.json valid JSON", jsonErr == nil, "")
	}

	settingsPath := filepath.Join(ghpmDir, "settings.json")
	data, err = os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		fmt.Printf("  [%s] settings.json — not present (using defaults)\n", warnLabel)
	} else if err != nil {
		check("settings.json readable", false, err.Error())
	} else {
		var v any
		jsonErr := json.Unmarshal(data, &v)
		check("settings.json valid JSON", jsonErr == nil, "")
	}

	manifest, err := config.LoadManifest()
	pkgsDir, pkgsErr := store.PackagesDir()
	if err == nil && manifest != nil && pkgsErr == nil {
		var stale []string
		for key := range manifest.Installs {
			pkgPath := filepath.Join(pkgsDir, key)
			if _, serr := os.Stat(pkgPath); os.IsNotExist(serr) {
				stale = append(stale, key)
			}
		}
		if len(stale) == 0 {
			fmt.Printf("  [%s] all installed packages present on disk\n", passLabel)
		} else {
			for _, key := range stale {
				fmt.Printf("  [%s] %s — package dir missing from disk\n", warnLabel, key)
			}
		}
	}

	releaseDir, err := store.ReleaseBaseDir()
	if err == nil {
		size := dirSize(releaseDir)
		fmt.Printf("  [%s] cache disk usage — %s (%s)\n", passLabel, humanBytes(size), releaseDir)
	}

	return nil
}

func checkSourced(entrypoint string, rcFiles ...string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	for _, rc := range rcFiles {
		data, err := os.ReadFile(filepath.Join(home, rc))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), entrypoint) {
			return true
		}
	}
	return false
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
		info, err := d.Info()
		if err == nil {
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
