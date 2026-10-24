package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRunCommand(settings *viper.Viper) *cobra.Command {
	command := &cobra.Command{
		Use: "run [flags] message", Short: "Ask a local tape-backed agent", Args: messageArgs,
		RunE: func(command *cobra.Command, args []string) (runErr error) {
			ctx, cancel := context.WithTimeout(command.Context(), settings.GetDuration("timeout"))
			defer cancel()
			a, tape, err := openAgent(ctx, settings)
			if err != nil {
				return err
			}
			defer func() { runErr = errors.Join(runErr, tape.Close()) }()
			answer, err := a.Run(ctx, message(args))
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), answer)
			return err
		},
	}
	modelFlags(command)
	command.Flags().Duration("timeout", time.Minute, "command timeout")
	return command
}
