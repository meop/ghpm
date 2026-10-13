package config

import (
	"os"
	"testing"

	"github.com/meop/ghpm/internal/faketool"
)

// TestMain lets the test binary stand in for a fake executable on windows;
// see faketool.
func TestMain(m *testing.M) {
	faketool.Main()
	os.Exit(m.Run())
}
