package cli

import (
	"context"

	"github.com/spf13/cobra"
)

type ctxKey struct{}

func cmdWithContext() *cobra.Command {
	cmd := &cobra.Command{}
	ctx := context.WithValue(context.Background(), ctxKey{}, true)
	cmd.SetContext(ctx)
	return cmd
}
