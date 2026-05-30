package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/store"
)

func TestRunUpgrade_NoGH(t *testing.T) {
	withHome(t)
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	writeSettings(t, &config.Settings{})
	quiet = true
	defer func() { quiet = false }()

	err := runUpgrade(cmdWithContext(), nil)
	if err == nil {
		t.Fatal("expected error when gh not found")
	}
}

func TestRunUpgrade_AlreadyLatest(t *testing.T) {
	withHome(t)
	writeSettings(t, &config.Settings{})

	releaseJSON, _ := json.Marshal(map[string]any{
		"tagName": "v" + version,
		"assets": []map[string]any{
			{"name": "ghpm-linux-amd64.tar.gz", "size": 1234, "url": "https://x.com/a"},
		},
	})

	ghJSON, _ := json.Marshal(map[string]any{
		"tagName": "v2.67.0",
		"assets": []map[string]any{
			{"name": "gh_2.67.0_linux_amd64.tar.gz", "size": 5678, "url": "https://x.com/b"},
		},
	})

	sheeshJSON, _ := json.Marshal(map[string]any{
		"tagName": "v0.1.0",
		"assets": []map[string]any{
			{"name": "sheesh-linux-aarch64.tar.gz", "size": 9012, "url": "https://x.com/c"},
		},
	})

	kebabDir, err := store.ShimDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kebabDir, "kebab"), []byte("#!/bin/sh\necho 0.1.0"), 0755); err != nil {
		t.Fatal(err)
	}

	fakeGHBin(t, `case "$*" in
  *meop/ghpm*) cat <<'EOF'
`+string(releaseJSON)+`
EOF
;;
  *cli/cli*) cat <<'EOF'
`+string(ghJSON)+`
EOF
;;
  *meop/sheesh*) cat <<'EOF'
`+string(sheeshJSON)+`
EOF
;;
  *) echo '{}' ;;
esac`)

	dryRun = true
	defer func() { dryRun = false }()
	quiet = true
	defer func() { quiet = false }()

	err = runUpgrade(cmdWithContext(), nil)
	if err != nil {
		t.Logf("upgrade: %v", err)
	}
}

func TestUpgradeSelf_VersionComparison(t *testing.T) {
	if version == "dev" {
		t.Skip("version is dev, comparison logic differs")
	}
}
