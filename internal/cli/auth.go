package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meop/golie/ui"

	"github.com/meop/ghpm/internal/ghapi"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log ghpm in to GitHub, see how it is, or log it out",
		Long: "ghpm asks GitHub's API with its own token: GH_TOKEN, then GITHUB_TOKEN, then the one `ghpm auth login`\n" +
			"keeps in ~/.ghpm/token. A fine-grained personal access token with Contents: Read-only is enough.",
		Args: cobra.NoArgs,
	}
	login := &cobra.Command{
		Use:   "login",
		Short: "Keep a GitHub token for ghpm, asked for at the terminal or read from stdin",
		Args:  cobra.NoArgs,
		RunE:  runAuthLogin,
	}
	login.Flags().Bool("with-token", false, "Read the token from stdin")
	cmd.AddCommand(
		login,
		&cobra.Command{
			Use:   "status",
			Short: "Say which token ghpm uses, and whose it is",
			Args:  cobra.NoArgs,
			RunE:  runAuthStatus,
		},
		&cobra.Command{
			Use:   "logout",
			Short: "Remove the token `ghpm auth login` kept",
			Args:  cobra.NoArgs,
			RunE:  runAuthLogout,
		},
	)
	return cmd
}

// authInput is where `auth login --with-token` reads from; a var for tests
var authInput io.Reader = os.Stdin

func runAuthLogin(cmd *cobra.Command, args []string) error {
	withToken, _ := cmd.Flags().GetBool("with-token")
	var tok string
	var err error
	if withToken || !ui.Interactive() {
		tok, err = bufio.NewReader(authInput).ReadString('\n')
		if err == io.EOF {
			err = nil
		}
	} else {
		tok, err = ui.ReadSecret(ghapi.TokenPrompt)
	}
	if err != nil {
		printFail(nil, "reading the token: %v", err)
		return errSilent
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		printFail(nil, "no token given")
		return errSilent
	}
	login, err := ghapi.User(cmd.Context(), tok)
	if err != nil {
		printFail(nil, "GitHub did not take the token: %v", err)
		return errSilent
	}
	path, err := ghapi.SaveToken(tok)
	if err != nil {
		printFail(nil, "keeping the token: %v", err)
		return errSilent
	}
	printPass(nil, "logged in to github.com as %s, the token kept in %s", login, path)
	if _, src := ghapi.Token(); src.FromEnv() {
		printWarn(nil, "%s is set, and ghpm uses it before this token", src)
	}
	return nil
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	login, src, err := authStatus(cmd.Context())
	if err != nil {
		printFail(nil, "%v", err)
		return errSilent
	}
	printPass(nil, "logged in to github.com as %s, with the token from %s", login, src)
	return nil
}

// authStatus is whose token ghpm uses, and where it came from
func authStatus(ctx context.Context) (login string, src ghapi.Source, err error) {
	tok, src := ghapi.Token()
	if tok == "" {
		return "", "", ghapi.ErrNoToken
	}
	login, err = ghapi.User(ctx, tok)
	if err != nil {
		return "", src, fmt.Errorf("the token from %s: %w", src, err)
	}
	return login, src, nil
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	removed, err := ghapi.RemoveToken()
	if err != nil {
		printFail(nil, "%v", err)
		return errSilent
	}
	if removed {
		printPass(nil, "logged out: the token is removed")
	} else {
		print("no token kept, so nothing to remove")
	}
	if _, src := ghapi.Token(); src.FromEnv() {
		printWarn(nil, "%s is still set, and ghpm uses it", src)
	}
	return nil
}
