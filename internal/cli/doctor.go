package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

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
	pass := color.New(color.FgGreen).Sprint("PASS")
	fail := color.New(color.FgRed).Sprint("FAIL")
	warn := color.New(color.FgYellow).Sprint("WARN")

	check := func(label string, ok bool, msg string) {
		status := pass
		if !ok {
			status = fail
		}
		fmt.Printf("  [%s] %s", status, label)
		if msg != "" {
			fmt.Printf(" — %s", msg)
		}
		fmt.Println()
	}

	fmt.Println("ghpm doctor")
	fmt.Println(repeatStr("─", 50))

	// 1. gh installed
	ghPath, err := exec.LookPath("gh")
	check("gh installed", err == nil, ghPath)

	// 2. gh authenticated
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

	// 3. ~/.ghpm/bin in PATH
	binDir, err := store.BinDir()
	if err == nil {
		pathEnv := os.Getenv("PATH")
		inPath := strings.Contains(pathEnv, binDir)
		msg := binDir
		if !inPath {
			msg = binDir + " (add to PATH)"
		}
		check("~/.ghpm/bin in PATH", inPath, msg)
	}

	// 4. manifest valid JSON
	manifestPath := filepath.Join(mustGhpmDir(), "manifest.json")
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

	// 5. settings valid (if present)
	settingsPath := filepath.Join(mustGhpmDir(), "settings.json")
	data, err = os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		fmt.Printf("  [%s] settings.json — not present (using defaults)\n", warn)
	} else if err != nil {
		check("settings.json readable", false, err.Error())
	} else {
		var v any
		jsonErr := json.Unmarshal(data, &v)
		check("settings.json valid JSON", jsonErr == nil, "")
	}

	// 6. Disk usage of release cache
	releaseDir, err := store.ReleaseBaseDir()
	if err == nil {
		size := dirSize(releaseDir)
		fmt.Printf("  [%s] cache disk usage — %s (%s)\n", pass, humanBytes(size), releaseDir)
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
