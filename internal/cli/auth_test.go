package cli

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/meop/golie/ui"

	"github.com/meop/ghpm/internal/fakegh"
	"github.com/meop/ghpm/internal/ghapi"
	"github.com/meop/ghpm/internal/store"
)

func authCmd(t *testing.T, sub string, args ...string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ui.SetOutput(&buf)
	t.Cleanup(func() { ui.SetOutput(os.Stdout) })
	for _, c := range newAuthCmd().Commands() {
		if c.Name() == sub {
			if err := c.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			c.SetContext(cmdWithContext().Context())
			return c, &buf
		}
	}
	t.Fatalf("no auth %s", sub)
	return nil, nil
}

// login keeps a token GitHub takes, readable by its owner alone, and status
// then says whose it is
func TestAuthLogin_KeepsATokenGitHubTakes(t *testing.T) {
	withHome(t)
	fakeAPI(t)
	t.Setenv("GH_TOKEN", "")
	authInput = strings.NewReader(fakegh.Token + "\n")
	t.Cleanup(func() { authInput = os.Stdin })

	cmd, out := authCmd(t, "login", "--with-token")
	if err := runAuthLogin(cmd, nil); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	path, _ := store.TokenFile()
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) != fakegh.Token {
		t.Fatalf("token file: %q, %v", data, err)
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0600 {
		t.Errorf("token file mode %v, want 0600", fi.Mode().Perm())
	}
	if !strings.Contains(out.String(), "as tester") {
		t.Errorf("login says whose token it is:\n%s", out)
	}

	cmd, out = authCmd(t, "status")
	if err := runAuthStatus(cmd, nil); err != nil || !strings.Contains(out.String(), "tester") || !strings.Contains(out.String(), path) {
		t.Errorf("status: %v\n%s", err, out)
	}
	if strings.Contains(out.String(), fakegh.Token) {
		t.Errorf("status printed the token:\n%s", out)
	}
}

// a token GitHub turns away is not kept
func TestAuthLogin_RefusesATokenGitHubTurnsAway(t *testing.T) {
	withHome(t)
	fakeAPI(t)
	t.Setenv("GH_TOKEN", "")
	authInput = strings.NewReader("wrong\n")
	t.Cleanup(func() { authInput = os.Stdin })

	cmd, _ := authCmd(t, "login", "--with-token")
	if err := runAuthLogin(cmd, nil); err == nil {
		t.Fatal("a refused token was taken")
	}
	if tok, _ := ghapi.Token(); tok != "" {
		t.Errorf("a refused token was kept: %q", tok)
	}
}

// GH_TOKEN comes before the kept token, and logout leaves it alone
func TestAuth_EnvComesFirstAndLogoutRemovesOnlyTheFile(t *testing.T) {
	withHome(t)
	fakeAPI(t)
	if _, err := ghapi.SaveToken("kept"); err != nil {
		t.Fatal(err)
	}
	if tok, src := ghapi.Token(); tok != fakegh.Token || src != "GH_TOKEN" {
		t.Errorf("got %q from %s, want GH_TOKEN's", tok, src)
	}
	cmd, out := authCmd(t, "logout")
	if err := runAuthLogout(cmd, nil); err != nil {
		t.Fatal(err)
	}
	path, _ := store.TokenFile()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("token file still there: %v", err)
	}
	if !strings.Contains(out.String(), "GH_TOKEN is still set") {
		t.Errorf("logout says GH_TOKEN is still in use:\n%s", out)
	}
}

func TestAuthStatus_NoToken(t *testing.T) {
	withHome(t)
	fakeAPI(t)
	t.Setenv("GH_TOKEN", "")
	cmd, out := authCmd(t, "status")
	if err := runAuthStatus(cmd, nil); err == nil || !strings.Contains(out.String(), "ghpm auth login") {
		t.Errorf("status with no token: %v\n%s", err, out)
	}
}
